package plugins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
)

const (
	licenseEnvelopePurpose = "license_snapshot"
	licenseSnapshotSchema  = "astercloud.license-snapshot.v1"
	maxLicenseBytes        = 2 * 1024 * 1024
)

var (
	ErrLicenseNotConfigured = errors.New("official license trust is not configured")
	ErrLicenseNotFound      = errors.New("local license is not imported")
	ErrLicenseSignature     = errors.New("license signature verification failed")
	ErrLicenseInvalid       = errors.New("license snapshot is invalid")
	ErrLicenseActivation    = errors.New("official license activation failed")
	ErrLicenseRedeem        = errors.New("official license redemption failed")
	ErrLicenseBinding       = errors.New("license snapshot is bound to another instance")
)

type licenseSnapshotPayload struct {
	SchemaVersion   string           `json:"schema_version"`
	SnapshotID      string           `json:"snapshot_id"`
	SnapshotVersion int64            `json:"snapshot_version"`
	License         snapshotLicense  `json:"license"`
	Customer        snapshotCustomer `json:"customer"`
	SKU             snapshotSKU      `json:"sku"`
	Instance        snapshotInstance `json:"instance"`
	Entitlements    []Entitlement    `json:"entitlements"`
	IssuedAt        time.Time        `json:"issued_at"`
	ExpiresAt       time.Time        `json:"expires_at"`
	Raw             json.RawMessage  `json:"-"`
}

type snapshotLicense struct {
	PublicID  string     `json:"public_id"`
	Edition   string     `json:"edition"`
	Status    string     `json:"status"`
	Seats     int        `json:"seats"`
	StartsAt  time.Time  `json:"starts_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type snapshotCustomer struct {
	PublicID string `json:"public_id"`
}

type snapshotSKU struct {
	PublicID string          `json:"public_id"`
	Code     string          `json:"code"`
	Features json.RawMessage `json:"features"`
	Limits   json.RawMessage `json:"limits"`
}

type snapshotInstance struct {
	PublicID         string    `json:"public_id"`
	Fingerprint      string    `json:"fingerprint"`
	DisplayName      string    `json:"display_name,omitempty"`
	FirstActivatedAt time.Time `json:"first_activated_at"`
}

type activationResponse struct {
	Envelope catalogEnvelope `json:"envelope"`
}

func (s *Service) LicenseStatus(ctx context.Context) (LicenseStatus, error) {
	record, ok, err := s.repo.LatestLicense(ctx)
	if err != nil {
		return LicenseStatus{}, err
	}
	if !ok {
		return LicenseStatus{
			Configured: s.licenseTrustConfigured(ctx),
			Status:     "not_imported",
		}, nil
	}
	status := licenseStatusFromRecord(record)
	if s.now().UTC().After(record.ExpiresAt) && status.Status == LicenseStatusActive {
		status.Status = LicenseStatusExpired
	}
	return status, nil
}

func (s *Service) ActivateLicense(ctx context.Context, request LicenseActivateRequest) (LicenseStatus, error) {
	cfg, err := s.effectiveLicenseConfig(ctx)
	if err != nil {
		return LicenseStatus{}, err
	}
	if cfg.URL == "" || cfg.PublicKeyID == "" || cfg.PublicKeyBase64 == "" {
		return LicenseStatus{}, ErrLicenseNotConfigured
	}
	secret := strings.TrimSpace(request.ActivationSecret)
	if strings.TrimSpace(request.LicenseID) == "" || secret == "" {
		return LicenseStatus{}, fmt.Errorf("%w: license_id and activation_secret are required", ErrLicenseActivation)
	}
	endpoint, err := licenseActivationURL(cfg.URL)
	if err != nil {
		return LicenseStatus{}, err
	}
	body, err := json.Marshal(map[string]string{
		"license_id":           strings.TrimSpace(request.LicenseID),
		"activation_secret":    secret,
		"instance_id":          defaultString(strings.TrimSpace(request.InstanceID), cfg.InstanceID),
		"instance_fingerprint": defaultString(strings.TrimSpace(request.Fingerprint), cfg.Fingerprint),
		"display_name":         defaultString(strings.TrimSpace(request.DisplayName), cfg.DisplayName),
	})
	if err != nil {
		return LicenseStatus{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return LicenseStatus{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	response, err := s.httpClient.Do(httpReq)
	if err != nil {
		return LicenseStatus{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return LicenseStatus{}, fmt.Errorf("%w: status %d", ErrLicenseActivation, response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxLicenseBytes+1))
	if err != nil {
		return LicenseStatus{}, err
	}
	if len(raw) > maxLicenseBytes {
		return LicenseStatus{}, fmt.Errorf("license activation response exceeds maximum size")
	}
	envelope, err := decodeActivationEnvelope(raw)
	if err != nil {
		return LicenseStatus{}, err
	}
	return s.saveLicenseEnvelope(ctx, envelope, secret, s.requestLicenseBinding(request.InstanceID, request.Fingerprint))
}

// RedeemLicense sends a one-time code to the official service. The local
// instance never interprets or stores the code; it only accepts a signed,
// instance-bound license envelope in response.
func (s *Service) RedeemLicense(ctx context.Context, request LicenseRedeemRequest) (LicenseStatus, error) {
	cfg, err := s.effectiveLicenseConfig(ctx)
	if err != nil {
		return LicenseStatus{}, err
	}
	if strings.TrimSpace(cfg.RedeemURL) == "" || cfg.PublicKeyID == "" || cfg.PublicKeyBase64 == "" {
		return LicenseStatus{}, ErrLicenseNotConfigured
	}
	code := strings.TrimSpace(request.Code)
	if code == "" {
		return LicenseStatus{}, fmt.Errorf("%w: code is required", ErrLicenseRedeem)
	}
	instanceID := defaultString(strings.TrimSpace(request.InstanceID), cfg.InstanceID)
	fingerprint := defaultString(strings.TrimSpace(request.Fingerprint), cfg.Fingerprint)
	endpoint, err := licenseRedeemURL(cfg.RedeemURL)
	if err != nil {
		return LicenseStatus{}, err
	}
	body, err := json.Marshal(map[string]string{
		"code":                 code,
		"instance_id":          instanceID,
		"instance_fingerprint": fingerprint,
		"display_name":         defaultString(strings.TrimSpace(request.DisplayName), cfg.DisplayName),
	})
	if err != nil {
		return LicenseStatus{}, err
	}
	hash := sha256.Sum256([]byte(instanceID + "|" + code))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return LicenseStatus{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Aster-Idempotency-Key", hex.EncodeToString(hash[:]))
	httpReq.Header.Set("X-Aster-Core-Version", s.coreVersion)
	response, err := s.httpClient.Do(httpReq)
	if err != nil {
		return LicenseStatus{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return LicenseStatus{}, fmt.Errorf("%w: status %d", ErrLicenseRedeem, response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxLicenseBytes+1))
	if err != nil {
		return LicenseStatus{}, err
	}
	if len(raw) > maxLicenseBytes {
		return LicenseStatus{}, fmt.Errorf("license redemption response exceeds maximum size")
	}
	envelope, err := decodeActivationEnvelope(raw)
	if err != nil {
		return LicenseStatus{}, err
	}
	return s.saveLicenseEnvelope(ctx, envelope, "", s.requestLicenseBinding(request.InstanceID, request.Fingerprint))
}

func (s *Service) requestLicenseBinding(instanceID string, fingerprint string) licenseBinding {
	return licenseBinding{
		InstanceID:  defaultString(strings.TrimSpace(instanceID), s.licenseInstanceID),
		Fingerprint: defaultString(strings.TrimSpace(fingerprint), s.licenseFingerprint),
	}
}

func (s *Service) ImportLicense(ctx context.Context, request LicenseImportRequest) (LicenseStatus, error) {
	envelope, err := decodeLicenseImportEnvelope(request)
	if err != nil {
		return LicenseStatus{}, err
	}
	return s.saveLicenseEnvelope(ctx, envelope, strings.TrimSpace(request.ActivationSecret), licenseBinding{})
}

type licenseBinding struct {
	InstanceID  string
	Fingerprint string
}

func (s *Service) saveLicenseEnvelope(ctx context.Context, envelope catalogEnvelope, activationSecret string, binding licenseBinding) (LicenseStatus, error) {
	payload, rawEnvelope, err := s.verifyLicenseEnvelope(ctx, envelope, binding)
	if err != nil {
		return LicenseStatus{}, err
	}
	now := s.now().UTC()
	ciphertext := ""
	hint := ""
	if activationSecret != "" {
		ciphertext, err = encryptSecret(s.secretKey, activationSecret)
		if err != nil {
			return LicenseStatus{}, err
		}
		hint = maskSecret(activationSecret)
	}
	entitlementsJSON := jsonString(payload.Entitlements, "[]")
	sum := sha256.Sum256(rawEnvelope)
	record := licenseRecord{
		LicenseID:                  strings.TrimSpace(payload.License.PublicID),
		CustomerID:                 strings.TrimSpace(payload.Customer.PublicID),
		InstanceID:                 strings.TrimSpace(payload.Instance.PublicID),
		SnapshotVersion:            payload.SnapshotVersion,
		Status:                     payload.License.Status,
		Edition:                    payload.License.Edition,
		KeyID:                      envelope.KeyID,
		EnvelopeSHA256:             hex.EncodeToString(sum[:]),
		EnvelopeJSON:               string(rawEnvelope),
		ActivationSecretCiphertext: ciphertext,
		ActivationSecretHint:       hint,
		EntitlementsJSON:           entitlementsJSON,
		IssuedAt:                   payload.IssuedAt,
		ExpiresAt:                  payload.ExpiresAt,
		ImportedAt:                 now,
		UpdatedAt:                  now,
	}
	if err := s.repo.SaveLicense(ctx, record); err != nil {
		return LicenseStatus{}, err
	}
	if err := s.refreshEntitledPlugins(ctx); err != nil {
		return LicenseStatus{}, err
	}
	return licenseStatusFromRecord(record), nil
}

func (s *Service) verifyLicenseEnvelope(ctx context.Context, envelope catalogEnvelope, binding licenseBinding) (licenseSnapshotPayload, []byte, error) {
	cfg, err := s.effectiveLicenseConfig(ctx)
	if err != nil {
		return licenseSnapshotPayload{}, nil, err
	}
	if cfg.PublicKeyID == "" || cfg.PublicKeyBase64 == "" {
		return licenseSnapshotPayload{}, nil, ErrLicenseNotConfigured
	}
	if err := verifySignedEnvelope(envelope, OfficialCatalogConfig{
		PublicKeyID:     cfg.PublicKeyID,
		PublicKeyBase64: cfg.PublicKeyBase64,
	}, licenseEnvelopePurpose, s.now().UTC()); err != nil {
		return licenseSnapshotPayload{}, nil, ErrLicenseSignature
	}
	var payload licenseSnapshotPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return licenseSnapshotPayload{}, nil, ErrLicenseInvalid
	}
	payload.Raw = envelope.Payload
	now := s.now().UTC()
	if payload.SchemaVersion != licenseSnapshotSchema ||
		payload.SnapshotVersion <= 0 ||
		strings.TrimSpace(payload.License.PublicID) == "" ||
		strings.TrimSpace(payload.Instance.PublicID) == "" ||
		payload.License.Status != LicenseStatusActive ||
		payload.License.StartsAt.After(now) ||
		(payload.License.ExpiresAt != nil && !payload.License.ExpiresAt.After(now)) ||
		!payload.ExpiresAt.After(now) {
		return licenseSnapshotPayload{}, nil, ErrLicenseInvalid
	}
	if instanceID := strings.TrimSpace(binding.InstanceID); instanceID != "" && payload.Instance.PublicID != instanceID {
		return licenseSnapshotPayload{}, nil, ErrLicenseBinding
	}
	if fingerprint := strings.TrimSpace(binding.Fingerprint); fingerprint != "" && payload.Instance.Fingerprint != fingerprint {
		return licenseSnapshotPayload{}, nil, ErrLicenseBinding
	}
	rawEnvelope, err := json.Marshal(envelope)
	if err != nil {
		return licenseSnapshotPayload{}, nil, err
	}
	return payload, rawEnvelope, nil
}

func (s *Service) localLicenseForPackage(ctx context.Context, record packageRecord) (licenseRecord, string, bool, error) {
	license, ok, err := s.repo.LatestLicense(ctx)
	if err != nil || !ok {
		return licenseRecord{}, "", false, err
	}
	if !licenseRecordActive(license, s.now().UTC()) || !licenseAllowsPackage(license, record, s.now().UTC()) {
		return licenseRecord{}, "", false, nil
	}
	secret := ""
	if strings.TrimSpace(license.ActivationSecretCiphertext) != "" {
		secret, err = decryptSecret(s.secretKey, license.ActivationSecretCiphertext)
		if err != nil {
			return licenseRecord{}, "", false, err
		}
	}
	return license, secret, true, nil
}

func (s *Service) pluginHasLocalEntitlement(ctx context.Context, plugin Plugin) (bool, error) {
	license, ok, err := s.repo.LatestLicense(ctx)
	if err != nil || !ok {
		return false, err
	}
	if !licenseRecordActive(license, s.now().UTC()) {
		return false, nil
	}
	records, err := s.repo.ListPackages(ctx, plugin.ID)
	if err != nil {
		return false, err
	}
	if len(records) == 0 {
		return licenseAllowsPlugin(license, plugin.ID, "", pluginSlugFromID(plugin.ID), s.now().UTC()), nil
	}
	for _, record := range records {
		if licenseAllowsPackage(license, record, s.now().UTC()) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) applyLocalEntitlement(ctx context.Context, plugin Plugin) (Plugin, error) {
	if plugin.Tier != TierPaidAddon || plugin.EntitlementStatus != EntitlementMissing {
		return plugin, nil
	}
	allowed, err := s.pluginHasLocalEntitlement(ctx, plugin)
	if err != nil || !allowed {
		return plugin, err
	}
	plugin.EntitlementStatus = EntitlementIncluded
	if plugin.Status == StatusLocked {
		plugin.Status = StatusDisabled
	}
	return plugin, nil
}

func (s *Service) refreshEntitledPlugins(ctx context.Context) error {
	plugins, err := s.repo.ListPlugins(ctx)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	for _, plugin := range plugins {
		effective, err := s.applyLocalEntitlement(ctx, plugin)
		if err != nil {
			return err
		}
		if effective.EntitlementStatus == plugin.EntitlementStatus && effective.Status == plugin.Status {
			continue
		}
		plugin.EntitlementStatus = effective.EntitlementStatus
		if err := s.repo.SavePlugin(ctx, plugin); err != nil {
			return err
		}
		if err := s.repo.UpdateStatus(ctx, plugin.ID, effective.Status, now); err != nil {
			return err
		}
	}
	return nil
}

func licenseRecordActive(record licenseRecord, now time.Time) bool {
	return record.Status == LicenseStatusActive && record.ExpiresAt.After(now)
}

func licenseAllowsPackage(record licenseRecord, pkg packageRecord, now time.Time) bool {
	return licenseAllowsPlugin(record, pkg.PluginID, pkg.PluginPublicID, pkg.PluginSlug, now) ||
		licenseAllowsResource(record, "download", pkg.PackageID, now)
}

func licenseAllowsPlugin(record licenseRecord, pluginID string, pluginPublicID string, pluginSlug string, now time.Time) bool {
	return licenseAllowsResource(record, "plugin", pluginID, now) ||
		licenseAllowsResource(record, "plugin", pluginPublicID, now) ||
		licenseAllowsResource(record, "plugin", pluginSlug, now)
}

func licenseAllowsResource(record licenseRecord, entitlementType string, resourceKey string, now time.Time) bool {
	resourceKey = strings.TrimSpace(resourceKey)
	if resourceKey == "" {
		return false
	}
	for _, entitlement := range entitlementsFromRecord(record) {
		if entitlement.Type != entitlementType || entitlement.ResourceKey != resourceKey || entitlement.Status != LicenseStatusActive {
			continue
		}
		if entitlement.StartsAt.After(now) || (entitlement.ExpiresAt != nil && !entitlement.ExpiresAt.After(now)) {
			continue
		}
		return true
	}
	return false
}

func entitlementsFromRecord(record licenseRecord) []Entitlement {
	out := make([]Entitlement, 0)
	if err := json.Unmarshal([]byte(defaultString(record.EntitlementsJSON, "[]")), &out); err != nil {
		return []Entitlement{}
	}
	return out
}

func licenseStatusFromRecord(record licenseRecord) LicenseStatus {
	return LicenseStatus{
		Configured:      true,
		LicenseID:       record.LicenseID,
		CustomerID:      record.CustomerID,
		InstanceID:      record.InstanceID,
		SnapshotVersion: record.SnapshotVersion,
		Status:          record.Status,
		Edition:         record.Edition,
		KeyID:           record.KeyID,
		EnvelopeSHA256:  record.EnvelopeSHA256,
		Entitlements:    entitlementsFromRecord(record),
		IssuedAt:        record.IssuedAt,
		ExpiresAt:       record.ExpiresAt,
		ImportedAt:      record.ImportedAt,
		Error:           record.Error,
	}
}

func decodeActivationEnvelope(body []byte) (catalogEnvelope, error) {
	var wrapped struct {
		Data activationResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Data.Envelope.SchemaVersion != "" {
		return wrapped.Data.Envelope, nil
	}
	var direct activationResponse
	if err := json.Unmarshal(body, &direct); err == nil && direct.Envelope.SchemaVersion != "" {
		return direct.Envelope, nil
	}
	var envelope catalogEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return catalogEnvelope{}, err
	}
	if envelope.SchemaVersion == "" {
		return catalogEnvelope{}, fmt.Errorf("license activation envelope is missing")
	}
	return envelope, nil
}

func decodeLicenseImportEnvelope(request LicenseImportRequest) (catalogEnvelope, error) {
	if len(request.Envelope) > 0 && strings.TrimSpace(string(request.Envelope)) != "" && string(request.Envelope) != "null" {
		return decodeLicenseEnvelopeBytes(request.Envelope)
	}
	if len(request.FileJSON) > 0 && strings.TrimSpace(string(request.FileJSON)) != "" && string(request.FileJSON) != "null" {
		return decodeLicenseEnvelopeBytes(request.FileJSON)
	}
	return catalogEnvelope{}, fmt.Errorf("license envelope is required")
}

func decodeLicenseEnvelopeBytes(raw []byte) (catalogEnvelope, error) {
	var envelope catalogEnvelope
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.SchemaVersion != "" {
		return envelope, nil
	}
	var file struct {
		Envelope catalogEnvelope `json:"envelope"`
	}
	if err := json.Unmarshal(raw, &file); err == nil && file.Envelope.SchemaVersion != "" {
		return file.Envelope, nil
	}
	var wrapped struct {
		Data struct {
			Envelope catalogEnvelope `json:"envelope"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return catalogEnvelope{}, err
	}
	if wrapped.Data.Envelope.SchemaVersion == "" {
		return catalogEnvelope{}, fmt.Errorf("license envelope is missing")
	}
	return wrapped.Data.Envelope, nil
}

func normalizeOfficialLicenseConfig(cfg OfficialLicenseConfig, catalog OfficialCatalogConfig) OfficialLicenseConfig {
	cfg.URL = strings.TrimSpace(cfg.URL)
	if cfg.URL == "" {
		cfg.URL = strings.TrimSpace(catalog.LicenseURL)
	}
	if cfg.URL == "" {
		cfg.URL = deriveLicenseURL(catalog.URL)
	}
	cfg.RedeemURL = strings.TrimSpace(cfg.RedeemURL)
	if cfg.RedeemURL == "" {
		cfg.RedeemURL = strings.TrimSpace(catalog.RedeemURL)
	}
	if cfg.RedeemURL == "" {
		cfg.RedeemURL = deriveRedeemURL(cfg.URL)
	}
	cfg.PublicKeyID = strings.TrimSpace(cfg.PublicKeyID)
	if cfg.PublicKeyID == "" {
		cfg.PublicKeyID = strings.TrimSpace(catalog.PublicKeyID)
	}
	cfg.PublicKeyBase64 = strings.TrimSpace(cfg.PublicKeyBase64)
	if cfg.PublicKeyBase64 == "" {
		cfg.PublicKeyBase64 = strings.TrimSpace(catalog.PublicKeyBase64)
	}
	cfg.InstanceID = strings.TrimSpace(cfg.InstanceID)
	cfg.Fingerprint = strings.TrimSpace(cfg.Fingerprint)
	if cfg.Fingerprint == "" {
		cfg.Fingerprint = defaultInstanceFingerprint()
	}
	cfg.DisplayName = strings.TrimSpace(cfg.DisplayName)
	if cfg.DisplayName == "" {
		cfg.DisplayName = defaultInstanceDisplayName()
	}
	return cfg
}

func (s *Service) licenseTrustConfigured(ctx context.Context) bool {
	cfg, err := s.effectiveLicenseConfig(ctx)
	if err != nil {
		cfg = normalizeOfficialLicenseConfig(s.licenseConfig, s.catalogConfig)
	}
	return cfg.PublicKeyID != "" && cfg.PublicKeyBase64 != ""
}

func licenseActivationURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("license URL must be http or https")
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(basePath, "/licenses/activate") {
		parsed.Path = basePath
	} else {
		parsed.Path = strings.TrimRight(basePath, "/") + "/licenses/activate"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func licenseRedeemURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("redeem URL must be http or https")
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(basePath, "/licenses/redeem"):
		parsed.Path = basePath
	case strings.HasSuffix(basePath, "/licenses/activate"):
		parsed.Path = strings.TrimSuffix(basePath, "/activate") + "/redeem"
	default:
		parsed.Path = strings.TrimRight(basePath, "/") + "/licenses/redeem"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func deriveLicenseURL(catalogURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(catalogURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(basePath, "/catalog/index") {
		basePath = strings.TrimSuffix(basePath, "/catalog/index")
	}
	parsed.Path = strings.TrimRight(basePath, "/") + "/licenses/activate"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func deriveRedeemURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(basePath, "/catalog/index") {
		basePath = strings.TrimSuffix(basePath, "/catalog/index")
	}
	if strings.HasSuffix(basePath, "/licenses/activate") {
		basePath = strings.TrimSuffix(basePath, "/activate")
	}
	parsed.Path = strings.TrimRight(basePath, "/") + "/licenses/redeem"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func defaultInstanceFingerprint() string {
	host, _ := os.Hostname()
	sum := sha256.Sum256([]byte(host + "|" + runtime.GOOS + "|" + runtime.GOARCH))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func defaultInstanceDisplayName() string {
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	if host == "" {
		return "AsterRouter"
	}
	return host
}

func pluginSlugFromID(pluginID string) string {
	return strings.TrimPrefix(strings.TrimSpace(pluginID), "com.astercloud.catalog.")
}
