package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
)

func TestPlatformAPIRequiresEnabledProfileAndExplicitSurfaceBinding(t *testing.T) {
	t.Run("disabled profile is not exposed", func(t *testing.T) {
		handler := newTestHandler(t, RuntimeConfig{AdminToken: "secret"})
		req := httptest.NewRequest(http.MethodGet, "/api/v1/platform/dashboard", nil)
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("disabled platform status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{
		Version: "test", StorageMode: "memory", EnabledProfiles: []string{"platform"}, DefaultProfile: "platform",
	})
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if err := control.EnsureSeedData(context.Background()); err != nil {
		t.Fatal(err)
	}
	user, err := control.CreateWorkspaceUser(context.Background(), "tester", controlplane.WorkspaceUserRequest{
		Email: "platform-operator@example.test", Status: controlplane.WorkspaceUserStatusActive, Role: controlplane.RoleDeveloper,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Options{
		Runtime:         RuntimeConfig{AdminToken: "secret"},
		SettingsService: settingsService,
		ControlService:  control,
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	})
	request := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/platform/dashboard", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Actor", user.Email)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := request("secret"); rec.Code != http.StatusForbidden {
		t.Fatalf("unbound platform operator status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := control.CreateRoleBinding(context.Background(), "tester", controlplane.RoleBindingRequest{
		UserID: user.ID, Role: controlplane.RolePlatformAdmin, ScopeType: controlplane.RoleScopeSurface, ScopeID: controlplane.SurfacePlatform,
	}); err != nil {
		t.Fatal(err)
	}
	if rec := request("secret"); rec.Code != http.StatusOK {
		t.Fatalf("bound platform operator status=%d body=%s", rec.Code, rec.Body.String())
	}
	key, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{Name: "gateway-only", ModelAllowlist: []string{"model"}})
	if err != nil {
		t.Fatal(err)
	}
	if rec := request(key.Key); rec.Code != http.StatusUnauthorized {
		t.Fatalf("gateway key reached platform API status=%d body=%s", rec.Code, rec.Body.String())
	}
	adminReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil)
	adminReq.Header.Set("Authorization", "Bearer secret")
	adminRec := httptest.NewRecorder()
	handler.ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusNotFound {
		t.Fatalf("enterprise API was exposed from a platform instance status=%d body=%s", adminRec.Code, adminRec.Body.String())
	}
}

func TestSystemProfileChangesRequireSystemAdministrator(t *testing.T) {
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{
		Version: "test", StorageMode: "memory", EnabledProfiles: []string{"enterprise"}, DefaultProfile: "enterprise",
	})
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	user, err := control.CreateWorkspaceUser(context.Background(), "tester", controlplane.WorkspaceUserRequest{
		Email: "platform-admin@example.test", Status: controlplane.WorkspaceUserStatusActive, Role: controlplane.RolePlatformAdmin,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Options{
		Runtime:         RuntimeConfig{AdminToken: "secret"},
		SettingsService: settingsService,
		ControlService:  control,
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	})

	denied := httptest.NewRequest(http.MethodPut, "/api/v1/system/profiles", bytes.NewBufferString(`{"enabled_profiles":["enterprise","platform"],"default_profile":"enterprise"}`))
	denied.Header.Set("Authorization", "Bearer secret")
	denied.Header.Set("X-Actor", user.Email)
	denied.Header.Set("Content-Type", "application/json")
	deniedRec := httptest.NewRecorder()
	handler.ServeHTTP(deniedRec, denied)
	if deniedRec.Code != http.StatusForbidden {
		t.Fatalf("non-system administrator status=%d body=%s", deniedRec.Code, deniedRec.Body.String())
	}

	allowed := httptest.NewRequest(http.MethodPut, "/api/v1/system/profiles", bytes.NewBufferString(`{"enabled_profiles":["platform"],"default_profile":"platform"}`))
	allowed.Header.Set("Authorization", "Bearer secret")
	allowed.Header.Set("Content-Type", "application/json")
	allowedRec := httptest.NewRecorder()
	handler.ServeHTTP(allowedRec, allowed)
	if allowedRec.Code != http.StatusOK {
		t.Fatalf("system administrator mutation status=%d body=%s", allowedRec.Code, allowedRec.Body.String())
	}
	current, err := settingsService.Admin(context.Background())
	if err != nil || current.DefaultProfile != "platform" || len(current.EnabledProfiles) != 1 || current.EnabledProfiles[0] != "platform" {
		t.Fatalf("installed profile was not switched=%+v err=%v", current.PublicSettings, err)
	}
	for _, path := range []string{"/api/v1/platform/settings", "/api/v1/platform/settings/email-templates/defaults"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("platform settings path=%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestProfileBundleChangesAreDeniedAcrossSurfaceSettings(t *testing.T) {
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{
		Version: "test", StorageMode: "memory", DemoMode: true, EnabledProfiles: []string{"personal", "relay_operator", "enterprise"}, DefaultProfile: "enterprise",
	})
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	user, err := control.CreateWorkspaceUser(context.Background(), "tester", controlplane.WorkspaceUserRequest{
		Email: "surface-settings@example.test", Status: controlplane.WorkspaceUserStatusActive, Role: controlplane.RolePlatformAdmin,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, surface := range []string{controlplane.SurfacePersonal, controlplane.SurfaceRelayOperator} {
		if _, err := control.CreateRoleBinding(context.Background(), "tester", controlplane.RoleBindingRequest{
			UserID: user.ID, Role: controlplane.RolePlatformAdmin, ScopeType: controlplane.RoleScopeSurface, ScopeID: surface,
		}); err != nil {
			t.Fatal(err)
		}
	}
	handler := New(Options{
		Runtime:         RuntimeConfig{AdminToken: "secret"},
		SettingsService: settingsService,
		ControlService:  control,
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	})
	current, err := settingsService.Admin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	current.EnabledProfiles = []string{"personal", "relay_operator", "enterprise", "platform"}
	body, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/v1/admin/settings", "/api/v1/console/settings", "/api/v1/operator/settings"} {
		req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("X-Actor", user.Email)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	updated, err := settingsService.Admin(context.Background())
	if err != nil || len(updated.EnabledProfiles) != 3 {
		t.Fatalf("profile bundle mutated after denied writes: %+v err=%v", updated.PublicSettings, err)
	}
}

func TestPlatformAPIKeysAllowOnlyWorkspaceOrServiceOwnership(t *testing.T) {
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{
		Version: "test", StorageMode: "memory", EnabledProfiles: []string{"platform"}, DefaultProfile: "platform",
	})
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	handler := New(Options{
		Runtime:         RuntimeConfig{AdminToken: "secret"},
		SettingsService: settingsService,
		ControlService:  control,
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	})
	request := func(payload string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/platform/api-keys", bytes.NewBufferString(payload))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := request(`{"name":"unbound","key_type":"service","model_allowlist":["model"],"qps_limit":1,"monthly_token_limit":1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unbound platform key status=%d body=%s", rec.Code, rec.Body.String())
	}
	valid := request(`{"name":"platform-service","key_type":"service","platform_tenant_id":"ptn_default","gateway_principal_id":"gpr_default_service","model_allowlist":["model"],"qps_limit":1,"monthly_token_limit":1}`)
	if valid.Code != http.StatusOK {
		t.Fatalf("platform service key status=%d body=%s", valid.Code, valid.Body.String())
	}
	for _, payload := range []string{
		`{"name":"customer","key_type":"customer","customer_id":"customer-1","model_allowlist":["model"],"qps_limit":1,"monthly_token_limit":1}`,
		`{"name":"user","key_type":"user","owner_user_id":"user-1","model_allowlist":["model"],"qps_limit":1,"monthly_token_limit":1}`,
	} {
		if rec := request(payload); rec.Code != http.StatusBadRequest {
			t.Fatalf("non-platform key status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	keys, err := control.ListAPIKeys(context.Background())
	if err != nil || len(keys) != 1 || keys[0].KeyType != controlplane.APIKeyTypeService || keys[0].ProfileScope != controlplane.ProfileScopePlatform || keys[0].PlatformTenantID != "ptn_default" || keys[0].GatewayPrincipalID != "gpr_default_service" {
		t.Fatalf("stored platform keys=%+v err=%v", keys, err)
	}
}

func TestPlatformOperationsAreProfileScopedAndDoNotDiscloseForeignResources(t *testing.T) {
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{
		Version: "test", StorageMode: "memory", EnabledProfiles: []string{"platform"}, DefaultProfile: "platform",
	})
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if err := control.SetArtifactStore(controlplane.NewMemoryArtifactStore()); err != nil {
		t.Fatal(err)
	}
	model, err := control.CreateGatewayModel(context.Background(), "tester", controlplane.GatewayModelRequest{
		ModelID: "platform-operations-model", Name: "Platform operations model", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	platformJob, platformArtifact := createScopedOperationsFixture(t, control, model.ModelID, "platform", controlplane.ProfileScopePlatform)
	foreignJob, foreignArtifact := createScopedOperationsFixture(t, control, model.ModelID, "enterprise", "enterprise")
	handler := New(Options{
		Runtime:         RuntimeConfig{AdminToken: "secret"},
		SettingsService: settingsService,
		ControlService:  control,
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	})
	request := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	jobs := request(http.MethodGet, "/api/v1/platform/ai-jobs?profile_scope=enterprise")
	var jobsResponse struct {
		Data []controlplane.AIJobAdminRecord `json:"data"`
	}
	if err := json.Unmarshal(jobs.Body.Bytes(), &jobsResponse); err != nil || jobs.Code != http.StatusOK || len(jobsResponse.Data) != 1 || jobsResponse.Data[0].ID != platformJob.ID {
		t.Fatalf("platform jobs status=%d data=%+v body=%s err=%v", jobs.Code, jobsResponse.Data, jobs.Body.String(), err)
	}
	jobSummary := request(http.MethodGet, "/api/v1/platform/ai-jobs/summary?profile_scope=enterprise")
	var jobSummaryResponse struct {
		Data controlplane.AIJobSummary `json:"data"`
	}
	if err := json.Unmarshal(jobSummary.Body.Bytes(), &jobSummaryResponse); err != nil || jobSummary.Code != http.StatusOK || jobSummaryResponse.Data.Total != 1 {
		t.Fatalf("platform job summary status=%d data=%+v body=%s err=%v", jobSummary.Code, jobSummaryResponse.Data, jobSummary.Body.String(), err)
	}
	if detail := request(http.MethodGet, "/api/v1/platform/ai-jobs/"+platformJob.ID); detail.Code != http.StatusOK {
		t.Fatalf("platform job detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	for _, result := range []*httptest.ResponseRecorder{
		request(http.MethodGet, "/api/v1/platform/ai-jobs/"+foreignJob.ID),
		request(http.MethodPost, "/api/v1/platform/ai-jobs/"+foreignJob.ID+"/cancel"),
	} {
		if result.Code != http.StatusNotFound || strings.Contains(result.Body.String(), foreignJob.ID) {
			t.Fatalf("foreign job was disclosed status=%d body=%s", result.Code, result.Body.String())
		}
	}
	if cancel := request(http.MethodPost, "/api/v1/platform/ai-jobs/"+platformJob.ID+"/cancel"); cancel.Code != http.StatusOK {
		t.Fatalf("platform job cancel status=%d body=%s", cancel.Code, cancel.Body.String())
	}
	audits := request(http.MethodGet, "/api/v1/platform/audit-logs?action=cancel&resource_type=ai_job")
	if audits.Code != http.StatusOK || !strings.Contains(audits.Body.String(), platformJob.ID) || strings.Contains(audits.Body.String(), foreignJob.ID) {
		t.Fatalf("platform job audit status=%d body=%s", audits.Code, audits.Body.String())
	}

	artifacts := request(http.MethodGet, "/api/v1/platform/artifacts?profile_scope=enterprise")
	var artifactsResponse struct {
		Data []controlplane.ArtifactAdminRecord `json:"data"`
	}
	if err := json.Unmarshal(artifacts.Body.Bytes(), &artifactsResponse); err != nil || artifacts.Code != http.StatusOK || len(artifactsResponse.Data) != 1 || artifactsResponse.Data[0].ID != platformArtifact.ID {
		t.Fatalf("platform artifacts status=%d data=%+v body=%s err=%v", artifacts.Code, artifactsResponse.Data, artifacts.Body.String(), err)
	}
	artifactSummary := request(http.MethodGet, "/api/v1/platform/artifacts/summary?profile_scope=enterprise")
	var artifactSummaryResponse struct {
		Data controlplane.ArtifactSummary `json:"data"`
	}
	if err := json.Unmarshal(artifactSummary.Body.Bytes(), &artifactSummaryResponse); err != nil || artifactSummary.Code != http.StatusOK || artifactSummaryResponse.Data.Total != 1 {
		t.Fatalf("platform artifact summary status=%d data=%+v body=%s err=%v", artifactSummary.Code, artifactSummaryResponse.Data, artifactSummary.Body.String(), err)
	}
	if detail := request(http.MethodGet, "/api/v1/platform/artifacts/"+platformArtifact.ID); detail.Code != http.StatusOK {
		t.Fatalf("platform artifact detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	if runtimes := request(http.MethodGet, "/api/v1/platform/artifact-runtimes"); runtimes.Code != http.StatusOK {
		t.Fatalf("platform artifact runtimes status=%d body=%s", runtimes.Code, runtimes.Body.String())
	}
	for _, result := range []*httptest.ResponseRecorder{
		request(http.MethodGet, "/api/v1/platform/artifacts/"+foreignArtifact.ID),
		request(http.MethodPost, "/api/v1/platform/artifacts/"+foreignArtifact.ID+"/retry-delivery"),
	} {
		if result.Code != http.StatusNotFound || strings.Contains(result.Body.String(), foreignArtifact.ID) {
			t.Fatalf("foreign artifact was disclosed status=%d body=%s", result.Code, result.Body.String())
		}
	}
}

func createScopedOperationsFixture(t *testing.T, control *controlplane.Service, modelID, suffix, profileScope string) (controlplane.AIJob, controlplane.Artifact) {
	t.Helper()
	job, _, err := control.BeginDurableAIJob(context.Background(), gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "operations-key-" + suffix,
		ProfileScope: profileScope, TenantID: "operations-tenant-" + suffix,
		PrincipalType: controlplane.APIKeyTypeService, PrincipalID: "operations-principal-" + suffix,
		ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	}, gatewaycore.CanonicalRequest{
		ID: "operations-request-" + suffix, Fingerprint: "operations-fingerprint-" + suffix,
		IdempotencyKey: "operations-idempotency-" + suffix, Protocol: gatewaycore.ProtocolAsterJobs,
		Operation: controlplane.GatewayOperationImageGeneration, Modality: controlplane.GatewayModalityImage,
		Lane: gatewaycore.LaneDurable, Model: modelID, Payload: []byte(`{"input":{"prompt":"synthetic"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := control.CreateArtifactFromReader(context.Background(), controlplane.ArtifactCreateInput{
		OperationID: job.OperationID, JobID: job.ID, Role: controlplane.ArtifactRoleFinal,
		Policy: controlplane.GatewayArtifactPolicyTemporary, MediaType: "image/png", StoreDriver: controlplane.ArtifactStoreDriverMemory,
	}, bytes.NewBufferString("synthetic-artifact-"+suffix))
	if err != nil {
		t.Fatal(err)
	}
	return job, artifact
}

func TestPlatformControlPlaneScopesPlatformDomainAndObservability(t *testing.T) {
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{
		Version: "test", StorageMode: "memory", EnabledProfiles: []string{"platform"}, DefaultProfile: "platform",
	})
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	handler := New(Options{
		Runtime:         RuntimeConfig{AdminToken: "secret"},
		SettingsService: settingsService,
		ControlService:  control,
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	})
	request := func(method, path, payload string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(payload))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	tenantsRec := request(http.MethodGet, "/api/v1/platform/tenants", "")
	if tenantsRec.Code != http.StatusOK {
		t.Fatalf("list default platform tenants status=%d body=%s", tenantsRec.Code, tenantsRec.Body.String())
	}
	var tenantsResponse struct {
		Data []controlplane.PlatformTenant `json:"data"`
	}
	if err := json.Unmarshal(tenantsRec.Body.Bytes(), &tenantsResponse); err != nil || len(tenantsResponse.Data) != 1 || tenantsResponse.Data[0].ID != "ptn_default" {
		t.Fatalf("default tenants=%+v err=%v", tenantsResponse.Data, err)
	}
	tenantRec := request(http.MethodPost, "/api/v1/platform/tenants", `{"name":"Partner API","slug":"partner-api","entitlement_reference":"partner-42"}`)
	if tenantRec.Code != http.StatusOK {
		t.Fatalf("create tenant status=%d body=%s", tenantRec.Code, tenantRec.Body.String())
	}
	var tenantResponse struct {
		Data controlplane.PlatformTenant `json:"data"`
	}
	if err := json.Unmarshal(tenantRec.Body.Bytes(), &tenantResponse); err != nil {
		t.Fatal(err)
	}
	principalRec := request(http.MethodPost, "/api/v1/platform/gateway-principals", `{"tenant_id":"`+tenantResponse.Data.ID+`","name":"Partner backend","principal_type":"integration","external_subject_reference":"partner-service"}`)
	if principalRec.Code != http.StatusOK {
		t.Fatalf("create principal status=%d body=%s", principalRec.Code, principalRec.Body.String())
	}
	var principalResponse struct {
		Data controlplane.GatewayPrincipal `json:"data"`
	}
	if err := json.Unmarshal(principalRec.Body.Bytes(), &principalResponse); err != nil {
		t.Fatal(err)
	}
	keyRec := request(http.MethodPost, "/api/v1/platform/api-keys", `{"name":"partner-key","key_type":"service","platform_tenant_id":"`+tenantResponse.Data.ID+`","gateway_principal_id":"`+principalResponse.Data.ID+`","model_allowlist":["model"],"qps_limit":1,"monthly_token_limit":1}`)
	if keyRec.Code != http.StatusOK {
		t.Fatalf("create scoped platform key status=%d body=%s", keyRec.Code, keyRec.Body.String())
	}
	var keyResponse struct {
		Data controlplane.APIKeyCreateResponse `json:"data"`
	}
	if err := json.Unmarshal(keyRec.Body.Bytes(), &keyResponse); err != nil {
		t.Fatal(err)
	}
	auth, err := control.AuthorizeGatewayModel(context.Background(), keyResponse.Data.Key, "model")
	if err != nil {
		t.Fatal(err)
	}
	if err := control.RecordGatewayUsage(context.Background(), auth, controlplane.GatewayUsageInput{Model: "model", Status: "forwarded", InputTokens: 2}); err != nil {
		t.Fatal(err)
	}
	if err := control.RecordGatewayTrace(context.Background(), auth, controlplane.GatewayTraceInput{Model: "model", Status: "forwarded"}); err != nil {
		t.Fatal(err)
	}
	if err := control.RecordGatewayCall(context.Background(), auth, "model", "forwarded", "platform call"); err != nil {
		t.Fatal(err)
	}
	if err := control.RecordGatewayUsage(context.Background(), controlplane.GatewayAuthContext{APIKey: controlplane.APIKeyRecord{ID: "legacy", Fingerprint: "legacy"}}, controlplane.GatewayUsageInput{Model: "legacy", Status: "forwarded"}); err != nil {
		t.Fatal(err)
	}

	usageRec := request(http.MethodGet, "/api/v1/platform/usage", "")
	if usageRec.Code != http.StatusOK || !strings.Contains(usageRec.Body.String(), tenantResponse.Data.ID) || strings.Contains(usageRec.Body.String(), "legacy") {
		t.Fatalf("platform usage scope status=%d body=%s", usageRec.Code, usageRec.Body.String())
	}
	traceRec := request(http.MethodGet, "/api/v1/platform/gateway-traces", "")
	if traceRec.Code != http.StatusOK || !strings.Contains(traceRec.Body.String(), tenantResponse.Data.ID) {
		t.Fatalf("platform trace scope status=%d body=%s", traceRec.Code, traceRec.Body.String())
	}
	auditRec := request(http.MethodGet, "/api/v1/platform/audit-logs", "")
	if auditRec.Code != http.StatusOK || !strings.Contains(auditRec.Body.String(), tenantResponse.Data.ID) {
		t.Fatalf("platform audit scope status=%d body=%s", auditRec.Code, auditRec.Body.String())
	}
	if err := control.RecordRiskRuleAlert(context.Background(), "legacy", "legacy-rule", "Legacy rule", "legacy alert", 1, 1); err != nil {
		t.Fatal(err)
	}
	alertsRec := request(http.MethodGet, "/api/v1/platform/alerts", "")
	if alertsRec.Code != http.StatusOK || !strings.Contains(alertsRec.Body.String(), tenantResponse.Data.ID) || strings.Contains(alertsRec.Body.String(), "legacy alert") {
		t.Fatalf("platform alert scope status=%d body=%s", alertsRec.Code, alertsRec.Body.String())
	}
}
