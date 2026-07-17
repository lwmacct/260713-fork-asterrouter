package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/gin-gonic/gin"
)

func TestPricingRuleSurfacesDoNotDiscloseForeignPurposes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	repo := controlplane.NewMemoryRepository()
	service := controlplane.NewService(repo, "/v1")
	usage := createPublishedRoutePricingRule(t, service, controlplane.PricingPurposeUsageCost)
	customer := createPublishedRoutePricingRule(t, service, controlplane.PricingPurposeCustomerCharge)

	amount := int64(1)
	for _, item := range []struct {
		id     string
		detail controlplane.PricingRuleDetail
	}{
		{id: "usage-evaluation", detail: usage},
		{id: "customer-evaluation", detail: customer},
	} {
		if err := repo.SavePricingEvaluation(ctx, controlplane.PricingEvaluation{
			ID: item.id, Purpose: item.detail.Rule.Purpose, Phase: "estimate", OperationID: item.id,
			PricingRuleID: item.detail.Rule.ID, PricingRuleVersionID: item.detail.ActiveVersion.ID,
			EngineVersion: 1, ExpressionHash: item.detail.ActiveVersion.ExpressionHash,
			FactsHash: item.detail.ActiveVersion.ExpressionHash, AmountMicros: &amount, Currency: "USD", Status: controlplane.PricingEvaluationStatusSuccess,
		}); err != nil {
			t.Fatal(err)
		}
	}

	router := gin.New()
	registerPricingRuleRoutes(router.Group("/admin"), service, PricingSurfaceAdmin)
	registerPricingRuleRoutes(router.Group("/platform"), service, PricingSurfacePlatform)
	registerPricingRuleRoutes(router.Group("/operator"), service, PricingSurfaceOperator)

	tests := []struct {
		path string
		want int
	}{
		{path: "/platform/pricing-rules/" + usage.Rule.ID, want: http.StatusOK},
		{path: "/platform/pricing-rules/" + customer.Rule.ID, want: http.StatusNotFound},
		{path: "/operator/pricing-rules/" + customer.Rule.ID, want: http.StatusOK},
		{path: "/operator/pricing-rules/" + usage.Rule.ID, want: http.StatusNotFound},
		{path: "/platform/pricing-evaluations/usage-evaluation", want: http.StatusOK},
		{path: "/platform/pricing-evaluations/customer-evaluation", want: http.StatusNotFound},
		{path: "/operator/pricing-evaluations/customer-evaluation", want: http.StatusOK},
		{path: "/operator/pricing-evaluations/usage-evaluation", want: http.StatusNotFound},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
		if recorder.Code != test.want {
			t.Errorf("GET %s status=%d want=%d body=%s", test.path, recorder.Code, test.want, recorder.Body.String())
		}
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/operator/pricing-rules", bytes.NewBufferString(`{"name":"forbidden","purpose":"usage_cost","scope_type":"global","scope_id":"","model":"*","currency":"USD","authoring_mode":"raw","expression":"v1: fixed_line(\"request\", \"request\", 1)","test_cases":[]}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("operator foreign-purpose create status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func createPublishedRoutePricingRule(t *testing.T, service *controlplane.Service, purpose string) controlplane.PricingRuleDetail {
	t.Helper()
	detail, err := service.CreatePricingRule(context.Background(), "route-test", controlplane.PricingRuleCreateRequest{
		Name: purpose, Purpose: purpose, ScopeType: controlplane.PricingScopeGlobal, Model: "*", Currency: "USD",
		AuthoringMode: controlplane.PricingAuthoringRaw, Expression: `v1: fixed_line("request", "request", 1)`,
	})
	if err != nil {
		t.Fatal(err)
	}
	detail, err = service.PublishPricingRule(context.Background(), "route-test", detail.Rule.ID, controlplane.PricingPublishRequest{
		DraftVersionID: detail.Draft.ID, ExpectedLockVersion: detail.Rule.LockVersion,
		ExpressionHash: detail.Draft.ExpressionHash, AcknowledgeImpact: purpose == controlplane.PricingPurposeCustomerCharge,
	})
	if err != nil {
		t.Fatal(err)
	}
	return detail
}
