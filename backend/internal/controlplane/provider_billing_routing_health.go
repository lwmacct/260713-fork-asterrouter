package controlplane

import (
	"context"
	"strings"
	"time"
)

const (
	providerBillingRoutingFailureThreshold = 3
	providerBillingMinimumStaleAfter       = 6 * time.Hour
)

const (
	ProviderBillingReasonKeyInvalid            = "provider_billing_key_invalid"
	ProviderBillingReasonAuthRejected          = "provider_billing_auth_rejected"
	ProviderBillingReasonKeyQuotaExhausted     = "provider_billing_key_quota_exhausted"
	ProviderBillingReasonSubscriptionExhausted = "provider_billing_subscription_exhausted"
	ProviderBillingReasonSyncUnhealthy         = "provider_billing_sync_unhealthy"
	ProviderBillingReasonEvidenceStale         = "provider_billing_evidence_stale"
	ProviderBillingReasonEvidenceMissing       = "provider_billing_evidence_missing"
	ProviderBillingReasonSourceObserveOnly     = "provider_billing_source_observe_only"
	ProviderBillingReasonSourceDisabled        = "provider_billing_source_disabled"
)

func providerBillingRoutingHealth(source ProviderBillingSource, balance *ProviderBalanceSnapshotRecord, now time.Time) ProviderBillingRoutingHealth {
	now = now.UTC()
	staleAfter := time.Duration(source.SyncIntervalSeconds*2) * time.Second
	if staleAfter < providerBillingMinimumStaleAfter {
		staleAfter = providerBillingMinimumStaleAfter
	}
	health := ProviderBillingRoutingHealth{
		SourceStatus: source.Status, EconomicSwitchEligible: false, EvaluatedAt: now,
		EvidenceStaleAfterSeconds: int(staleAfter / time.Second),
	}
	switch source.Status {
	case ProviderBillingSourceObserveOnly:
		health.Status = ProviderBillingRoutingHealthObserveOnly
		health.ReasonCodes = []string{ProviderBillingReasonSourceObserveOnly}
		return health
	case ProviderBillingSourceDisabled:
		health.Status = ProviderBillingRoutingHealthDisabled
		health.ReasonCodes = []string{ProviderBillingReasonSourceDisabled}
		return health
	}

	health.Status = ProviderBillingRoutingHealthHealthy
	health.EconomicSwitchEligible = true
	health.EvidenceObservedAt = cloneTimePointer(source.LastSuccessAt)
	if balance != nil && (health.EvidenceObservedAt == nil || balance.ObservedAt.After(*health.EvidenceObservedAt)) {
		health.EvidenceObservedAt = timePointer(balance.ObservedAt.UTC())
	}
	if providerBillingContains(source.Warnings, "account_key_reported_invalid") {
		health.HardBlocked = true
		health.ReasonCodes = append(health.ReasonCodes, ProviderBillingReasonKeyInvalid)
	}
	if source.LastErrorCode == "upstream_auth_rejected" {
		health.HardBlocked = true
		health.ReasonCodes = append(health.ReasonCodes, ProviderBillingReasonAuthRejected)
	}
	if balance != nil && balance.AmountMicros <= 0 {
		switch balance.Kind {
		case ProviderBalanceKindKeyQuota:
			health.HardBlocked = true
			health.ReasonCodes = append(health.ReasonCodes, ProviderBillingReasonKeyQuotaExhausted)
		case ProviderBalanceKindSubscription:
			if !balance.Unlimited {
				health.HardBlocked = true
				health.ReasonCodes = append(health.ReasonCodes, ProviderBillingReasonSubscriptionExhausted)
			}
		}
	}
	if source.ConsecutiveFailures >= providerBillingRoutingFailureThreshold {
		health.ReasonCodes = append(health.ReasonCodes, ProviderBillingReasonSyncUnhealthy)
	}
	if source.LastSuccessAt == nil {
		health.ReasonCodes = append(health.ReasonCodes, ProviderBillingReasonEvidenceMissing)
	} else if now.Sub(source.LastSuccessAt.UTC()) > staleAfter {
		health.ReasonCodes = append(health.ReasonCodes, ProviderBillingReasonEvidenceStale)
	}
	health.ReasonCodes = cleanStringList(health.ReasonCodes)
	if health.HardBlocked {
		health.Status = ProviderBillingRoutingHealthBlocked
		health.EconomicSwitchEligible = false
	} else if len(health.ReasonCodes) > 0 {
		health.Status = ProviderBillingRoutingHealthDegraded
		health.EconomicSwitchEligible = false
	}
	return health
}

func providerBillingContains(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func (s *Service) providerBillingRoutingHealthByAccount(ctx context.Context, now time.Time) (map[string]ProviderBillingRoutingHealth, error) {
	sources, err := s.repo.ListProviderBillingSources(ctx)
	if err != nil {
		return nil, err
	}
	return s.providerBillingRoutingHealthForSources(ctx, sources, now)
}

func (s *Service) providerBillingRoutingHealthForSources(ctx context.Context, sources []ProviderBillingSource, now time.Time) (map[string]ProviderBillingRoutingHealth, error) {
	balances, err := s.repo.ListLatestProviderBalanceSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	balanceBySource := make(map[string]ProviderBalanceSnapshotRecord, len(balances))
	for _, balance := range balances {
		balanceBySource[balance.SourceID] = balance
	}
	healthByAccount := make(map[string]ProviderBillingRoutingHealth, len(sources))
	for _, source := range sources {
		var balance *ProviderBalanceSnapshotRecord
		if value, found := balanceBySource[source.ID]; found {
			copy := value
			balance = &copy
		}
		healthByAccount[source.ProviderAccountID] = providerBillingRoutingHealth(source, balance, now)
	}
	return healthByAccount, nil
}

func (s *Service) enrichProviderBillingSources(ctx context.Context, sources []ProviderBillingSource) ([]ProviderBillingSource, error) {
	healthByAccount, err := s.providerBillingRoutingHealthForSources(ctx, sources, s.nowUTC())
	if err != nil {
		return nil, err
	}
	for index := range sources {
		if health, found := healthByAccount[sources[index].ProviderAccountID]; found {
			copy := health
			sources[index].RoutingHealth = &copy
		}
	}
	return sources, nil
}

func (s *Service) enrichProviderBillingSource(ctx context.Context, source ProviderBillingSource) (ProviderBillingSource, error) {
	sources, err := s.enrichProviderBillingSources(ctx, []ProviderBillingSource{source})
	if err != nil {
		return ProviderBillingSource{}, err
	}
	return sources[0], nil
}
