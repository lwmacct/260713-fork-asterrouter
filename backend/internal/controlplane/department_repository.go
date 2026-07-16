package controlplane

import (
	"context"
	"sort"
)

func (r *MemoryRepository) ListDepartments(context.Context) ([]Department, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Department, 0, len(r.departments))
	for _, department := range r.departments {
		out = append(out, department)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Status == out[j].Status {
			return out[i].Code < out[j].Code
		}
		return out[i].Status < out[j].Status
	})
	return out, nil
}

func (r *MemoryRepository) SaveDepartment(_ context.Context, department Department) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.departments[department.ID] = department
	return nil
}

func (r *PostgresRepository) ListDepartments(ctx context.Context) ([]Department, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, name, code, parent_id, cost_center, monthly_budget_cents, status, created_at, updated_at
FROM departments
ORDER BY status ASC, code ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Department, 0)
	for rows.Next() {
		var department Department
		if err := rows.Scan(&department.ID, &department.Name, &department.Code, &department.ParentID, &department.CostCenter, &department.MonthlyBudgetCents, &department.Status, &department.CreatedAt, &department.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, department)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveDepartment(ctx context.Context, department Department) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO departments(id, name, code, parent_id, cost_center, monthly_budget_cents, status, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(id) DO UPDATE SET
  name = EXCLUDED.name,
  code = EXCLUDED.code,
  parent_id = EXCLUDED.parent_id,
  cost_center = EXCLUDED.cost_center,
  monthly_budget_cents = EXCLUDED.monthly_budget_cents,
  status = EXCLUDED.status,
  updated_at = EXCLUDED.updated_at
`, department.ID, department.Name, department.Code, department.ParentID, department.CostCenter, department.MonthlyBudgetCents, department.Status, department.CreatedAt, department.UpdatedAt)
	return err
}
