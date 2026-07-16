package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/coder/websocket"
)

type realtimeUpstreamFixture struct {
	server      *httptest.Server
	connections atomic.Int32
	mu          sync.Mutex
	authority   string
	safetyID    string
	model       string
	messages    []map[string]any
	serveErrors []error
}

func newRealtimeUpstreamFixture(t *testing.T) *realtimeUpstreamFixture {
	t.Helper()
	fixture := &realtimeUpstreamFixture{}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.connections.Add(1)
		fixture.mu.Lock()
		fixture.authority = r.Header.Get("Authorization")
		fixture.safetyID = r.Header.Get("OpenAI-Safety-Identifier")
		fixture.model = r.URL.Query().Get("model")
		fixture.mu.Unlock()
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			fixture.addServeError(err)
			return
		}
		defer conn.CloseNow()
		ctx := context.Background()
		if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"session.created","session":{"id":"provider-session"}}`)); err != nil {
			fixture.addServeError(err)
			return
		}
		responseNumber := 0
		for {
			messageType, payload, err := conn.Read(ctx)
			if err != nil {
				status := websocket.CloseStatus(err)
				if status != websocket.StatusNormalClosure && status != websocket.StatusGoingAway {
					fixture.addServeError(err)
				}
				return
			}
			if messageType != websocket.MessageText {
				fixture.addServeError(errors.New("upstream received non-text message"))
				return
			}
			var event map[string]any
			if json.Unmarshal(payload, &event) != nil {
				fixture.addServeError(errors.New("upstream received invalid JSON"))
				return
			}
			fixture.mu.Lock()
			fixture.messages = append(fixture.messages, event)
			fixture.mu.Unlock()
			switch event["type"] {
			case "session.update":
				if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"session.updated","session":{"model":"upstream-realtime"}}`)); err != nil {
					fixture.addServeError(err)
					return
				}
			case "response.create":
				responseNumber++
				delta, _ := json.Marshal(map[string]any{"type": "response.output_audio.delta", "delta": base64.StdEncoding.EncodeToString([]byte{byte(responseNumber), 2, 3})})
				if err := conn.Write(ctx, websocket.MessageText, delta); err != nil {
					fixture.addServeError(err)
					return
				}
				done, _ := json.Marshal(map[string]any{
					"type": "response.done",
					"response": map[string]any{"id": "response", "usage": map[string]any{
						"input_tokens":  responseNumber * 5,
						"output_tokens": responseNumber * 3,
					}},
				})
				if err := conn.Write(ctx, websocket.MessageText, done); err != nil {
					fixture.addServeError(err)
					return
				}
			}
		}
	}))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (fixture *realtimeUpstreamFixture) addServeError(err error) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	fixture.serveErrors = append(fixture.serveErrors, err)
}

func (fixture *realtimeUpstreamFixture) snapshot() (string, string, string, []map[string]any, []error) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	messages := append([]map[string]any(nil), fixture.messages...)
	errorsSeen := append([]error(nil), fixture.serveErrors...)
	return fixture.authority, fixture.safetyID, fixture.model, messages, errorsSeen
}

type realtimeGatewayFixture struct {
	server  *httptest.Server
	control *controlplane.Service
	key     controlplane.APIKeyCreateResponse
}

func newRealtimeGatewayFixture(t *testing.T, upstreamURL string) realtimeGatewayFixture {
	t.Helper()
	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "test", controlplane.ProviderRequest{
		Name: "Realtime provider", Type: "openai_compatible", BaseURL: upstreamURL + "/v1",
		Status: controlplane.ProviderStatusActive, Models: []string{"upstream-realtime"}, APIKey: "provider-fallback-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	account := createGatewayTestAccount(t, control, provider, "upstream-realtime", "provider-realtime-secret", 10, 1)
	model, err := control.CreateGatewayModel(context.Background(), "test", controlplane.GatewayModelRequest{
		ModelID: "public-realtime", Name: "Public realtime", Modality: controlplane.GatewayModalityAudio,
		DefaultRouteGroup: "default", Status: controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.CreateModelRoute(context.Background(), "test", controlplane.ModelRouteRequest{
		GatewayModelID: model.ID, RouteGroup: "default", ProviderAccountID: account.ID,
		UpstreamModel: "upstream-realtime", Priority: 10, Weight: 100, Status: controlplane.ModelRouteStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	key, err := control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "Realtime caller", ModelAllowlist: []string{"public-realtime"}, Scopes: []string{controlplane.GatewayScopeInvoke},
		AllowedModalities: []string{controlplane.GatewayModalityAudio}, AllowedOperations: []string{controlplane.GatewayOperationRealtimeSession},
		ConcurrencyLimit: 1, ArtifactPolicy: controlplane.GatewayArtifactPolicyProxyOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return realtimeGatewayFixture{server: server, control: control, key: key}
}

func TestGatewayRealtimeRelaysAndAccountsSession(t *testing.T) {
	upstream := newRealtimeUpstreamFixture(t)
	fixture := newRealtimeGatewayFixture(t, upstream.server.URL)
	target := strings.Replace(fixture.server.URL, "http://", "ws://", 1) + "/v1/realtime?model=public-realtime"
	conn, response, err := websocket.Dial(context.Background(), target, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Idempotency-Key": []string{"realtime-success-1"},
			"X-AsterRouter-Estimated-Audio-Duration-Ms": []string{"60000"},
		},
		Subprotocols: []string{"realtime", "openai-insecure-api-key." + fixture.key.Key},
	})
	if err != nil {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		t.Fatalf("dial gateway status=%d err=%v", status, err)
	}
	operationID := response.Header.Get("X-AsterRouter-Operation-ID")
	sessionID := response.Header.Get("X-AsterRouter-Realtime-Session-ID")
	if operationID == "" || sessionID == "" || conn.Subprotocol() != "realtime" || strings.Contains(response.Header.Get("Sec-WebSocket-Protocol"), fixture.key.Key) {
		t.Fatalf("handshake operation=%q session=%q subprotocol=%q", operationID, sessionID, conn.Subprotocol())
	}
	readRealtimeEvent(t, conn, "session.created")
	writeRealtimeEvent(t, conn, map[string]any{"type": "session.update", "event_id": "event-session", "session": map[string]any{"model": "public-realtime"}})
	writeRealtimeEvent(t, conn, map[string]any{"type": "session.update", "event_id": "event-session", "session": map[string]any{"model": "must-not-forward"}})
	readRealtimeEvent(t, conn, "session.updated")
	writeRealtimeEvent(t, conn, map[string]any{"type": "input_audio_buffer.append", "event_id": "event-audio", "audio": base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})})
	for index := 1; index <= 2; index++ {
		writeRealtimeEvent(t, conn, map[string]any{"type": "response.create", "event_id": "event-response-" + string(rune('0'+index)), "response": map[string]any{"model": "public-realtime"}})
		readRealtimeEvent(t, conn, "response.output_audio.delta")
		readRealtimeEvent(t, conn, "response.done")
	}
	if err := conn.Close(websocket.StatusNormalClosure, "done"); err != nil {
		t.Fatalf("close realtime client: %v", err)
	}
	replay, replayResponse, replayErr := websocket.Dial(context.Background(), target, &websocket.DialOptions{
		HTTPHeader:   http.Header{"Idempotency-Key": []string{"realtime-success-1"}, "X-AsterRouter-Estimated-Audio-Duration-Ms": []string{"60000"}},
		Subprotocols: []string{"realtime", "openai-insecure-api-key." + fixture.key.Key},
	})
	if replay != nil {
		replay.CloseNow()
		t.Fatal("idempotency replay opened a second realtime connection")
	}
	if replayErr == nil || replayResponse == nil || replayResponse.StatusCode != http.StatusConflict {
		t.Fatalf("replay response=%v err=%v", replayResponse, replayErr)
	}

	operation := waitForRealtimeOperation(t, fixture.control, operationID)
	if operation.Status != controlplane.AIOperationStatusSucceeded || operation.ErrorType != "" {
		t.Fatalf("operation=%+v", operation)
	}
	session, found, err := fixture.control.RealtimeSession(context.Background(), sessionID)
	if err != nil || !found || session.Status != controlplane.RealtimeSessionStatusCompleted || session.OperationID != operationID ||
		session.InputAudioBytes != 4 || session.OutputAudioBytes != 6 || session.ClientMessageCount != 5 || session.ProviderMessageCount != 6 ||
		session.UsageVersion != 3 || session.ConnectedAt == nil || session.ClosedAt == nil {
		t.Fatalf("session=%+v found=%t err=%v", session, found, err)
	}
	attempt, found, err := fixture.control.AIAttempt(context.Background(), session.AttemptID)
	if err != nil || !found || attempt.Status != controlplane.AIAttemptStatusSucceeded || attempt.UpstreamModel != "upstream-realtime" {
		t.Fatalf("attempt=%+v found=%t err=%v", attempt, found, err)
	}
	usage, err := fixture.control.UsageReport(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	records := make([]controlplane.UsageRecord, 0, 3)
	for _, record := range usage.Recent {
		if record.OperationID == operationID {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].UsageVersion < records[j].UsageVersion })
	if len(records) != 3 || records[0].UsageSource != "provider_incremental" || records[1].UsageSource != "provider_incremental" || records[2].UsageSource != "gateway_final" ||
		records[0].InputTokens != 5 || records[0].OutputTokens != 3 || records[1].InputTokens != 10 || records[1].OutputTokens != 6 ||
		records[2].UsageDimensions[controlplane.UsageDimensionInputBytes].Quantity != 4 || records[2].UsageDimensions[controlplane.UsageDimensionOutputBytes].Quantity != 6 ||
		records[2].UsageDimensions[controlplane.UsageDimensionRealtimeAudioMilliseconds].Unit != controlplane.UsageUnitMillisecond {
		t.Fatalf("realtime usage=%+v", records)
	}
	hold, found, err := fixture.control.BillingHoldForOperation(context.Background(), operationID)
	if err != nil || !found || hold.Status != controlplane.BillingHoldStatusSettled {
		t.Fatalf("hold=%+v found=%t err=%v", hold, found, err)
	}
	traces, err := fixture.control.ListGatewayTraces(context.Background(), 20)
	if err != nil || len(traces) == 0 || traces[0].OperationID != operationID || traces[0].AttemptID != session.AttemptID || traces[0].MessageCount != 11 {
		t.Fatalf("traces=%+v err=%v", traces, err)
	}
	authority, safetyID, upstreamModel, messages, serveErrors := upstream.snapshot()
	if authority != "Bearer provider-realtime-secret" || !strings.HasPrefix(safetyID, "aster_") || strings.Contains(safetyID, fixture.key.Key) || upstreamModel != "upstream-realtime" || len(serveErrors) != 0 {
		t.Fatalf("upstream authority=%q safety=%q model=%q errors=%v", authority, safetyID, upstreamModel, serveErrors)
	}
	if len(messages) != 4 || nestedRealtimeModel(messages[0], "session") != "upstream-realtime" || nestedRealtimeModel(messages[2], "response") != "upstream-realtime" || nestedRealtimeModel(messages[3], "response") != "upstream-realtime" {
		t.Fatalf("upstream messages=%+v", messages)
	}
	assertRealtimePermitsReleased(t, fixture)
}

func TestGatewayRealtimeRejectsBeforeUpstreamUpgrade(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		http.Error(w, "must not be called", http.StatusInternalServerError)
	}))
	defer upstream.Close()
	fixture := newRealtimeGatewayFixture(t, upstream.URL)
	wrongPolicy, err := fixture.control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "Wrong realtime policy", ModelAllowlist: []string{"public-realtime"}, Scopes: []string{controlplane.GatewayScopeInvoke},
		AllowedModalities: []string{controlplane.GatewayModalityAudio}, AllowedOperations: []string{controlplane.GatewayOperationSpeechGeneration},
	})
	if err != nil {
		t.Fatal(err)
	}
	base := fixture.server.URL + "/v1/realtime?model=public-realtime"
	tests := []struct {
		name   string
		target string
		header http.Header
		status int
	}{
		{name: "missing credential", target: base, status: http.StatusUnauthorized},
		{name: "query credential", target: base + "&api_key=" + fixture.key.Key, status: http.StatusUnauthorized},
		{name: "conflicting credential", target: base, header: http.Header{"Authorization": []string{"Bearer " + fixture.key.Key}, "Sec-WebSocket-Protocol": []string{"realtime, openai-insecure-api-key." + fixture.key.Key}}, status: http.StatusUnauthorized},
		{name: "operation policy", target: base, header: http.Header{"Authorization": []string{"Bearer " + wrongPolicy.Key}, "Idempotency-Key": []string{"wrong-policy"}}, status: http.StatusForbidden},
		{name: "duplicate model", target: base + "&model=other", header: http.Header{"Authorization": []string{"Bearer " + fixture.key.Key}, "Idempotency-Key": []string{"duplicate-model"}}, status: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, test.target, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header = test.header
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.status {
				t.Fatalf("status=%d, want %d", response.StatusCode, test.status)
			}
		})
	}
	if upstreamCalls.Load() != 0 {
		t.Fatalf("rejected requests reached upstream %d times", upstreamCalls.Load())
	}
}

func TestGatewayRealtimeCapacityIsHeldUntilSessionCloses(t *testing.T) {
	upstream := newRealtimeUpstreamFixture(t)
	fixture := newRealtimeGatewayFixture(t, upstream.server.URL)
	secondKey, err := fixture.control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "Second realtime caller", ModelAllowlist: []string{"public-realtime"}, Scopes: []string{controlplane.GatewayScopeInvoke},
		AllowedModalities: []string{controlplane.GatewayModalityAudio}, AllowedOperations: []string{controlplane.GatewayOperationRealtimeSession},
		ConcurrencyLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	first := dialRealtimeWithBearer(t, fixture.server.URL, fixture.key.Key, "capacity-first", true)
	readRealtimeEvent(t, first, "session.created")
	assertRealtimeDialStatus(t, fixture.server.URL, fixture.key.Key, "capacity-same-key", http.StatusTooManyRequests)
	assertRealtimeDialStatus(t, fixture.server.URL, secondKey.Key, "capacity-provider", http.StatusTooManyRequests)
	if upstream.connections.Load() != 1 {
		t.Fatalf("capacity-rejected sessions reached upstream: connections=%d", upstream.connections.Load())
	}
	if err := first.Close(websocket.StatusNormalClosure, "release capacity"); err != nil {
		t.Fatal(err)
	}
	assertRealtimePermitsReleased(t, fixture)
	second := dialRealtimeWithBearer(t, fixture.server.URL, secondKey.Key, "capacity-after-release", true)
	readRealtimeEvent(t, second, "session.created")
	if err := second.Close(websocket.StatusNormalClosure, "done"); err != nil {
		t.Fatal(err)
	}
	if upstream.connections.Load() != 2 {
		t.Fatalf("released provider capacity was not reusable: connections=%d", upstream.connections.Load())
	}
}

func TestGatewayRealtimeStopsAfterOngoingTokenQuota(t *testing.T) {
	upstream := newRealtimeUpstreamFixture(t)
	fixture := newRealtimeGatewayFixture(t, upstream.server.URL)
	quotaKey, err := fixture.control.CreateAPIKey(context.Background(), "test", controlplane.APIKeyCreateRequest{
		Name: "Quota realtime caller", ModelAllowlist: []string{"public-realtime"}, Scopes: []string{controlplane.GatewayScopeInvoke},
		AllowedModalities: []string{controlplane.GatewayModalityAudio}, AllowedOperations: []string{controlplane.GatewayOperationRealtimeSession},
		MonthlyTokenLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	target := strings.Replace(fixture.server.URL, "http://", "ws://", 1) + "/v1/realtime?model=public-realtime"
	conn, response, err := websocket.Dial(context.Background(), target, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + quotaKey.Key}, "Idempotency-Key": []string{"realtime-quota"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	readRealtimeEvent(t, conn, "session.created")
	writeRealtimeEvent(t, conn, map[string]any{"type": "response.create", "event_id": "quota-response"})
	readRealtimeEvent(t, conn, "response.output_audio.delta")
	readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_, _, readErr := conn.Read(readCtx)
	cancel()
	if readErr == nil || websocket.CloseStatus(readErr) != websocket.StatusPolicyViolation {
		t.Fatalf("quota close error=%v status=%d", readErr, websocket.CloseStatus(readErr))
	}
	operationID := response.Header.Get("X-AsterRouter-Operation-ID")
	sessionID := response.Header.Get("X-AsterRouter-Realtime-Session-ID")
	operation := waitForRealtimeOperation(t, fixture.control, operationID)
	session, found, err := fixture.control.RealtimeSession(context.Background(), sessionID)
	if err != nil || !found || operation.Status != controlplane.AIOperationStatusFailed || operation.ErrorType != "quota_exceeded" || session.Status != controlplane.RealtimeSessionStatusFailed || session.ErrorType != "quota_exceeded" || session.UsageVersion != 2 {
		t.Fatalf("operation=%+v session=%+v found=%t err=%v", operation, session, found, err)
	}
	usage, err := fixture.control.UsageReport(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	versions := make([]int, 0, 2)
	for _, record := range usage.Recent {
		if record.OperationID == operationID {
			versions = append(versions, record.UsageVersion)
		}
	}
	sort.Ints(versions)
	if len(versions) != 2 || versions[0] != 1 || versions[1] != 2 {
		t.Fatalf("quota usage versions=%v", versions)
	}
	hold, found, err := fixture.control.BillingHoldForOperation(context.Background(), operationID)
	if err != nil || !found || hold.Status != controlplane.BillingHoldStatusDisputed {
		t.Fatalf("hold=%+v found=%t err=%v", hold, found, err)
	}
}

func TestGatewayRealtimeRevalidatesCredentialAndPolicyAtUsageBoundary(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*testing.T, realtimeGatewayFixture)
		wantError string
	}{
		{
			name: "disabled key", wantError: "credential_revoked",
			mutate: func(t *testing.T, fixture realtimeGatewayFixture) {
				t.Helper()
				if err := fixture.control.DisableAPIKey(context.Background(), "test", fixture.key.Record.ID); err != nil {
					t.Fatalf("DisableAPIKey(): %v", err)
				}
			},
		},
		{
			name: "operation policy tightened", wantError: "policy_revoked",
			mutate: func(t *testing.T, fixture realtimeGatewayFixture) {
				t.Helper()
				record := fixture.key.Record
				_, err := fixture.control.UpdateAPIKey(context.Background(), "test", record.ID, controlplane.APIKeyUpdateRequest{
					Name: record.Name, ModelAllowlist: record.ModelAllowlist, Scopes: []string{controlplane.GatewayScopeInvoke},
					AllowedModalities: []string{controlplane.GatewayModalityAudio}, AllowedOperations: []string{controlplane.GatewayOperationSpeechGeneration},
				})
				if err != nil {
					t.Fatalf("UpdateAPIKey(): %v", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := newRealtimeUpstreamFixture(t)
			fixture := newRealtimeGatewayFixture(t, upstream.server.URL)
			target := strings.Replace(fixture.server.URL, "http://", "ws://", 1) + "/v1/realtime?model=public-realtime"
			conn, response, err := websocket.Dial(context.Background(), target, &websocket.DialOptions{HTTPHeader: http.Header{
				"Authorization": []string{"Bearer " + fixture.key.Key}, "Idempotency-Key": []string{"realtime-revalidate-" + strings.ReplaceAll(test.name, " ", "-")},
			}})
			if err != nil {
				t.Fatalf("dial realtime: %v", err)
			}
			readRealtimeEvent(t, conn, "session.created")
			test.mutate(t, fixture)
			writeRealtimeEvent(t, conn, map[string]any{"type": "response.create", "event_id": "revalidate-response"})
			readRealtimeEvent(t, conn, "response.output_audio.delta")
			readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_, _, readErr := conn.Read(readCtx)
			cancel()
			if readErr == nil || websocket.CloseStatus(readErr) != websocket.StatusPolicyViolation {
				t.Fatalf("revalidation close error=%v status=%d", readErr, websocket.CloseStatus(readErr))
			}
			operationID := response.Header.Get("X-AsterRouter-Operation-ID")
			sessionID := response.Header.Get("X-AsterRouter-Realtime-Session-ID")
			operation := waitForRealtimeOperation(t, fixture.control, operationID)
			session, found, sessionErr := fixture.control.RealtimeSession(context.Background(), sessionID)
			if sessionErr != nil || !found || operation.Status != controlplane.AIOperationStatusFailed || operation.ErrorType != test.wantError ||
				session.Status != controlplane.RealtimeSessionStatusFailed || session.ErrorType != test.wantError {
				t.Fatalf("operation=%+v session=%+v found=%t err=%v", operation, session, found, sessionErr)
			}
		})
	}
}

func TestGatewayRealtimeUpstreamHandshakeFailureDoesNotUpgradeClient(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "synthetic unavailable", http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	fixture := newRealtimeGatewayFixture(t, upstream.URL)
	target := strings.Replace(fixture.server.URL, "http://", "ws://", 1) + "/v1/realtime?model=public-realtime"
	conn, response, err := websocket.Dial(context.Background(), target, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization":   []string{"Bearer " + fixture.key.Key},
		"Idempotency-Key": []string{"realtime-handshake-failure"},
	}})
	if conn != nil {
		conn.CloseNow()
		t.Fatal("client WebSocket was upgraded despite upstream handshake failure")
	}
	if err == nil || response == nil || response.StatusCode != http.StatusBadGateway {
		t.Fatalf("response=%v err=%v", response, err)
	}
	operationID := response.Header.Get("X-AsterRouter-Operation-ID")
	operation := waitForRealtimeOperation(t, fixture.control, operationID)
	if operation.Status != controlplane.AIOperationStatusFailed || operation.ErrorType != "upstream_handshake_error" {
		t.Fatalf("operation=%+v", operation)
	}
	hold, found, holdErr := fixture.control.BillingHoldForOperation(context.Background(), operationID)
	if holdErr != nil || !found || hold.Status != controlplane.BillingHoldStatusReleased {
		t.Fatalf("hold=%+v found=%t err=%v", hold, found, holdErr)
	}
}

func TestGatewayRealtimeFailsOverBeforeClientUpgrade(t *testing.T) {
	success := newRealtimeUpstreamFixture(t)
	fixture := newRealtimeGatewayFixture(t, success.server.URL)
	var failedCalls atomic.Int32
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		failedCalls.Add(1)
		http.Error(w, "synthetic first candidate failure", http.StatusServiceUnavailable)
	}))
	defer failing.Close()
	provider, err := fixture.control.CreateProvider(context.Background(), "test", controlplane.ProviderRequest{
		Name: "Failing realtime provider", Type: "openai_compatible", BaseURL: failing.URL + "/v1",
		Status: controlplane.ProviderStatusActive, Models: []string{"failing-upstream"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	account := createGatewayTestAccount(t, fixture.control, provider, "failing-upstream", "failing-secret", 1, 1)
	resolved, found, err := fixture.control.ResolveGatewayModel(context.Background(), "public-realtime")
	if err != nil || !found {
		t.Fatalf("resolved=%+v found=%t err=%v", resolved, found, err)
	}
	if _, err := fixture.control.CreateModelRoute(context.Background(), "test", controlplane.ModelRouteRequest{
		GatewayModelID: resolved.GatewayModel.ID, RouteGroup: "default", ProviderAccountID: account.ID,
		UpstreamModel: "failing-upstream", Priority: 1, Weight: 100, Status: controlplane.ModelRouteStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	target := strings.Replace(fixture.server.URL, "http://", "ws://", 1) + "/v1/realtime?model=public-realtime"
	conn, response, err := websocket.Dial(context.Background(), target, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + fixture.key.Key}, "Idempotency-Key": []string{"realtime-failover"},
	}})
	if err != nil {
		t.Fatalf("failover dial: %v", err)
	}
	readRealtimeEvent(t, conn, "session.created")
	if err := conn.Close(websocket.StatusNormalClosure, "done"); err != nil {
		t.Fatal(err)
	}
	operationID := response.Header.Get("X-AsterRouter-Operation-ID")
	operation := waitForRealtimeOperation(t, fixture.control, operationID)
	if operation.Status != controlplane.AIOperationStatusSucceeded || failedCalls.Load() != 1 || success.connections.Load() != 1 {
		t.Fatalf("operation=%+v failed_calls=%d success_calls=%d", operation, failedCalls.Load(), success.connections.Load())
	}
	traces, err := fixture.control.ListGatewayTraces(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	var routeAttempts string
	for _, trace := range traces {
		if trace.OperationID == operationID {
			routeAttempts = trace.RouteAttempts
			break
		}
	}
	if !strings.Contains(routeAttempts, `"outcome":"failed"`) || !strings.Contains(routeAttempts, `"outcome":"selected"`) {
		t.Fatalf("route attempts=%s", routeAttempts)
	}
}

func TestGatewayRealtimePersistsUnexpectedProviderDisconnect(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_ = conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"session.created","session":{"id":"disconnect"}}`))
		_ = conn.Close(websocket.StatusInternalError, "synthetic provider disconnect")
	}))
	defer upstream.Close()
	fixture := newRealtimeGatewayFixture(t, upstream.URL)
	target := strings.Replace(fixture.server.URL, "http://", "ws://", 1) + "/v1/realtime?model=public-realtime"
	conn, response, err := websocket.Dial(context.Background(), target, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + fixture.key.Key}, "Idempotency-Key": []string{"realtime-provider-disconnect"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	readRealtimeEvent(t, conn, "session.created")
	readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_, _, readErr := conn.Read(readCtx)
	cancel()
	if readErr == nil {
		t.Fatal("unexpected provider disconnect did not close the client session")
	}
	operationID := response.Header.Get("X-AsterRouter-Operation-ID")
	sessionID := response.Header.Get("X-AsterRouter-Realtime-Session-ID")
	operation := waitForRealtimeOperation(t, fixture.control, operationID)
	session, found, err := fixture.control.RealtimeSession(context.Background(), sessionID)
	if err != nil || !found || operation.Status != controlplane.AIOperationStatusFailed || session.Status != controlplane.RealtimeSessionStatusFailed || session.ErrorType != "provider_connection_error" || session.ClosedAt == nil {
		t.Fatalf("operation=%+v session=%+v found=%t err=%v read_err=%v", operation, session, found, err, readErr)
	}
	hold, found, err := fixture.control.BillingHoldForOperation(context.Background(), operationID)
	if err != nil || !found || hold.Status != controlplane.BillingHoldStatusDisputed {
		t.Fatalf("hold=%+v found=%t err=%v", hold, found, err)
	}
	attempt, found, err := fixture.control.AIAttempt(context.Background(), session.AttemptID)
	if err != nil || !found || attempt.Status != controlplane.AIAttemptStatusFailed || attempt.ErrorType != "provider_connection_error" {
		t.Fatalf("attempt=%+v found=%t err=%v", attempt, found, err)
	}
}

func TestRealtimeRelayBoundsAndTermination(t *testing.T) {
	tests := []struct {
		name      string
		configure func(realtimeRelayConfig) realtimeRelayConfig
		trigger   func(*testing.T, *websocket.Conn, chan error, chan error)
		wantType  string
		normal    bool
	}{
		{
			name: "idle timeout", configure: func(config realtimeRelayConfig) realtimeRelayConfig {
				config.IdleTimeout = 20 * time.Millisecond
				return config
			}, wantType: "idle_timeout",
		},
		{
			name: "maximum duration", configure: func(config realtimeRelayConfig) realtimeRelayConfig {
				config.MaxSession = 20 * time.Millisecond
				return config
			}, normal: true,
		},
		{
			name: "binary rejected", trigger: func(t *testing.T, client *websocket.Conn, _, _ chan error) {
				if err := client.Write(context.Background(), websocket.MessageBinary, []byte{1, 2, 3}); err != nil {
					t.Fatal(err)
				}
			}, wantType: "protocol_error",
		},
		{
			name: "oversized message", configure: func(config realtimeRelayConfig) realtimeRelayConfig {
				config.MessageLimit = 64
				return config
			}, trigger: func(t *testing.T, client *websocket.Conn, _, _ chan error) {
				payload := []byte(`{"type":"session.update","event_id":"oversized","padding":"` + strings.Repeat("x", 128) + `"}`)
				_ = client.Write(context.Background(), websocket.MessageText, payload)
			}, wantType: "message_too_large",
		},
		{
			name: "message rate", configure: func(config realtimeRelayConfig) realtimeRelayConfig {
				config.ClientMessagesPS = 2
				return config
			}, trigger: func(t *testing.T, client *websocket.Conn, _, _ chan error) {
				for index := 0; index < 3; index++ {
					writeRealtimeEvent(t, client, map[string]any{"type": "session.update", "event_id": "rate-" + string(rune('0'+index))})
				}
			}, wantType: "message_rate_exceeded",
		},
		{
			name: "credential lease lost", trigger: func(_ *testing.T, _ *websocket.Conn, credentialLost, _ chan error) {
				credentialLost <- errors.New("lost")
			}, wantType: "credential_capacity_lease_lost",
		},
		{
			name: "provider lease lost", trigger: func(_ *testing.T, _ *websocket.Conn, _, providerLost chan error) {
				providerLost <- errors.New("lost")
			}, wantType: "provider_capacity_lease_lost",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			downstreamClient, downstreamRelay := newRealtimeSocketPair(t)
			upstreamRelay, _ := newRealtimeSocketPair(t)
			credentialLost := make(chan error, 1)
			providerLost := make(chan error, 1)
			config := defaultRealtimeRelayConfig()
			config.MaxSession = time.Second
			config.IdleTimeout = time.Second
			config.WriteTimeout = time.Second
			if test.configure != nil {
				config = test.configure(config)
			}
			outcome := make(chan realtimeRelayOutcome, 1)
			go func() {
				outcome <- runRealtimeRelay(context.Background(), downstreamRelay, upstreamRelay, "upstream-model", credentialLost, providerLost, config, nil, nil)
			}()
			if test.trigger != nil {
				test.trigger(t, downstreamClient, credentialLost, providerLost)
			}
			select {
			case got := <-outcome:
				if got.Normal != test.normal || got.ErrorType != test.wantType {
					t.Fatalf("outcome=%+v, want normal=%t error_type=%q", got, test.normal, test.wantType)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("realtime relay did not terminate")
			}
		})
	}
}

func TestRealtimeRelayPeriodicallyRevalidatesIdleSession(t *testing.T) {
	_, downstreamRelay := newRealtimeSocketPair(t)
	upstreamRelay, _ := newRealtimeSocketPair(t)
	config := defaultRealtimeRelayConfig()
	config.MaxSession = time.Second
	config.IdleTimeout = time.Second
	config.WriteTimeout = time.Second
	config.RevalidateEvery = 20 * time.Millisecond
	config.RevalidateTimeout = 100 * time.Millisecond
	outcome := make(chan realtimeRelayOutcome, 1)
	go func() {
		outcome <- runRealtimeRelay(context.Background(), downstreamRelay, upstreamRelay, "upstream-model", make(chan error), make(chan error), config, nil,
			func(context.Context) error { return errRealtimeCredentialGone })
	}()
	select {
	case got := <-outcome:
		if got.Normal || got.ErrorType != "credential_revoked" || !errors.Is(got.Err, errRealtimeCredentialGone) {
			t.Fatalf("outcome=%+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("realtime relay did not terminate after failed periodic revalidation")
	}
}

func TestRealtimeEventValidationRewritesAndDeduplicates(t *testing.T) {
	dedupe := newRealtimeEventDedupe(2)
	payload := []byte(`{"type":"session.update","event_id":"same","session":{"model":"public"}}`)
	forwarded, audioBytes, duplicate, err := rewriteRealtimeClientEvent(payload, "upstream", dedupe)
	if err != nil || duplicate || audioBytes != 0 {
		t.Fatalf("first rewrite duplicate=%t audio=%d err=%v", duplicate, audioBytes, err)
	}
	var event map[string]any
	if json.Unmarshal(forwarded, &event) != nil || nestedRealtimeModel(event, "session") != "upstream" {
		t.Fatalf("rewritten event=%s", forwarded)
	}
	if _, _, duplicate, err := rewriteRealtimeClientEvent(payload, "upstream", dedupe); err != nil || !duplicate {
		t.Fatalf("duplicate=%t err=%v", duplicate, err)
	}
	audio := []byte{1, 2, 3, 4, 5}
	audioPayload, _ := json.Marshal(map[string]any{"type": "input_audio_buffer.append", "event_id": "audio", "audio": base64.StdEncoding.EncodeToString(audio)})
	if _, audioBytes, duplicate, err := rewriteRealtimeClientEvent(audioPayload, "upstream", dedupe); err != nil || duplicate || audioBytes != int64(len(audio)) {
		t.Fatalf("audio bytes=%d duplicate=%t err=%v", audioBytes, duplicate, err)
	}
	invalidAudio := []byte(`{"type":"input_audio_buffer.append","event_id":"invalid","audio":"%%%"}`)
	if _, _, _, err := rewriteRealtimeClientEvent(invalidAudio, "upstream", dedupe); !errors.Is(err, errRealtimeInvalidEvent) {
		t.Fatalf("invalid audio error=%v", err)
	}
}

func TestRealtimeRelayConfigUsesAdmissionDuration(t *testing.T) {
	config := realtimeRelayConfigForRequest(gatewaycore.CanonicalRequest{AudioDurationMS: 1250})
	if config.MaxSession != 1250*time.Millisecond {
		t.Fatalf("max session=%s", config.MaxSession)
	}
	defaultConfig := realtimeRelayConfigForRequest(gatewaycore.CanonicalRequest{})
	if defaultConfig.MaxSession != realtimeMaxSession {
		t.Fatalf("default max session=%s", defaultConfig.MaxSession)
	}
}

func readRealtimeEvent(t *testing.T, conn *websocket.Conn, wantType string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	messageType, payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read %s: %v", wantType, err)
	}
	var event map[string]any
	if messageType != websocket.MessageText || json.Unmarshal(payload, &event) != nil || event["type"] != wantType {
		t.Fatalf("event type=%v payload=%s, want %s", event["type"], payload, wantType)
	}
	return event
}

func writeRealtimeEvent(t *testing.T, conn *websocket.Conn, event map[string]any) {
	t.Helper()
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatalf("write realtime event: %v", err)
	}
}

func waitForRealtimeOperation(t *testing.T, control *controlplane.Service, operationID string) controlplane.AIOperation {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		operation, found, err := control.AIOperation(context.Background(), operationID)
		if err != nil {
			t.Fatal(err)
		}
		if found && (operation.Status == controlplane.AIOperationStatusSucceeded || operation.Status == controlplane.AIOperationStatusFailed) {
			return operation
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("realtime operation %q did not reach a terminal state", operationID)
	return controlplane.AIOperation{}
}

func nestedRealtimeModel(event map[string]any, field string) string {
	nested, _ := event[field].(map[string]any)
	model, _ := nested["model"].(string)
	return model
}

func assertRealtimePermitsReleased(t *testing.T, fixture realtimeGatewayFixture) {
	t.Helper()
	request, err := gatewaycore.CanonicalizeRealtimeSession(http.Header{"Idempotency-Key": []string{"permit-check"}}, "public-realtime")
	if err != nil {
		t.Fatal(err)
	}
	_, canonicalAuth, err := fixture.control.AuthorizeCanonicalGatewayRequest(context.Background(), gatewaycore.CredentialEnvelope{BearerToken: fixture.key.Key}, request)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	var credentialPermit *controlplane.GatewayCredentialPermit
	var reason string
	var acquired bool
	for time.Now().Before(deadline) {
		credentialPermit, reason, acquired, err = fixture.control.TryAcquireGatewayCredentialPermit(context.Background(), canonicalAuth, 0)
		if err != nil {
			t.Fatalf("credential permit reason=%q acquired=%t err=%v", reason, acquired, err)
		}
		if acquired {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !acquired {
		t.Fatalf("credential permit remained unavailable: reason=%q", reason)
	}
	credentialPermit.Release()
	candidates, _, err := fixture.control.GatewayProviderCandidatesForModel(context.Background(), "public-realtime")
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates=%+v err=%v", candidates, err)
	}
	var providerPermit controlplane.ProviderAccountPermit
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		providerPermit, reason, acquired, err = fixture.control.TryAcquireProviderAccountPermitContext(context.Background(), candidates[0], 0, "provider-release-check")
		if err != nil {
			t.Fatalf("provider permit reason=%q acquired=%t err=%v", reason, acquired, err)
		}
		if acquired {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !acquired {
		t.Fatalf("provider permit remained unavailable: reason=%q", reason)
	}
	providerPermit.Release()
}

func dialRealtimeWithBearer(t *testing.T, serverURL, key, idempotencyKey string, expectSuccess bool) *websocket.Conn {
	t.Helper()
	target := strings.Replace(serverURL, "http://", "ws://", 1) + "/v1/realtime?model=public-realtime"
	conn, response, err := websocket.Dial(context.Background(), target, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization":   []string{"Bearer " + key},
		"Idempotency-Key": []string{idempotencyKey},
	}})
	if expectSuccess && err != nil {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		t.Fatalf("dial realtime status=%d err=%v", status, err)
	}
	return conn
}

func assertRealtimeDialStatus(t *testing.T, serverURL, key, idempotencyKey string, wantStatus int) {
	t.Helper()
	target := strings.Replace(serverURL, "http://", "ws://", 1) + "/v1/realtime?model=public-realtime"
	conn, response, err := websocket.Dial(context.Background(), target, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization":   []string{"Bearer " + key},
		"Idempotency-Key": []string{idempotencyKey},
	}})
	if conn != nil {
		conn.CloseNow()
		t.Fatalf("unexpected realtime connection for expected status %d", wantStatus)
	}
	if err == nil || response == nil || response.StatusCode != wantStatus {
		t.Fatalf("response=%v err=%v, want status=%d", response, err, wantStatus)
	}
}

func newRealtimeSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
	}))
	t.Cleanup(server.Close)
	target := strings.Replace(server.URL, "http://", "ws://", 1)
	client, _, err := websocket.Dial(context.Background(), target, nil)
	if err != nil {
		t.Fatal(err)
	}
	var peer *websocket.Conn
	select {
	case peer = <-accepted:
	case <-time.After(time.Second):
		client.CloseNow()
		t.Fatal("test WebSocket peer was not accepted")
	}
	t.Cleanup(func() {
		_ = client.CloseNow()
		_ = peer.CloseNow()
	})
	return client, peer
}
