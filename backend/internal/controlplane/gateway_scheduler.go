package controlplane

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

const gatewaySchedulingWindow = time.Minute

type gatewayRateSample struct {
	at     time.Time
	tokens int
}

type gatewayScheduler struct {
	mu             sync.Mutex
	rateSamples    map[string][]gatewayRateSample
	halfOpenProbes map[string]bool
}

func newGatewayScheduler() *gatewayScheduler {
	return &gatewayScheduler{
		rateSamples:    map[string][]gatewayRateSample{},
		halfOpenProbes: map[string]bool{},
	}
}

type ProviderAccountPermit struct {
	release func()
}

func (p ProviderAccountPermit) Release() {
	if p.release != nil {
		p.release()
	}
}

// TryAcquireProviderAccountPermit atomically reserves RPM/TPM capacity and a
// half-open circuit probe. A successful reservation remains in the rolling
// minute window because it represents admitted work; Release only frees the
// half-open probe lock.
func (s *Service) TryAcquireProviderAccountPermit(provider GatewayProvider, estimatedTokens int) (ProviderAccountPermit, string, bool) {
	if s.scheduler == nil || provider.AccountID == "" {
		return ProviderAccountPermit{}, "", true
	}
	now := time.Now().UTC()
	estimatedTokens = nonNegative(estimatedTokens)
	scheduler := s.scheduler
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()

	if provider.CircuitState == CircuitStateOpen && !provider.CircuitProbe {
		return ProviderAccountPermit{}, "circuit_open", false
	}
	if provider.CircuitProbe && scheduler.halfOpenProbes[provider.AccountID] {
		return ProviderAccountPermit{}, "circuit_half_open_busy", false
	}
	samples := scheduler.pruneSamples(provider.AccountID, now)
	requests, tokens := rateWindowUsage(samples)
	if provider.RPMLimit > 0 && requests >= provider.RPMLimit {
		return ProviderAccountPermit{}, "rpm_exhausted", false
	}
	if provider.TPMLimit > 0 && tokens+estimatedTokens > provider.TPMLimit {
		return ProviderAccountPermit{}, "tpm_exhausted", false
	}
	scheduler.rateSamples[provider.AccountID] = append(samples, gatewayRateSample{at: now, tokens: estimatedTokens})
	if provider.CircuitProbe {
		scheduler.halfOpenProbes[provider.AccountID] = true
	}
	released := false
	return ProviderAccountPermit{release: func() {
		scheduler.mu.Lock()
		defer scheduler.mu.Unlock()
		if released {
			return
		}
		released = true
		if provider.CircuitProbe {
			delete(scheduler.halfOpenProbes, provider.AccountID)
		}
	}}, "", true
}

func (s *Service) providerAccountRateHeadroom(account ProviderAccount, now time.Time) float64 {
	if s.scheduler == nil {
		return 1
	}
	concurrencyUsed := s.providerAccountSlotUsage(account.ID)
	s.scheduler.mu.Lock()
	defer s.scheduler.mu.Unlock()
	samples := s.scheduler.pruneSamples(account.ID, now)
	requests, tokens := rateWindowUsage(samples)
	rpmHeadroom := remainingRatio(requests, account.RPMLimit)
	tpmHeadroom := remainingRatio(tokens, account.TPMLimit)
	concurrencyHeadroom := remainingRatio(concurrencyUsed, account.Concurrency)
	return math.Min(rpmHeadroom, math.Min(tpmHeadroom, concurrencyHeadroom))
}

func (s *gatewayScheduler) pruneSamples(accountID string, now time.Time) []gatewayRateSample {
	cutoff := now.Add(-gatewaySchedulingWindow)
	samples := s.rateSamples[accountID]
	kept := samples[:0]
	for _, sample := range samples {
		if sample.at.After(cutoff) {
			kept = append(kept, sample)
		}
	}
	s.rateSamples[accountID] = kept
	return kept
}

func rateWindowUsage(samples []gatewayRateSample) (requests int, tokens int) {
	for _, sample := range samples {
		requests++
		tokens += sample.tokens
	}
	return requests, tokens
}

func remainingRatio(used int, limit int) float64 {
	if limit <= 0 {
		return 1
	}
	remaining := float64(limit-used) / float64(limit)
	if remaining < 0 {
		return 0
	}
	if remaining > 1 {
		return 1
	}
	return remaining
}

func weightedCandidateScore(weight int) float64 {
	if weight <= 0 {
		weight = 1
	}
	u := rand.Float64()
	if u == 0 {
		u = math.SmallestNonzeroFloat64
	}
	return -math.Log(u) / float64(weight)
}
