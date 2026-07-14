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
	return s.repo.ListEffectivePricingDecisions(ctx)
}

func (s *Service) EvaluateEffectivePricingDecision(ctx context.Context, actor string, request EffectivePricingDecisionEvaluationRequest) (EffectivePricingDecision, error) {
	request.Model = strings.TrimSpace(request.Model)
	request.UpstreamModel = strings.TrimSpace(request.UpstreamModel)
	request.Protocol = strings.TrimSpace(request.Protocol)
	request.CurrentProviderAccountID = strings.TrimSpace(request.CurrentProviderAccountID)
	request.CandidateProviderAccountID = strings.TrimSpace(request.CandidateProviderAccountID)
	if request.Model == "" || request.Protocol == "" || request.CurrentProviderAccountID == "" || request.CandidateProviderAccountID == "" || request.CurrentProviderAccountID == request.CandidateProviderAccountID {
		return EffectivePricingDecision{}, errors.New("model, protocol, and distinct current/candidate provider accounts are required")
	}
	report, err := s.EffectivePricingReport(ctx, EffectivePricingReportQuery{Model: request.UpstreamModel, Protocol: request.Protocol})
	if err != nil {
		return EffectivePricingDecision{}, err
	}
	current, currentFound := effectivePricingReportRowByAccount(report.Rows, request.CurrentProviderAccountID)
	candidate, candidateFound := effectivePricingReportRowByAccount(report.Rows, request.CandidateProviderAccountID)
	if !currentFound || !candidateFound {
		return EffectivePricingDecision{}, errors.New("current and candidate accounts must both have comparable usage")
	}
	if current.EffectiveCostMicrosPer1M <= 0 || candidate.EffectiveCostMicrosPer1M <= 0 {
		return EffectivePricingDecision{}, errors.New("current and candidate accounts require effective cost evidence")
	}
	improvement := float64(current.EffectiveCostMicrosPer1M-candidate.EffectiveCostMicrosPer1M) / float64(current.EffectiveCostMicrosPer1M)
	blockingReasons := effectivePricingDecisionBlockingReasons(candidate.ReasonCodes)
	decisionReasons := []string{}
	status := EffectivePricingDecisionHold
	costThresholdMet := improvement >= report.Policy.MinCostImprovement
	cacheHitImprovement := candidate.CacheTokenHitRate - current.CacheTokenHitRate
	affinityImprovement := candidate.AffinityConsistencyRate - current.AffinityConsistencyRate
	cacheEvidenceImproved := oneOf(candidate.CacheSupportStatus, CacheSupportObserved, CacheSupportBilledVerified) && cacheHitImprovement >= report.Policy.MinCacheHitRateImprovement
	affinityEvidenceImproved := oneOf(candidate.PoolAffinityGrade, PoolAffinityProbable, PoolAffinityVerified) && affinityImprovement >= report.Policy.MinAffinityImprovement
	cacheTiebreakerMet := !costThresholdMet && improvement >= -report.Policy.MaxCacheTiebreakCostRegression && candidate.MetricsCoverage >= report.Policy.MinMetricsCoverage && (cacheEvidenceImproved || affinityEvidenceImproved)
	if cacheTiebreakerMet {
		decisionReasons = append(decisionReasons, "cache_quality_tiebreaker")
	} else if !costThresholdMet {
		blockingReasons = append(blockingReasons, "cost_improvement_below_threshold")
		if !cacheEvidenceImproved && !affinityEvidenceImproved {
			blockingReasons = append(blockingReasons, "cache_quality_improvement_below_threshold")
		}
	}
	if candidate.BillingConsistencyRate < report.Policy.MinBillingConsistency {
		blockingReasons = append(blockingReasons, "billing_consistency_low")
	}
	if !oneOf(candidate.CostConfidence, ProcurementCostConfidenceExact, ProcurementCostConfidenceDerived) {
		blockingReasons = append(blockingReasons, "automatic_switch_confidence_low")
	}
	if len(blockingReasons) == 0 {
		status = EffectivePricingDecisionRecommended
	}
	reasons := append(decisionReasons, blockingReasons...)
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
		ID: "epd_" + randomID(12), Model: request.Model, Protocol: request.Protocol,
		CurrentProviderAccountID: request.CurrentProviderAccountID, CandidateProviderAccountID: request.CandidateProviderAccountID,
		CurrentSnapshotID: currentSnapshot.ID, CandidateSnapshotID: candidateSnapshot.ID,
		CurrentCostMicrosPer1M: current.EffectiveCostMicrosPer1M, CandidateCostMicrosPer1M: candidate.EffectiveCostMicrosPer1M,
		CostImprovement: improvement, Status: status, ReasonCodes: cleanStringList(reasons), CanaryPercent: report.Policy.CanaryPercent,
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

func (s *Service) ActOnEffectivePricingDecision(ctx context.Context, actor, id string, request EffectivePricingDecisionActionRequest) (EffectivePricingDecision, error) {
	decision, err := s.effectivePricingDecisionByID(ctx, id)
	if err != nil {
		return EffectivePricingDecision{}, err
	}
	action := strings.TrimSpace(request.Action)
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
	if err := s.repo.SaveEffectivePricingDecision(ctx, decision); err != nil {
		return EffectivePricingDecision{}, err
	}
	if err := s.audit(ctx, actor, action, "effective_pricing_decision", decision.ID, fmt.Sprintf("Effective pricing decision changed to %s", decision.Status)); err != nil {
		return EffectivePricingDecision{}, err
	}
	return decision, nil
}

func (s *Service) OrderGatewayCandidatesByEffectivePricing(ctx context.Context, model, protocol, requestFingerprint string, candidates []GatewayProvider) []GatewayProvider {
	if len(candidates) < 2 {
		return candidates
	}
	decisions, err := s.repo.ListEffectivePricingDecisions(ctx)
	if err != nil {
		return candidates
	}
	for _, decision := range decisions {
		if decision.Model != model || decision.Protocol != protocol || !oneOf(decision.Status, EffectivePricingDecisionCanary, EffectivePricingDecisionActive) {
			continue
		}
		if decision.Status == EffectivePricingDecisionCanary && !inEffectivePricingCanary(s.secretKey, decision.ID, requestFingerprint, decision.CanaryPercent) {
			continue
		}
		for index, candidate := range candidates {
			if candidate.AccountID != decision.CandidateProviderAccountID {
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

func effectivePricingReportRowByAccount(rows []EffectivePricingReportRow, accountID string) (EffectivePricingReportRow, bool) {
	for _, row := range rows {
		if row.ProviderAccountID == accountID {
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
	decisions, err := s.repo.ListEffectivePricingDecisions(ctx)
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

func inEffectivePricingCanary(secret, decisionID, fingerprint string, percent int) bool {
	if percent <= 0 || strings.TrimSpace(fingerprint) == "" {
		return false
	}
	if percent >= 100 {
		return true
	}
	service := &Service{secretKey: secret}
	key := service.gatewayAffinityScopeKey("canary", GatewayAffinityInput{CredentialID: decisionID, StickyKey: fingerprint})
	value, err := strconv.ParseUint(key[len(key)-2:], 16, 8)
	return err == nil && int(value)%100 < percent
}
