package server

import (
	"bytes"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

const (
	usageNormalizationMissing     = gatewaycore.UsageNormalizationMissing
	usageNormalizationInvalid     = gatewaycore.UsageNormalizationInvalid
	usageNormalizationPartial     = gatewaycore.UsageNormalizationPartial
	usageNormalizationOpenAI      = gatewaycore.UsageNormalizationOpenAI
	usageNormalizationAnthropic   = gatewaycore.UsageNormalizationAnthropic
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
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		observation := gatewaycore.NormalizeUsage([]byte(payload))
		c.observation = gatewaycore.MergeNormalizedUsage(c.observation, observation)
	}
}

func (c *gatewaySSEUsageCollector) Observation() gatewayUsageObservation {
	if c.observation.UsageNormalizationStatus == "" {
		c.observation.UsageNormalizationStatus = usageNormalizationMissing
	}
	return c.observation
}
