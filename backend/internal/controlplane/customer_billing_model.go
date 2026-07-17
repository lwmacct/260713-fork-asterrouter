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
	UserID              string    `json:"user_id"`
	GiftBalanceMicros   int64     `json:"gift_balance_micros"`
	ProfitBalanceMicros int64     `json:"profit_balance_micros"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type CustomerBillingEntry struct {
	ID                 string    `json:"id"`
	UserID             string    `json:"-"`
	Kind               string    `json:"kind"`
	AmountMicros       int64     `json:"amount_micros"`
	BalanceAfterMicros int64     `json:"balance_after_micros"`
	Reference          string    `json:"reference"`
	Description        string    `json:"description"`
	CreatedAt          time.Time `json:"created_at"`
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
	ID                    string     `json:"id"`
	UserID                string     `json:"-"`
	Title                 string     `json:"title"`
	AmountMicros          int64      `json:"amount_micros"`
	MinimumRechargeMicros int64      `json:"minimum_recharge_micros"`
	Status                string     `json:"status"`
	ExpiresAt             *time.Time `json:"expires_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
}

type CustomerRedemptionCode struct {
	ID             string     `json:"id"`
	CodeHash       string     `json:"-"`
	Title          string     `json:"title"`
	AmountMicros   int64      `json:"amount_micros"`
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
	BalanceMicros       int64                    `json:"balance_micros"`
	GiftBalanceMicros   int64                    `json:"gift_balance_micros"`
	ProfitBalanceMicros int64                    `json:"profit_balance_micros"`
	TotalMicros         int64                    `json:"total_micros"`
	RechargeOptions     []int64                  `json:"recharge_options"`
	PaymentChannels     []CustomerPaymentChannel `json:"payment_channels"`
	Vouchers            []CustomerVoucher        `json:"vouchers"`
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
	AmountMicros  int64  `json:"amount_micros"`
	PaymentMethod string `json:"payment_method"`
	VoucherID     string `json:"voucher_id"`
}

type CustomerRechargeOrder struct {
	ID            string    `json:"id"`
	AmountMicros  int64     `json:"amount_micros"`
	PaymentMethod string    `json:"payment_method"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}
