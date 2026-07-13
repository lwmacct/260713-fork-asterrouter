package testutil

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type OpenAIMode string

const (
	OpenAINormal        OpenAIMode = "normal"
	OpenAIStream        OpenAIMode = "stream"
	OpenAIHTTPError     OpenAIMode = "http_error"
	OpenAIMalformed     OpenAIMode = "malformed"
	OpenAIWaitForCancel OpenAIMode = "wait_for_cancel"
)

type OpenAIRequest struct {
	Method        string
	Path          string
	Authorization string
	Accept        string
	Body          []byte
	Model         string
}

type FakeOpenAI struct {
	server *httptest.Server

	mu         sync.RWMutex
	mode       OpenAIMode
	statusCode int
	requests   []OpenAIRequest
	requestCh  chan struct{}
}

func NewFakeOpenAI(t testing.TB) *FakeOpenAI {
	t.Helper()
	fake := &FakeOpenAI{
		mode:       OpenAINormal,
		statusCode: http.StatusTooManyRequests,
		requestCh:  make(chan struct{}, 32),
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *FakeOpenAI) BaseURL() string {
	return f.server.URL + "/v1"
}

func (f *FakeOpenAI) SetMode(mode OpenAIMode) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mode = mode
}

func (f *FakeOpenAI) SetHTTPError(statusCode int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mode = OpenAIHTTPError
	f.statusCode = statusCode
}

func (f *FakeOpenAI) Requests() []OpenAIRequest {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make([]OpenAIRequest, len(f.requests))
	copy(result, f.requests)
	return result
}

func (f *FakeOpenAI) WaitForRequest(t testing.TB) {
	t.Helper()
	select {
	case <-f.requestCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for fake OpenAI request")
	}
}

func (f *FakeOpenAI) serveHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var payload struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &payload)

	f.mu.Lock()
	f.requests = append(f.requests, OpenAIRequest{
		Method:        r.Method,
		Path:          r.URL.Path,
		Authorization: r.Header.Get("Authorization"),
		Accept:        r.Header.Get("Accept"),
		Body:          append([]byte(nil), body...),
		Model:         payload.Model,
	})
	mode := f.mode
	statusCode := f.statusCode
	f.mu.Unlock()
	select {
	case f.requestCh <- struct{}{}:
	default:
	}

	if r.URL.Path == "/v1/models" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"upstream-model","object":"model"}]}`)
		return
	}

	switch mode {
	case OpenAIStream:
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"id\":\"stream-1\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	case OpenAIHTTPError:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = io.WriteString(w, `{"error":{"type":"upstream_error","message":"synthetic failure"}}`)
	case OpenAIMalformed:
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":`)
	case OpenAIWaitForCancel:
		<-r.Context().Done()
	default:
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"completion-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":11}}`)
	}
}
