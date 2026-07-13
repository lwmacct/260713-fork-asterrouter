ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS avatar_data_url TEXT NOT NULL DEFAULT '';

DO $$ BEGIN
  ALTER TABLE workspace_users ADD CONSTRAINT workspace_users_avatar_size
    CHECK (octet_length(avatar_data_url) <= 262144);
EXCEPTION
  WHEN duplicate_object THEN NULL;
END $$;
