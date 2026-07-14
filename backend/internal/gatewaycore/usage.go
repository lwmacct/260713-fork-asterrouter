package gatewaycore

import (
	"bytes"
	"encoding/json"
)

const (
	UsageNormalizationMissing   = "missing"
	UsageNormalizationInvalid   = "invalid"
	UsageNormalizationPartial   = "partial"
	UsageNormalizationOpenAI    = "normalized_openai"
	UsageNormalizationAnthropic = "normalized_anthropic"
	UsageNormalizationGeneric   = "normalized_generic"
)

type NormalizedUsage struct {
	InputTokens              int
	OutputTokens             int
	TotalInputTokens         *int
	UncachedInputTokens      *int
	CacheReadTokens          *int
	CacheWrite5mTokens       *int
	CacheWrite1hTokens       *int
	CacheFieldsPresent       bool
	UsageNormalizationStatus string
	inputTokensPresent       bool
	outputTokensPresent      bool
}

func NormalizeUsage(body []byte) NormalizedUsage {
	var root map[string]json.RawMessage
	if len(bytes.TrimSpace(body)) == 0 || json.Unmarshal(body, &root) != nil {
		return NormalizedUsage{UsageNormalizationStatus: UsageNormalizationInvalid}
	}
	usage, found := usageObjectFromRoot(root)
	if !found {
		return NormalizedUsage{UsageNormalizationStatus: UsageNormalizationMissing}
	}
	return normalizeUsageObject(usage)
}

func MergeNormalizedUsage(current, next NormalizedUsage) NormalizedUsage {
	if next.inputTokensPresent {
		current.InputTokens = next.InputTokens
		current.inputTokensPresent = true
	}
	if next.outputTokensPresent {
		current.OutputTokens = next.OutputTokens
		current.outputTokensPresent = true
	}
	if next.TotalInputTokens != nil {
		current.TotalInputTokens = next.TotalInputTokens
	}
	if next.UncachedInputTokens != nil {
		current.UncachedInputTokens = next.UncachedInputTokens
	}
	if next.CacheReadTokens != nil {
		current.CacheReadTokens = next.CacheReadTokens
	}
	if next.CacheWrite5mTokens != nil {
		current.CacheWrite5mTokens = next.CacheWrite5mTokens
	}
	if next.CacheWrite1hTokens != nil {
		current.CacheWrite1hTokens = next.CacheWrite1hTokens
	}
	current.CacheFieldsPresent = current.CacheFieldsPresent || next.CacheFieldsPresent
	if current.UsageNormalizationStatus == "" || current.UsageNormalizationStatus == UsageNormalizationMissing || current.UsageNormalizationStatus == UsageNormalizationInvalid || current.UsageNormalizationStatus == UsageNormalizationPartial || next.UsageNormalizationStatus == UsageNormalizationOpenAI || next.UsageNormalizationStatus == UsageNormalizationAnthropic {
		current.UsageNormalizationStatus = next.UsageNormalizationStatus
	}
	return current
}

func usageObjectFromRoot(root map[string]json.RawMessage) (map[string]json.RawMessage, bool) {
	if raw, ok := root["usage"]; ok {
		var usage map[string]json.RawMessage
		if json.Unmarshal(raw, &usage) == nil {
			return usage, true
		}
	}
	if raw, ok := root["message"]; ok {
		var message map[string]json.RawMessage
		if json.Unmarshal(raw, &message) == nil {
			if usageRaw, ok := message["usage"]; ok {
				var usage map[string]json.RawMessage
				if json.Unmarshal(usageRaw, &usage) == nil {
					return usage, true
				}
			}
		}
	}
	return nil, false
}

func normalizeUsageObject(usage map[string]json.RawMessage) NormalizedUsage {
	promptTokens, hasPromptTokens := usageInt(usage, "prompt_tokens")
	completionTokens, hasCompletionTokens := usageInt(usage, "completion_tokens")
	inputTokens, hasInputTokens := usageInt(usage, "input_tokens")
	outputTokens, hasOutputTokens := usageInt(usage, "output_tokens")
	_, hasPromptDetails := usage["prompt_tokens_details"]
	_, hasInputDetails := usage["input_tokens_details"]
	_, hasOutputDetails := usage["output_tokens_details"]
	cacheReadInputTokens, hasAnthropicCacheRead := usageInt(usage, "cache_read_input_tokens")
	cacheCreationInputTokens, hasAnthropicCacheWrite := usageInt(usage, "cache_creation_input_tokens")
	if hasPromptTokens || hasCompletionTokens || hasPromptDetails || hasInputDetails || hasOutputDetails {
		if !hasPromptTokens && hasInputTokens {
			promptTokens, hasPromptTokens = inputTokens, true
		}
		if !hasCompletionTokens && hasOutputTokens {
			completionTokens, hasCompletionTokens = outputTokens, true
		}
		return normalizeOpenAIUsage(usage, promptTokens, completionTokens, hasPromptTokens, hasCompletionTokens)
	}
	if hasAnthropicCacheRead || hasAnthropicCacheWrite {
		return normalizeAnthropicUsage(usage, inputTokens, outputTokens, cacheReadInputTokens, cacheCreationInputTokens, hasInputTokens, hasOutputTokens, hasAnthropicCacheRead, hasAnthropicCacheWrite)
	}
	if hasInputTokens || hasOutputTokens {
		return NormalizedUsage{
			InputTokens: inputTokens, OutputTokens: outputTokens, inputTokensPresent: hasInputTokens, outputTokensPresent: hasOutputTokens,
			TotalInputTokens: intPointerWhen(hasInputTokens, inputTokens), UncachedInputTokens: intPointerWhen(hasInputTokens, inputTokens),
			UsageNormalizationStatus: UsageNormalizationGeneric,
		}
	}
	return NormalizedUsage{UsageNormalizationStatus: UsageNormalizationPartial}
}

func normalizeOpenAIUsage(usage map[string]json.RawMessage, inputTokens, outputTokens int, hasInputTokens, hasOutputTokens bool) NormalizedUsage {
	cacheReadTokens, cacheReadPresent := usageInt(usage, "cached_tokens")
	for _, detailsField := range []string{"prompt_tokens_details", "input_tokens_details"} {
		if details, ok := usageObject(usage, detailsField); ok {
			if value, present := usageInt(details, "cached_tokens"); present {
				cacheReadTokens, cacheReadPresent = value, true
			}
		}
	}
	if value, present := usageInt(usage, "cache_read_tokens"); present {
		cacheReadTokens, cacheReadPresent = value, true
	}
	cacheWrite5mTokens, cacheWrite5mPresent := usageInt(usage, "cache_write_5m_tokens")
	cacheWrite1hTokens, cacheWrite1hPresent := usageInt(usage, "cache_write_1h_tokens")
	if value, present := usageInt(usage, "cache_write_tokens"); present && !cacheWrite5mPresent && !cacheWrite1hPresent {
		cacheWrite5mTokens, cacheWrite5mPresent = value, true
	}
	cacheFieldsPresent := cacheReadPresent || cacheWrite5mPresent || cacheWrite1hPresent
	uncachedInputTokens := inputTokens
	if cacheFieldsPresent {
		uncachedInputTokens = maxInt(0, inputTokens-cacheReadTokens-cacheWrite5mTokens-cacheWrite1hTokens)
	}
	return NormalizedUsage{
		InputTokens: inputTokens, OutputTokens: outputTokens, inputTokensPresent: hasInputTokens, outputTokensPresent: hasOutputTokens,
		TotalInputTokens: intPointerWhen(hasInputTokens, inputTokens), UncachedInputTokens: intPointerWhen(hasInputTokens, uncachedInputTokens),
		CacheReadTokens: intPointerWhen(cacheReadPresent, cacheReadTokens), CacheWrite5mTokens: intPointerWhen(cacheWrite5mPresent, cacheWrite5mTokens),
		CacheWrite1hTokens: intPointerWhen(cacheWrite1hPresent, cacheWrite1hTokens), CacheFieldsPresent: cacheFieldsPresent,
		UsageNormalizationStatus: UsageNormalizationOpenAI,
	}
}

func normalizeAnthropicUsage(usage map[string]json.RawMessage, uncachedInputTokens, outputTokens, cacheReadTokens, cacheCreationTokens int, hasInputTokens, hasOutputTokens, cacheReadPresent, cacheCreationPresent bool) NormalizedUsage {
	cacheWrite5mTokens, cacheWrite1hTokens := 0, 0
	cacheWrite5mPresent, cacheWrite1hPresent := false, false
	if creation, ok := usageObject(usage, "cache_creation"); ok {
		cacheWrite5mTokens, cacheWrite5mPresent = usageInt(creation, "ephemeral_5m_input_tokens")
		cacheWrite1hTokens, cacheWrite1hPresent = usageInt(creation, "ephemeral_1h_input_tokens")
	}
	if cacheCreationPresent && !cacheWrite5mPresent && !cacheWrite1hPresent {
		cacheWrite5mTokens, cacheWrite5mPresent = cacheCreationTokens, true
	}
	totalInputTokens := uncachedInputTokens + cacheReadTokens + cacheWrite5mTokens + cacheWrite1hTokens
	return NormalizedUsage{
		InputTokens: totalInputTokens, OutputTokens: outputTokens,
		inputTokensPresent: hasInputTokens || cacheReadPresent || cacheCreationPresent, outputTokensPresent: hasOutputTokens,
		TotalInputTokens:    intPointerWhen(hasInputTokens || cacheReadPresent || cacheCreationPresent, totalInputTokens),
		UncachedInputTokens: intPointerWhen(hasInputTokens, uncachedInputTokens), CacheReadTokens: intPointerWhen(cacheReadPresent, cacheReadTokens),
		CacheWrite5mTokens: intPointerWhen(cacheWrite5mPresent, cacheWrite5mTokens), CacheWrite1hTokens: intPointerWhen(cacheWrite1hPresent, cacheWrite1hTokens),
		CacheFieldsPresent:       cacheReadPresent || cacheCreationPresent || cacheWrite5mPresent || cacheWrite1hPresent,
		UsageNormalizationStatus: UsageNormalizationAnthropic,
	}
}

func usageInt(object map[string]json.RawMessage, key string) (int, bool) {
	raw, ok := object[key]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, false
	}
	var value int
	if json.Unmarshal(raw, &value) != nil || value < 0 {
		return 0, false
	}
	return value, true
}

func usageObject(object map[string]json.RawMessage, key string) (map[string]json.RawMessage, bool) {
	raw, ok := object[key]
	if !ok {
		return nil, false
	}
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil {
		return nil, false
	}
	return value, true
}

func intPointerWhen(present bool, value int) *int {
	if !present {
		return nil
	}
	copyValue := value
	return &copyValue
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
