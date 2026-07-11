package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
)

func newTestRuntime(t *testing.T, cfg config.Config) (http.Handler, *controlplane.Service) {
	t.Helper()
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if err := controlService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	pluginService := plugins.NewService(plugins.NewMemoryRepository())
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("Plugin EnsureSeedData(): %v", err)
	}
	systemService := system.NewService(system.Config{Version: "test", BuildType: "source"})
	return New(Options{Config: cfg, SettingsService: settingsService, ControlService: controlService, PluginService: pluginService, SystemService: systemService}), controlService
}

func newTestHandler(t *testing.T, cfg config.Config) http.Handler {
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
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if err := controlService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	pluginService := plugins.NewService(plugins.NewMemoryRepository())
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("Plugin EnsureSeedData(): %v", err)
	}
	return New(Options{
		Config:          config.Config{},
		AuthService:     auth.NewService(auth.Config{Username: "admin", Password: "secret", SecretKey: "test-secret"}),
		SettingsService: settingsService,
		ControlService:  controlService,
		PluginService:   pluginService,
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	}), controlService
}

func TestPublicSettingsEndpoint(t *testing.T) {
	handler := newTestHandler(t, config.Config{})

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
	handler := newTestHandler(t, config.Config{AdminToken: "secret"})

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

func TestSetupProfileEndpoint(t *testing.T) {
	repo := settings.NewMemoryRepository()
	svc := settings.NewService(repo, settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	pluginService := plugins.NewService(plugins.NewMemoryRepository())
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("Plugin EnsureSeedData(): %v", err)
	}
	systemService := system.NewService(system.Config{Version: "test", BuildType: "source"})
	handler := New(Options{Config: config.Config{}, SettingsService: svc, ControlService: controlService, PluginService: pluginService, SystemService: systemService})

	body := bytes.NewBufferString(`{"profiles":["enterprise","personal"],"default_profile":"personal"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/profiles", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got, err := svc.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin(): %v", err)
	}
	if got.DefaultProfile != "personal" || len(got.EnabledProfiles) != 2 || !got.SetupCompleted {
		t.Fatalf("setup not persisted: %+v", got)
	}
}
