package server

import "testing"

var benchmarkGatewayBody = []byte(`{"model":"public-model","messages":[{"role":"system","content":"synthetic system prompt"},{"role":"user","content":"synthetic user prompt"}],"max_completion_tokens":512}`)

func BenchmarkGatewayRewriteModel(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		body, err := rewriteGatewayModel(benchmarkGatewayBody, "upstream-model")
		if err != nil || len(body) == 0 {
			b.Fatalf("rewriteGatewayModel() body=%q err=%v", body, err)
		}
	}
}

func BenchmarkGatewayEstimateTokens(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if tokens := estimateGatewayRequestTokens(benchmarkGatewayBody); tokens <= 512 {
			b.Fatalf("estimateGatewayRequestTokens() = %d", tokens)
		}
	}
}

func BenchmarkGatewayParseUsage(b *testing.B) {
	body := []byte(`{"usage":{"prompt_tokens":123,"completion_tokens":456}}`)
	b.ReportAllocs()
	for b.Loop() {
		input, output := parseUpstreamUsage(body)
		if input != 123 || output != 456 {
			b.Fatalf("parseUpstreamUsage() = %d, %d", input, output)
		}
	}
}
