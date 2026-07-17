package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/astercloud/asterrouter/backend/internal/pricing"
)

var (
	ErrBillingHoldBudgetExceeded      = errors.New("billing hold exceeds available monthly budget")
	ErrBillingHoldEstimateUnavailable = errors.New("billing hold cost estimate is unavailable")
	ErrBillingHoldImageQuotaExceeded  = errors.New("billing hold exceeds the monthly image quota")
	ErrBillingHoldVideoQuotaExceeded  = errors.New("billing hold exceeds the monthly video quota")
	ErrBillingHoldAudioQuotaExceeded  = errors.New("billing hold exceeds the monthly audio quota")
	ErrBillingHoldUsageEstimate       = errors.New("billing hold requires a media usage estimate")
	ErrBillingHoldStateConflict       = errors.New("billing hold state changed concurrently")
)

const (
	BillingHoldStatusReserved  = "reserved"
	BillingHoldStatusCommitted = "committed"
	BillingHoldStatusSettled   = "settled"
	BillingHoldStatusReleased  = "released"
	BillingHoldStatusDisputed  = "disputed"

	BillingHoldDefaultTTL             = AIJobDefaultTTL
	billingHoldDefaultMaxOutputTokens = 4096
)

type BillingHold struct {
	ID                       string          `json:"id"`
	OperationID              string          `json:"operation_id"`
	ProfileScope             string          `json:"profile_scope"`
	TenantID                 string          `json:"tenant_id"`
	CredentialID             string          `json:"credential_id"`
	CredentialSource         string          `json:"credential_source"`
	IntegrationID            string          `json:"integration_id"`
	PrincipalType            string          `json:"principal_type"`
	PrincipalID              string          `json:"principal_id"`
	ExternalSubjectReference string          `json:"external_subject_reference"`
	RequestFingerprint       string          `json:"request_fingerprint"`
	Status                   string          `json:"status"`
	Version                  int             `json:"version"`
	ReservedAmountMicros     int64           `json:"reserved_amount_micros"`
	ReservedUsageDimensions  UsageDimensions `json:"reserved_usage_dimensions"`
	SettledAmountMicros      int64           `json:"settled_amount_micros"`
	Currency                 string          `json:"currency"`
	EstimateSource           string          `json:"estimate_source"`
	Reason                   string          `json:"reason,omitempty"`
	BudgetPeriodStart        time.Time       `json:"budget_period_start"`
	ExpiresAt                time.Time       `json:"expires_at"`
	CreatedAt                time.Time       `json:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"`
	SettledAt                *time.Time      `json:"settled_at,omitempty"`
	ReleasedAt               *time.Time      `json:"released_at,omitempty"`
}

type BillingHoldPricingVersion struct {
	HoldID                 string `json:"hold_id"`
	Purpose                string `json:"purpose"`
	PricingRuleVersionID   string `json:"pricing_rule_version_id"`
	EstimateEvaluationID   string `json:"estimate_evaluation_id"`
	SettlementEvaluationID string `json:"settlement_evaluation_id,omitempty"`
}

type BillingHoldAdmission struct {
	Hold                     BillingHold
	PricingVersions          []BillingHoldPricingVersion
	PricingEvaluations       []PricingEvaluation
	MonthlyBudgetMicros      int64
	MonthlyImageLimit        int
	MonthlyVideoSecondsLimit int
	MonthlyAudioSecondsLimit int
}

func (s *Service) newBillingHoldAdmission(ctx context.Context, operation AIOperation, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) (BillingHoldAdmission, error) {
	estimate, err := s.estimateBillingHold(ctx, operation, auth, request)
	if err != nil {
		return BillingHoldAdmission{}, err
	}
	if auth.Limits.MonthlyBudgetMicros > 0 && estimate.ReservedAmountMicros == 0 && estimate.Source == "unpriced" {
		return BillingHoldAdmission{}, ErrBillingHoldEstimateUnavailable
	}
	if auth.Limits.MonthlyBudgetMicros > 0 && estimate.Currency != pricing.CurrencyUSD {
		return BillingHoldAdmission{}, ErrBillingHoldEstimateUnavailable
	}
	reservedUsage, err := usageReservationForCanonicalRequest(request)
	if err != nil {
		return BillingHoldAdmission{}, err
	}
	if auth.Limits.MonthlyVideoSecondsLimit > 0 && request.Modality == "video" && request.VideoDurationMS <= 0 {
		return BillingHoldAdmission{}, ErrBillingHoldUsageEstimate
	}
	if auth.Limits.MonthlyAudioSecondsLimit > 0 && request.Modality == "audio" {
		durationMS := request.AudioDurationMS
		if request.Operation == GatewayOperationAudioTranscription || request.Operation == GatewayOperationAudioTranslation {
			durationMS = request.InputAudioDurationMS
		}
		if durationMS <= 0 {
			return BillingHoldAdmission{}, ErrBillingHoldUsageEstimate
		}
	}
	now := operation.CreatedAt.UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	hold := BillingHold{
		ID: "hold_" + randomID(12), OperationID: operation.ID, ProfileScope: operation.ProfileScope, TenantID: operation.TenantID,
		CredentialID: operation.CredentialID, CredentialSource: operation.CredentialSource, IntegrationID: operation.IntegrationID,
		PrincipalType: operation.PrincipalType, PrincipalID: operation.PrincipalID, ExternalSubjectReference: operation.ExternalSubjectReference,
		RequestFingerprint: operation.RequestFingerprint, Status: BillingHoldStatusReserved, Version: 1,
		ReservedAmountMicros: estimate.ReservedAmountMicros, ReservedUsageDimensions: reservedUsage,
		Currency: estimate.Currency, EstimateSource: estimate.Source,
		BudgetPeriodStart: periodStart, ExpiresAt: now.Add(BillingHoldDefaultTTL), CreatedAt: now, UpdatedAt: now,
	}
	for index := range estimate.PricingVersions {
		estimate.PricingVersions[index].HoldID = hold.ID
	}
	return BillingHoldAdmission{
		Hold: hold, PricingVersions: estimate.PricingVersions, PricingEvaluations: estimate.PricingEvaluations, MonthlyBudgetMicros: nonNegativeInt64(auth.Limits.MonthlyBudgetMicros),
		MonthlyImageLimit:        nonNegative(auth.Limits.MonthlyImageLimit),
		MonthlyVideoSecondsLimit: nonNegative(auth.Limits.MonthlyVideoSecondsLimit),
		MonthlyAudioSecondsLimit: nonNegative(auth.Limits.MonthlyAudioSecondsLimit),
	}, nil
}

type billingHoldEstimate struct {
	ReservedAmountMicros int64
	Currency             string
	Source               string
	PricingVersions      []BillingHoldPricingVersion
	PricingEvaluations   []PricingEvaluation
}

func usageReservationForCanonicalRequest(request gatewaycore.CanonicalRequest) (UsageDimensions, error) {
	reserved := UsageDimensions{}
	switch request.Modality {
	case GatewayModalityImage:
		count := request.OutputCount
		if count <= 0 && request.Operation == GatewayOperationImageGeneration {
			count = 1
		}
		if count > 0 {
			reserved[UsageDimensionOutputImages] = UsageDimension{Quantity: int64(count), Unit: UsageUnitCount, Source: "request", Confidence: UsageConfidenceEstimated}
		}
	case "video":
		if request.VideoDurationMS > 0 {
			reserved[UsageDimensionOutputVideoMilliseconds] = UsageDimension{Quantity: request.VideoDurationMS, Unit: UsageUnitMillisecond, Source: "request", Confidence: UsageConfidenceEstimated}
		}
	case "audio":
		if request.InputAudioDurationMS > 0 {
			reserved[UsageDimensionInputAudioMilliseconds] = UsageDimension{Quantity: request.InputAudioDurationMS, Unit: UsageUnitMillisecond, Source: "request", Confidence: UsageConfidenceEstimated}
		}
		if request.AudioDurationMS > 0 {
			reserved[UsageDimensionOutputAudioMilliseconds] = UsageDimension{Quantity: request.AudioDurationMS, Unit: UsageUnitMillisecond, Source: "request", Confidence: UsageConfidenceEstimated}
		}
	}
	return NormalizeUsageDimensions(reserved)
}

func (s *Service) estimateBillingHold(ctx context.Context, operation AIOperation, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) (billingHoldEstimate, error) {
	var limits struct {
		MaxTokens           int   `json:"max_tokens"`
		MaxCompletionTokens int   `json:"max_completion_tokens"`
		MaxCostMicros       int64 `json:"max_cost_micros"`
	}
	if len(request.Payload) > 0 {
		if err := json.Unmarshal(request.Payload, &limits); err != nil {
			return billingHoldEstimate{}, err
		}
	}
	if limits.MaxTokens < 0 || limits.MaxCompletionTokens < 0 || limits.MaxCostMicros < 0 {
		return billingHoldEstimate{}, errors.New("billing hold request limits must be non-negative")
	}
	estimate := billingHoldEstimate{ReservedAmountMicros: limits.MaxCostMicros, Currency: pricing.CurrencyUSD, Source: "unpriced"}
	facts := estimatePricingFacts(operation, request, limits.MaxTokens, limits.MaxCompletionTokens)
	selection, found, err := s.SelectPricingRule(ctx, PricingPurposeUsageCost, "", request.Model)
	if err != nil {
		return billingHoldEstimate{}, err
	}
	if found {
		result, evaluateErr := s.pricingEngine.Evaluate(selection.Compiled, facts)
		if evaluateErr != nil {
			return billingHoldEstimate{}, evaluateErr
		}
		estimate.ReservedAmountMicros = max(estimate.ReservedAmountMicros, result.AmountMicros)
		estimate.Currency = result.Currency
		estimate.Source = "pricing_rule"
		evaluation := successfulPricingEvaluation(operation, PricingPurposeUsageCost, pricing.PhaseEstimate, selection, facts, result)
		estimate.PricingEvaluations = append(estimate.PricingEvaluations, evaluation)
		estimate.PricingVersions = append(estimate.PricingVersions, BillingHoldPricingVersion{Purpose: PricingPurposeUsageCost, PricingRuleVersionID: selection.Version.ID, EstimateEvaluationID: evaluation.ID})
	} else if estimate.ReservedAmountMicros > 0 {
		estimate.Source = "request_max_cost"
	}
	if auth.PrincipalType == APIKeyTypeCustomer {
		if s.customerPricingResolver == nil {
			return billingHoldEstimate{}, errors.New("customer pricing context resolver is unavailable")
		}
		context, resolveErr := s.customerPricingResolver.ResolveCustomerPricingContext(ctx, auth.PrincipalID)
		if resolveErr != nil || context.Status != PricingRuleStatusActive || context.Currency != pricing.CurrencyUSD {
			return billingHoldEstimate{}, errors.New("customer pricing context is unavailable")
		}
		chargeSelection, chargeFound, selectErr := s.SelectPricingRule(ctx, PricingPurposeCustomerCharge, context.PlanID, request.Model)
		if selectErr != nil {
			return billingHoldEstimate{}, selectErr
		}
		if !chargeFound {
			return billingHoldEstimate{}, &pricing.Error{Code: pricing.ErrorRuleUnavailable, Message: "customer charge pricing rule is unavailable"}
		}
		result, evaluateErr := s.pricingEngine.Evaluate(chargeSelection.Compiled, facts)
		if evaluateErr != nil {
			return billingHoldEstimate{}, evaluateErr
		}
		evaluation := successfulPricingEvaluation(operation, PricingPurposeCustomerCharge, pricing.PhaseEstimate, chargeSelection, facts, result)
		estimate.PricingEvaluations = append(estimate.PricingEvaluations, evaluation)
		estimate.PricingVersions = append(estimate.PricingVersions, BillingHoldPricingVersion{Purpose: PricingPurposeCustomerCharge, PricingRuleVersionID: chargeSelection.Version.ID, EstimateEvaluationID: evaluation.ID})
	}
	return estimate, nil
}

func estimatePricingFacts(operation AIOperation, request gatewaycore.CanonicalRequest, maxTokens, maxCompletionTokens int) pricing.Facts {
	outputTokens := max(maxTokens, maxCompletionTokens)
	if outputTokens == 0 && request.Modality == GatewayModalityText {
		outputTokens = billingHoldDefaultMaxOutputTokens
	}
	inputTokens := max(1, len(request.Payload)/4)
	outputCount := request.OutputCount
	if outputCount <= 0 && request.Operation == GatewayOperationImageGeneration {
		outputCount = 1
	}
	return pricing.Facts{
		TotalInputTokens: int64(inputTokens), UncachedInputTokens: int64(inputTokens), OutputTokens: int64(outputTokens),
		OutputImages: int64(max(outputCount, 0)), OutputVideoMilliseconds: max(request.VideoDurationMS, 0),
		InputAudioMilliseconds: max(request.InputAudioDurationMS, 0), OutputAudioMilliseconds: max(request.AudioDurationMS, 0),
		InputCharacters: max(request.InputCharacters, 0), Protocol: string(request.Protocol), Operation: request.Operation,
		Modality: request.Modality, Lane: string(request.Lane), Stream: request.Stream, OutputCount: int64(max(outputCount, 0)),
		NormalizationStatus: "estimated_request", Phase: pricing.PhaseEstimate, ObservedAt: operation.CreatedAt,
	}
}

func successfulPricingEvaluation(operation AIOperation, purpose, phase string, selection PricingSelection, facts pricing.Facts, result pricing.Result) PricingEvaluation {
	amount := result.AmountMicros
	return PricingEvaluation{
		ID: "peval_" + randomID(12), Purpose: purpose, Phase: phase, OperationID: operation.ID,
		PricingRuleID: selection.Rule.ID, PricingRuleVersionID: selection.Version.ID, EngineVersion: result.EngineVersion,
		ExpressionHash: result.ExpressionHash, FactsHash: result.FactsHash, Facts: facts, AmountMicros: &amount,
		Currency: result.Currency, MatchedTier: result.MatchedTier, Lines: result.Lines,
		NormalizationStatus: facts.NormalizationStatus, Status: PricingEvaluationStatusSuccess, CreatedAt: operation.CreatedAt,
	}
}

func validateBillingHoldAdmission(operation AIOperation, admission BillingHoldAdmission) error {
	hold := admission.Hold
	if _, err := NormalizeUsageDimensions(hold.ReservedUsageDimensions); err != nil {
		return err
	}
	if strings.TrimSpace(hold.ID) == "" || hold.OperationID != operation.ID || hold.CredentialID != operation.CredentialID ||
		hold.ProfileScope != operation.ProfileScope || hold.TenantID != operation.TenantID || hold.CredentialSource != operation.CredentialSource ||
		hold.IntegrationID != operation.IntegrationID || hold.PrincipalType != operation.PrincipalType || hold.PrincipalID != operation.PrincipalID ||
		hold.ExternalSubjectReference != operation.ExternalSubjectReference ||
		hold.RequestFingerprint == "" || hold.RequestFingerprint != operation.RequestFingerprint || hold.Status != BillingHoldStatusReserved ||
		hold.Version != 1 || hold.ReservedAmountMicros < 0 || hold.SettledAmountMicros != 0 || len(strings.TrimSpace(hold.Currency)) != 3 ||
		hold.BudgetPeriodStart.IsZero() || hold.CreatedAt.IsZero() || !hold.ExpiresAt.After(hold.CreatedAt) ||
		admission.MonthlyBudgetMicros < 0 || admission.MonthlyImageLimit < 0 || admission.MonthlyVideoSecondsLimit < 0 || admission.MonthlyAudioSecondsLimit < 0 {
		return errors.New("invalid billing hold admission")
	}
	evaluations := make(map[string]PricingEvaluation, len(admission.PricingEvaluations))
	for _, evaluation := range admission.PricingEvaluations {
		if evaluation.ID == "" || evaluation.OperationID != operation.ID || evaluation.Phase != pricing.PhaseEstimate || evaluation.Status != PricingEvaluationStatusSuccess || evaluation.AmountMicros == nil {
			return errors.New("invalid billing hold pricing evaluation")
		}
		evaluations[evaluation.ID] = evaluation
	}
	purposes := make(map[string]struct{}, len(admission.PricingVersions))
	for _, version := range admission.PricingVersions {
		evaluation, found := evaluations[version.EstimateEvaluationID]
		if !found || version.HoldID != hold.ID || version.Purpose != evaluation.Purpose || version.PricingRuleVersionID != evaluation.PricingRuleVersionID || version.SettlementEvaluationID != "" {
			return errors.New("invalid billing hold pricing version")
		}
		if _, duplicate := purposes[version.Purpose]; duplicate {
			return errors.New("duplicate billing hold pricing purpose")
		}
		purposes[version.Purpose] = struct{}{}
	}
	if len(evaluations) != len(admission.PricingVersions) {
		return errors.New("orphan billing hold pricing evaluation")
	}
	return nil
}

func billingHoldCountsAgainstBudget(status string) bool {
	return oneOf(status, BillingHoldStatusReserved, BillingHoldStatusCommitted, BillingHoldStatusDisputed)
}

func billingHoldTransitionAllowed(fromStatus, toStatus string) bool {
	switch fromStatus {
	case BillingHoldStatusReserved:
		return oneOf(toStatus, BillingHoldStatusCommitted, BillingHoldStatusSettled, BillingHoldStatusReleased, BillingHoldStatusDisputed)
	case BillingHoldStatusCommitted:
		return oneOf(toStatus, BillingHoldStatusSettled, BillingHoldStatusDisputed)
	case BillingHoldStatusDisputed:
		return oneOf(toStatus, BillingHoldStatusSettled, BillingHoldStatusReleased)
	default:
		return false
	}
}

func prepareBillingHoldTransition(hold BillingHold, toStatus string, settledAmount int64, reason string, at time.Time) (BillingHold, error) {
	toStatus = strings.TrimSpace(toStatus)
	if hold.Status == toStatus {
		return hold, nil
	}
	if !billingHoldTransitionAllowed(hold.Status, toStatus) || settledAmount < 0 {
		return BillingHold{}, fmt.Errorf("invalid billing hold transition %s -> %s", hold.Status, toStatus)
	}
	hold.Status = toStatus
	hold.Version++
	hold.Reason = strings.TrimSpace(reason)
	hold.UpdatedAt = at.UTC()
	if toStatus == BillingHoldStatusSettled {
		hold.SettledAmountMicros = settledAmount
		hold.SettledAt = timePointer(at.UTC())
	}
	if toStatus == BillingHoldStatusReleased {
		hold.ReleasedAt = timePointer(at.UTC())
	}
	return hold, nil
}

func (s *Service) BillingHoldForOperation(ctx context.Context, operationID string) (BillingHold, bool, error) {
	return s.repo.FindBillingHoldByOperationID(ctx, strings.TrimSpace(operationID))
}

func (s *Service) CommitBillingHold(ctx context.Context, operationID, reason string) error {
	return s.transitionBillingHold(ctx, operationID, BillingHoldStatusCommitted, 0, reason)
}

func (s *Service) DisputeBillingHold(ctx context.Context, operationID, reason string) error {
	return s.transitionBillingHold(ctx, operationID, BillingHoldStatusDisputed, 0, reason)
}

func (s *Service) ReleaseBillingHold(ctx context.Context, operationID, reason string) error {
	return s.transitionBillingHold(ctx, operationID, BillingHoldStatusReleased, 0, reason)
}

func (s *Service) transitionBillingHold(ctx context.Context, operationID, status string, settledAmount int64, reason string) error {
	hold, found, err := s.repo.FindBillingHoldByOperationID(ctx, strings.TrimSpace(operationID))
	if err != nil || !found {
		return err
	}
	if hold.Status == status || oneOf(hold.Status, BillingHoldStatusSettled, BillingHoldStatusReleased) ||
		(hold.Status == BillingHoldStatusDisputed && status == BillingHoldStatusCommitted) {
		return nil
	}
	_, updated, err := s.repo.TransitionBillingHold(ctx, hold.OperationID, hold.Version, status, settledAmount, strings.TrimSpace(reason), s.nowUTC())
	if err != nil {
		return err
	}
	if !updated {
		return ErrBillingHoldStateConflict
	}
	return nil
}
