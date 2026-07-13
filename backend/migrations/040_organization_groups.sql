CREATE TABLE IF NOT EXISTS organization_groups (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS organization_groups_name_idx
  ON organization_groups(lower(name));

CREATE TABLE IF NOT EXISTS organization_group_members (
  group_id TEXT NOT NULL REFERENCES organization_groups(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY(group_id, user_id),
  UNIQUE(user_id)
);

CREATE INDEX IF NOT EXISTS organization_group_members_group_idx
  ON organization_group_members(group_id, created_at);
