package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

func TestGatewayDurableJobLifecycleAndIdempotency(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	if _, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "public-image-job", Name: "Public image job", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatalf("CreateGatewayModel(): %v", err)
	}
	createdKey, err := control.CreateAPIKey(context.Background(), "test", durableJobAPIKeyRequest("job owner"))
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	body := `{"model":"public-image-job","operation":"image_generation","modality":"image","input":{"prompt":"synthetic","count":1}}`

	first := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", createdKey.Key, "job-http-idem-1", body)
	if first.Code != http.StatusAccepted || first.Header().Get("Location") == "" || first.Header().Get("X-AsterRouter-Operation-ID") == "" {
		t.Fatalf("first status=%d headers=%v body=%s", first.Code, first.Header(), first.Body.String())
	}
	var accepted publicAIJobResponse
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode accepted job: %v", err)
	}
	if accepted.ID == "" || accepted.Status != controlplane.AIJobStatusQueued || accepted.Capability.Modality != "image" || accepted.ArtifactPolicy != controlplane.GatewayArtifactPolicyTemporary {
		t.Fatalf("accepted job=%+v", accepted)
	}
	if strings.Contains(first.Body.String(), "synthetic") || strings.Contains(first.Body.String(), createdKey.Key) {
		t.Fatalf("job response leaked request or credential: %s", first.Body.String())
	}

	replayBody := `{
  "input": {"count": 1, "prompt": "synthetic"},
  "modality": "image",
  "operation": "image_generation",
  "model": "public-image-job"
}`
	replay := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", createdKey.Key, "job-http-idem-1", replayBody)
	var replayed publicAIJobResponse
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotent-Replayed") != "true" || json.Unmarshal(replay.Body.Bytes(), &replayed) != nil || replayed.ID != accepted.ID {
		t.Fatalf("replay status=%d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	}
	conflict := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", createdKey.Key, "job-http-idem-1", strings.Replace(body, "synthetic", "different", 1))
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "idempotency_conflict") {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	rotated, err := control.RotateAPIKey(context.Background(), "test", createdKey.Record.ID)
	if err != nil {
		t.Fatalf("RotateAPIKey(): %v", err)
	}
	retiredKeyGet := performGatewayJobRequest(handler, http.MethodGet, "/v1/jobs/"+accepted.ID, createdKey.Key, "", "")
	if retiredKeyGet.Code != http.StatusUnauthorized {
		t.Fatalf("retired key get status=%d body=%s", retiredKeyGet.Code, retiredKeyGet.Body.String())
	}
	get := performGatewayJobRequest(handler, http.MethodGet, "/v1/jobs/"+accepted.ID, rotated.Key, "", "")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), accepted.ID) {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}
	cancel := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs/"+accepted.ID+"/cancel", rotated.Key, "", "")
	if cancel.Code != http.StatusOK || !strings.Contains(cancel.Body.String(), `"status":"canceled"`) {
		t.Fatalf("cancel status=%d body=%s", cancel.Code, cancel.Body.String())
	}
	cancelReplay := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs/"+accepted.ID+"/cancel", rotated.Key, "", "")
	if cancelReplay.Code != http.StatusOK || !strings.Contains(cancelReplay.Body.String(), `"status_version":2`) {
		t.Fatalf("cancel replay status=%d body=%s", cancelReplay.Code, cancelReplay.Body.String())
	}
}

func TestGatewayJobActionCreatesOwnedIdempotentChildJob(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	if _, err := control.CreateGatewayModel(context.Background(), "action-image", controlplane.GatewayModelRequest{
		ModelID: "action-image", Name: "Action image", Modality: controlplane.GatewayModalityImage, Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "action owner", ModelAllowlist: []string{"action-image"},
		Scopes:            []string{controlplane.GatewayScopeInvoke, controlplane.GatewayScopeJobsRead, controlplane.GatewayScopeJobsActions},
		AllowedModalities: []string{controlplane.GatewayModalityImage}, AllowedOperations: []string{controlplane.GatewayOperationImageGeneration},
		LanePolicy: controlplane.GatewayLanePolicyDurableOnly, ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceResponse := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", key.Key, "action-source-idem", `{"model":"action-image","operation":"image_generation","modality":"image","input":{"prompt":"source"}}`)
	if sourceResponse.Code != http.StatusAccepted {
		t.Fatalf("source status=%d body=%s", sourceResponse.Code, sourceResponse.Body.String())
	}
	var source publicAIJobResponse
	if err := json.Unmarshal(sourceResponse.Body.Bytes(), &source); err != nil {
		t.Fatal(err)
	}
	claimed, err := control.ClaimReadyAIJobs(context.Background(), "action-test-worker", time.Minute, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%+v err=%v", claimed, err)
	}
	running, err := control.TransitionAIJob(context.Background(), claimed[0].ID, claimed[0].StatusVersion, claimed[0].FenceToken, controlplane.AIJobStatusRunning, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.TransitionAIJob(context.Background(), running.ID, running.StatusVersion, running.FenceToken, controlplane.AIJobStatusSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	actionBody := `{"action":"variation","input":{"prompt":"variation"}}`
	first := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs/"+source.ID+"/actions", key.Key, "action-child-idem", actionBody)
	if first.Code != http.StatusAccepted || first.Header().Get("Location") == "" {
		t.Fatalf("first status=%d headers=%v body=%s", first.Code, first.Header(), first.Body.String())
	}
	var child publicAIJobResponse
	if err := json.Unmarshal(first.Body.Bytes(), &child); err != nil || child.ID == source.ID || child.Status != controlplane.AIJobStatusQueued {
		t.Fatalf("child=%+v err=%v", child, err)
	}
	replay := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs/"+source.ID+"/actions", key.Key, "action-child-idem", actionBody)
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotent-Replayed") != "true" || !strings.Contains(replay.Body.String(), child.ID) {
		t.Fatalf("replay status=%d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	}
	invalid := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs/"+source.ID+"/actions", key.Key, "action-invalid", `{"action":"unknown","input":{"prompt":"variation"}}`)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "invalid_request_error") {
		t.Fatalf("invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	conflict := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs/"+source.ID+"/actions", key.Key, "action-child-idem", `{"action":"variation","input":{"prompt":"different"}}`)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "idempotency_conflict") {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	noActionKey, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "action denied", ModelAllowlist: []string{"action-image"},
		Scopes:            []string{controlplane.GatewayScopeInvoke, controlplane.GatewayScopeJobsRead},
		AllowedModalities: []string{controlplane.GatewayModalityImage}, AllowedOperations: []string{controlplane.GatewayOperationImageGeneration},
		LanePolicy: controlplane.GatewayLanePolicyDurableOnly, ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	})
	if err != nil {
		t.Fatal(err)
	}
	denied := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs/"+source.ID+"/actions", noActionKey.Key, "action-denied", actionBody)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("denied status=%d body=%s", denied.Code, denied.Body.String())
	}
}

func TestGatewayMediaGenerationRoutesUseDurableJobContract(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	for _, test := range []struct {
		path      string
		model     string
		modality  string
		operation string
		body      string
		idem      string
	}{
		{path: "/v1/videos/generations", model: "public-video", modality: controlplane.GatewayModalityVideo, operation: controlplane.GatewayOperationVideoGeneration, body: `{"model":"public-video","prompt":"synthetic","duration_seconds":2}`, idem: "video-route-idem"},
		{path: "/v1/audio/generations", model: "public-audio", modality: controlplane.GatewayModalityAudio, operation: controlplane.GatewayOperationAudioGeneration, body: `{"model":"public-audio","prompt":"synthetic","duration_seconds":1}`, idem: "audio-route-idem"},
	} {
		t.Run(test.modality, func(t *testing.T) {
			if _, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
				ModelID: test.model, Name: test.modality, Modality: test.modality, Status: controlplane.GatewayModelStatusActive,
			}); err != nil {
				t.Fatal(err)
			}
			keyRequest := controlplane.APIKeyCreateRequest{
				Name: "media route " + test.modality, ModelAllowlist: []string{test.model},
				Scopes:            []string{controlplane.GatewayScopeInvoke, controlplane.GatewayScopeJobsRead, controlplane.GatewayScopeJobsCancel},
				AllowedModalities: []string{test.modality}, AllowedOperations: []string{test.operation},
				LanePolicy: controlplane.GatewayLanePolicyDurableOnly, ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
			}
			key, err := control.CreateAPIKey(context.Background(), "test", keyRequest)
			if err != nil {
				t.Fatal(err)
			}
			response := performGatewayJobRequest(handler, http.MethodPost, test.path, key.Key, test.idem, test.body)
			if response.Code != http.StatusAccepted {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var job publicAIJobResponse
			if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
				t.Fatal(err)
			}
			if job.Capability.Modality != test.modality || job.Capability.Operation != test.operation || job.Status != controlplane.AIJobStatusQueued {
				t.Fatalf("job=%+v", job)
			}
		})
	}
}

func TestGatewayMediaDirectModesFailClosedWithoutCreatingJobs(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	if _, err := control.CreateGatewayModel(context.Background(), "direct-video", controlplane.GatewayModelRequest{
		ModelID: "direct-video", Name: "Direct video", Modality: controlplane.GatewayModalityVideo, Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "direct video", ModelAllowlist: []string{"direct-video"},
		Scopes:            []string{controlplane.GatewayScopeInvoke, controlplane.GatewayScopeJobsRead},
		AllowedModalities: []string{controlplane.GatewayModalityVideo}, AllowedOperations: []string{controlplane.GatewayOperationVideoGeneration},
		LanePolicy: controlplane.GatewayLanePolicyDirectOnly, ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "blocking", body: `{"model":"direct-video","prompt":"synthetic","duration_seconds":1,"response_mode":"blocking"}`},
		{name: "stream", body: `{"model":"direct-video","prompt":"synthetic","duration_seconds":1,"response_mode":"stream"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := performGatewayJobRequest(handler, http.MethodPost, "/v1/videos/generations", key.Key, "direct-"+test.name, test.body)
			if response.Code != http.StatusServiceUnavailable || (!strings.Contains(response.Body.String(), "unsupported_capability") && !strings.Contains(response.Body.String(), "route_unavailable")) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			jobs, listErr := control.ListAIJobsAdmin(context.Background(), controlplane.AIJobQuery{Limit: 10})
			if listErr != nil || len(jobs) != 0 {
				t.Fatalf("direct mode created jobs=%+v err=%v", jobs, listErr)
			}
		})
	}
}

func TestGatewayMediaGenerationRouteFailsClosedWithoutAdapter(t *testing.T) {
	handler, control := newTestRuntimeWithDurableAdmission(t, RuntimeConfig{}, nil)
	if _, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "unavailable-video", Name: "Unavailable video", Modality: controlplane.GatewayModalityVideo, Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "unavailable video", ModelAllowlist: []string{"unavailable-video"}, Scopes: []string{controlplane.GatewayScopeInvoke},
		AllowedModalities: []string{controlplane.GatewayModalityVideo}, AllowedOperations: []string{controlplane.GatewayOperationVideoGeneration},
		LanePolicy: controlplane.GatewayLanePolicyDurableOnly, ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := performGatewayJobRequest(handler, http.MethodPost, "/v1/videos/generations", key.Key, "unavailable-video-idem", `{"model":"unavailable-video","prompt":"synthetic"}`)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	jobs, err := control.ClaimReadyAIJobs(context.Background(), "media-fail-closed", time.Minute, 1)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	traces, err := control.ListGatewayTraces(context.Background(), 10)
	if err != nil || len(traces) != 1 || traces[0].RouteReason != controlplane.DurableAIJobCapabilityRuntimeUnavailable || !strings.Contains(traces[0].RouteAttempts, controlplane.DurableAIJobCapabilityRuntimeUnavailable) {
		t.Fatalf("traces=%+v err=%v", traces, err)
	}
}

func TestGatewayDurableJobQueueBackpressure(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	if _, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "limited-image-job", Name: "Limited image job", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	if err := control.SetAIJobAdmissionLimits(controlplane.AIJobAdmissionLimits{Principal: 1}); err != nil {
		t.Fatal(err)
	}
	request := durableJobAPIKeyRequest("limited job owner")
	request.ModelAllowlist = []string{"limited-image-job"}
	owner, err := control.CreateAPIKey(context.Background(), "test", request)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"limited-image-job","operation":"image_generation","modality":"image","input":{"prompt":"synthetic"}}`
	first := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", owner.Key, "limited-job-first", body)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	second := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", owner.Key, "limited-job-second", body)
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" || !strings.Contains(second.Body.String(), "queue_capacity_exceeded") {
		t.Fatalf("second status=%d headers=%v body=%s", second.Code, second.Header(), second.Body.String())
	}
}

func TestGatewayDurableJobFailsClosedWithoutExecutableAdapter(t *testing.T) {
	handler, control := newTestRuntimeWithDurableAdmission(t, RuntimeConfig{}, nil)
	if _, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "unavailable-image-job", Name: "Unavailable image job", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	request := durableJobAPIKeyRequest("unavailable job owner")
	request.ModelAllowlist = []string{"unavailable-image-job"}
	owner, err := control.CreateAPIKey(context.Background(), "test", request)
	if err != nil {
		t.Fatal(err)
	}
	response := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", owner.Key, "unavailable-job", `{"model":"unavailable-image-job","operation":"image_generation","modality":"image","input":{"prompt":"synthetic"}}`)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "unsupported_capability") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	jobs, err := control.ClaimReadyAIJobs(context.Background(), "fail-closed-test", time.Minute, 1)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
}

func TestGatewayDurableJobCapabilityRejectionPersistsInternalTraceEvidence(t *testing.T) {
	evaluation := controlplane.DurableAIJobSupportEvaluation{
		GatewayModelID: "gateway-model-internal", RouteGroup: "default", HasRoutes: true,
		RejectionReason: controlplane.DurableAIJobCapabilityAllAdaptersExcluded,
		Exclusions: []controlplane.GatewayCandidateExclusion{{
			RouteID: "route-internal", ProviderID: "provider-internal", ProviderAccountID: "account-internal",
			UpstreamModel: "upstream-internal", Reason: controlplane.DurableAIJobCapabilityModalityUnsupported,
		}},
	}
	handler, control := newTestRuntimeWithDurableAdmission(t, RuntimeConfig{}, rejectingDurableAIJobAdmission{evaluation: evaluation})
	if _, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "trace-video-job", Name: "Trace video job", Modality: controlplane.GatewayModalityVideo, Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "trace video owner", ModelAllowlist: []string{"trace-video-job"}, Scopes: []string{controlplane.GatewayScopeInvoke},
		AllowedModalities: []string{controlplane.GatewayModalityVideo}, AllowedOperations: []string{controlplane.GatewayOperationVideoGeneration},
		LanePolicy: controlplane.GatewayLanePolicyDurableOnly, ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", key.Key, "trace-capability-idem", `{"model":"trace-video-job","operation":"video_generation","modality":"video","input":{"prompt":"synthetic"}}`)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "unsupported_capability") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	for _, internal := range []string{"provider-internal", "account-internal", "route-internal", controlplane.DurableAIJobCapabilityModalityUnsupported} {
		if strings.Contains(response.Body.String(), internal) {
			t.Fatalf("public response leaked %q: %s", internal, response.Body.String())
		}
	}
	traces, err := control.ListGatewayTraces(context.Background(), 10)
	if err != nil || len(traces) != 1 {
		t.Fatalf("traces=%+v err=%v", traces, err)
	}
	trace := traces[0]
	if trace.APIKeyID != key.Record.ID || trace.ErrorType != "unsupported_capability" || trace.RouteReason != controlplane.DurableAIJobCapabilityAllAdaptersExcluded || trace.RequestFingerprint == "" || trace.GatewayModelID != evaluation.GatewayModelID {
		t.Fatalf("trace=%+v", trace)
	}
	for _, evidence := range []string{"provider-internal", "account-internal", "route-internal", controlplane.DurableAIJobCapabilityModalityUnsupported} {
		if !strings.Contains(trace.RouteAttempts, evidence) {
			t.Fatalf("trace evidence missing %q: %s", evidence, trace.RouteAttempts)
		}
	}
}

func TestGatewayDurableJobAuthorizationAndNonDisclosure(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	if _, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "isolated-image-job", Name: "Isolated image job", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	ownerRequest := durableJobAPIKeyRequest("owner")
	ownerRequest.ModelAllowlist = []string{"isolated-image-job"}
	owner, err := control.CreateAPIKey(context.Background(), "test", ownerRequest)
	if err != nil {
		t.Fatal(err)
	}
	otherRequest := ownerRequest
	otherRequest.Name = "other principal"
	other, err := control.CreateAPIKey(context.Background(), "test", otherRequest)
	if err != nil {
		t.Fatal(err)
	}
	noReadRequest := ownerRequest
	noReadRequest.Name = "no read scope"
	noReadRequest.Scopes = []string{controlplane.GatewayScopeInvoke, controlplane.GatewayScopeJobsCancel}
	noRead, err := control.CreateAPIKey(context.Background(), "test", noReadRequest)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"isolated-image-job","operation":"image_generation","modality":"image","input":{"prompt":"synthetic"}}`
	created := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", owner.Key, "isolated-job-idem", body)
	var job publicAIJobResponse
	if created.Code != http.StatusAccepted || json.Unmarshal(created.Body.Bytes(), &job) != nil {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}

	for _, test := range []struct {
		name     string
		key      string
		code     int
		typeName string
	}{
		{name: "other principal", key: other.Key, code: http.StatusNotFound, typeName: "resource_not_found"},
		{name: "missing read scope", key: noRead.Key, code: http.StatusForbidden, typeName: "policy_not_allowed"},
		{name: "missing credential", key: "", code: http.StatusUnauthorized, typeName: "invalid_api_key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := performGatewayJobRequest(handler, http.MethodGet, "/v1/jobs/"+job.ID, test.key, "", "")
			if response.Code != test.code || !strings.Contains(response.Body.String(), test.typeName) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}

	missingIdempotency := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", owner.Key, "", body)
	if missingIdempotency.Code != http.StatusBadRequest || !strings.Contains(missingIdempotency.Body.String(), "idempotency_key_required") {
		t.Fatalf("missing idempotency status=%d body=%s", missingIdempotency.Code, missingIdempotency.Body.String())
	}
	wrongModality := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", owner.Key, "wrong-modality-idem", strings.Replace(body, `"modality":"image"`, `"modality":"video"`, 1))
	if wrongModality.Code != http.StatusForbidden || !strings.Contains(wrongModality.Body.String(), "policy_not_allowed") {
		t.Fatalf("wrong modality status=%d body=%s", wrongModality.Code, wrongModality.Body.String())
	}
}

func durableJobAPIKeyRequest(name string) controlplane.APIKeyCreateRequest {
	return controlplane.APIKeyCreateRequest{
		Name: name, ModelAllowlist: []string{"public-image-job"},
		Scopes:            []string{controlplane.GatewayScopeInvoke, controlplane.GatewayScopeJobsRead, controlplane.GatewayScopeJobsCancel},
		AllowedModalities: []string{"image"}, AllowedOperations: []string{"image_generation"},
		LanePolicy: controlplane.GatewayLanePolicyDurableOnly, ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	}
}

func performGatewayJobRequest(handler http.Handler, method, target, key, idempotencyKey, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type rejectingDurableAIJobAdmission struct {
	evaluation controlplane.DurableAIJobSupportEvaluation
	err        error
}

func (admission rejectingDurableAIJobAdmission) SupportsDurableAIJob(context.Context, gatewaycore.CanonicalAuthContext, gatewaycore.CanonicalRequest) (bool, error) {
	return admission.evaluation.Supported, admission.err
}

func (admission rejectingDurableAIJobAdmission) EvaluateDurableAIJobSupport(context.Context, gatewaycore.CanonicalAuthContext, gatewaycore.CanonicalRequest) (controlplane.DurableAIJobSupportEvaluation, error) {
	return admission.evaluation, admission.err
}
