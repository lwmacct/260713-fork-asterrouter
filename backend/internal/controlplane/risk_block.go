package controlplane

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrGatewayRiskBlocked = errors.New("gateway api key is temporarily blocked by risk control")

type GatewayRiskBlock struct {
	APIKeyID  string    `json:"api_key_id"`
	RuleID    string    `json:"rule_id"`
	Reason    string    `json:"reason"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) BlockAPIKey(ctx context.Context, apiKeyID, ruleID, reason string, expiresAt time.Time) error {
	apiKeyID = strings.TrimSpace(apiKeyID)
	if apiKeyID == "" || !expiresAt.After(time.Now().UTC()) {
		return errors.New("valid api key and future risk block expiry are required")
	}
	return s.repo.SaveGatewayRiskBlock(ctx, GatewayRiskBlock{APIKeyID: apiKeyID, RuleID: strings.TrimSpace(ruleID), Reason: strings.TrimSpace(reason), ExpiresAt: expiresAt.UTC(), CreatedAt: time.Now().UTC()})
}

func (s *Service) ListActiveGatewayRiskBlocks(ctx context.Context) ([]GatewayRiskBlock, error) {
	return s.repo.ListActiveGatewayRiskBlocks(ctx, time.Now().UTC())
}

func (s *Service) ClearGatewayRiskBlock(ctx context.Context, actor, apiKeyID string) error {
	apiKeyID = strings.TrimSpace(apiKeyID)
	if apiKeyID == "" {
		return errors.New("api key id is required")
	}
	if err := s.repo.DeleteGatewayRiskBlock(ctx, apiKeyID); err != nil {
		return err
	}
	return s.audit(ctx, actor, "risk_unblock", "api_key", apiKeyID, "Cleared temporary gateway risk block")
}
