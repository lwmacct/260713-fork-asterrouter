package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

func TestAdminArtifactEndpointsReturnFilteredRedactedRecords(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	artifact := createAdminRouteArtifact(t, control)
	if err := control.SetArtifactSink(serverArtifactSink{id: "customer-sink"}); err != nil {
		t.Fatal(err)
	}
	if err := control.SetArtifactProxy(&serverArtifactProxy{providerID: "provider-runtime"}); err != nil {
		t.Fatal(err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/artifacts?status=ready&policy=managed&limit=10", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	for _, forbidden := range []string{"external_reference", "store_key", "principal_id", "external_subject_reference", "provider-secret-reference"} {
		if strings.Contains(listRec.Body.String(), forbidden) {
			t.Fatalf("list disclosed %q: %s", forbidden, listRec.Body.String())
		}
	}
	var listResponse struct {
		Data []controlplane.ArtifactAdminRecord `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResponse); err != nil || len(listResponse.Data) != 1 || listResponse.Data[0].ID != artifact.ID {
		t.Fatalf("list=%+v err=%v", listResponse.Data, err)
	}

	summaryReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/artifacts/summary?limit=1&offset=100", nil)
	summaryRec := httptest.NewRecorder()
	handler.ServeHTTP(summaryRec, summaryReq)
	var summaryResponse struct {
		Data controlplane.ArtifactSummary `json:"data"`
	}
	if err := json.Unmarshal(summaryRec.Body.Bytes(), &summaryResponse); err != nil || summaryRec.Code != http.StatusOK || summaryResponse.Data.Total != 1 {
		t.Fatalf("summary status=%d body=%s err=%v", summaryRec.Code, summaryRec.Body.String(), err)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/artifacts/"+artifact.ID, nil)
	detailRec := httptest.NewRecorder()
	handler.ServeHTTP(detailRec, detailReq)
	var detailResponse struct {
		Data controlplane.ArtifactAdminDetail `json:"data"`
	}
	if err := json.Unmarshal(detailRec.Body.Bytes(), &detailResponse); err != nil || detailRec.Code != http.StatusOK || detailResponse.Data.Artifact.ID != artifact.ID || len(detailResponse.Data.Events) != 2 {
		t.Fatalf("detail status=%d body=%s err=%v", detailRec.Code, detailRec.Body.String(), err)
	}

	runtimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/artifact-runtimes", nil)
	runtimeRec := httptest.NewRecorder()
	handler.ServeHTTP(runtimeRec, runtimeReq)
	var runtimeResponse struct {
		Data []controlplane.ArtifactRuntime `json:"data"`
	}
	if err := json.Unmarshal(runtimeRec.Body.Bytes(), &runtimeResponse); err != nil || runtimeRec.Code != http.StatusOK || len(runtimeResponse.Data) != 2 {
		t.Fatalf("runtimes status=%d body=%s err=%v", runtimeRec.Code, runtimeRec.Body.String(), err)
	}

	invalidReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/artifacts?status=invalid", nil)
	invalidRec := httptest.NewRecorder()
	handler.ServeHTTP(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid query status=%d body=%s", invalidRec.Code, invalidRec.Body.String())
	}
}

func TestAdminArtifactRBACSeparatesReadAndRetryAndRequiresGlobalScope(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "secret"})
	ctx := context.Background()
	auditor, err := control.CreateWorkspaceUser(ctx, "tester", controlplane.WorkspaceUserRequest{
		Email: "artifact-auditor@example.test", Status: controlplane.WorkspaceUserStatusActive, Role: controlplane.RoleReadOnlyAuditor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.CreateRoleBinding(ctx, "tester", controlplane.RoleBindingRequest{
		UserID: auditor.ID, Role: controlplane.RoleReadOnlyAuditor, ScopeType: controlplane.RoleScopeGlobal,
	}); err != nil {
		t.Fatal(err)
	}

	readReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/artifacts", nil)
	readReq.Header.Set("Authorization", "Bearer secret")
	readReq.Header.Set("X-Actor", auditor.Email)
	readRec := httptest.NewRecorder()
	handler.ServeHTTP(readRec, readReq)
	if readRec.Code != http.StatusOK {
		t.Fatalf("auditor read status=%d body=%s", readRec.Code, readRec.Body.String())
	}

	retryReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/artifacts/artifact_missing/retry-delivery", nil)
	retryReq.Header.Set("Authorization", "Bearer secret")
	retryReq.Header.Set("X-Actor", auditor.Email)
	retryRec := httptest.NewRecorder()
	handler.ServeHTTP(retryRec, retryReq)
	if retryRec.Code != http.StatusForbidden {
		t.Fatalf("auditor retry status=%d body=%s", retryRec.Code, retryRec.Body.String())
	}

	department, err := control.CreateDepartment(ctx, "tester", controlplane.DepartmentRequest{Name: "Artifact team", Code: "artifact-team", Status: controlplane.DepartmentStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := control.CreateWorkspaceUser(ctx, "tester", controlplane.WorkspaceUserRequest{
		Email: "artifact-department@example.test", Status: controlplane.WorkspaceUserStatusActive, Role: controlplane.RoleDeveloper,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.CreateRoleBinding(ctx, "tester", controlplane.RoleBindingRequest{
		UserID: manager.ID, Role: controlplane.RolePlatformAdmin, ScopeType: controlplane.RoleScopeDepartment, ScopeID: department.ID,
	}); err != nil {
		t.Fatal(err)
	}
	scopedReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/artifacts", nil)
	scopedReq.Header.Set("Authorization", "Bearer secret")
	scopedReq.Header.Set("X-Actor", manager.Email)
	scopedRec := httptest.NewRecorder()
	handler.ServeHTTP(scopedRec, scopedReq)
	if scopedRec.Code != http.StatusForbidden || strings.Contains(scopedRec.Body.String(), "artifact_") {
		t.Fatalf("department artifact read status=%d body=%s", scopedRec.Code, scopedRec.Body.String())
	}
}

func createAdminRouteArtifact(t *testing.T, control *controlplane.Service) controlplane.Artifact {
	t.Helper()
	ctx := context.Background()
	model, err := control.CreateGatewayModel(ctx, "tester", controlplane.GatewayModelRequest{
		ModelID: "admin-artifact-model", Name: "Admin artifact model", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := control.BeginDurableAIJob(ctx, gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "admin-artifact-key", ProfileScope: controlplane.ProfileScopePlatform,
		TenantID: "admin-artifact-tenant", PrincipalType: controlplane.APIKeyTypeService, PrincipalID: "admin-artifact-principal",
		ArtifactPolicy: controlplane.GatewayArtifactPolicyManaged,
	}, gatewaycore.CanonicalRequest{
		ID: "admin-artifact-request", Fingerprint: "admin-artifact-fingerprint", IdempotencyKey: "admin-artifact-idempotency",
		Protocol: gatewaycore.ProtocolAsterJobs, Operation: "image_generation", Modality: "image", Lane: gatewaycore.LaneDurable,
		Model: model.ModelID, Payload: []byte(`{"input":{"prompt":"synthetic"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := control.CreateArtifactFromReader(ctx, controlplane.ArtifactCreateInput{
		OperationID: job.OperationID, JobID: job.ID, Role: controlplane.ArtifactRoleMetadata,
		Policy: controlplane.GatewayArtifactPolicyManaged, MediaType: "application/json",
		ExternalReference: "provider-secret-reference",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

type serverArtifactSink struct{ id string }

func (s serverArtifactSink) ID() string                            { return s.id }
func (serverArtifactSink) Accepts(controlplane.ArtifactOwner) bool { return true }
func (serverArtifactSink) DeliverArtifact(context.Context, controlplane.ArtifactSinkRequest, io.Reader) (controlplane.ArtifactSinkResult, error) {
	return controlplane.ArtifactSinkResult{ExternalReference: "s3://customer/artifact"}, nil
}
func (serverArtifactSink) DeleteArtifact(context.Context, controlplane.ArtifactSinkRequest) error {
	return nil
}
