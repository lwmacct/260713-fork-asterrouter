ALTER TABLE csv_export_jobs
  ADD COLUMN IF NOT EXISTS owner TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS csv_export_jobs_owner_created_idx
  ON csv_export_jobs(owner, created_at DESC);
