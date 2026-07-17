package controlplane

import "time"

const (
	DepartmentStatusActive   = "active"
	DepartmentStatusArchived = "archived"
)

type Department struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Code                string    `json:"code"`
	ParentID            string    `json:"parent_id"`
	CostCenter          string    `json:"cost_center"`
	MonthlyBudgetMicros int64     `json:"monthly_budget_micros"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type DepartmentRequest struct {
	Name                string `json:"name"`
	Code                string `json:"code"`
	ParentID            string `json:"parent_id"`
	CostCenter          string `json:"cost_center"`
	MonthlyBudgetMicros int64  `json:"monthly_budget_micros"`
	Status              string `json:"status"`
}
