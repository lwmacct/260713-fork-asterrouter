package controlplane

import (
	"context"
	"testing"
)

func publishTestUsagePricingRule(t *testing.T, service *Service, expression string) PricingRuleDetail {
	t.Helper()
	ctx := context.Background()
	detail, err := service.CreatePricingRule(ctx, "pricing-test", PricingRuleCreateRequest{
		Name: "Test usage pricing", Purpose: PricingPurposeUsageCost, ScopeType: PricingScopeGlobal, Model: "*",
		Currency: "USD", AuthoringMode: PricingAuthoringRaw, Expression: expression,
	})
	if err != nil {
		t.Fatalf("CreatePricingRule(): %v", err)
	}
	if detail.Draft == nil {
		t.Fatal("CreatePricingRule() did not return a draft")
	}
	detail, err = service.PublishPricingRule(ctx, "pricing-test", detail.Rule.ID, PricingPublishRequest{
		DraftVersionID: detail.Draft.ID, ExpectedLockVersion: detail.Rule.LockVersion,
		ExpectedActiveVersion: detail.Rule.ActiveVersionID, ExpressionHash: detail.Draft.ExpressionHash,
	})
	if err != nil {
		t.Fatalf("PublishPricingRule(): %v", err)
	}
	return detail
}

func testMicros(value int64) *int64 {
	return &value
}

func recordTestPricedGatewayUsage(t *testing.T, service *Service, auth GatewayAuthContext, input GatewayUsageInput, amountMicros int64) {
	t.Helper()
	ctx := context.Background()
	if err := service.RecordGatewayUsage(ctx, auth, input); err != nil {
		t.Fatalf("RecordGatewayUsage(): %v", err)
	}
	records, err := service.repo.QueryUsageRecords(ctx, UsageQuery{APIKeyID: auth.APIKey.ID, Limit: 1})
	if err != nil || len(records) != 1 {
		t.Fatalf("QueryUsageRecords() records=%+v err=%v", records, err)
	}
	record := records[0]
	record.UsageCostMicros = testMicros(amountMicros)
	record.UsageCostCurrency = "USD"
	record.PricingStatus = "priced"
	if err := service.repo.SaveUsageRecord(ctx, record); err != nil {
		t.Fatalf("SaveUsageRecord(): %v", err)
	}
	if auth.effectiveMonthlyBudgetMicros() > 0 {
		if err := service.syncAPIKeyBudgetAlertForAuth(ctx, auth, record.CreatedAt); err != nil {
			t.Fatalf("syncAPIKeyBudgetAlertForAuth(): %v", err)
		}
	}
}
