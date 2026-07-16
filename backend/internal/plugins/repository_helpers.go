package plugins

import (
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
)

type pluginScanner interface {
	Scan(dest ...any) error
}

type configScanner interface {
	Scan(dest ...any) error
}

type deliveryAttemptScanner interface {
	Scan(dest ...any) error
}

type catalogSnapshotScanner interface {
	Scan(dest ...any) error
}

type packageRecordScanner interface {
	Scan(dest ...any) error
}

type affectedVersionScanner interface {
	Scan(dest ...any) error
}

type packageCacheRecordScanner interface {
	Scan(dest ...any) error
}

type packageInstallationRecordScanner interface {
	Scan(dest ...any) error
}

type licenseRecordScanner interface {
	Scan(dest ...any) error
}

type pluginAPITokenScanner interface {
	Scan(dest ...any) error
}

type officialFeedScanner interface {
	Scan(dest ...any) error
}

type officialFeedSyncRunScanner interface {
	Scan(dest ...any) error
}

func scanPlugin(scanner pluginScanner) (Plugin, error) {
	var plugin Plugin
	var surfaces string
	if err := scanner.Scan(&plugin.ID, &plugin.PluginID, &plugin.Name, &plugin.Description, &plugin.Category, &plugin.Type, &plugin.Tier, &plugin.Version, &plugin.Vendor, &plugin.Status, &plugin.EntitlementStatus, &surfaces, &plugin.EntryPoint, &plugin.Configurable, &plugin.CreatedAt, &plugin.UpdatedAt); err != nil {
		return Plugin{}, err
	}
	plugin.Surfaces = parseStringList(surfaces)
	return plugin, nil
}

func scanConfig(scanner configScanner) (configRecord, error) {
	var record configRecord
	var settings string
	var secretCiphertexts string
	var secretHints string
	if err := scanner.Scan(&record.PluginID, &settings, &secretCiphertexts, &secretHints, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return configRecord{}, err
	}
	record.Settings = parseStringMap(settings)
	record.SecretCiphertexts = parseStringMap(secretCiphertexts)
	record.SecretHints = parseStringMap(secretHints)
	return record, nil
}

func scanDeliveryAttempt(scanner deliveryAttemptScanner) (DeliveryAttempt, error) {
	var attempt DeliveryAttempt
	if err := scanner.Scan(&attempt.ID, &attempt.PluginID, &attempt.AlertID, &attempt.AlertType, &attempt.AlertSeverity, &attempt.Status, &attempt.Target, &attempt.HTTPStatus, &attempt.Error, &attempt.CreatedAt); err != nil {
		return DeliveryAttempt{}, err
	}
	return attempt, nil
}

func scanCatalogSnapshot(scanner catalogSnapshotScanner) (catalogSnapshotRecord, error) {
	var record catalogSnapshotRecord
	if err := scanner.Scan(&record.ID, &record.Mode, &record.SourceURL, &record.CatalogVersion, &record.PayloadSHA256, &record.KeyID, &record.Signature, &record.PluginCount, &record.AdvisoryCount, &record.Status, &record.Error, &record.PayloadJSON, &record.SyncedAt); err != nil {
		return catalogSnapshotRecord{}, err
	}
	return record, nil
}

func scanPackageRecord(scanner packageRecordScanner) (packageRecord, error) {
	var record packageRecord
	if err := scanner.Scan(&record.PackageID, &record.PluginID, &record.PluginSlug, &record.PluginPublicID, &record.VersionPublicID, &record.Version, &record.Channel, &record.RequiredEntitlement, &record.MinCoreVersion, &record.MaxCoreVersion, &record.PackageURI, &record.OS, &record.Arch, &record.SHA256, &record.SizeBytes, &record.SignatureJSON, &record.Revoked, &record.CompatibilityJSON, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return packageRecord{}, err
	}
	return record, nil
}

func scanAffectedVersionRecord(scanner affectedVersionScanner) (affectedVersionRecord, error) {
	var record affectedVersionRecord
	if err := scanner.Scan(&record.PublicID, &record.AdvisoryPublicID, &record.AdvisoryID, &record.AdvisorySeverity, &record.AdvisoryTitle, &record.PluginID, &record.PluginSlug, &record.VersionRange, &record.FixedVersion, &record.Revoked, &record.CreatedAt); err != nil {
		return affectedVersionRecord{}, err
	}
	return record, nil
}

func scanPackageCacheRecord(scanner packageCacheRecordScanner) (packageCacheRecord, error) {
	var record packageCacheRecord
	if err := scanner.Scan(&record.PackageID, &record.PluginID, &record.Version, &record.OS, &record.Arch, &record.SHA256, &record.SizeBytes, &record.CachePath, &record.Status, &record.Error, &record.CachedAt, &record.UpdatedAt); err != nil {
		return packageCacheRecord{}, err
	}
	return record, nil
}

func scanPackageInstallationRecord(scanner packageInstallationRecordScanner) (packageInstallationRecord, error) {
	var record packageInstallationRecord
	if err := scanner.Scan(&record.PluginID, &record.PackageID, &record.Version, &record.OS, &record.Arch, &record.CachePath, &record.Status, &record.InstalledAt, &record.UpdatedAt); err != nil {
		return packageInstallationRecord{}, err
	}
	return record, nil
}

func scanLicenseRecord(scanner licenseRecordScanner) (licenseRecord, error) {
	var record licenseRecord
	if err := scanner.Scan(&record.LicenseID, &record.CustomerID, &record.InstanceID, &record.SnapshotVersion, &record.Status, &record.Edition, &record.KeyID, &record.EnvelopeSHA256, &record.EnvelopeJSON, &record.ActivationSecretCiphertext, &record.ActivationSecretHint, &record.EntitlementsJSON, &record.IssuedAt, &record.ExpiresAt, &record.ImportedAt, &record.UpdatedAt, &record.Error); err != nil {
		return licenseRecord{}, err
	}
	return record, nil
}

func scanPluginAPITokenRecord(scanner pluginAPITokenScanner) (pluginAPITokenRecord, error) {
	var record pluginAPITokenRecord
	var scopes string
	var surfaces string
	var expiresAt sql.NullTime
	var lastUsedAt sql.NullTime
	if err := scanner.Scan(&record.ID, &record.Name, &record.PluginID, &record.TokenPrefix, &record.TokenHash, &scopes, &surfaces, &record.Status, &expiresAt, &lastUsedAt, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return pluginAPITokenRecord{}, err
	}
	record.Scopes = parseStringList(scopes)
	record.Surfaces = parseStringList(surfaces)
	if expiresAt.Valid {
		record.ExpiresAt = &expiresAt.Time
	}
	if lastUsedAt.Valid {
		record.LastUsedAt = &lastUsedAt.Time
	}
	return record, nil
}

func scanOfficialFeedRecord(scanner officialFeedScanner) (officialFeedRecord, error) {
	var record officialFeedRecord
	if err := scanner.Scan(
		&record.ServiceKey,
		&record.FeedID,
		&record.FeedVersion,
		&record.DataSchemaVersion,
		&record.Status,
		&record.SignatureVerified,
		&record.PayloadSHA256,
		&record.SizeBytes,
		&record.PayloadCiphertext,
		&record.EnvelopeJSON,
		&record.IssuedAt,
		&record.ExpiresAt,
		&record.ImportedAt,
		&record.UpdatedAt,
	); err != nil {
		return officialFeedRecord{}, err
	}
	return record, nil
}

func scanOfficialFeedSyncRunRecord(scanner officialFeedSyncRunScanner) (officialFeedSyncRunRecord, error) {
	var record officialFeedSyncRunRecord
	if err := scanner.Scan(
		&record.ID,
		&record.ServiceKey,
		&record.FeedID,
		&record.Mode,
		&record.Status,
		&record.RequestID,
		&record.SourceURL,
		&record.ErrorCode,
		&record.Error,
		&record.StartedAt,
		&record.FinishedAt,
	); err != nil {
		return officialFeedSyncRunRecord{}, err
	}
	return record, nil
}

func sortPlugins(plugins []Plugin) {
	sort.Slice(plugins, func(i, j int) bool {
		if plugins[i].Category == plugins[j].Category {
			if plugins[i].Tier == plugins[j].Tier {
				return plugins[i].Name < plugins[j].Name
			}
			return tierRank(plugins[i].Tier) < tierRank(plugins[j].Tier)
		}
		return plugins[i].Category < plugins[j].Category
	})
}

func sortPackageRecords(packages []packageRecord) {
	sort.Slice(packages, func(i, j int) bool {
		if packages[i].Version == packages[j].Version {
			if packages[i].OS == packages[j].OS {
				return packages[i].Arch < packages[j].Arch
			}
			return packages[i].OS < packages[j].OS
		}
		return packages[i].Version > packages[j].Version
	})
}

func tierRank(tier string) int {
	switch tier {
	case TierCore:
		return 0
	case TierFreeCore:
		return 1
	case TierProfileBundle:
		return 2
	case TierPaidAddon:
		return 3
	default:
		return 9
	}
}

func marshalStringList(values []string) string {
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func parseStringList(value string) []string {
	var out []string
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return []string{}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func marshalStringMap(values map[string]string) string {
	if values == nil {
		return "{}"
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func parseStringMap(value string) map[string]string {
	var out map[string]string
	if err := json.Unmarshal([]byte(value), &out); err != nil || out == nil {
		return map[string]string{}
	}
	return out
}

func cloneConfigRecord(record configRecord) configRecord {
	record.Settings = cloneStringMap(record.Settings)
	record.SecretCiphertexts = cloneStringMap(record.SecretCiphertexts)
	record.SecretHints = cloneStringMap(record.SecretHints)
	return record
}

func cloneAdvisoryRecord(record advisoryRecord) advisoryRecord {
	if record.Affected == nil {
		record.Affected = []affectedVersionRecord{}
		return record
	}
	record.Affected = append([]affectedVersionRecord(nil), record.Affected...)
	return record
}

func cloneStringMap(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func deliveryAttemptMatches(attempt DeliveryAttempt, query DeliveryQuery) bool {
	if query.PluginID != "" && attempt.PluginID != query.PluginID {
		return false
	}
	if query.AlertID != "" && attempt.AlertID != query.AlertID {
		return false
	}
	if query.Status != "" && attempt.Status != query.Status {
		return false
	}
	return true
}

func affectedVersionMatchesPlugin(item affectedVersionRecord, pluginID string) bool {
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return true
	}
	return item.PluginID == pluginID || item.PluginSlug == pluginID
}

func normalizeListWindow(limit int, offset int, fallback int, max int) (int, int) {
	if limit <= 0 {
		limit = fallback
	}
	if limit > max {
		limit = max
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
