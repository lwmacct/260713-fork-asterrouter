package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

const (
	defaultCacheProbePrefixTokens = int64(2048)
	minCacheProbePrefixTokens     = int64(256)
	maxCacheProbePrefixTokens     = int64(32768)
	cacheProbeResponseBodyLimit   = int64(2 << 20)
	cacheProbeRunStaleAfter       = 2 * time.Minute
)

type cacheProbePhaseObservation struct {
	usage             gatewaycore.NormalizedUsage
	ttftMS            int64
	upstreamRequestID string
	costMicros        int64
}

func (s *Service) RunProviderCacheProbe(ctx context.Context, actor string, request CacheProbeRequest) (ProviderCacheProbeRun, error) {
	request.ProviderAccountID = strings.TrimSpace(request.ProviderAccountID)
	request.UpstreamModel = strings.TrimSpace(request.UpstreamModel)
	request.Protocol = strings.TrimSpace(request.Protocol)
	if request.PrefixTokens == 0 {
		request.PrefixTokens = defaultCacheProbePrefixTokens
	}
	if request.ProviderAccountID == "" || request.UpstreamModel == "" {
		return ProviderCacheProbeRun{}, errors.New("provider_account_id and upstream_model are required")
	}
	if !oneOf(request.Protocol, string(gatewaycore.ProtocolOpenAIChat), string(gatewaycore.ProtocolAnthropicMessages)) {
		return ProviderCacheProbeRun{}, errors.New("cache probes support openai_chat_completions or anthropic_messages")
	}
	if request.PrefixTokens < minCacheProbePrefixTokens || request.PrefixTokens > maxCacheProbePrefixTokens {
		return ProviderCacheProbeRun{}, fmt.Errorf("prefix_tokens must be between %d and %d", minCacheProbePrefixTokens, maxCacheProbePrefixTokens)
	}
	if request.MaxCostMicros <= 0 {
		return ProviderCacheProbeRun{}, errors.New("max_cost_micros confirmation is required")
	}

	account, err := s.providerAccountByID(ctx, request.ProviderAccountID)
	if err != nil {
		return ProviderCacheProbeRun{}, err
	}
	provider, err := s.providerByID(ctx, account.ProviderID)
	if err != nil {
		return ProviderCacheProbeRun{}, err
	}
	if provider.Status != ProviderStatusActive || account.Status != AccountStatusActive || !account.Schedulable || account.AuthType != "api_key" || !account.SecretConfigured || account.SecretCiphertext == "" {
		return ProviderCacheProbeRun{}, errors.New("provider account is not eligible for a cache probe")
	}
	if len(account.Models) > 0 && !contains(account.Models, request.UpstreamModel) {
		return ProviderCacheProbeRun{}, errors.New("upstream_model is not enabled for the provider account")
	}
	secret, err := decryptSecret(s.secretKey, account.SecretCiphertext)
	if err != nil || strings.TrimSpace(secret) == "" {
		return ProviderCacheProbeRun{}, errors.New("provider account secret cannot be used for a cache probe")
	}
	capability, err := s.cacheProbeCapability(ctx, request.ProviderAccountID, request.UpstreamModel, request.Protocol)
	if err != nil {
		return ProviderCacheProbeRun{}, err
	}

	now := s.nowUTC()
	seriesID := "cacheprobe_series_" + randomID(12)
	sessionHash := prefix(hashAPIKey(s.secretKey+"\x00"+seriesID), 32)
	stablePrefix := syntheticCacheProbePrefix(seriesID, request.PrefixTokens, false)
	run := ProviderCacheProbeRun{
		ID: "cacheprobe_" + randomID(12), ProviderID: provider.ID, ProviderAccountID: account.ID,
		UpstreamModel: request.UpstreamModel, Protocol: request.Protocol, ProbeSeriesID: seriesID,
		SessionHash: sessionHash, PrefixFingerprint: hashAPIKey(stablePrefix), PrefixTokens: request.PrefixTokens,
		Status: CacheProbeStatusRunning, StartedAt: now, FinishedAt: now,
	}
	policy, err := s.EffectivePricingPolicy(ctx)
	if err != nil {
		return ProviderCacheProbeRun{}, err
	}
	if !policy.ProbeEnabled {
		return s.skipProviderCacheProbe(ctx, actor, run, "probe_disabled")
	}

	preflight, found, err := s.estimateGatewayProcurementCost(ctx, GatewayUsageInput{
		ProviderID: provider.ID, ProviderAccountID: account.ID, UpstreamModel: request.UpstreamModel, Protocol: request.Protocol,
		InputTokens: int(request.PrefixTokens), OutputTokens: 1,
	}, now)
	if err != nil {
		return ProviderCacheProbeRun{}, err
	}
	if !found {
		return ProviderCacheProbeRun{}, errors.New("an active procurement price is required before running a cache probe")
	}
	if preflight.CostMicros > math.MaxInt64/3 {
		return ProviderCacheProbeRun{}, errors.New("cache probe cost estimate overflow")
	}
	run.EstimatedCostMicros = preflight.CostMicros * 3
	if run.EstimatedCostMicros > request.MaxCostMicros {
		return ProviderCacheProbeRun{}, fmt.Errorf("estimated probe cost %d exceeds confirmed max_cost_micros %d", run.EstimatedCostMicros, request.MaxCostMicros)
	}

	slotRelease, slotAvailable := s.TryAcquireProviderAccountSlot(account.ID, account.Concurrency)
	if !slotAvailable {
		return s.skipProviderCacheProbe(ctx, actor, run, "provider_account_capacity_unavailable")
	}
	defer slotRelease()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	reserved, reason, err := s.repo.ReserveProviderCacheProbeRun(ctx, run, CacheProbeReservationLimits{
		DayStart: dayStart, Now: now, Cooldown: time.Duration(policy.ProbeCooldownSeconds) * time.Second,
		StaleAfter: cacheProbeRunStaleAfter, DailyTokenBudget: policy.ProbeDailyTokenBudget,
		DailyCostBudgetMicros: policy.ProbeDailyCostBudgetMicros,
	})
	if err != nil {
		return ProviderCacheProbeRun{}, err
	}
	if !reserved {
		return s.skipProviderCacheProbe(ctx, actor, run, reason)
	}

	controlPrefix := syntheticCacheProbePrefix(seriesID, request.PrefixTokens, true)
	phases := []struct {
		name   string
		prefix string
		suffix string
	}{
		{name: "warm", prefix: stablePrefix, suffix: "warm cache seed"},
		{name: "reuse", prefix: stablePrefix, suffix: "reuse cache verification"},
		{name: "negative_control", prefix: controlPrefix, suffix: "negative control"},
	}
	actualCost := int64(0)
	completed := make([]cacheProbePhaseObservation, 0, len(phases))
	for _, phase := range phases {
		observation, phaseErr := s.runCacheProbePhase(ctx, provider, account, capability, request, secret, sessionHash, phase.name, phase.prefix, phase.suffix)
		if phaseErr != nil {
			run.Status = CacheProbeStatusFailed
			run.FailureReason = phase.name + ": " + phaseErr.Error()
			run.FinishedAt = s.nowUTC()
			if saveErr := s.repo.SaveProviderCacheProbeRun(ctx, run); saveErr != nil {
				return ProviderCacheProbeRun{}, saveErr
			}
			_ = s.audit(ctx, actor, "run", "provider_cache_probe", run.ID, fmt.Sprintf("Cache probe failed for account %s: %s", account.ID, run.FailureReason))
			return run, nil
		}
		completed = append(completed, observation)
		actualCost += observation.costMicros
		applyCacheProbePhaseObservation(&run, phase.name, observation)
		if actualCost > request.MaxCostMicros {
			run.Status = CacheProbeStatusFailed
			run.FailureReason = "confirmed_cost_ceiling_exceeded"
			run.FinishedAt = s.nowUTC()
			if err := s.repo.SaveProviderCacheProbeRun(ctx, run); err != nil {
				return ProviderCacheProbeRun{}, err
			}
			return run, nil
		}
	}
	if actualCost > 0 {
		run.EstimatedCostMicros = actualCost
	}
	run.CacheFieldsPresent = completed[0].usage.CacheFieldsPresent || completed[1].usage.CacheFieldsPresent || completed[2].usage.CacheFieldsPresent
	run.Status, run.FailureReason = classifyCacheProbe(completed[0], completed[1], completed[2])
	run.FinishedAt = s.nowUTC()
	if err := s.repo.SaveProviderCacheProbeRun(ctx, run); err != nil {
		return ProviderCacheProbeRun{}, err
	}
	if err := s.updateProviderCacheCapabilityFromProbe(ctx, capability, run); err != nil {
		return ProviderCacheProbeRun{}, err
	}
	if err := s.audit(ctx, actor, "run", "provider_cache_probe", run.ID, fmt.Sprintf("Cache probe %s for account %s model %s", run.Status, account.ID, request.UpstreamModel)); err != nil {
		return ProviderCacheProbeRun{}, err
	}
	return run, nil
}

func (s *Service) runCacheProbePhase(ctx context.Context, provider ProviderConnection, account ProviderAccount, capability ProviderCacheCapability, request CacheProbeRequest, secret, sessionHash, phase, prefixText, suffix string) (cacheProbePhaseObservation, error) {
	permit, reason, ok := s.TryAcquireProviderAccountPermit(GatewayProvider{
		AccountID: account.ID, RPMLimit: account.RPMLimit, TPMLimit: account.TPMLimit, CircuitState: account.CircuitState,
	}, int(request.PrefixTokens)+1)
	if !ok {
		return cacheProbePhaseObservation{}, errors.New(reason)
	}
	defer permit.Release()
	payload, err := cacheProbePayload(request.Protocol, request.UpstreamModel, prefixText, suffix, sessionHash, capability)
	if err != nil {
		return cacheProbePhaseObservation{}, err
	}
	endpoint := strings.TrimRight(provider.BaseURL, "/")
	if request.Protocol == string(gatewaycore.ProtocolAnthropicMessages) {
		endpoint += "/messages"
	} else {
		endpoint += "/chat/completions"
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return cacheProbePhaseObservation{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	if request.Protocol == string(gatewaycore.ProtocolAnthropicMessages) {
		httpRequest.Header.Set("X-Api-Key", secret)
		httpRequest.Header.Set("Anthropic-Version", "2023-06-01")
	} else {
		httpRequest.Header.Set("Authorization", "Bearer "+secret)
	}
	if capability.AffinityTransport == AffinityTransportHeader {
		if strings.TrimSpace(capability.AffinityField) == "" {
			return cacheProbePhaseObservation{}, errors.New("cache affinity header field is not configured")
		}
		httpRequest.Header.Set(capability.AffinityField, sessionHash)
	}
	startedAt := time.Now()
	client := s.providerCacheProbeHTTPClient
	if client == nil {
		client = &http.Client{Timeout: providerProbeTimeout}
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return cacheProbePhaseObservation{}, err
	}
	defer response.Body.Close()
	body, ttftMS, err := readCacheProbeResponse(response.Body, startedAt)
	if err != nil {
		return cacheProbePhaseObservation{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return cacheProbePhaseObservation{}, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	usage := gatewaycore.NormalizeUsage(body)
	estimate, _, err := s.estimateGatewayProcurementCost(ctx, GatewayUsageInput{
		ProviderID: provider.ID, ProviderAccountID: account.ID, UpstreamModel: request.UpstreamModel, Protocol: request.Protocol,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, TotalInputTokens: usage.TotalInputTokens,
		UncachedInputTokens: usage.UncachedInputTokens, CacheReadTokens: usage.CacheReadTokens,
		CacheWrite5mTokens: usage.CacheWrite5mTokens, CacheWrite1hTokens: usage.CacheWrite1hTokens,
		CacheFieldsPresent: usage.CacheFieldsPresent, UsageNormalizationStatus: usage.UsageNormalizationStatus,
	}, s.nowUTC())
	if err != nil {
		return cacheProbePhaseObservation{}, err
	}
	return cacheProbePhaseObservation{
		usage: usage, ttftMS: ttftMS, upstreamRequestID: cacheProbeUpstreamRequestID(response.Header), costMicros: estimate.CostMicros,
	}, nil
}

func cacheProbePayload(protocol, model, prefixText, suffix, sessionHash string, capability ProviderCacheCapability) ([]byte, error) {
	var payload map[string]any
	if protocol == string(gatewaycore.ProtocolAnthropicMessages) {
		payload = map[string]any{
			"model": model, "max_tokens": 1,
			"system":   []map[string]any{{"type": "text", "text": prefixText, "cache_control": map[string]any{"type": "ephemeral"}}},
			"messages": []map[string]any{{"role": "user", "content": suffix}},
		}
	} else {
		payload = map[string]any{
			"model": model, "max_tokens": 1, "temperature": 0,
			"messages": []map[string]any{{"role": "system", "content": prefixText}, {"role": "user", "content": suffix}},
		}
		if capability.CacheControlMode == "prompt_cache_key" {
			payload["prompt_cache_key"] = sessionHash
		}
	}
	if capability.AffinityTransport == AffinityTransportBody {
		if strings.TrimSpace(capability.AffinityField) == "" {
			return nil, errors.New("cache affinity body field is not configured")
		}
		payload[capability.AffinityField] = sessionHash
	}
	return json.Marshal(payload)
}

func readCacheProbeResponse(reader io.Reader, startedAt time.Time) ([]byte, int64, error) {
	limited := io.LimitReader(reader, cacheProbeResponseBodyLimit+1)
	buffer := bytes.Buffer{}
	chunk := make([]byte, 32*1024)
	ttftMS := int64(0)
	observedFirstByte := false
	for {
		n, err := limited.Read(chunk)
		if n > 0 {
			if !observedFirstByte {
				ttftMS = time.Since(startedAt).Milliseconds()
				observedFirstByte = true
			}
			_, _ = buffer.Write(chunk[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ttftMS, err
		}
	}
	if int64(buffer.Len()) > cacheProbeResponseBodyLimit {
		return nil, ttftMS, errors.New("provider cache probe response is too large")
	}
	return buffer.Bytes(), ttftMS, nil
}

func applyCacheProbePhaseObservation(run *ProviderCacheProbeRun, phase string, observation cacheProbePhaseObservation) {
	readTokens := int64(valueOr(observation.usage.CacheReadTokens, 0))
	writeTokens := int64(valueOr(observation.usage.CacheWrite5mTokens, 0) + valueOr(observation.usage.CacheWrite1hTokens, 0))
	switch phase {
	case "warm":
		run.WarmCacheReadTokens, run.WarmCacheWriteTokens, run.WarmTTFTMS = readTokens, writeTokens, observation.ttftMS
		run.WarmUpstreamRequestID = observation.upstreamRequestID
	case "reuse":
		run.ReuseCacheReadTokens, run.ReuseCacheWriteTokens, run.ReuseTTFTMS = readTokens, writeTokens, observation.ttftMS
		run.ReuseUpstreamRequestID = observation.upstreamRequestID
	case "negative_control":
		run.ControlCacheReadTokens, run.ControlCacheWriteTokens, run.ControlTTFTMS = readTokens, writeTokens, observation.ttftMS
		run.ControlUpstreamRequestID = observation.upstreamRequestID
	}
}

func classifyCacheProbe(warm, reuse, control cacheProbePhaseObservation) (string, string) {
	if !warm.usage.CacheFieldsPresent || !reuse.usage.CacheFieldsPresent || !control.usage.CacheFieldsPresent {
		return CacheProbeStatusFailed, "cache_usage_fields_missing"
	}
	warmRead := valueOr(warm.usage.CacheReadTokens, 0)
	reuseRead := valueOr(reuse.usage.CacheReadTokens, 0)
	controlRead := valueOr(control.usage.CacheReadTokens, 0)
	if reuseRead <= 0 || reuseRead <= warmRead {
		return CacheProbeStatusFailed, "cache_reuse_not_observed"
	}
	if controlRead >= reuseRead {
		return CacheProbeStatusFailed, "negative_control_not_distinct"
	}
	return CacheProbeStatusSucceeded, ""
}

func (s *Service) cacheProbeCapability(ctx context.Context, accountID, model, protocol string) (ProviderCacheCapability, error) {
	capabilities, err := s.repo.ListProviderCacheCapabilities(ctx)
	if err != nil {
		return ProviderCacheCapability{}, err
	}
	for _, capability := range capabilities {
		if capability.ProviderAccountID == accountID && capability.UpstreamModel == model && capability.Protocol == protocol {
			return capability, nil
		}
	}
	now := s.nowUTC()
	return ProviderCacheCapability{
		ID:                "cachecap_" + prefix(hashAPIKey(accountID+"\x00"+model+"\x00"+protocol), 24),
		ProviderAccountID: accountID, UpstreamModel: model, Protocol: protocol,
		SupportStatus: CacheSupportUnknown, PoolAffinityGrade: PoolAffinityUnknown,
		AffinityTransport: AffinityTransportNone, CacheControlMode: "passthrough_if_present", UsageSchema: "auto",
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *Service) updateProviderCacheCapabilityFromProbe(ctx context.Context, capability ProviderCacheCapability, run ProviderCacheProbeRun) error {
	runs, err := s.repo.ListProviderCacheProbeRuns(ctx, 500)
	if err != nil {
		return err
	}
	var completed, succeeded int64
	consecutiveSuccesses, consecutiveFailures := 0, 0
	countingSuccesses, countingFailures := true, true
	for _, current := range runs {
		if current.ProviderAccountID != run.ProviderAccountID || current.UpstreamModel != run.UpstreamModel || current.Protocol != run.Protocol {
			continue
		}
		isSuccess := current.Status == CacheProbeStatusSucceeded
		isCapabilityFailure := current.Status == CacheProbeStatusFailed && cacheProbeFailureIsCapabilityEvidence(current.FailureReason)
		if !isSuccess && !isCapabilityFailure {
			continue
		}
		completed++
		if isSuccess {
			succeeded++
		}
		if countingSuccesses && isSuccess {
			consecutiveSuccesses++
		} else {
			countingSuccesses = false
		}
		if countingFailures && isCapabilityFailure {
			consecutiveFailures++
		} else {
			countingFailures = false
		}
	}
	now := s.nowUTC()
	capability.ProbeSampleCount = completed
	capability.AffinityConsistencyRate = safeRatio(succeeded, completed)
	capability.UpdatedAt = now
	if run.CacheFieldsPresent {
		capability.LastObservedAt = &now
	}
	if run.Status == CacheProbeStatusSucceeded {
		if capability.SupportStatus != CacheSupportBilledVerified {
			capability.SupportStatus = CacheSupportObserved
		}
		capability.PoolAffinityGrade = PoolAffinityProbable
		if consecutiveSuccesses >= 3 {
			capability.PoolAffinityGrade = PoolAffinityVerified
		}
		capability.DegradedReason = ""
		capability.LastVerifiedAt = &now
	} else if consecutiveFailures >= 3 {
		capability.DegradedReason = run.FailureReason
		if run.FailureReason == "cache_usage_fields_missing" {
			if oneOf(capability.SupportStatus, CacheSupportObserved, CacheSupportBilledVerified, CacheSupportDegraded) {
				capability.SupportStatus = CacheSupportDegraded
			} else {
				capability.SupportStatus = CacheSupportUnsupported
			}
			capability.PoolAffinityGrade = PoolAffinityOpaque
		} else {
			capability.SupportStatus = CacheSupportDegraded
			capability.PoolAffinityGrade = PoolAffinityFragmented
		}
	} else if run.CacheFieldsPresent && capability.SupportStatus == CacheSupportUnknown {
		capability.SupportStatus = CacheSupportAccepted
	}
	if capability.UsageSchema == "" || capability.UsageSchema == "auto" {
		capability.UsageSchema = probeUsageSchema(run.Protocol)
	}
	return s.repo.SaveProviderCacheCapability(ctx, capability)
}

func (s *Service) skipProviderCacheProbe(ctx context.Context, actor string, run ProviderCacheProbeRun, reason string) (ProviderCacheProbeRun, error) {
	run.Status = CacheProbeStatusSkipped
	run.FailureReason = strings.TrimSpace(reason)
	if run.FailureReason == "" {
		run.FailureReason = "probe_not_reserved"
	}
	run.FinishedAt = s.nowUTC()
	if err := s.repo.SaveProviderCacheProbeRun(ctx, run); err != nil {
		return ProviderCacheProbeRun{}, err
	}
	if err := s.audit(ctx, actor, "skip", "provider_cache_probe", run.ID, fmt.Sprintf("Skipped cache probe for account %s: %s", run.ProviderAccountID, run.FailureReason)); err != nil {
		return ProviderCacheProbeRun{}, err
	}
	return run, nil
}

func syntheticCacheProbePrefix(seriesID string, prefixTokens int64, control bool) string {
	marker := "ASTERROUTER SYNTHETIC CACHE PROBE " + seriesID + " STABLE PREFIX. "
	if control {
		marker = "ASTERROUTER SYNTHETIC NEGATIVE CONTROL " + seriesID + ". "
	}
	targetBytes := int(prefixTokens * 4)
	if len(marker) >= targetBytes {
		return marker[:targetBytes]
	}
	unit := "synthetic context contains no customer data; cache reuse measurement only. "
	return (marker + strings.Repeat(unit, (targetBytes-len(marker))/len(unit)+1))[:targetBytes]
}

func cacheProbeUpstreamRequestID(header http.Header) string {
	for _, key := range []string{"X-Request-ID", "Request-ID", "Anthropic-Request-ID", "X-Request-Id"} {
		if value := strings.TrimSpace(header.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func cacheProbeFailureIsCapabilityEvidence(reason string) bool {
	return oneOf(reason, "cache_usage_fields_missing", "cache_reuse_not_observed", "negative_control_not_distinct")
}

func probeUsageSchema(protocol string) string {
	if protocol == string(gatewaycore.ProtocolAnthropicMessages) {
		return gatewaycore.UsageNormalizationAnthropic
	}
	return gatewaycore.UsageNormalizationOpenAI
}
