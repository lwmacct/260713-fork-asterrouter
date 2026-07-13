CREATE TABLE IF NOT EXISTS customer_wallets (
  user_id TEXT PRIMARY KEY REFERENCES workspace_users(id) ON DELETE CASCADE,
  gift_balance_cents INTEGER NOT NULL DEFAULT 0,
  profit_balance_cents INTEGER NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS customer_billing_entries (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  amount_cents INTEGER NOT NULL,
  balance_after_cents INTEGER NOT NULL,
  reference TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS customer_billing_entries_user_created_idx
  ON customer_billing_entries(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS customer_redemption_codes (
  id TEXT PRIMARY KEY,
  code_hash TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL DEFAULT '',
  amount_cents INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  max_redemptions INTEGER NOT NULL DEFAULT 1,
  redeemed_count INTEGER NOT NULL DEFAULT 0,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS customer_redemptions (
  code_id TEXT NOT NULL REFERENCES customer_redemption_codes(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  entry_id TEXT NOT NULL REFERENCES customer_billing_entries(id) ON DELETE CASCADE,
  redeemed_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY(code_id, user_id)
);

CREATE TABLE IF NOT EXISTS customer_vouchers (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  amount_cents INTEGER NOT NULL,
  minimum_recharge_cents INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active',
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS customer_vouchers_user_status_idx
  ON customer_vouchers(user_id, status, expires_at);
