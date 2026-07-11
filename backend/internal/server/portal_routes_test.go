package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestPortalRoutesRequireLoginWhenAuthServiceEnabled(t *testing.T) {
	handler := newAuthTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/portal/workspace", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("portal workspace should require login status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPortalWorkspaceAndAPIKeyRoutes(t *testing.T) {
	handler := newAuthTestHandler(t)
	token := loginForTest(t, handler)

	workspaceReq := httptest.NewRequest(http.MethodGet, "/api/v1/portal/workspace", nil)
	workspaceReq.Header.Set("Authorization", "Bearer "+token)
	workspaceRec := httptest.NewRecorder()
	handler.ServeHTTP(workspaceRec, workspaceReq)
	if workspaceRec.Code != http.StatusOK {
		t.Fatalf("portal workspace status=%d body=%s", workspaceRec.Code, workspaceRec.Body.String())
	}
	var workspaceResp struct {
		Data controlplane.PortalWorkspace `json:"data"`
	}
	if err := json.Unmarshal(workspaceRec.Body.Bytes(), &workspaceResp); err != nil {
		t.Fatalf("decode workspace: %v", err)
	}
	if !workspaceResp.Data.CanManageKeys || len(workspaceResp.Data.Projects) == 0 || workspaceResp.Data.GatewayPath != "/v1" {
		t.Fatalf("workspace mismatch: %+v", workspaceResp.Data)
	}

	body := bytes.NewBufferString(`{"name":"Portal HTTP key","model_allowlist":["gpt-4o-mini"],"qps_limit":1,"monthly_token_limit":1000}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/portal/api-keys", body)
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("portal key create status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var createResp struct {
		Data controlplane.APIKeyCreateResponse `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create key: %v", err)
	}
	if createResp.Data.Key == "" || createResp.Data.Record.ID == "" {
		t.Fatalf("create key mismatch: %+v", createResp.Data)
	}
	if createResp.Data.Record.ProjectID != "proj_platform" || createResp.Data.Record.ApplicationID != "app_internal_sandbox" {
		t.Fatalf("workspace default boundary mismatch: %+v", createResp.Data.Record)
	}

	rotateReq := httptest.NewRequest(http.MethodPost, "/api/v1/portal/api-keys/"+createResp.Data.Record.ID+"/rotate", nil)
	rotateReq.Header.Set("Authorization", "Bearer "+token)
	rotateRec := httptest.NewRecorder()
	handler.ServeHTTP(rotateRec, rotateReq)
	if rotateRec.Code != http.StatusOK {
		t.Fatalf("portal key rotate status=%d body=%s", rotateRec.Code, rotateRec.Body.String())
	}

	disableReq := httptest.NewRequest(http.MethodPost, "/api/v1/portal/api-keys/"+createResp.Data.Record.ID+"/disable", nil)
	disableReq.Header.Set("Authorization", "Bearer "+token)
	disableRec := httptest.NewRecorder()
	handler.ServeHTTP(disableRec, disableReq)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("portal key disable status=%d body=%s", disableRec.Code, disableRec.Body.String())
	}
}

func loginForTest(t *testing.T, handler http.Handler) string {
	t.Helper()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var loginResp struct {
		Data auth.LoginResult `json:"data"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	return loginResp.Data.AccessToken
}
