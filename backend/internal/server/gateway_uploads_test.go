package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestGatewayUploadCreatesOwnedInputArtifactAndReplaysIdempotently(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
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
	handler, control := newTestRuntime(t, RuntimeConfig{})
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

func TestGatewayResumableUploadChunksAreOrderedIdempotentAndOwnerScoped(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	if err := control.SetArtifactStore(controlplane.NewMemoryArtifactStore()); err != nil {
		t.Fatal(err)
	}
	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "resumable caller", ModelAllowlist: []string{"upload-model"},
		Scopes:         []string{controlplane.GatewayScopeArtifactsWrite, controlplane.GatewayScopeArtifactsRead},
		ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	})
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("resumable-content")
	totalDigest := sha256.Sum256(content)
	initRequest := httptest.NewRequest(http.MethodPost, "/v1/uploads", nil)
	initRequest.Header.Set("Authorization", "Bearer "+key.Key)
	initRequest.Header.Set("Idempotency-Key", "resumable-init")
	initRequest.Header.Set("Upload-Length", strconv.Itoa(len(content)))
	initRequest.Header.Set("Content-Type", "image/png")
	initRequest.Header.Set("X-Checksum-SHA256", hex.EncodeToString(totalDigest[:]))
	initResponse := httptest.NewRecorder()
	handler.ServeHTTP(initResponse, initRequest)
	if initResponse.Code != http.StatusCreated {
		t.Fatalf("init status=%d body=%s", initResponse.Code, initResponse.Body.String())
	}
	var session publicUploadResponse
	if err := json.Unmarshal(initResponse.Body.Bytes(), &session); err != nil || session.ID == "" || session.Status != controlplane.ArtifactStatusUploading || session.Offset != 0 {
		t.Fatalf("session=%+v err=%v body=%s", session, err, initResponse.Body.String())
	}

	chunkRequest := func(offset int, payload []byte, idempotency string) *httptest.ResponseRecorder {
		digest := sha256.Sum256(payload)
		req := httptest.NewRequest(http.MethodPatch, "/v1/uploads/"+session.ID, bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+key.Key)
		req.Header.Set("Upload-Offset", strconv.Itoa(offset))
		req.Header.Set("X-Checksum-SHA256", hex.EncodeToString(digest[:]))
		req.Header.Set("Content-Type", "image/png")
		if idempotency != "" {
			req.Header.Set("Idempotency-Key", idempotency)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	firstChunk := chunkRequest(0, content[:8], "chunk-1")
	if firstChunk.Code != http.StatusOK || firstChunk.Header().Get("Upload-Offset") != "8" {
		t.Fatalf("first chunk status=%d headers=%v body=%s", firstChunk.Code, firstChunk.Header(), firstChunk.Body.String())
	}
	retryChunk := chunkRequest(0, content[:8], "chunk-1-retry")
	if retryChunk.Code != http.StatusOK || retryChunk.Header().Get("Upload-Offset") != "8" {
		t.Fatalf("retry chunk status=%d headers=%v body=%s", retryChunk.Code, retryChunk.Header(), retryChunk.Body.String())
	}
	secondChunk := chunkRequest(8, content[8:], "chunk-2")
	if secondChunk.Code != http.StatusOK || secondChunk.Header().Get("Upload-Offset") != strconv.Itoa(len(content)) {
		t.Fatalf("second chunk status=%d headers=%v body=%s", secondChunk.Code, secondChunk.Header(), secondChunk.Body.String())
	}
	badOffset := chunkRequest(0, []byte("different"), "chunk-conflict")
	if badOffset.Code != http.StatusConflict || !strings.Contains(badOffset.Body.String(), "upload_offset_conflict") {
		t.Fatalf("bad offset status=%d body=%s", badOffset.Code, badOffset.Body.String())
	}
	completeRequest := httptest.NewRequest(http.MethodPost, "/v1/uploads/"+session.ID+"/complete", nil)
	completeRequest.Header.Set("Authorization", "Bearer "+key.Key)
	completeResponse := httptest.NewRecorder()
	handler.ServeHTTP(completeResponse, completeRequest)
	if completeResponse.Code != http.StatusOK {
		t.Fatalf("complete status=%d body=%s", completeResponse.Code, completeResponse.Body.String())
	}
	var completed publicUploadResponse
	if err := json.Unmarshal(completeResponse.Body.Bytes(), &completed); err != nil || completed.Status != controlplane.ArtifactStatusReady || completed.Offset != int64(len(content)) {
		t.Fatalf("completed=%+v err=%v body=%s", completed, err, completeResponse.Body.String())
	}
	contentResponse := performGatewayArtifactRequest(handler, http.MethodGet, "/v1/artifacts/"+session.ID+"/content", key.Key, "")
	if contentResponse.Code != http.StatusOK || !bytes.Equal(contentResponse.Body.Bytes(), content) {
		t.Fatalf("content status=%d body=%q", contentResponse.Code, contentResponse.Body.String())
	}

	// The operation now contains derived chunk artifacts. Replay must still
	// return the input upload session rather than whichever artifact was newest.
	replay := httptest.NewRequest(http.MethodPost, "/v1/uploads", nil)
	replay.Header.Set("Authorization", "Bearer "+key.Key)
	replay.Header.Set("Idempotency-Key", "resumable-init")
	replay.Header.Set("Upload-Length", strconv.Itoa(len(content)))
	replay.Header.Set("Content-Type", "image/png")
	replay.Header.Set("X-Checksum-SHA256", hex.EncodeToString(totalDigest[:]))
	replayResponse := httptest.NewRecorder()
	handler.ServeHTTP(replayResponse, replay)
	if replayResponse.Code != http.StatusOK || replayResponse.Header().Get("Idempotent-Replayed") != "true" || !strings.Contains(replayResponse.Body.String(), session.ID) {
		t.Fatalf("replay status=%d headers=%v body=%s", replayResponse.Code, replayResponse.Header(), replayResponse.Body.String())
	}

	otherKey, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "foreign resumable caller", ModelAllowlist: []string{"artifact-upload"}, Scopes: []string{controlplane.GatewayScopeArtifactsWrite}, ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	})
	if err != nil {
		t.Fatal(err)
	}
	foreignPatch := httptest.NewRequest(http.MethodPatch, "/v1/uploads/"+session.ID, bytes.NewReader([]byte("x")))
	foreignPatch.Header.Set("Authorization", "Bearer "+otherKey.Key)
	foreignPatch.Header.Set("Upload-Offset", "0")
	foreignPatch.Header.Set("X-Checksum-SHA256", strings.Repeat("0", 64))
	foreignPatchResponse := httptest.NewRecorder()
	handler.ServeHTTP(foreignPatchResponse, foreignPatch)
	if foreignPatchResponse.Code != http.StatusNotFound || !strings.Contains(foreignPatchResponse.Body.String(), "resource_not_found") {
		t.Fatalf("foreign patch status=%d body=%s", foreignPatchResponse.Code, foreignPatchResponse.Body.String())
	}
}

func TestGatewayResumableUploadRejectsIncompleteAndTotalChecksumMismatch(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	if err := control.SetArtifactStore(controlplane.NewMemoryArtifactStore()); err != nil {
		t.Fatal(err)
	}
	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "checksum caller", ModelAllowlist: []string{"artifact-upload"}, Scopes: []string{controlplane.GatewayScopeArtifactsWrite, controlplane.GatewayScopeArtifactsRead}, ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	})
	if err != nil {
		t.Fatal(err)
	}
	newSession := func(idem, checksum string, size int) publicUploadResponse {
		req := httptest.NewRequest(http.MethodPost, "/v1/uploads", nil)
		req.Header.Set("Authorization", "Bearer "+key.Key)
		req.Header.Set("Idempotency-Key", idem)
		req.Header.Set("Upload-Length", strconv.Itoa(size))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("X-Checksum-SHA256", checksum)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("init status=%d body=%s", rec.Code, rec.Body.String())
		}
		var session publicUploadResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &session); err != nil {
			t.Fatal(err)
		}
		return session
	}
	partial := []byte("partial")
	partialDigest := sha256.Sum256(partial)
	partialSession := newSession("incomplete-init", strings.Repeat("1", 64), len(partial)+2)
	patch := httptest.NewRequest(http.MethodPatch, "/v1/uploads/"+partialSession.ID, bytes.NewReader(partial))
	patch.Header.Set("Authorization", "Bearer "+key.Key)
	patch.Header.Set("Upload-Offset", "0")
	patch.Header.Set("X-Checksum-SHA256", hex.EncodeToString(partialDigest[:]))
	patchResponse := httptest.NewRecorder()
	handler.ServeHTTP(patchResponse, patch)
	if patchResponse.Code != http.StatusOK {
		t.Fatalf("partial patch status=%d body=%s", patchResponse.Code, patchResponse.Body.String())
	}
	complete := httptest.NewRequest(http.MethodPost, "/v1/uploads/"+partialSession.ID+"/complete", nil)
	complete.Header.Set("Authorization", "Bearer "+key.Key)
	completeResponse := httptest.NewRecorder()
	handler.ServeHTTP(completeResponse, complete)
	if completeResponse.Code != http.StatusConflict || !strings.Contains(completeResponse.Body.String(), "upload_incomplete") {
		t.Fatalf("incomplete status=%d body=%s", completeResponse.Code, completeResponse.Body.String())
	}

	wrongTotal := []byte("wrong-total")
	wrongSession := newSession("wrong-total-init", strings.Repeat("2", 64), len(wrongTotal))
	wrongDigest := sha256.Sum256(wrongTotal)
	wrongPatch := httptest.NewRequest(http.MethodPatch, "/v1/uploads/"+wrongSession.ID, bytes.NewReader(wrongTotal))
	wrongPatch.Header.Set("Authorization", "Bearer "+key.Key)
	wrongPatch.Header.Set("Upload-Offset", "0")
	wrongPatch.Header.Set("X-Checksum-SHA256", hex.EncodeToString(wrongDigest[:]))
	wrongPatchResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongPatchResponse, wrongPatch)
	if wrongPatchResponse.Code != http.StatusOK {
		t.Fatalf("wrong total patch status=%d body=%s", wrongPatchResponse.Code, wrongPatchResponse.Body.String())
	}
	wrongComplete := httptest.NewRequest(http.MethodPost, "/v1/uploads/"+wrongSession.ID+"/complete", nil)
	wrongComplete.Header.Set("Authorization", "Bearer "+key.Key)
	wrongCompleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongCompleteResponse, wrongComplete)
	if wrongCompleteResponse.Code != http.StatusUnprocessableEntity || !strings.Contains(wrongCompleteResponse.Body.String(), "artifact_integrity_failed") {
		t.Fatalf("wrong checksum status=%d body=%s", wrongCompleteResponse.Code, wrongCompleteResponse.Body.String())
	}
	metadata := httptest.NewRequest(http.MethodGet, "/v1/uploads/"+wrongSession.ID, nil)
	metadata.Header.Set("Authorization", "Bearer "+key.Key)
	metadataResponse := httptest.NewRecorder()
	handler.ServeHTTP(metadataResponse, metadata)
	if metadataResponse.Code != http.StatusOK || !strings.Contains(metadataResponse.Body.String(), `"status":"failed"`) {
		t.Fatalf("wrong checksum metadata status=%d body=%s", metadataResponse.Code, metadataResponse.Body.String())
	}
	hold, found, err := control.BillingHoldForOperation(context.Background(), wrongSession.OperationID)
	if err != nil || !found || hold.Status != controlplane.BillingHoldStatusReleased {
		t.Fatalf("wrong checksum billing hold=%+v found=%t err=%v", hold, found, err)
	}
}
