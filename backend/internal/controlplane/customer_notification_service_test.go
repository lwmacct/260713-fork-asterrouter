package controlplane

import (
	"context"
	"testing"
	"time"
)

func TestCustomerNotificationSettingsDefaultsValidationAndIsolation(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	first, _, err := svc.RegisterWorkspaceUser(ctx, "notify-first@example.test", "long-password", "First", false)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := svc.RegisterWorkspaceUser(ctx, "notify-second@example.test", "long-password", "Second", false)
	if err != nil {
		t.Fatal(err)
	}

	settings, err := svc.CustomerNotificationSettings(ctx, first.Email)
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.Preferences) != 9 {
		t.Fatalf("default preferences = %d, want 9", len(settings.Preferences))
	}
	marketing := notificationPreferenceByType(t, settings.Preferences, CustomerNotificationMarketing)
	if marketing.Enabled || !marketing.Marketing {
		t.Fatalf("marketing default = %+v", marketing)
	}

	invalid := cloneNotificationPreferences(settings.Preferences)
	balance := notificationPreferenceByType(t, invalid, CustomerNotificationBalanceLow)
	negative := -1.0
	balance.Threshold = &negative
	replaceNotificationPreference(invalid, balance)
	if _, err := svc.UpdateCustomerNotificationSettings(ctx, first.Email, CustomerNotificationSettingsRequest{Preferences: invalid}); err == nil {
		t.Fatal("negative balance threshold was accepted")
	}

	updated := cloneNotificationPreferences(settings.Preferences)
	marketing = notificationPreferenceByType(t, updated, CustomerNotificationMarketing)
	marketing.Enabled = true
	replaceNotificationPreference(updated, marketing)
	result, err := svc.UpdateCustomerNotificationSettings(ctx, first.Email, CustomerNotificationSettingsRequest{Preferences: updated})
	if err != nil {
		t.Fatal(err)
	}
	if !notificationPreferenceByType(t, result.Preferences, CustomerNotificationMarketing).Enabled {
		t.Fatal("saved marketing preference was not persisted")
	}
	secondSettings, err := svc.CustomerNotificationSettings(ctx, second.Email)
	if err != nil {
		t.Fatal(err)
	}
	if notificationPreferenceByType(t, secondSettings.Preferences, CustomerNotificationMarketing).Enabled {
		t.Fatal("first user's preference leaked to second user")
	}
}

func TestCustomerNotificationsAreIsolatedAndReadable(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	first, _, err := svc.RegisterWorkspaceUser(ctx, "inbox-first@example.test", "long-password", "First", false)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := svc.RegisterWorkspaceUser(ctx, "inbox-second@example.test", "long-password", "Second", false)
	if err != nil {
		t.Fatal(err)
	}

	for index := 0; index < 2; index++ {
		if err := svc.publishCustomerNotification(ctx, customerNotificationInput{
			UserID: first.ID, EventType: CustomerNotificationAnnouncement, Title: "维护通知",
			Content: "服务维护窗口", DedupeKey: "announcement:test:" + string(rune('a'+index)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	firstList, err := svc.CustomerNotifications(ctx, first.Email, CustomerNotificationQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if firstList.Total != 2 || firstList.Unread != 2 || len(firstList.Items) != 2 {
		t.Fatalf("first list = %+v", firstList)
	}
	secondList, err := svc.CustomerNotifications(ctx, second.Email, CustomerNotificationQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if secondList.Total != 0 || secondList.Unread != 0 {
		t.Fatalf("notification leaked to second user: %+v", secondList)
	}
	if err := svc.MarkCustomerNotificationRead(ctx, second.Email, firstList.Items[0].ID); err == nil {
		t.Fatal("second user marked first user's notification")
	}
	if err := svc.MarkCustomerNotificationRead(ctx, first.Email, firstList.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.MarkAllCustomerNotificationsRead(ctx, first.Email)
	if err != nil || updated != 1 {
		t.Fatalf("mark all updated=%d err=%v", updated, err)
	}
	firstList, err = svc.CustomerNotifications(ctx, first.Email, CustomerNotificationQuery{Limit: 20})
	if err != nil || firstList.Unread != 0 {
		t.Fatalf("unread after mark all = %d err=%v", firstList.Unread, err)
	}
}

func TestCustomerBillingAndUsageCreateDeduplicatedNotifications(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	user, _, err := svc.RegisterWorkspaceUser(ctx, "events@example.test", "long-password", "Events", false)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := svc.RegisterWorkspaceUser(ctx, "events-other@example.test", "long-password", "Other", false)
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateAPIKey(ctx, user.Email, APIKeyCreateRequest{
		Name: "Customer events", KeyType: APIKeyTypeUser, OwnerUserID: user.ID, ModelAllowlist: []string{"model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := GatewayAuthContext{APIKey: created.Record}
	for index := 0; index < 6; index++ {
		if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "model", Status: "error", ErrorType: "upstream_5xx"}); err != nil {
			t.Fatal(err)
		}
	}

	list, err := svc.CustomerNotifications(ctx, user.Email, CustomerNotificationQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 2 {
		t.Fatalf("usage notifications total=%d items=%+v", list.Total, list.Items)
	}
	if countNotificationsByType(list.Items, CustomerNotificationBalanceLow) != 1 || countNotificationsByType(list.Items, CustomerNotificationErrorRate) != 1 {
		t.Fatalf("usage notification types = %+v", list.Items)
	}

	code := "ASTER-NOTIFY-500"
	if err := repo.SaveCustomerRedemptionCode(ctx, CustomerRedemptionCode{
		ID: "crc_notify", CodeHash: hashCustomerRedemptionCode(code), Title: "通知测试",
		AmountCents: 500, Status: CustomerRedemptionCodeActive, MaxRedemptions: 1, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RedeemCustomerCode(ctx, user.Email, CustomerRedeemRequest{Code: code}); err != nil {
		t.Fatal(err)
	}
	list, err = svc.CustomerNotifications(ctx, user.Email, CustomerNotificationQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if countNotificationsByType(list.Items, CustomerNotificationPayment) != 1 {
		t.Fatalf("payment notification missing: %+v", list.Items)
	}
	otherList, err := svc.CustomerNotifications(ctx, other.Email, CustomerNotificationQuery{Limit: 20})
	if err != nil || otherList.Total != 0 {
		t.Fatalf("event notifications leaked to other user: %+v err=%v", otherList, err)
	}
}

func TestCustomerMonthlyBillAndBroadcastNotifications(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	user, _, err := svc.RegisterWorkspaceUser(ctx, "digest@example.test", "long-password", "Digest", false)
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateAPIKey(ctx, user.Email, APIKeyCreateRequest{
		Name: "Digest key", KeyType: APIKeyTypeUser, OwnerUserID: user.ID, ModelAllowlist: []string{"model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveUsageRecord(ctx, UsageRecord{
		ID: "usage_digest", APIKeyID: created.Record.ID, Model: "model", Status: "forwarded",
		InputTokens: 80, OutputTokens: 20, CostCents: 45, CreatedAt: time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
	if err := svc.PublishDueCustomerMonthlyBills(ctx, now); err != nil {
		t.Fatal(err)
	}
	if err := svc.PublishDueCustomerMonthlyBills(ctx, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := svc.PublishCustomerBroadcast(ctx, CustomerNotificationAnnouncement, "服务公告", "维护窗口", "/customer/notifications", "notice-one"); err != nil {
		t.Fatal(err)
	}
	if err := svc.PublishCustomerBroadcast(ctx, CustomerNotificationMarketing, "营销活动", "默认不应送达", "/customer/notifications", "marketing-one"); err != nil {
		t.Fatal(err)
	}
	list, err := svc.CustomerNotifications(ctx, user.Email, CustomerNotificationQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if countNotificationsByType(list.Items, CustomerNotificationMonthlyBill) != 1 {
		t.Fatalf("monthly bill was not deduplicated: %+v", list.Items)
	}
	if countNotificationsByType(list.Items, CustomerNotificationAnnouncement) != 1 {
		t.Fatalf("announcement missing: %+v", list.Items)
	}
	if countNotificationsByType(list.Items, CustomerNotificationMarketing) != 0 {
		t.Fatalf("disabled marketing notification was delivered: %+v", list.Items)
	}
}

func notificationPreferenceByType(t *testing.T, preferences []CustomerNotificationPreference, eventType string) CustomerNotificationPreference {
	t.Helper()
	for _, preference := range preferences {
		if preference.EventType == eventType {
			return preference
		}
	}
	t.Fatalf("preference %s not found", eventType)
	return CustomerNotificationPreference{}
}

func replaceNotificationPreference(preferences []CustomerNotificationPreference, replacement CustomerNotificationPreference) {
	for index := range preferences {
		if preferences[index].EventType == replacement.EventType {
			preferences[index] = replacement
			return
		}
	}
}

func cloneNotificationPreferences(input []CustomerNotificationPreference) []CustomerNotificationPreference {
	out := make([]CustomerNotificationPreference, len(input))
	for index, preference := range input {
		out[index] = preference
		out[index].Channels = append([]string(nil), preference.Channels...)
		if preference.Threshold != nil {
			value := *preference.Threshold
			out[index].Threshold = &value
		}
	}
	return out
}

func countNotificationsByType(items []CustomerNotification, eventType string) int {
	total := 0
	for _, item := range items {
		if item.EventType == eventType {
			total++
		}
	}
	return total
}
