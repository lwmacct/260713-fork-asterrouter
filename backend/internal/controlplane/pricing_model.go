package controlplane

import (
	"context"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/pricing"
)

const (
	PricingPurposeUsageCost         = "usage_cost"
	PricingPurposeCustomerCharge    = "customer_charge"
	PricingScopeGlobal              = "global"
	PricingScopeOperatorPlan        = "operator_plan"
	PricingRuleStatusActive         = "active"
	PricingRuleStatusDisabled       = "disabled"
	PricingVersionStateDraft        = "draft"
	PricingVersionStatePublished    = "published"
	PricingAuthoringVisual          = "visual"
	PricingAuthoringRaw             = "raw"
	PricingEvaluationStatusSuccess  = "succeeded"
	PricingEvaluationStatusFailed   = "failed"
	PricingEvaluationStatusDisputed = "disputed"
)

type PricingRule struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Purpose         string    `json:"purpose"`
	ScopeType       string    `json:"scope_type"`
	ScopeID         string    `json:"scope_id"`
	Model           string    `json:"model"`
	Status          string    `json:"status"`
	ActiveVersionID string    `json:"active_version_id,omitempty"`
	LockVersion     int64     `json:"lock_version"`
	CreatedBy       string    `json:"created_by"`
	UpdatedBy       string    `json:"updated_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type PricingRuleVersion struct {
	ID             string                `json:"id"`
	RuleID         string                `json:"rule_id"`
	Revision       int                   `json:"revision"`
	EngineVersion  int                   `json:"engine_version"`
	Currency       string                `json:"currency"`
	Expression     string                `json:"expression"`
	ExpressionHash string                `json:"expression_hash"`
	Analysis       pricing.RuleAnalysis  `json:"analysis"`
	AuthoringMode  string                `json:"authoring_mode"`
	TestCases      []PricingRuleTestCase `json:"test_cases"`
	State          string                `json:"state"`
	CreatedBy      string                `json:"created_by"`
	PublishedBy    string                `json:"published_by,omitempty"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
	PublishedAt    *time.Time            `json:"published_at,omitempty"`
}

type PricingRuleTestCase struct {
	Name                 string        `json:"name"`
	Facts                pricing.Facts `json:"facts"`
	ExpectedTier         string        `json:"expected_tier"`
	ExpectedAmountMicros *int64        `json:"expected_amount_micros,omitempty"`
}

type PricingEvaluation struct {
	ID                   string                `json:"id"`
	Purpose              string                `json:"purpose"`
	Phase                string                `json:"phase"`
	OperationID          string                `json:"operation_id"`
	AttemptID            string                `json:"attempt_id"`
	UsageRecordID        string                `json:"usage_record_id,omitempty"`
	UsageVersion         int                   `json:"usage_version"`
	PricingRuleID        string                `json:"pricing_rule_id"`
	PricingRuleVersionID string                `json:"pricing_rule_version_id"`
	EngineVersion        int                   `json:"engine_version"`
	ExpressionHash       string                `json:"expression_hash"`
	FactsHash            string                `json:"facts_hash"`
	Facts                pricing.Facts         `json:"facts"`
	AmountMicros         *int64                `json:"amount_micros,omitempty"`
	Currency             string                `json:"currency"`
	MatchedTier          string                `json:"matched_tier"`
	Lines                []pricing.PricingLine `json:"lines"`
	NormalizationStatus  string                `json:"normalization_status"`
	Status               string                `json:"status"`
	FailureCode          string                `json:"failure_code,omitempty"`
	ReplayOfID           string                `json:"replay_of_id,omitempty"`
	CreatedAt            time.Time             `json:"created_at"`
}

type PricingRuleQuery struct {
	Purpose   string
	ScopeType string
	ScopeID   string
	Model     string
	Status    string
}

type PricingRuleCreateRequest struct {
	Name          string                `json:"name"`
	Purpose       string                `json:"purpose"`
	ScopeType     string                `json:"scope_type"`
	ScopeID       string                `json:"scope_id"`
	Model         string                `json:"model"`
	Currency      string                `json:"currency"`
	AuthoringMode string                `json:"authoring_mode"`
	Expression    string                `json:"expression"`
	TestCases     []PricingRuleTestCase `json:"test_cases"`
}

type PricingDraftUpdateRequest struct {
	ExpectedLockVersion int64                 `json:"expected_lock_version"`
	Name                string                `json:"name"`
	Currency            string                `json:"currency"`
	AuthoringMode       string                `json:"authoring_mode"`
	Expression          string                `json:"expression"`
	TestCases           []PricingRuleTestCase `json:"test_cases"`
}

type PricingPublishRequest struct {
	DraftVersionID        string `json:"draft_version_id"`
	ExpectedLockVersion   int64  `json:"expected_lock_version"`
	ExpectedActiveVersion string `json:"expected_active_version_id"`
	ExpressionHash        string `json:"expression_hash"`
	AcknowledgeImpact     bool   `json:"acknowledge_customer_impact"`
}

type PricingActivateRequest struct {
	ExpectedLockVersion int64 `json:"expected_lock_version"`
}

type PricingRuleDetail struct {
	Rule          PricingRule          `json:"rule"`
	ActiveVersion *PricingRuleVersion  `json:"active_version,omitempty"`
	Draft         *PricingRuleVersion  `json:"draft,omitempty"`
	Versions      []PricingRuleVersion `json:"versions"`
}

type PricingValidationResult struct {
	Valid          bool                    `json:"valid"`
	ExpressionHash string                  `json:"expression_hash,omitempty"`
	Analysis       *pricing.RuleAnalysis   `json:"analysis,omitempty"`
	TestResults    []PricingTestCaseResult `json:"test_results"`
	Errors         []*pricing.Error        `json:"errors"`
}

type PricingTestCaseResult struct {
	Name         string                `json:"name"`
	Passed       bool                  `json:"passed"`
	AmountMicros int64                 `json:"amount_micros"`
	Tier         string                `json:"tier"`
	Lines        []pricing.PricingLine `json:"lines,omitempty"`
	Error        *pricing.Error        `json:"error,omitempty"`
}

type PricingSimulationRequest struct {
	RuleVersionID string        `json:"rule_version_id"`
	Expression    string        `json:"expression"`
	Currency      string        `json:"currency"`
	Facts         pricing.Facts `json:"facts"`
}

type CustomerPricingContext struct {
	CustomerID string `json:"customer_id"`
	PlanID     string `json:"plan_id"`
	Status     string `json:"status"`
	Currency   string `json:"currency"`
}

type CustomerPricingContextResolver interface {
	ResolveCustomerPricingContext(ctx context.Context, customerID string) (CustomerPricingContext, error)
	ValidatePricingPlan(ctx context.Context, planID string) error
}
