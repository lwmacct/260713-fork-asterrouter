package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

type nativeProtocolFixture struct {
	handler http.Handler
	control *controlplane.Service
	key     string
	mu      sync.Mutex
	paths   []string
	headers []http.Header
	bodies  [][]byte
}

func newNativeProtocolFixture(t *testing.T) *nativeProtocolFixture {
	t.Helper()
	fixture := &nativeProtocolFixture{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fixture.mu.Lock()
		fixture.paths = append(fixture.paths, r.URL.Path)
		fixture.headers = append(fixture.headers, r.Header.Clone())
		fixture.bodies = append(fixture.bodies, append([]byte(nil), body...))
		fixture.mu.Unlock()
		stream := strings.Contains(r.Header.Get("Accept"), "text/event-stream")
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			switch {
			case strings.HasSuffix(r.URL.Path, "/messages"):
				_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\n")
				_, _ = io.WriteString(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":2}}\n\n")
				_, _ = io.WriteString(w, "event: message_stop\ndata: {}\n\n")
			case strings.Contains(r.URL.Path, ":streamGenerateContent"):
				_, _ = io.WriteString(w, "data: {\"candidates\":[{\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":2}}\n\n")
			default:
				_, _ = io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\"}\n\n")
				_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"usage\":{\"input_tokens\":3,\"output_tokens\":2}}\n\n")
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","content":[],"usage":{"input_tokens":3,"output_tokens":2}}`)
		case strings.Contains(r.URL.Path, ":generateContent"):
			_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`)
		default:
			_, _ = io.WriteString(w, `{"id":"resp_1","object":"response","output":[],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`)
		}
	}))
	t.Cleanup(upstream.Close)
	handler, control := newTestRuntime(t, RuntimeConfig{})
	fixture.handler, fixture.control = handler, control
	provider, err := control.CreateProvider(context.Background(), "test", controlplane.ProviderRequest{
		Name: "Native protocol provider", Type: "openai_compatible", BaseURL: upstream.URL + "/v1", Status: controlplane.ProviderStatusActive, Models: []string{"native-upstream"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "native-chat", Name: "Native chat", Modality: "chat", DefaultRouteGroup: "default", Status: controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	account := createGatewayTestAccount(t, control, provider, "native-upstream", "provider-secret", 10, 4)
	if _, err := control.CreateModelRoute(context.Background(), "test", controlplane.ModelRouteRequest{
		GatewayModelID: model.ID, RouteGroup: "default", ProviderAccountID: account.ID, UpstreamModel: "native-upstream", Priority: 10, Weight: 100, Status: controlplane.ModelRouteStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "native protocol caller", ModelAllowlist: []string{"native-chat"}, Scopes: []string{controlplane.GatewayScopeInvoke}, MonthlyTokenLimit: 10000,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.key = key.Key
	return fixture
}

func TestGatewayNativeProtocolsForwardJSONAndCredentialHeaders(t *testing.T) {
	fixture := newNativeProtocolFixture(t)
	tests := []struct {
		name       string
		path       string
		credential string
		body       string
		wantPath   string
		wantHeader string
		wantValue  string
		wantBody   string
	}{
		{name: "responses", path: "/v1/responses", credential: "Bearer ", body: `{"model":"native-chat","input":"hello","stream":false}`, wantPath: "/v1/responses", wantHeader: "Authorization", wantValue: "Bearer provider-secret", wantBody: `"model":"native-upstream"`},
		{name: "anthropic", path: "/v1/messages", credential: "X-API-Key: ", body: `{"model":"native-chat","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`, wantPath: "/v1/messages", wantHeader: "x-api-key", wantValue: "provider-secret", wantBody: `"model":"native-upstream"`},
		{name: "gemini", path: "/v1beta/models/native-chat:generateContent", credential: "X-Goog-API-Key: ", body: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, wantPath: "/v1/models/native-upstream:generateContent", wantHeader: "x-goog-api-key", wantValue: "provider-secret", wantBody: `"model"`},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "native-json-"+test.name+"-"+string(rune('a'+index)))
			if strings.HasPrefix(test.credential, "Bearer") {
				request.Header.Set("Authorization", "Bearer "+fixture.key)
			} else {
				parts := strings.SplitN(test.credential, ": ", 2)
				request.Header.Set(parts[0], fixture.key)
			}
			response := httptest.NewRecorder()
			fixture.handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			fixture.mu.Lock()
			if len(fixture.paths) == 0 {
				fixture.mu.Unlock()
				t.Fatalf("upstream was not called; body=%s", response.Body.String())
			}
			path := fixture.paths[len(fixture.paths)-1]
			headers := fixture.headers[len(fixture.headers)-1]
			body := fixture.bodies[len(fixture.bodies)-1]
			fixture.mu.Unlock()
			if path != test.wantPath || headers.Get(test.wantHeader) != test.wantValue {
				t.Fatalf("upstream path=%q headers=%v", path, headers)
			}
			if test.name == "gemini" {
				if bytes.Contains(body, []byte(`"model"`)) {
					t.Fatalf("Gemini body unexpectedly contains model: %s", body)
				}
			} else if !bytes.Contains(body, []byte(test.wantBody)) {
				t.Fatalf("rewritten body=%s missing %s", body, test.wantBody)
			}
		})
	}
}

func TestGatewayNativeProtocolsRecognizeSSETerminalEventsAndUsage(t *testing.T) {
	fixture := newNativeProtocolFixture(t)
	tests := []struct {
		name string
		path string
		body string
		set  func(*http.Request)
	}{
		{name: "responses", path: "/v1/responses", body: `{"model":"native-chat","input":"hello","stream":true}`, set: func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+fixture.key) }},
		{name: "anthropic", path: "/v1/messages", body: `{"model":"native-chat","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":true}`, set: func(r *http.Request) { r.Header.Set("X-API-Key", fixture.key) }},
		{name: "gemini", path: "/v1beta/models/native-chat:streamGenerateContent", body: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, set: func(r *http.Request) { r.Header.Set("X-Goog-API-Key", fixture.key) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "native-stream-"+test.name)
			test.set(request)
			response := httptest.NewRecorder()
			fixture.handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/event-stream") {
				t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
			}
			if response.Body.Len() == 0 {
				t.Fatal("stream response is empty")
			}
		})
	}
	usage, err := fixture.control.UsageReport(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage.Recent) < len(tests) {
		encoded, _ := json.Marshal(usage.Recent)
		t.Fatalf("usage records=%s", encoded)
	}
}
