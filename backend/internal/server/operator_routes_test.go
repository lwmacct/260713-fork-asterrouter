package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	operatorcore "github.com/astercloud/asterrouter/backend/internal/operator"
)

func TestOperatorBusinessLifecycle(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})
	group := operatorPost[operatorcore.CustomerGroup](t, handler, "/api/v1/operator/customer-groups", `{"name":"Standard","status":"active"}`)
	plan := operatorPost[operatorcore.Plan](t, handler, "/api/v1/operator/plans", `{"name":"Starter","included_tokens":1000000,"monthly_limit_micros":1000,"status":"active"}`)
	customerBody := `{"name":"Customer A","email":"a@example.com","group_id":"` + group.ID + `","plan_id":"` + plan.ID + `","credit_micros":500,"status":"active"}`
	customer := operatorPost[operatorcore.Customer](t, handler, "/api/v1/operator/customers", customerBody)
	entry := operatorPost[operatorcore.BalanceEntry](t, handler, "/api/v1/operator/balance-entries", `{"customer_id":"`+customer.ID+`","kind":"allocation_increase","amount_micros":2500,"note":"initial allocation"}`)
	if entry.BalanceAfter != 2500 {
		t.Fatalf("balance = %+v", entry)
	}
	key := operatorPost[controlplane.APIKeyCreateResponse](t, handler, "/api/v1/operator/customers/"+customer.ID+"/keys", `{"name":"Customer key","model_allowlist":["gpt-5"],"qps_limit":5}`)
	if key.Record.CustomerID != customer.ID || key.Record.KeyType != controlplane.APIKeyTypeCustomer || key.Key == "" {
		t.Fatalf("customer key = %+v", key)
	}
	operatorPost[controlplane.PricingRuleDetail](t, handler, "/api/v1/operator/pricing-rules", `{"name":"GPT price","purpose":"customer_charge","scope_type":"operator_plan","scope_id":"`+plan.ID+`","model":"*","currency":"USD","authoring_mode":"raw","expression":"v1: fixed_line(\"charge\", \"request\", 0)","test_cases":[]}`)
	operatorPost[operatorcore.RiskRule](t, handler, "/api/v1/operator/risk-rules", `{"name":"Burst traffic","rule_type":"rpm","threshold":100,"window_minutes":5,"action":"review","status":"active"}`)
	operatorPost[operatorcore.Notice](t, handler, "/api/v1/operator/notices", `{"title":"Maintenance","content":"Scheduled maintenance","audience":"all","status":"published"}`)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/operator/dashboard", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard status=%d body=%s", rec.Code, rec.Body.String())
	}
	var dashboard struct {
		Data operatorcore.Dashboard `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &dashboard)
	if dashboard.Data.Customers != 1 || dashboard.Data.Plans != 1 || dashboard.Data.BalanceMicros != 2500 || dashboard.Data.RiskRules != 1 || dashboard.Data.PublishedNotice != 1 {
		t.Fatalf("dashboard=%+v", dashboard.Data)
	}
}

func operatorPost[T any](t *testing.T, handler http.Handler, path string, body string) T {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s status=%d body=%s", path, rec.Code, rec.Body.String())
	}
	var response struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return response.Data
}
