package system

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/buildinfo"
	"github.com/gowebpki/jcs"
)

const (
	cacheTTL                 = 20 * time.Minute
	defaultMaxDownloadBytes  = 500 << 20
	manifestResponseMaxBytes = 4 << 20
	officialCatalogMaxBytes  = 8 << 20

	updateSourceNone            = "none"
	updateSourceManifest        = "manifest"
	updateSourceOfficialCatalog = "official_catalog"

	officialEnvelopeSchema     = "astercloud.signed-envelope.v1"
	officialCatalogIndexSchema = "astercloud.catalog-index.v1"
	officialCoreReleaseSchema  = "astercloud.core-release.v1"
	officialCatalogPurpose     = "catalog_index"
	officialCoreReleasePurpose = "core_release"
)

var (
	ErrNoUpdateAvailable   = errors.New("no update available")
	ErrUpdateNotConfigured = errors.New("update manifest is not configured")
	ErrUpdateUnsupported   = errors.New("one-click update is not supported for this build")
	ErrNoCompatibleAsset   = errors.New("no compatible update asset found")
	ErrChecksumRequired    = errors.New("update asset sha256 checksum is required")
	ErrRestartUnsupported  = errors.New("service restart is not enabled")
	ErrUpdateSignature     = errors.New("official update signature verification failed")
)

type Config struct {
	Version            string
	BuildType          string
	ManifestURL        string
	OfficialCatalogURL string
	OfficialKeyID      string
	OfficialPublicKey  string
	AllowRestart       bool
	DatabaseURL        string
	PluginCacheDir     string
	PluginActiveDir    string
	BackupDir          string
	DiagnosticDir      string
	MaxArchiveBytes    int64
	MaxDownloadBytes   int64
	HTTPClient         *http.Client
}

type Service struct {
	version            string
	buildType          string
	manifestURL        string
	officialCatalogURL string
	officialKeyID      string
	officialPublicKey  string
	allowRestart       bool
	databaseURL        string
	pluginCacheDir     string
	pluginActiveDir    string
	backupDir          string
	diagnosticDir      string
	maxArchiveBytes    int64
	maxDownloadBytes   int64
	client             *http.Client

	mu            sync.Mutex
	cached        *UpdateInfo
	cachedAt      time.Time
	cachedChannel string
}

type UpdateInfo struct {
	CurrentVersion     string       `json:"current_version"`
	LatestVersion      string       `json:"latest_version"`
	HasUpdate          bool         `json:"has_update"`
	ReleaseInfo        *ReleaseInfo `json:"release_info,omitempty"`
	Cached             bool         `json:"cached"`
	Warning            string       `json:"warning,omitempty"`
	BuildType          string       `json:"build_type"`
	UpdateSupported    bool         `json:"update_supported"`
	ManifestConfigured bool         `json:"manifest_configured"`
	RestartSupported   bool         `json:"restart_supported"`
	Channel            string       `json:"channel"`
	Platform           string       `json:"platform"`
	Source             string       `json:"source"`
	SignedMetadata     bool         `json:"signed_metadata"`
}

type ReleaseInfo struct {
	Version     string  `json:"version"`
	Name        string  `json:"name"`
	Notes       string  `json:"notes"`
	PublishedAt string  `json:"published_at"`
	HTMLURL     string  `json:"html_url"`
	Asset       *Asset  `json:"asset,omitempty"`
	Assets      []Asset `json:"assets,omitempty"`
}

type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type ApplyResult struct {
	Message         string `json:"message"`
	OperationID     string `json:"operation_id"`
	NeedRestart     bool   `json:"need_restart"`
	AlreadyUpToDate bool   `json:"already_up_to_date"`
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	ManualAction    string `json:"manual_action,omitempty"`
}

type manifestFile struct {
	Version     string            `json:"version"`
	Channel     string            `json:"channel"`
	Name        string            `json:"name"`
	Notes       string            `json:"notes"`
	PublishedAt string            `json:"published_at"`
	HTMLURL     string            `json:"html_url"`
	Assets      []Asset           `json:"assets"`
	Releases    []manifestRelease `json:"releases"`
}

type manifestRelease struct {
	Version     string  `json:"version"`
	Channel     string  `json:"channel"`
	Name        string  `json:"name"`
	Notes       string  `json:"notes"`
	PublishedAt string  `json:"published_at"`
	HTMLURL     string  `json:"html_url"`
	Assets      []Asset `json:"assets"`
}

type officialEnvelope struct {
	SchemaVersion string          `json:"schema_version"`
	Purpose       string          `json:"purpose"`
	KeyID         string          `json:"key_id"`
	Algorithm     string          `json:"algorithm"`
	IssuedAt      string          `json:"issued_at"`
	ExpiresAt     string          `json:"expires_at,omitempty"`
	Payload       json.RawMessage `json:"payload"`
	Signature     string          `json:"signature"`
}

type officialCatalogIndex struct {
	SchemaVersion  string                     `json:"schema_version"`
	CatalogVersion int64                      `json:"catalog_version"`
	GeneratedAt    time.Time                  `json:"generated_at"`
	CoreReleases   []officialCoreReleaseIndex `json:"core_releases"`
}

type officialCoreReleaseIndex struct {
	PublicID            string           `json:"public_id"`
	Version             string           `json:"version"`
	Channel             string           `json:"channel"`
	SHA256              string           `json:"sha256"`
	SizeBytes           int64            `json:"size_bytes"`
	MinSupportedVersion string           `json:"min_supported_version,omitempty"`
	PublishedAt         *time.Time       `json:"published_at,omitempty"`
	Signature           officialEnvelope `json:"signature"`
}

type officialCoreReleasePayload struct {
	SchemaVersion       string `json:"schema_version"`
	Version             string `json:"version"`
	Channel             string `json:"channel"`
	SHA256              string `json:"sha256"`
	SizeBytes           int64  `json:"size_bytes"`
	URI                 string `json:"uri"`
	MinSupportedVersion string `json:"min_supported_version,omitempty"`
}

func NewService(cfg Config) *Service {
	version := strings.TrimSpace(cfg.Version)
	if version == "" {
		version = buildinfo.Version
	}
	buildType := strings.TrimSpace(cfg.BuildType)
	if buildType == "" {
		buildType = "source"
	}
	maxBytes := cfg.MaxDownloadBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxDownloadBytes
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Service{
		version:            version,
		buildType:          buildType,
		manifestURL:        strings.TrimSpace(cfg.ManifestURL),
		officialCatalogURL: strings.TrimSpace(cfg.OfficialCatalogURL),
		officialKeyID:      strings.TrimSpace(cfg.OfficialKeyID),
		officialPublicKey:  strings.TrimSpace(cfg.OfficialPublicKey),
		allowRestart:       cfg.AllowRestart,
		databaseURL:        strings.TrimSpace(cfg.DatabaseURL),
		pluginCacheDir:     strings.TrimSpace(cfg.PluginCacheDir),
		pluginActiveDir:    strings.TrimSpace(cfg.PluginActiveDir),
		backupDir:          strings.TrimSpace(cfg.BackupDir),
		diagnosticDir:      strings.TrimSpace(cfg.DiagnosticDir),
		maxArchiveBytes:    defaultArchiveBytes(cfg.MaxArchiveBytes),
		maxDownloadBytes:   maxBytes,
		client:             client,
	}
}

func (s *Service) CheckUpdate(ctx context.Context, force bool, channel string) (UpdateInfo, error) {
	channel = normalizeChannel(channel)
	if channel == "manual" {
		info := s.baseInfo(channel)
		info.Warning = "Update channel is manual. Automatic checks are disabled."
		return info, nil
	}
	if !s.updateSourceConfigured() {
		info := s.baseInfo(channel)
		info.Warning = "Update source is not configured. Use manual update or configure a signed catalog or manifest source."
		return info, nil
	}

	if !force {
		if cached, ok := s.cachedInfo(channel); ok {
			return cached, nil
		}
	}

	manifest, source, signedMetadata, err := s.loadUpdateManifest(ctx)
	if err != nil {
		if cached, ok := s.cachedInfo(channel); ok {
			cached.Warning = "Using cached update data: " + err.Error()
			return cached, nil
		}
		info := s.baseInfo(channel)
		info.Warning = err.Error()
		return info, nil
	}

	release, ok := selectRelease(manifest, channel)
	if !ok {
		info := s.baseInfo(channel)
		info.Source = source
		info.SignedMetadata = signedMetadata
		info.Warning = "No release is available for the selected channel."
		s.storeCache(channel, info)
		return info, nil
	}

	asset := selectAsset(release.Assets)
	latest := strings.TrimPrefix(strings.TrimSpace(release.Version), "v")
	info := s.baseInfo(channel)
	info.LatestVersion = latest
	info.HasUpdate = compareVersions(info.CurrentVersion, latest) < 0
	info.ManifestConfigured = true
	info.Source = source
	info.SignedMetadata = signedMetadata
	info.ReleaseInfo = &ReleaseInfo{
		Version:     latest,
		Name:        release.Name,
		Notes:       release.Notes,
		PublishedAt: release.PublishedAt,
		HTMLURL:     release.HTMLURL,
		Asset:       asset,
		Assets:      release.Assets,
	}
	info.UpdateSupported = info.HasUpdate && s.buildType == "release" && asset != nil
	if info.HasUpdate && s.buildType != "release" {
		info.Warning = "This build was not produced as a release artifact. Use manual update for source builds."
	}
	if info.HasUpdate && s.buildType == "release" && asset == nil {
		info.Warning = "No compatible update asset was found for this platform."
	}
	s.storeCache(channel, info)
	return info, nil
}

func (s *Service) PerformUpdate(ctx context.Context, channel string, operationID string) (ApplyResult, error) {
	info, err := s.CheckUpdate(ctx, true, channel)
	if err != nil {
		return ApplyResult{}, err
	}
	if info.SignedMetadata && isUpdateSignatureWarning(info.Warning) {
		return manualUpdateResult(info, operationID), fmt.Errorf("%w: %s", ErrUpdateSignature, info.Warning)
	}
	if !info.ManifestConfigured {
		return manualUpdateResult(info, operationID), ErrUpdateNotConfigured
	}
	if normalizeChannel(channel) == "manual" {
		return manualUpdateResult(info, operationID), ErrUpdateUnsupported
	}
	if !info.HasUpdate {
		return ApplyResult{
			Message:         "Already up to date",
			OperationID:     operationID,
			AlreadyUpToDate: true,
			CurrentVersion:  info.CurrentVersion,
			LatestVersion:   info.LatestVersion,
		}, nil
	}
	if s.buildType != "release" {
		return manualUpdateResult(info, operationID), ErrUpdateUnsupported
	}
	if info.ReleaseInfo == nil || info.ReleaseInfo.Asset == nil {
		return manualUpdateResult(info, operationID), ErrNoCompatibleAsset
	}
	asset := *info.ReleaseInfo.Asset
	if strings.TrimSpace(asset.SHA256) == "" {
		return manualUpdateResult(info, operationID), ErrChecksumRequired
	}
	if err := s.applyAsset(ctx, asset); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{
		Message:        "Update completed. Restart the service to run the new version.",
		OperationID:    operationID,
		NeedRestart:    true,
		CurrentVersion: info.CurrentVersion,
		LatestVersion:  info.LatestVersion,
	}, nil
}

func (s *Service) Rollback(operationID string) (ApplyResult, error) {
	exePath, err := executablePath()
	if err != nil {
		return ApplyResult{}, err
	}
	backupPath := exePath + ".backup"
	if _, err := os.Stat(backupPath); err != nil {
		if os.IsNotExist(err) {
			return ApplyResult{}, fmt.Errorf("no rollback backup found")
		}
		return ApplyResult{}, err
	}
	currentBackup := exePath + ".rollback-current"
	_ = os.Remove(currentBackup)
	if err := os.Rename(exePath, currentBackup); err != nil {
		return ApplyResult{}, fmt.Errorf("prepare rollback: %w", err)
	}
	if err := os.Rename(backupPath, exePath); err != nil {
		_ = os.Rename(currentBackup, exePath)
		return ApplyResult{}, fmt.Errorf("restore backup: %w", err)
	}
	_ = os.Remove(currentBackup)
	return ApplyResult{
		Message:     "Rollback completed. Restart the service to run the restored version.",
		OperationID: operationID,
		NeedRestart: true,
	}, nil
}

func (s *Service) Restart(operationID string, delay time.Duration) (ApplyResult, error) {
	if !s.allowRestart {
		return ApplyResult{
			Message:      "Automatic restart is disabled.",
			OperationID:  operationID,
			ManualAction: "Restart the service manually, or set ASTERROUTER_SERVER_MAINTENANCE_ALLOW_RESTART=true for managed deployments.",
		}, ErrRestartUnsupported
	}
	if delay <= 0 {
		delay = 500 * time.Millisecond
	}
	go func() {
		time.Sleep(delay)
		os.Exit(0)
	}()
	return ApplyResult{
		Message:     "Service restart initiated.",
		OperationID: operationID,
	}, nil
}

func (s *Service) baseInfo(channel string) UpdateInfo {
	return UpdateInfo{
		CurrentVersion:     s.version,
		LatestVersion:      s.version,
		BuildType:          s.buildType,
		UpdateSupported:    false,
		ManifestConfigured: s.updateSourceConfigured(),
		RestartSupported:   s.allowRestart,
		Channel:            channel,
		Platform:           runtime.GOOS + "/" + runtime.GOARCH,
		Source:             s.updateSource(),
		SignedMetadata:     s.officialCatalogFieldsPresent(),
	}
}

func (s *Service) cachedInfo(channel string) (UpdateInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached == nil || s.cachedChannel != channel || time.Since(s.cachedAt) > cacheTTL {
		return UpdateInfo{}, false
	}
	out := *s.cached
	out.Cached = true
	return out, true
}

func (s *Service) storeCache(channel string, info UpdateInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copyInfo := info
	copyInfo.Cached = false
	s.cached = &copyInfo
	s.cachedAt = time.Now()
	s.cachedChannel = channel
}

func (s *Service) fetchManifest(ctx context.Context) (manifestFile, error) {
	if !isHTTPURL(s.manifestURL) {
		return manifestFile{}, fmt.Errorf("update manifest URL must be http or https")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.manifestURL, nil)
	if err != nil {
		return manifestFile{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return manifestFile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return manifestFile{}, fmt.Errorf("manifest request failed with status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, manifestResponseMaxBytes+1))
	if err != nil {
		return manifestFile{}, err
	}
	if len(body) > manifestResponseMaxBytes {
		return manifestFile{}, fmt.Errorf("manifest response exceeds %d bytes", manifestResponseMaxBytes)
	}
	var manifest manifestFile
	if err := json.Unmarshal(body, &manifest); err != nil {
		return manifestFile{}, err
	}
	return manifest, nil
}

func (s *Service) loadUpdateManifest(ctx context.Context) (manifestFile, string, bool, error) {
	if s.officialCatalogFieldsPresent() {
		if !s.officialCatalogConfigured() {
			return manifestFile{}, updateSourceOfficialCatalog, true, fmt.Errorf("%w: official catalog trust material is incomplete", ErrUpdateSignature)
		}
		manifest, err := s.fetchOfficialManifest(ctx)
		return manifest, updateSourceOfficialCatalog, true, err
	}
	manifest, err := s.fetchManifest(ctx)
	return manifest, updateSourceManifest, false, err
}

func (s *Service) fetchOfficialManifest(ctx context.Context) (manifestFile, error) {
	envelope, err := s.fetchOfficialCatalogEnvelope(ctx)
	if err != nil {
		return manifestFile{}, err
	}
	now := time.Now().UTC()
	if err := s.verifyOfficialEnvelope(envelope, officialCatalogPurpose, now); err != nil {
		return manifestFile{}, err
	}
	var index officialCatalogIndex
	if err := json.Unmarshal(envelope.Payload, &index); err != nil {
		return manifestFile{}, fmt.Errorf("decode official catalog payload: %w", err)
	}
	if strings.TrimSpace(index.SchemaVersion) != officialCatalogIndexSchema || index.CatalogVersion <= 0 {
		return manifestFile{}, fmt.Errorf("%w: invalid official catalog schema", ErrUpdateSignature)
	}
	manifest := manifestFile{Releases: make([]manifestRelease, 0, len(index.CoreReleases))}
	for _, item := range index.CoreReleases {
		release, ok, err := s.officialCoreRelease(item, now)
		if err != nil {
			return manifestFile{}, err
		}
		if ok {
			manifest.Releases = append(manifest.Releases, release)
		}
	}
	return manifest, nil
}

func (s *Service) fetchOfficialCatalogEnvelope(ctx context.Context) (officialEnvelope, error) {
	if !isHTTPURL(s.officialCatalogURL) {
		return officialEnvelope{}, fmt.Errorf("official catalog URL must be http or https")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.officialCatalogURL, nil)
	if err != nil {
		return officialEnvelope{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return officialEnvelope{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return officialEnvelope{}, fmt.Errorf("official catalog request failed with status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, officialCatalogMaxBytes+1))
	if err != nil {
		return officialEnvelope{}, err
	}
	if len(body) > officialCatalogMaxBytes {
		return officialEnvelope{}, fmt.Errorf("official catalog response exceeds %d bytes", officialCatalogMaxBytes)
	}
	envelope, err := decodeOfficialEnvelope(body)
	if err != nil {
		return officialEnvelope{}, err
	}
	return envelope, nil
}

func (s *Service) officialCoreRelease(item officialCoreReleaseIndex, now time.Time) (manifestRelease, bool, error) {
	if err := s.verifyOfficialEnvelope(item.Signature, officialCoreReleasePurpose, now); err != nil {
		return manifestRelease{}, false, err
	}
	var payload officialCoreReleasePayload
	if err := json.Unmarshal(item.Signature.Payload, &payload); err != nil {
		return manifestRelease{}, false, fmt.Errorf("decode official core release payload: %w", err)
	}
	if err := validateOfficialCoreRelease(item, payload); err != nil {
		return manifestRelease{}, false, err
	}
	if payload.MinSupportedVersion != "" && compareVersions(s.version, payload.MinSupportedVersion) < 0 {
		return manifestRelease{}, false, nil
	}
	publishedAt := ""
	if item.PublishedAt != nil {
		publishedAt = item.PublishedAt.UTC().Format(time.RFC3339)
	}
	return manifestRelease{
		Version:     payload.Version,
		Channel:     payload.Channel,
		Name:        "AsterRouter " + strings.TrimPrefix(payload.Version, "v"),
		PublishedAt: publishedAt,
		Assets: []Asset{
			{
				Name:   "asterrouter-" + strings.TrimPrefix(payload.Version, "v"),
				URL:    payload.URI,
				SHA256: payload.SHA256,
				Size:   payload.SizeBytes,
			},
		},
	}, true, nil
}

func validateOfficialCoreRelease(item officialCoreReleaseIndex, payload officialCoreReleasePayload) error {
	if strings.TrimSpace(payload.SchemaVersion) != officialCoreReleaseSchema {
		return fmt.Errorf("%w: invalid official core release schema", ErrUpdateSignature)
	}
	if strings.TrimSpace(payload.Version) == "" || strings.TrimSpace(payload.Channel) == "" || strings.TrimSpace(payload.URI) == "" {
		return fmt.Errorf("%w: incomplete official core release payload", ErrUpdateSignature)
	}
	if !isHTTPURL(payload.URI) {
		return fmt.Errorf("%w: official core release uri must be http or https", ErrUpdateSignature)
	}
	if strings.TrimSpace(item.Version) != strings.TrimSpace(payload.Version) ||
		normalizeChannel(item.Channel) != normalizeChannel(payload.Channel) ||
		normalizeSHA256(item.SHA256) != normalizeSHA256(payload.SHA256) ||
		item.SizeBytes != payload.SizeBytes ||
		strings.TrimSpace(item.MinSupportedVersion) != strings.TrimSpace(payload.MinSupportedVersion) {
		return fmt.Errorf("%w: official core release index mismatch", ErrUpdateSignature)
	}
	if payload.SizeBytes <= 0 {
		return fmt.Errorf("%w: official core release size is invalid", ErrUpdateSignature)
	}
	if !validSHA256(payload.SHA256) {
		return fmt.Errorf("%w: official core release checksum is invalid", ErrUpdateSignature)
	}
	return nil
}

func decodeOfficialEnvelope(body []byte) (officialEnvelope, error) {
	var direct officialEnvelope
	if err := json.Unmarshal(body, &direct); err == nil && direct.SchemaVersion != "" {
		return direct, nil
	}
	var wrapped struct {
		Data officialEnvelope `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return officialEnvelope{}, err
	}
	if wrapped.Data.SchemaVersion == "" {
		return officialEnvelope{}, fmt.Errorf("official catalog envelope is missing")
	}
	return wrapped.Data, nil
}

func (s *Service) verifyOfficialEnvelope(envelope officialEnvelope, purpose string, now time.Time) error {
	if envelope.SchemaVersion != officialEnvelopeSchema ||
		envelope.Purpose != purpose ||
		envelope.KeyID != s.officialKeyID ||
		envelope.Algorithm != "Ed25519" ||
		len(envelope.Payload) == 0 {
		return ErrUpdateSignature
	}
	issuedAt, err := time.Parse(time.RFC3339Nano, envelope.IssuedAt)
	if err != nil || issuedAt.After(now.Add(5*time.Minute)) {
		return ErrUpdateSignature
	}
	if envelope.ExpiresAt != "" {
		expiresAt, err := time.Parse(time.RFC3339Nano, envelope.ExpiresAt)
		if err != nil || !expiresAt.After(now) {
			return ErrUpdateSignature
		}
	}
	publicKey, err := decodeBase64Material(s.officialPublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return ErrUpdateSignature
	}
	signature, err := decodeBase64Material(envelope.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return ErrUpdateSignature
	}
	message, err := officialEnvelopeSigningMessage(envelope)
	if err != nil {
		return ErrUpdateSignature
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), message, signature) {
		return ErrUpdateSignature
	}
	return nil
}

func officialEnvelopeSigningMessage(envelope officialEnvelope) ([]byte, error) {
	unsigned := struct {
		SchemaVersion string          `json:"schema_version"`
		Purpose       string          `json:"purpose"`
		KeyID         string          `json:"key_id"`
		Algorithm     string          `json:"algorithm"`
		IssuedAt      string          `json:"issued_at"`
		ExpiresAt     string          `json:"expires_at,omitempty"`
		Payload       json.RawMessage `json:"payload"`
	}{
		SchemaVersion: envelope.SchemaVersion,
		Purpose:       envelope.Purpose,
		KeyID:         envelope.KeyID,
		Algorithm:     envelope.Algorithm,
		IssuedAt:      envelope.IssuedAt,
		ExpiresAt:     envelope.ExpiresAt,
		Payload:       envelope.Payload,
	}
	raw, err := json.Marshal(unsigned)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(raw)
}

func decodeBase64Material(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.RawURLEncoding.DecodeString(value)
}

func selectRelease(manifest manifestFile, channel string) (manifestRelease, bool) {
	releases := manifest.Releases
	if len(releases) == 0 && manifest.Version != "" {
		releases = []manifestRelease{{
			Version:     manifest.Version,
			Channel:     manifest.Channel,
			Name:        manifest.Name,
			Notes:       manifest.Notes,
			PublishedAt: manifest.PublishedAt,
			HTMLURL:     manifest.HTMLURL,
			Assets:      manifest.Assets,
		}}
	}
	var selected manifestRelease
	found := false
	for _, release := range releases {
		if !releaseMatchesChannel(release.Channel, channel) {
			continue
		}
		if !found || compareVersions(selected.Version, release.Version) < 0 {
			selected = release
			found = true
		}
	}
	return selected, found
}

func releaseMatchesChannel(releaseChannel string, channel string) bool {
	releaseChannel = normalizeChannel(releaseChannel)
	channel = normalizeChannel(channel)
	return releaseChannel == channel || releaseChannel == "stable" && channel == ""
}

func selectAsset(assets []Asset) *Asset {
	for _, asset := range assets {
		if platformPartMatches(asset.OS, runtime.GOOS) && platformPartMatches(asset.Arch, runtime.GOARCH) {
			copyAsset := asset
			return &copyAsset
		}
	}
	return nil
}

func platformPartMatches(value string, current string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == current
}

func (s *Service) applyAsset(ctx context.Context, asset Asset) error {
	if !isHTTPURL(asset.URL) {
		return fmt.Errorf("asset URL must be http or https")
	}
	exePath, err := executablePath()
	if err != nil {
		return err
	}
	exeDir := filepath.Dir(exePath)
	tempDir, err := os.MkdirTemp(exeDir, ".asterrouter-update-*")
	if err != nil {
		return fmt.Errorf("create update temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	newBinaryPath := filepath.Join(tempDir, "asterrouter.new")
	if err := s.downloadFile(ctx, asset, newBinaryPath); err != nil {
		return err
	}
	if err := verifySHA256(newBinaryPath, asset.SHA256); err != nil {
		return err
	}
	if err := os.Chmod(newBinaryPath, 0755); err != nil {
		return fmt.Errorf("chmod update asset: %w", err)
	}

	backupPath := exePath + ".backup"
	_ = os.Remove(backupPath)
	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("backup current executable: %w", err)
	}
	if err := os.Rename(newBinaryPath, exePath); err != nil {
		if restoreErr := os.Rename(backupPath, exePath); restoreErr != nil {
			return fmt.Errorf("replace executable failed: %w; restore failed: %v", err, restoreErr)
		}
		return fmt.Errorf("replace executable failed and backup was restored: %w", err)
	}
	return nil
}

func (s *Service) downloadFile(ctx context.Context, asset Asset, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}
	if resp.ContentLength > s.maxDownloadBytes {
		return fmt.Errorf("download exceeds %d bytes", s.maxDownloadBytes)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	written, err := io.Copy(out, io.LimitReader(resp.Body, s.maxDownloadBytes+1))
	if err != nil {
		return err
	}
	if written > s.maxDownloadBytes {
		return fmt.Errorf("download exceeds %d bytes", s.maxDownloadBytes)
	}
	if asset.Size > 0 && written != asset.Size {
		return fmt.Errorf("download size mismatch: expected %d, got %d", asset.Size, written)
	}
	return nil
}

func verifySHA256(path string, want string) error {
	want = strings.ToLower(strings.TrimSpace(want))
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != want {
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", want, got)
	}
	return nil
}

func executablePath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate executable: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	return exePath, nil
}

func manualUpdateResult(info UpdateInfo, operationID string) ApplyResult {
	return ApplyResult{
		Message:        "Manual update is required for this build or platform.",
		OperationID:    operationID,
		CurrentVersion: info.CurrentVersion,
		LatestVersion:  info.LatestVersion,
		ManualAction:   "Download the matching release artifact, verify its checksum, replace the binary, and restart the service.",
	}
}

func normalizeChannel(channel string) string {
	switch strings.TrimSpace(channel) {
	case "beta":
		return "beta"
	case "manual":
		return "manual"
	default:
		return "stable"
	}
}

func isHTTPURL(value string) bool {
	return strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://")
}

func (s *Service) updateSourceConfigured() bool {
	if s.officialCatalogFieldsPresent() {
		return true
	}
	return strings.TrimSpace(s.manifestURL) != ""
}

func (s *Service) updateSource() string {
	if s.officialCatalogFieldsPresent() {
		return updateSourceOfficialCatalog
	}
	if strings.TrimSpace(s.manifestURL) != "" {
		return updateSourceManifest
	}
	return updateSourceNone
}

func (s *Service) officialCatalogFieldsPresent() bool {
	return strings.TrimSpace(s.officialCatalogURL) != "" ||
		strings.TrimSpace(s.officialKeyID) != "" ||
		strings.TrimSpace(s.officialPublicKey) != ""
}

func (s *Service) officialCatalogConfigured() bool {
	return strings.TrimSpace(s.officialCatalogURL) != "" &&
		strings.TrimSpace(s.officialKeyID) != "" &&
		strings.TrimSpace(s.officialPublicKey) != ""
}

func normalizeSHA256(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(normalizeSHA256(value))
	return err == nil && len(decoded) == sha256.Size
}

func isUpdateSignatureWarning(value string) bool {
	return strings.Contains(value, ErrUpdateSignature.Error())
}

func compareVersions(current, latest string) int {
	currentParts := parseVersion(current)
	latestParts := parseVersion(latest)
	for i := 0; i < 3; i++ {
		if currentParts[i] < latestParts[i] {
			return -1
		}
		if currentParts[i] > latestParts[i] {
			return 1
		}
	}
	return 0
}

func parseVersion(value string) [3]int {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(value, ".")
	out := [3]int{}
	for i := 0; i < len(parts) && i < len(out); i++ {
		out[i] = parseVersionPart(parts[i])
	}
	return out
}

func parseVersionPart(value string) int {
	var b strings.Builder
	for _, r := range value {
		if r < '0' || r > '9' {
			break
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return 0
	}
	n, err := strconv.Atoi(b.String())
	if err != nil {
		return 0
	}
	return n
}
