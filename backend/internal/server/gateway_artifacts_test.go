package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestGatewayArtifactAuthorizationRangeAndDeletion(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	if _, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "artifact-image-model", Name: "Artifact image model", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	request := durableJobAPIKeyRequest("artifact owner")
	request.ModelAllowlist = []string{"artifact-image-model"}
	request.Scopes = append(request.Scopes, controlplane.GatewayScopeArtifactsRead, controlplane.GatewayScopeArtifactsDelete)
	owner, err := control.CreateAPIKey(context.Background(), "test", request)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"artifact-image-model","operation":"image_generation","modality":"image","input":{"prompt":"synthetic"}}`
	created := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", owner.Key, "artifact-http-idem", body)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create job status=%d body=%s", created.Code, created.Body.String())
	}
	var job publicAIJobResponse
	if err := json.Unmarshal(created.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	store := controlplane.NewMemoryArtifactStore()
	if err := control.SetArtifactStore(store); err != nil {
		t.Fatal(err)
	}
	payload := []byte("public-synthetic-image")
	artifact, err := control.CreateArtifactFromReader(context.Background(), controlplane.ArtifactCreateInput{
		OperationID: job.OperationID, JobID: job.ID, Role: controlplane.ArtifactRoleFinal, MediaType: "image/png",
		StoreDriver: controlplane.ArtifactStoreDriverMemory, ExpectedSizeBytes: int64(len(payload)), MaxBytes: 1024,
		ExternalReference: "https://provider.invalid/private-object",
	}, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := control.RotateAPIKey(context.Background(), "test", owner.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	metadata := performGatewayJobRequest(handler, http.MethodGet, "/v1/artifacts/"+artifact.ID, rotated.Key, "", "")
	if metadata.Code != http.StatusOK || !strings.Contains(metadata.Body.String(), artifact.ID) {
		t.Fatalf("metadata status=%d body=%s", metadata.Code, metadata.Body.String())
	}
	for _, secret := range []string{"store_key", "external_reference", "private-object", "provider.invalid"} {
		if strings.Contains(metadata.Body.String(), secret) {
			t.Fatalf("artifact metadata leaked %q: %s", secret, metadata.Body.String())
		}
	}
	list := performGatewayJobRequest(handler, http.MethodGet, "/v1/jobs/"+job.ID+"/artifacts", rotated.Key, "", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), artifact.ID) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	rangeResponse := performGatewayArtifactRequest(handler, http.MethodGet, "/v1/artifacts/"+artifact.ID+"/content", rotated.Key, "bytes=2-7")
	if rangeResponse.Code != http.StatusPartialContent || rangeResponse.Body.String() != string(payload[2:8]) ||
		rangeResponse.Header().Get("Content-Range") == "" || rangeResponse.Header().Get("ETag") == "" {
		t.Fatalf("range status=%d headers=%v body=%q", rangeResponse.Code, rangeResponse.Header(), rangeResponse.Body.String())
	}
	invalidRange := performGatewayArtifactRequest(handler, http.MethodGet, "/v1/artifacts/"+artifact.ID+"/content", rotated.Key, "bytes=999-")
	if invalidRange.Code != http.StatusRequestedRangeNotSatisfiable || invalidRange.Header().Get("Content-Range") == "" {
		t.Fatalf("invalid range status=%d headers=%v body=%s", invalidRange.Code, invalidRange.Header(), invalidRange.Body.String())
	}
	otherRequest := request
	otherRequest.Name = "other artifact owner"
	other, err := control.CreateAPIKey(context.Background(), "test", otherRequest)
	if err != nil {
		t.Fatal(err)
	}
	crossOwner := performGatewayJobRequest(handler, http.MethodGet, "/v1/artifacts/"+artifact.ID, other.Key, "", "")
	if crossOwner.Code != http.StatusNotFound || !strings.Contains(crossOwner.Body.String(), "resource_not_found") {
		t.Fatalf("cross-owner status=%d body=%s", crossOwner.Code, crossOwner.Body.String())
	}
	noScopeRequest := request
	noScopeRequest.Name = "artifact scope denied"
	noScopeRequest.Scopes = []string{controlplane.GatewayScopeInvoke, controlplane.GatewayScopeJobsRead}
	noScope, err := control.CreateAPIKey(context.Background(), "test", noScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	denied := performGatewayJobRequest(handler, http.MethodGet, "/v1/artifacts/"+artifact.ID, noScope.Key, "", "")
	if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "policy_not_allowed") {
		t.Fatalf("missing scope status=%d body=%s", denied.Code, denied.Body.String())
	}
	deleteResponse := performGatewayJobRequest(handler, http.MethodDelete, "/v1/artifacts/"+artifact.ID, rotated.Key, "", "")
	if deleteResponse.Code != http.StatusAccepted || !strings.Contains(deleteResponse.Body.String(), controlplane.ArtifactStatusDeleteRequested) {
		t.Fatalf("delete status=%d body=%s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if processed, err := control.RunArtifactDeletionWorkerOnce(context.Background(), 1); err != nil || processed != 1 {
		t.Fatalf("delete worker processed=%d err=%v", processed, err)
	}
	deletedContent := performGatewayArtifactRequest(handler, http.MethodGet, "/v1/artifacts/"+artifact.ID+"/content", rotated.Key, "")
	if deletedContent.Code != http.StatusGone || !strings.Contains(deletedContent.Body.String(), "artifact_unavailable") {
		t.Fatalf("deleted content status=%d body=%s", deletedContent.Code, deletedContent.Body.String())
	}
}

func TestPublicArtifactResponseExposesOnlyDeliveredCustomerSinkReference(t *testing.T) {
	artifact := controlplane.Artifact{
		ID: "artifact-customer", Policy: controlplane.GatewayArtifactPolicyCustomerSink,
		Status: controlplane.ArtifactStatusDelivered, ExternalReference: "s3://customer-bucket/artifact-customer",
	}
	response := newPublicArtifactResponse(artifact)
	if response.ExternalReference != artifact.ExternalReference {
		t.Fatalf("external reference=%q", response.ExternalReference)
	}
	artifact.Policy = controlplane.GatewayArtifactPolicyMetadataOnly
	if response := newPublicArtifactResponse(artifact); response.ExternalReference != "" {
		t.Fatalf("metadata-only reference leaked=%q", response.ExternalReference)
	}
	artifact.Policy = controlplane.GatewayArtifactPolicyCustomerSink
	artifact.Status = controlplane.ArtifactStatusDeliveryFailed
	if response := newPublicArtifactResponse(artifact); response.ExternalReference != "" {
		t.Fatalf("failed delivery reference leaked=%q", response.ExternalReference)
	}
	artifact.Policy = controlplane.GatewayArtifactPolicyProxyOnly
	artifact.Status = controlplane.ArtifactStatusReady
	artifact.ExternalReference = "provider-secret-reference"
	response = newPublicArtifactResponse(artifact)
	if response.ExternalReference != "" || response.Links["content"] == "" {
		t.Fatalf("proxy response=%+v", response)
	}
}

func TestGatewayProxyArtifactContentAndUnavailablePlugin(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	if _, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "proxy-image-model", Name: "Proxy image model", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	keyRequest := durableJobAPIKeyRequest("proxy artifact owner")
	keyRequest.ModelAllowlist = []string{"proxy-image-model"}
	keyRequest.ArtifactPolicy = controlplane.GatewayArtifactPolicyProxyOnly
	keyRequest.Scopes = append(keyRequest.Scopes, controlplane.GatewayScopeArtifactsRead)
	owner, err := control.CreateAPIKey(context.Background(), "test", keyRequest)
	if err != nil {
		t.Fatal(err)
	}
	created := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", owner.Key, "proxy-artifact-idem", `{"model":"proxy-image-model","operation":"image_generation","modality":"image","input":{"prompt":"synthetic"}}`)
	var job publicAIJobResponse
	if created.Code != http.StatusAccepted || json.Unmarshal(created.Body.Bytes(), &job) != nil {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	attempt, err := control.BeginAIAttempt(context.Background(), job.OperationID, 1, controlplane.GatewayProvider{ID: "provider-proxy-http"})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("proxied-http-image")
	if err := control.SetArtifactProxy(&serverArtifactProxy{providerID: "provider-proxy-http", payload: payload}); err != nil {
		t.Fatal(err)
	}
	artifact, err := control.CreateArtifactFromReader(context.Background(), controlplane.ArtifactCreateInput{
		OperationID: job.OperationID, JobID: job.ID, AttemptID: attempt.ID, Role: controlplane.ArtifactRoleFinal,
		Policy: controlplane.GatewayArtifactPolicyProxyOnly, MediaType: "image/png", ExternalReference: "provider-internal-reference",
		ExpectedSizeBytes: int64(len(payload)), MaxBytes: 1024,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata := performGatewayJobRequest(handler, http.MethodGet, "/v1/artifacts/"+artifact.ID, owner.Key, "", "")
	if metadata.Code != http.StatusOK || strings.Contains(metadata.Body.String(), "provider-internal-reference") || !strings.Contains(metadata.Body.String(), "/content") {
		t.Fatalf("metadata status=%d body=%s", metadata.Code, metadata.Body.String())
	}
	content := performGatewayArtifactRequest(handler, http.MethodGet, "/v1/artifacts/"+artifact.ID+"/content", owner.Key, "bytes=2-7")
	if content.Code != http.StatusPartialContent || !bytes.Equal(content.Body.Bytes(), payload[2:8]) || content.Header().Get("Content-Range") != "bytes 2-7/18" {
		t.Fatalf("content status=%d range=%q body=%q", content.Code, content.Header().Get("Content-Range"), content.Body.Bytes())
	}

	missingAttempt, err := control.BeginAIAttempt(context.Background(), job.OperationID, 2, controlplane.GatewayProvider{ID: "provider-proxy-missing"})
	if err != nil {
		t.Fatal(err)
	}
	missing, err := control.CreateArtifactFromReader(context.Background(), controlplane.ArtifactCreateInput{
		OperationID: job.OperationID, JobID: job.ID, AttemptID: missingAttempt.ID, Role: controlplane.ArtifactRolePreview,
		Policy: controlplane.GatewayArtifactPolicyProxyOnly, MediaType: "image/png", ExternalReference: "provider-missing-reference",
		ExpectedSizeBytes: int64(len(payload)), MaxBytes: 1024,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	unavailable := performGatewayArtifactRequest(handler, http.MethodGet, "/v1/artifacts/"+missing.ID+"/content", owner.Key, "")
	if unavailable.Code != http.StatusServiceUnavailable || !strings.Contains(unavailable.Body.String(), "artifact_proxy_unavailable") {
		t.Fatalf("unavailable status=%d body=%s", unavailable.Code, unavailable.Body.String())
	}
}

type serverArtifactProxy struct {
	providerID string
	payload    []byte
}

func (p *serverArtifactProxy) ProviderID() string { return p.providerID }

func (p *serverArtifactProxy) OpenArtifact(_ context.Context, _ controlplane.ArtifactProxyRequest, requested *controlplane.ArtifactByteRange) (controlplane.ArtifactRead, error) {
	offset := int64(0)
	length := int64(len(p.payload))
	if requested != nil {
		offset = requested.Offset
		length = requested.Length
		if length == 0 || length > int64(len(p.payload))-offset {
			length = int64(len(p.payload)) - offset
		}
	}
	return controlplane.ArtifactRead{
		Body: io.NopCloser(bytes.NewReader(p.payload[offset : offset+length])), Offset: offset,
		SizeBytes: length, TotalBytes: int64(len(p.payload)),
	}, nil
}

func performGatewayArtifactRequest(handler http.Handler, method, target, key, byteRange string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	request.Header.Set("Authorization", "Bearer "+key)
	if byteRange != "" {
		request.Header.Set("Range", byteRange)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
