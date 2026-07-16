package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

type testAIJobRuntime struct{}

func (testAIJobRuntime) SupportsDurableAIJob(context.Context, gatewaycore.CanonicalAuthContext, gatewaycore.CanonicalRequest) (bool, error) {
	return true, nil
}

func (testAIJobRuntime) Status() controlplane.DurableAIJobRuntimeStatus {
	return controlplane.DurableAIJobRuntimeStatus{Running: true, QueueDriver: "memory", WorkerID: "test-worker"}
}

func TestAdminAIJobEndpointsProvideRuntimeDetailAndSafeActions(t *testing.T) {
	handler, control := newTestRuntimeWithDurableAdmission(t, RuntimeConfig{AdminToken: "secret"}, testAIJobRuntime{})
	ctx := context.Background()
	model, err := control.CreateGatewayModel(ctx, "tester", controlplane.GatewayModelRequest{
		ModelID: "admin-job-model", Name: "Admin job model", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := control.BeginDurableAIJob(ctx, gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "admin-job-key", ProfileScope: controlplane.ProfileScopePlatform,
		TenantID: "admin-job-tenant", PrincipalType: controlplane.APIKeyTypeService, PrincipalID: "admin-job-principal",
		ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	}, gatewaycore.CanonicalRequest{
		ID: "admin-job-request", Fingerprint: "admin-job-fingerprint", IdempotencyKey: "admin-job-idempotency",
		Protocol: gatewaycore.ProtocolAsterJobs, Operation: "image_generation", Modality: "image", Lane: gatewaycore.LaneDurable,
		Model: model.ModelID, Payload: []byte(`{"request_payload":"secret-marker"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ai-jobs?status=queued", nil)
	request.Header.Set("Authorization", "Bearer secret")
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, request)
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "secret-marker") || strings.Contains(list.Body.String(), "request_payload") {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	var listResponse struct {
		Data []controlplane.AIJobAdminRecord `json:"data"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listResponse); err != nil || len(listResponse.Data) != 1 || listResponse.Data[0].ID != job.ID {
		t.Fatalf("list=%+v err=%v", listResponse.Data, err)
	}

	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ai-jobs/"+job.ID, nil)
	detailRequest.Header.Set("Authorization", "Bearer secret")
	detail := httptest.NewRecorder()
	handler.ServeHTTP(detail, detailRequest)
	if detail.Code != http.StatusOK || strings.Contains(detail.Body.String(), "secret-marker") {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}

	runtimeRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ai-jobs/runtime", nil)
	runtimeRequest.Header.Set("Authorization", "Bearer secret")
	runtime := httptest.NewRecorder()
	handler.ServeHTTP(runtime, runtimeRequest)
	if runtime.Code != http.StatusOK || !strings.Contains(runtime.Body.String(), `"queue_driver":"memory"`) {
		t.Fatalf("runtime status=%d body=%s", runtime.Code, runtime.Body.String())
	}

	cancelRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ai-jobs/"+job.ID+"/cancel", nil)
	cancelRequest.Header.Set("Authorization", "Bearer secret")
	cancel := httptest.NewRecorder()
	handler.ServeHTTP(cancel, cancelRequest)
	if cancel.Code != http.StatusOK || !strings.Contains(cancel.Body.String(), `"status":"canceled"`) {
		t.Fatalf("cancel status=%d body=%s", cancel.Code, cancel.Body.String())
	}

	invalidRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ai-jobs?status=invalid", nil)
	invalidRequest.Header.Set("Authorization", "Bearer secret")
	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, invalidRequest)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestAdminAIJobRBACRequiresGlobalScope(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "secret"})
	ctx := context.Background()
	department, err := control.CreateDepartment(ctx, "tester", controlplane.DepartmentRequest{Name: "Job team", Code: "job-team", Status: controlplane.DepartmentStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := control.CreateWorkspaceUser(ctx, "tester", controlplane.WorkspaceUserRequest{Email: "job-department@example.test", Status: controlplane.WorkspaceUserStatusActive, Role: controlplane.RoleDeveloper})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.CreateRoleBinding(ctx, "tester", controlplane.RoleBindingRequest{UserID: manager.ID, Role: controlplane.RolePlatformAdmin, ScopeType: controlplane.RoleScopeDepartment, ScopeID: department.ID}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ai-jobs", nil)
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("X-Actor", manager.Email)
	record := httptest.NewRecorder()
	handler.ServeHTTP(record, request)
	if record.Code != http.StatusForbidden || strings.Contains(record.Body.String(), "request_payload") {
		t.Fatalf("status=%d body=%s", record.Code, record.Body.String())
	}
}
