package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"
)

func (r *MemoryRepository) GetCustomerWallet(_ context.Context, userID string) (CustomerWallet, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if wallet, ok := r.customerWallets[userID]; ok {
		return wallet, nil
	}
	return CustomerWallet{UserID: userID}, nil
}

func (r *MemoryRepository) ListCustomerBillingEntries(_ context.Context, query CustomerBillingQuery) ([]CustomerBillingEntry, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]CustomerBillingEntry, 0)
	for _, entry := range r.customerEntries {
		if entry.UserID != query.UserID || (query.Kind != "" && entry.Kind != query.Kind) {
			continue
		}
		if query.From != nil && entry.CreatedAt.Before(*query.From) {
			continue
		}
		if query.To != nil && entry.CreatedAt.After(*query.To) {
			continue
		}
		items = append(items, entry)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	total := len(items)
	if query.Offset >= total {
		return []CustomerBillingEntry{}, total, nil
	}
	end := query.Offset + query.Limit
	if end > total {
		end = total
	}
	return append([]CustomerBillingEntry(nil), items[query.Offset:end]...), total, nil
}

func (r *MemoryRepository) ListAvailableCustomerVouchers(_ context.Context, userID string, now time.Time) ([]CustomerVoucher, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]CustomerVoucher, 0)
	for _, voucher := range r.customerVouchers {
		if voucher.UserID == userID && voucher.Status == "active" && (voucher.ExpiresAt == nil || voucher.ExpiresAt.After(now)) {
			items = append(items, voucher)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ExpiresAt == nil {
			return false
		}
		if items[j].ExpiresAt == nil {
			return true
		}
		return items[i].ExpiresAt.Before(*items[j].ExpiresAt)
	})
	return items, nil
}

func (r *MemoryRepository) SaveCustomerRedemptionCode(_ context.Context, code CustomerRedemptionCode) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.customerCodes[code.ID] = code
	return nil
}

func (r *MemoryRepository) RedeemCustomerCode(_ context.Context, request CustomerCodeRedemption) (CustomerBillingEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var code CustomerRedemptionCode
	found := false
	for _, item := range r.customerCodes {
		if item.CodeHash == request.CodeHash {
			code, found = item, true
			break
		}
	}
	if !found {
		return CustomerBillingEntry{}, ErrCustomerCodeNotFound
	}
	if code.ExpiresAt != nil && !code.ExpiresAt.After(request.Now) {
		return CustomerBillingEntry{}, ErrCustomerCodeExpired
	}
	if code.Status != CustomerRedemptionCodeActive || code.MaxRedemptions <= code.RedeemedCount {
		return CustomerBillingEntry{}, ErrCustomerCodeUnavailable
	}
	redemptionKey := code.ID + ":" + request.UserID
	if _, exists := r.customerRedemptions[redemptionKey]; exists {
		return CustomerBillingEntry{}, ErrCustomerCodeAlreadyUsed
	}
	user, exists := r.workspaceUsers[request.UserID]
	if !exists || user.Status != WorkspaceUserStatusActive {
		return CustomerBillingEntry{}, errors.New("客户账户不存在或已停用")
	}
	user.BalanceCents += code.AmountCents
	user.UpdatedAt = request.Now
	entry := CustomerBillingEntry{
		ID: request.EntryID, UserID: request.UserID, Kind: CustomerBillingKindRedeem,
		AmountCents: code.AmountCents, BalanceAfterCents: user.BalanceCents,
		Reference: code.ID, Description: code.Title, CreatedAt: request.Now,
	}
	code.RedeemedCount++
	if code.RedeemedCount >= code.MaxRedemptions {
		code.Status = "used"
	}
	r.workspaceUsers[user.ID] = user
	r.customerCodes[code.ID] = code
	r.customerEntries[entry.ID] = entry
	r.customerRedemptions[redemptionKey] = CustomerRedemption{CodeID: code.ID, UserID: user.ID, EntryID: entry.ID, RedeemedAt: request.Now}
	return entry, nil
}

func (r *PostgresRepository) GetCustomerWallet(ctx context.Context, userID string) (CustomerWallet, error) {
	wallet := CustomerWallet{UserID: userID}
	err := r.db.QueryRowContext(ctx, `SELECT gift_balance_cents, profit_balance_cents, updated_at FROM customer_wallets WHERE user_id=$1`, userID).
		Scan(&wallet.GiftBalanceCents, &wallet.ProfitBalanceCents, &wallet.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return wallet, nil
	}
	return wallet, err
}

func (r *PostgresRepository) ListCustomerBillingEntries(ctx context.Context, query CustomerBillingQuery) ([]CustomerBillingEntry, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM customer_billing_entries
WHERE user_id=$1 AND ($2='' OR kind=$2)
  AND ($3::timestamptz IS NULL OR created_at >= $3)
  AND ($4::timestamptz IS NULL OR created_at <= $4)`, query.UserID, query.Kind, query.From, query.To).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id,user_id,kind,amount_cents,balance_after_cents,reference,description,created_at
FROM customer_billing_entries
WHERE user_id=$1 AND ($2='' OR kind=$2)
  AND ($3::timestamptz IS NULL OR created_at >= $3)
  AND ($4::timestamptz IS NULL OR created_at <= $4)
ORDER BY created_at DESC LIMIT $5 OFFSET $6`, query.UserID, query.Kind, query.From, query.To, query.Limit, query.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]CustomerBillingEntry, 0)
	for rows.Next() {
		var item CustomerBillingEntry
		if err := rows.Scan(&item.ID, &item.UserID, &item.Kind, &item.AmountCents, &item.BalanceAfterCents, &item.Reference, &item.Description, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *PostgresRepository) ListAvailableCustomerVouchers(ctx context.Context, userID string, now time.Time) ([]CustomerVoucher, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id,user_id,title,amount_cents,minimum_recharge_cents,status,expires_at,created_at
FROM customer_vouchers WHERE user_id=$1 AND status='active' AND (expires_at IS NULL OR expires_at > $2)
ORDER BY expires_at ASC NULLS LAST, created_at DESC`, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CustomerVoucher, 0)
	for rows.Next() {
		var item CustomerVoucher
		if err := rows.Scan(&item.ID, &item.UserID, &item.Title, &item.AmountCents, &item.MinimumRechargeCents, &item.Status, &item.ExpiresAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresRepository) SaveCustomerRedemptionCode(ctx context.Context, code CustomerRedemptionCode) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO customer_redemption_codes(id,code_hash,title,amount_cents,status,max_redemptions,redeemed_count,expires_at,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(id) DO UPDATE SET code_hash=EXCLUDED.code_hash,title=EXCLUDED.title,amount_cents=EXCLUDED.amount_cents,status=EXCLUDED.status,max_redemptions=EXCLUDED.max_redemptions,redeemed_count=EXCLUDED.redeemed_count,expires_at=EXCLUDED.expires_at`,
		code.ID, code.CodeHash, code.Title, code.AmountCents, code.Status, code.MaxRedemptions, code.RedeemedCount, code.ExpiresAt, code.CreatedAt)
	return err
}

func (r *PostgresRepository) RedeemCustomerCode(ctx context.Context, request CustomerCodeRedemption) (CustomerBillingEntry, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return CustomerBillingEntry{}, err
	}
	defer tx.Rollback()
	var code CustomerRedemptionCode
	err = tx.QueryRowContext(ctx, `
SELECT id,title,amount_cents,status,max_redemptions,redeemed_count,expires_at,created_at
FROM customer_redemption_codes WHERE code_hash=$1 FOR UPDATE`, request.CodeHash).
		Scan(&code.ID, &code.Title, &code.AmountCents, &code.Status, &code.MaxRedemptions, &code.RedeemedCount, &code.ExpiresAt, &code.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return CustomerBillingEntry{}, ErrCustomerCodeNotFound
	}
	if err != nil {
		return CustomerBillingEntry{}, err
	}
	if code.ExpiresAt != nil && !code.ExpiresAt.After(request.Now) {
		return CustomerBillingEntry{}, ErrCustomerCodeExpired
	}
	if code.Status != CustomerRedemptionCodeActive || code.RedeemedCount >= code.MaxRedemptions {
		return CustomerBillingEntry{}, ErrCustomerCodeUnavailable
	}
	var alreadyUsed bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM customer_redemptions WHERE code_id=$1 AND user_id=$2)`, code.ID, request.UserID).Scan(&alreadyUsed); err != nil {
		return CustomerBillingEntry{}, err
	}
	if alreadyUsed {
		return CustomerBillingEntry{}, ErrCustomerCodeAlreadyUsed
	}
	var currentBalance int
	if err := tx.QueryRowContext(ctx, `SELECT balance_cents FROM workspace_users WHERE id=$1 AND status=$2 FOR UPDATE`, request.UserID, WorkspaceUserStatusActive).Scan(&currentBalance); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CustomerBillingEntry{}, errors.New("客户账户不存在或已停用")
		}
		return CustomerBillingEntry{}, err
	}
	entry := CustomerBillingEntry{
		ID: request.EntryID, UserID: request.UserID, Kind: CustomerBillingKindRedeem,
		AmountCents: code.AmountCents, BalanceAfterCents: currentBalance + code.AmountCents,
		Reference: code.ID, Description: strings.TrimSpace(code.Title), CreatedAt: request.Now,
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workspace_users SET balance_cents=$1,updated_at=$2 WHERE id=$3`, entry.BalanceAfterCents, request.Now, request.UserID); err != nil {
		return CustomerBillingEntry{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO customer_billing_entries(id,user_id,kind,amount_cents,balance_after_cents,reference,description,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, entry.ID, entry.UserID, entry.Kind, entry.AmountCents, entry.BalanceAfterCents, entry.Reference, entry.Description, entry.CreatedAt); err != nil {
		return CustomerBillingEntry{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO customer_redemptions(code_id,user_id,entry_id,redeemed_at) VALUES($1,$2,$3,$4)`, code.ID, request.UserID, entry.ID, request.Now); err != nil {
		return CustomerBillingEntry{}, err
	}
	nextStatus := code.Status
	if code.RedeemedCount+1 >= code.MaxRedemptions {
		nextStatus = "used"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE customer_redemption_codes SET redeemed_count=redeemed_count+1,status=$1 WHERE id=$2`, nextStatus, code.ID); err != nil {
		return CustomerBillingEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return CustomerBillingEntry{}, err
	}
	return entry, nil
}
