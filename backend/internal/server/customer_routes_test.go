package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
)

func TestCustomerNotificationRoutesPersistAndMarkRead(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "secret"})
	user, _, err := control.RegisterWorkspaceUser(context.Background(), "notify-routes@example.test", "long-password", "Notify", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := control.ChangeCurrentAccountPassword(context.Background(), user.Email, controlplane.AccountPasswordUpdateRequest{
		CurrentPassword: "long-password", NewPassword: "updated-long-password",
	}); err != nil {
		t.Fatal(err)
	}

	request := func(method, path string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("X-Actor", user.Email)
		if len(body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	settingsRec := request(http.MethodGet, "/api/v1/customer/notification-settings", nil)
	if settingsRec.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", settingsRec.Code, settingsRec.Body.String())
	}
	var settingsResponse struct {
		Data controlplane.CustomerNotificationSettings `json:"data"`
	}
	if err := json.Unmarshal(settingsRec.Body.Bytes(), &settingsResponse); err != nil {
		t.Fatal(err)
	}
	if len(settingsResponse.Data.Preferences) != 9 {
		t.Fatalf("settings preferences=%d", len(settingsResponse.Data.Preferences))
	}
	payload, err := json.Marshal(controlplane.CustomerNotificationSettingsRequest{Preferences: settingsResponse.Data.Preferences})
	if err != nil {
		t.Fatal(err)
	}
	if rec := request(http.MethodPut, "/api/v1/customer/notification-settings", payload); rec.Code != http.StatusOK {
		t.Fatalf("save settings status=%d body=%s", rec.Code, rec.Body.String())
	}

	listRec := request(http.MethodGet, "/api/v1/customer/notifications", nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("notifications status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResponse struct {
		Data controlplane.CustomerNotificationList `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResponse); err != nil {
		t.Fatal(err)
	}
	if listResponse.Data.Unread != 1 || len(listResponse.Data.Items) != 1 {
		t.Fatalf("notifications=%+v", listResponse.Data)
	}
	id := listResponse.Data.Items[0].ID
	if rec := request(http.MethodPost, "/api/v1/customer/notifications/"+id+"/read", nil); rec.Code != http.StatusOK {
		t.Fatalf("mark read status=%d body=%s", rec.Code, rec.Body.String())
	}
	listRec = request(http.MethodGet, "/api/v1/customer/notifications", nil)
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResponse); err != nil {
		t.Fatal(err)
	}
	if listResponse.Data.Unread != 0 || !listResponse.Data.Items[0].IsRead {
		t.Fatalf("notification was not marked read: %+v", listResponse.Data)
	}
}

func TestCustomerBillingRoutesAreSeparateFromOperator(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "secret"})
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
		Runtime: RuntimeConfig{AdminToken: "secret"}, SettingsService: settingsService, ControlService: control,
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
