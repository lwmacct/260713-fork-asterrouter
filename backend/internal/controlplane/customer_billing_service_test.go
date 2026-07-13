package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCustomerBillingRedeemIsAtomicAndIsolated(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	first, _, err := svc.RegisterWorkspaceUser(ctx, "first-customer@example.test", "long-password", "First", false, WorkspaceUserDefaults{BalanceCents: 200})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := svc.RegisterWorkspaceUser(ctx, "second-customer@example.test", "long-password", "Second", false)
	if err != nil {
		t.Fatal(err)
	}
	code := "ASTER-CUSTOMER-500"
	if err := repo.SaveCustomerRedemptionCode(ctx, CustomerRedemptionCode{
		ID: "crc_test", CodeHash: hashCustomerRedemptionCode(code), Title: "测试兑换金",
		AmountCents: 500, Status: CustomerRedemptionCodeActive, MaxRedemptions: 2, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.RedeemCustomerCode(ctx, first.Email, CustomerRedeemRequest{Code: code})
	if err != nil {
		t.Fatalf("RedeemCustomerCode(): %v", err)
	}
	if result.Entry.AmountCents != 500 || result.Overview.BalanceCents != 700 {
		t.Fatalf("unexpected first redemption: %+v", result)
	}
	if _, err := svc.RedeemCustomerCode(ctx, first.Email, CustomerRedeemRequest{Code: code}); !errors.Is(err, ErrCustomerCodeAlreadyUsed) {
		t.Fatalf("duplicate redemption error = %v", err)
	}
	if _, err := svc.RedeemCustomerCode(ctx, second.Email, CustomerRedeemRequest{Code: code}); err != nil {
		t.Fatalf("second customer redemption: %v", err)
	}

	firstEntries, err := svc.CustomerBillingEntries(ctx, first.Email, CustomerBillingQuery{UserID: second.ID, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if firstEntries.Total != 1 || len(firstEntries.Items) != 1 || firstEntries.Items[0].Reference != "crc_test" {
		t.Fatalf("first customer entries leaked or missing: %+v", firstEntries)
	}
	secondEntries, err := svc.CustomerBillingEntries(ctx, second.Email, CustomerBillingQuery{UserID: first.ID, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if secondEntries.Total != 1 || len(secondEntries.Items) != 1 {
		t.Fatalf("second customer entries leaked or missing: %+v", secondEntries)
	}
}

func TestCustomerRechargeDoesNotChangeBalanceWithoutPaymentProvider(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	user, _, err := svc.RegisterWorkspaceUser(ctx, "recharge@example.test", "long-password", "Recharge", false, WorkspaceUserDefaults{BalanceCents: 900})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateCustomerRechargeOrder(ctx, user.Email, CustomerRechargeRequest{AmountCents: 1000, PaymentMethod: "wechat"})
	if !errors.Is(err, ErrCustomerPaymentUnavailable) {
		t.Fatalf("recharge error = %v", err)
	}
	overview, err := svc.CustomerBillingOverview(ctx, user.Email)
	if err != nil {
		t.Fatal(err)
	}
	if overview.BalanceCents != 900 || overview.TotalCents != 900 {
		t.Fatalf("unconfigured payment changed balance: %+v", overview)
	}
}

func TestActiveWorkspaceUserCanUseCustomerSurface(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	user, _, err := svc.RegisterWorkspaceUser(ctx, "customer-surface@example.test", "long-password", "Customer", false)
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := svc.ActorCanSurface(ctx, user.Email, SurfaceCustomer)
	if err != nil || !allowed {
		t.Fatalf("customer surface allowed=%v err=%v", allowed, err)
	}
	allowed, err = svc.ActorCanSurface(ctx, user.Email, SurfaceRelayOperator)
	if err != nil || allowed {
		t.Fatalf("operator surface allowed=%v err=%v", allowed, err)
	}
}
