package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/settings"
)

func TestGatewayAcceptsSignedExternalAuthContextAndRejectsItFromControlPlane(t *testing.T) {
	handler, control := newPlatformExternalAuthHandler(t)
	ctx := context.Background()
	if _, err := control.CreateGatewayModel(ctx, "tester", controlplane.GatewayModelRequest{ModelID: "model-a", Name: "Model A", Status: controlplane.GatewayModelStatusActive}); err != nil {
		t.Fatal(err)
	}
	tenant, err := control.CreatePlatformTenant(ctx, "operator", controlplane.PlatformTenantRequest{Name: "Gateway product", Slug: "gateway-product"})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := control.CreateGatewayPrincipal(ctx, "operator", controlplane.GatewayPrincipalRequest{TenantID: tenant.ID, Name: "Gateway backend", PrincipalType: controlplane.GatewayPrincipalTypeIntegration})
	if err != nil {
		t.Fatal(err)
	}
	integration, err := control.CreateExternalAuthIntegration(ctx, "operator", controlplane.ExternalAuthIntegrationRequest{
		TenantID: tenant.ID, GatewayPrincipalID: principal.ID, Name: "Gateway integration", KeyID: "gateway-v1", Audience: "https://gateway.example/v1",
		ModelAllowlist: []string{"model-a"}, QPSLimit: 5, MonthlyTokenLimit: 500, MaxTTLSeconds: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	contextToken := signedGatewayContext(t, controlplane.ExternalAuthContextClaims{
		Version: 1, IntegrationID: integration.Record.ID, KeyID: integration.Record.KeyID, TenantID: tenant.ID,
		SubjectReference: "opaque-subject", Audience: integration.Record.Audience,
		IssuedAt: time.Now().Add(-time.Minute).Unix(), ExpiresAt: time.Now().Add(time.Minute).Unix(),
		ModelAllowlist: []string{"model-a"}, QPSLimit: 2, MonthlyTokenLimit: 100,
	}, integration.Secret)

	modelsReq := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	modelsReq.Header.Set("Authorization", "Aster-Context "+contextToken)
	modelsRec := httptest.NewRecorder()
	handler.ServeHTTP(modelsRec, modelsReq)
	if modelsRec.Code != http.StatusOK || !bytes.Contains(modelsRec.Body.Bytes(), []byte("model-a")) {
		t.Fatalf("models status=%d body=%s", modelsRec.Code, modelsRec.Body.String())
	}

	chatReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"model-a","messages":[{"role":"user","content":"ping"}]}`))
	chatReq.Header.Set("Content-Type", "application/json")
	chatReq.Header.Set("Authorization", "Aster-Context "+contextToken)
	chatRec := httptest.NewRecorder()
	handler.ServeHTTP(chatRec, chatReq)
	if chatRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("chat status=%d body=%s", chatRec.Code, chatRec.Body.String())
	}
	usage, err := control.UsageReportQuery(ctx, controlplane.UsageQuery{ProfileScope: controlplane.ProfileScopePlatform, ExternalAuthIntegrationID: integration.Record.ID})
	if err != nil || len(usage.Recent) != 1 || usage.Recent[0].ExternalSubjectReference != "opaque-subject" {
		t.Fatalf("gateway evidence usage=%+v err=%v", usage, err)
	}

	controlReq := httptest.NewRequest(http.MethodGet, "/api/v1/platform/dashboard", nil)
	controlReq.Header.Set("Authorization", "Aster-Context "+contextToken)
	controlRec := httptest.NewRecorder()
	handler.ServeHTTP(controlRec, controlReq)
	if controlRec.Code != http.StatusUnauthorized {
		t.Fatalf("delegated context reached control plane status=%d body=%s", controlRec.Code, controlRec.Body.String())
	}
}

func TestPlatformExternalAuthIntegrationRoutesReturnSecretOnlyOnce(t *testing.T) {
	handler, control := newPlatformExternalAuthHandler(t)
	ctx := context.Background()
	tenant, err := control.CreatePlatformTenant(ctx, "operator", controlplane.PlatformTenantRequest{Name: "Route product", Slug: "route-product"})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := control.CreateGatewayPrincipal(ctx, "operator", controlplane.GatewayPrincipalRequest{TenantID: tenant.ID, Name: "Route backend", PrincipalType: controlplane.GatewayPrincipalTypeIntegration})
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"tenant_id":"` + tenant.ID + `","gateway_principal_id":"` + principal.ID + `","name":"Route integration","key_id":"route-v1","audience":"https://gateway.example/v1","model_allowlist":["model-a"],"qps_limit":1,"monthly_token_limit":10,"max_ttl_seconds":300}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/platform/external-auth-integrations", bytes.NewBufferString(payload))
	createReq.Header.Set("Authorization", "Bearer secret")
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var createResponse struct {
		Data controlplane.ExternalAuthIntegrationCreateResponse `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResponse); err != nil || createResponse.Data.Secret == "" || createResponse.Data.Record.SecretCiphertext != "" {
		t.Fatalf("create response=%+v err=%v", createResponse, err)
	}
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/platform/external-auth-integrations", nil)
	listReq.Header.Set("Authorization", "Bearer secret")
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || bytes.Contains(listRec.Body.Bytes(), []byte(createResponse.Data.Secret)) {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	rotateReq := httptest.NewRequest(http.MethodPost, "/api/v1/platform/external-auth-integrations/"+createResponse.Data.Record.ID+"/rotate-secret", nil)
	rotateReq.Header.Set("Authorization", "Bearer secret")
	rotateRec := httptest.NewRecorder()
	handler.ServeHTTP(rotateRec, rotateReq)
	var rotateResponse struct {
		Data controlplane.ExternalAuthIntegrationCreateResponse `json:"data"`
	}
	if rotateRec.Code != http.StatusOK || json.Unmarshal(rotateRec.Body.Bytes(), &rotateResponse) != nil || rotateResponse.Data.Secret == "" || rotateResponse.Data.Secret == createResponse.Data.Secret {
		t.Fatalf("rotate status=%d body=%s", rotateRec.Code, rotateRec.Body.String())
	}
}

func TestPlatformUsageSinkRoutesMaskSecretsAndRejectCrossSinkRequeue(t *testing.T) {
	repo := controlplane.NewMemoryRepository()
	control := controlplane.NewService(repo, "/v1", "usage-route-test-secret")
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{
		Version: "test", StorageMode: "memory", EnabledProfiles: []string{"platform"}, DefaultProfile: "platform",
	})
	handler := New(Options{Runtime: RuntimeConfig{AdminToken: "secret"}, SettingsService: settingsService, ControlService: control})
	ctx := context.Background()
	tenant, err := control.CreatePlatformTenant(ctx, "operator", controlplane.PlatformTenantRequest{Name: "Usage routes", Slug: "usage-routes"})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := control.CreateGatewayPrincipal(ctx, "operator", controlplane.GatewayPrincipalRequest{TenantID: tenant.ID, Name: "Usage backend", PrincipalType: controlplane.GatewayPrincipalTypeIntegration})
	if err != nil {
		t.Fatal(err)
	}
	integration, err := control.CreateExternalAuthIntegration(ctx, "operator", controlplane.ExternalAuthIntegrationRequest{
		TenantID: tenant.ID, GatewayPrincipalID: principal.ID, Name: "Usage routes integration", KeyID: "usage-routes-v1", Audience: "https://gateway.example/v1",
		ModelAllowlist: []string{"model-a"}, QPSLimit: 5, MonthlyTokenLimit: 500, MaxTTLSeconds: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, payload string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(payload))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	create := func(name string) controlplane.PlatformUsageSinkCreateResponse {
		payload := `{"tenant_id":"` + tenant.ID + `","external_auth_integration_id":"` + integration.Record.ID + `","name":"` + name + `","endpoint_url":"https://billing.example/events","max_attempts":1}`
		rec := request(http.MethodPost, "/api/v1/platform/usage-sinks", payload)
		if rec.Code != http.StatusOK {
			t.Fatalf("create usage sink status=%d body=%s", rec.Code, rec.Body.String())
		}
		var response struct {
			Data controlplane.PlatformUsageSinkCreateResponse `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.Data.SigningSecret == "" || response.Data.Record.SigningSecretCiphertext != "" || response.Data.Record.EndpointURLCiphertext != "" {
			t.Fatalf("create usage sink response=%+v err=%v", response, err)
		}
		return response.Data
	}
	first := create("Billing A")
	second := create("Billing B")
	list := request(http.MethodGet, "/api/v1/platform/usage-sinks", "")
	if list.Code != http.StatusOK || bytes.Contains(list.Body.Bytes(), []byte(first.SigningSecret)) || bytes.Contains(list.Body.Bytes(), []byte(second.SigningSecret)) {
		t.Fatalf("list usage sinks status=%d body=%s", list.Code, list.Body.String())
	}

	auth := controlplane.GatewayAuthContext{
		APIKey:                   controlplane.APIKeyRecord{ID: "synthetic-subject", Fingerprint: "subject-fp", ProfileScope: controlplane.ProfileScopePlatform, PlatformTenantID: tenant.ID, GatewayPrincipalID: principal.ID},
		PlatformTenant:           &tenant,
		GatewayPrincipal:         &principal,
		ExternalAuthIntegration:  &integration.Record,
		ExternalSubjectReference: "opaque-subject",
	}
	if err := control.RecordGatewayUsage(ctx, auth, controlplane.GatewayUsageInput{Model: "model-a", Status: "forwarded", InputTokens: 2}); err != nil {
		t.Fatal(err)
	}
	events, err := control.ListPlatformUsageDeliveryEvents(ctx, controlplane.PlatformUsageDeliveryQuery{SinkID: first.Record.ID})
	if err != nil || len(events) != 1 {
		t.Fatalf("first sink events=%+v err=%v", events, err)
	}
	claimed, err := repo.ClaimDuePlatformUsageDeliveryEvents(ctx, time.Now().UTC(), time.Now().UTC().Add(time.Second), "route-test-lease", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed events=%+v", claimed)
	}
	if err := repo.ReschedulePlatformUsageDeliveryEvent(ctx, events[0].ID, "route-test-lease", time.Now().UTC(), http.StatusBadGateway, "billing unavailable", true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	cross := request(http.MethodPost, "/api/v1/platform/usage-sinks/"+second.Record.ID+"/deliveries/"+events[0].ID+"/requeue", "")
	if cross.Code != http.StatusNotFound {
		t.Fatalf("cross sink requeue status=%d body=%s", cross.Code, cross.Body.String())
	}
	valid := request(http.MethodPost, "/api/v1/platform/usage-sinks/"+first.Record.ID+"/deliveries/"+events[0].ID+"/requeue", "")
	if valid.Code != http.StatusOK {
		t.Fatalf("valid requeue status=%d body=%s", valid.Code, valid.Body.String())
	}
}

func newPlatformExternalAuthHandler(t *testing.T) (http.Handler, *controlplane.Service) {
	t.Helper()
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{
		Version: "test", StorageMode: "memory", EnabledProfiles: []string{"platform"}, DefaultProfile: "platform",
	})
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	return New(Options{Runtime: RuntimeConfig{AdminToken: "secret"}, SettingsService: settingsService, ControlService: control}), control
}

func signedGatewayContext(t testing.TB, claims controlplane.ExternalAuthContextClaims, secret string) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
