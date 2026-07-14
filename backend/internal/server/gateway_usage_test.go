package server

import "testing"

func TestParseGatewayUsageNormalizesCacheSchemas(t *testing.T) {
	tests := []struct {
		name             string
		body             string
		wantInput        int
		wantOutput       int
		wantTotal        *int
		wantUncached     *int
		wantRead         *int
		wantWrite5m      *int
		wantWrite1h      *int
		wantCachePresent bool
		wantStatus       string
	}{
		{
			name:      "openai cached details",
			body:      `{"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":60}}}`,
			wantInput: 100, wantOutput: 20, wantTotal: testIntPointer(100), wantUncached: testIntPointer(40), wantRead: testIntPointer(60),
			wantCachePresent: true, wantStatus: usageNormalizationOpenAI,
		},
		{
			name:      "openai explicit zero cache",
			body:      `{"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":0}}}`,
			wantInput: 100, wantOutput: 20, wantTotal: testIntPointer(100), wantUncached: testIntPointer(100), wantRead: testIntPointer(0),
			wantCachePresent: true, wantStatus: usageNormalizationOpenAI,
		},
		{
			name:      "openai cache fields missing",
			body:      `{"usage":{"prompt_tokens":100,"completion_tokens":20}}`,
			wantInput: 100, wantOutput: 20, wantTotal: testIntPointer(100), wantUncached: testIntPointer(100),
			wantCachePresent: false, wantStatus: usageNormalizationOpenAI,
		},
		{
			name:      "anthropic cache creation details",
			body:      `{"usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":200,"cache_creation_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":20,"ephemeral_1h_input_tokens":10}}}`,
			wantInput: 330, wantOutput: 50, wantTotal: testIntPointer(330), wantUncached: testIntPointer(100), wantRead: testIntPointer(200), wantWrite5m: testIntPointer(20), wantWrite1h: testIntPointer(10),
			wantCachePresent: true, wantStatus: usageNormalizationAnthropic,
		},
		{
			name:      "nested anthropic message usage",
			body:      `{"message":{"usage":{"input_tokens":10,"output_tokens":0,"cache_read_input_tokens":90,"cache_creation_input_tokens":0}}}`,
			wantInput: 100, wantOutput: 0, wantTotal: testIntPointer(100), wantUncached: testIntPointer(10), wantRead: testIntPointer(90), wantWrite5m: testIntPointer(0),
			wantCachePresent: true, wantStatus: usageNormalizationAnthropic,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parseGatewayUsage([]byte(test.body))
			if got.InputTokens != test.wantInput || got.OutputTokens != test.wantOutput || got.CacheFieldsPresent != test.wantCachePresent || got.UsageNormalizationStatus != test.wantStatus {
				t.Fatalf("parseGatewayUsage() = %+v", got)
			}
			assertOptionalInt(t, "total", got.TotalInputTokens, test.wantTotal)
			assertOptionalInt(t, "uncached", got.UncachedInputTokens, test.wantUncached)
			assertOptionalInt(t, "read", got.CacheReadTokens, test.wantRead)
			assertOptionalInt(t, "write5m", got.CacheWrite5mTokens, test.wantWrite5m)
			assertOptionalInt(t, "write1h", got.CacheWrite1hTokens, test.wantWrite1h)
		})
	}
}

func TestGatewaySSEUsageCollectorMergesUsageEvents(t *testing.T) {
	collector := gatewaySSEUsageCollector{}
	collector.Write([]byte("data: {\"message\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":0,\"cache_read_input_tokens\":90,\"cache_creation_input_tokens\":0}}}\n\n"))
	collector.Write([]byte("data: {\"usage\":{\"output_tokens\":25}}\n\ndata: [DONE]\n\n"))
	got := collector.Observation()
	if got.InputTokens != 100 || got.OutputTokens != 25 || !got.CacheFieldsPresent || got.UsageNormalizationStatus != usageNormalizationAnthropic {
		t.Fatalf("merged observation = %+v", got)
	}
	assertOptionalInt(t, "read", got.CacheReadTokens, testIntPointer(90))
}

func TestParseGatewayUsageDistinguishesMissingAndInvalid(t *testing.T) {
	if got := parseGatewayUsage([]byte(`{"id":"no-usage"}`)); got.UsageNormalizationStatus != usageNormalizationMissing {
		t.Fatalf("missing usage status = %q", got.UsageNormalizationStatus)
	}
	if got := parseGatewayUsage([]byte(`{"usage":`)); got.UsageNormalizationStatus != usageNormalizationInvalid {
		t.Fatalf("invalid usage status = %q", got.UsageNormalizationStatus)
	}
}

func testIntPointer(value int) *int {
	return &value
}

func assertOptionalInt(t *testing.T, name string, got, want *int) {
	t.Helper()
	if got == nil || want == nil {
		if got != nil || want != nil {
			t.Fatalf("%s pointer = %v, want %v", name, got, want)
		}
		return
	}
	if *got != *want {
		t.Fatalf("%s = %d, want %d", name, *got, *want)
	}
}
