package controlplane

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	customerErrorRateWindow      = 5 * time.Minute
	customerErrorRateMinRequests = 5
)

var ErrCustomerNotificationNotFound = errors.New("通知不存在")

func defaultCustomerNotificationPreferences() []CustomerNotificationPreference {
	balanceThreshold := 10.0
	errorThreshold := 20.0
	return []CustomerNotificationPreference{
		{EventType: CustomerNotificationBalanceLow, Enabled: true, Channels: []string{CustomerNotificationChannelInApp, CustomerNotificationChannelEmail}, Threshold: &balanceThreshold},
		{EventType: CustomerNotificationErrorRate, Enabled: true, Channels: []string{CustomerNotificationChannelInApp, CustomerNotificationChannelEmail}, Threshold: &errorThreshold},
		{EventType: CustomerNotificationPayment, Enabled: true, Channels: []string{CustomerNotificationChannelInApp, CustomerNotificationChannelEmail}},
		{EventType: CustomerNotificationMonthlyBill, Enabled: true, Channels: []string{CustomerNotificationChannelInApp, CustomerNotificationChannelEmail}},
		{EventType: CustomerNotificationAnnouncement, Enabled: true, Channels: []string{CustomerNotificationChannelInApp}},
		{EventType: CustomerNotificationModelUpdate, Enabled: true, Channels: []string{CustomerNotificationChannelInApp}},
		{EventType: CustomerNotificationAccountSecurity, Enabled: true, Channels: []string{CustomerNotificationChannelInApp, CustomerNotificationChannelEmail}},
		{EventType: CustomerNotificationMarketing, Enabled: false, Channels: []string{CustomerNotificationChannelInApp, CustomerNotificationChannelEmail}, Marketing: true},
		{EventType: CustomerNotificationProductUpdate, Enabled: false, Channels: []string{CustomerNotificationChannelInApp, CustomerNotificationChannelEmail}, Marketing: true},
	}
}

func (s *Service) CustomerNotificationSettings(ctx context.Context, actor string) (CustomerNotificationSettings, error) {
	user, err := s.customerWorkspaceUser(ctx, actor)
	if err != nil {
		return CustomerNotificationSettings{}, err
	}
	preferences, err := s.customerNotificationPreferencesForUser(ctx, user.ID)
	if err != nil {
		return CustomerNotificationSettings{}, err
	}
	return CustomerNotificationSettings{Preferences: preferences}, nil
}

func (s *Service) UpdateCustomerNotificationSettings(ctx context.Context, actor string, request CustomerNotificationSettingsRequest) (CustomerNotificationSettings, error) {
	user, err := s.customerWorkspaceUser(ctx, actor)
	if err != nil {
		return CustomerNotificationSettings{}, err
	}
	preferences, err := normalizeCustomerNotificationPreferences(request.Preferences)
	if err != nil {
		return CustomerNotificationSettings{}, err
	}
	now := time.Now().UTC()
	if err := s.repo.SaveCustomerNotificationPreferences(ctx, user.ID, preferences, now); err != nil {
		return CustomerNotificationSettings{}, err
	}
	for index := range preferences {
		preferences[index].UpdatedAt = timePointer(now)
	}
	return CustomerNotificationSettings{Preferences: preferences}, nil
}

func (s *Service) CustomerNotifications(ctx context.Context, actor string, query CustomerNotificationQuery) (CustomerNotificationList, error) {
	user, err := s.customerWorkspaceUser(ctx, actor)
	if err != nil {
		return CustomerNotificationList{}, err
	}
	query.UserID = user.ID
	query.Limit, query.Offset = normalizeListWindow(query.Limit, query.Offset, 20, 100)
	items, total, unread, err := s.repo.ListCustomerNotifications(ctx, query)
	if err != nil {
		return CustomerNotificationList{}, err
	}
	return CustomerNotificationList{Items: items, Total: total, Unread: unread, Limit: query.Limit, Offset: query.Offset}, nil
}

func (s *Service) MarkCustomerNotificationRead(ctx context.Context, actor, id string) error {
	user, err := s.customerWorkspaceUser(ctx, actor)
	if err != nil {
		return err
	}
	found, err := s.repo.MarkCustomerNotificationRead(ctx, user.ID, strings.TrimSpace(id), time.Now().UTC())
	if err != nil {
		return err
	}
	if !found {
		return ErrCustomerNotificationNotFound
	}
	return nil
}

func (s *Service) MarkAllCustomerNotificationsRead(ctx context.Context, actor string) (int, error) {
	user, err := s.customerWorkspaceUser(ctx, actor)
	if err != nil {
		return 0, err
	}
	return s.repo.MarkAllCustomerNotificationsRead(ctx, user.ID, time.Now().UTC())
}

func (s *Service) customerNotificationPreferencesForUser(ctx context.Context, userID string) ([]CustomerNotificationPreference, error) {
	stored, err := s.repo.GetCustomerNotificationPreferences(ctx, userID)
	if err != nil {
		return nil, err
	}
	byType := make(map[string]CustomerNotificationPreference, len(stored))
	for _, preference := range stored {
		byType[preference.EventType] = preference
	}
	preferences := defaultCustomerNotificationPreferences()
	for index, fallback := range preferences {
		if preference, ok := byType[fallback.EventType]; ok {
			preference.Marketing = fallback.Marketing
			preference.Channels = append([]string(nil), preference.Channels...)
			preferences[index] = preference
		}
	}
	return preferences, nil
}

func normalizeCustomerNotificationPreferences(input []CustomerNotificationPreference) ([]CustomerNotificationPreference, error) {
	if len(input) == 0 {
		return nil, errors.New("通知设置不能为空")
	}
	defaults := defaultCustomerNotificationPreferences()
	allowed := make(map[string]CustomerNotificationPreference, len(defaults))
	for _, preference := range defaults {
		allowed[preference.EventType] = preference
	}
	provided := make(map[string]CustomerNotificationPreference, len(input))
	for _, preference := range input {
		preference.EventType = strings.TrimSpace(preference.EventType)
		fallback, ok := allowed[preference.EventType]
		if !ok {
			return nil, fmt.Errorf("不支持的通知类型：%s", preference.EventType)
		}
		if _, duplicate := provided[preference.EventType]; duplicate {
			return nil, fmt.Errorf("通知类型重复：%s", preference.EventType)
		}
		channels, err := normalizeCustomerNotificationChannels(preference.Channels)
		if err != nil {
			return nil, fmt.Errorf("%s：%w", preference.EventType, err)
		}
		if preference.Enabled && len(channels) == 0 {
			return nil, fmt.Errorf("%s：开启通知时至少选择一个渠道", preference.EventType)
		}
		preference.Channels = channels
		preference.Marketing = fallback.Marketing
		switch preference.EventType {
		case CustomerNotificationBalanceLow:
			if preference.Threshold == nil || math.IsNaN(*preference.Threshold) || *preference.Threshold < 0 || *preference.Threshold > 1000000 {
				return nil, errors.New("余额阈值必须在 0 到 1000000 元之间")
			}
		case CustomerNotificationErrorRate:
			if preference.Threshold == nil || math.IsNaN(*preference.Threshold) || *preference.Threshold < 1 || *preference.Threshold > 100 {
				return nil, errors.New("错误率阈值必须在 1% 到 100% 之间")
			}
		default:
			preference.Threshold = nil
		}
		provided[preference.EventType] = preference
	}
	result := make([]CustomerNotificationPreference, 0, len(defaults))
	for _, fallback := range defaults {
		preference, ok := provided[fallback.EventType]
		if !ok {
			preference = fallback
		}
		result = append(result, preference)
	}
	return result, nil
}

func normalizeCustomerNotificationChannels(input []string) ([]string, error) {
	seen := map[string]bool{}
	for _, channel := range input {
		channel = strings.ToLower(strings.TrimSpace(channel))
		if channel != CustomerNotificationChannelInApp && channel != CustomerNotificationChannelEmail {
			return nil, fmt.Errorf("不支持的通知渠道：%s", channel)
		}
		seen[channel] = true
	}
	channels := make([]string, 0, 2)
	for _, channel := range []string{CustomerNotificationChannelInApp, CustomerNotificationChannelEmail} {
		if seen[channel] {
			channels = append(channels, channel)
		}
	}
	return channels, nil
}

func (s *Service) publishCustomerNotification(ctx context.Context, input customerNotificationInput) error {
	user, err := s.workspaceUserByID(ctx, input.UserID)
	if err != nil || user.Status != WorkspaceUserStatusActive {
		return err
	}
	preferences, err := s.customerNotificationPreferencesForUser(ctx, user.ID)
	if err != nil {
		return err
	}
	var preference CustomerNotificationPreference
	for _, candidate := range preferences {
		if candidate.EventType == input.EventType {
			preference = candidate
			break
		}
	}
	if !preference.Enabled || len(preference.Channels) == 0 {
		return nil
	}
	now := time.Now().UTC()
	notification := CustomerNotification{
		ID: "cnt_" + randomID(12), UserID: user.ID, EventType: input.EventType,
		Title: strings.TrimSpace(input.Title), Content: strings.TrimSpace(input.Content), Link: strings.TrimSpace(input.Link),
		DedupeKey: strings.TrimSpace(input.DedupeKey), VisibleInApp: contains(preference.Channels, CustomerNotificationChannelInApp), CreatedAt: now,
	}
	created, err := s.repo.CreateCustomerNotification(ctx, notification)
	if err != nil || !created {
		return err
	}
	if notification.VisibleInApp {
		_ = s.repo.SaveCustomerNotificationDelivery(ctx, CustomerNotificationDelivery{
			ID: "cnd_" + randomID(12), NotificationID: notification.ID, UserID: user.ID, EventType: input.EventType,
			Channel: CustomerNotificationChannelInApp, Status: CustomerNotificationDeliverySent, CreatedAt: now, UpdatedAt: now,
		})
	}
	if contains(preference.Channels, CustomerNotificationChannelEmail) {
		delivery := CustomerNotificationDelivery{
			ID: "cnd_" + randomID(12), NotificationID: notification.ID, UserID: user.ID, EventType: input.EventType,
			Channel: CustomerNotificationChannelEmail, Status: CustomerNotificationDeliveryPending, CreatedAt: now, UpdatedAt: now,
		}
		_ = s.repo.SaveCustomerNotificationDelivery(ctx, delivery)
		if s.customerNotificationDispatcher == nil {
			delivery.Status = CustomerNotificationDeliveryFailed
			delivery.Error = "email dispatcher is not configured"
		} else if dispatchErr := s.customerNotificationDispatcher.DispatchCustomerNotification(ctx, user, notification); dispatchErr != nil {
			delivery.Status = CustomerNotificationDeliveryFailed
			delivery.Error = dispatchErr.Error()
		} else {
			delivery.Status = CustomerNotificationDeliverySent
		}
		delivery.UpdatedAt = time.Now().UTC()
		_ = s.repo.SaveCustomerNotificationDelivery(ctx, delivery)
	}
	return nil
}

func (s *Service) syncCustomerUsageNotifications(ctx context.Context, auth GatewayAuthContext, record UsageRecord) error {
	userID := strings.TrimSpace(auth.APIKey.OwnerUserID)
	if userID == "" {
		return nil
	}
	user, err := s.workspaceUserByID(ctx, userID)
	if err != nil || user.Status != WorkspaceUserStatusActive {
		return err
	}
	preferences, err := s.customerNotificationPreferencesForUser(ctx, user.ID)
	if err != nil {
		return err
	}
	byType := make(map[string]CustomerNotificationPreference, len(preferences))
	for _, preference := range preferences {
		byType[preference.EventType] = preference
	}
	now := record.CreatedAt
	if preference := byType[CustomerNotificationBalanceLow]; preference.Enabled && preference.Threshold != nil && float64(user.BalanceMicros) < *preference.Threshold*1_000_000 {
		_ = s.publishCustomerNotification(ctx, customerNotificationInput{
			UserID: user.ID, EventType: CustomerNotificationBalanceLow, Title: "账户余额不足",
			Content: fmt.Sprintf("当前余额 US$%.6f，已低于您设置的 US$%.6f 阈值，请及时充值以免调用中断。", float64(user.BalanceMicros)/1_000_000, *preference.Threshold),
			Link:    "/customer/billing", DedupeKey: "balance_low:" + user.ID + ":" + now.Format("2006-01-02"),
		})
	}
	preference := byType[CustomerNotificationErrorRate]
	if !preference.Enabled || preference.Threshold == nil {
		return nil
	}
	aggregate, err := s.repo.SummarizeUsageRecords(ctx, UsageQuery{APIKeyID: auth.APIKey.ID, CreatedFrom: now.Add(-customerErrorRateWindow)})
	if err != nil || aggregate.TotalRequests < customerErrorRateMinRequests {
		return err
	}
	errorRate := float64(aggregate.ErrorRequests) * 100 / float64(aggregate.TotalRequests)
	if errorRate < *preference.Threshold {
		return nil
	}
	bucket := now.Truncate(30 * time.Minute).Format("20060102T1504")
	return s.publishCustomerNotification(ctx, customerNotificationInput{
		UserID: user.ID, EventType: CustomerNotificationErrorRate, Title: "异常调用告警",
		Content: fmt.Sprintf("API Key %s 最近 5 分钟错误率为 %.1f%%（%d/%d），已超过您设置的 %.1f%% 阈值。", auth.APIKey.Name, errorRate, aggregate.ErrorRequests, aggregate.TotalRequests, *preference.Threshold),
		Link:    "/customer/usage", DedupeKey: "abuse_5xx:" + user.ID + ":" + auth.APIKey.ID + ":" + bucket,
	})
}

func (s *Service) publishAccountSecurityNotification(ctx context.Context, user WorkspaceUser, title, content, action string) error {
	return s.publishCustomerNotification(ctx, customerNotificationInput{
		UserID: user.ID, EventType: CustomerNotificationAccountSecurity, Title: title, Content: content,
		Link: "/customer/account", DedupeKey: "account_security:" + action + ":" + user.ID + ":" + user.UpdatedAt.Format(time.RFC3339Nano),
	})
}

func (s *Service) PublishCustomerBroadcast(ctx context.Context, eventType, title, content, link, dedupeKey string) error {
	switch eventType {
	case CustomerNotificationAnnouncement, CustomerNotificationModelUpdate, CustomerNotificationMarketing, CustomerNotificationProductUpdate:
	default:
		return fmt.Errorf("unsupported customer broadcast type: %s", eventType)
	}
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, user := range users {
		if user.Status != WorkspaceUserStatusActive {
			continue
		}
		if err := s.publishCustomerNotification(ctx, customerNotificationInput{
			UserID: user.ID, EventType: eventType, Title: title, Content: content, Link: link,
			DedupeKey: eventType + ":" + dedupeKey,
		}); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Service) PublishDueCustomerMonthlyBills(ctx context.Context, now time.Time) error {
	now = now.UTC()
	if now.Day() != 1 {
		return nil
	}
	periodEnd := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodStart := periodEnd.AddDate(0, -1, 0)
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return err
	}
	keys, err := s.repo.ListAPIKeys(ctx)
	if err != nil {
		return err
	}
	keyIDsByUser := map[string][]string{}
	for _, key := range keys {
		if key.OwnerUserID != "" {
			keyIDsByUser[key.OwnerUserID] = append(keyIDsByUser[key.OwnerUserID], key.ID)
		}
	}
	var firstErr error
	for _, user := range users {
		if user.Status != WorkspaceUserStatusActive {
			continue
		}
		aggregate := UsageAggregate{}
		if keyIDs := keyIDsByUser[user.ID]; len(keyIDs) > 0 {
			aggregate, err = s.repo.SummarizeUsageRecords(ctx, UsageQuery{APIKeyIDs: keyIDs, CreatedFrom: periodStart, CreatedTo: periodEnd.Add(-time.Nanosecond)})
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
		if err := s.publishCustomerNotification(ctx, customerNotificationInput{
			UserID: user.ID, EventType: CustomerNotificationMonthlyBill, Title: periodStart.Format("2006 年 01 月") + "账单摘要",
			Content: fmt.Sprintf("上月共调用 %d 次，使用 %d Token，费用 US$%.6f。", aggregate.TotalRequests, aggregate.TotalTokens, float64(aggregate.TotalUsageCostMicros)/1_000_000),
			Link:    "/customer/billing", DedupeKey: "monthly_bill:" + user.ID + ":" + periodStart.Format("2006-01"),
		}); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Service) RunCustomerNotificationScheduler(ctx context.Context, onError func(error)) {
	run := func() {
		if err := s.PublishDueCustomerMonthlyBills(ctx, time.Now().UTC()); err != nil && onError != nil {
			onError(err)
		}
	}
	run()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
