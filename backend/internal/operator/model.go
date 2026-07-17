package operator

import "time"

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

type CustomerGroup struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CustomerGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type Customer struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	GroupID       string    `json:"group_id"`
	PlanID        string    `json:"plan_id"`
	Status        string    `json:"status"`
	BalanceMicros int64     `json:"balance_micros"`
	CreditMicros  int64     `json:"credit_micros"`
	Notes         string    `json:"notes"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CustomerRequest struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	GroupID      string `json:"group_id"`
	PlanID       string `json:"plan_id"`
	Status       string `json:"status"`
	Notes        string `json:"notes"`
	CreditMicros int64  `json:"credit_micros"`
}

type Plan struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	MonthlyFeeMicros   int64     `json:"monthly_fee_micros"`
	IncludedTokens     int64     `json:"included_tokens"`
	MonthlyLimitMicros int64     `json:"monthly_limit_micros"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type PlanRequest struct {
	Name               string `json:"name"`
	Description        string `json:"description"`
	Status             string `json:"status"`
	MonthlyFeeMicros   int64  `json:"monthly_fee_micros"`
	IncludedTokens     int64  `json:"included_tokens"`
	MonthlyLimitMicros int64  `json:"monthly_limit_micros"`
}

type BalanceEntry struct {
	ID              string    `json:"id"`
	CustomerID      string    `json:"customer_id"`
	Kind            string    `json:"kind"`
	AmountMicros    int64     `json:"amount_micros"`
	BalanceAfter    int64     `json:"balance_after_micros"`
	Currency        string    `json:"currency"`
	BillingLedgerID string    `json:"billing_ledger_id,omitempty"`
	Reference       string    `json:"reference"`
	Note            string    `json:"note"`
	Actor           string    `json:"actor"`
	CreatedAt       time.Time `json:"created_at"`
}

type BalanceEntryRequest struct {
	CustomerID   string `json:"customer_id"`
	Kind         string `json:"kind"`
	Reference    string `json:"reference"`
	Note         string `json:"note"`
	AmountMicros int64  `json:"amount_micros"`
}

type RiskRule struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	RuleType    string    `json:"rule_type"`
	Threshold   float64   `json:"threshold"`
	WindowMins  int       `json:"window_minutes"`
	Action      string    `json:"action"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type RiskRuleRequest struct {
	Name        string  `json:"name"`
	RuleType    string  `json:"rule_type"`
	Action      string  `json:"action"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	Threshold   float64 `json:"threshold"`
	WindowMins  int     `json:"window_minutes"`
}

type Notice struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Content   string     `json:"content"`
	Audience  string     `json:"audience"`
	Status    string     `json:"status"`
	PublishAt *time.Time `json:"publish_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type NoticeRequest struct {
	Title     string `json:"title"`
	Content   string `json:"content"`
	Audience  string `json:"audience"`
	Status    string `json:"status"`
	PublishAt string `json:"publish_at"`
}

type Dashboard struct {
	Customers       int   `json:"customers"`
	ActiveCustomers int   `json:"active_customers"`
	Plans           int   `json:"plans"`
	BalanceMicros   int64 `json:"balance_micros"`
	RiskRules       int   `json:"risk_rules"`
	PublishedNotice int   `json:"published_notices"`
}
