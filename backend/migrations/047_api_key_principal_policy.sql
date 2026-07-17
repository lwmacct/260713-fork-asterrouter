ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS principal_type TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS principal_reference TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS scopes TEXT NOT NULL DEFAULT '["gateway:invoke","models:read"]';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS allowed_modalities TEXT NOT NULL DEFAULT '["metadata","text"]';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS allowed_operations TEXT NOT NULL DEFAULT '["list_models","chat_completion"]';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS rpm_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS tpm_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS concurrency_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS monthly_budget_micros BIGINT NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS monthly_image_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS monthly_video_seconds_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS monthly_audio_seconds_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS allowed_cidrs TEXT NOT NULL DEFAULT '[]';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS lane_policy TEXT NOT NULL DEFAULT 'direct_only';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS artifact_policy TEXT NOT NULL DEFAULT 'proxy_only';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS rotation_family_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS api_keys_tenant_principal_idx
  ON api_keys(profile_scope, tenant_id, principal_reference, status);

CREATE INDEX IF NOT EXISTS api_keys_rotation_family_idx
  ON api_keys(rotation_family_id)
  WHERE rotation_family_id <> '';
