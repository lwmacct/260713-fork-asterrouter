package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var (
	ErrCustomerCodeNotFound       = errors.New("兑换码无效")
	ErrCustomerCodeExpired        = errors.New("兑换码已过期")
	ErrCustomerCodeUnavailable    = errors.New("兑换码已失效")
	ErrCustomerCodeAlreadyUsed    = errors.New("该兑换码已兑换")
	ErrCustomerPaymentUnavailable = errors.New("支付渠道尚未配置，请联系站点管理员")
)

func (s *Service) CustomerBillingOverview(ctx context.Context, actor string) (CustomerBillingOverview, error) {
	user, err := s.customerWorkspaceUser(ctx, actor)
	if err != nil {
		return CustomerBillingOverview{}, err
	}
	wallet, err := s.repo.GetCustomerWallet(ctx, user.ID)
	if err != nil {
		return CustomerBillingOverview{}, err
	}
	vouchers, err := s.repo.ListAvailableCustomerVouchers(ctx, user.ID, time.Now().UTC())
	if err != nil {
		return CustomerBillingOverview{}, err
	}
	return CustomerBillingOverview{
		BalanceCents:       user.BalanceCents,
		GiftBalanceCents:   wallet.GiftBalanceCents,
		ProfitBalanceCents: wallet.ProfitBalanceCents,
		TotalCents:         user.BalanceCents + wallet.GiftBalanceCents + wallet.ProfitBalanceCents,
		RechargeOptions:    []int{1000, 2000, 5000, 10000, 20000, 50000},
		PaymentChannels: []CustomerPaymentChannel{
			{ID: "wechat", Name: "微信支付", Enabled: false},
			{ID: "alipay", Name: "支付宝", Enabled: false},
		},
		Vouchers: vouchers,
	}, nil
}

func (s *Service) CustomerBillingEntries(ctx context.Context, actor string, query CustomerBillingQuery) (CustomerBillingEntries, error) {
	user, err := s.customerWorkspaceUser(ctx, actor)
	if err != nil {
		return CustomerBillingEntries{}, err
	}
	query.UserID = user.ID
	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Limit > 10000 {
		query.Limit = 10000
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	items, total, err := s.repo.ListCustomerBillingEntries(ctx, query)
	if err != nil {
		return CustomerBillingEntries{}, err
	}
	return CustomerBillingEntries{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func (s *Service) RedeemCustomerCode(ctx context.Context, actor string, request CustomerRedeemRequest) (CustomerRedeemResult, error) {
	user, err := s.customerWorkspaceUser(ctx, actor)
	if err != nil {
		return CustomerRedeemResult{}, err
	}
	code := strings.TrimSpace(request.Code)
	if code == "" {
		return CustomerRedeemResult{}, errors.New("请输入兑换码")
	}
	now := time.Now().UTC()
	entry, err := s.repo.RedeemCustomerCode(ctx, CustomerCodeRedemption{
		UserID: user.ID, CodeHash: hashCustomerRedemptionCode(code), EntryID: "cbe_" + randomToken(12), Now: now,
	})
	if err != nil {
		return CustomerRedeemResult{}, err
	}
	overview, err := s.CustomerBillingOverview(ctx, actor)
	if err != nil {
		return CustomerRedeemResult{}, err
	}
	return CustomerRedeemResult{Entry: entry, Overview: overview}, nil
}

func (s *Service) CreateCustomerRechargeOrder(ctx context.Context, actor string, request CustomerRechargeRequest) (CustomerRechargeOrder, error) {
	if _, err := s.customerWorkspaceUser(ctx, actor); err != nil {
		return CustomerRechargeOrder{}, err
	}
	if request.AmountCents < 100 || request.AmountCents > 10000000 {
		return CustomerRechargeOrder{}, errors.New("充值金额必须在 1 元到 100000 元之间")
	}
	if request.PaymentMethod != "wechat" && request.PaymentMethod != "alipay" {
		return CustomerRechargeOrder{}, errors.New("请选择支付方式")
	}
	return CustomerRechargeOrder{}, ErrCustomerPaymentUnavailable
}

func (s *Service) customerWorkspaceUser(ctx context.Context, actor string) (WorkspaceUser, error) {
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return WorkspaceUser{}, err
	}
	user, ok := workspaceUserByActor(users, actor)
	if !ok || user.Status != WorkspaceUserStatusActive {
		return WorkspaceUser{}, errors.New("客户账户不存在或已停用")
	}
	return user, nil
}

func hashCustomerRedemptionCode(code string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}
