package controlplane

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

func (s *Service) ListProcurementPrices(ctx context.Context) ([]ProcurementPrice, error) {
	return s.repo.ListProcurementPrices(ctx)
}

func (s *Service) CreateProcurementPrice(ctx context.Context, actor string, request ProcurementPriceRequest) (ProcurementPrice, error) {
	price, err := s.procurementPriceFromRequest(ctx, request, ProcurementPrice{})
	if err != nil {
		return ProcurementPrice{}, err
	}
	price.ID = "price_" + randomID(12)
	if err := s.repo.SaveProcurementPrice(ctx, price); err != nil {
		return ProcurementPrice{}, err
	}
	if err := s.audit(ctx, actor, "create", "procurement_price", price.ID, fmt.Sprintf("Created procurement price for account %s model %s", price.ProviderAccountID, price.UpstreamModel)); err != nil {
		return ProcurementPrice{}, err
	}
	return price, nil
}

func (s *Service) UpdateProcurementPrice(ctx context.Context, actor, id string, request ProcurementPriceRequest) (ProcurementPrice, error) {
	existing, err := s.procurementPriceByID(ctx, id)
	if err != nil {
		return ProcurementPrice{}, err
	}
	price, err := s.procurementPriceFromRequest(ctx, request, existing)
	if err != nil {
		return ProcurementPrice{}, err
	}
	price.ID = existing.ID
	price.CreatedAt = existing.CreatedAt
	if err := s.repo.SaveProcurementPrice(ctx, price); err != nil {
		return ProcurementPrice{}, err
	}
	if err := s.audit(ctx, actor, "update", "procurement_price", price.ID, fmt.Sprintf("Updated procurement price for account %s model %s", price.ProviderAccountID, price.UpstreamModel)); err != nil {
		return ProcurementPrice{}, err
	}
	return price, nil
}

func (s *Service) procurementPriceFromRequest(ctx context.Context, request ProcurementPriceRequest, existing ProcurementPrice) (ProcurementPrice, error) {
	request.ProviderID = strings.TrimSpace(request.ProviderID)
	request.ProviderAccountID = strings.TrimSpace(request.ProviderAccountID)
	request.UpstreamModel = strings.TrimSpace(request.UpstreamModel)
	request.Protocol = strings.TrimSpace(request.Protocol)
	if request.ProviderID == "" || request.ProviderAccountID == "" || request.UpstreamModel == "" || request.Protocol == "" {
		return ProcurementPrice{}, errors.New("provider_id, provider_account_id, upstream_model, and protocol are required")
	}
	account, err := s.providerAccountByID(ctx, request.ProviderAccountID)
	if err != nil {
		return ProcurementPrice{}, err
	}
	if account.ProviderID != request.ProviderID {
		return ProcurementPrice{}, errors.New("provider account does not belong to provider")
	}
	if _, err := s.providerByID(ctx, request.ProviderID); err != nil {
		return ProcurementPrice{}, err
	}
	prices := []int64{request.UncachedInputMicrosPer1MTokens, request.CacheReadMicrosPer1MTokens, request.CacheWrite5mMicrosPer1MTokens, request.CacheWrite1hMicrosPer1MTokens, request.OutputMicrosPer1MTokens, request.RequestMicros, request.ReferenceInputMicrosPer1MTokens, request.ReferenceOutputMicrosPer1MTokens}
	for _, value := range prices {
		if value < 0 {
			return ProcurementPrice{}, errors.New("procurement prices must be non-negative")
		}
	}
	if request.QuotedMultiplier < 0 || request.RechargeMultiplier < 0 {
		return ProcurementPrice{}, errors.New("multipliers must be non-negative")
	}
	status := strings.TrimSpace(request.Status)
	if status == "" {
		status = ProcurementPriceStatusActive
	}
	if !oneOf(status, ProcurementPriceStatusActive, ProcurementPriceStatusDisabled) {
		return ProcurementPrice{}, errors.New("invalid procurement price status")
	}
	confidence := strings.TrimSpace(request.Confidence)
	if confidence == "" {
		confidence = ProcurementCostConfidenceEstimated
	}
	if !validProcurementConfidence(confidence) {
		return ProcurementPrice{}, errors.New("invalid procurement cost confidence")
	}
	now := s.nowUTC()
	effectiveFrom := now
	if strings.TrimSpace(request.EffectiveFrom) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(request.EffectiveFrom))
		if err != nil {
			return ProcurementPrice{}, errors.New("effective_from must be RFC3339")
		}
		effectiveFrom = parsed.UTC()
	}
	expiresAt, err := parseOptionalDate(request.ExpiresAt)
	if err != nil {
		return ProcurementPrice{}, err
	}
	if expiresAt != nil && !expiresAt.After(effectiveFrom) {
		return ProcurementPrice{}, errors.New("expires_at must be after effective_from")
	}
	createdAt := existing.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	currency := strings.ToUpper(strings.TrimSpace(request.Currency))
	if currency == "" {
		currency = "USD"
	}
	if len(currency) != 3 {
		return ProcurementPrice{}, errors.New("currency must be a three-letter code")
	}
	sourceKind := strings.TrimSpace(request.SourceKind)
	if sourceKind == "" {
		sourceKind = "manual"
	}
	return ProcurementPrice{
		ProviderID: request.ProviderID, ProviderAccountID: request.ProviderAccountID,
		UpstreamModel: request.UpstreamModel, Protocol: request.Protocol, Currency: currency,
		UncachedInputMicrosPer1MTokens: request.UncachedInputMicrosPer1MTokens,
		CacheReadMicrosPer1MTokens:     request.CacheReadMicrosPer1MTokens,
		CacheWrite5mMicrosPer1MTokens:  request.CacheWrite5mMicrosPer1MTokens,
		CacheWrite1hMicrosPer1MTokens:  request.CacheWrite1hMicrosPer1MTokens,
		OutputMicrosPer1MTokens:        request.OutputMicrosPer1MTokens, RequestMicros: request.RequestMicros,
		ReferenceInputMicrosPer1MTokens:  request.ReferenceInputMicrosPer1MTokens,
		ReferenceOutputMicrosPer1MTokens: request.ReferenceOutputMicrosPer1MTokens,
		QuotedMultiplier:                 request.QuotedMultiplier, RechargeMultiplier: request.RechargeMultiplier,
		SourceKind: sourceKind, SourceReference: strings.TrimSpace(request.SourceReference), EvidenceHash: strings.TrimSpace(request.EvidenceHash),
		Confidence: confidence, Status: status, EffectiveFrom: effectiveFrom, ExpiresAt: expiresAt, CreatedAt: createdAt, UpdatedAt: now,
	}, nil
}

func (s *Service) procurementPriceByID(ctx context.Context, id string) (ProcurementPrice, error) {
	id = strings.TrimSpace(id)
	prices, err := s.repo.ListProcurementPrices(ctx)
	if err != nil {
		return ProcurementPrice{}, err
	}
	for _, price := range prices {
		if price.ID == id {
			return price, nil
		}
	}
	return ProcurementPrice{}, fmt.Errorf("procurement price %q not found", id)
}

func (s *Service) ListProviderBillingLines(ctx context.Context) ([]ProviderBillingLine, error) {
	return s.repo.ListProviderBillingLines(ctx)
}

func (s *Service) ImportProviderBillingLine(ctx context.Context, actor string, request ProviderBillingLineRequest) (ProviderBillingLine, error) {
	request.ProviderID = strings.TrimSpace(request.ProviderID)
	request.ProviderAccountID = strings.TrimSpace(request.ProviderAccountID)
	if request.ProviderID == "" || request.ProviderAccountID == "" || request.AmountMicros < 0 {
		return ProviderBillingLine{}, errors.New("provider_id, provider_account_id, and non-negative amount_micros are required")
	}
	account, err := s.providerAccountByID(ctx, request.ProviderAccountID)
	if err != nil {
		return ProviderBillingLine{}, err
	}
	if account.ProviderID != request.ProviderID {
		return ProviderBillingLine{}, errors.New("provider account does not belong to provider")
	}
	if strings.TrimSpace(request.ExternalLineID) == "" && strings.TrimSpace(request.ExternalRequestID) == "" && strings.TrimSpace(request.UsageRecordID) == "" {
		return ProviderBillingLine{}, errors.New("external_line_id, external_request_id, or usage_record_id is required")
	}
	confidence := strings.TrimSpace(request.Confidence)
	if confidence == "" {
		confidence = ProcurementCostConfidenceUnknown
	}
	if !validProcurementConfidence(confidence) {
		return ProviderBillingLine{}, errors.New("invalid billing confidence")
	}
	currency := strings.ToUpper(strings.TrimSpace(request.Currency))
	if currency == "" {
		currency = "USD"
	}
	if len(currency) != 3 {
		return ProviderBillingLine{}, errors.New("currency must be a three-letter code")
	}
	startedAt, err := parseOptionalDate(request.UsageStartedAt)
	if err != nil {
		return ProviderBillingLine{}, err
	}
	endedAt, err := parseOptionalDate(request.UsageEndedAt)
	if err != nil {
		return ProviderBillingLine{}, err
	}
	if startedAt != nil && endedAt != nil && endedAt.Before(*startedAt) {
		return ProviderBillingLine{}, errors.New("usage_ended_at must not be before usage_started_at")
	}
	now := s.nowUTC()
	line := ProviderBillingLine{
		ID: "bill_" + randomID(12), ProviderID: request.ProviderID, ProviderAccountID: request.ProviderAccountID,
		ExternalLineID: strings.TrimSpace(request.ExternalLineID), ExternalRequestID: strings.TrimSpace(request.ExternalRequestID),
		UsageRecordID: strings.TrimSpace(request.UsageRecordID), UpstreamModel: strings.TrimSpace(request.UpstreamModel), Currency: currency,
		AmountMicros: request.AmountMicros, InputCostMicros: nonNegativeInt64Pointer(request.InputCostMicros), OutputCostMicros: nonNegativeInt64Pointer(request.OutputCostMicros),
		CacheReadCostMicros: nonNegativeInt64Pointer(request.CacheReadCostMicros), CacheWriteCostMicros: nonNegativeInt64Pointer(request.CacheWriteCostMicros),
		SourceKind: strings.TrimSpace(request.SourceKind), Confidence: confidence, ReconciliationStatus: BillingReconciliationPending,
		RawPayloadHash: strings.TrimSpace(request.RawPayloadHash), UsageStartedAt: startedAt, UsageEndedAt: endedAt, CreatedAt: now, UpdatedAt: now,
	}
	if line.SourceKind == "" {
		line.SourceKind = "manual"
	}
	usageRecords, err := s.matchBillingUsage(ctx, line)
	if err != nil {
		return ProviderBillingLine{}, err
	}
	if len(usageRecords) == 1 {
		record := usageRecords[0]
		line.UsageRecordID = record.ID
		line.ReconciliationStatus = BillingReconciliationMatched
		if line.Confidence == ProcurementCostConfidenceUnknown {
			line.Confidence = ProcurementCostConfidenceExact
		}
		record.ProcurementCostMicros = &line.AmountMicros
		record.ProcurementCostCurrency = line.Currency
		record.ProcurementCostSource = "billing"
		record.ProcurementCostConfidence = line.Confidence
		record.ProviderBillingLineID = line.ID
		if err := s.repo.SaveProviderBillingLineAndReconcileUsage(ctx, line, record); err != nil {
			return ProviderBillingLine{}, err
		}
	} else {
		if len(usageRecords) > 1 {
			line.ReconciliationStatus = BillingReconciliationAmbiguous
		} else if line.SourceKind == "balance_snapshot" {
			line.ReconciliationStatus = BillingReconciliationUnallocated
			line.Confidence = ProcurementCostConfidenceUnallocated
		}
		if err := s.repo.SaveProviderBillingLine(ctx, line); err != nil {
			return ProviderBillingLine{}, err
		}
	}
	if err := s.audit(ctx, actor, "import", "provider_billing_line", line.ID, fmt.Sprintf("Imported provider billing line for account %s with reconciliation %s", line.ProviderAccountID, line.ReconciliationStatus)); err != nil {
		return ProviderBillingLine{}, err
	}
	return line, nil
}

func (s *Service) matchBillingUsage(ctx context.Context, line ProviderBillingLine) ([]UsageRecord, error) {
	query := UsageQuery{Limit: 2, AccountID: line.ProviderAccountID}
	if line.UsageRecordID != "" {
		query.ID = line.UsageRecordID
	} else if line.ExternalRequestID != "" {
		query.UpstreamRequestID = line.ExternalRequestID
	} else {
		return []UsageRecord{}, nil
	}
	records, err := s.repo.QueryUsageRecords(ctx, query)
	if err != nil {
		return nil, err
	}
	out := records[:0]
	for _, record := range records {
		if line.UpstreamModel == "" || line.UpstreamModel == record.UpstreamModel {
			out = append(out, record)
		}
	}
	return out, nil
}

func (s *Service) ListProviderCacheCapabilities(ctx context.Context) ([]ProviderCacheCapability, error) {
	return s.repo.ListProviderCacheCapabilities(ctx)
}

func (s *Service) UpsertProviderCacheCapability(ctx context.Context, actor string, request ProviderCacheCapabilityRequest) (ProviderCacheCapability, error) {
	request.ProviderAccountID = strings.TrimSpace(request.ProviderAccountID)
	request.UpstreamModel = strings.TrimSpace(request.UpstreamModel)
	request.Protocol = strings.TrimSpace(request.Protocol)
	if request.ProviderAccountID == "" || request.UpstreamModel == "" || request.Protocol == "" {
		return ProviderCacheCapability{}, errors.New("provider_account_id, upstream_model, and protocol are required")
	}
	if _, err := s.providerAccountByID(ctx, request.ProviderAccountID); err != nil {
		return ProviderCacheCapability{}, err
	}
	supportStatus := strings.TrimSpace(request.SupportStatus)
	if supportStatus == "" {
		supportStatus = CacheSupportUnknown
	}
	if !oneOf(supportStatus, CacheSupportUnknown, CacheSupportClaimed, CacheSupportAccepted, CacheSupportObserved, CacheSupportBilledVerified, CacheSupportDegraded, CacheSupportUnsupported) {
		return ProviderCacheCapability{}, errors.New("invalid cache support status")
	}
	poolAffinity := strings.TrimSpace(request.PoolAffinityGrade)
	if poolAffinity == "" {
		poolAffinity = PoolAffinityUnknown
	}
	if !oneOf(poolAffinity, PoolAffinityUnknown, PoolAffinityVerified, PoolAffinityProbable, PoolAffinityOpaque, PoolAffinityFragmented) {
		return ProviderCacheCapability{}, errors.New("invalid pool affinity grade")
	}
	transport := strings.TrimSpace(request.AffinityTransport)
	if transport == "" {
		transport = AffinityTransportNone
	}
	if !oneOf(transport, AffinityTransportNone, AffinityTransportHeader, AffinityTransportBody) {
		return ProviderCacheCapability{}, errors.New("invalid affinity transport")
	}
	now := s.nowUTC()
	id := "cachecap_" + prefix(hashAPIKey(request.ProviderAccountID+"\x00"+request.UpstreamModel+"\x00"+request.Protocol), 24)
	capability := ProviderCacheCapability{
		ID: id, ProviderAccountID: request.ProviderAccountID, UpstreamModel: request.UpstreamModel, Protocol: request.Protocol,
		SupportStatus: supportStatus, PoolAffinityGrade: poolAffinity, AffinityTransport: transport,
		AffinityField: strings.TrimSpace(request.AffinityField), CacheControlMode: strings.TrimSpace(request.CacheControlMode),
		UsageSchema: strings.TrimSpace(request.UsageSchema), CreatedAt: now, UpdatedAt: now,
	}
	existing, err := s.repo.ListProviderCacheCapabilities(ctx)
	if err != nil {
		return ProviderCacheCapability{}, err
	}
	for _, current := range existing {
		if current.ID == id {
			capability.MetricsCoverage = current.MetricsCoverage
			capability.EligibleRequestHitRate = current.EligibleRequestHitRate
			capability.CacheTokenHitRate = current.CacheTokenHitRate
			capability.CacheWriteReadRatio = current.CacheWriteReadRatio
			capability.AffinityConsistencyRate = current.AffinityConsistencyRate
			capability.BillingConsistencyRate = current.BillingConsistencyRate
			capability.ProductionSampleCount = current.ProductionSampleCount
			capability.ProbeSampleCount = current.ProbeSampleCount
			capability.DegradedReason = current.DegradedReason
			capability.LastObservedAt = current.LastObservedAt
			capability.LastVerifiedAt = current.LastVerifiedAt
			capability.CreatedAt = current.CreatedAt
			break
		}
	}
	if capability.CacheControlMode == "" {
		capability.CacheControlMode = "passthrough_if_present"
	}
	if capability.UsageSchema == "" {
		capability.UsageSchema = "auto"
	}
	if err := s.repo.SaveProviderCacheCapability(ctx, capability); err != nil {
		return ProviderCacheCapability{}, err
	}
	if err := s.audit(ctx, actor, "upsert", "provider_cache_capability", capability.ID, fmt.Sprintf("Updated cache capability for account %s model %s", capability.ProviderAccountID, capability.UpstreamModel)); err != nil {
		return ProviderCacheCapability{}, err
	}
	return capability, nil
}

func (s *Service) ListProviderCacheProbeRuns(ctx context.Context, limit int) ([]ProviderCacheProbeRun, error) {
	return s.repo.ListProviderCacheProbeRuns(ctx, limit)
}

func (s *Service) EffectivePricingPolicy(ctx context.Context) (EffectivePricingPolicy, error) {
	policy, found, err := s.repo.GetEffectivePricingPolicy(ctx)
	if err != nil {
		return EffectivePricingPolicy{}, err
	}
	if found {
		return policy, nil
	}
	return defaultEffectivePricingPolicy(s.nowUTC()), nil
}

func (s *Service) UpdateEffectivePricingPolicy(ctx context.Context, actor string, request EffectivePricingPolicyRequest) (EffectivePricingPolicy, error) {
	if request.MinCacheHitRateImprovement == 0 {
		request.MinCacheHitRateImprovement = 0.10
	}
	if request.MinAffinityImprovement == 0 {
		request.MinAffinityImprovement = 0.10
	}
	if !oneOf(request.Mode, EffectivePricingModeObserveOnly, EffectivePricingModeRecommend, EffectivePricingModeCanary, EffectivePricingModeBalanced, EffectivePricingModeCostFirst, EffectivePricingModeFixedRoute) {
		return EffectivePricingPolicy{}, errors.New("invalid effective pricing mode")
	}
	if request.WindowHours < 1 || request.WindowHours > 24*30 || request.MinSampleCount < 1 || request.CanaryPercent < 1 || request.CanaryPercent > 100 {
		return EffectivePricingPolicy{}, errors.New("invalid effective pricing window, sample count, or canary percent")
	}
	for _, ratio := range []float64{request.MinMetricsCoverage, request.MinBillingConsistency, request.MinCostImprovement, request.MinCacheHitRateImprovement, request.MinAffinityImprovement, request.MaxCacheTiebreakCostRegression, request.MaxErrorRateRegression, request.MaxP95LatencyRegression} {
		if ratio < 0 || ratio > 1 {
			return EffectivePricingPolicy{}, errors.New("effective pricing ratios must be between 0 and 1")
		}
	}
	if request.SupplierAffinityTTLSeconds < 1 || request.AccountAffinityTTLSeconds < 1 || request.ProbeDailyTokenBudget < 0 || request.ProbeDailyCostBudgetMicros < 0 || request.ProbeCooldownSeconds < 0 {
		return EffectivePricingPolicy{}, errors.New("invalid affinity or probe limits")
	}
	now := s.nowUTC()
	existing, found, err := s.repo.GetEffectivePricingPolicy(ctx)
	if err != nil {
		return EffectivePricingPolicy{}, err
	}
	createdAt := now
	if found {
		createdAt = existing.CreatedAt
	}
	policy := EffectivePricingPolicy{
		ID: defaultEffectivePricingPolicyID, Mode: request.Mode, WindowHours: request.WindowHours,
		MinSampleCount: request.MinSampleCount, MinMetricsCoverage: request.MinMetricsCoverage,
		MinBillingConsistency: request.MinBillingConsistency, MinCostImprovement: request.MinCostImprovement,
		MinCacheHitRateImprovement: request.MinCacheHitRateImprovement, MinAffinityImprovement: request.MinAffinityImprovement,
		MaxCacheTiebreakCostRegression: request.MaxCacheTiebreakCostRegression,
		MaxErrorRateRegression:         request.MaxErrorRateRegression, MaxP95LatencyRegression: request.MaxP95LatencyRegression,
		CanaryPercent: request.CanaryPercent, SupplierAffinityTTLSeconds: request.SupplierAffinityTTLSeconds,
		AccountAffinityTTLSeconds: request.AccountAffinityTTLSeconds, ProbeEnabled: request.ProbeEnabled,
		ProbeDailyTokenBudget: request.ProbeDailyTokenBudget, ProbeDailyCostBudgetMicros: request.ProbeDailyCostBudgetMicros,
		ProbeCooldownSeconds: request.ProbeCooldownSeconds, UpdatedBy: actor, CreatedAt: createdAt, UpdatedAt: now,
	}
	if err := s.repo.SaveEffectivePricingPolicy(ctx, policy); err != nil {
		return EffectivePricingPolicy{}, err
	}
	if err := s.audit(ctx, actor, "update", "effective_pricing_policy", policy.ID, fmt.Sprintf("Updated effective pricing policy mode to %s", policy.Mode)); err != nil {
		return EffectivePricingPolicy{}, err
	}
	return policy, nil
}

func defaultEffectivePricingPolicy(now time.Time) EffectivePricingPolicy {
	return EffectivePricingPolicy{
		ID: defaultEffectivePricingPolicyID, Mode: EffectivePricingModeObserveOnly, WindowHours: 24,
		MinSampleCount: 200, MinMetricsCoverage: 0.8, MinBillingConsistency: 0.95, MinCostImprovement: 0.08,
		MinCacheHitRateImprovement: 0.10, MinAffinityImprovement: 0.10, MaxCacheTiebreakCostRegression: 0.02,
		MaxErrorRateRegression: 0.005, MaxP95LatencyRegression: 0.2, CanaryPercent: 5,
		SupplierAffinityTTLSeconds: int(defaultSupplierAffinityTTL / time.Second), AccountAffinityTTLSeconds: int(defaultAccountAffinityTTL / time.Second),
		ProbeDailyTokenBudget: 100_000, ProbeDailyCostBudgetMicros: 10_000_000, ProbeCooldownSeconds: 3600,
		CreatedAt: now, UpdatedAt: now,
	}
}

func (s *Service) EffectivePricingReport(ctx context.Context, query EffectivePricingReportQuery) (EffectivePricingReport, error) {
	policy, err := s.EffectivePricingPolicy(ctx)
	if err != nil {
		return EffectivePricingReport{}, err
	}
	windowHours := query.WindowHours
	if windowHours <= 0 {
		windowHours = policy.WindowHours
	}
	if windowHours < 1 || windowHours > 24*30 {
		return EffectivePricingReport{}, errors.New("window_hours must be between 1 and 720")
	}
	windowEnd := s.nowUTC()
	windowStart := windowEnd.Add(-time.Duration(windowHours) * time.Hour)
	aggregates, err := s.repo.SummarizeEffectivePricingUsage(ctx, windowStart, windowEnd)
	if err != nil {
		return EffectivePricingReport{}, err
	}
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return EffectivePricingReport{}, err
	}
	accounts, err := s.repo.ListProviderAccounts(ctx)
	if err != nil {
		return EffectivePricingReport{}, err
	}
	prices, err := s.repo.ListProcurementPrices(ctx)
	if err != nil {
		return EffectivePricingReport{}, err
	}
	capabilities, err := s.repo.ListProviderCacheCapabilities(ctx)
	if err != nil {
		return EffectivePricingReport{}, err
	}
	billingLines, err := s.repo.ListProviderBillingLines(ctx)
	if err != nil {
		return EffectivePricingReport{}, err
	}
	providerNames := map[string]string{}
	for _, provider := range providers {
		providerNames[provider.ID] = provider.Name
	}
	accountNames := map[string]string{}
	for _, account := range accounts {
		accountNames[account.ID] = account.Name
	}
	capabilityByKey := map[string]ProviderCacheCapability{}
	for _, capability := range capabilities {
		capabilityByKey[effectivePricingKey(capability.ProviderAccountID, capability.UpstreamModel, capability.Protocol)] = capability
	}
	rows := make([]EffectivePricingReportRow, 0, len(aggregates))
	for _, aggregate := range aggregates {
		if model := strings.TrimSpace(query.Model); model != "" && aggregate.UpstreamModel != model {
			continue
		}
		if protocol := strings.TrimSpace(query.Protocol); protocol != "" && aggregate.Protocol != protocol {
			continue
		}
		price, hasPrice := activeProcurementPrice(prices, GatewayUsageInput{ProviderID: aggregate.ProviderID, ProviderAccountID: aggregate.ProviderAccountID, UpstreamModel: aggregate.UpstreamModel, Protocol: aggregate.Protocol}, windowEnd)
		costMicros, confidence := effectiveAggregateCost(aggregate, price, hasPrice)
		tokenCount := aggregate.TotalInputTokens + aggregate.OutputTokens
		effectiveCostPer1M := scaledPerMillion(costMicros, tokenCount)
		referenceCost := aggregateReferenceCost(aggregate, price, hasPrice)
		effectiveMultiplier := safeRatio(costMicros, referenceCost)
		metricsCoverage := safeRatio(aggregate.CacheMetricsRequestCount, aggregate.SuccessfulRequestCount)
		eligibleRequestHitRate := safeRatio(aggregate.CacheHitRequestCount, aggregate.CacheMetricsRequestCount)
		cacheHitRate := safeRatio(aggregate.CacheReadTokens, aggregate.TotalInputTokens)
		cacheWriteReadRatio := safeRatio(aggregate.CacheWrite5mTokens+aggregate.CacheWrite1hTokens, aggregate.CacheReadTokens)
		billingAmount, billingMatches := matchingBillingCost(billingLines, aggregate, windowStart, windowEnd)
		if billingMatches == aggregate.RequestCount && aggregate.RequestCount > 0 {
			confidence = ProcurementCostConfidenceExact
		} else if billingMatches > 0 {
			confidence = ProcurementCostConfidenceDerived
		}
		billingConsistency := safeRatio(billingMatches, aggregate.RequestCount)
		capability := capabilityByKey[effectivePricingKey(aggregate.ProviderAccountID, aggregate.UpstreamModel, aggregate.Protocol)]
		if capability.BillingConsistencyRate > 0 {
			billingConsistency = capability.BillingConsistencyRate
		}
		observedAt := windowEnd
		if aggregate.LastCacheObservedAt != nil {
			observedAt = *aggregate.LastCacheObservedAt
		}
		productionMetrics := ProviderCacheProductionMetrics{
			ID:                "cachecap_" + prefix(hashAPIKey(aggregate.ProviderAccountID+"\x00"+aggregate.UpstreamModel+"\x00"+aggregate.Protocol), 24),
			ProviderAccountID: aggregate.ProviderAccountID, UpstreamModel: aggregate.UpstreamModel, Protocol: aggregate.Protocol,
			MetricsCoverage: metricsCoverage, EligibleRequestHitRate: eligibleRequestHitRate,
			CacheTokenHitRate: cacheHitRate, CacheWriteReadRatio: cacheWriteReadRatio,
			BillingConsistencyRate: billingConsistency, ProductionSampleCount: aggregate.RequestCount,
			MetricsObserved:       aggregate.CacheMetricsRequestCount > 0,
			CacheActivityObserved: aggregate.CacheReadTokens+aggregate.CacheWrite5mTokens+aggregate.CacheWrite1hTokens > 0,
			ObservedAt:            observedAt,
		}
		capability = applyProviderCacheProductionMetrics(capability, productionMetrics)
		if err := s.repo.UpsertProviderCacheProductionMetrics(ctx, productionMetrics); err != nil {
			return EffectivePricingReport{}, err
		}
		row := EffectivePricingReportRow{
			ProviderID: aggregate.ProviderID, ProviderName: providerNames[aggregate.ProviderID],
			ProviderAccountID: aggregate.ProviderAccountID, ProviderAccountName: accountNames[aggregate.ProviderAccountID],
			UpstreamModel: aggregate.UpstreamModel, Protocol: aggregate.Protocol, Currency: price.Currency,
			QuotedMultiplier: price.QuotedMultiplier, BilledMultiplier: safeRatio(billingAmount, referenceCost),
			EffectiveMultiplier: effectiveMultiplier, EffectiveCostMicrosPer1M: effectiveCostPer1M,
			RequestCount: aggregate.RequestCount, ErrorRate: safeRatio(aggregate.ErrorCount, aggregate.RequestCount),
			MetricsCoverage: metricsCoverage, EligibleRequestHitRate: eligibleRequestHitRate,
			CacheTokenHitRate: cacheHitRate, CacheWriteReadRatio: cacheWriteReadRatio,
			BillingConsistencyRate: billingConsistency, AffinityConsistencyRate: capability.AffinityConsistencyRate,
			CacheSupportStatus: capability.SupportStatus, PoolAffinityGrade: capability.PoolAffinityGrade,
			CostConfidence: confidence, PriceID: price.ID,
		}
		if row.Currency == "" {
			row.Currency = "USD"
		}
		if row.CacheSupportStatus == "" {
			row.CacheSupportStatus = CacheSupportUnknown
		}
		if row.PoolAffinityGrade == "" {
			row.PoolAffinityGrade = PoolAffinityUnknown
		}
		row.Recommendation, row.ReasonCodes = effectivePricingRecommendation(row, policy)
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].EffectiveCostMicrosPer1M == 0 {
			return false
		}
		if rows[j].EffectiveCostMicrosPer1M == 0 {
			return true
		}
		return rows[i].EffectiveCostMicrosPer1M < rows[j].EffectiveCostMicrosPer1M
	})
	for index := range rows {
		if rows[index].Recommendation == "eligible" {
			if index == 0 {
				rows[index].Recommendation = "preferred"
			} else {
				rows[index].Recommendation = "higher_effective_cost"
				rows[index].ReasonCodes = append(rows[index].ReasonCodes, "higher_effective_cost")
			}
		}
	}
	decisions, err := s.repo.ListEffectivePricingDecisions(ctx)
	if err != nil {
		return EffectivePricingReport{}, err
	}
	filteredDecisions := decisions[:0]
	for _, decision := range decisions {
		if (query.Model == "" || decision.Model == query.Model) && (query.Protocol == "" || decision.Protocol == query.Protocol) {
			filteredDecisions = append(filteredDecisions, decision)
		}
	}
	return EffectivePricingReport{WindowStart: windowStart, WindowEnd: windowEnd, Policy: policy, Rows: rows, Decisions: filteredDecisions}, nil
}

func effectivePricingRecommendation(row EffectivePricingReportRow, policy EffectivePricingPolicy) (string, []string) {
	reasons := []string{}
	if row.RequestCount < policy.MinSampleCount {
		reasons = append(reasons, "insufficient_sample")
	}
	if row.MetricsCoverage < policy.MinMetricsCoverage {
		reasons = append(reasons, "cache_metrics_coverage_low")
	}
	if !oneOf(row.CostConfidence, ProcurementCostConfidenceExact, ProcurementCostConfidenceDerived, ProcurementCostConfidenceEstimated) {
		reasons = append(reasons, "cost_confidence_low")
	}
	if row.CacheSupportStatus == CacheSupportDegraded || row.PoolAffinityGrade == PoolAffinityFragmented {
		reasons = append(reasons, "cache_or_pool_degraded")
		return "reduce_weight", reasons
	}
	if len(reasons) > 0 {
		return "observe", reasons
	}
	return "eligible", reasons
}

func effectiveAggregateCost(aggregate EffectivePricingUsageAggregate, price ProcurementPrice, hasPrice bool) (int64, string) {
	if aggregate.ProcurementCostRecordCount == aggregate.RequestCount && aggregate.RequestCount > 0 {
		confidence := price.Confidence
		if confidence == "" {
			confidence = ProcurementCostConfidenceDerived
		}
		return aggregate.ProcurementCostMicros, confidence
	}
	if !hasPrice {
		return aggregate.ProcurementCostMicros, ProcurementCostConfidenceUnknown
	}
	cost := price.RequestMicros * aggregate.RequestCount
	components := []struct {
		tokens int64
		rate   int64
	}{
		{aggregate.UncachedInputTokens, price.UncachedInputMicrosPer1MTokens},
		{aggregate.CacheReadTokens, price.CacheReadMicrosPer1MTokens},
		{aggregate.CacheWrite5mTokens, price.CacheWrite5mMicrosPer1MTokens},
		{aggregate.CacheWrite1hTokens, price.CacheWrite1hMicrosPer1MTokens},
		{aggregate.OutputTokens, price.OutputMicrosPer1MTokens},
	}
	for _, component := range components {
		cost += scaledTokenCost(component.tokens, component.rate)
	}
	confidence := price.Confidence
	if confidence == "" {
		confidence = ProcurementCostConfidenceEstimated
	}
	return cost, confidence
}

func aggregateReferenceCost(aggregate EffectivePricingUsageAggregate, price ProcurementPrice, hasPrice bool) int64 {
	if !hasPrice {
		return 0
	}
	return scaledTokenCost(aggregate.TotalInputTokens, price.ReferenceInputMicrosPer1MTokens) + scaledTokenCost(aggregate.OutputTokens, price.ReferenceOutputMicrosPer1MTokens)
}

func scaledTokenCost(tokens, rate int64) int64 {
	if tokens <= 0 || rate <= 0 {
		return 0
	}
	return int64(math.Round(float64(tokens) * float64(rate) / 1_000_000))
}

func scaledPerMillion(costMicros, tokens int64) int64 {
	if costMicros <= 0 || tokens <= 0 {
		return 0
	}
	return int64(math.Round(float64(costMicros) * 1_000_000 / float64(tokens)))
}

func safeRatio(numerator, denominator int64) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func matchingBillingCost(lines []ProviderBillingLine, aggregate EffectivePricingUsageAggregate, from, to time.Time) (int64, int64) {
	var amount int64
	var matches int64
	for _, line := range lines {
		if line.ProviderAccountID != aggregate.ProviderAccountID || (line.UpstreamModel != "" && line.UpstreamModel != aggregate.UpstreamModel) || line.ReconciliationStatus != BillingReconciliationMatched {
			continue
		}
		observedAt := line.CreatedAt
		if line.UsageStartedAt != nil {
			observedAt = *line.UsageStartedAt
		}
		if observedAt.Before(from) || observedAt.After(to) {
			continue
		}
		amount += line.AmountMicros
		matches++
	}
	return amount, matches
}

func effectivePricingKey(accountID, model, protocol string) string {
	return accountID + "\x00" + model + "\x00" + protocol
}

func validProcurementConfidence(value string) bool {
	return oneOf(value, ProcurementCostConfidenceExact, ProcurementCostConfidenceDerived, ProcurementCostConfidenceEstimated, ProcurementCostConfidenceUnallocated, ProcurementCostConfidenceUnknown)
}

func applyProviderCacheProductionMetrics(capability ProviderCacheCapability, metrics ProviderCacheProductionMetrics) ProviderCacheCapability {
	if capability.ID == "" {
		capability = ProviderCacheCapability{
			ID: metrics.ID, ProviderAccountID: metrics.ProviderAccountID, UpstreamModel: metrics.UpstreamModel,
			Protocol: metrics.Protocol, SupportStatus: CacheSupportUnknown, PoolAffinityGrade: PoolAffinityUnknown,
			AffinityTransport: AffinityTransportNone, CacheControlMode: "passthrough_if_present", UsageSchema: "auto",
			CreatedAt: metrics.ObservedAt,
		}
	}
	capability.MetricsCoverage = metrics.MetricsCoverage
	capability.EligibleRequestHitRate = metrics.EligibleRequestHitRate
	capability.CacheTokenHitRate = metrics.CacheTokenHitRate
	capability.CacheWriteReadRatio = metrics.CacheWriteReadRatio
	capability.BillingConsistencyRate = metrics.BillingConsistencyRate
	capability.ProductionSampleCount = metrics.ProductionSampleCount
	capability.UpdatedAt = metrics.ObservedAt
	if metrics.MetricsObserved {
		capability.LastObservedAt = &metrics.ObservedAt
		if metrics.CacheActivityObserved && oneOf(capability.SupportStatus, CacheSupportUnknown, CacheSupportClaimed, CacheSupportAccepted) {
			capability.SupportStatus = CacheSupportObserved
		} else if !metrics.CacheActivityObserved && oneOf(capability.SupportStatus, CacheSupportUnknown, CacheSupportClaimed) {
			capability.SupportStatus = CacheSupportAccepted
		}
	}
	return capability
}
