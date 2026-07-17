package controlplane

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/pricing"
)

var (
	ErrPricingRuleNotFound        = errors.New("pricing rule not found")
	ErrPricingVersionNotFound     = errors.New("pricing rule version not found")
	ErrPricingCASConflict         = errors.New("pricing rule version conflict")
	ErrPricingPublishedImmutable  = errors.New("published pricing version is immutable")
	ErrPricingInvalidSlot         = errors.New("pricing rule selection slot is invalid")
	ErrPricingCurrencyUnsupported = errors.New("only USD pricing rules are supported")
)

type PricingSelection struct {
	Rule           PricingRule
	Version        PricingRuleVersion
	Compiled       pricing.CompiledRule
	ExpressionHash string
}

func (s *Service) PricingEvaluation(ctx context.Context, id string) (PricingEvaluation, bool, error) {
	return s.repo.FindPricingEvaluation(ctx, strings.TrimSpace(id))
}

func (s *Service) PricingRuleVersionDetail(ctx context.Context, versionID string) (PricingRule, PricingRuleVersion, error) {
	version, found, err := s.repo.FindPricingRuleVersion(ctx, strings.TrimSpace(versionID))
	if err != nil {
		return PricingRule{}, PricingRuleVersion{}, err
	}
	if !found {
		return PricingRule{}, PricingRuleVersion{}, ErrPricingVersionNotFound
	}
	rule, found, err := s.repo.FindPricingRule(ctx, version.RuleID)
	if err != nil {
		return PricingRule{}, PricingRuleVersion{}, err
	}
	if !found {
		return PricingRule{}, PricingRuleVersion{}, ErrPricingRuleNotFound
	}
	return rule, version, nil
}

func (s *Service) ListPricingRules(ctx context.Context, query PricingRuleQuery) ([]PricingRule, error) {
	rules, err := s.repo.ListPricingRules(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PricingRule, 0, len(rules))
	for _, rule := range rules {
		if query.Purpose != "" && rule.Purpose != query.Purpose || query.ScopeType != "" && rule.ScopeType != query.ScopeType || query.ScopeID != "" && rule.ScopeID != query.ScopeID || query.Model != "" && rule.Model != query.Model || query.Status != "" && rule.Status != query.Status {
			continue
		}
		out = append(out, rule)
	}
	return out, nil
}

func (s *Service) CreatePricingRule(ctx context.Context, actor string, request PricingRuleCreateRequest) (PricingRuleDetail, error) {
	rule, err := normalizePricingRule(request, actor, s.nowUTC())
	if err != nil {
		return PricingRuleDetail{}, err
	}
	if err := s.validatePricingRuleSlot(ctx, rule); err != nil {
		return PricingRuleDetail{}, err
	}
	draft := PricingRuleVersion{ID: "pdraft_" + randomID(12), RuleID: rule.ID, EngineVersion: pricing.EngineVersionV1, Currency: pricing.CurrencyUSD, Expression: request.Expression, ExpressionHash: pricing.ExpressionHash(request.Expression), AuthoringMode: normalizeAuthoringMode(request.AuthoringMode), TestCases: clonePricingTestCases(request.TestCases), State: PricingVersionStateDraft, CreatedBy: actor, CreatedAt: rule.CreatedAt, UpdatedAt: rule.CreatedAt}
	if err := s.repo.CreatePricingRule(ctx, rule, draft); err != nil {
		return PricingRuleDetail{}, err
	}
	_ = s.audit(ctx, actor, "pricing_rule.create", "pricing_rule", rule.ID, "Created pricing rule")
	return PricingRuleDetail{Rule: rule, Draft: &draft, Versions: []PricingRuleVersion{draft}}, nil
}

func (s *Service) PricingRuleDetail(ctx context.Context, id string) (PricingRuleDetail, error) {
	rule, found, err := s.repo.FindPricingRule(ctx, id)
	if err != nil {
		return PricingRuleDetail{}, err
	}
	if !found {
		return PricingRuleDetail{}, ErrPricingRuleNotFound
	}
	versions, err := s.repo.ListPricingRuleVersions(ctx, rule.ID)
	if err != nil {
		return PricingRuleDetail{}, err
	}
	detail := PricingRuleDetail{Rule: rule, Versions: versions}
	for index := range versions {
		version := versions[index]
		if version.ID == rule.ActiveVersionID {
			copy := version
			detail.ActiveVersion = &copy
		}
		if version.State == PricingVersionStateDraft {
			copy := version
			detail.Draft = &copy
		}
	}
	return detail, nil
}

func (s *Service) UpdatePricingRuleDraft(ctx context.Context, actor, ruleID string, request PricingDraftUpdateRequest) (PricingRuleDetail, error) {
	detail, err := s.PricingRuleDetail(ctx, ruleID)
	if err != nil {
		return PricingRuleDetail{}, err
	}
	if request.ExpectedLockVersion != detail.Rule.LockVersion {
		return PricingRuleDetail{}, ErrPricingCASConflict
	}
	if strings.TrimSpace(request.Expression) == "" {
		return PricingRuleDetail{}, errors.New("expression is required")
	}
	now := s.nowUTC()
	draft := PricingRuleVersion{ID: "pdraft_" + randomID(12), RuleID: ruleID, EngineVersion: pricing.EngineVersionV1, Currency: pricing.CurrencyUSD, Expression: request.Expression, ExpressionHash: pricing.ExpressionHash(request.Expression), AuthoringMode: normalizeAuthoringMode(request.AuthoringMode), TestCases: clonePricingTestCases(request.TestCases), State: PricingVersionStateDraft, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
	if detail.Draft != nil {
		draft.ID = detail.Draft.ID
		draft.CreatedAt = detail.Draft.CreatedAt
	}
	rule := detail.Rule
	if strings.TrimSpace(request.Name) != "" {
		rule.Name = strings.TrimSpace(request.Name)
	}
	rule.UpdatedBy = actor
	rule.UpdatedAt = now
	updated, applied, err := s.repo.SavePricingRuleDraft(ctx, rule, draft, request.ExpectedLockVersion)
	if err != nil {
		return PricingRuleDetail{}, err
	}
	if !applied {
		return PricingRuleDetail{}, ErrPricingCASConflict
	}
	_ = s.audit(ctx, actor, "pricing_rule.draft_update", "pricing_rule", ruleID, "Updated pricing draft")
	return PricingRuleDetail{Rule: updated, ActiveVersion: detail.ActiveVersion, Draft: &draft, Versions: append([]PricingRuleVersion{draft}, detail.Versions...)}, nil
}

func (s *Service) ValidatePricingRule(ctx context.Context, expression string, testCases []PricingRuleTestCase) PricingValidationResult {
	result := PricingValidationResult{TestResults: make([]PricingTestCaseResult, 0, len(testCases))}
	rule, err := s.pricingEngine.Compile(expression)
	if err != nil {
		result.Errors = append(result.Errors, pricingError(err))
		return result
	}
	result.ExpressionHash = rule.ExpressionHash
	result.Analysis = &rule.Analysis
	result.Valid = true
	for _, testCase := range testCases {
		item := PricingTestCaseResult{Name: testCase.Name}
		evaluated, evalErr := s.pricingEngine.Evaluate(rule, testCase.Facts)
		if evalErr != nil {
			item.Error = pricingError(evalErr)
			result.Valid = false
		} else {
			item.AmountMicros, item.Tier, item.Lines = evaluated.AmountMicros, evaluated.MatchedTier, evaluated.Lines
			item.Passed = (testCase.ExpectedTier == "" || testCase.ExpectedTier == evaluated.MatchedTier) && (testCase.ExpectedAmountMicros == nil || *testCase.ExpectedAmountMicros == evaluated.AmountMicros)
			if !item.Passed {
				result.Valid = false
			}
		}
		result.TestResults = append(result.TestResults, item)
	}
	return result
}

func (s *Service) SimulatePricing(ctx context.Context, request PricingSimulationRequest) (pricing.Result, error) {
	var rule pricing.CompiledRule
	var err error
	if strings.TrimSpace(request.RuleVersionID) != "" {
		version, found, findErr := s.repo.FindPricingRuleVersion(ctx, request.RuleVersionID)
		if findErr != nil {
			return pricing.Result{}, findErr
		}
		if !found {
			return pricing.Result{}, ErrPricingVersionNotFound
		}
		rule, err = s.pricingEngine.CompileByHash(version.Expression, version.ExpressionHash)
	} else {
		rule, err = s.pricingEngine.Compile(request.Expression)
	}
	if err != nil {
		return pricing.Result{}, err
	}
	if request.Currency != "" && strings.ToUpper(request.Currency) != pricing.CurrencyUSD {
		return pricing.Result{}, ErrPricingCurrencyUnsupported
	}
	return s.pricingEngine.Evaluate(rule, request.Facts)
}

func (s *Service) PublishPricingRule(ctx context.Context, actor, ruleID string, request PricingPublishRequest) (PricingRuleDetail, error) {
	detail, err := s.PricingRuleDetail(ctx, ruleID)
	if err != nil {
		return PricingRuleDetail{}, err
	}
	if request.ExpectedLockVersion != detail.Rule.LockVersion {
		return PricingRuleDetail{}, ErrPricingCASConflict
	}
	if detail.Rule.Purpose == PricingPurposeCustomerCharge && !request.AcknowledgeImpact {
		return PricingRuleDetail{}, errors.New("customer charge impact acknowledgement is required")
	}
	version, found, err := s.repo.FindPricingRuleVersion(ctx, request.DraftVersionID)
	if err != nil {
		return PricingRuleDetail{}, err
	}
	if !found || version.RuleID != ruleID || version.State != PricingVersionStateDraft {
		return PricingRuleDetail{}, ErrPricingVersionNotFound
	}
	if request.ExpressionHash != "" && request.ExpressionHash != pricing.ExpressionHash(version.Expression) {
		return PricingRuleDetail{}, errors.New("publish expression hash does not match draft")
	}
	compiled, err := s.pricingEngine.Compile(version.Expression)
	if err != nil {
		return PricingRuleDetail{}, err
	}
	validation := s.ValidatePricingRule(ctx, version.Expression, version.TestCases)
	if !validation.Valid {
		if len(validation.Errors) > 0 {
			return PricingRuleDetail{}, validation.Errors[0]
		}
		return PricingRuleDetail{}, errors.New("pricing rule test cases failed")
	}
	now := s.nowUTC()
	version.EngineVersion, version.ExpressionHash, version.Analysis = compiled.EngineVersion, compiled.ExpressionHash, compiled.Analysis
	version.PublishedBy, version.UpdatedAt = actor, now
	rule, published, applied, err := s.repo.PublishPricingRuleVersion(ctx, version, request.ExpectedLockVersion, request.ExpectedActiveVersion)
	if err != nil {
		return PricingRuleDetail{}, err
	}
	if !applied {
		return PricingRuleDetail{}, ErrPricingCASConflict
	}
	_ = s.audit(ctx, actor, "pricing_rule.publish", "pricing_rule", ruleID, "Published pricing rule version "+published.ID)
	return s.PricingRuleDetail(ctx, rule.ID)
}

func (s *Service) ActivatePricingRuleVersion(ctx context.Context, actor, ruleID, versionID string, expectedLockVersion int64) error {
	updated, applied, err := s.repo.ActivatePricingRuleVersion(ctx, ruleID, versionID, expectedLockVersion, actor, s.nowUTC())
	if err != nil {
		return err
	}
	if !applied {
		return ErrPricingCASConflict
	}
	_ = s.audit(ctx, actor, "pricing_rule.activate_version", "pricing_rule", updated.ID, "Activated pricing version "+versionID)
	return nil
}

func (s *Service) DisablePricingRule(ctx context.Context, actor, ruleID string, expectedLockVersion int64) error {
	updated, applied, err := s.repo.SetPricingRuleStatus(ctx, ruleID, PricingRuleStatusDisabled, expectedLockVersion, actor, s.nowUTC())
	if err != nil {
		return err
	}
	if !applied {
		return ErrPricingCASConflict
	}
	_ = s.audit(ctx, actor, "pricing_rule.disable", "pricing_rule", updated.ID, "Disabled pricing rule")
	return nil
}

func (s *Service) SelectPricingRule(ctx context.Context, purpose, planID, model string) (PricingSelection, bool, error) {
	rules, err := s.repo.ListPricingRules(ctx)
	if err != nil {
		return PricingSelection{}, false, err
	}
	var selected *PricingRule
	best := -1
	for index := range rules {
		rule := &rules[index]
		if rule.Status != PricingRuleStatusActive || rule.Purpose != purpose || (rule.ScopeType == PricingScopeOperatorPlan && rule.ScopeID != planID) || (rule.ScopeType == PricingScopeGlobal && rule.ScopeID != "") || (rule.Model != model && rule.Model != "*") {
			continue
		}
		score := 0
		if purpose == PricingPurposeCustomerCharge && rule.ScopeType == PricingScopeOperatorPlan {
			score += 2
		}
		if rule.Model == model {
			score++
		}
		if score > best {
			selected, best = rule, score
		}
	}
	if selected == nil || selected.ActiveVersionID == "" {
		return PricingSelection{}, false, nil
	}
	version, found, err := s.repo.FindPricingRuleVersion(ctx, selected.ActiveVersionID)
	if err != nil {
		return PricingSelection{}, false, err
	}
	if !found || version.State != PricingVersionStatePublished {
		return PricingSelection{}, false, nil
	}
	compiled, err := s.pricingEngine.CompileByHash(version.Expression, version.ExpressionHash)
	if err != nil {
		return PricingSelection{}, false, err
	}
	return PricingSelection{Rule: *selected, Version: version, Compiled: compiled, ExpressionHash: version.ExpressionHash}, true, nil
}

func (s *Service) EstimateModelUsageCostMicros(ctx context.Context, model string, inputTokens, outputTokens int) (int, bool, error) {
	selection, found, err := s.SelectPricingRule(ctx, PricingPurposeUsageCost, "", strings.TrimSpace(model))
	if err != nil || !found {
		return 0, found, err
	}
	facts := pricing.Facts{TotalInputTokens: int64(max(inputTokens, 0)), UncachedInputTokens: int64(max(inputTokens, 0)), OutputTokens: int64(max(outputTokens, 0)), Phase: pricing.PhaseEstimate}
	result, err := s.pricingEngine.Evaluate(selection.Compiled, facts)
	if err != nil {
		return 0, false, err
	}
	return int(result.AmountMicros), true, nil
}

func normalizePricingRule(request PricingRuleCreateRequest, actor string, now time.Time) (PricingRule, error) {
	purpose, scopeType, model := strings.TrimSpace(request.Purpose), strings.TrimSpace(request.ScopeType), strings.TrimSpace(request.Model)
	if purpose != PricingPurposeUsageCost && purpose != PricingPurposeCustomerCharge || scopeType != PricingScopeGlobal && scopeType != PricingScopeOperatorPlan || model == "" {
		return PricingRule{}, ErrPricingInvalidSlot
	}
	if purpose == PricingPurposeUsageCost && scopeType != PricingScopeGlobal {
		return PricingRule{}, ErrPricingInvalidSlot
	}
	scopeID := strings.TrimSpace(request.ScopeID)
	if scopeType == PricingScopeGlobal && scopeID != "" || scopeType == PricingScopeOperatorPlan && scopeID == "" {
		return PricingRule{}, ErrPricingInvalidSlot
	}
	if strings.ToUpper(strings.TrimSpace(request.Currency)) != "" && strings.ToUpper(strings.TrimSpace(request.Currency)) != pricing.CurrencyUSD {
		return PricingRule{}, ErrPricingCurrencyUnsupported
	}
	return PricingRule{ID: "prule_" + randomID(12), Name: strings.TrimSpace(request.Name), Purpose: purpose, ScopeType: scopeType, ScopeID: scopeID, Model: model, Status: PricingRuleStatusActive, LockVersion: 1, CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}, nil
}

func normalizeAuthoringMode(value string) string {
	if strings.TrimSpace(value) == PricingAuthoringRaw {
		return PricingAuthoringRaw
	}
	return PricingAuthoringVisual
}

func clonePricingTestCases(values []PricingRuleTestCase) []PricingRuleTestCase {
	return append([]PricingRuleTestCase(nil), values...)
}

func pricingError(err error) *pricing.Error {
	var value *pricing.Error
	if errors.As(err, &value) {
		return value
	}
	return &pricing.Error{Code: pricing.ErrorCompileFailed, Message: err.Error()}
}

func (s *Service) validatePricingRuleSlot(ctx context.Context, rule PricingRule) error {
	if rule.ScopeType == PricingScopeOperatorPlan {
		if s.customerPricingResolver == nil {
			return errors.New("customer pricing context resolver is unavailable")
		}
		if err := s.customerPricingResolver.ValidatePricingPlan(ctx, rule.ScopeID); err != nil {
			return err
		}
	}
	if rule.Model != "*" {
		models, err := s.repo.ListGatewayModels(ctx)
		if err != nil {
			return err
		}
		found := false
		for _, model := range models {
			if model.ModelID == rule.Model {
				found = true
				break
			}
		}
		if !found {
			return errors.New("pricing rule model does not exist")
		}
	}
	rules, err := s.repo.ListPricingRules(ctx)
	if err != nil {
		return err
	}
	for _, existing := range rules {
		if pricingRuleSameSlot(existing, rule) {
			return errors.New("pricing rule slot already exists")
		}
	}
	return nil
}
