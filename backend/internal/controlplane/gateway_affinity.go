package controlplane

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSupplierAffinityTTL = 24 * time.Hour
	defaultAccountAffinityTTL  = 30 * time.Minute
)

type GatewayAffinityInput struct {
	TenantID      string
	PrincipalID   string
	CredentialID  string
	Model         string
	Protocol      string
	RouteGroup    string
	StickyKey     string
	PolicyVersion int
}

func (s *Service) PreferGatewayCandidatesWithAffinity(ctx context.Context, input GatewayAffinityInput, candidates []GatewayProvider) []GatewayProvider {
	if s == nil || s.repo == nil || len(candidates) < 2 {
		return candidates
	}
	now := s.nowUTC()
	supplierTTL, accountTTL := s.gatewayAffinityTTLs(ctx)
	if strings.TrimSpace(input.StickyKey) != "" {
		key := s.gatewayAffinityScopeKey(AffinityBindingAccount, input)
		if binding, found, err := s.repo.FindRoutingAffinityBinding(ctx, key, now); err == nil && found {
			if preferred, ok := preferBoundGatewayCandidate(candidates, binding, true, "session account affinity reused"); ok {
				binding.LastReusedAt = now
				binding.ExpiresAt = now.Add(accountTTL)
				_ = s.repo.SaveRoutingAffinityBinding(ctx, binding)
				return preferred
			}
		}
	}
	key := s.gatewayAffinityScopeKey(AffinityBindingSupplier, input)
	if binding, found, err := s.repo.FindRoutingAffinityBinding(ctx, key, now); err == nil && found {
		if preferred, ok := preferBoundGatewayCandidate(candidates, binding, false, "customer supplier affinity reused"); ok {
			binding.LastReusedAt = now
			binding.ExpiresAt = now.Add(supplierTTL)
			_ = s.repo.SaveRoutingAffinityBinding(ctx, binding)
			return preferred
		}
	}
	return candidates
}

func (s *Service) BindGatewayCandidateAffinity(ctx context.Context, input GatewayAffinityInput, provider GatewayProvider) error {
	if s == nil || s.repo == nil || strings.TrimSpace(provider.ID) == "" {
		return nil
	}
	now := s.nowUTC()
	supplierTTL, accountTTL := s.gatewayAffinityTTLs(ctx)
	supplierBinding := RoutingAffinityBinding{
		ScopeKey: s.gatewayAffinityScopeKey(AffinityBindingSupplier, input), Kind: AffinityBindingSupplier,
		ProviderID: provider.ID, Model: strings.TrimSpace(input.Model), Protocol: strings.TrimSpace(input.Protocol),
		PolicyVersion: input.PolicyVersion, CreatedAt: now, LastReusedAt: now, ExpiresAt: now.Add(supplierTTL),
	}
	if err := s.repo.SaveRoutingAffinityBinding(ctx, supplierBinding); err != nil {
		return err
	}
	if strings.TrimSpace(input.StickyKey) == "" || !provider.StickyEnabled || strings.TrimSpace(provider.AccountID) == "" || strings.TrimSpace(provider.RouteID) == "" {
		return nil
	}
	if provider.StickyTTLSeconds > 0 {
		accountTTL = time.Duration(provider.StickyTTLSeconds) * time.Second
	}
	accountBinding := RoutingAffinityBinding{
		ScopeKey: s.gatewayAffinityScopeKey(AffinityBindingAccount, input), Kind: AffinityBindingAccount,
		ProviderID: provider.ID, ProviderAccountID: provider.AccountID, RouteID: provider.RouteID,
		Model: strings.TrimSpace(input.Model), Protocol: strings.TrimSpace(input.Protocol), PolicyVersion: input.PolicyVersion,
		CreatedAt: now, LastReusedAt: now, ExpiresAt: now.Add(accountTTL),
	}
	return s.repo.SaveRoutingAffinityBinding(ctx, accountBinding)
}

func preferBoundGatewayCandidate(candidates []GatewayProvider, binding RoutingAffinityBinding, requireAccount bool, reason string) ([]GatewayProvider, bool) {
	for index, candidate := range candidates {
		if candidate.ID != binding.ProviderID {
			continue
		}
		if requireAccount && (!candidate.StickyEnabled || candidate.AccountID != binding.ProviderAccountID || candidate.RouteID != binding.RouteID) {
			continue
		}
		out := append([]GatewayProvider(nil), candidates...)
		selected := out[index]
		selected.SelectionReason = appendSelectionReason(selected.SelectionReason, reason)
		if index > 0 {
			copy(out[1:index+1], out[0:index])
		}
		out[0] = selected
		return out, true
	}
	return candidates, false
}

func appendSelectionReason(current, reason string) string {
	current = strings.TrimSpace(current)
	if current == "" {
		return reason
	}
	return current + "; " + reason
}

func (s *Service) gatewayAffinityTTLs(ctx context.Context) (time.Duration, time.Duration) {
	supplierTTL := defaultSupplierAffinityTTL
	accountTTL := defaultAccountAffinityTTL
	policy, found, err := s.repo.GetEffectivePricingPolicy(ctx)
	if err != nil || !found {
		return supplierTTL, accountTTL
	}
	if policy.SupplierAffinityTTLSeconds > 0 {
		supplierTTL = time.Duration(policy.SupplierAffinityTTLSeconds) * time.Second
	}
	if policy.AccountAffinityTTLSeconds > 0 {
		accountTTL = time.Duration(policy.AccountAffinityTTLSeconds) * time.Second
	}
	return supplierTTL, accountTTL
}

func (s *Service) gatewayAffinityScopeKey(kind string, input GatewayAffinityInput) string {
	identity := ""
	switch kind {
	case AffinityBindingAccount:
		identity = strings.Join([]string{kind, input.CredentialID, input.Model, input.Protocol, input.RouteGroup, input.StickyKey, strconv.Itoa(input.PolicyVersion)}, "\x00")
	default:
		principalID := strings.TrimSpace(input.PrincipalID)
		if principalID == "" {
			principalID = strings.TrimSpace(input.CredentialID)
		}
		identity = strings.Join([]string{kind, input.TenantID, principalID, input.Model, input.Protocol, input.RouteGroup, strconv.Itoa(input.PolicyVersion)}, "\x00")
	}
	mac := hmac.New(sha256.New, []byte(s.secretKey))
	_, _ = mac.Write([]byte(identity))
	return "affinity_" + hex.EncodeToString(mac.Sum(nil))
}
