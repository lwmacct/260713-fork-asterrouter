package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestGatewayJobEventSSEReplaysFromCursorAndClosesAtTerminalState(t *testing.T) {
	handler, _, owner, job := gatewayJobEventTestRuntime(t)
	cancel := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs/"+job.ID+"/cancel", owner.Key, "", "")
	if cancel.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", cancel.Code, cancel.Body.String())
	}

	all := performGatewayJobEventRequest(handler, job.ID, owner.Key, "")
	if all.Code != http.StatusOK || !strings.Contains(all.Header().Get("Content-Type"), "text/event-stream") || !all.Flushed {
		t.Fatalf("all events status=%d headers=%v body=%s", all.Code, all.Header(), all.Body.String())
	}
	if !strings.Contains(all.Body.String(), "id: 1\nevent: job.queued") || !strings.Contains(all.Body.String(), "id: 2\nevent: job.cancelled") {
		t.Fatalf("all events body=%s", all.Body.String())
	}
	if strings.Contains(all.Body.String(), "client_requested") || strings.Contains(all.Body.String(), owner.Key) {
		t.Fatalf("event stream leaked internal reason or credential: %s", all.Body.String())
	}

	resumed := performGatewayJobEventRequest(handler, job.ID, owner.Key, "1")
	if resumed.Code != http.StatusOK || strings.Contains(resumed.Body.String(), "job.queued") || !strings.Contains(resumed.Body.String(), "id: 2\nevent: job.cancelled") {
		t.Fatalf("resumed events status=%d body=%s", resumed.Code, resumed.Body.String())
	}
}

func TestGatewayJobEventSSEStreamsLiveCancellation(t *testing.T) {
	handler, _, owner, job := gatewayJobEventTestRuntime(t)
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancelContext := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelContext()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/jobs/"+job.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+owner.Key)
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Last-Event-ID", "1")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("stream status=%d body=%s", response.StatusCode, body)
	}

	cancelRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/jobs/"+job.ID+"/cancel", nil)
	if err != nil {
		t.Fatal(err)
	}
	cancelRequest.Header.Set("Authorization", "Bearer "+owner.Key)
	cancelResponse, err := server.Client().Do(cancelRequest)
	if err != nil {
		t.Fatalf("cancel job: %v", err)
	}
	_, _ = io.Copy(io.Discard, cancelResponse.Body)
	_ = cancelResponse.Body.Close()
	if cancelResponse.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d", cancelResponse.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read event stream: %v", err)
	}
	if strings.Contains(string(body), "job.queued") || !strings.Contains(string(body), "id: 2\nevent: job.cancelled") {
		t.Fatalf("live event body=%s", body)
	}
}

func TestGatewayJobEventSSEPublishesAvailableArtifactWithoutChangingCursor(t *testing.T) {
	handler, control, owner, job := gatewayJobEventTestRuntime(t)
	if err := control.SetArtifactStore(controlplane.NewMemoryArtifactStore()); err != nil {
		t.Fatal(err)
	}
	artifact, err := control.CreateArtifactFromReader(context.Background(), controlplane.ArtifactCreateInput{
		OperationID: job.OperationID, JobID: job.ID, Role: controlplane.ArtifactRoleFinal,
		Policy: controlplane.GatewayArtifactPolicyTemporary, MediaType: "image/png", StoreDriver: controlplane.ArtifactStoreDriverMemory,
		ExpectedSizeBytes: 5, MaxBytes: 16,
	}, strings.NewReader("image"))
	if err != nil {
		t.Fatalf("CreateArtifactFromReader(): %v", err)
	}
	cancel := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs/"+job.ID+"/cancel", owner.Key, "", "")
	if cancel.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", cancel.Code, cancel.Body.String())
	}

	stream := performGatewayJobEventRequest(handler, job.ID, owner.Key, "1")
	body := stream.Body.String()
	if stream.Code != http.StatusOK || !strings.Contains(body, "event: job.artifact.available") || !strings.Contains(body, artifact.ID) || !strings.Contains(body, `"status":"ready"`) {
		t.Fatalf("artifact event status=%d body=%s", stream.Code, body)
	}
	if strings.Contains(body, "id: "+artifact.ID) || !strings.Contains(body, "id: 2\nevent: job.cancelled") {
		t.Fatalf("artifact event changed job cursor: %s", body)
	}
}

func TestGatewayJobEventSSEReplaysDurableProgressWithoutChangingStatusCursor(t *testing.T) {
	handler, control, owner, publicJob := gatewayJobEventTestRuntime(t)
	ctx := context.Background()
	claimed, err := control.ClaimReadyAIJobs(ctx, "progress-sse-worker", time.Minute, 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != publicJob.ID {
		t.Fatalf("claimed=%+v err=%v", claimed, err)
	}
	job := claimed[0]
	attempt, err := control.BeginAIAttempt(ctx, job.OperationID, 1, controlplane.GatewayProvider{
		ID: "progress-provider", AccountID: "progress-account", AdapterID: "progress-adapter",
		RouteID: "progress-route", UpstreamModel: "progress-upstream",
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared, _, err := control.PrepareAIAttemptDispatch(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	submitted, changed, err := control.MarkAIAttemptDispatchSubmitted(ctx, prepared.ID, prepared.DispatchVersion, time.Now().UTC().Add(time.Minute))
	if err != nil || !changed {
		t.Fatalf("submitted=%+v changed=%t err=%v", submitted, changed, err)
	}
	bound, changed, err := control.BindAIAttemptProviderTask(ctx, submitted.ID, submitted.DispatchVersion, controlplane.ProviderTaskReference{
		ProviderTaskID: "progress-provider-task", Status: "running",
	}, time.Now().UTC().Add(time.Minute))
	if err != nil || !changed {
		t.Fatalf("bound=%+v changed=%t err=%v", bound, changed, err)
	}
	percent := 35
	progress, created, err := control.RecordAIJobProgress(ctx, job, bound, controlplane.ProviderProgressObservation{Sequence: 7, Percent: &percent, Stage: "rendering"})
	if err != nil || !created {
		t.Fatalf("progress=%+v created=%t err=%v", progress, created, err)
	}
	running, err := control.TransitionAIJob(ctx, job.ID, job.StatusVersion, job.FenceToken, controlplane.AIJobStatusRunning, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.TransitionAIJob(ctx, running.ID, running.StatusVersion, running.FenceToken, controlplane.AIJobStatusSucceeded, ""); err != nil {
		t.Fatal(err)
	}

	stream := performGatewayJobEventRequest(handler, job.ID, owner.Key, "3")
	body := stream.Body.String()
	if stream.Code != http.StatusOK || !strings.Contains(body, "event: job.progress") || !strings.Contains(body, progress.ID) || !strings.Contains(body, `"percent":35`) || !strings.Contains(body, `"stage":"rendering"`) {
		t.Fatalf("progress stream status=%d body=%s", stream.Code, body)
	}
	if strings.Contains(body, "progress-provider-task") || strings.Contains(body, "id: "+progress.ID) || !strings.Contains(body, "id: 4\nevent: job.completed") {
		t.Fatalf("progress stream leaked provider state or changed cursor: %s", body)
	}
	replayed := performGatewayJobEventRequest(handler, job.ID, owner.Key, "3")
	if replayed.Code != http.StatusOK || !strings.Contains(replayed.Body.String(), progress.ID) {
		t.Fatalf("replayed progress status=%d body=%s", replayed.Code, replayed.Body.String())
	}
}

func TestArtifactAvailableEventWaitsForCustomerSinkDelivery(t *testing.T) {
	artifact := controlplane.Artifact{ID: "artifact-customer", Policy: controlplane.GatewayArtifactPolicyCustomerSink, Status: controlplane.ArtifactStatusReady}
	if artifactAvailableForJobEvent(artifact) {
		t.Fatal("customer sink artifact became available before delivery")
	}
	artifact.Status = controlplane.ArtifactStatusDelivered
	if !artifactAvailableForJobEvent(artifact) {
		t.Fatal("delivered customer sink artifact is not available")
	}
}

func TestGatewayJobEventSSERevalidatesCredentialDuringConnection(t *testing.T) {
	handler, control, owner, job := gatewayJobEventTestRuntime(t)
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancelContext := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelContext()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/jobs/"+job.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+owner.Key)
	request.Header.Set("Last-Event-ID", "1")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	defer response.Body.Close()
	if err := control.DisableAPIKey(context.Background(), "test", owner.Record.ID); err != nil {
		t.Fatalf("DisableAPIKey(): %v", err)
	}
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatalf("read closed event stream: %v", err)
	}

	reconnect, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/jobs/"+job.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	reconnect.Header.Set("Authorization", "Bearer "+owner.Key)
	reconnectResponse, err := server.Client().Do(reconnect)
	if err != nil {
		t.Fatalf("reconnect event stream: %v", err)
	}
	defer reconnectResponse.Body.Close()
	if reconnectResponse.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(reconnectResponse.Body)
		t.Fatalf("reconnect status=%d body=%s", reconnectResponse.StatusCode, body)
	}
}

func TestGatewayJobEventSSEEnforcesCredentialConcurrencyAndReleasesOnDisconnect(t *testing.T) {
	handler, _, owner, job := gatewayJobEventTestRuntimeWithKey(t, func(request *controlplane.APIKeyCreateRequest) {
		request.ConcurrencyLimit = 1
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	firstRequest, err := http.NewRequest(http.MethodGet, server.URL+"/v1/jobs/"+job.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	firstRequest.Header.Set("Authorization", "Bearer "+owner.Key)
	firstRequest.Header.Set("Last-Event-ID", "1")
	firstResponse, err := server.Client().Do(firstRequest)
	if err != nil {
		t.Fatalf("open first event stream: %v", err)
	}
	if firstResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(firstResponse.Body)
		_ = firstResponse.Body.Close()
		t.Fatalf("first stream status=%d body=%s", firstResponse.StatusCode, body)
	}

	secondRequest, err := http.NewRequest(http.MethodGet, server.URL+"/v1/jobs/"+job.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest.Header.Set("Authorization", "Bearer "+owner.Key)
	secondRequest.Header.Set("Last-Event-ID", "1")
	secondResponse, err := server.Client().Do(secondRequest)
	if err != nil {
		t.Fatalf("open second event stream: %v", err)
	}
	secondBody, _ := io.ReadAll(secondResponse.Body)
	_ = secondResponse.Body.Close()
	if secondResponse.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(secondBody), "capacity_limit_exceeded") {
		_ = firstResponse.Body.Close()
		t.Fatalf("second stream status=%d body=%s", secondResponse.StatusCode, secondBody)
	}

	_ = firstResponse.Body.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		retryRequest, requestErr := http.NewRequest(http.MethodGet, server.URL+"/v1/jobs/"+job.ID+"/events", nil)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		retryRequest.Header.Set("Authorization", "Bearer "+owner.Key)
		retryRequest.Header.Set("Last-Event-ID", "1")
		retryResponse, requestErr := server.Client().Do(retryRequest)
		if requestErr != nil {
			t.Fatalf("retry event stream: %v", requestErr)
		}
		if retryResponse.StatusCode == http.StatusOK {
			_ = retryResponse.Body.Close()
			break
		}
		_, _ = io.Copy(io.Discard, retryResponse.Body)
		_ = retryResponse.Body.Close()
		if retryResponse.StatusCode != http.StatusTooManyRequests || time.Now().After(deadline) {
			t.Fatalf("retry stream status=%d after first disconnect", retryResponse.StatusCode)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestGatewayJobEventSSEAuthorizationAndCursorValidation(t *testing.T) {
	handler, control, owner, job := gatewayJobEventTestRuntime(t)
	otherRequest := durableJobAPIKeyRequest("other event principal")
	otherRequest.ModelAllowlist = []string{"event-image-job"}
	other, err := control.CreateAPIKey(context.Background(), "test", otherRequest)
	if err != nil {
		t.Fatal(err)
	}
	if response := performGatewayJobEventRequest(handler, job.ID, other.Key, ""); response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "resource_not_found") {
		t.Fatalf("cross-principal status=%d body=%s", response.Code, response.Body.String())
	}
	if response := performGatewayJobEventRequest(handler, job.ID, owner.Key, "invalid"); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_event_cursor") {
		t.Fatalf("invalid cursor status=%d body=%s", response.Code, response.Body.String())
	}
	if response := performGatewayJobEventRequest(handler, job.ID, "", "invalid"); response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "invalid_api_key") {
		t.Fatalf("unauthorized cursor status=%d body=%s", response.Code, response.Body.String())
	}
}

func gatewayJobEventTestRuntime(t *testing.T) (http.Handler, *controlplane.Service, controlplane.APIKeyCreateResponse, publicAIJobResponse) {
	return gatewayJobEventTestRuntimeWithKey(t, nil)
}

func gatewayJobEventTestRuntimeWithKey(t *testing.T, configure func(*controlplane.APIKeyCreateRequest)) (http.Handler, *controlplane.Service, controlplane.APIKeyCreateResponse, publicAIJobResponse) {
	t.Helper()
	handler, control := newTestRuntime(t, RuntimeConfig{})
	if _, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "event-image-job", Name: "Event image job", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatalf("CreateGatewayModel(): %v", err)
	}
	keyRequest := durableJobAPIKeyRequest("event owner")
	keyRequest.ModelAllowlist = []string{"event-image-job"}
	if configure != nil {
		configure(&keyRequest)
	}
	owner, err := control.CreateAPIKey(context.Background(), "test", keyRequest)
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	created := performGatewayJobRequest(handler, http.MethodPost, "/v1/jobs", owner.Key, "event-job-idem", `{"model":"event-image-job","operation":"image_generation","modality":"image","input":{"prompt":"synthetic"}}`)
	var job publicAIJobResponse
	if created.Code != http.StatusAccepted || json.Unmarshal(created.Body.Bytes(), &job) != nil || job.ID == "" {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	return handler, control, owner, job
}

func performGatewayJobEventRequest(handler http.Handler, jobID, key, cursor string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/v1/jobs/"+jobID+"/events", nil)
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	request.Header.Set("Accept", "text/event-stream")
	if cursor != "" {
		request.Header.Set("Last-Event-ID", cursor)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
