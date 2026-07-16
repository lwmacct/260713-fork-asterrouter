package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (r *PostgresRepository) FindActiveGatewayRiskBlock(ctx context.Context, apiKeyID string, now time.Time) (GatewayRiskBlock, bool, error) {
	var block GatewayRiskBlock
	err := r.db.QueryRowContext(ctx, `SELECT api_key_id,rule_id,reason,expires_at,created_at FROM gateway_risk_blocks WHERE api_key_id=$1 AND expires_at>$2`, apiKeyID, now).Scan(&block.APIKeyID, &block.RuleID, &block.Reason, &block.ExpiresAt, &block.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return GatewayRiskBlock{}, false, nil
	}
	return block, err == nil, err
}

func (r *PostgresRepository) SaveGatewayRiskBlock(ctx context.Context, block GatewayRiskBlock) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO gateway_risk_blocks(api_key_id,rule_id,reason,expires_at,created_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(api_key_id) DO UPDATE SET rule_id=EXCLUDED.rule_id,reason=EXCLUDED.reason,expires_at=GREATEST(gateway_risk_blocks.expires_at,EXCLUDED.expires_at),created_at=EXCLUDED.created_at`, block.APIKeyID, block.RuleID, block.Reason, block.ExpiresAt, block.CreatedAt)
	return err
}

func (r *PostgresRepository) ListActiveGatewayRiskBlocks(ctx context.Context, now time.Time) ([]GatewayRiskBlock, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT api_key_id,rule_id,reason,expires_at,created_at FROM gateway_risk_blocks WHERE expires_at>$1 ORDER BY expires_at DESC`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]GatewayRiskBlock, 0)
	for rows.Next() {
		var block GatewayRiskBlock
		if err := rows.Scan(&block.APIKeyID, &block.RuleID, &block.Reason, &block.ExpiresAt, &block.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, block)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) DeleteGatewayRiskBlock(ctx context.Context, apiKeyID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM gateway_risk_blocks WHERE api_key_id=$1`, apiKeyID)
	return err
}
