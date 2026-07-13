package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGatewayRiskBlockIsEnforcedAndExpires(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	service := NewService(repo, "/v1")
	auth := GatewayAuthContext{APIKey: APIKeyRecord{ID: "key_1"}}

	if err := service.BlockAPIKey(ctx, auth.APIKey.ID, "rule_1", "rpm threshold", time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatalf("BlockAPIKey(): %v", err)
	}
	if err := service.EnforceGatewayPolicy(ctx, auth); !errors.Is(err, ErrGatewayRiskBlocked) {
		t.Fatalf("EnforceGatewayPolicy() err = %v", err)
	}
	blocks, err := service.ListActiveGatewayRiskBlocks(ctx)
	if err != nil || len(blocks) != 1 || blocks[0].APIKeyID != auth.APIKey.ID {
		t.Fatalf("ListActiveGatewayRiskBlocks() blocks=%+v err=%v", blocks, err)
	}
	if err := service.ClearGatewayRiskBlock(ctx, "admin", auth.APIKey.ID); err != nil {
		t.Fatalf("ClearGatewayRiskBlock(): %v", err)
	}
	if err := service.EnforceGatewayPolicy(ctx, auth); err != nil {
		t.Fatalf("cleared block must not be enforced: %v", err)
	}
	if err := service.BlockAPIKey(ctx, auth.APIKey.ID, "rule_1", "rpm threshold", time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatalf("BlockAPIKey(second): %v", err)
	}
	repo.mu.Lock()
	block := repo.riskBlocks[auth.APIKey.ID]
	block.ExpiresAt = time.Now().UTC().Add(-time.Second)
	repo.riskBlocks[auth.APIKey.ID] = block
	repo.mu.Unlock()
	if err := service.EnforceGatewayPolicy(ctx, auth); err != nil {
		t.Fatalf("expired block must not be enforced: %v", err)
	}
}
