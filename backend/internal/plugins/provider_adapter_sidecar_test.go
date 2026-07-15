package plugins

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestProviderAdapterSidecarCapabilityAndLifecycleContract(t *testing.T) {
	const (
		pluginID = "com.asterrouter.test.media-adapter"
		version  = "1.0.0"
		token    = "runtime-token"
		apiKey   = "provider-secret-must-not-escape"
	)
	var requestsMu sync.Mutex
	requests := map[string][]byte{}
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+token || request.Header.Get("Content-Type") != "application/json" {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(request.Body)
		requestsMu.Lock()
		requests[request.URL.Path] = append([]byte(nil), body...)
		requestsMu.Unlock()
		if strings.Contains(string(body), `"force_error":true`) {
			http.Error(response, apiKey, http.StatusInternalServerError)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/provider-adapter/dispatch":
			_, _ = response.Write([]byte(`{"outcome":"accepted","task":{"provider_task_id":"task-1","provider_request_id":"request-1","status":"running"}}`))
		case "/v1/provider-adapter/reconcile":
			_, _ = response.Write([]byte(`{"outcome":"accepted","task":{"provider_task_id":"task-1","provider_request_id":"request-1","status":"succeeded"},"progress":{"sequence":2,"percent":100,"stage":"completed"},"outputs":[{"output_id":"final-image","role":"final","media_type":"image/png","expected_size_bytes":12,"provider_reference":"provider://task-1/final"}]}`))
		case "/v1/provider-adapter/cancel":
			_, _ = response.Write([]byte(`{"outcome":"accepted","task":{"provider_task_id":"task-1","provider_request_id":"request-1","status":"canceled"}}`))
		case "/v1/provider-adapter/output":
			response.Header().Set("Content-Type", "image/png")
			_, _ = response.Write([]byte("image-output"))
		default:
			http.NotFound(response, request)
		}
	}))
	defer upstream.Close()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	if err := repo.SavePlugin(context.Background(), Plugin{
		ID: pluginID, PluginID: pluginID, Name: "Media adapter", Type: "sidecar", Status: StatusEnabled,
		Tier: TierFreeCore, EntitlementStatus: EntitlementFree, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePackageInstallation(context.Background(), packageInstallationRecord{
		PluginID: pluginID, PackageID: "pkg-media-adapter", Version: version, Status: PackageInstallInstalled, InstalledAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	activeRoot := t.TempDir()
	service := NewServiceWithOptions(repo, ServiceOptions{PluginActiveDir: activeRoot, ProviderAdapterHTTPClient: upstream.Client()})
	activeDir, err := service.activePackageDir(pluginID, version)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(activeDir, 0750); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(sidecarManifest{
		ID: pluginID, Version: version, Runtime: "sidecar",
		ProviderAdapters: []providerAdapterManifestCapability{{
			ProviderTypes: []string{"test_media"}, Modalities: []string{"image"}, Operations: []string{"image_generation"},
			ArtifactPolicies:     []string{controlplane.GatewayArtifactPolicyTemporary},
			SupportsCancellation: true, SupportsCallbacks: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activeDir, "plugin.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	service.sidecars[pluginID] = &sidecarProcess{
		PluginID: pluginID, Version: version, Endpoint: upstream.URL, Token: token,
		Command: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}, done: done,
	}
	service.supervisors[pluginID] = &sidecarSupervisor{wake: make(chan struct{}, 1)}
	t.Cleanup(func() { close(done) })
	if err := service.AuthorizeSidecarProviderCallback(context.Background(), pluginID, token); err != nil {
		t.Fatalf("AuthorizeSidecarProviderCallback(): %v", err)
	}

	provider := controlplane.GatewayProvider{
		ID: "provider-1", Type: "test_media", BaseURL: "https://provider.example/v1", APIKey: apiKey,
		AccountID: "account-1", UpstreamModel: "image-v1",
	}
	job := controlplane.AIJob{
		ID: "job-1", OperationID: "operation-1", Protocol: "aster_jobs", Operation: "image_generation",
		Modality: "image", Model: "public-image", ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	}
	selected, supported, err := service.SelectDurableAIJobAdapter(context.Background(), provider, job)
	if err != nil || !supported || selected != pluginID {
		t.Fatalf("SelectDurableAIJobAdapter() selected=%q supported=%t err=%v", selected, supported, err)
	}
	unsupportedJob := job
	unsupportedJob.ArtifactPolicy = controlplane.GatewayArtifactPolicyMetadataOnly
	if selected, supported, reason, err := service.ExplainDurableAIJobAdapterSelection(context.Background(), provider, unsupportedJob); err != nil || supported || selected != "" || reason != controlplane.DurableAIJobCapabilityArtifactPolicyUnsupported {
		t.Fatalf("unsupported selection=%q supported=%t reason=%q err=%v", selected, supported, reason, err)
	}
	provider.AdapterID = selected
	attempt := controlplane.AIAttempt{ID: "attempt-1", AttemptNumber: 1, ProviderAdapterID: selected}
	intent := controlplane.ProviderDispatchIntent{
		Version: 1, AttemptID: attempt.ID, OperationID: job.OperationID, DispatchKey: attempt.ID,
		RequestFingerprint: "fingerprint", ProviderID: provider.ID, ProviderAccountID: provider.AccountID,
		ProviderAdapterID: selected, RouteID: "route-1", UpstreamModel: provider.UpstreamModel,
	}
	dispatched, err := service.DispatchProviderTask(context.Background(), provider, job, attempt, controlplane.ProviderDispatchCommand{
		Intent: intent, Payload: []byte(`{"input":{"prompt":"synthetic"}}`),
	})
	if err != nil || dispatched.Outcome != controlplane.ProviderDispatchOutcomeAccepted || dispatched.Task.ProviderTaskID != "task-1" {
		t.Fatalf("DispatchProviderTask() result=%+v err=%v", dispatched, err)
	}
	reconciled, err := service.ReconcileProviderTask(context.Background(), provider, job, attempt, intent, dispatched.Task)
	if err != nil || reconciled.Task.Status != "succeeded" || reconciled.Progress == nil || reconciled.Progress.Sequence != 2 || reconciled.Progress.Percent == nil || *reconciled.Progress.Percent != 100 || len(reconciled.Outputs) != 1 {
		t.Fatalf("ReconcileProviderTask() result=%+v err=%v", reconciled, err)
	}
	attempt.ProviderTaskID = reconciled.Task.ProviderTaskID
	attempt.ProviderRequestID = reconciled.Task.ProviderRequestID
	attempt.ProviderTaskStatus = reconciled.Task.Status
	canCancel, err := service.SupportsDurableAIJobCancellation(context.Background(), provider, job, attempt)
	if err != nil || !canCancel {
		t.Fatalf("cancellation capability=%t err=%v", canCancel, err)
	}
	cancelled, err := service.CancelProviderTask(context.Background(), provider, job, attempt, intent, reconciled.Task)
	if err != nil || cancelled.Task.Status != "canceled" || cancelled.Outcome != controlplane.ProviderDispatchOutcomeAccepted {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
	output, err := service.OpenProviderOutput(context.Background(), provider, job, attempt, reconciled.Outputs[0])
	if err != nil {
		t.Fatal(err)
	}
	outputBody, err := io.ReadAll(output)
	_ = output.Close()
	if err != nil || string(outputBody) != "image-output" {
		t.Fatalf("output=%q err=%v", outputBody, err)
	}

	requestsMu.Lock()
	dispatchBody := string(requests["/v1/provider-adapter/dispatch"])
	requestsMu.Unlock()
	if !strings.Contains(dispatchBody, apiKey) || !strings.Contains(dispatchBody, "synthetic") || strings.Contains(dispatchBody, "RequestPayloadCiphertext") {
		t.Fatalf("sidecar dispatch contract body=%s", dispatchBody)
	}
	_, err = service.DispatchProviderTask(context.Background(), provider, job, attempt, controlplane.ProviderDispatchCommand{
		Intent: intent, Payload: []byte(`{"force_error":true}`),
	})
	if err == nil || strings.Contains(err.Error(), apiKey) {
		t.Fatalf("sidecar error leaked provider secret: %v", err)
	}

	if err := repo.UpdateStatus(context.Background(), pluginID, StatusDisabled, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if selected, supported, err := service.SelectDurableAIJobAdapter(context.Background(), provider, job); err != nil || supported || selected != "" {
		t.Fatalf("disabled adapter selected=%q supported=%t err=%v", selected, supported, err)
	}
	if _, supported, reason, err := service.ExplainDurableAIJobAdapterSelection(context.Background(), provider, job); err != nil || supported || reason != controlplane.DurableAIJobCapabilityAdapterUnavailable {
		t.Fatalf("disabled adapter supported=%t reason=%q err=%v", supported, reason, err)
	}
}

func TestReadSidecarManifestRejectsInvalidProviderAdapterCapabilities(t *testing.T) {
	for _, manifest := range []string{
		`{"id":"adapter","version":"1","runtime":"sidecar","provider_adapters":[{"provider_types":[],"modalities":["image"],"operations":["image_generation"]}]}`,
		`{"id":"adapter","version":"1","runtime":"sidecar","provider_adapters":[{"provider_types":["*"],"modalities":["image"],"operations":["image_generation"]}]}`,
		`{"id":"adapter","version":"1","runtime":"sidecar","provider_adapters":[{"provider_types":["media"],"modalities":["image"],"operations":["image_generation"],"artifact_policies":["*"]}]}`,
	} {
		path := filepath.Join(t.TempDir(), "plugin.json")
		if err := os.WriteFile(path, []byte(manifest), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readSidecarManifest(path); err == nil {
			t.Fatalf("readSidecarManifest() accepted %s", manifest)
		}
	}
}

func TestManifestSupportsProviderJobEnforcesDeclaredArtifactPolicies(t *testing.T) {
	manifest := sidecarManifest{ProviderAdapters: []providerAdapterManifestCapability{{
		ProviderTypes: []string{"test_media"}, Modalities: []string{"video"}, Operations: []string{"video_generation"},
		ArtifactPolicies: []string{"temporary", "customer_sink"},
	}}}
	provider := controlplane.GatewayProvider{Type: "test_media"}
	job := controlplane.AIJob{Modality: "video", Operation: "video_generation", ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary}
	if !manifestSupportsProviderJob(manifest, provider, job) {
		t.Fatal("temporary artifact policy should be supported")
	}
	job.ArtifactPolicy = controlplane.GatewayArtifactPolicyMetadataOnly
	if manifestSupportsProviderJob(manifest, provider, job) {
		t.Fatal("undeclared artifact policy should be rejected")
	}
	manifest.ProviderAdapters[0].ArtifactPolicies = nil
	if !manifestSupportsProviderJob(manifest, provider, job) {
		t.Fatal("legacy manifest without artifact policy declaration should remain compatible")
	}
}

func TestManifestProviderJobSupportExplainsMostSpecificMismatch(t *testing.T) {
	manifest := sidecarManifest{ProviderAdapters: []providerAdapterManifestCapability{{
		ProviderTypes: []string{"media_provider"}, Modalities: []string{"video"}, Operations: []string{"video_generation"},
		ArtifactPolicies: []string{controlplane.GatewayArtifactPolicyTemporary},
	}}}
	tests := []struct {
		name     string
		provider controlplane.GatewayProvider
		job      controlplane.AIJob
		reason   string
	}{
		{name: "provider type", provider: controlplane.GatewayProvider{Type: "other"}, job: controlplane.AIJob{Modality: "video", Operation: "video_generation", ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary}, reason: controlplane.DurableAIJobCapabilityProviderTypeUnsupported},
		{name: "modality", provider: controlplane.GatewayProvider{Type: "media_provider"}, job: controlplane.AIJob{Modality: "audio", Operation: "video_generation", ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary}, reason: controlplane.DurableAIJobCapabilityModalityUnsupported},
		{name: "operation", provider: controlplane.GatewayProvider{Type: "media_provider"}, job: controlplane.AIJob{Modality: "video", Operation: "video_edit", ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary}, reason: controlplane.DurableAIJobCapabilityOperationUnsupported},
		{name: "artifact policy", provider: controlplane.GatewayProvider{Type: "media_provider"}, job: controlplane.AIJob{Modality: "video", Operation: "video_generation", ArtifactPolicy: controlplane.GatewayArtifactPolicyManaged}, reason: controlplane.DurableAIJobCapabilityArtifactPolicyUnsupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if supported, reason := manifestProviderJobSupport(manifest, test.provider, test.job); supported || reason != test.reason {
				t.Fatalf("supported=%t reason=%q want=%q", supported, reason, test.reason)
			}
		})
	}
}
