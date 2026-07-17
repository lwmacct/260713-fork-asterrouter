package controlplane

import (
	"context"
	"errors"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/pricing"
)

type pricingContextResolverStub struct{}

func (pricingContextResolverStub) ResolveCustomerPricingContext(context.Context, string) (CustomerPricingContext, error) {
	return CustomerPricingContext{}, nil
}

func (pricingContextResolverStub) ValidatePricingPlan(_ context.Context, planID string) error {
	if planID == "plan-a" {
		return nil
	}
	return errors.New("operator plan not found")
}

func TestPricingRuleLifecycleValidationAndCAS(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	detail, err := svc.CreatePricingRule(ctx, "tester", PricingRuleCreateRequest{
		Name: "Token pricing", Purpose: PricingPurposeUsageCost, ScopeType: PricingScopeGlobal, Model: "*",
		Currency: pricing.CurrencyUSD, AuthoringMode: PricingAuthoringRaw,
		Expression: `v1: token_line("input", uncached_input_tokens, 1000000)`,
		TestCases: []PricingRuleTestCase{{
			Name: "one million", Facts: pricing.Facts{UncachedInputTokens: 1_000_000, AvailableFacts: map[string]bool{"uncached_input_tokens": true}},
			ExpectedAmountMicros: int64Pointer(1_000_000),
		}},
	})
	if err != nil || detail.Draft == nil {
		t.Fatalf("CreatePricingRule() detail=%+v err=%v", detail, err)
	}
	valid := svc.ValidatePricingRule(ctx, detail.Draft.Expression, detail.Draft.TestCases)
	if !valid.Valid || valid.ExpressionHash != detail.Draft.ExpressionHash || len(valid.TestResults) != 1 || !valid.TestResults[0].Passed {
		t.Fatalf("ValidatePricingRule()=%+v", valid)
	}
	invalid := svc.ValidatePricingRule(ctx, `v1: uncached_input_tokens * 3`, nil)
	if invalid.Valid || len(invalid.Errors) != 1 || invalid.Errors[0].Code != pricing.ErrorForbiddenAST {
		t.Fatalf("invalid validation=%+v", invalid)
	}

	if _, err := svc.UpdatePricingRuleDraft(ctx, "tester", detail.Rule.ID, PricingDraftUpdateRequest{
		ExpectedLockVersion: detail.Rule.LockVersion + 1, Name: detail.Rule.Name, Currency: pricing.CurrencyUSD,
		AuthoringMode: PricingAuthoringRaw, Expression: detail.Draft.Expression,
	}); !errors.Is(err, ErrPricingCASConflict) {
		t.Fatalf("stale draft update error=%v", err)
	}
	if _, err := svc.PublishPricingRule(ctx, "tester", detail.Rule.ID, PricingPublishRequest{
		DraftVersionID: detail.Draft.ID, ExpectedLockVersion: detail.Rule.LockVersion,
		ExpressionHash: "not-the-draft-hash",
	}); err == nil {
		t.Fatal("publish accepted mismatched expression hash")
	}

	published, err := svc.PublishPricingRule(ctx, "tester", detail.Rule.ID, PricingPublishRequest{
		DraftVersionID: detail.Draft.ID, ExpectedLockVersion: detail.Rule.LockVersion,
		ExpectedActiveVersion: detail.Rule.ActiveVersionID, ExpressionHash: detail.Draft.ExpressionHash,
	})
	if err != nil || published.ActiveVersion == nil || published.Draft != nil || published.ActiveVersion.State != PricingVersionStatePublished {
		t.Fatalf("PublishPricingRule() detail=%+v err=%v", published, err)
	}
	activeID, activeExpression := published.ActiveVersion.ID, published.ActiveVersion.Expression
	published.ActiveVersion.Expression = "mutated outside repository"
	_, stored, err := svc.PricingRuleVersionDetail(ctx, activeID)
	if err != nil || stored.Expression != activeExpression {
		t.Fatalf("published version mutated: version=%+v err=%v", stored, err)
	}

	updated, err := svc.UpdatePricingRuleDraft(ctx, "tester", published.Rule.ID, PricingDraftUpdateRequest{
		ExpectedLockVersion: published.Rule.LockVersion, Name: "Token pricing v2", Currency: pricing.CurrencyUSD,
		AuthoringMode: PricingAuthoringRaw, Expression: `v1: token_line("input", uncached_input_tokens, 2000000)`,
	})
	if err != nil || updated.Draft == nil || updated.Draft.ID == activeID || updated.ActiveVersion == nil || updated.ActiveVersion.Expression != activeExpression {
		t.Fatalf("UpdatePricingRuleDraft() detail=%+v err=%v", updated, err)
	}
}

func TestPricingRuleSelectionPriority(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	svc.SetCustomerPricingContextResolver(pricingContextResolverStub{})
	if _, err := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{ModelID: "model-a", Name: "Model A", Status: GatewayModelStatusActive}); err != nil {
		t.Fatal(err)
	}

	createPublishedPricingRule(t, svc, PricingRuleCreateRequest{Name: "global wildcard", Purpose: PricingPurposeCustomerCharge, ScopeType: PricingScopeGlobal, Model: "*", Currency: pricing.CurrencyUSD, Expression: `v1: fixed_line("request", "request", 1)`})
	createPublishedPricingRule(t, svc, PricingRuleCreateRequest{Name: "global exact", Purpose: PricingPurposeCustomerCharge, ScopeType: PricingScopeGlobal, Model: "model-a", Currency: pricing.CurrencyUSD, Expression: `v1: fixed_line("request", "request", 2)`})
	createPublishedPricingRule(t, svc, PricingRuleCreateRequest{Name: "plan wildcard", Purpose: PricingPurposeCustomerCharge, ScopeType: PricingScopeOperatorPlan, ScopeID: "plan-a", Model: "*", Currency: pricing.CurrencyUSD, Expression: `v1: fixed_line("request", "request", 3)`})
	createPublishedPricingRule(t, svc, PricingRuleCreateRequest{Name: "plan exact", Purpose: PricingPurposeCustomerCharge, ScopeType: PricingScopeOperatorPlan, ScopeID: "plan-a", Model: "model-a", Currency: pricing.CurrencyUSD, Expression: `v1: fixed_line("request", "request", 4)`})

	tests := []struct {
		name, plan, model string
		want              int64
	}{
		{name: "plan exact", plan: "plan-a", model: "model-a", want: 4},
		{name: "plan wildcard", plan: "plan-a", model: "model-b", want: 3},
		{name: "global exact", plan: "plan-b", model: "model-a", want: 2},
		{name: "global wildcard", plan: "plan-b", model: "model-b", want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selection, found, err := svc.SelectPricingRule(ctx, PricingPurposeCustomerCharge, test.plan, test.model)
			if err != nil || !found {
				t.Fatalf("SelectPricingRule() found=%t err=%v", found, err)
			}
			result, err := svc.pricingEngine.Evaluate(selection.Compiled, pricing.Facts{})
			if err != nil || result.AmountMicros != test.want {
				t.Fatalf("Evaluate() result=%+v err=%v", result, err)
			}
		})
	}
}

func TestPricingRuleRejectsUnsupportedCurrencyAndCustomerPublishWithoutAcknowledgement(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	if _, err := svc.CreatePricingRule(ctx, "tester", PricingRuleCreateRequest{
		Name: "CNY", Purpose: PricingPurposeUsageCost, ScopeType: PricingScopeGlobal, Model: "*", Currency: "CNY", Expression: `v1: fixed_line("request", "request", 1)`,
	}); !errors.Is(err, ErrPricingCurrencyUnsupported) {
		t.Fatalf("non-USD create error=%v", err)
	}
	detail, err := svc.CreatePricingRule(ctx, "tester", PricingRuleCreateRequest{
		Name: "Customer", Purpose: PricingPurposeCustomerCharge, ScopeType: PricingScopeGlobal, Model: "*", Currency: pricing.CurrencyUSD, Expression: `v1: fixed_line("request", "request", 1)`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishPricingRule(ctx, "tester", detail.Rule.ID, PricingPublishRequest{
		DraftVersionID: detail.Draft.ID, ExpectedLockVersion: detail.Rule.LockVersion, ExpressionHash: detail.Draft.ExpressionHash,
	}); err == nil {
		t.Fatal("customer charge publish did not require impact acknowledgement")
	}
}

func createPublishedPricingRule(t *testing.T, svc *Service, request PricingRuleCreateRequest) PricingRuleDetail {
	t.Helper()
	detail, err := svc.CreatePricingRule(context.Background(), "tester", request)
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.PublishPricingRule(context.Background(), "tester", detail.Rule.ID, PricingPublishRequest{
		DraftVersionID: detail.Draft.ID, ExpectedLockVersion: detail.Rule.LockVersion,
		ExpressionHash: detail.Draft.ExpressionHash, AcknowledgeImpact: request.Purpose == PricingPurposeCustomerCharge,
	})
	if err != nil {
		t.Fatal(err)
	}
	return detail
}
