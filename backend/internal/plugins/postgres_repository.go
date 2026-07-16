package plugins

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(ctx context.Context, databaseURL string) (*PostgresRepository, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	repo := &PostgresRepository{db: db}
	if err := repo.Health(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := repo.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repo, nil
}

func (r *PostgresRepository) migrate(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS plugins (
  id TEXT PRIMARY KEY,
  plugin_id TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL,
  type TEXT NOT NULL,
  tier TEXT NOT NULL,
  version TEXT NOT NULL,
  vendor TEXT NOT NULL,
  status TEXT NOT NULL,
  entitlement_status TEXT NOT NULL,
  surfaces TEXT NOT NULL DEFAULT '[]',
  entry_point TEXT NOT NULL DEFAULT '',
  configurable BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS plugin_configs (
  plugin_id TEXT PRIMARY KEY,
  settings_json TEXT NOT NULL DEFAULT '{}',
  secret_ciphertexts_json TEXT NOT NULL DEFAULT '{}',
  secret_hints_json TEXT NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS notification_deliveries (
  id TEXT PRIMARY KEY,
  plugin_id TEXT NOT NULL,
  alert_id TEXT NOT NULL,
  alert_type TEXT NOT NULL,
  alert_severity TEXT NOT NULL,
  status TEXT NOT NULL,
  target TEXT NOT NULL DEFAULT '',
  http_status INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS notification_deliveries_plugin_created_idx
  ON notification_deliveries(plugin_id, created_at DESC);

CREATE INDEX IF NOT EXISTS notification_deliveries_alert_idx
  ON notification_deliveries(alert_id, created_at DESC);

CREATE TABLE IF NOT EXISTS official_catalog_snapshots (
  id TEXT PRIMARY KEY,
  mode TEXT NOT NULL,
  source_url TEXT NOT NULL DEFAULT '',
  catalog_version BIGINT NOT NULL DEFAULT 0,
  payload_sha256 TEXT NOT NULL DEFAULT '',
  key_id TEXT NOT NULL DEFAULT '',
  signature TEXT NOT NULL DEFAULT '',
  plugin_count INTEGER NOT NULL DEFAULT 0,
  advisory_count INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL DEFAULT '{}',
  synced_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE official_catalog_snapshots
  ADD COLUMN IF NOT EXISTS advisory_count INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS official_catalog_snapshots_synced_idx
  ON official_catalog_snapshots(synced_at DESC);

CREATE TABLE IF NOT EXISTS official_plugin_packages (
  package_id TEXT PRIMARY KEY,
  plugin_id TEXT NOT NULL,
  plugin_slug TEXT NOT NULL,
  plugin_public_id TEXT NOT NULL DEFAULT '',
  version_public_id TEXT NOT NULL DEFAULT '',
  version TEXT NOT NULL,
  channel TEXT NOT NULL DEFAULT '',
  required_entitlement BOOLEAN NOT NULL DEFAULT false,
  min_core_version TEXT NOT NULL DEFAULT '',
  max_core_version TEXT NOT NULL DEFAULT '',
  package_uri TEXT NOT NULL DEFAULT '',
  os TEXT NOT NULL,
  arch TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  size_bytes BIGINT NOT NULL DEFAULT 0,
  signature_json TEXT NOT NULL DEFAULT '{}',
  revoked BOOLEAN NOT NULL DEFAULT false,
  compatibility_json TEXT NOT NULL DEFAULT '[]',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS official_plugin_packages_plugin_idx
  ON official_plugin_packages(plugin_id, version DESC);

ALTER TABLE official_plugin_packages
  ADD COLUMN IF NOT EXISTS package_uri TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS official_security_advisories (
  public_id TEXT PRIMARY KEY,
  advisory_id TEXT NOT NULL,
  severity TEXT NOT NULL,
  title TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  published_at TIMESTAMPTZ NOT NULL,
  signature_json TEXT NOT NULL DEFAULT '{}',
  synced_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS official_security_advisories_published_idx
  ON official_security_advisories(published_at DESC);

CREATE TABLE IF NOT EXISTS official_security_advisory_affected_versions (
  public_id TEXT PRIMARY KEY,
  advisory_public_id TEXT NOT NULL REFERENCES official_security_advisories(public_id) ON DELETE CASCADE,
  advisory_id TEXT NOT NULL,
  advisory_severity TEXT NOT NULL,
  advisory_title TEXT NOT NULL,
  plugin_id TEXT NOT NULL DEFAULT '',
  plugin_slug TEXT NOT NULL DEFAULT '',
  version_range TEXT NOT NULL,
  fixed_version TEXT NOT NULL DEFAULT '',
  revoked BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS official_security_advisory_affected_plugin_idx
  ON official_security_advisory_affected_versions(plugin_slug, revoked);

CREATE INDEX IF NOT EXISTS official_security_advisory_affected_plugin_id_idx
  ON official_security_advisory_affected_versions(plugin_id, revoked);

CREATE TABLE IF NOT EXISTS official_plugin_package_caches (
  package_id TEXT PRIMARY KEY,
  plugin_id TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  arch TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  size_bytes BIGINT NOT NULL DEFAULT 0,
  cache_path TEXT NOT NULL,
  status TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  cached_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS official_plugin_package_caches_plugin_idx
  ON official_plugin_package_caches(plugin_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS official_plugin_installations (
  plugin_id TEXT PRIMARY KEY,
  package_id TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  arch TEXT NOT NULL,
  cache_path TEXT NOT NULL,
  status TEXT NOT NULL,
  installed_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS official_plugin_installations_status_idx
  ON official_plugin_installations(status, updated_at DESC);

CREATE TABLE IF NOT EXISTS official_license_snapshots (
  license_id TEXT NOT NULL,
  customer_id TEXT NOT NULL DEFAULT '',
  instance_id TEXT NOT NULL DEFAULT '',
  snapshot_version BIGINT NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  edition TEXT NOT NULL DEFAULT '',
  key_id TEXT NOT NULL DEFAULT '',
  envelope_sha256 TEXT PRIMARY KEY,
  envelope_json TEXT NOT NULL,
  activation_secret_ciphertext TEXT NOT NULL DEFAULT '',
  activation_secret_hint TEXT NOT NULL DEFAULT '',
  entitlements_json TEXT NOT NULL DEFAULT '[]',
  issued_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  imported_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  error TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS official_license_snapshots_imported_idx
  ON official_license_snapshots(imported_at DESC);

CREATE TABLE IF NOT EXISTS plugin_api_tokens (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  plugin_id TEXT NOT NULL DEFAULT '',
  token_prefix TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  scopes_json TEXT NOT NULL DEFAULT '[]',
  surfaces_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL,
  expires_at TIMESTAMPTZ,
  last_used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS plugin_api_tokens_plugin_idx
  ON plugin_api_tokens(plugin_id, created_at DESC);

CREATE INDEX IF NOT EXISTS plugin_api_tokens_status_idx
  ON plugin_api_tokens(status, created_at DESC);

CREATE TABLE IF NOT EXISTS official_feed_snapshots (
  service_key TEXT NOT NULL,
  feed_id TEXT NOT NULL,
  feed_version TEXT NOT NULL,
  data_schema_version TEXT NOT NULL,
  status TEXT NOT NULL,
  signature_verified BOOLEAN NOT NULL DEFAULT false,
  payload_sha256 TEXT NOT NULL,
  size_bytes BIGINT NOT NULL,
  payload_ciphertext TEXT NOT NULL,
  envelope_json TEXT NOT NULL,
  issued_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  imported_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY(service_key, feed_id)
);

CREATE INDEX IF NOT EXISTS official_feed_snapshots_service_idx
  ON official_feed_snapshots(service_key, imported_at DESC);

CREATE TABLE IF NOT EXISTS official_feed_sync_runs (
  id TEXT PRIMARY KEY,
  service_key TEXT NOT NULL,
  feed_id TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL,
  status TEXT NOT NULL,
  request_id TEXT NOT NULL DEFAULT '',
  source_url TEXT NOT NULL DEFAULT '',
  error_code TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ NOT NULL,
  finished_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS official_feed_sync_runs_service_idx
  ON official_feed_sync_runs(service_key, started_at DESC);
`)
	return err
}

func (r *PostgresRepository) ListPlugins(ctx context.Context) ([]Plugin, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, plugin_id, name, description, category, type, tier, version, vendor, status, entitlement_status, surfaces, entry_point, configurable, created_at, updated_at
FROM plugins
ORDER BY category ASC, tier ASC, name ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Plugin, 0)
	for rows.Next() {
		plugin, err := scanPlugin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, plugin)
	}
	sortPlugins(out)
	return out, rows.Err()
}

func (r *PostgresRepository) SavePlugin(ctx context.Context, plugin Plugin) error {
	surfaces := marshalStringList(plugin.Surfaces)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO plugins(id, plugin_id, name, description, category, type, tier, version, vendor, status, entitlement_status, surfaces, entry_point, configurable, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
ON CONFLICT(id) DO UPDATE SET
  plugin_id = EXCLUDED.plugin_id,
  name = EXCLUDED.name,
  description = EXCLUDED.description,
  category = EXCLUDED.category,
  type = EXCLUDED.type,
  tier = EXCLUDED.tier,
  version = EXCLUDED.version,
  vendor = EXCLUDED.vendor,
  entitlement_status = EXCLUDED.entitlement_status,
  surfaces = EXCLUDED.surfaces,
  entry_point = EXCLUDED.entry_point,
  configurable = EXCLUDED.configurable,
  updated_at = EXCLUDED.updated_at
`, plugin.ID, plugin.PluginID, plugin.Name, plugin.Description, plugin.Category, plugin.Type, plugin.Tier, plugin.Version, plugin.Vendor, plugin.Status, plugin.EntitlementStatus, surfaces, plugin.EntryPoint, plugin.Configurable, plugin.CreatedAt, plugin.UpdatedAt)
	return err
}

func (r *PostgresRepository) FindPlugin(ctx context.Context, id string) (Plugin, bool, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, plugin_id, name, description, category, type, tier, version, vendor, status, entitlement_status, surfaces, entry_point, configurable, created_at, updated_at
FROM plugins
WHERE id = $1 OR plugin_id = $1
`, id)
	plugin, err := scanPlugin(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return Plugin{}, false, nil
		}
		return Plugin{}, false, err
	}
	return plugin, true, nil
}

func (r *PostgresRepository) UpdateStatus(ctx context.Context, id string, status string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE plugins SET status = $1, updated_at = $2 WHERE id = $3 OR plugin_id = $3`, status, updatedAt, id)
	return err
}

func (r *PostgresRepository) FindConfig(ctx context.Context, pluginID string) (configRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT plugin_id, settings_json, secret_ciphertexts_json, secret_hints_json, created_at, updated_at
FROM plugin_configs
WHERE plugin_id = $1
`, pluginID)
	record, err := scanConfig(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return configRecord{}, false, nil
		}
		return configRecord{}, false, err
	}
	return record, true, nil
}

func (r *PostgresRepository) SaveConfig(ctx context.Context, record configRecord) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO plugin_configs(plugin_id, settings_json, secret_ciphertexts_json, secret_hints_json, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT(plugin_id) DO UPDATE SET
  settings_json = EXCLUDED.settings_json,
  secret_ciphertexts_json = EXCLUDED.secret_ciphertexts_json,
  secret_hints_json = EXCLUDED.secret_hints_json,
  updated_at = EXCLUDED.updated_at
`, record.PluginID, marshalStringMap(record.Settings), marshalStringMap(record.SecretCiphertexts), marshalStringMap(record.SecretHints), record.CreatedAt, record.UpdatedAt)
	return err
}

func (r *PostgresRepository) QueryDeliveryAttempts(ctx context.Context, query DeliveryQuery) ([]DeliveryAttempt, error) {
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 50, 500)
	clauses := []string{}
	args := []any{}
	appendDeliveryFilter(&clauses, &args, "plugin_id", query.PluginID)
	appendDeliveryFilter(&clauses, &args, "alert_id", query.AlertID)
	appendDeliveryFilter(&clauses, &args, "status", query.Status)
	sqlText := `
SELECT id, plugin_id, alert_id, alert_type, alert_severity, status, target, http_status, error, created_at
FROM notification_deliveries`
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit, offset)
	sqlText += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DeliveryAttempt, 0)
	for rows.Next() {
		attempt, err := scanDeliveryAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, attempt)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveDeliveryAttempt(ctx context.Context, attempt DeliveryAttempt) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO notification_deliveries(id, plugin_id, alert_id, alert_type, alert_severity, status, target, http_status, error, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
`, attempt.ID, attempt.PluginID, attempt.AlertID, attempt.AlertType, attempt.AlertSeverity, attempt.Status, attempt.Target, attempt.HTTPStatus, attempt.Error, attempt.CreatedAt)
	return err
}

func (r *PostgresRepository) SaveCatalogSnapshot(ctx context.Context, record catalogSnapshotRecord) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO official_catalog_snapshots(id, mode, source_url, catalog_version, payload_sha256, key_id, signature, plugin_count, advisory_count, status, error, payload_json, synced_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
`, record.ID, record.Mode, record.SourceURL, record.CatalogVersion, record.PayloadSHA256, record.KeyID, record.Signature, record.PluginCount, record.AdvisoryCount, record.Status, record.Error, record.PayloadJSON, record.SyncedAt)
	return err
}

func (r *PostgresRepository) LatestCatalogSnapshot(ctx context.Context) (catalogSnapshotRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, mode, source_url, catalog_version, payload_sha256, key_id, signature, plugin_count, advisory_count, status, error, payload_json, synced_at
FROM official_catalog_snapshots
ORDER BY synced_at DESC
LIMIT 1
`)
	record, err := scanCatalogSnapshot(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return catalogSnapshotRecord{}, false, nil
		}
		return catalogSnapshotRecord{}, false, err
	}
	return record, true, nil
}

func (r *PostgresRepository) SavePackage(ctx context.Context, record packageRecord) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO official_plugin_packages(
  package_id, plugin_id, plugin_slug, plugin_public_id, version_public_id, version, channel,
  required_entitlement, min_core_version, max_core_version, package_uri, os, arch, sha256, size_bytes,
  signature_json, revoked, compatibility_json, created_at, updated_at
)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
ON CONFLICT(package_id) DO UPDATE SET
  plugin_id = EXCLUDED.plugin_id,
  plugin_slug = EXCLUDED.plugin_slug,
  plugin_public_id = EXCLUDED.plugin_public_id,
  version_public_id = EXCLUDED.version_public_id,
  version = EXCLUDED.version,
  channel = EXCLUDED.channel,
  required_entitlement = EXCLUDED.required_entitlement,
  min_core_version = EXCLUDED.min_core_version,
  max_core_version = EXCLUDED.max_core_version,
  package_uri = EXCLUDED.package_uri,
  os = EXCLUDED.os,
  arch = EXCLUDED.arch,
  sha256 = EXCLUDED.sha256,
  size_bytes = EXCLUDED.size_bytes,
  signature_json = EXCLUDED.signature_json,
  revoked = EXCLUDED.revoked,
  compatibility_json = EXCLUDED.compatibility_json,
  updated_at = EXCLUDED.updated_at
`, record.PackageID, record.PluginID, record.PluginSlug, record.PluginPublicID, record.VersionPublicID, record.Version, record.Channel, record.RequiredEntitlement, record.MinCoreVersion, record.MaxCoreVersion, record.PackageURI, record.OS, record.Arch, record.SHA256, record.SizeBytes, record.SignatureJSON, record.Revoked, record.CompatibilityJSON, record.CreatedAt, record.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListPackages(ctx context.Context, pluginID string) ([]packageRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT package_id, plugin_id, plugin_slug, plugin_public_id, version_public_id, version, channel,
       required_entitlement, min_core_version, max_core_version, package_uri, os, arch, sha256, size_bytes,
       signature_json, revoked, compatibility_json, created_at, updated_at
FROM official_plugin_packages
WHERE plugin_id = $1
ORDER BY version DESC, os ASC, arch ASC
`, strings.TrimSpace(pluginID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]packageRecord, 0)
	for rows.Next() {
		record, err := scanPackageRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) FindPackage(ctx context.Context, packageID string) (packageRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT package_id, plugin_id, plugin_slug, plugin_public_id, version_public_id, version, channel,
       required_entitlement, min_core_version, max_core_version, package_uri, os, arch, sha256, size_bytes,
       signature_json, revoked, compatibility_json, created_at, updated_at
FROM official_plugin_packages
WHERE package_id = $1
`, strings.TrimSpace(packageID))
	record, err := scanPackageRecord(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return packageRecord{}, false, nil
		}
		return packageRecord{}, false, err
	}
	return record, true, nil
}

func (r *PostgresRepository) SaveAdvisory(ctx context.Context, record advisoryRecord) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO official_security_advisories(public_id, advisory_id, severity, title, summary, published_at, signature_json, synced_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(public_id) DO UPDATE SET
  advisory_id = EXCLUDED.advisory_id,
  severity = EXCLUDED.severity,
  title = EXCLUDED.title,
  summary = EXCLUDED.summary,
  published_at = EXCLUDED.published_at,
  signature_json = EXCLUDED.signature_json,
  synced_at = EXCLUDED.synced_at
`, record.PublicID, record.AdvisoryID, record.Severity, record.Title, record.Summary, record.PublishedAt, record.SignatureJSON, record.SyncedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM official_security_advisory_affected_versions WHERE advisory_public_id = $1`, record.PublicID); err != nil {
		return err
	}
	for _, item := range record.Affected {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO official_security_advisory_affected_versions(
  public_id, advisory_public_id, advisory_id, advisory_severity, advisory_title,
  plugin_id, plugin_slug, version_range, fixed_version, revoked, created_at
)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT(public_id) DO UPDATE SET
  advisory_public_id = EXCLUDED.advisory_public_id,
  advisory_id = EXCLUDED.advisory_id,
  advisory_severity = EXCLUDED.advisory_severity,
  advisory_title = EXCLUDED.advisory_title,
  plugin_id = EXCLUDED.plugin_id,
  plugin_slug = EXCLUDED.plugin_slug,
  version_range = EXCLUDED.version_range,
  fixed_version = EXCLUDED.fixed_version,
  revoked = EXCLUDED.revoked,
  created_at = EXCLUDED.created_at
`, item.PublicID, item.AdvisoryPublicID, item.AdvisoryID, item.AdvisorySeverity, item.AdvisoryTitle, item.PluginID, item.PluginSlug, item.VersionRange, item.FixedVersion, item.Revoked, item.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *PostgresRepository) ListRevokedAffectedVersions(ctx context.Context, pluginID string) ([]affectedVersionRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT public_id, advisory_public_id, advisory_id, advisory_severity, advisory_title,
       plugin_id, plugin_slug, version_range, fixed_version, revoked, created_at
FROM official_security_advisory_affected_versions
WHERE revoked = true
  AND ($1 = '' OR plugin_id = $1 OR plugin_slug = $1)
ORDER BY created_at ASC
`, strings.TrimSpace(pluginID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]affectedVersionRecord, 0)
	for rows.Next() {
		record, err := scanAffectedVersionRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SavePackageCache(ctx context.Context, record packageCacheRecord) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO official_plugin_package_caches(package_id, plugin_id, version, os, arch, sha256, size_bytes, cache_path, status, error, cached_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT(package_id) DO UPDATE SET
  plugin_id = EXCLUDED.plugin_id,
  version = EXCLUDED.version,
  os = EXCLUDED.os,
  arch = EXCLUDED.arch,
  sha256 = EXCLUDED.sha256,
  size_bytes = EXCLUDED.size_bytes,
  cache_path = EXCLUDED.cache_path,
  status = EXCLUDED.status,
  error = EXCLUDED.error,
  cached_at = EXCLUDED.cached_at,
  updated_at = EXCLUDED.updated_at
`, record.PackageID, record.PluginID, record.Version, record.OS, record.Arch, record.SHA256, record.SizeBytes, record.CachePath, record.Status, record.Error, record.CachedAt, record.UpdatedAt)
	return err
}

func (r *PostgresRepository) FindPackageCache(ctx context.Context, packageID string) (packageCacheRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT package_id, plugin_id, version, os, arch, sha256, size_bytes, cache_path, status, error, cached_at, updated_at
FROM official_plugin_package_caches
WHERE package_id = $1
`, strings.TrimSpace(packageID))
	record, err := scanPackageCacheRecord(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return packageCacheRecord{}, false, nil
		}
		return packageCacheRecord{}, false, err
	}
	return record, true, nil
}

func (r *PostgresRepository) SavePackageInstallation(ctx context.Context, record packageInstallationRecord) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO official_plugin_installations(plugin_id, package_id, version, os, arch, cache_path, status, installed_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(plugin_id) DO UPDATE SET
  package_id = EXCLUDED.package_id,
  version = EXCLUDED.version,
  os = EXCLUDED.os,
  arch = EXCLUDED.arch,
  cache_path = EXCLUDED.cache_path,
  status = EXCLUDED.status,
  installed_at = EXCLUDED.installed_at,
  updated_at = EXCLUDED.updated_at
`, record.PluginID, record.PackageID, record.Version, record.OS, record.Arch, record.CachePath, record.Status, record.InstalledAt, record.UpdatedAt)
	return err
}

func (r *PostgresRepository) FindPackageInstallation(ctx context.Context, pluginID string) (packageInstallationRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT plugin_id, package_id, version, os, arch, cache_path, status, installed_at, updated_at
FROM official_plugin_installations
WHERE plugin_id = $1
`, strings.TrimSpace(pluginID))
	record, err := scanPackageInstallationRecord(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return packageInstallationRecord{}, false, nil
		}
		return packageInstallationRecord{}, false, err
	}
	return record, true, nil
}

func (r *PostgresRepository) SaveLicense(ctx context.Context, record licenseRecord) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO official_license_snapshots(
  license_id, customer_id, instance_id, snapshot_version, status, edition, key_id,
  envelope_sha256, envelope_json, activation_secret_ciphertext, activation_secret_hint,
  entitlements_json, issued_at, expires_at, imported_at, updated_at, error
)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
ON CONFLICT(envelope_sha256) DO UPDATE SET
  activation_secret_ciphertext = EXCLUDED.activation_secret_ciphertext,
  activation_secret_hint = EXCLUDED.activation_secret_hint,
  imported_at = EXCLUDED.imported_at,
  updated_at = EXCLUDED.updated_at,
  error = EXCLUDED.error
`, record.LicenseID, record.CustomerID, record.InstanceID, record.SnapshotVersion, record.Status, record.Edition, record.KeyID, record.EnvelopeSHA256, record.EnvelopeJSON, record.ActivationSecretCiphertext, record.ActivationSecretHint, record.EntitlementsJSON, record.IssuedAt, record.ExpiresAt, record.ImportedAt, record.UpdatedAt, record.Error)
	return err
}

func (r *PostgresRepository) LatestLicense(ctx context.Context) (licenseRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT license_id, customer_id, instance_id, snapshot_version, status, edition, key_id,
       envelope_sha256, envelope_json, activation_secret_ciphertext, activation_secret_hint,
       entitlements_json, issued_at, expires_at, imported_at, updated_at, error
FROM official_license_snapshots
ORDER BY imported_at DESC
LIMIT 1
`)
	record, err := scanLicenseRecord(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return licenseRecord{}, false, nil
		}
		return licenseRecord{}, false, err
	}
	return record, true, nil
}

func (r *PostgresRepository) SavePluginAPIToken(ctx context.Context, record pluginAPITokenRecord) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO plugin_api_tokens(
  id, name, plugin_id, token_prefix, token_hash, scopes_json, surfaces_json,
  status, expires_at, last_used_at, created_at, updated_at
)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
`, record.ID, record.Name, record.PluginID, record.TokenPrefix, record.TokenHash, marshalStringList(record.Scopes), marshalStringList(record.Surfaces), record.Status, record.ExpiresAt, record.LastUsedAt, record.CreatedAt, record.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListPluginAPITokens(ctx context.Context, pluginID string) ([]pluginAPITokenRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, name, plugin_id, token_prefix, token_hash, scopes_json, surfaces_json,
       status, expires_at, last_used_at, created_at, updated_at
FROM plugin_api_tokens
WHERE ($1 = '' OR plugin_id = $1)
ORDER BY created_at DESC
`, strings.TrimSpace(pluginID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []pluginAPITokenRecord{}
	for rows.Next() {
		record, err := scanPluginAPITokenRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) FindPluginAPIToken(ctx context.Context, tokenHash string) (pluginAPITokenRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, name, plugin_id, token_prefix, token_hash, scopes_json, surfaces_json,
       status, expires_at, last_used_at, created_at, updated_at
FROM plugin_api_tokens
WHERE token_hash = $1
`, strings.TrimSpace(tokenHash))
	record, err := scanPluginAPITokenRecord(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return pluginAPITokenRecord{}, false, nil
		}
		return pluginAPITokenRecord{}, false, err
	}
	return record, true, nil
}

func (r *PostgresRepository) RevokePluginAPIToken(ctx context.Context, id string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE plugin_api_tokens SET status = $1, updated_at = $2 WHERE id = $3`, PluginAPITokenRevoked, updatedAt, strings.TrimSpace(id))
	return err
}

func (r *PostgresRepository) TouchPluginAPIToken(ctx context.Context, id string, usedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE plugin_api_tokens SET last_used_at = $1 WHERE id = $2`, usedAt, strings.TrimSpace(id))
	return err
}

func (r *PostgresRepository) SaveOfficialFeed(ctx context.Context, record officialFeedRecord) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO official_feed_snapshots(
  service_key, feed_id, feed_version, data_schema_version, status, signature_verified,
  payload_sha256, size_bytes, payload_ciphertext, envelope_json,
  issued_at, expires_at, imported_at, updated_at
)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT(service_key, feed_id) DO UPDATE SET
  feed_version = EXCLUDED.feed_version,
  data_schema_version = EXCLUDED.data_schema_version,
  status = EXCLUDED.status,
  signature_verified = EXCLUDED.signature_verified,
  payload_sha256 = EXCLUDED.payload_sha256,
  size_bytes = EXCLUDED.size_bytes,
  payload_ciphertext = EXCLUDED.payload_ciphertext,
  envelope_json = EXCLUDED.envelope_json,
  issued_at = EXCLUDED.issued_at,
  expires_at = EXCLUDED.expires_at,
  imported_at = EXCLUDED.imported_at,
  updated_at = EXCLUDED.updated_at
`, record.ServiceKey, record.FeedID, record.FeedVersion, record.DataSchemaVersion, record.Status, record.SignatureVerified, record.PayloadSHA256, record.SizeBytes, record.PayloadCiphertext, record.EnvelopeJSON, record.IssuedAt, record.ExpiresAt, record.ImportedAt, record.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListOfficialFeeds(ctx context.Context, serviceKey string) ([]officialFeedRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT service_key, feed_id, feed_version, data_schema_version, status, signature_verified,
       payload_sha256, size_bytes, payload_ciphertext, envelope_json,
       issued_at, expires_at, imported_at, updated_at
FROM official_feed_snapshots
WHERE ($1 = '' OR service_key = $1)
ORDER BY imported_at DESC
`, strings.TrimSpace(serviceKey))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []officialFeedRecord{}
	for rows.Next() {
		record, err := scanOfficialFeedRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) LatestOfficialFeed(ctx context.Context, serviceKey string) (officialFeedRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT service_key, feed_id, feed_version, data_schema_version, status, signature_verified,
       payload_sha256, size_bytes, payload_ciphertext, envelope_json,
       issued_at, expires_at, imported_at, updated_at
FROM official_feed_snapshots
WHERE service_key = $1
ORDER BY imported_at DESC
LIMIT 1
`, strings.TrimSpace(serviceKey))
	record, err := scanOfficialFeedRecord(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return officialFeedRecord{}, false, nil
		}
		return officialFeedRecord{}, false, err
	}
	return record, true, nil
}

func (r *PostgresRepository) UpdateOfficialFeedStatus(ctx context.Context, serviceKey string, feedID string, status string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
UPDATE official_feed_snapshots
SET status = $1, updated_at = $2
WHERE service_key = $3 AND feed_id = $4
`, strings.TrimSpace(status), updatedAt, strings.TrimSpace(serviceKey), strings.TrimSpace(feedID))
	return err
}

func (r *PostgresRepository) SaveOfficialFeedSyncRun(ctx context.Context, record officialFeedSyncRunRecord) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO official_feed_sync_runs(
  id, service_key, feed_id, mode, status, request_id, source_url,
  error_code, error, started_at, finished_at
)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT(id) DO UPDATE SET
  service_key = EXCLUDED.service_key,
  feed_id = EXCLUDED.feed_id,
  mode = EXCLUDED.mode,
  status = EXCLUDED.status,
  request_id = EXCLUDED.request_id,
  source_url = EXCLUDED.source_url,
  error_code = EXCLUDED.error_code,
  error = EXCLUDED.error,
  started_at = EXCLUDED.started_at,
  finished_at = EXCLUDED.finished_at
`, record.ID, record.ServiceKey, record.FeedID, record.Mode, record.Status, record.RequestID, record.SourceURL, record.ErrorCode, record.Error, record.StartedAt, record.FinishedAt)
	return err
}

func (r *PostgresRepository) ListOfficialFeedSyncRuns(ctx context.Context, serviceKey string, limit int) ([]officialFeedSyncRunRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, service_key, feed_id, mode, status, request_id, source_url,
       error_code, error, started_at, finished_at
FROM official_feed_sync_runs
WHERE ($1 = '' OR service_key = $1)
ORDER BY started_at DESC
LIMIT $2
`, strings.TrimSpace(serviceKey), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []officialFeedSyncRunRecord{}
	for rows.Next() {
		record, err := scanOfficialFeedSyncRunRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) Health(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func (r *PostgresRepository) Close() error {
	return r.db.Close()
}

func appendDeliveryFilter(clauses *[]string, args *[]any, column string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	*args = append(*args, value)
	*clauses = append(*clauses, fmt.Sprintf("%s = $%d", column, len(*args)))
}
