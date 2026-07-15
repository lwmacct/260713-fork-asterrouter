package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func (s *Service) ListEffectivePricingDecisions(ctx context.Context) ([]EffectivePricingDecision, error) {
	decisions, err := s.repo.ListEffectivePricingDecisions(ctx)
	if err != nil {
		return nil, err
	}
	snapshots, err := s.repo.ListEffectivePriceSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	snapshotByID := make(map[string]EffectivePriceSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		snapshotByID[snapshot.ID] = snapshot
	}
	for index := range decisions {
		if snapshot, ok := snapshotByID[decisions[index].CandidateSnapshotID]; ok {
			decisions[index].UpstreamModel = snapshot.UpstreamModel
		}
	}
	return decisions, nil
}

func (s *Service) EvaluateEffectivePricingDecision(ctx context.Context, actor string, request EffectivePricingDecisionEvaluationRequest) (EffectivePricingDecision, error) {
	request.Model = strings.TrimSpace(request.Model)
	request.UpstreamModel = strings.TrimSpace(request.UpstreamModel)
	request.Protocol = strings.TrimSpace(request.Protocol)
	request.CurrentProviderAccountID = strings.TrimSpace(request.CurrentProviderAccountID)
	request.CandidateProviderAccountID = strings.TrimSpace(request.CandidateProviderAccountID)
	if request.Model == "" || request.UpstreamModel == "" || request.Protocol == "" || request.CurrentProviderAccountID == "" || request.CandidateProviderAccountID == "" || request.CurrentProviderAccountID == request.CandidateProviderAccountID {
		return EffectivePricingDecision{}, errors.New("model, upstream_model, protocol, and distinct current/candidate provider accounts are required")
	}
	report, err := s.EffectivePricingReport(ctx, EffectivePricingReportQuery{Model: request.UpstreamModel, Protocol: request.Protocol})
	if err != nil {
		return EffectivePricingDecision{}, err
	}
	current, currentFound := effectivePricingReportRow(report.Rows, request.CurrentProviderAccountID, request.UpstreamModel, request.Protocol)
	candidate, candidateFound := effectivePricingReportRow(report.Rows, request.CandidateProviderAccountID, request.UpstreamModel, request.Protocol)
	if !currentFound || !candidateFound {
		return EffectivePricingDecision{}, errors.New("current and candidate accounts must both have comparable usage")
	}
	if !current.CostAvailable || !candidate.CostAvailable || current.EffectiveCostMicrosPer1M <= 0 || candidate.EffectiveCostMicrosPer1M < 0 {
		return EffectivePricingDecision{}, errors.New("current and candidate accounts require effective cost evidence")
	}
	assessment := assessEffectivePricingCandidate(current, candidate, report.Policy)
	status := EffectivePricingDecisionHold
	if assessment.Eligible {
		status = EffectivePricingDecisionRecommended
	}
	now := s.nowUTC()
	currentSnapshot := effectivePriceSnapshotFromReportRow(current, report, now)
	candidateSnapshot := effectivePriceSnapshotFromReportRow(candidate, report, now)
	if err := s.repo.SaveEffectivePriceSnapshot(ctx, currentSnapshot); err != nil {
		return EffectivePricingDecision{}, err
	}
	if err := s.repo.SaveEffectivePriceSnapshot(ctx, candidateSnapshot); err != nil {
		return EffectivePricingDecision{}, err
	}
	decision := EffectivePricingDecision{
		ID: "epd_" + randomID(12), Model: request.Model, UpstreamModel: request.UpstreamModel, Protocol: request.Protocol,
		CurrentProviderAccountID: request.CurrentProviderAccountID, CandidateProviderAccountID: request.CandidateProviderAccountID,
		CurrentSnapshotID: currentSnapshot.ID, CandidateSnapshotID: candidateSnapshot.ID,
		CurrentCostMicrosPer1M: current.EffectiveCostMicrosPer1M, CandidateCostMicrosPer1M: candidate.EffectiveCostMicrosPer1M,
		CostImprovement: assessment.CostImprovement, Status: status, ReasonCodes: assessment.ReasonCodes, CanaryPercent: report.Policy.CanaryPercent,
		SampleCount: candidate.RequestCount, Confidence: candidate.CostConfidence, CreatedBy: actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.SaveEffectivePricingDecision(ctx, decision); err != nil {
		return EffectivePricingDecision{}, err
	}
	if err := s.audit(ctx, actor, "evaluate", "effective_pricing_decision", decision.ID, fmt.Sprintf("Evaluated provider switch from %s to %s with status %s", decision.CurrentProviderAccountID, decision.CandidateProviderAccountID, decision.Status)); err != nil {
		return EffectivePricingDecision{}, err
	}
	return decision, nil
}

type effectivePricingCandidateAssessment struct {
	CostImprovement float64
	ReasonCodes     []string
	Eligible        bool
}

func assessEffectivePricingCandidate(current, candidate EffectivePricingReportRow, policy EffectivePricingPolicy) effectivePricingCandidateAssessment {
	improvement := float64(current.EffectiveCostMicrosPer1M-candidate.EffectiveCostMicrosPer1M) / float64(current.EffectiveCostMicrosPer1M)
	blockingReasons := effectivePricingDecisionBlockingReasons(candidate.ReasonCodes)
	blockingReasons = append(blockingReasons, effectivePricingQualityRegressionReasons(current, candidate, policy)...)
	decisionReasons := []string{}
	costThresholdMet := improvement >= policy.MinCostImprovement
	cacheHitImprovement := candidate.CacheTokenHitRate - current.CacheTokenHitRate
	cacheSavingsImprovement := candidate.CacheSavingsRate - current.CacheSavingsRate
	affinityImprovement := candidate.AffinityConsistencyRate - current.AffinityConsistencyRate
	cacheEvidenceImproved := current.CacheEconomicsAvailable && candidate.CacheEconomicsAvailable && oneOf(candidate.CacheSupportStatus, CacheSupportObserved, CacheSupportBilledVerified) && cacheHitImprovement >= policy.MinCacheHitRateImprovement && cacheSavingsImprovement > 0
	affinityEvidenceImproved := oneOf(candidate.PoolAffinityGrade, PoolAffinityProbable, PoolAffinityVerified) && affinityImprovement >= policy.MinAffinityImprovement
	cacheTiebreakerMet := !costThresholdMet && improvement >= -policy.MaxCacheTiebreakCostRegression && candidate.MetricsCoverage >= policy.MinMetricsCoverage && (cacheEvidenceImproved || affinityEvidenceImproved)
	if !cacheTiebreakerMet && !costThresholdMet {
		blockingReasons = append(blockingReasons, "cost_improvement_below_threshold")
		if !cacheEvidenceImproved && !affinityEvidenceImproved {
			blockingReasons = append(blockingReasons, "cache_quality_improvement_below_threshold")
			if !current.CacheEconomicsAvailable || !candidate.CacheEconomicsAvailable {
				blockingReasons = append(blockingReasons, "cache_economics_evidence_missing")
			}
		}
	}
	if candidate.BillingConsistencyRate < policy.MinBillingConsistency {
		blockingReasons = append(blockingReasons, "billing_consistency_low")
	}
	if !oneOf(candidate.CostConfidence, ProcurementCostConfidenceExact, ProcurementCostConfidenceDerived) {
		blockingReasons = append(blockingReasons, "automatic_switch_confidence_low")
	}
	if len(blockingReasons) == 0 && cacheTiebreakerMet {
		decisionReasons = append(decisionReasons, "cache_quality_tiebreaker")
	}
	return effectivePricingCandidateAssessment{
		CostImprovement: improvement,
		ReasonCodes:     cleanStringList(append(decisionReasons, blockingReasons...)),
		Eligible:        len(blockingReasons) == 0,
	}
}

func effectivePricingDecisionBlockingReasons(reasons []string) []string {
	out := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		if reason == "higher_effective_cost" {
			continue
		}
		out = append(out, reason)
	}
	return out
}

func effectivePricingQualityRegressionReasons(current, candidate EffectivePricingReportRow, policy EffectivePricingPolicy) []string {
	reasons := []string{}
	if candidate.ErrorRate-current.ErrorRate > policy.MaxErrorRateRegression {
		reasons = append(reasons, "error_rate_regression_exceeded")
	}
	if policy.Mode == EffectivePricingModeCostFirst {
		return reasons
	}
	if current.P95LatencyMS <= 0 || candidate.P95LatencyMS <= 0 {
		return append(reasons, "p95_latency_evidence_missing")
	}
	regression := float64(candidate.P95LatencyMS-current.P95LatencyMS) / float64(current.P95LatencyMS)
	if regression > policy.MaxP95LatencyRegression {
		reasons = append(reasons, "p95_latency_regression_exceeded")
	}
	return reasons
}

func (s *Service) ActOnEffectivePricingDecision(ctx context.Context, actor, id string, request EffectivePricingDecisionActionRequest) (EffectivePricingDecision, error) {
	decision, err := s.effectivePricingDecisionByID(ctx, id)
	if err != nil {
		return EffectivePricingDecision{}, err
	}
	expectedStatus := decision.Status
	expectedUpdatedAt := decision.UpdatedAt
	action := strings.TrimSpace(request.Action)
	if oneOf(action, "approve_canary", "activate") {
		healthByAccount, healthErr := s.providerBillingRoutingHealthByAccount(ctx, s.nowUTC())
		if healthErr != nil {
			return EffectivePricingDecision{}, fmt.Errorf("provider billing health is unavailable: %w", healthErr)
		}
		if health, found := healthByAccount[decision.CandidateProviderAccountID]; found && !health.EconomicSwitchEligible {
			return EffectivePricingDecision{}, fmt.Errorf("candidate provider billing health does not allow economic switching: %s", strings.Join(health.ReasonCodes, ","))
		}
	}
	switch action {
	case "approve_canary":
		if decision.Status != EffectivePricingDecisionRecommended {
			return EffectivePricingDecision{}, errors.New("only recommended decisions can start canary")
		}
		if !oneOf(decision.Confidence, ProcurementCostConfidenceExact, ProcurementCostConfidenceDerived) {
			return EffectivePricingDecision{}, errors.New("canary requires exact or derived cost confidence")
		}
		if request.CanaryPercent > 0 {
			if request.CanaryPercent > 100 {
				return EffectivePricingDecision{}, errors.New("canary_percent must be between 1 and 100")
			}
			decision.CanaryPercent = request.CanaryPercent
		}
		decision.Status = EffectivePricingDecisionCanary
		now := s.nowUTC()
		decision.HealthyWindowCount = 0
		decision.DegradedWindowCount = 0
		decision.LastEvaluationID = ""
		decision.LastEvaluationVerdict = ""
		decision.LastEvaluationReasonCodes = nil
		decision.LastEvaluatedWindowEnd = &now
		decision.MonitoringStartedAt = &now
		decision.LastHealthyAt = nil
		decision.LastAutomaticAction = ""
	case "activate":
		if decision.Status != EffectivePricingDecisionCanary {
			return EffectivePricingDecision{}, errors.New("only canary decisions can become active")
		}
		decision.Status = EffectivePricingDecisionActive
	case "rollback":
		if !oneOf(decision.Status, EffectivePricingDecisionCanary, EffectivePricingDecisionActive, EffectivePricingDecisionDegraded) {
			return EffectivePricingDecision{}, errors.New("decision cannot be rolled back from its current status")
		}
		decision.Status = EffectivePricingDecisionRolledBack
	case "hold":
		decision.Status = EffectivePricingDecisionHold
	default:
		return EffectivePricingDecision{}, errors.New("invalid decision action")
	}
	decision.UpdatedAt = s.nowUTC()
	updated, err := s.repo.UpdateEffectivePricingDecision(ctx, decision, expectedStatus, expectedUpdatedAt)
	if err != nil {
		return EffectivePricingDecision{}, err
	}
	if !updated {
		return EffectivePricingDecision{}, errors.New("effective pricing decision changed; reload before applying the action")
	}
	if err := s.audit(ctx, actor, action, "effective_pricing_decision", decision.ID, fmt.Sprintf("Effective pricing decision changed to %s", decision.Status)); err != nil {
		return EffectivePricingDecision{}, err
	}
	return decision, nil
}

func (s *Service) OrderGatewayCandidatesByEffectivePricing(ctx context.Context, model, protocol, cohortKey string, candidates []GatewayProvider) []GatewayProvider {
	if len(candidates) < 2 {
		return candidates
	}
	decisions, err := s.repo.ListEffectivePricingDecisions(ctx)
	if err != nil {
		return candidates
	}
	healthByAccount, err := s.providerBillingRoutingHealthByAccount(ctx, s.nowUTC())
	if err != nil {
		return candidates
	}
	for _, decision := range decisions {
		if decision.Model != model || decision.Protocol != protocol || !oneOf(decision.Status, EffectivePricingDecisionCanary, EffectivePricingDecisionActive) {
			continue
		}
		if decision.Status == EffectivePricingDecisionCanary && !inEffectivePricingCanary(s.secretKey, decision.ID, cohortKey, decision.CanaryPercent) {
			continue
		}
		for index, candidate := range candidates {
			if candidate.AccountID != decision.CandidateProviderAccountID {
				continue
			}
			if health, found := healthByAccount[candidate.AccountID]; found && !health.EconomicSwitchEligible {
				continue
			}
			out := append([]GatewayProvider(nil), candidates...)
			selected := out[index]
			selected.SelectionReason = appendSelectionReason(selected.SelectionReason, "effective pricing "+decision.Status+" decision "+decision.ID)
			if index > 0 {
				copy(out[1:index+1], out[0:index])
			}
			out[0] = selected
			return out
		}
	}
	return candidates
}

func effectivePricingReportRow(rows []EffectivePricingReportRow, accountID, upstreamModel, protocol string) (EffectivePricingReportRow, bool) {
	for _, row := range rows {
		if row.ProviderAccountID == accountID && row.UpstreamModel == upstreamModel && row.Protocol == protocol {
			return row, true
		}
	}
	return EffectivePricingReportRow{}, false
}

func effectivePriceSnapshotFromReportRow(row EffectivePricingReportRow, report EffectivePricingReport, now time.Time) EffectivePriceSnapshot {
	return EffectivePriceSnapshot{
		ID: "eps_" + randomID(12), ProviderID: row.ProviderID, ProviderAccountID: row.ProviderAccountID,
		UpstreamModel: row.UpstreamModel, Protocol: row.Protocol, Currency: row.Currency,
		EffectiveCostMicrosPer1M: row.EffectiveCostMicrosPer1M, EffectiveMultiplier: row.EffectiveMultiplier,
		QuotedMultiplier: row.QuotedMultiplier, CacheTokenHitRate: row.CacheTokenHitRate,
		MetricsCoverage: row.MetricsCoverage, BillingConsistencyRate: row.BillingConsistencyRate,
		RequestCount: row.RequestCount, CostConfidence: row.CostConfidence, PriceID: row.PriceID,
		WindowStart: report.WindowStart, WindowEnd: report.WindowEnd, ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
}

func (s *Service) effectivePricingDecisionByID(ctx context.Context, id string) (EffectivePricingDecision, error) {
	id = strings.TrimSpace(id)
	decisions, err := s.ListEffectivePricingDecisions(ctx)
	if err != nil {
		return EffectivePricingDecision{}, err
	}
	for _, decision := range decisions {
		if decision.ID == id {
			return decision, nil
		}
	}
	return EffectivePricingDecision{}, fmt.Errorf("effective pricing decision %q not found", id)
}

func inEffectivePricingCanary(secret, decisionID, cohortKey string, percent int) bool {
	if percent <= 0 || strings.TrimSpace(cohortKey) == "" {
		return false
	}
	if percent >= 100 {
		return true
	}
	service := &Service{secretKey: secret}
	key := service.gatewayAffinityScopeKey(AffinityBindingAccount, GatewayAffinityInput{CredentialID: decisionID, StickyKey: cohortKey})
	value, err := strconv.ParseUint(key[len(key)-16:], 16, 64)
	return err == nil && value%10_000 < uint64(percent)*100
}
