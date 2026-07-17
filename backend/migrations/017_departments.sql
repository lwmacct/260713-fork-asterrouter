CREATE TABLE IF NOT EXISTS departments (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  code TEXT NOT NULL UNIQUE,
  parent_id TEXT NOT NULL DEFAULT '',
  cost_center TEXT NOT NULL DEFAULT '',
  monthly_budget_micros BIGINT NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS departments_parent_idx
  ON departments(parent_id);

CREATE INDEX IF NOT EXISTS departments_cost_center_idx
  ON departments(cost_center);
