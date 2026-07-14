package gatewaycore

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestExtractCredentialUsesOneProtocolApprovedTransport(t *testing.T) {
	tests := []struct {
		name       string
		protocol   Protocol
		headers    http.Header
		wantToken  string
		wantSigned string
		transport  string
	}{
		{name: "bearer", protocol: ProtocolOpenAIChat, headers: http.Header{"Authorization": []string{"Bearer key-1"}}, wantToken: "key-1", transport: "authorization_bearer"},
		{name: "signed context", protocol: ProtocolOpenAIChat, headers: http.Header{"Authorization": []string{"Aster-Context signed-1"}}, wantSigned: "signed-1", transport: "authorization_aster_context"},
		{name: "anthropic", protocol: ProtocolAnthropicMessages, headers: http.Header{"X-Api-Key": []string{"key-2"}}, wantToken: "key-2", transport: "anthropic_x_api_key"},
		{name: "gemini", protocol: ProtocolGeminiGenerate, headers: http.Header{"X-Goog-Api-Key": []string{"key-3"}}, wantToken: "key-3", transport: "gemini_x_goog_api_key"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := &http.Request{Header: test.headers, URL: &url.URL{}}
			got, err := ExtractCredential(req, test.protocol)
			if err != nil {
				t.Fatalf("ExtractCredential(): %v", err)
			}
			if got.BearerToken != test.wantToken || got.SignedContext != test.wantSigned || got.Transport != test.transport {
				t.Fatalf("credential = %+v", got)
			}
		})
	}
}

func TestExtractCredentialFailsClosed(t *testing.T) {
	tests := []struct {
		name     string
		protocol Protocol
		rawURL   string
		headers  http.Header
		want     error
	}{
		{name: "missing", protocol: ProtocolOpenAIChat, rawURL: "https://gateway.test/v1/chat/completions", want: ErrCredentialMissing},
		{name: "query key", protocol: ProtocolOpenAIChat, rawURL: "https://gateway.test/v1/chat/completions?api_key=secret", want: ErrQueryCredentialRejected},
		{name: "multiple", protocol: ProtocolAnthropicMessages, rawURL: "https://gateway.test/v1/messages", headers: http.Header{"Authorization": []string{"Bearer one"}, "X-Api-Key": []string{"two"}}, want: ErrCredentialConflict},
		{name: "anthropic header on openai", protocol: ProtocolOpenAIChat, rawURL: "https://gateway.test/v1/chat/completions", headers: http.Header{"X-Api-Key": []string{"one"}}, want: ErrCredentialTransportRejected},
		{name: "unknown scheme", protocol: ProtocolOpenAIChat, rawURL: "https://gateway.test/v1/chat/completions", headers: http.Header{"Authorization": []string{"Basic abc"}}, want: ErrCredentialTransportRejected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, test.rawURL, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header = test.headers
			_, err = ExtractCredential(req, test.protocol)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCanonicalizeOpenAIChatProducesStableSafeEnvelope(t *testing.T) {
	raw := []byte(`{"model":"public-model","messages":[{"role":"user","content":"synthetic"}],"stream":true,"user":"subject-1"}`)
	header := http.Header{"X-Request-Id": []string{"request-1"}, "Idempotency-Key": []string{"idem-1"}}
	got, err := CanonicalizeOpenAIChat(raw, header)
	if err != nil {
		t.Fatalf("CanonicalizeOpenAIChat(): %v", err)
	}
	if got.ID != "op_request-1" || got.Protocol != ProtocolOpenAIChat || got.Lane != LaneDirect || got.Model != "public-model" || !got.Stream || got.MessageCount != 1 || got.IdempotencyKey != "idem-1" || got.StickyKey != "subject-1" {
		t.Fatalf("canonical request = %+v", got)
	}
	if len(got.Fingerprint) != 64 || !strings.Contains(string(got.Payload), "synthetic") {
		t.Fatalf("canonical payload or fingerprint is invalid: %+v", got)
	}
	raw[0] = '['
	if got.Payload[0] != '{' {
		t.Fatal("canonical request retained a mutable caller buffer")
	}
}

func TestCanonicalizeOpenAIChatRejectsInvalidInput(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte(`[]`), []byte(`{"messages":[]}`), []byte(`{"model":`)} {
		if _, err := CanonicalizeOpenAIChat(raw, http.Header{}); !errors.Is(err, ErrInvalidCanonicalRequest) {
			t.Fatalf("payload %q error = %v", raw, err)
		}
	}
}
