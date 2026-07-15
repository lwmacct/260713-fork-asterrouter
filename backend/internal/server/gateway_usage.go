package server

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

const (
	usageNormalizationMissing     = gatewaycore.UsageNormalizationMissing
	usageNormalizationInvalid     = gatewaycore.UsageNormalizationInvalid
	usageNormalizationPartial     = gatewaycore.UsageNormalizationPartial
	usageNormalizationOpenAI      = gatewaycore.UsageNormalizationOpenAI
	usageNormalizationAnthropic   = gatewaycore.UsageNormalizationAnthropic
	usageNormalizationGemini      = gatewaycore.UsageNormalizationGemini
	usageNormalizationGeneric     = gatewaycore.UsageNormalizationGeneric
	maxGatewaySSEPendingLineBytes = 1 << 20
)

type gatewayUsageObservation = gatewaycore.NormalizedUsage

func parseGatewayUsage(body []byte) gatewayUsageObservation {
	return gatewaycore.NormalizeUsage(body)
}

func mergeGatewayUsageObservation(current, next gatewayUsageObservation) gatewayUsageObservation {
	return gatewaycore.MergeNormalizedUsage(current, next)
}

type gatewaySSEUsageCollector struct {
	pending     []byte
	observation gatewayUsageObservation
	completed   bool
}

func (c *gatewaySSEUsageCollector) Write(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	c.pending = append(c.pending, chunk...)
	for {
		index := bytes.IndexByte(c.pending, '\n')
		if index < 0 {
			if len(c.pending) > maxGatewaySSEPendingLineBytes {
				c.pending = c.pending[:0]
			}
			return
		}
		line := strings.TrimSpace(string(c.pending[:index]))
		c.pending = c.pending[index+1:]
		if strings.HasPrefix(line, "event:") && gatewaySSETerminalEvent(strings.TrimSpace(strings.TrimPrefix(line, "event:"))) {
			c.completed = true
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			c.completed = true
			continue
		}
		if payload == "" {
			continue
		}
		if gatewaySSEPayloadTerminal(payload) {
			c.completed = true
		}
		observation := gatewaycore.NormalizeUsage([]byte(payload))
		c.observation = gatewaycore.MergeNormalizedUsage(c.observation, observation)
	}
}

func (c *gatewaySSEUsageCollector) Completed() bool {
	return c.completed
}

func gatewaySSETerminalEvent(event string) bool {
	switch strings.ToLower(strings.TrimSpace(event)) {
	case "message_stop", "response.completed", "response.complete", "done", "completion":
		return true
	default:
		return false
	}
}

func gatewaySSEPayloadTerminal(payload string) bool {
	var envelope struct {
		Choices []struct {
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(payload), &envelope) != nil {
		return false
	}
	if gatewaySSETerminalEvent(envelope.Type) {
		return true
	}
	for _, choice := range envelope.Choices {
		if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
			return true
		}
	}
	return false
}

func (c *gatewaySSEUsageCollector) Observation() gatewayUsageObservation {
	if c.observation.UsageNormalizationStatus == "" {
		c.observation.UsageNormalizationStatus = usageNormalizationMissing
	}
	return c.observation
}
