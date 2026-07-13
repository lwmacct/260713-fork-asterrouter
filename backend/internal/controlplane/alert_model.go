package controlplane

import "time"

const (
	AlertTypeAPIKeyQuota           = "api_key_quota"
	AlertTypeAPIKeyBudget          = "api_key_budget"
	AlertTypeGatewayErrorRate      = "gateway_error_rate"
	AlertTypeProviderHealth        = "provider_health"
	AlertTypeProviderAccountHealth = "provider_account_health"
	AlertTypeRiskRule              = "risk_rule"

	AlertSeverityInfo     = "info"
	AlertSeverityWarning  = "warning"
	AlertSeverityCritical = "critical"

	AlertStatusActive       = "active"
	AlertStatusAcknowledged = "acknowledged"
	AlertStatusResolved     = "resolved"
)

type AlertEvent struct {
	ID             string            `json:"id"`
	Type           string            `json:"type"`
	Severity       string            `json:"severity"`
	Status         string            `json:"status"`
	Title          string            `json:"title"`
	Summary        string            `json:"summary"`
	ResourceType   string            `json:"resource_type"`
	ResourceID     string            `json:"resource_id"`
	DedupeKey      string            `json:"dedupe_key"`
	Metadata       map[string]string `json:"metadata"`
	FirstSeenAt    time.Time         `json:"first_seen_at"`
	LastSeenAt     time.Time         `json:"last_seen_at"`
	AcknowledgedAt *time.Time        `json:"acknowledged_at,omitempty"`
	AcknowledgedBy string            `json:"acknowledged_by"`
	ResolvedAt     *time.Time        `json:"resolved_at,omitempty"`
	ResolvedBy     string            `json:"resolved_by"`
}

type AlertQuery struct {
	Limit        int
	Offset       int
	Search       string
	Type         string
	Severity     string
	Status       string
	ResourceType string
	ResourceIDs  []string
	CreatedFrom  time.Time
	CreatedTo    time.Time
}

type AlertSummary struct {
	Total        int `json:"total"`
	Active       int `json:"active"`
	Acknowledged int `json:"acknowledged"`
	Resolved     int `json:"resolved"`
	Warning      int `json:"warning"`
	Critical     int `json:"critical"`
}
