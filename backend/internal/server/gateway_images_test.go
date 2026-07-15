package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/gin-gonic/gin"
)

type directImageAdapterStub struct {
	selectCalls     atomic.Int64
	dispatchCalls   atomic.Int64
	openCalls       atomic.Int64
	reconcileCalls  atomic.Int64
	supported       bool
	result          controlplane.ProviderDispatchResult
	reconcileResult controlplane.ProviderDispatchResult
	dispatchErr     error
	openErr         error
	outputs         map[string][]byte
}

func (stub *directImageAdapterStub) SelectDirectAIAdapter(_ context.Context, _ controlplane.GatewayProvider, request gatewaycore.CanonicalRequest, _ string) (string, bool, error) {
	stub.selectCalls.Add(1)
	if request.PreviewMode == "required" {
		return "", false, nil
	}
	return "test-direct-image", stub.supported, nil
}

func (stub *directImageAdapterStub) DispatchDirectAI(_ context.Context, _ controlplane.GatewayProvider, _ controlplane.AIOperation, _ controlplane.AIAttempt, _ gatewaycore.CanonicalRequest, _ controlplane.ProviderDispatchCommand) (controlplane.ProviderDispatchResult, error) {
	stub.dispatchCalls.Add(1)
	return stub.result, stub.dispatchErr
}

func (stub *directImageAdapterStub) ReconcileDirectAI(_ context.Context, _ controlplane.GatewayProvider, _ controlplane.AIOperation, _ controlplane.AIAttempt, _ controlplane.ProviderDispatchIntent, _ controlplane.ProviderTaskReference) (controlplane.ProviderDispatchResult, error) {
	stub.reconcileCalls.Add(1)
	if stub.reconcileResult.Outcome != "" {
		return stub.reconcileResult, nil
	}
	return stub.result, nil
}

func (stub *directImageAdapterStub) OpenDirectAIOutput(_ context.Context, _ controlplane.GatewayProvider, _ controlplane.AIOperation, _ controlplane.AIAttempt, _ gatewaycore.CanonicalRequest, _ controlplane.ProviderDispatchResult, output controlplane.ProviderOutputDescriptor) (io.ReadCloser, error) {
	stub.openCalls.Add(1)
	if stub.openErr != nil {
		return nil, stub.openErr
	}
	data, found := stub.outputs[output.OutputID]
	if !found {
		return nil, errors.New("synthetic output not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (stub *directImageAdapterStub) selected() int64 {
	return stub.selectCalls.Load()
}

func (stub *directImageAdapterStub) dispatched() int64 {
	return stub.dispatchCalls.Load()
}

type directImageHTTPFixture struct {
	handler    http.Handler
	control    *controlplane.Service
	key        string
	candidates []controlplane.GatewayProvider
}

func newDirectImageHTTPFixture(t *testing.T, adapter *directImageAdapterStub, routes, concurrency int, withStore bool) directImageHTTPFixture {
	return newDirectImageHTTPFixtureWithLimit(t, adapter, routes, concurrency, withStore, 0)
}

func newDirectImageHTTPFixtureWithLimit(t *testing.T, adapter *directImageAdapterStub, routes, concurrency int, withStore bool, monthlyImageLimit int) directImageHTTPFixture {
	return newDirectImageHTTPFixtureWithAdmission(t, adapter, routes, concurrency, withStore, monthlyImageLimit, allowDurableAIJobs{})
}

func newDirectImageHTTPFixtureWithAdmission(t *testing.T, adapter *directImageAdapterStub, routes, concurrency int, withStore bool, monthlyImageLimit int, durableJobs DurableAIJobAdmission) directImageHTTPFixture {
	t.Helper()
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if withStore {
		if err := control.SetArtifactStore(controlplane.NewMemoryArtifactStore()); err != nil {
			t.Fatal(err)
		}
	}
	model, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "public-image", Name: "Public image", Modality: controlplane.GatewayModalityImage,
		DefaultRouteGroup: "default", Status: controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < routes; index++ {
		provider, createErr := control.CreateProvider(context.Background(), "test", controlplane.ProviderRequest{
			Name: "Image provider", Type: "openai_compatible", BaseURL: "https://provider.invalid/v1",
			Status: controlplane.ProviderStatusActive, Models: []string{"provider-image"}, APIKey: "provider-secret",
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		account := createGatewayTestAccount(t, control, provider, "provider-image", "provider-secret", 10+index, concurrency)
		if _, createErr = control.CreateModelRoute(context.Background(), "test", controlplane.ModelRouteRequest{
			GatewayModelID: model.ID, RouteGroup: "default", ProviderAccountID: account.ID,
			UpstreamModel: "provider-image", Priority: 10 + index, Weight: 100, Status: controlplane.ModelRouteStatusActive,
		}); createErr != nil {
			t.Fatal(createErr)
		}
	}
	created, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "image caller", ModelAllowlist: []string{"public-image"},
		Scopes:            []string{controlplane.GatewayScopeInvoke, controlplane.GatewayScopeJobsRead, controlplane.GatewayScopeArtifactsRead},
		AllowedModalities: []string{controlplane.GatewayModalityImage}, AllowedOperations: []string{controlplane.GatewayOperationImageGeneration},
		LanePolicy: controlplane.GatewayLanePolicyDirectAndDurable, ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
		ConcurrencyLimit: 4, MonthlyImageLimit: monthlyImageLimit,
	})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerGatewayRoutes(router, control, durableJobs, adapter)
	candidates, _, err := control.GatewayProviderCandidatesForModel(context.Background(), "public-image")
	if err != nil {
		t.Fatal(err)
	}
	return directImageHTTPFixture{handler: router, control: control, key: created.Key, candidates: candidates}
}

func successfulDirectImageAdapter(payloads ...[]byte) *directImageAdapterStub {
	outputs := make(map[string][]byte, len(payloads))
	descriptors := make([]controlplane.ProviderOutputDescriptor, 0, len(payloads))
	for index, payload := range payloads {
		outputID := "image-" + string(rune('a'+index))
		digest := sha256.Sum256(payload)
		outputs[outputID] = append([]byte(nil), payload...)
		descriptors = append(descriptors, controlplane.ProviderOutputDescriptor{
			OutputID: outputID, Role: controlplane.ArtifactRoleFinal, MediaType: "image/png",
			ExpectedSizeBytes: int64(len(payload)), ExpectedSHA256: hex.EncodeToString(digest[:]), ProviderReference: "stub://" + outputID,
		})
	}
	return &directImageAdapterStub{
		supported: true, outputs: outputs,
		result: controlplane.ProviderDispatchResult{
			Outcome: controlplane.ProviderDispatchOutcomeAccepted,
			Task:    controlplane.ProviderTaskReference{ProviderTaskID: "provider-task", ProviderRequestID: "provider-request", Status: "succeeded"},
			Outputs: descriptors, ReconcileAfter: time.Now().UTC(),
		},
	}
}

func performImageGeneration(handler http.Handler, key, idempotencyKey, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Idempotency-Key", idempotencyKey)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestGatewayImageBlockingPersistsArtifactAndReplaysIdempotently(t *testing.T) {
	payload := []byte("synthetic-image-content")
	adapter := successfulDirectImageAdapter(payload)
	procurementCost := int64(73)
	adapter.result.Billing = controlplane.ProviderBillingObservation{
		Status: controlplane.ProviderBillingStatusFinal, ProcurementCostMicros: &procurementCost,
		Currency: "USD", Source: "provider_invoice", Confidence: controlplane.ProcurementCostConfidenceExact,
	}
	fixture := newDirectImageHTTPFixture(t, adapter, 1, 2, true)
	body := `{"model":"public-image","prompt":"synthetic","delivery_mode":"inline"}`

	first := performImageGeneration(fixture.handler, fixture.key, "image-blocking-idem", body)
	if first.Code != http.StatusOK || first.Header().Get("X-AsterRouter-Operation-ID") == "" {
		t.Fatalf("first status=%d headers=%v body=%s", first.Code, first.Header(), first.Body.String())
	}
	var response directImageResponse
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil || len(response.Data) != 1 {
		t.Fatalf("response=%+v err=%v body=%s", response, err, first.Body.String())
	}
	if response.Data[0].B64JSON != base64.StdEncoding.EncodeToString(payload) || response.Data[0].ArtifactID == "" {
		t.Fatalf("image response=%+v", response)
	}
	content := performGatewayArtifactRequest(fixture.handler, http.MethodGet, "/v1/artifacts/"+response.Data[0].ArtifactID+"/content", fixture.key, "")
	if content.Code != http.StatusOK || !bytes.Equal(content.Body.Bytes(), payload) {
		t.Fatalf("artifact status=%d body=%q", content.Code, content.Body.String())
	}

	replayed := performImageGeneration(fixture.handler, fixture.key, "image-blocking-idem", body)
	if replayed.Code != http.StatusOK || replayed.Header().Get("Idempotent-Replayed") != "true" || replayed.Body.String() != first.Body.String() || adapter.dispatched() != 1 {
		t.Fatalf("replay status=%d calls=%d headers=%v body=%s", replayed.Code, adapter.dispatched(), replayed.Header(), replayed.Body.String())
	}
	conflict := performImageGeneration(fixture.handler, fixture.key, "image-blocking-idem", strings.Replace(body, "synthetic", "different", 1))
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "idempotency_conflict") || adapter.dispatched() != 1 {
		t.Fatalf("conflict status=%d calls=%d body=%s", conflict.Code, adapter.dispatched(), conflict.Body.String())
	}
	usage, err := fixture.control.UsageReport(context.Background(), 10)
	if err != nil || len(usage.Recent) != 1 || usage.TotalOutputImages != 1 || usage.Recent[0].UsageDimensions[controlplane.UsageDimensionOutputImages].Quantity != 1 || usage.Recent[0].UsageDimensions[controlplane.UsageDimensionOutputBytes].Quantity != int64(len(payload)) || usage.Recent[0].ProcurementCostMicros == nil || *usage.Recent[0].ProcurementCostMicros != procurementCost {
		t.Fatalf("image usage=%+v err=%v", usage, err)
	}
}

func TestGatewayImageQuotaRejectsDirectAndAsyncBeforeProviderOrJob(t *testing.T) {
	for _, responseMode := range []string{"blocking", "async"} {
		t.Run(responseMode, func(t *testing.T) {
			adapter := successfulDirectImageAdapter([]byte("unused"), []byte("unused-2"))
			fixture := newDirectImageHTTPFixtureWithLimit(t, adapter, 1, 2, true, 1)
			body := `{"model":"public-image","prompt":"synthetic","n":2,"delivery_mode":"artifact","response_mode":"` + responseMode + `"}`
			response := performImageGeneration(fixture.handler, fixture.key, "image-quota-"+responseMode, body)
			if response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), "image_quota_exceeded") || adapter.dispatched() != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, adapter.dispatched(), response.Body.String())
			}
			jobs, err := fixture.control.ListAIJobsAdmin(context.Background(), controlplane.AIJobQuery{Limit: 10})
			if err != nil || len(jobs) != 0 {
				t.Fatalf("quota rejection jobs=%+v err=%v", jobs, err)
			}
		})
	}
}

func TestGatewayImageStreamEmitsFinalUsageAndDoneOnly(t *testing.T) {
	adapter := successfulDirectImageAdapter([]byte("stream-image"))
	fixture := newDirectImageHTTPFixture(t, adapter, 1, 2, true)
	response := performImageGeneration(fixture.handler, fixture.key, "image-stream-idem", `{"model":"public-image","prompt":"synthetic","response_mode":"stream","preview_mode":"preferred","delivery_mode":"artifact"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	body := response.Body.String()
	for _, event := range []string{"event: image.final", "event: usage.finalized", "event: done", "/content"} {
		if !strings.Contains(body, event) {
			t.Fatalf("missing %q in %s", event, body)
		}
	}
	if strings.Contains(body, "image.preview") {
		t.Fatalf("final-only provider produced a synthetic preview: %s", body)
	}
}

func TestGatewayMediaDirectStreamUsesCoreArtifactAndUsagePipeline(t *testing.T) {
	adapter := successfulDirectImageAdapter([]byte("synthetic-video"))
	fixture := newDirectImageHTTPFixture(t, adapter, 0, 2, true)
	model, err := fixture.control.CreateGatewayModel(context.Background(), "public-video-direct", controlplane.GatewayModelRequest{
		ModelID: "public-video-direct", Name: "Public video direct", Modality: controlplane.GatewayModalityVideo,
		DefaultRouteGroup: "default", Status: controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := fixture.control.CreateProvider(context.Background(), "test", controlplane.ProviderRequest{
		Name: "Video provider", Type: "openai_compatible", BaseURL: "https://provider.invalid/v1",
		Status: controlplane.ProviderStatusActive, Models: []string{"provider-video"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	account := createGatewayTestAccount(t, fixture.control, provider, "provider-video", "provider-secret", 10, 2)
	if _, err := fixture.control.CreateModelRoute(context.Background(), "test", controlplane.ModelRouteRequest{
		GatewayModelID: model.ID, RouteGroup: "default", ProviderAccountID: account.ID,
		UpstreamModel: "provider-video", Priority: 10, Weight: 100, Status: controlplane.ModelRouteStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	key, err := fixture.control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "video caller", ModelAllowlist: []string{"public-video-direct"},
		Scopes:            []string{controlplane.GatewayScopeInvoke, controlplane.GatewayScopeArtifactsRead},
		AllowedModalities: []string{controlplane.GatewayModalityVideo}, AllowedOperations: []string{controlplane.GatewayOperationVideoGeneration},
		LanePolicy: controlplane.GatewayLanePolicyDirectOnly, ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := performGatewayJobRequest(fixture.handler, http.MethodPost, "/v1/videos/generations", key.Key, "video-direct-stream", `{"model":"public-video-direct","prompt":"synthetic","duration_seconds":1.5,"response_mode":"stream","delivery_mode":"artifact"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	body := response.Body.String()
	for _, event := range []string{"event: video.final", "event: usage.finalized", "event: done", "/v1/artifacts/"} {
		if !strings.Contains(body, event) {
			t.Fatalf("missing %q in %s", event, body)
		}
	}
	usage, err := fixture.control.UsageReport(context.Background(), 10)
	if err != nil || len(usage.Recent) != 1 || usage.Recent[0].UsageDimensions[controlplane.UsageDimensionOutputVideoMilliseconds].Quantity != 1500 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
}

func TestGatewayImageRequiredPreviewFailsClosedBeforeDispatch(t *testing.T) {
	adapter := successfulDirectImageAdapter([]byte("unused"))
	fixture := newDirectImageHTTPFixture(t, adapter, 1, 2, true)
	response := performImageGeneration(fixture.handler, fixture.key, "image-preview-idem", `{"model":"public-image","prompt":"synthetic","response_mode":"stream","preview_mode":"required","delivery_mode":"artifact"}`)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "unsupported_capability") || adapter.dispatched() != 0 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, adapter.dispatched(), response.Body.String())
	}
}

func TestGatewayImageAsyncCapabilityRejectionPersistsTrace(t *testing.T) {
	adapter := successfulDirectImageAdapter([]byte("unused"))
	fixture := newDirectImageHTTPFixtureWithAdmission(t, adapter, 1, 2, true, 0, nil)
	response := performImageGeneration(fixture.handler, fixture.key, "image-async-unavailable", `{"model":"public-image","prompt":"synthetic","response_mode":"async","delivery_mode":"artifact"}`)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "unsupported_capability") || adapter.selected() != 0 || adapter.dispatched() != 0 {
		t.Fatalf("status=%d select=%d dispatch=%d body=%s", response.Code, adapter.selected(), adapter.dispatched(), response.Body.String())
	}
	traces, err := fixture.control.ListGatewayTraces(context.Background(), 10)
	if err != nil || len(traces) != 1 || traces[0].RouteReason != controlplane.DurableAIJobCapabilityRuntimeUnavailable || traces[0].RequestFingerprint == "" {
		t.Fatalf("traces=%+v err=%v", traces, err)
	}
	jobs, err := fixture.control.ListAIJobsAdmin(context.Background(), controlplane.AIJobQuery{Limit: 10})
	if err != nil || len(jobs) != 0 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
}

func TestGatewayImageCapacityBackpressureDoesNotDispatchOrCreateJob(t *testing.T) {
	adapter := successfulDirectImageAdapter([]byte("unused"))
	fixture := newDirectImageHTTPFixture(t, adapter, 1, 1, true)
	if len(fixture.candidates) != 1 {
		t.Fatalf("candidates=%+v", fixture.candidates)
	}
	permit, _, acquired, err := fixture.control.TryAcquireProviderAccountPermitContext(context.Background(), fixture.candidates[0], 1, "occupy-image-capacity")
	if err != nil || !acquired {
		t.Fatalf("occupy capacity acquired=%t err=%v", acquired, err)
	}
	defer permit.Release()
	response := performImageGeneration(fixture.handler, fixture.key, "image-capacity-idem", `{"model":"public-image","prompt":"synthetic","delivery_mode":"artifact"}`)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" || adapter.dispatched() != 0 {
		t.Fatalf("status=%d calls=%d headers=%v body=%s", response.Code, adapter.dispatched(), response.Header(), response.Body.String())
	}
	jobs, err := fixture.control.ListAIJobsAdmin(context.Background(), controlplane.AIJobQuery{Limit: 10})
	if err != nil || len(jobs) != 0 {
		t.Fatalf("direct backpressure created jobs=%+v err=%v", jobs, err)
	}
}

func TestGatewayImageAsyncCreatesAndReplaysOneDurableJob(t *testing.T) {
	adapter := successfulDirectImageAdapter([]byte("unused"))
	fixture := newDirectImageHTTPFixture(t, adapter, 1, 2, true)
	body := `{"model":"public-image","prompt":"synthetic","response_mode":"async"}`
	first := performImageGeneration(fixture.handler, fixture.key, "image-async-idem", body)
	if first.Code != http.StatusAccepted || first.Header().Get("Location") == "" || adapter.selected() != 0 || adapter.dispatched() != 0 {
		t.Fatalf("first status=%d select=%d dispatch=%d headers=%v body=%s", first.Code, adapter.selected(), adapter.dispatched(), first.Header(), first.Body.String())
	}
	var accepted publicAIJobResponse
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil || accepted.ID == "" || accepted.Capability.Operation != controlplane.GatewayOperationImageGeneration {
		t.Fatalf("accepted=%+v err=%v body=%s", accepted, err, first.Body.String())
	}
	replay := performImageGeneration(fixture.handler, fixture.key, "image-async-idem", body)
	var replayed publicAIJobResponse
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotent-Replayed") != "true" || json.Unmarshal(replay.Body.Bytes(), &replayed) != nil || replayed.ID != accepted.ID {
		t.Fatalf("replay status=%d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	}
	jobs, err := fixture.control.ListAIJobsAdmin(context.Background(), controlplane.AIJobQuery{Limit: 10})
	if err != nil || len(jobs) != 1 || jobs[0].ID != accepted.ID || jobs[0].Protocol != string(gatewaycore.ProtocolOpenAIImages) {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
}

func TestGatewayImageUnknownSubmissionDoesNotFallbackOrLeakAdapterError(t *testing.T) {
	leaked := "provider-secret-must-not-leak"
	adapter := &directImageAdapterStub{
		supported:   true,
		result:      controlplane.ProviderDispatchResult{Outcome: controlplane.ProviderDispatchOutcomeUnknown, ReconcileAfter: time.Now().UTC().Add(time.Minute)},
		dispatchErr: errors.New(leaked),
	}
	fixture := newDirectImageHTTPFixture(t, adapter, 2, 2, true)
	response := performImageGeneration(fixture.handler, fixture.key, "image-unknown-idem", `{"model":"public-image","prompt":"synthetic","delivery_mode":"artifact"}`)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "provider_status_unknown") || adapter.dispatched() != 1 || adapter.selected() != 1 {
		t.Fatalf("status=%d select=%d dispatch=%d body=%s", response.Code, adapter.selected(), adapter.dispatched(), response.Body.String())
	}
	if strings.Contains(response.Body.String(), leaked) {
		t.Fatalf("response leaked adapter error: %s", response.Body.String())
	}
	traces, err := fixture.control.ListGatewayTraces(context.Background(), 10)
	if err != nil || len(traces) == 0 {
		t.Fatalf("traces=%+v err=%v", traces, err)
	}
	encoded, _ := json.Marshal(traces)
	if bytes.Contains(encoded, []byte(leaked)) {
		t.Fatalf("trace leaked adapter error: %s", encoded)
	}
	jobs, err := fixture.control.ListAIJobsAdmin(context.Background(), controlplane.AIJobQuery{Limit: 10})
	if err != nil || len(jobs) != 0 {
		t.Fatalf("unknown direct request created jobs=%+v err=%v", jobs, err)
	}
}

func TestGatewayImageArtifactFailureDoesNotRegenerateOrLeakReaderError(t *testing.T) {
	leaked := "reader-secret-must-not-leak"
	adapter := successfulDirectImageAdapter([]byte("unreadable"))
	adapter.openErr = errors.New(leaked)
	fixture := newDirectImageHTTPFixture(t, adapter, 2, 2, true)
	response := performImageGeneration(fixture.handler, fixture.key, "image-artifact-error-idem", `{"model":"public-image","prompt":"synthetic","delivery_mode":"artifact"}`)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "artifact_integrity_failed") || adapter.dispatched() != 1 || adapter.selected() != 1 {
		t.Fatalf("status=%d select=%d dispatch=%d body=%s", response.Code, adapter.selected(), adapter.dispatched(), response.Body.String())
	}
	if strings.Contains(response.Body.String(), leaked) {
		t.Fatalf("response leaked reader error: %s", response.Body.String())
	}
	traces, err := fixture.control.ListGatewayTraces(context.Background(), 10)
	encoded, _ := json.Marshal(traces)
	if err != nil || bytes.Contains(encoded, []byte(leaked)) {
		t.Fatalf("traces=%s err=%v", encoded, err)
	}
}

func TestGatewayImageAcceptedInvalidResponseDisputesBillingWithoutFallback(t *testing.T) {
	adapter := &directImageAdapterStub{
		supported: true,
		result: controlplane.ProviderDispatchResult{
			Outcome: controlplane.ProviderDispatchOutcomeAccepted,
			Task:    controlplane.ProviderTaskReference{ProviderTaskID: "accepted-without-output", Status: "succeeded"},
		},
	}
	fixture := newDirectImageHTTPFixture(t, adapter, 2, 2, true)
	response := performImageGeneration(fixture.handler, fixture.key, "image-invalid-provider-response", `{"model":"public-image","prompt":"synthetic","delivery_mode":"artifact"}`)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "provider_response_invalid") || adapter.dispatched() != 1 || adapter.selected() != 1 {
		t.Fatalf("status=%d select=%d dispatch=%d body=%s", response.Code, adapter.selected(), adapter.dispatched(), response.Body.String())
	}
	operationID := response.Header().Get("X-AsterRouter-Operation-ID")
	hold, found, err := fixture.control.BillingHoldForOperation(context.Background(), operationID)
	if err != nil || !found || hold.Status != controlplane.BillingHoldStatusDisputed {
		t.Fatalf("billing hold=%+v found=%t err=%v", hold, found, err)
	}
}

func TestGatewayImageProviderTerminalFailureSettlesFinalBilling(t *testing.T) {
	procurementCost := int64(91)
	adapter := &directImageAdapterStub{
		supported: true,
		result: controlplane.ProviderDispatchResult{
			Outcome: controlplane.ProviderDispatchOutcomeAccepted,
			Task:    controlplane.ProviderTaskReference{ProviderTaskID: "accepted-failed", ProviderRequestID: "failed-request", Status: "failed"},
			UsageDimensions: controlplane.UsageDimensions{controlplane.UsageDimensionOutputImages: {
				Quantity: 1, Unit: controlplane.UsageUnitCount, Source: "provider", Confidence: controlplane.UsageConfidenceReported,
			}},
			Billing: controlplane.ProviderBillingObservation{
				Status: controlplane.ProviderBillingStatusFinal, ProcurementCostMicros: &procurementCost,
				Currency: "USD", Source: "provider_invoice", Confidence: controlplane.ProcurementCostConfidenceExact,
			},
		},
	}
	fixture := newDirectImageHTTPFixture(t, adapter, 2, 2, true)
	response := performImageGeneration(fixture.handler, fixture.key, "image-provider-terminal-failure", `{"model":"public-image","prompt":"synthetic","delivery_mode":"artifact"}`)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "provider_terminal_failure") || adapter.dispatched() != 1 || adapter.selected() != 1 {
		t.Fatalf("status=%d select=%d dispatch=%d body=%s", response.Code, adapter.selected(), adapter.dispatched(), response.Body.String())
	}
	operationID := response.Header().Get("X-AsterRouter-Operation-ID")
	hold, found, err := fixture.control.BillingHoldForOperation(context.Background(), operationID)
	if err != nil || !found || hold.Status != controlplane.BillingHoldStatusSettled {
		t.Fatalf("billing hold=%+v found=%t err=%v", hold, found, err)
	}
	usage, err := fixture.control.UsageReport(context.Background(), 10)
	if err != nil || usage.TotalOutputImages != 1 {
		t.Fatalf("terminal usage=%+v err=%v", usage, err)
	}
	foundFinal := false
	for _, record := range usage.Recent {
		if record.UsageSource == "provider_final" && record.UsageVersion == 2 && record.ProcurementCostMicros != nil && *record.ProcurementCostMicros == procurementCost {
			foundFinal = true
		}
	}
	if !foundFinal {
		t.Fatalf("provider final usage missing: %+v", usage.Recent)
	}
}

func TestGatewayImageUnknownSubmissionReconcilesFinalBilling(t *testing.T) {
	procurementCost := int64(107)
	adapter := &directImageAdapterStub{
		supported: true,
		result: controlplane.ProviderDispatchResult{
			Outcome:        controlplane.ProviderDispatchOutcomeUnknown,
			Task:           controlplane.ProviderTaskReference{ProviderTaskID: "unknown-task", ProviderRequestID: "unknown-request", Status: "unknown"},
			ReconcileAfter: time.Now().UTC(),
		},
		reconcileResult: controlplane.ProviderDispatchResult{
			Outcome: controlplane.ProviderDispatchOutcomeAccepted,
			Task:    controlplane.ProviderTaskReference{ProviderTaskID: "unknown-task", ProviderRequestID: "unknown-request", Status: "failed"},
			UsageDimensions: controlplane.UsageDimensions{controlplane.UsageDimensionOutputImages: {
				Quantity: 1, Unit: controlplane.UsageUnitCount, Source: "provider", Confidence: controlplane.UsageConfidenceReported,
			}},
			Billing: controlplane.ProviderBillingObservation{
				Status: controlplane.ProviderBillingStatusFinal, ProcurementCostMicros: &procurementCost,
				Currency: "USD", Source: "provider_invoice", Confidence: controlplane.ProcurementCostConfidenceExact,
			},
		},
	}
	fixture := newDirectImageHTTPFixture(t, adapter, 1, 2, true)
	response := performImageGeneration(fixture.handler, fixture.key, "image-unknown-reconcile", `{"model":"public-image","prompt":"synthetic","delivery_mode":"artifact"}`)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "provider_status_unknown") || adapter.dispatched() != 1 {
		t.Fatalf("initial status=%d dispatch=%d body=%s", response.Code, adapter.dispatched(), response.Body.String())
	}
	operationID := response.Header().Get("X-AsterRouter-Operation-ID")
	hold, found, err := fixture.control.BillingHoldForOperation(context.Background(), operationID)
	if err != nil || !found || hold.Status != controlplane.BillingHoldStatusDisputed {
		t.Fatalf("initial hold=%+v found=%t err=%v", hold, found, err)
	}
	report, err := fixture.control.RunDirectAIReconcilerOnce(context.Background(), 10, adapter)
	if err != nil || report.Reconciled != 1 || report.Completed != 1 || adapter.reconcileCalls.Load() != 1 {
		t.Fatalf("reconcile report=%+v calls=%d err=%v", report, adapter.reconcileCalls.Load(), err)
	}
	hold, found, err = fixture.control.BillingHoldForOperation(context.Background(), operationID)
	if err != nil || !found || hold.Status != controlplane.BillingHoldStatusSettled {
		t.Fatalf("reconciled hold=%+v found=%t err=%v", hold, found, err)
	}
	usage, err := fixture.control.UsageReport(context.Background(), 10)
	if err != nil || usage.TotalOutputImages != 1 {
		t.Fatalf("reconciled usage=%+v err=%v", usage, err)
	}
	foundFinal := false
	for _, record := range usage.Recent {
		if record.OperationID == operationID && record.UsageSource == "provider_final" && record.UsageVersion == 2 && record.ProcurementCostMicros != nil && *record.ProcurementCostMicros == procurementCost {
			foundFinal = true
		}
	}
	if !foundFinal {
		t.Fatalf("reconciled final usage missing: %+v", usage.Recent)
	}
	if replay, replayErr := fixture.control.RunDirectAIReconcilerOnce(context.Background(), 10, adapter); replayErr != nil || replay.Reconciled != 0 || adapter.reconcileCalls.Load() != 1 {
		t.Fatalf("replay report=%+v calls=%d err=%v", replay, adapter.reconcileCalls.Load(), replayErr)
	}
}
