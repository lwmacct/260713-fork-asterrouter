package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
)

func TestCustomerBillingRoutesAreSeparateFromOperator(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{AdminToken: "secret"})
	user, err := control.CreateWorkspaceUser(context.Background(), "tester", controlplane.WorkspaceUserRequest{
		Email: "relay-customer@example.test", DisplayName: "Relay Customer",
		Status: controlplane.WorkspaceUserStatusActive, Role: controlplane.RoleDeveloper,
	})
	if err != nil {
		t.Fatal(err)
	}

	billingReq := httptest.NewRequest(http.MethodGet, "/api/v1/customer/billing", nil)
	billingReq.Header.Set("Authorization", "Bearer secret")
	billingReq.Header.Set("X-Actor", user.Email)
	billingRec := httptest.NewRecorder()
	handler.ServeHTTP(billingRec, billingReq)
	if billingRec.Code != http.StatusOK {
		t.Fatalf("customer billing status=%d body=%s", billingRec.Code, billingRec.Body.String())
	}

	operatorReq := httptest.NewRequest(http.MethodGet, "/api/v1/operator/dashboard", nil)
	operatorReq.Header.Set("Authorization", "Bearer secret")
	operatorReq.Header.Set("X-Actor", user.Email)
	operatorRec := httptest.NewRecorder()
	handler.ServeHTTP(operatorRec, operatorReq)
	if operatorRec.Code != http.StatusForbidden {
		t.Fatalf("customer crossed into operator status=%d body=%s", operatorRec.Code, operatorRec.Body.String())
	}

	rechargeReq := httptest.NewRequest(http.MethodPost, "/api/v1/customer/billing/recharge-orders", bytes.NewBufferString(`{"amount_cents":1000,"payment_method":"wechat"}`))
	rechargeReq.Header.Set("Authorization", "Bearer secret")
	rechargeReq.Header.Set("X-Actor", user.Email)
	rechargeReq.Header.Set("Content-Type", "application/json")
	rechargeRec := httptest.NewRecorder()
	handler.ServeHTTP(rechargeRec, rechargeReq)
	if rechargeRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured recharge status=%d body=%s", rechargeRec.Code, rechargeRec.Body.String())
	}
}

func TestCustomerRoutesRequireRelayOperatorProfile(t *testing.T) {
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{
		Version: "test", StorageMode: "memory", EnabledProfiles: []string{"enterprise"}, DefaultProfile: "enterprise",
	})
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	user, _, err := control.RegisterWorkspaceUser(context.Background(), "enterprise-only@example.test", "long-password", "Enterprise", false)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Options{
		Config: config.Config{AdminToken: "secret"}, SettingsService: settingsService, ControlService: control,
		SystemService: system.NewService(system.Config{Version: "test", BuildType: "source"}),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/customer/billing", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Actor", user.Email)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("customer route must be hidden outside relay mode status=%d body=%s", rec.Code, rec.Body.String())
	}
}
