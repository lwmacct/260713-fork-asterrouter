ALTER TABLE operator_balance_entries
  ADD COLUMN IF NOT EXISTS billing_ledger_id TEXT NOT NULL DEFAULT '';

ALTER TABLE operator_balance_entries
  ADD COLUMN IF NOT EXISTS reference TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS operator_balance_ledger_idx
  ON operator_balance_entries(billing_ledger_id)
  WHERE billing_ledger_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS operator_balance_reference_idx
  ON operator_balance_entries(customer_id, reference)
  WHERE reference <> '';
