package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const defaultEffectivePricingMonitorTick = time.Minute

func (s *Service) ListEffectivePricingDecisionEvaluations(ctx context.Context, decisionID string, limit int) ([]EffectivePricingDecisionEvaluation, error) {
	decisionID = strings.TrimSpace(decisionID)
	if decisionID == "" {
		return nil, errors.New("effective pricing decision id is required")
	}
	return s.repo.ListEffectivePricingDecisionEvaluations(ctx, decisionID, limit)
}

// EvaluateEffectivePricingDecisionWindows evaluates at most one completed
// canonical window per active decision. The repository de-duplicates that
// window across instances and commits evidence with any transition atomically.
func (s *Service) EvaluateEffectivePricingDecisionWindows(ctx context.Context, actor string) ([]EffectivePricingDecisionEvaluation, error) {
	policy, err := s.EffectivePricingPolicy(ctx)
	if err != nil {
		return nil, err
	}
	interval := time.Duration(policy.EvaluationIntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = time.Hour
	}
	windowEnd := s.nowUTC().Truncate(interval)
	decisions, err := s.ListEffectivePricingDecisions(ctx)
	if err != nil {
		return nil, err
	}
	evaluations := []EffectivePricingDecisionEvaluation{}
	var joined error
	for _, decision := range decisions {
		if !oneOf(decision.Status, EffectivePricingDecisionCanary, EffectivePricingDecisionActive) {
			continue
		}
		evaluation, applied, evaluateErr := s.evaluateEffectivePricingDecisionWindow(ctx, actor, policy, decision, windowEnd)
		if evaluateErr != nil {
			joined = errors.Join(joined, fmt.Errorf("decision %s: %w", decision.ID, evaluateErr))
			continue
		}
		if applied {
			evaluations = append(evaluations, evaluation)
		}
	}
	return evaluations, joined
}

func (s *Service) evaluateEffectivePricingDecisionWindow(ctx context.Context, actor string, policy EffectivePricingPolicy, decision EffectivePricingDecision, windowEnd time.Time) (EffectivePricingDecisionEvaluation, bool, error) {
	monitoringStartedAt := decision.UpdatedAt
	if decision.MonitoringStartedAt != nil {
		monitoringStartedAt = *decision.MonitoringStartedAt
	}
	if !windowEnd.After(monitoringStartedAt) || decision.LastEvaluatedWindowEnd != nil && !windowEnd.After(*decision.LastEvaluatedWindowEnd) {
		return EffectivePricingDecisionEvaluation{}, false, nil
	}
	windowStart := windowEnd.Add(-time.Duration(policy.WindowHours) * time.Hour)
	if monitoringStartedAt.After(windowStart) {
		windowStart = monitoringStartedAt
	}
	report, err := s.effectivePricingReportForWindow(ctx, EffectivePricingReportQuery{
		Model: decision.UpstreamModel, Protocol: decision.Protocol, WindowHours: policy.WindowHours,
	}, policy, windowStart, windowEnd)
	if err != nil {
		return EffectivePricingDecisionEvaluation{}, false, err
	}
	current, currentFound := effectivePricingReportRow(report.Rows, decision.CurrentProviderAccountID, decision.UpstreamModel, decision.Protocol)
	candidate, candidateFound := effectivePricingReportRow(report.Rows, decision.CandidateProviderAccountID, decision.UpstreamModel, decision.Protocol)
	verdict := EffectivePricingEvaluationInconclusive
	reasons := []string{}
	costImprovement := 0.0
	if !currentFound {
		reasons = append(reasons, "current_usage_evidence_missing")
	}
	if !candidateFound {
		reasons = append(reasons, "candidate_usage_evidence_missing")
	}
	if currentFound && candidateFound {
		if !current.CostAvailable || !candidate.CostAvailable || current.EffectiveCostMicrosPer1M <= 0 || candidate.EffectiveCostMicrosPer1M < 0 {
			reasons = append(reasons, "effective_cost_evidence_missing")
		} else {
			assessment := assessEffectivePricingCandidate(current, candidate, policy)
			costImprovement = assessment.CostImprovement
			reasons = assessment.ReasonCodes
			if assessment.Eligible {
				verdict = EffectivePricingEvaluationHealthy
			} else if effectivePricingReasonsProveDegradation(reasons) {
				verdict = EffectivePricingEvaluationDegraded
			}
		}
	}
	reasons = cleanStringList(reasons)
	now := s.nowUTC()
	evaluation := EffectivePricingDecisionEvaluation{
		ID: "epw_" + randomID(12), DecisionID: decision.ID, WindowStart: windowStart, WindowEnd: windowEnd,
		Verdict: verdict, ReasonCodes: reasons, CurrentRequestCount: current.RequestCount,
		CandidateRequestCount: candidate.RequestCount, CurrentCostMicrosPer1M: current.EffectiveCostMicrosPer1M,
		CandidateCostMicrosPer1M: candidate.EffectiveCostMicrosPer1M, CostImprovement: costImprovement,
		CurrentCacheTokenHitRate: current.CacheTokenHitRate, CandidateCacheTokenHitRate: candidate.CacheTokenHitRate,
		CurrentCacheSavingsRate: current.CacheSavingsRate, CandidateCacheSavingsRate: candidate.CacheSavingsRate,
		CurrentAffinityConsistencyRate: current.AffinityConsistencyRate, CandidateAffinityConsistencyRate: candidate.AffinityConsistencyRate,
		CurrentErrorRate: current.ErrorRate, CandidateErrorRate: candidate.ErrorRate,
		CurrentP95LatencyMS: current.P95LatencyMS, CandidateP95LatencyMS: candidate.P95LatencyMS,
		CurrentMetricsCoverage: current.MetricsCoverage, CandidateMetricsCoverage: candidate.MetricsCoverage,
		CurrentBillingConsistencyRate: current.BillingConsistencyRate, CandidateBillingConsistencyRate: candidate.BillingConsistencyRate,
		CreatedAt: now,
	}
	updatedDecision := decision
	updatedDecision.LastEvaluationID = evaluation.ID
	updatedDecision.LastEvaluationVerdict = verdict
	updatedDecision.LastEvaluationReasonCodes = append([]string(nil), reasons...)
	updatedDecision.LastEvaluatedWindowEnd = &evaluation.WindowEnd
	if updatedDecision.MonitoringStartedAt == nil {
		startedAt := monitoringStartedAt
		updatedDecision.MonitoringStartedAt = &startedAt
	}
	if candidateFound {
		updatedDecision.CandidateCostMicrosPer1M = candidate.EffectiveCostMicrosPer1M
		updatedDecision.SampleCount = candidate.RequestCount
		updatedDecision.Confidence = candidate.CostConfidence
	}
	if currentFound {
		updatedDecision.CurrentCostMicrosPer1M = current.EffectiveCostMicrosPer1M
	}
	updatedDecision.CostImprovement = costImprovement
	switch verdict {
	case EffectivePricingEvaluationHealthy:
		updatedDecision.HealthyWindowCount = min(updatedDecision.HealthyWindowCount+1, max(policy.PromotionWindowCount, 1))
		updatedDecision.DegradedWindowCount = 0
		updatedDecision.LastHealthyAt = &now
	case EffectivePricingEvaluationDegraded:
		updatedDecision.DegradedWindowCount = min(updatedDecision.DegradedWindowCount+1, max(policy.DegradationWindowCount, 1))
		updatedDecision.HealthyWindowCount = 0
	default:
		updatedDecision.HealthyWindowCount = 0
		updatedDecision.DegradedWindowCount = 0
	}
	if automaticEffectivePricingActionsAllowed(policy) {
		switch {
		case decision.Status == EffectivePricingDecisionCanary && verdict == EffectivePricingEvaluationHealthy && updatedDecision.HealthyWindowCount >= policy.PromotionWindowCount:
			updatedDecision.Status = EffectivePricingDecisionActive
			updatedDecision.LastAutomaticAction = "activate"
			evaluation.AutomaticAction = "activate"
		case oneOf(decision.Status, EffectivePricingDecisionCanary, EffectivePricingDecisionActive) && verdict == EffectivePricingEvaluationDegraded && updatedDecision.DegradedWindowCount >= policy.DegradationWindowCount:
			updatedDecision.Status = EffectivePricingDecisionRolledBack
			updatedDecision.LastAutomaticAction = "rollback"
			evaluation.AutomaticAction = "rollback"
		}
	}
	var currentSnapshot, candidateSnapshot *EffectivePriceSnapshot
	if currentFound {
		snapshot := effectivePriceSnapshotFromReportRow(current, report, now)
		currentSnapshot = &snapshot
		evaluation.CurrentSnapshotID = snapshot.ID
		updatedDecision.CurrentSnapshotID = snapshot.ID
	}
	if candidateFound {
		snapshot := effectivePriceSnapshotFromReportRow(candidate, report, now)
		candidateSnapshot = &snapshot
		evaluation.CandidateSnapshotID = snapshot.ID
		updatedDecision.CandidateSnapshotID = snapshot.ID
	}
	updatedDecision.UpdatedAt = now
	var audit *AuditLog
	if evaluation.AutomaticAction != "" {
		event := s.newAuditLog(actor, "automatic_"+evaluation.AutomaticAction, "effective_pricing_decision", decision.ID,
			fmt.Sprintf("Automatic effective pricing %s after %d healthy and %d degraded windows", evaluation.AutomaticAction, updatedDecision.HealthyWindowCount, updatedDecision.DegradedWindowCount))
		audit = &event
	}
	applied, err := s.repo.CommitEffectivePricingDecisionEvaluation(ctx, EffectivePricingDecisionEvaluationCommit{
		Evaluation: evaluation, Decision: updatedDecision, ExpectedStatus: decision.Status,
		ExpectedUpdatedAt: decision.UpdatedAt, CurrentSnapshot: currentSnapshot,
		CandidateSnapshot: candidateSnapshot, Audit: audit,
	})
	return evaluation, applied, err
}

func effectivePricingReasonsProveDegradation(reasons []string) bool {
	for _, reason := range reasons {
		if !oneOf(reason,
			"insufficient_sample", "cache_metrics_coverage_low", "cost_confidence_low",
			"automatic_switch_confidence_low", "p95_latency_evidence_missing",
			"cache_economics_evidence_missing", "current_usage_evidence_missing",
			"candidate_usage_evidence_missing", "effective_cost_evidence_missing",
			ProviderBillingReasonSyncUnhealthy, ProviderBillingReasonEvidenceStale,
			ProviderBillingReasonEvidenceMissing, ProviderBillingReasonSourceObserveOnly,
			ProviderBillingReasonSourceDisabled,
		) {
			return true
		}
	}
	return false
}

func automaticEffectivePricingActionsAllowed(policy EffectivePricingPolicy) bool {
	return policy.AutomaticActionsEnabled && oneOf(policy.Mode,
		EffectivePricingModeCanary, EffectivePricingModeBalanced, EffectivePricingModeCostFirst)
}

func (s *Service) RunEffectivePricingDecisionMonitor(ctx context.Context, tick time.Duration, onError func(error)) {
	if tick <= 0 {
		tick = defaultEffectivePricingMonitorTick
	}
	evaluate := func() {
		if _, err := s.EvaluateEffectivePricingDecisionWindows(ctx, "system:effective-pricing-monitor"); err != nil && onError != nil {
			onError(err)
		}
	}
	evaluate()
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			evaluate()
		}
	}
}
