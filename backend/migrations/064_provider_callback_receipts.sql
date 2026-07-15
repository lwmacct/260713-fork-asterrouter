CREATE TABLE IF NOT EXISTS provider_callback_receipts (
    event_id TEXT PRIMARY KEY,
    adapter_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL REFERENCES ai_attempts(id) ON DELETE CASCADE,
    provider_id TEXT NOT NULL,
    provider_account_id TEXT NOT NULL,
    provider_task_id TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    error_type TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    processed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS provider_callback_receipts_attempt_idx
    ON provider_callback_receipts (attempt_id, created_at DESC);
