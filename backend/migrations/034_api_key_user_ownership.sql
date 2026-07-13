ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS owner_user_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS api_keys_owner_user_idx
  ON api_keys(owner_user_id, status)
  WHERE owner_user_id <> '';
