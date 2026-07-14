CREATE TABLE IF NOT EXISTS artifacts (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL REFERENCES ai_operations(id) ON DELETE RESTRICT,
  job_id TEXT REFERENCES ai_jobs(id) ON DELETE RESTRICT,
  attempt_id TEXT REFERENCES ai_attempts(id) ON DELETE RESTRICT,
  source_artifact_id TEXT REFERENCES artifacts(id) ON DELETE RESTRICT,
  profile_scope TEXT NOT NULL DEFAULT '',
  tenant_id TEXT NOT NULL DEFAULT '',
  integration_id TEXT NOT NULL DEFAULT '',
  principal_type TEXT NOT NULL DEFAULT '',
  principal_id TEXT NOT NULL DEFAULT '',
  external_subject_reference TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL,
  policy TEXT NOT NULL,
  status TEXT NOT NULL,
  status_version INTEGER NOT NULL,
  media_type TEXT NOT NULL DEFAULT '',
  size_bytes BIGINT NOT NULL DEFAULT 0,
  sha256 TEXT NOT NULL DEFAULT '',
  store_driver TEXT NOT NULL DEFAULT 'none',
  store_key TEXT NOT NULL DEFAULT '',
  external_reference TEXT NOT NULL DEFAULT '',
  error_type TEXT NOT NULL DEFAULT '',
  retain_until TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  ready_at TIMESTAMPTZ,
  delivered_at TIMESTAMPTZ,
  deleted_at TIMESTAMPTZ,
  CHECK (status_version > 0),
  CHECK (size_bytes >= 0)
);

CREATE INDEX IF NOT EXISTS artifacts_owner_created_idx
  ON artifacts(profile_scope, tenant_id, integration_id, principal_type, principal_id, external_subject_reference, created_at DESC);

CREATE INDEX IF NOT EXISTS artifacts_job_created_idx
  ON artifacts(job_id, created_at);

CREATE INDEX IF NOT EXISTS artifacts_retention_idx
  ON artifacts(status, retain_until);

CREATE INDEX IF NOT EXISTS artifacts_deletion_idx
  ON artifacts(status, updated_at)
  WHERE status IN ('delete_requested', 'delete_failed');

CREATE TABLE IF NOT EXISTS artifact_events (
  id TEXT PRIMARY KEY,
  artifact_id TEXT NOT NULL REFERENCES artifacts(id) ON DELETE RESTRICT,
  version INTEGER NOT NULL,
  event_type TEXT NOT NULL,
  from_status TEXT NOT NULL DEFAULT '',
  to_status TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE(artifact_id, version)
);

CREATE INDEX IF NOT EXISTS artifact_events_artifact_version_idx
  ON artifact_events(artifact_id, version);
