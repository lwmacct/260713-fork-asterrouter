package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestGatewayNormalAndStreamingSoak(t *testing.T) {
	if os.Getenv("ASTER_GATEWAY_SOAK") != "1" {
		t.Skip("ASTER_GATEWAY_SOAK=1 is not set")
	}
	duration := 30 * time.Minute
	if value := os.Getenv("ASTER_GATEWAY_SOAK_DURATION"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed < time.Second {
			t.Fatalf("ASTER_GATEWAY_SOAK_DURATION must be at least 1s: %q", value)
		}
		duration = parsed
	}
	interval := 100 * time.Millisecond
	if value := os.Getenv("ASTER_GATEWAY_SOAK_INTERVAL"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed < time.Millisecond {
			t.Fatalf("ASTER_GATEWAY_SOAK_INTERVAL must be at least 1ms: %q", value)
		}
		interval = parsed
	}

	upstream := testutil.NewFakeOpenAI(t)
	handler, control, key := gatewayContractRuntime(t, upstream)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	beforeGoroutines := runtime.NumGoroutine()

	started := time.Now()
	deadline := started.Add(duration)
	requests := 0
	for time.Now().Before(deadline) {
		stream := requests%2 == 1
		if stream {
			upstream.SetMode(testutil.OpenAIStream)
		} else {
			upstream.SetMode(testutil.OpenAINormal)
		}
		body := `{"model":"public-model","messages":[{"role":"user","content":"synthetic soak"}]`
		if stream {
			body += `,"stream":true`
		}
		body += `}`
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d stream=%t status=%d body=%s", requests, stream, rec.Code, rec.Body.String())
		}
		requests++
		time.Sleep(interval)
	}

	usage, err := control.UsageReport(context.Background(), requests+1)
	if err != nil {
		t.Fatalf("UsageReport(): %v", err)
	}
	traces, err := control.ListGatewayTraces(context.Background(), requests+1)
	if err != nil {
		t.Fatalf("ListGatewayTraces(): %v", err)
	}
	if len(usage.Recent) != requests || len(traces) != requests {
		t.Fatalf("evidence count requests=%d usage=%d traces=%d", requests, len(usage.Recent), len(traces))
	}

	runtime.GC()
	time.Sleep(250 * time.Millisecond)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	afterGoroutines := runtime.NumGoroutine()
	goroutineDelta := afterGoroutines - beforeGoroutines
	if goroutineDelta > 16 {
		t.Fatalf("goroutine growth exceeds threshold: before=%d after=%d delta=%d", beforeGoroutines, afterGoroutines, goroutineDelta)
	}

	heapDelta := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("soak_duration=%s requests=%s interval=%s goroutines_before=%d goroutines_after=%d goroutine_delta=%d heap_alloc_delta_bytes=%d",
		time.Since(started).Round(time.Millisecond), strconv.Itoa(requests), interval, beforeGoroutines, afterGoroutines, goroutineDelta, heapDelta)
}
