package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

type audioProtocolFixture struct {
	handler http.Handler
	control *controlplane.Service
	key     string

	mu       sync.Mutex
	requests []audioUpstreamRequest
}

type audioUpstreamRequest struct {
	path        string
	model       string
	file        []byte
	body        []byte
	contentType string
}

func newAudioProtocolFixture(t *testing.T, artifactPolicy string) *audioProtocolFixture {
	t.Helper()
	fixture := &audioProtocolFixture{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record := audioUpstreamRequest{path: r.URL.Path, contentType: r.Header.Get("Content-Type")}
		if strings.HasSuffix(r.URL.Path, "/audio/speech") {
			body, _ := io.ReadAll(r.Body)
			record.body = append([]byte(nil), body...)
			var payload map[string]any
			_ = json.Unmarshal(body, &payload)
			record.model, _ = payload["model"].(string)
			fixture.appendRequest(record)
			if payload["stream_format"] == "sse" {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "event: audio.delta\ndata: {\"type\":\"audio.delta\",\"delta\":\"c3ludGhldGlj\"}\n\n")
				_, _ = io.WriteString(w, "event: audio.done\ndata: {\"type\":\"audio.done\"}\n\n")
				return
			}
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("synthetic-audio"))
			return
		}
		if err := r.ParseMultipartForm(gatewayAudioRequestBodyLimit); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		record.model = r.FormValue("model")
		file, _, err := r.FormFile("file")
		if err == nil {
			record.file, _ = io.ReadAll(file)
			_ = file.Close()
		}
		fixture.appendRequest(record)
		if strings.HasSuffix(r.URL.Path, "/audio/transcriptions") && r.FormValue("stream") == "true" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: transcript.text.delta\ndata: {\"type\":\"transcript.text.delta\",\"delta\":\"hello\"}\n\n")
			_, _ = io.WriteString(w, "event: transcript.text.done\ndata: {\"type\":\"transcript.text.done\",\"text\":\"hello\"}\n\n")
			return
		}
		if strings.HasSuffix(r.URL.Path, "/audio/translations") {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "translated")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"hello"}`)
	}))
	t.Cleanup(upstream.Close)

	handler, control := newTestRuntime(t, RuntimeConfig{})
	fixture.handler, fixture.control = handler, control
	if artifactPolicy == controlplane.GatewayArtifactPolicyTemporary || artifactPolicy == controlplane.GatewayArtifactPolicyManaged {
		if err := control.SetArtifactStore(controlplane.NewMemoryArtifactStore()); err != nil {
			t.Fatal(err)
		}
	}
	provider, err := control.CreateProvider(context.Background(), "test", controlplane.ProviderRequest{
		Name: "Audio protocol provider", Type: "openai_compatible", BaseURL: upstream.URL + "/v1", Status: controlplane.ProviderStatusActive,
		Models: []string{"audio-upstream"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "public-audio-protocol", Name: "Public audio", Modality: controlplane.GatewayModalityAudio,
		DefaultRouteGroup: "default", Status: controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	account := createGatewayTestAccount(t, control, provider, "audio-upstream", "provider-secret", 10, 4)
	if _, err := control.CreateModelRoute(context.Background(), "test", controlplane.ModelRouteRequest{
		GatewayModelID: model.ID, RouteGroup: "default", ProviderAccountID: account.ID, UpstreamModel: "audio-upstream",
		Priority: 10, Weight: 100, Status: controlplane.ModelRouteStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "audio protocol caller", ModelAllowlist: []string{"public-audio-protocol"}, Scopes: []string{controlplane.GatewayScopeInvoke},
		AllowedModalities: []string{controlplane.GatewayModalityAudio},
		AllowedOperations: []string{controlplane.GatewayOperationAudioTranscription, controlplane.GatewayOperationAudioTranslation, controlplane.GatewayOperationSpeechGeneration},
		ArtifactPolicy:    artifactPolicy,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.key = key.Key
	return fixture
}

func (f *audioProtocolFixture) appendRequest(request audioUpstreamRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, request)
}

func (f *audioProtocolFixture) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func newAudioMultipartRequest(t *testing.T, target, key, idempotencyKey string, fields map[string]string, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	file, err := writer.CreateFormFile("file", "sample.wav")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, target, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return request
}

func TestGatewayAudioTranscriptionForwardsMultipartAndDoesNotDuplicateInputArtifact(t *testing.T) {
	fixture := newAudioProtocolFixture(t, controlplane.GatewayArtifactPolicyProxyOnly)
	content := []byte("synthetic-wave")
	request := newAudioMultipartRequest(t, "/v1/audio/transcriptions", fixture.key, "audio-transcription-idem", map[string]string{
		"model": "public-audio-protocol", "language": "en",
	}, content)
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"text":"hello"`) {
		t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if response.Header().Get("X-AsterRouter-Input-Artifact-ID") == "" || response.Header().Get("X-AsterRouter-Operation-ID") == "" {
		t.Fatalf("missing operation or input artifact headers: %v", response.Header())
	}
	fixture.mu.Lock()
	upstream := fixture.requests[0]
	fixture.mu.Unlock()
	if upstream.path != "/v1/audio/transcriptions" || upstream.model != "audio-upstream" || !bytes.Equal(upstream.file, content) || !strings.HasPrefix(upstream.contentType, "multipart/form-data;") {
		t.Fatalf("upstream=%+v", upstream)
	}
	artifacts, err := fixture.control.ListArtifactsAdmin(context.Background(), controlplane.ArtifactQuery{OperationID: response.Header().Get("X-AsterRouter-Operation-ID"), Limit: 10})
	if err != nil || len(artifacts) != 1 || artifacts[0].Role != controlplane.ArtifactRoleInput || artifacts[0].StoreDriver != controlplane.ArtifactStoreDriverNone || artifacts[0].SizeBytes != int64(len(content)) {
		t.Fatalf("artifacts=%+v err=%v", artifacts, err)
	}

	replay := newAudioMultipartRequest(t, "/v1/audio/transcriptions", fixture.key, "audio-transcription-idem", map[string]string{"model": "public-audio-protocol", "language": "en"}, content)
	replayResponse := httptest.NewRecorder()
	fixture.handler.ServeHTTP(replayResponse, replay)
	if replayResponse.Code != http.StatusConflict || fixture.requestCount() != 1 {
		t.Fatalf("replay status=%d upstream_calls=%d body=%s", replayResponse.Code, fixture.requestCount(), replayResponse.Body.String())
	}
	artifacts, err = fixture.control.ListArtifactsAdmin(context.Background(), controlplane.ArtifactQuery{OperationID: response.Header().Get("X-AsterRouter-Operation-ID"), Limit: 10})
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("replay artifacts=%+v err=%v", artifacts, err)
	}
}

func TestGatewayAudioSpeechReturnsBinaryAndAudioStreamsTerminate(t *testing.T) {
	fixture := newAudioProtocolFixture(t, controlplane.GatewayArtifactPolicyProxyOnly)
	speech := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(`{"model":"public-audio-protocol","input":"hello","voice":"alloy","response_format":"mp3","response_mode":"blocking"}`))
	speech.Header.Set("Content-Type", "application/json")
	speech.Header.Set("Authorization", "Bearer "+fixture.key)
	speech.Header.Set("Idempotency-Key", "speech-binary")
	speechResponse := httptest.NewRecorder()
	fixture.handler.ServeHTTP(speechResponse, speech)
	if speechResponse.Code != http.StatusOK || speechResponse.Header().Get("Content-Type") != "audio/mpeg" || speechResponse.Body.String() != "synthetic-audio" {
		t.Fatalf("speech status=%d headers=%v body=%q", speechResponse.Code, speechResponse.Header(), speechResponse.Body.String())
	}
	fixture.mu.Lock()
	speechUpstream := fixture.requests[0]
	fixture.mu.Unlock()
	if bytes.Contains(speechUpstream.body, []byte("response_mode")) {
		t.Fatalf("AsterRouter response_mode leaked upstream: %s", speechUpstream.body)
	}

	transcription := newAudioMultipartRequest(t, "/v1/audio/transcriptions", fixture.key, "transcription-stream", map[string]string{
		"model": "public-audio-protocol", "stream": "true",
	}, []byte("stream-wave"))
	transcriptionResponse := httptest.NewRecorder()
	fixture.handler.ServeHTTP(transcriptionResponse, transcription)
	if transcriptionResponse.Code != http.StatusOK || !strings.Contains(transcriptionResponse.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(transcriptionResponse.Body.String(), "transcript.text.done") {
		t.Fatalf("transcription stream status=%d headers=%v body=%s", transcriptionResponse.Code, transcriptionResponse.Header(), transcriptionResponse.Body.String())
	}

	streamSpeech := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(`{"model":"public-audio-protocol","input":"hello","voice":"alloy","stream_format":"sse"}`))
	streamSpeech.Header.Set("Content-Type", "application/json")
	streamSpeech.Header.Set("Authorization", "Bearer "+fixture.key)
	streamSpeech.Header.Set("Idempotency-Key", "speech-stream")
	streamSpeechResponse := httptest.NewRecorder()
	fixture.handler.ServeHTTP(streamSpeechResponse, streamSpeech)
	if streamSpeechResponse.Code != http.StatusOK || !strings.Contains(streamSpeechResponse.Body.String(), "audio.done") {
		t.Fatalf("speech stream status=%d headers=%v body=%s", streamSpeechResponse.Code, streamSpeechResponse.Header(), streamSpeechResponse.Body.String())
	}
}

func TestGatewayAudioSpeechRetainsOutputUnderTemporaryPolicy(t *testing.T) {
	fixture := newAudioProtocolFixture(t, controlplane.GatewayArtifactPolicyTemporary)
	request := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(`{"model":"public-audio-protocol","input":"hello","voice":"alloy"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+fixture.key)
	request.Header.Set("Idempotency-Key", "speech-temporary")
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("X-AsterRouter-Output-Artifact-ID") == "" || response.Body.String() != "synthetic-audio" {
		t.Fatalf("status=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
	artifacts, err := fixture.control.ListArtifactsAdmin(context.Background(), controlplane.ArtifactQuery{OperationID: response.Header().Get("X-AsterRouter-Operation-ID"), Limit: 10})
	if err != nil || len(artifacts) != 1 || artifacts[0].Role != controlplane.ArtifactRoleFinal || artifacts[0].StoreDriver != controlplane.ArtifactStoreDriverMemory || artifacts[0].SizeBytes != int64(len("synthetic-audio")) {
		t.Fatalf("artifacts=%+v err=%v", artifacts, err)
	}
}

func TestGatewayAudioStreamRejectsRetentionPolicyBeforeUpstream(t *testing.T) {
	fixture := newAudioProtocolFixture(t, controlplane.GatewayArtifactPolicyTemporary)
	request := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(`{"model":"public-audio-protocol","input":"hello","voice":"alloy","stream_format":"sse"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+fixture.key)
	request.Header.Set("Idempotency-Key", "speech-stream-retention")
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || fixture.requestCount() != 0 || !strings.Contains(response.Body.String(), "unsupported_artifact_policy") {
		t.Fatalf("status=%d upstream_calls=%d body=%s", response.Code, fixture.requestCount(), response.Body.String())
	}
}

func TestGatewayAudioRejectsMissingCredentialBeforeUpstream(t *testing.T) {
	fixture := newAudioProtocolFixture(t, controlplane.GatewayArtifactPolicyProxyOnly)
	request := newAudioMultipartRequest(t, "/v1/audio/translations", "", "translation-no-auth", map[string]string{"model": "public-audio-protocol"}, []byte("wave"))
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || fixture.requestCount() != 0 {
		t.Fatalf("status=%d upstream_calls=%d body=%s", response.Code, fixture.requestCount(), response.Body.String())
	}
}

func TestGatewayAudioTranslationPreservesTextResponse(t *testing.T) {
	fixture := newAudioProtocolFixture(t, controlplane.GatewayArtifactPolicyProxyOnly)
	request := newAudioMultipartRequest(t, "/v1/audio/translations", fixture.key, "translation-text", map[string]string{"model": "public-audio-protocol"}, []byte("wave"))
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain") || response.Body.String() != "translated" {
		t.Fatalf("status=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
	fixture.mu.Lock()
	upstream := fixture.requests[0]
	fixture.mu.Unlock()
	if upstream.path != "/v1/audio/translations" || upstream.model != "audio-upstream" {
		t.Fatalf("upstream=%+v", upstream)
	}
}

func TestGatewayAudioAsyncRequiresUploadAndDurableJob(t *testing.T) {
	fixture := newAudioProtocolFixture(t, controlplane.GatewayArtifactPolicyProxyOnly)
	speech := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(`{"model":"public-audio-protocol","input":"hello","voice":"alloy","response_mode":"async"}`))
	speech.Header.Set("Content-Type", "application/json")
	speech.Header.Set("Authorization", "Bearer "+fixture.key)
	speech.Header.Set("Idempotency-Key", "speech-async")
	speechResponse := httptest.NewRecorder()
	fixture.handler.ServeHTTP(speechResponse, speech)
	if speechResponse.Code != http.StatusBadRequest {
		t.Fatalf("speech async status=%d body=%s", speechResponse.Code, speechResponse.Body.String())
	}

	transcription := newAudioMultipartRequest(t, "/v1/audio/transcriptions", fixture.key, "transcription-async", map[string]string{
		"model": "public-audio-protocol", "response_mode": "async",
	}, []byte("wave"))
	transcriptionResponse := httptest.NewRecorder()
	fixture.handler.ServeHTTP(transcriptionResponse, transcription)
	if transcriptionResponse.Code != http.StatusBadRequest || fixture.requestCount() != 0 {
		t.Fatalf("transcription async status=%d upstream_calls=%d body=%s", transcriptionResponse.Code, fixture.requestCount(), transcriptionResponse.Body.String())
	}
}

func TestGatewayAudioDurableFlowUsesUploadArtifactAndIdempotentJob(t *testing.T) {
	fixture := newAudioProtocolFixture(t, controlplane.GatewayArtifactPolicyTemporary)
	key, err := fixture.control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "audio durable caller", ModelAllowlist: []string{"public-audio-protocol"},
		Scopes:            []string{controlplane.GatewayScopeInvoke, controlplane.GatewayScopeArtifactsWrite, controlplane.GatewayScopeArtifactsRead, controlplane.GatewayScopeJobsRead},
		AllowedModalities: []string{controlplane.GatewayModalityAudio}, AllowedOperations: []string{controlplane.GatewayOperationAudioTranscription},
		LanePolicy: controlplane.GatewayLanePolicyDurableOnly, ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("durable-wave")
	digest := sha256.Sum256(payload)
	upload := httptest.NewRequest(http.MethodPost, "/v1/uploads", bytes.NewReader(payload))
	upload.Header.Set("Authorization", "Bearer "+key.Key)
	upload.Header.Set("Idempotency-Key", "audio-durable-upload")
	upload.Header.Set("Content-Type", "audio/wav")
	upload.Header.Set("X-Checksum-SHA256", hex.EncodeToString(digest[:]))
	uploadResponse := httptest.NewRecorder()
	fixture.handler.ServeHTTP(uploadResponse, upload)
	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", uploadResponse.Code, uploadResponse.Body.String())
	}
	var uploaded publicUploadResponse
	if err := json.Unmarshal(uploadResponse.Body.Bytes(), &uploaded); err != nil || uploaded.ArtifactID == "" {
		t.Fatalf("upload=%+v err=%v", uploaded, err)
	}
	jobBody := `{"model":"public-audio-protocol","operation":"audio_transcription","modality":"audio","input":{"artifact_id":"` + uploaded.ArtifactID + `","duration_seconds":1,"language":"en"}}`
	job := performGatewayJobRequest(fixture.handler, http.MethodPost, "/v1/jobs", key.Key, "audio-durable-job", jobBody)
	if job.Code != http.StatusAccepted {
		t.Fatalf("job status=%d body=%s", job.Code, job.Body.String())
	}
	var accepted publicAIJobResponse
	if err := json.Unmarshal(job.Body.Bytes(), &accepted); err != nil || accepted.Capability.Operation != controlplane.GatewayOperationAudioTranscription || accepted.Capability.Modality != controlplane.GatewayModalityAudio {
		t.Fatalf("job=%+v err=%v", accepted, err)
	}
	replay := performGatewayJobRequest(fixture.handler, http.MethodPost, "/v1/jobs", key.Key, "audio-durable-job", jobBody)
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotent-Replayed") != "true" || !strings.Contains(replay.Body.String(), accepted.ID) {
		t.Fatalf("replay status=%d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	}
}
