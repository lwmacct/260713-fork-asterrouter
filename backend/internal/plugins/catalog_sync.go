package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	catalogEnvelopeSchema = "astercloud.signed-envelope.v1"
	catalogIndexSchema    = "astercloud.catalog-index.v1"
	catalogIndexPurpose   = "catalog_index"
	advisoryPayloadSchema = "astercloud.security-advisory.v1"
	advisoryPurpose       = "security_advisory"
	catalogSyncSucceeded  = "succeeded"
	catalogSyncFailed     = "failed"
	catalogSyncDisabled   = "disabled"
	maxCatalogBytes       = 8 * 1024 * 1024
)

var (
	ErrCatalogSyncDisabled  = errors.New("official catalog sync is disabled")
	ErrCatalogNotConfigured = errors.New("official catalog sync is not configured")
	ErrCatalogSignature     = errors.New("official catalog signature verification failed")
)

type catalogEnvelope struct {
	SchemaVersion string          `json:"schema_version"`
	Purpose       string          `json:"purpose"`
	KeyID         string          `json:"key_id"`
	Algorithm     string          `json:"algorithm"`
	IssuedAt      string          `json:"issued_at"`
	ExpiresAt     string          `json:"expires_at,omitempty"`
	Payload       json.RawMessage `json:"payload"`
	Signature     string          `json:"signature"`
}

type remoteCatalogIndex struct {
	SchemaVersion  string                  `json:"schema_version"`
	CatalogVersion int64                   `json:"catalog_version"`
	GeneratedAt    time.Time               `json:"generated_at"`
	Plugins        []remoteCatalogPlugin   `json:"plugins"`
	Advisories     []remoteCatalogAdvisory `json:"security_advisories"`
}

type remoteCatalogPlugin struct {
	PublicID   string                 `json:"public_id"`
	PluginID   string                 `json:"plugin_id,omitempty"`
	Slug       string                 `json:"slug"`
	Name       string                 `json:"name"`
	Summary    string                 `json:"summary"`
	Category   string                 `json:"category"`
	VendorName string                 `json:"vendor_name"`
	Visibility string                 `json:"visibility"`
	Tier       string                 `json:"tier"`
	Versions   []remoteCatalogVersion `json:"versions"`
}

type remoteCatalogVersion struct {
	PublicID            string                 `json:"public_id"`
	Version             string                 `json:"version"`
	Channel             string                 `json:"channel"`
	Status              string                 `json:"status"`
	MinCoreVersion      string                 `json:"min_core_version,omitempty"`
	MaxCoreVersion      string                 `json:"max_core_version,omitempty"`
	RequiredEntitlement bool                   `json:"required_entitlement"`
	Compatibility       []remoteCompatibility  `json:"compatibility,omitempty"`
	Packages            []remoteCatalogPackage `json:"packages,omitempty"`
}

type remoteCompatibility struct {
	CoreVersionRange string `json:"core_version_range"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	Result           string `json:"result"`
}

type remoteCatalogPackage struct {
	PublicID  string          `json:"public_id"`
	OS        string          `json:"os"`
	Arch      string          `json:"arch"`
	SHA256    string          `json:"sha256"`
	SizeBytes int64           `json:"size_bytes"`
	Signature catalogEnvelope `json:"signature"`
	Revoked   bool            `json:"revoked"`
}

type remoteCatalogAdvisory struct {
	PublicID    string                  `json:"public_id"`
	AdvisoryID  string                  `json:"advisory_id"`
	Severity    string                  `json:"severity"`
	Title       string                  `json:"title"`
	Summary     string                  `json:"summary"`
	PublishedAt *time.Time              `json:"published_at,omitempty"`
	Affected    []remoteAffectedVersion `json:"affected_versions,omitempty"`
	Signature   catalogEnvelope         `json:"signature"`
}

type remoteAffectedVersion struct {
	PublicID     string    `json:"public_id"`
	AdvisoryID   string    `json:"advisory_id"`
	PluginID     string    `json:"plugin_id"`
	PluginSlug   string    `json:"plugin_slug,omitempty"`
	VersionRange string    `json:"version_range"`
	FixedVersion string    `json:"fixed_version"`
	Revoked      bool      `json:"revoked"`
	CreatedAt    time.Time `json:"created_at"`
}

type advisorySignaturePayload struct {
	SchemaVersion string    `json:"schema_version"`
	AdvisoryID    string    `json:"advisory_id"`
	Severity      string    `json:"severity"`
	Title         string    `json:"title"`
	Summary       string    `json:"summary"`
	PublishedAt   time.Time `json:"published_at"`
}

func (s *Service) OfficialCatalogStatus(ctx context.Context) (OfficialCatalogStatus, error) {
	record, ok, err := s.repo.LatestCatalogSnapshot(ctx)
	if err != nil {
		return OfficialCatalogStatus{}, err
	}
	if !ok {
		cfg, err := s.effectiveCatalogConfig(ctx)
		if err != nil {
			cfg = normalizeOfficialCatalogConfig(s.catalogConfig)
		}
		return OfficialCatalogStatus{
			Mode:            cfg.Mode,
			BootstrapURL:    cfg.BootstrapURL,
			SourceURL:       cfg.URL,
			LicenseURL:      catalogLicenseURL(cfg),
			TrustConfigured: catalogTrustConfigured(cfg),
			Status:          initialCatalogStatus(cfg),
		}, nil
	}
	status := record.OfficialStatus()
	cfg, err := s.effectiveCatalogConfig(ctx)
	if err != nil {
		cfg = normalizeOfficialCatalogConfig(s.catalogConfig)
	}
	status.BootstrapURL = cfg.BootstrapURL
	if status.SourceURL == "" {
		status.SourceURL = cfg.URL
	}
	status.LicenseURL = catalogLicenseURL(cfg)
	status.TrustConfigured = catalogTrustConfigured(cfg)
	return status, nil
}

func (s *Service) SyncOfficialCatalog(ctx context.Context) (OfficialCatalogStatus, error) {
	cfg, err := s.effectiveCatalogConfig(ctx)
	if err != nil {
		fallback := normalizeOfficialCatalogConfig(s.catalogConfig)
		status := OfficialCatalogStatus{Mode: fallback.Mode, BootstrapURL: fallback.BootstrapURL, SourceURL: fallback.URL, LicenseURL: catalogLicenseURL(fallback), TrustConfigured: catalogTrustConfigured(fallback), Status: catalogSyncFailed, Error: err.Error(), SyncedAt: s.now().UTC()}
		_ = s.repo.SaveCatalogSnapshot(ctx, snapshotFromStatus(status, "", ""))
		return status, err
	}
	if cfg.Mode == CatalogModeDisabled {
		status := OfficialCatalogStatus{Mode: cfg.Mode, BootstrapURL: cfg.BootstrapURL, SourceURL: cfg.URL, LicenseURL: catalogLicenseURL(cfg), TrustConfigured: catalogTrustConfigured(cfg), Status: catalogSyncDisabled}
		return status, ErrCatalogSyncDisabled
	}
	if cfg.Mode != CatalogModeOnline && cfg.Mode != CatalogModePrivateMirror {
		return s.OfficialCatalogStatus(ctx)
	}
	if cfg.URL == "" || cfg.PublicKeyID == "" || cfg.PublicKeyBase64 == "" {
		status := OfficialCatalogStatus{Mode: cfg.Mode, BootstrapURL: cfg.BootstrapURL, SourceURL: cfg.URL, LicenseURL: catalogLicenseURL(cfg), TrustConfigured: catalogTrustConfigured(cfg), Status: catalogSyncFailed, Error: ErrCatalogNotConfigured.Error(), SyncedAt: s.now().UTC()}
		_ = s.repo.SaveCatalogSnapshot(ctx, snapshotFromStatus(status, "", ""))
		return status, ErrCatalogNotConfigured
	}
	envelope, rawPayload, err := s.fetchCatalogEnvelope(ctx, cfg.URL)
	if err != nil {
		status := OfficialCatalogStatus{Mode: cfg.Mode, BootstrapURL: cfg.BootstrapURL, SourceURL: cfg.URL, LicenseURL: catalogLicenseURL(cfg), TrustConfigured: catalogTrustConfigured(cfg), Status: catalogSyncFailed, Error: err.Error(), SyncedAt: s.now().UTC()}
		_ = s.repo.SaveCatalogSnapshot(ctx, snapshotFromStatus(status, "", ""))
		return status, err
	}
	if err := verifyCatalogEnvelope(envelope, cfg, s.now().UTC()); err != nil {
		status := OfficialCatalogStatus{Mode: cfg.Mode, BootstrapURL: cfg.BootstrapURL, SourceURL: cfg.URL, LicenseURL: catalogLicenseURL(cfg), TrustConfigured: catalogTrustConfigured(cfg), Status: catalogSyncFailed, Error: err.Error(), KeyID: envelope.KeyID, SyncedAt: s.now().UTC()}
		_ = s.repo.SaveCatalogSnapshot(ctx, snapshotFromStatus(status, string(rawPayload), envelope.Signature))
		return status, err
	}
	var index remoteCatalogIndex
	if err := json.Unmarshal(rawPayload, &index); err != nil {
		status := OfficialCatalogStatus{Mode: cfg.Mode, BootstrapURL: cfg.BootstrapURL, SourceURL: cfg.URL, LicenseURL: catalogLicenseURL(cfg), TrustConfigured: catalogTrustConfigured(cfg), Status: catalogSyncFailed, Error: "decode catalog payload: " + err.Error(), KeyID: envelope.KeyID, SyncedAt: s.now().UTC()}
		_ = s.repo.SaveCatalogSnapshot(ctx, snapshotFromStatus(status, string(rawPayload), envelope.Signature))
		return status, err
	}
	if strings.TrimSpace(index.SchemaVersion) != catalogIndexSchema || index.CatalogVersion <= 0 {
		err := fmt.Errorf("invalid official catalog schema")
		status := OfficialCatalogStatus{Mode: cfg.Mode, BootstrapURL: cfg.BootstrapURL, SourceURL: cfg.URL, LicenseURL: catalogLicenseURL(cfg), TrustConfigured: catalogTrustConfigured(cfg), Status: catalogSyncFailed, Error: err.Error(), KeyID: envelope.KeyID, SyncedAt: s.now().UTC()}
		_ = s.repo.SaveCatalogSnapshot(ctx, snapshotFromStatus(status, string(rawPayload), envelope.Signature))
		return status, err
	}
	plugins := mapRemoteCatalogPlugins(index, s.now().UTC())
	packages := mapRemoteCatalogPackages(index, s.now().UTC())
	advisories, err := mapVerifiedRemoteAdvisories(index, cfg, s.now().UTC())
	if err != nil {
		status := OfficialCatalogStatus{Mode: cfg.Mode, BootstrapURL: cfg.BootstrapURL, SourceURL: cfg.URL, LicenseURL: catalogLicenseURL(cfg), TrustConfigured: catalogTrustConfigured(cfg), Status: catalogSyncFailed, Error: err.Error(), KeyID: envelope.KeyID, SyncedAt: s.now().UTC()}
		_ = s.repo.SaveCatalogSnapshot(ctx, snapshotFromStatus(status, string(rawPayload), envelope.Signature))
		return status, err
	}
	for _, plugin := range plugins {
		if err := s.saveRemotePlugin(ctx, plugin); err != nil {
			status := OfficialCatalogStatus{Mode: cfg.Mode, BootstrapURL: cfg.BootstrapURL, SourceURL: cfg.URL, LicenseURL: catalogLicenseURL(cfg), TrustConfigured: catalogTrustConfigured(cfg), Status: catalogSyncFailed, Error: err.Error(), KeyID: envelope.KeyID, SyncedAt: s.now().UTC()}
			_ = s.repo.SaveCatalogSnapshot(ctx, snapshotFromStatus(status, string(rawPayload), envelope.Signature))
			return status, err
		}
	}
	for _, record := range packages {
		if err := s.repo.SavePackage(ctx, record); err != nil {
			status := OfficialCatalogStatus{Mode: cfg.Mode, BootstrapURL: cfg.BootstrapURL, SourceURL: cfg.URL, LicenseURL: catalogLicenseURL(cfg), TrustConfigured: catalogTrustConfigured(cfg), Status: catalogSyncFailed, Error: err.Error(), KeyID: envelope.KeyID, SyncedAt: s.now().UTC()}
			_ = s.repo.SaveCatalogSnapshot(ctx, snapshotFromStatus(status, string(rawPayload), envelope.Signature))
			return status, err
		}
	}
	for _, record := range advisories {
		if err := s.repo.SaveAdvisory(ctx, record); err != nil {
			status := OfficialCatalogStatus{Mode: cfg.Mode, BootstrapURL: cfg.BootstrapURL, SourceURL: cfg.URL, LicenseURL: catalogLicenseURL(cfg), TrustConfigured: catalogTrustConfigured(cfg), Status: catalogSyncFailed, Error: err.Error(), KeyID: envelope.KeyID, SyncedAt: s.now().UTC()}
			_ = s.repo.SaveCatalogSnapshot(ctx, snapshotFromStatus(status, string(rawPayload), envelope.Signature))
			return status, err
		}
	}
	payloadHash := sha256.Sum256(rawPayload)
	status := OfficialCatalogStatus{
		Mode:            cfg.Mode,
		BootstrapURL:    cfg.BootstrapURL,
		SourceURL:       cfg.URL,
		LicenseURL:      catalogLicenseURL(cfg),
		TrustConfigured: catalogTrustConfigured(cfg),
		CatalogVersion:  index.CatalogVersion,
		PayloadSHA256:   hex.EncodeToString(payloadHash[:]),
		KeyID:           envelope.KeyID,
		PluginCount:     len(plugins),
		AdvisoryCount:   len(advisories),
		Status:          catalogSyncSucceeded,
		SyncedAt:        s.now().UTC(),
	}
	if err := s.repo.SaveCatalogSnapshot(ctx, snapshotFromStatus(status, string(rawPayload), envelope.Signature)); err != nil {
		return OfficialCatalogStatus{}, err
	}
	return status, nil
}

func (s *Service) fetchCatalogEnvelope(ctx context.Context, sourceURL string) (catalogEnvelope, json.RawMessage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return catalogEnvelope{}, nil, err
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return catalogEnvelope{}, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return catalogEnvelope{}, nil, fmt.Errorf("official catalog returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxCatalogBytes+1))
	if err != nil {
		return catalogEnvelope{}, nil, err
	}
	if len(body) > maxCatalogBytes {
		return catalogEnvelope{}, nil, fmt.Errorf("official catalog response exceeds maximum size")
	}
	envelope, err := decodeCatalogEnvelope(body)
	if err != nil {
		return catalogEnvelope{}, nil, err
	}
	return envelope, envelope.Payload, nil
}

func decodeCatalogEnvelope(body []byte) (catalogEnvelope, error) {
	var direct catalogEnvelope
	if err := json.Unmarshal(body, &direct); err == nil && direct.SchemaVersion != "" {
		return direct, nil
	}
	var wrapped struct {
		Data catalogEnvelope `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return catalogEnvelope{}, err
	}
	if wrapped.Data.SchemaVersion == "" {
		return catalogEnvelope{}, fmt.Errorf("official catalog envelope is missing")
	}
	return wrapped.Data, nil
}

func verifyCatalogEnvelope(envelope catalogEnvelope, cfg OfficialCatalogConfig, now time.Time) error {
	return verifySignedEnvelope(envelope, cfg, catalogIndexPurpose, now)
}

func mapVerifiedRemoteAdvisories(index remoteCatalogIndex, cfg OfficialCatalogConfig, now time.Time) ([]advisoryRecord, error) {
	out := make([]advisoryRecord, 0, len(index.Advisories))
	for _, item := range index.Advisories {
		record, err := verifiedRemoteAdvisory(item, cfg, now)
		if err != nil {
			return nil, err
		}
		if record.PublicID != "" {
			out = append(out, record)
		}
	}
	return out, nil
}

func verifiedRemoteAdvisory(item remoteCatalogAdvisory, cfg OfficialCatalogConfig, now time.Time) (advisoryRecord, error) {
	publicID := strings.TrimSpace(item.PublicID)
	if publicID == "" {
		return advisoryRecord{}, nil
	}
	if err := verifySignedEnvelope(item.Signature, cfg, advisoryPurpose, now); err != nil {
		return advisoryRecord{}, ErrCatalogSignature
	}
	var payload advisorySignaturePayload
	if err := json.Unmarshal(item.Signature.Payload, &payload); err != nil {
		return advisoryRecord{}, ErrCatalogSignature
	}
	if !advisoryPayloadMatches(item, payload) {
		return advisoryRecord{}, ErrCatalogSignature
	}
	signatureJSON, err := json.Marshal(item.Signature)
	if err != nil {
		return advisoryRecord{}, err
	}
	publishedAt := payload.PublishedAt.UTC()
	if item.PublishedAt != nil {
		publishedAt = item.PublishedAt.UTC()
	}
	record := advisoryRecord{
		PublicID:      publicID,
		AdvisoryID:    strings.TrimSpace(item.AdvisoryID),
		Severity:      strings.TrimSpace(item.Severity),
		Title:         strings.TrimSpace(item.Title),
		Summary:       strings.TrimSpace(item.Summary),
		PublishedAt:   publishedAt,
		SignatureJSON: string(signatureJSON),
		SyncedAt:      now,
	}
	for _, affected := range item.Affected {
		if strings.TrimSpace(affected.PublicID) == "" || strings.TrimSpace(affected.VersionRange) == "" {
			continue
		}
		createdAt := affected.CreatedAt.UTC()
		if createdAt.IsZero() {
			createdAt = now
		}
		record.Affected = append(record.Affected, affectedVersionRecord{
			PublicID:         strings.TrimSpace(affected.PublicID),
			AdvisoryPublicID: record.PublicID,
			AdvisoryID:       record.AdvisoryID,
			AdvisorySeverity: record.Severity,
			AdvisoryTitle:    record.Title,
			PluginID:         strings.TrimSpace(affected.PluginID),
			PluginSlug:       sanitizeCatalogSlug(affected.PluginSlug),
			VersionRange:     strings.TrimSpace(affected.VersionRange),
			FixedVersion:     strings.TrimSpace(affected.FixedVersion),
			Revoked:          affected.Revoked,
			CreatedAt:        createdAt,
		})
	}
	return record, nil
}

func advisoryPayloadMatches(item remoteCatalogAdvisory, payload advisorySignaturePayload) bool {
	if payload.SchemaVersion != advisoryPayloadSchema {
		return false
	}
	if strings.TrimSpace(payload.AdvisoryID) != strings.TrimSpace(item.AdvisoryID) ||
		defaultString(strings.TrimSpace(payload.Severity), "medium") != strings.TrimSpace(item.Severity) ||
		strings.TrimSpace(payload.Title) != strings.TrimSpace(item.Title) ||
		strings.TrimSpace(payload.Summary) != strings.TrimSpace(item.Summary) {
		return false
	}
	if item.PublishedAt != nil && !payload.PublishedAt.UTC().Equal(item.PublishedAt.UTC()) {
		return false
	}
	return !payload.PublishedAt.IsZero()
}

func (s *Service) saveRemotePlugin(ctx context.Context, plugin Plugin) error {
	current, ok, err := s.repo.FindPlugin(ctx, plugin.ID)
	if err != nil {
		return err
	}
	targetStatus := plugin.Status
	if ok {
		plugin.CreatedAt = current.CreatedAt
		if current.Status == StatusEnabled && plugin.Status != StatusLocked {
			targetStatus = StatusEnabled
		}
	}
	if err := s.repo.SavePlugin(ctx, plugin); err != nil {
		return err
	}
	return s.repo.UpdateStatus(ctx, plugin.ID, targetStatus, plugin.UpdatedAt)
}

func latestCatalogVersion(versions []remoteCatalogVersion) (string, bool) {
	for _, version := range versions {
		if version.Status == "published" || version.Status == "deprecated" {
			return version.Version, version.RequiredEntitlement
		}
	}
	return "", false
}

func remoteTier(value string, requiresEntitlement bool) (string, string, string) {
	switch strings.TrimSpace(value) {
	case "free":
		return TierFreeCore, EntitlementFree, StatusDisabled
	default:
		if requiresEntitlement {
			return TierPaidAddon, EntitlementMissing, StatusLocked
		}
		return TierFreeCore, EntitlementFree, StatusDisabled
	}
}

func normalizeOfficialCatalogConfig(cfg OfficialCatalogConfig) OfficialCatalogConfig {
	cfg.Mode = strings.TrimSpace(cfg.Mode)
	if cfg.Mode == "" {
		cfg.Mode = CatalogModeDisabled
	}
	switch cfg.Mode {
	case CatalogModeOnline, CatalogModePrivateMirror, CatalogModeOffline, CatalogModeDisabled:
	default:
		cfg.Mode = CatalogModeDisabled
	}
	cfg.BootstrapURL = normalizeBootstrapURL(cfg.BootstrapURL)
	cfg.URL = strings.TrimSpace(cfg.URL)
	cfg.LicenseURL = strings.TrimSpace(cfg.LicenseURL)
	cfg.PublicKeyID = strings.TrimSpace(cfg.PublicKeyID)
	cfg.PublicKeyBase64 = strings.TrimSpace(cfg.PublicKeyBase64)
	return cfg
}

func initialCatalogStatus(cfg OfficialCatalogConfig) string {
	if cfg.Mode == CatalogModeDisabled {
		return catalogSyncDisabled
	}
	return "not_synced"
}

func snapshotFromStatus(status OfficialCatalogStatus, payloadJSON string, signature string) catalogSnapshotRecord {
	return catalogSnapshotRecord{
		ID:             "ocs_" + randomID(18),
		Mode:           status.Mode,
		SourceURL:      status.SourceURL,
		CatalogVersion: status.CatalogVersion,
		PayloadSHA256:  status.PayloadSHA256,
		KeyID:          status.KeyID,
		Signature:      signature,
		PluginCount:    status.PluginCount,
		AdvisoryCount:  status.AdvisoryCount,
		Status:         status.Status,
		Error:          status.Error,
		PayloadJSON:    payloadJSON,
		SyncedAt:       status.SyncedAt,
	}
}

func catalogLicenseURL(cfg OfficialCatalogConfig) string {
	if strings.TrimSpace(cfg.LicenseURL) != "" {
		return strings.TrimSpace(cfg.LicenseURL)
	}
	return deriveLicenseURL(cfg.URL)
}

func catalogTrustConfigured(cfg OfficialCatalogConfig) bool {
	return strings.TrimSpace(cfg.PublicKeyID) != "" && strings.TrimSpace(cfg.PublicKeyBase64) != ""
}

func (r catalogSnapshotRecord) OfficialStatus() OfficialCatalogStatus {
	return OfficialCatalogStatus{
		Mode:           r.Mode,
		SourceURL:      r.SourceURL,
		CatalogVersion: r.CatalogVersion,
		PayloadSHA256:  r.PayloadSHA256,
		KeyID:          r.KeyID,
		PluginCount:    r.PluginCount,
		AdvisoryCount:  r.AdvisoryCount,
		Status:         r.Status,
		Error:          r.Error,
		SyncedAt:       r.SyncedAt,
	}
}
