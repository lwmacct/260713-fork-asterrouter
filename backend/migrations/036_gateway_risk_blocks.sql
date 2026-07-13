CREATE TABLE IF NOT EXISTS gateway_risk_blocks (
  api_key_id TEXT PRIMARY KEY,
  rule_id TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS gateway_risk_blocks_expiry_idx
  ON gateway_risk_blocks(expires_at);
