package operator

import (
	"context"
	"database/sql"
	"time"
)

func (r *PostgresRepository) ListGroups(ctx context.Context) ([]CustomerGroup, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,description,status,created_at,updated_at FROM operator_customer_groups ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomerGroup{}
	for rows.Next() {
		var v CustomerGroup
		if err := rows.Scan(&v.ID, &v.Name, &v.Description, &v.Status, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgresRepository) SaveGroup(ctx context.Context, v CustomerGroup) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO operator_customer_groups(id,name,description,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,status=EXCLUDED.status,updated_at=EXCLUDED.updated_at`, v.ID, v.Name, v.Description, v.Status, v.CreatedAt, v.UpdatedAt)
	return err
}
func (r *PostgresRepository) DeleteGroup(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM operator_customer_groups WHERE id=$1`, id)
	return err
}

func (r *PostgresRepository) ListCustomers(ctx context.Context) ([]Customer, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,email,group_id,plan_id,status,balance_micros,credit_micros,notes,created_at,updated_at FROM operator_customers ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Customer{}
	for rows.Next() {
		var v Customer
		if err := rows.Scan(&v.ID, &v.Name, &v.Email, &v.GroupID, &v.PlanID, &v.Status, &v.BalanceMicros, &v.CreditMicros, &v.Notes, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgresRepository) SaveCustomer(ctx context.Context, v Customer) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO operator_customers(id,name,email,group_id,plan_id,status,balance_micros,credit_micros,notes,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,email=EXCLUDED.email,group_id=EXCLUDED.group_id,plan_id=EXCLUDED.plan_id,status=EXCLUDED.status,credit_micros=EXCLUDED.credit_micros,notes=EXCLUDED.notes,updated_at=EXCLUDED.updated_at`, v.ID, v.Name, v.Email, v.GroupID, v.PlanID, v.Status, v.BalanceMicros, v.CreditMicros, v.Notes, v.CreatedAt, v.UpdatedAt)
	return err
}
func (r *PostgresRepository) DeleteCustomer(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM operator_customers WHERE id=$1`, id)
	return err
}

func (r *PostgresRepository) ListPlans(ctx context.Context) ([]Plan, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,description,monthly_fee_micros,included_tokens,monthly_limit_micros,status,created_at,updated_at FROM operator_plans ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Plan{}
	for rows.Next() {
		var v Plan
		if err := rows.Scan(&v.ID, &v.Name, &v.Description, &v.MonthlyFeeMicros, &v.IncludedTokens, &v.MonthlyLimitMicros, &v.Status, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgresRepository) SavePlan(ctx context.Context, v Plan) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO operator_plans(id,name,description,monthly_fee_micros,included_tokens,monthly_limit_micros,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,included_tokens=EXCLUDED.included_tokens,monthly_limit_micros=EXCLUDED.monthly_limit_micros,status=EXCLUDED.status,updated_at=EXCLUDED.updated_at`, v.ID, v.Name, v.Description, v.MonthlyFeeMicros, v.IncludedTokens, v.MonthlyLimitMicros, v.Status, v.CreatedAt, v.UpdatedAt)
	return err
}
func (r *PostgresRepository) DeletePlan(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM operator_plans WHERE id=$1`, id)
	return err
}

func (r *PostgresRepository) ListBalanceEntries(ctx context.Context) ([]BalanceEntry, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,customer_id,kind,amount_micros,balance_after_micros,currency,billing_ledger_id,reference,note,actor,created_at FROM operator_balance_entries ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BalanceEntry{}
	for rows.Next() {
		var v BalanceEntry
		if err := rows.Scan(&v.ID, &v.CustomerID, &v.Kind, &v.AmountMicros, &v.BalanceAfter, &v.Currency, &v.BillingLedgerID, &v.Reference, &v.Note, &v.Actor, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgresRepository) ApplyBalanceEntry(ctx context.Context, v BalanceEntry) (BalanceEntry, error) {
	if v.Currency == "" {
		v.Currency = "USD"
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return BalanceEntry{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var current int64
	if v.BillingLedgerID != "" {
		var existing BalanceEntry
		err := tx.QueryRowContext(ctx, `SELECT id,customer_id,kind,amount_micros,balance_after_micros,currency,billing_ledger_id,reference,note,actor,created_at FROM operator_balance_entries WHERE billing_ledger_id=$1`, v.BillingLedgerID).Scan(&existing.ID, &existing.CustomerID, &existing.Kind, &existing.AmountMicros, &existing.BalanceAfter, &existing.Currency, &existing.BillingLedgerID, &existing.Reference, &existing.Note, &existing.Actor, &existing.CreatedAt)
		if err == nil {
			return existing, tx.Commit()
		}
		if err != sql.ErrNoRows {
			return BalanceEntry{}, err
		}
	} else if v.Reference != "" {
		var existing BalanceEntry
		var existingCreatedAt time.Time
		err := tx.QueryRowContext(ctx, `SELECT id,customer_id,kind,amount_micros,balance_after_micros,currency,billing_ledger_id,reference,note,actor,created_at FROM operator_balance_entries WHERE customer_id=$1 AND reference=$2`, v.CustomerID, v.Reference).Scan(&existing.ID, &existing.CustomerID, &existing.Kind, &existing.AmountMicros, &existing.BalanceAfter, &existing.Currency, &existing.BillingLedgerID, &existing.Reference, &existing.Note, &existing.Actor, &existingCreatedAt)
		if err == nil {
			existing.CreatedAt = existingCreatedAt
			return existing, tx.Commit()
		}
		if err != sql.ErrNoRows {
			return BalanceEntry{}, err
		}
	}
	if err := tx.QueryRowContext(ctx, `SELECT balance_micros FROM operator_customers WHERE id=$1 FOR UPDATE`, v.CustomerID).Scan(&current); err != nil {
		return BalanceEntry{}, err
	}
	v.BalanceAfter = current + v.AmountMicros
	if _, err := tx.ExecContext(ctx, `UPDATE operator_customers SET balance_micros=$1,updated_at=$2 WHERE id=$3`, v.BalanceAfter, v.CreatedAt, v.CustomerID); err != nil {
		return BalanceEntry{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO operator_balance_entries(id,customer_id,kind,amount_micros,balance_after_micros,currency,billing_ledger_id,reference,note,actor,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, v.ID, v.CustomerID, v.Kind, v.AmountMicros, v.BalanceAfter, v.Currency, v.BillingLedgerID, v.Reference, v.Note, v.Actor, v.CreatedAt); err != nil {
		return BalanceEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return BalanceEntry{}, err
	}
	return v, nil
}

func (r *PostgresRepository) ListRiskRules(ctx context.Context) ([]RiskRule, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,rule_type,threshold,window_minutes,action,description,status,created_at,updated_at FROM operator_risk_rules ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RiskRule{}
	for rows.Next() {
		var v RiskRule
		if err := rows.Scan(&v.ID, &v.Name, &v.RuleType, &v.Threshold, &v.WindowMins, &v.Action, &v.Description, &v.Status, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgresRepository) SaveRiskRule(ctx context.Context, v RiskRule) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO operator_risk_rules(id,name,rule_type,threshold,window_minutes,action,description,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,rule_type=EXCLUDED.rule_type,threshold=EXCLUDED.threshold,window_minutes=EXCLUDED.window_minutes,action=EXCLUDED.action,description=EXCLUDED.description,status=EXCLUDED.status,updated_at=EXCLUDED.updated_at`, v.ID, v.Name, v.RuleType, v.Threshold, v.WindowMins, v.Action, v.Description, v.Status, v.CreatedAt, v.UpdatedAt)
	return err
}
func (r *PostgresRepository) DeleteRiskRule(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM operator_risk_rules WHERE id=$1`, id)
	return err
}

func (r *PostgresRepository) ListNotices(ctx context.Context) ([]Notice, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,title,content,audience,status,publish_at,created_at,updated_at FROM operator_notices ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Notice{}
	for rows.Next() {
		var v Notice
		var publish sql.NullTime
		if err := rows.Scan(&v.ID, &v.Title, &v.Content, &v.Audience, &v.Status, &publish, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		if publish.Valid {
			v.PublishAt = &publish.Time
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgresRepository) SaveNotice(ctx context.Context, v Notice) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO operator_notices(id,title,content,audience,status,publish_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO UPDATE SET title=EXCLUDED.title,content=EXCLUDED.content,audience=EXCLUDED.audience,status=EXCLUDED.status,publish_at=EXCLUDED.publish_at,updated_at=EXCLUDED.updated_at`, v.ID, v.Title, v.Content, v.Audience, v.Status, v.PublishAt, v.CreatedAt, v.UpdatedAt)
	return err
}
func (r *PostgresRepository) DeleteNotice(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM operator_notices WHERE id=$1`, id)
	return err
}
