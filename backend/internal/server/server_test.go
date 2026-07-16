package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	operatorcore "github.com/astercloud/asterrouter/backend/internal/operator"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
)

type allowDurableAIJobs struct{}

func (allowDurableAIJobs) SupportsDurableAIJob(context.Context, gatewaycore.CanonicalAuthContext, gatewaycore.CanonicalRequest) (bool, error) {
	return true, nil
}

func newTestRuntime(t *testing.T, cfg RuntimeConfig) (http.Handler, *controlplane.Service) {
	return newTestRuntimeWithDurableAdmission(t, cfg, allowDurableAIJobs{})
}

func newTestRuntimeWithDurableAdmission(t *testing.T, cfg RuntimeConfig, durableJobs DurableAIJobAdmission) (http.Handler, *controlplane.Service) {
	t.Helper()
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory", DemoMode: true, EnabledProfiles: []string{"personal", "relay_operator", "enterprise"}})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if err := controlService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	pluginService := plugins.NewService(plugins.NewMemoryRepository())
	operatorService := operatorcore.NewService(operatorcore.NewMemoryRepository(), controlService)
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("Plugin EnsureSeedData(): %v", err)
	}
	systemService := system.NewService(system.Config{Version: "test", BuildType: "source"})
	var runtime AIJobRuntimeStatusProvider
	if value, ok := durableJobs.(AIJobRuntimeStatusProvider); ok {
		runtime = value
	}
	return New(Options{Runtime: cfg, SettingsService: settingsService, ControlService: controlService, OperatorService: operatorService, PluginService: pluginService, SystemService: systemService, DurableAIJobs: durableJobs, AIJobRuntime: runtime}), controlService
}

func newTestHandler(t *testing.T, cfg RuntimeConfig) http.Handler {
	t.Helper()
	handler, _ := newTestRuntime(t, cfg)
	return handler
}

func newAuthTestHandler(t *testing.T) http.Handler {
	t.Helper()
	handler, _ := newAuthTestRuntime(t)
	return handler
}

func newAuthTestRuntime(t *testing.T) (http.Handler, *controlplane.Service) {
	t.Helper()
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory", DemoMode: true, EnabledProfiles: []string{"personal", "relay_operator", "enterprise"}})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if err := controlService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	localAdmin, err := controlService.EnsureLocalAdmin(context.Background(), "admin", "secret", controlplane.WorkspaceUserDefaults{ConcurrencyLimit: 5})
	if err != nil {
		t.Fatalf("EnsureLocalAdmin(): %v", err)
	}
	pluginService := plugins.NewService(plugins.NewMemoryRepository())
	operatorService := operatorcore.NewService(operatorcore.NewMemoryRepository(), controlService)
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("Plugin EnsureSeedData(): %v", err)
	}
	return New(Options{
		Runtime:         RuntimeConfig{},
		AuthService:     auth.NewService(auth.Config{Username: "admin", Password: "secret", PasswordHash: localAdmin.PasswordHash, SecretKey: "test-secret"}),
		SettingsService: settingsService,
		ControlService:  controlService,
		OperatorService: operatorService,
		PluginService:   pluginService,
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	}), controlService
}

func TestPublicSettingsEndpoint(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int                     `json:"code"`
		Data settings.PublicSettings `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.SiteName != "AsterRouter" {
		t.Fatalf("site_name = %q", resp.Data.SiteName)
	}
}

func TestAdminSettingsRequiresToken(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{AdminToken: "secret"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminSettingsRequiresLoginWhenAuthServiceEnabled(t *testing.T) {
	handler := newAuthTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLoginAllowsAdminSettingsAccess(t *testing.T) {
	handler := newAuthTestHandler(t)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var loginResp struct {
		Data auth.LoginResult `json:"data"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if loginResp.Data.AccessToken == "" {
		t.Fatalf("empty access token: %+v", loginResp.Data)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("settings status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuthenticationResponsesExposeServerDerivedAllowedSurfaces(t *testing.T) {
	handler, control := newAuthTestRuntime(t)
	user, _, err := control.RegisterWorkspaceUser(t.Context(), "surface-summary@example.test", "synthetic-password-123", "Surface Summary", false)
	if err != nil {
		t.Fatal(err)
	}
	login := func() auth.LoginResult {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"surface-summary@example.test","password":"synthetic-password-123"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var response struct {
			Data auth.LoginResult `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("login status=%d body=%s err=%v", rec.Code, rec.Body.String(), err)
		}
		return response.Data
	}

	initial := login()
	if !containsSurface(initial.User.AllowedSurfaces, controlplane.SurfaceCustomer) || !containsSurface(initial.User.AllowedSurfaces, controlplane.SurfacePortal) || containsSurface(initial.User.AllowedSurfaces, controlplane.SurfaceRelayOperator) || containsSurface(initial.User.AllowedSurfaces, controlplane.SurfaceEnterprise) {
		t.Fatalf("initial allowed surfaces=%v", initial.User.AllowedSurfaces)
	}
	if _, err := control.CreateRoleBinding(t.Context(), "tester", controlplane.RoleBindingRequest{
		UserID: user.ID, Role: controlplane.RolePlatformAdmin, ScopeType: controlplane.RoleScopeSurface, ScopeID: controlplane.SurfaceRelayOperator,
	}); err != nil {
		t.Fatal(err)
	}
	bound := login()
	if !containsSurface(bound.User.AllowedSurfaces, controlplane.SurfaceRelayOperator) {
		t.Fatalf("bound allowed surfaces=%v", bound.User.AllowedSurfaces)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+bound.AccessToken)
	meRec := httptest.NewRecorder()
	handler.ServeHTTP(meRec, meReq)
	var meResponse struct {
		Data auth.User `json:"data"`
	}
	if err := json.Unmarshal(meRec.Body.Bytes(), &meResponse); err != nil || meRec.Code != http.StatusOK || !containsSurface(meResponse.Data.AllowedSurfaces, controlplane.SurfaceRelayOperator) {
		t.Fatalf("auth/me status=%d body=%s response=%+v err=%v", meRec.Code, meRec.Body.String(), meResponse, err)
	}
}

func containsSurface(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestLoginAgreementIsEnforcedAfterPayloadBinding(t *testing.T) {
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	current, err := settingsService.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin(): %v", err)
	}
	current.LoginAgreementEnabled = true
	current.LegalDocuments = []settings.LegalDocument{{ID: "terms", Name: "Terms", Slug: "terms", Content: "Terms"}}
	if _, err := settingsService.Update(context.Background(), current); err != nil {
		t.Fatalf("Update(): %v", err)
	}
	handler := New(Options{
		AuthService:     auth.NewService(auth.Config{Username: "admin", Password: "secret", SecretKey: "test-secret"}),
		SettingsService: settingsService,
		ControlService:  controlplane.NewService(controlplane.NewMemoryRepository(), "/v1"),
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	})

	for _, test := range []struct {
		name     string
		accepted bool
		status   int
	}{
		{name: "missing acceptance is rejected", accepted: false, status: http.StatusForbidden},
		{name: "explicit acceptance allows login", accepted: true, status: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"username":"admin","password":"secret","agreement_accepted":%t}`, test.accepted)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != test.status {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, test.status, rec.Body.String())
			}
		})
	}
}

func TestLegacyCaptchaEndpointDisablesCaptcha(t *testing.T) {
	handler := newAuthTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/iam/get-captcha-code?locale=zh_CN", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			CaptchaOnOff bool   `json:"captchaOnOff"`
			Img          string `json:"img"`
			UUID         string `json:"uuid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.CaptchaOnOff || resp.Data.Img != "" || resp.Data.UUID != "" {
		t.Fatalf("captcha response = %+v", resp.Data)
	}
}

func TestSetupProfileEndpoint(t *testing.T) {
	repo := settings.NewMemoryRepository()
	svc := settings.NewService(repo, settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	pluginService := plugins.NewService(plugins.NewMemoryRepository())
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("Plugin EnsureSeedData(): %v", err)
	}
	systemService := system.NewService(system.Config{Version: "test", BuildType: "source"})
	handler := New(Options{Runtime: RuntimeConfig{}, SettingsService: svc, ControlService: controlService, PluginService: pluginService, SystemService: systemService})
	postProfile := func(profile string) *httptest.ResponseRecorder {
		t.Helper()
		body := bytes.NewBufferString(fmt.Sprintf(`{"profile":%q}`, profile))
		req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/profiles", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	rec := postProfile("platform")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got, err := svc.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin(): %v", err)
	}
	if got.DefaultProfile != "platform" || len(got.EnabledProfiles) != 1 || got.EnabledProfiles[0] != "platform" || !got.SetupCompleted {
		t.Fatalf("setup not persisted: %+v", got)
	}

	retry := postProfile("platform")
	if retry.Code != http.StatusOK {
		t.Fatalf("same-profile retry status = %d body=%s", retry.Code, retry.Body.String())
	}
	var retryResponse struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(retry.Body.Bytes(), &retryResponse); err != nil {
		t.Fatalf("decode same-profile retry: %v", err)
	}
	if _, exposed := retryResponse.Data["invitation_codes"]; exposed {
		t.Fatalf("same-profile retry exposed admin-only settings: %s", retry.Body.String())
	}

	conflict := postProfile("enterprise")
	if conflict.Code != http.StatusBadRequest {
		t.Fatalf("different-profile retry status = %d, want %d body=%s", conflict.Code, http.StatusBadRequest, conflict.Body.String())
	}
	got, err = svc.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin() after conflict: %v", err)
	}
	if got.DefaultProfile != "platform" || len(got.EnabledProfiles) != 1 || got.EnabledProfiles[0] != "platform" {
		t.Fatalf("conflicting retry mutated setup: %+v", got)
	}
}

func TestSetupProfileEndpointSerializesConcurrentInstalls(t *testing.T) {
	for _, test := range []struct {
		name          string
		profiles      []string
		wantSucceeded int
	}{
		{name: "same profile retries are idempotent", profiles: []string{"platform", "platform"}, wantSucceeded: 2},
		{name: "different profiles conflict", profiles: []string{"platform", "enterprise"}, wantSucceeded: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
			controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
			handler := New(Options{
				SettingsService: svc,
				ControlService:  controlService,
				SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
			})
			start := make(chan struct{})
			responses := make(chan *httptest.ResponseRecorder, len(test.profiles))
			for _, profile := range test.profiles {
				go func(profile string) {
					<-start
					body := bytes.NewBufferString(fmt.Sprintf(`{"profile":%q}`, profile))
					req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/profiles", body)
					req.Header.Set("Content-Type", "application/json")
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, req)
					responses <- rec
				}(profile)
			}
			close(start)

			succeeded := 0
			for range test.profiles {
				rec := <-responses
				if rec.Code == http.StatusOK {
					succeeded++
					continue
				}
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("unexpected status = %d body=%s", rec.Code, rec.Body.String())
				}
			}
			if succeeded != test.wantSucceeded {
				t.Fatalf("successful requests = %d, want %d", succeeded, test.wantSucceeded)
			}

			current, err := svc.Admin(context.Background())
			if err != nil {
				t.Fatalf("Admin(): %v", err)
			}
			if !current.SetupCompleted || len(current.EnabledProfiles) != 1 || current.DefaultProfile != current.EnabledProfiles[0] {
				t.Fatalf("persisted deployment profile is inconsistent: %+v", current.PublicSettings)
			}
			tenants, err := controlService.ListPlatformTenants(context.Background())
			if err != nil {
				t.Fatalf("ListPlatformTenants(): %v", err)
			}
			if current.DefaultProfile == "platform" && len(tenants) == 0 {
				t.Fatal("platform installation did not create its bootstrap tenant")
			}
			if current.DefaultProfile != "platform" && len(tenants) != 0 {
				t.Fatalf("losing platform request created bootstrap tenants: %+v", tenants)
			}
		})
	}
}

type enterpriseFirstSetupRepository struct {
	*settings.MemoryRepository
	enterpriseInstalled chan struct{}
}

func newEnterpriseFirstSetupRepository() *enterpriseFirstSetupRepository {
	return &enterpriseFirstSetupRepository{
		MemoryRepository:    settings.NewMemoryRepository(),
		enterpriseInstalled: make(chan struct{}),
	}
}

func (r *enterpriseFirstSetupRepository) InitializeDeploymentProfile(ctx context.Context, profile string) error {
	if profile == controlplane.ProfileScopePlatform {
		<-r.enterpriseInstalled
	}
	err := r.MemoryRepository.InitializeDeploymentProfile(ctx, profile)
	if profile == "enterprise" {
		close(r.enterpriseInstalled)
	}
	return err
}

func TestSetupProfileEndpointDoesNotBootstrapLosingPlatformInstall(t *testing.T) {
	repo := newEnterpriseFirstSetupRepository()
	svc := settings.NewService(repo, settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	handler := New(Options{
		SettingsService: svc,
		ControlService:  controlService,
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	})
	type response struct {
		profile string
		record  *httptest.ResponseRecorder
	}
	start := make(chan struct{})
	responses := make(chan response, 2)
	for _, profile := range []string{"platform", "enterprise"} {
		go func(profile string) {
			<-start
			req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/profiles", bytes.NewBufferString(fmt.Sprintf(`{"profile":%q}`, profile)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			responses <- response{profile: profile, record: rec}
		}(profile)
	}
	close(start)

	for range 2 {
		result := <-responses
		want := http.StatusBadRequest
		if result.profile == "enterprise" {
			want = http.StatusOK
		}
		if result.record.Code != want {
			t.Fatalf("%s status = %d, want %d body=%s", result.profile, result.record.Code, want, result.record.Body.String())
		}
	}
	tenants, err := controlService.ListPlatformTenants(context.Background())
	if err != nil {
		t.Fatalf("ListPlatformTenants(): %v", err)
	}
	if len(tenants) != 0 {
		t.Fatalf("losing platform request created bootstrap tenants: %+v", tenants)
	}
}

type failingDeploymentProfileRepository struct {
	*settings.MemoryRepository
}

func (r *failingDeploymentProfileRepository) InitializeDeploymentProfile(context.Context, string) error {
	return errors.New("database secret detail")
}

func TestSetupProfileEndpointReturnsSanitizedServerErrorWhenPersistenceFails(t *testing.T) {
	svc := settings.NewService(&failingDeploymentProfileRepository{MemoryRepository: settings.NewMemoryRepository()}, settings.ServiceOptions{
		Version: "test", StorageMode: "memory",
	})
	handler := New(Options{
		SettingsService: svc,
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/profiles", bytes.NewBufferString(`{"profile":"enterprise"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("database secret detail")) {
		t.Fatalf("setup response exposed repository error: %s", rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("failed to initialize deployment profile")) {
		t.Fatalf("setup response did not include the public error category: %s", rec.Body.String())
	}
}

func TestSetupProfileEndpointRequiresOneValidProfile(t *testing.T) {
	svc := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	handler := New(Options{
		SettingsService: svc,
		ControlService:  controlplane.NewService(controlplane.NewMemoryRepository(), "/v1"),
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	})

	for _, body := range []string{
		`{"profiles":["enterprise","platform"],"default_profile":"enterprise"}`,
		`{"profile":"unsupported"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/profiles", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d, want %d, response=%s", body, rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	}
}
