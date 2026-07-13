ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS balance_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS concurrency_limit INTEGER NOT NULL DEFAULT 5;
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS rpm_limit INTEGER NOT NULL DEFAULT 0;

ALTER TABLE workspace_users ADD CONSTRAINT workspace_users_balance_nonnegative CHECK (balance_cents >= 0);
ALTER TABLE workspace_users ADD CONSTRAINT workspace_users_concurrency_nonnegative CHECK (concurrency_limit >= 0);
ALTER TABLE workspace_users ADD CONSTRAINT workspace_users_rpm_nonnegative CHECK (rpm_limit >= 0);
