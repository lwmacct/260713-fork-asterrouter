package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestGatewayUploadCreatesOwnedInputArtifactAndReplaysIdempotently(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	if err := control.SetArtifactStore(controlplane.NewMemoryArtifactStore()); err != nil {
		t.Fatal(err)
	}
	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "upload caller", ModelAllowlist: []string{"upload-model"},
		Scopes:            []string{controlplane.GatewayScopeInvoke, controlplane.GatewayScopeArtifactsWrite, controlplane.GatewayScopeArtifactsRead, controlplane.GatewayScopeJobsRead},
		AllowedModalities: []string{controlplane.GatewayModalityImage}, AllowedOperations: []string{controlplane.GatewayOperationImageGeneration},
		LanePolicy:     controlplane.GatewayLanePolicyDurableOnly,
		ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("synthetic-upload-content")
	request := func(idempotency string, body []byte) *httptest.ResponseRecorder {
		bodyDigest := sha256.Sum256(body)
		req := httptest.NewRequest(http.MethodPost, "/v1/uploads", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key.Key)
		req.Header.Set("Idempotency-Key", idempotency)
		req.Header.Set("Content-Type", "image/png")
		req.Header.Set("X-Checksum-SHA256", hex.EncodeToString(bodyDigest[:]))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	first := request("upload-idem-1", payload)
	if first.Code != http.StatusCreated || first.Header().Get("Location") == "" {
		t.Fatalf("first status=%d headers=%v body=%s", first.Code, first.Header(), first.Body.String())
	}
	var uploaded publicUploadResponse
	if err := json.Unmarshal(first.Body.Bytes(), &uploaded); err != nil || uploaded.ID == "" || uploaded.Status != controlplane.ArtifactStatusReady || uploaded.Offset != int64(len(payload)) {
		t.Fatalf("upload=%+v err=%v body=%s", uploaded, err, first.Body.String())
	}
	hold, found, err := control.BillingHoldForOperation(context.Background(), uploaded.OperationID)
	if err != nil || !found || hold.Status != controlplane.BillingHoldStatusReleased {
		t.Fatalf("upload billing hold=%+v found=%t err=%v", hold, found, err)
	}
	content := performGatewayArtifactRequest(handler, http.MethodGet, "/v1/artifacts/"+uploaded.ArtifactID+"/content", key.Key, "")
	if content.Code != http.StatusOK || !bytes.Equal(content.Body.Bytes(), payload) {
		t.Fatalf("content status=%d body=%q", content.Code, content.Body.String())
	}
	replay := request("upload-idem-1", payload)
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotent-Replayed") != "true" || !strings.Contains(replay.Body.String(), uploaded.ID) {
		t.Fatalf("replay status=%d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	}
	conflictPayload := []byte("different-upload-content")
	conflict := request("upload-idem-1", conflictPayload)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "idempotency_conflict") {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	metadata := httptest.NewRequest(http.MethodGet, "/v1/uploads/"+uploaded.ID, nil)
	metadata.Header.Set("Authorization", "Bearer "+key.Key)
	metadataRec := httptest.NewRecorder()
	handler.ServeHTTP(metadataRec, metadata)
	if metadataRec.Code != http.StatusOK || !strings.Contains(metadataRec.Body.String(), uploaded.ArtifactID) {
		t.Fatalf("metadata status=%d body=%s", metadataRec.Code, metadataRec.Body.String())
	}
	if _, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "upload-model", Name: "Upload model", Modality: controlplane.GatewayModalityImage, Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	job := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", key.Key, "upload-input-job", `{"model":"upload-model","operation":"image_generation","modality":"image","input":{"prompt":"use-upload","artifact_id":"`+uploaded.ArtifactID+`"}}`)
	if job.Code != http.StatusAccepted || !strings.Contains(job.Body.String(), `"status":"queued"`) {
		t.Fatalf("artifact reference job status=%d body=%s", job.Code, job.Body.String())
	}
	otherKey, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "other caller", ModelAllowlist: []string{"upload-model"}, Scopes: []string{controlplane.GatewayScopeInvoke},
		AllowedModalities: []string{controlplane.GatewayModalityImage}, AllowedOperations: []string{controlplane.GatewayOperationImageGeneration},
		LanePolicy: controlplane.GatewayLanePolicyDurableOnly, ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	})
	if err != nil {
		t.Fatal(err)
	}
	foreign := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", otherKey.Key, "foreign-input-job", `{"model":"upload-model","operation":"image_generation","modality":"image","input":{"prompt":"foreign","artifact_id":"`+uploaded.ArtifactID+`"}}`)
	if foreign.Code != http.StatusNotFound || !strings.Contains(foreign.Body.String(), "resource_not_found") {
		t.Fatalf("foreign artifact status=%d body=%s", foreign.Code, foreign.Body.String())
	}
}

func TestGatewayUploadFailsClosedWithoutStoreOrChecksum(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "upload caller", ModelAllowlist: []string{"upload-model"},
		Scopes: []string{controlplane.GatewayScopeArtifactsWrite}, ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/uploads", bytes.NewBufferString("payload"))
	req.Header.Set("Authorization", "Bearer "+key.Key)
	req.Header.Set("Idempotency-Key", "upload-no-store")
	req.Header.Set("Content-Type", "application/octet-stream")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "artifact_store_unavailable") {
		t.Fatalf("no-store status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := control.SetArtifactStore(controlplane.NewMemoryArtifactStore()); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/uploads", bytes.NewBufferString("payload"))
	req.Header.Set("Authorization", "Bearer "+key.Key)
	req.Header.Set("Idempotency-Key", "upload-no-checksum")
	req.Header.Set("Content-Type", "application/octet-stream")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_request_error") {
		t.Fatalf("no-checksum status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/uploads", bytes.NewBufferString("payload"))
	req.Header.Set("Authorization", "Bearer "+key.Key)
	req.Header.Set("Idempotency-Key", "upload-offset")
	req.Header.Set("Upload-Offset", "1")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented || !strings.Contains(rec.Body.String(), "upload_resumable_not_supported") {
		t.Fatalf("offset status=%d body=%s", rec.Code, rec.Body.String())
	}
}
