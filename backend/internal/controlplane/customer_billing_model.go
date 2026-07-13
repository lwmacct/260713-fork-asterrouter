package controlplane

import "time"

const (
	CustomerBillingKindRecharge = "recharge"
	CustomerBillingKindRedeem   = "redeem"
	CustomerBillingKindUsage    = "usage"
	CustomerBillingKindRefund   = "refund"
	CustomerBillingKindGift     = "gift"
	CustomerBillingKindProfit   = "profit"

	CustomerRedemptionCodeActive = "active"
)

type CustomerWallet struct {
	UserID             string    `json:"user_id"`
	GiftBalanceCents   int       `json:"gift_balance_cents"`
	ProfitBalanceCents int       `json:"profit_balance_cents"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type CustomerBillingEntry struct {
	ID                string    `json:"id"`
	UserID            string    `json:"-"`
	Kind              string    `json:"kind"`
	AmountCents       int       `json:"amount_cents"`
	BalanceAfterCents int       `json:"balance_after_cents"`
	Reference         string    `json:"reference"`
	Description       string    `json:"description"`
	CreatedAt         time.Time `json:"created_at"`
}

type CustomerBillingQuery struct {
	UserID string
	Kind   string
	From   *time.Time
	To     *time.Time
	Limit  int
	Offset int
}

type CustomerVoucher struct {
	ID                   string     `json:"id"`
	UserID               string     `json:"-"`
	Title                string     `json:"title"`
	AmountCents          int        `json:"amount_cents"`
	MinimumRechargeCents int        `json:"minimum_recharge_cents"`
	Status               string     `json:"status"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
}

type CustomerRedemptionCode struct {
	ID             string     `json:"id"`
	CodeHash       string     `json:"-"`
	Title          string     `json:"title"`
	AmountCents    int        `json:"amount_cents"`
	Status         string     `json:"status"`
	MaxRedemptions int        `json:"max_redemptions"`
	RedeemedCount  int        `json:"redeemed_count"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type CustomerRedemption struct {
	CodeID     string    `json:"code_id"`
	UserID     string    `json:"user_id"`
	EntryID    string    `json:"entry_id"`
	RedeemedAt time.Time `json:"redeemed_at"`
}

type CustomerCodeRedemption struct {
	UserID   string
	CodeHash string
	EntryID  string
	Now      time.Time
}

type CustomerPaymentChannel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type CustomerBillingOverview struct {
	BalanceCents       int                      `json:"balance_cents"`
	GiftBalanceCents   int                      `json:"gift_balance_cents"`
	ProfitBalanceCents int                      `json:"profit_balance_cents"`
	TotalCents         int                      `json:"total_cents"`
	RechargeOptions    []int                    `json:"recharge_options"`
	PaymentChannels    []CustomerPaymentChannel `json:"payment_channels"`
	Vouchers           []CustomerVoucher        `json:"vouchers"`
}

type CustomerBillingEntries struct {
	Items  []CustomerBillingEntry `json:"items"`
	Total  int                    `json:"total"`
	Limit  int                    `json:"limit"`
	Offset int                    `json:"offset"`
}

type CustomerRedeemRequest struct {
	Code string `json:"code"`
}

type CustomerRedeemResult struct {
	Entry    CustomerBillingEntry    `json:"entry"`
	Overview CustomerBillingOverview `json:"overview"`
}

type CustomerRechargeRequest struct {
	AmountCents   int    `json:"amount_cents"`
	PaymentMethod string `json:"payment_method"`
	VoucherID     string `json:"voucher_id"`
}

type CustomerRechargeOrder struct {
	ID            string    `json:"id"`
	AmountCents   int       `json:"amount_cents"`
	PaymentMethod string    `json:"payment_method"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}
