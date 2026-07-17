package controlplane

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	apiKeyQuotaWarningPercent       = 80
	gatewayErrorRateWindow          = 15 * time.Minute
	gatewayErrorRateMinRequests     = 10
	gatewayErrorRateWarningPercent  = 20
	gatewayErrorRateCriticalPercent = 50
)

type alertInput struct {
	Type                      string
	Severity                  string
	Title                     string
	Summary                   string
	ResourceType              string
	ResourceID                string
	DedupeKey                 string
	Metadata                  map[string]string
	ObservedAt                time.Time
	ProfileScope              string
	PlatformTenantID          string
	PlatformTenantName        string
	GatewayPrincipalID        string
	GatewayPrincipalName      string
	ExternalAuthIntegrationID string
	ExternalSubjectReference  string
}

func (s *Service) ListAlertEventsQuery(ctx context.Context, query AlertQuery) ([]AlertEvent, error) {
	return s.repo.QueryAlertEvents(ctx, query)
}

func (s *Service) AlertSummaryQuery(ctx context.Context, query AlertQuery) (AlertSummary, error) {
	return s.repo.SummarizeAlertEvents(ctx, query)
}

func (s *Service) AlertEventByID(ctx context.Context, id string) (AlertEvent, error) {
	return s.alertByID(ctx, id)
}

func (s *Service) RecordRiskRuleAlert(ctx context.Context, apiKeyID, ruleID, ruleName, summary string, value, threshold float64) error {
	input := alertInput{
		Type: AlertTypeRiskRule, Severity: AlertSeverityWarning,
		Title: "Gateway risk rule requires review", Summary: summary,
		ResourceType: "api_key", ResourceID: apiKeyID,
		DedupeKey: "risk_rule:" + strings.TrimSpace(ruleID) + ":" + strings.TrimSpace(apiKeyID),
		Metadata:  map[string]string{"rule_id": ruleID, "rule_name": ruleName, "value": fmt.Sprintf("%.2f", value), "threshold": fmt.Sprintf("%.2f", threshold)},
	}
	s.applyPlatformAlertIdentityForKey(ctx, &input, apiKeyID)
	return s.upsertAlert(ctx, input)
}

func (s *Service) AcknowledgeAlert(ctx context.Context, actor string, id string) (AlertEvent, error) {
	event, err := s.alertByID(ctx, id)
	if err != nil {
		return AlertEvent{}, err
	}
	if event.Status == AlertStatusResolved {
		return AlertEvent{}, errors.New("resolved alert cannot be acknowledged")
	}
	now := s.nowUTC()
	event.Status = AlertStatusAcknowledged
	event.AcknowledgedAt = &now
	event.AcknowledgedBy = normalizeActor(actor)
	event.LastSeenAt = now
	if err := s.repo.SaveAlertEvent(ctx, event); err != nil {
		return AlertEvent{}, err
	}
	if err := s.audit(ctx, actor, "acknowledge", "alert_event", event.ID, fmt.Sprintf("Acknowledged alert %s", event.Title)); err != nil {
		return AlertEvent{}, err
	}
	return event, nil
}

func (s *Service) ResolveAlert(ctx context.Context, actor string, id string) (AlertEvent, error) {
	event, err := s.alertByID(ctx, id)
	if err != nil {
		return AlertEvent{}, err
	}
	now := s.nowUTC()
	event.Status = AlertStatusResolved
	event.ResolvedAt = &now
	event.ResolvedBy = normalizeActor(actor)
	event.LastSeenAt = now
	if err := s.repo.SaveAlertEvent(ctx, event); err != nil {
		return AlertEvent{}, err
	}
	if err := s.audit(ctx, actor, "resolve", "alert_event", event.ID, fmt.Sprintf("Resolved alert %s", event.Title)); err != nil {
		return AlertEvent{}, err
	}
	return event, nil
}

func (s *Service) upsertAlert(ctx context.Context, input alertInput) error {
	input.DedupeKey = strings.TrimSpace(input.DedupeKey)
	if input.DedupeKey == "" {
		return errors.New("alert dedupe key is required")
	}
	now := input.ObservedAt.UTC()
	if now.IsZero() {
		now = s.nowUTC()
	}
	event, ok, err := s.repo.FindAlertByDedupeKey(ctx, input.DedupeKey)
	if err != nil {
		return err
	}
	if !ok {
		event = AlertEvent{
			ID:          "alert_" + randomID(12),
			Status:      AlertStatusActive,
			DedupeKey:   input.DedupeKey,
			FirstSeenAt: now,
		}
	}
	previousStatus := event.Status
	previousSeverity := event.Severity
	if event.Status == AlertStatusResolved {
		event.Status = AlertStatusActive
		event.AcknowledgedAt = nil
		event.AcknowledgedBy = ""
		event.ResolvedAt = nil
		event.ResolvedBy = ""
	}
	event.Type = normalizeAlertType(input.Type)
	event.Severity = normalizeAlertSeverity(input.Severity)
	event.Title = strings.TrimSpace(input.Title)
	event.Summary = strings.TrimSpace(input.Summary)
	event.ResourceType = strings.TrimSpace(input.ResourceType)
	event.ResourceID = strings.TrimSpace(input.ResourceID)
	event.ProfileScope = strings.TrimSpace(input.ProfileScope)
	event.PlatformTenantID = strings.TrimSpace(input.PlatformTenantID)
	event.PlatformTenantName = strings.TrimSpace(input.PlatformTenantName)
	event.GatewayPrincipalID = strings.TrimSpace(input.GatewayPrincipalID)
	event.GatewayPrincipalName = strings.TrimSpace(input.GatewayPrincipalName)
	event.ExternalAuthIntegrationID = strings.TrimSpace(input.ExternalAuthIntegrationID)
	event.ExternalSubjectReference = strings.TrimSpace(input.ExternalSubjectReference)
	event.Metadata = cloneStringMap(input.Metadata)
	event.LastSeenAt = now
	if event.Title == "" {
		event.Title = event.Type
	}
	if event.Summary == "" {
		event.Summary = event.Title
	}
	if err := s.repo.SaveAlertEvent(ctx, event); err != nil {
		return err
	}
	if !ok || previousStatus == AlertStatusResolved || previousSeverity != event.Severity {
		_ = s.dispatchAlert(ctx, event)
	}
	return nil
}

func (s *Service) resolveAlertByDedupeKey(ctx context.Context, actor string, dedupeKey string, summary string) error {
	event, ok, err := s.repo.FindAlertByDedupeKey(ctx, strings.TrimSpace(dedupeKey))
	if err != nil || !ok {
		return err
	}
	if event.Status == AlertStatusResolved {
		return nil
	}
	now := s.nowUTC()
	event.Status = AlertStatusResolved
	event.ResolvedAt = &now
	event.ResolvedBy = normalizeActor(actor)
	event.LastSeenAt = now
	if strings.TrimSpace(summary) != "" {
		event.Summary = strings.TrimSpace(summary)
	}
	return s.repo.SaveAlertEvent(ctx, event)
}

func (s *Service) syncAPIKeyQuotaAlert(ctx context.Context, auth GatewayAuthContext, usedTokens int, now time.Time) error {
	now = now.UTC()
	dedupeKey := apiKeyQuotaAlertDedupeKey(auth.APIKey.ID, now)
	limit := auth.effectiveMonthlyTokenLimit()
	if limit <= 0 {
		return s.resolveAlertByDedupeKey(ctx, systemActor, dedupeKey, fmt.Sprintf("API key %s has no monthly token quota.", auth.APIKey.Name))
	}
	usedPercent := percentCeil(usedTokens, limit)
	if usedPercent < apiKeyQuotaWarningPercent {
		return s.resolveAlertByDedupeKey(ctx, systemActor, dedupeKey, fmt.Sprintf("API key %s quota usage is within policy.", auth.APIKey.Name))
	}
	severity := AlertSeverityWarning
	title := "API key monthly token quota reached warning threshold"
	if usedTokens >= limit {
		severity = AlertSeverityCritical
		title = "API key monthly token quota exhausted"
	}
	input := alertInput{
		Type:         AlertTypeAPIKeyQuota,
		Severity:     severity,
		Title:        title,
		Summary:      fmt.Sprintf("API key %s used %s of %s monthly tokens (%d%%).", auth.APIKey.Name, formatInt(usedTokens), formatInt(limit), usedPercent),
		ResourceType: "api_key",
		ResourceID:   auth.APIKey.ID,
		DedupeKey:    dedupeKey,
		Metadata: map[string]string{
			"api_key_name":         auth.APIKey.Name,
			"api_key_fingerprint":  auth.APIKey.Fingerprint,
			"monthly_token_limit":  strconv.Itoa(limit),
			"current_month_tokens": strconv.Itoa(usedTokens),
			"quota_used_percent":   strconv.Itoa(usedPercent),
			"quota_month":          alertMonthKey(now),
		},
		ObservedAt: now,
	}
	applyGatewayPlatformSnapshotToAlertInput(&input, auth)
	return s.upsertAlert(ctx, input)
}

func (s *Service) syncAPIKeyQuotaAlertForAuth(ctx context.Context, auth GatewayAuthContext, now time.Time) error {
	if auth.effectiveMonthlyTokenLimit() <= 0 {
		return nil
	}
	used, err := s.repo.SumUsageTokensByAPIKeySince(ctx, auth.APIKey.ID, monthStart(now))
	if err != nil {
		return err
	}
	return s.syncAPIKeyQuotaAlert(ctx, auth, used, now)
}

func (s *Service) syncAPIKeyBudgetAlert(ctx context.Context, auth GatewayAuthContext, usedMicros int64, now time.Time) error {
	now = now.UTC()
	limit := auth.effectiveMonthlyBudgetMicros()
	limitMicros := int64(limit)
	dedupeKey := "api_key_budget:" + auth.APIKey.ID + ":" + alertMonthKey(now)
	if limit <= 0 {
		return s.resolveAlertByDedupeKey(ctx, systemActor, dedupeKey, fmt.Sprintf("API key %s has no monthly budget policy.", auth.APIKey.Name))
	}
	usedPercent := percentCeil64(usedMicros, limitMicros)
	if usedPercent < apiKeyQuotaWarningPercent {
		return s.resolveAlertByDedupeKey(ctx, systemActor, dedupeKey, fmt.Sprintf("API key %s budget usage is within policy.", auth.APIKey.Name))
	}
	severity := AlertSeverityWarning
	title := "API key monthly budget reached warning threshold"
	if usedMicros >= limitMicros {
		severity = AlertSeverityCritical
		title = "API key monthly budget exhausted"
	}
	input := alertInput{Type: AlertTypeAPIKeyBudget, Severity: severity, Title: title, Summary: fmt.Sprintf("API key %s used %d of %d monthly microdollars (%d%%).", auth.APIKey.Name, usedMicros, limitMicros, usedPercent), ResourceType: "api_key", ResourceID: auth.APIKey.ID, DedupeKey: dedupeKey, Metadata: map[string]string{"api_key_name": auth.APIKey.Name, "api_key_fingerprint": auth.APIKey.Fingerprint, "monthly_budget_micros": strconv.FormatInt(limitMicros, 10), "current_month_usage_cost_micros": strconv.FormatInt(usedMicros, 10), "budget_used_percent": strconv.Itoa(usedPercent), "budget_month": alertMonthKey(now)}, ObservedAt: now}
	applyGatewayPlatformSnapshotToAlertInput(&input, auth)
	return s.upsertAlert(ctx, input)
}

func percentCeil64(value, limit int64) int {
	if value <= 0 || limit <= 0 {
		return 0
	}
	if value > math.MaxInt64/100 {
		return 100
	}
	return int((value*100 + limit - 1) / limit)
}

func (s *Service) syncAPIKeyBudgetAlertForAuth(ctx context.Context, auth GatewayAuthContext, now time.Time) error {
	if auth.effectiveMonthlyBudgetMicros() <= 0 {
		return nil
	}
	used, err := s.repo.SumUsageCostMicrosByAPIKeySince(ctx, auth.APIKey.ID, monthStart(now))
	if err != nil {
		return err
	}
	return s.syncAPIKeyBudgetAlert(ctx, auth, used, now)
}

func (s *Service) syncGatewayErrorRateAlert(ctx context.Context, auth GatewayAuthContext) error {
	windowEnd := s.nowUTC()
	windowStart := windowEnd.Add(-gatewayErrorRateWindow)
	dedupeKey := "gateway_error_rate:" + auth.APIKey.ID
	aggregate, err := s.repo.SummarizeUsageRecords(ctx, UsageQuery{
		APIKeyID:    auth.APIKey.ID,
		CreatedFrom: windowStart,
	})
	if err != nil {
		return err
	}
	if aggregate.TotalRequests < gatewayErrorRateMinRequests {
		return s.resolveAlertByDedupeKey(ctx, systemActor, dedupeKey, fmt.Sprintf("Workspace key %s has fewer than %d requests in the rolling error-rate window.", auth.APIKey.Name, gatewayErrorRateMinRequests))
	}
	errorRate := percentCeil(aggregate.ErrorRequests, aggregate.TotalRequests)
	if errorRate < gatewayErrorRateWarningPercent {
		return s.resolveAlertByDedupeKey(ctx, systemActor, dedupeKey, fmt.Sprintf("Workspace key %s gateway error rate recovered to %d%%.", auth.APIKey.Name, errorRate))
	}
	severity := AlertSeverityWarning
	title := "Gateway error rate reached warning threshold"
	if errorRate >= gatewayErrorRateCriticalPercent {
		severity = AlertSeverityCritical
		title = "Gateway error rate is critical"
	}
	input := alertInput{
		Type:         AlertTypeGatewayErrorRate,
		Severity:     severity,
		Title:        title,
		Summary:      fmt.Sprintf("Workspace key %s had %s errors out of %s requests in the last %s (%d%%).", auth.APIKey.Name, formatInt(aggregate.ErrorRequests), formatInt(aggregate.TotalRequests), formatDuration(gatewayErrorRateWindow), errorRate),
		ResourceType: "api_key",
		ResourceID:   auth.APIKey.ID,
		DedupeKey:    dedupeKey,
		Metadata: map[string]string{
			"api_key_id":            auth.APIKey.ID,
			"api_key_name":          auth.APIKey.Name,
			"window_seconds":        strconv.Itoa(int(gatewayErrorRateWindow.Seconds())),
			"window_started_at":     windowStart.Format(time.RFC3339),
			"window_ended_at":       windowEnd.Format(time.RFC3339),
			"total_requests":        strconv.Itoa(aggregate.TotalRequests),
			"error_requests":        strconv.Itoa(aggregate.ErrorRequests),
			"error_rate_percent":    strconv.Itoa(errorRate),
			"warning_threshold":     strconv.Itoa(gatewayErrorRateWarningPercent),
			"critical_threshold":    strconv.Itoa(gatewayErrorRateCriticalPercent),
			"min_request_threshold": strconv.Itoa(gatewayErrorRateMinRequests),
		},
	}
	applyGatewayPlatformSnapshotToAlertInput(&input, auth)
	return s.upsertAlert(ctx, input)
}

func applyGatewayPlatformSnapshotToAlertInput(input *alertInput, auth GatewayAuthContext) {
	if input == nil || auth.APIKey.ProfileScope != ProfileScopePlatform || auth.PlatformTenant == nil || auth.GatewayPrincipal == nil {
		return
	}
	input.ProfileScope = ProfileScopePlatform
	input.PlatformTenantID = auth.PlatformTenant.ID
	input.PlatformTenantName = auth.PlatformTenant.Name
	input.GatewayPrincipalID = auth.GatewayPrincipal.ID
	input.GatewayPrincipalName = auth.GatewayPrincipal.Name
	if auth.ExternalAuthIntegration != nil {
		input.ExternalAuthIntegrationID = auth.ExternalAuthIntegration.ID
		input.ExternalSubjectReference = auth.ExternalSubjectReference
	}
}

func (s *Service) applyPlatformAlertIdentityForKey(ctx context.Context, input *alertInput, apiKeyID string) {
	if input == nil {
		return
	}
	key, err := s.apiKeyByID(ctx, apiKeyID)
	if err != nil || key.ProfileScope != ProfileScopePlatform {
		return
	}
	identity, err := s.platformCredentialIdentity(ctx, key.PlatformTenantID, key.GatewayPrincipalID)
	if err != nil {
		input.ProfileScope = ProfileScopePlatform
		input.PlatformTenantID = key.PlatformTenantID
		input.GatewayPrincipalID = key.GatewayPrincipalID
		return
	}
	input.ProfileScope = ProfileScopePlatform
	input.PlatformTenantID = identity.tenant.ID
	input.PlatformTenantName = identity.tenant.Name
	input.GatewayPrincipalID = identity.principal.ID
	input.GatewayPrincipalName = identity.principal.Name
}

func (s *Service) syncProviderHealthAlert(ctx context.Context, provider ProviderConnection, check ProviderHealthCheck) error {
	dedupeKey := "provider_health:" + provider.ID
	if check.Status != "warning" && check.Status != "error" {
		return s.resolveAlertByDedupeKey(ctx, systemActor, dedupeKey, fmt.Sprintf("Provider %s recovered with status %s.", provider.Name, check.Status))
	}
	severity := AlertSeverityWarning
	if check.Status == "error" {
		severity = AlertSeverityCritical
	}
	return s.upsertAlert(ctx, alertInput{
		Type:         AlertTypeProviderHealth,
		Severity:     severity,
		Title:        fmt.Sprintf("Provider %s health is %s", provider.Name, check.Status),
		Summary:      check.Message,
		ResourceType: "provider",
		ResourceID:   provider.ID,
		DedupeKey:    dedupeKey,
		Metadata: map[string]string{
			"provider_name": provider.Name,
			"provider_type": provider.Type,
			"health_status": check.Status,
			"latency_ms":    strconv.FormatInt(check.LatencyMS, 10),
			"checked_at":    check.CheckedAt.Format(time.RFC3339),
		},
	})
}

func (s *Service) syncProviderAccountHealthAlert(ctx context.Context, account ProviderAccount, provider ProviderConnection, check ProviderAccountHealthCheck) error {
	dedupeKey := "provider_account_health:" + account.ID
	if check.Status != "warning" && check.Status != "error" {
		return s.resolveAlertByDedupeKey(ctx, systemActor, dedupeKey, fmt.Sprintf("Provider account %s recovered with status %s.", account.Name, check.Status))
	}
	severity := AlertSeverityWarning
	if check.Status == "error" {
		severity = AlertSeverityCritical
	}
	return s.upsertAlert(ctx, alertInput{
		Type:         AlertTypeProviderAccountHealth,
		Severity:     severity,
		Title:        fmt.Sprintf("Provider account %s health is %s", account.Name, check.Status),
		Summary:      check.Message,
		ResourceType: "provider_account",
		ResourceID:   account.ID,
		DedupeKey:    dedupeKey,
		Metadata: map[string]string{
			"account_name":  account.Name,
			"provider_id":   provider.ID,
			"provider_name": provider.Name,
			"platform":      account.Platform,
			"health_status": check.Status,
			"latency_ms":    strconv.FormatInt(check.LatencyMS, 10),
			"checked_at":    check.CheckedAt.Format(time.RFC3339),
		},
	})
}

func (s *Service) alertByID(ctx context.Context, id string) (AlertEvent, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return AlertEvent{}, errors.New("alert id is required")
	}
	event, ok, err := s.repo.FindAlertEvent(ctx, id)
	if err != nil {
		return AlertEvent{}, err
	}
	if !ok {
		return AlertEvent{}, fmt.Errorf("alert %q not found", id)
	}
	return event, nil
}

func (s *Service) dispatchAlert(ctx context.Context, event AlertEvent) error {
	if s.alertDispatcher == nil {
		return nil
	}
	return s.alertDispatcher.DispatchAlert(ctx, event)
}

func normalizeAlertType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "system"
	}
	return value
}

func normalizeAlertSeverity(value string) string {
	switch strings.TrimSpace(value) {
	case AlertSeverityCritical:
		return AlertSeverityCritical
	case AlertSeverityInfo:
		return AlertSeverityInfo
	default:
		return AlertSeverityWarning
	}
}

func normalizeActor(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return "local-admin"
	}
	return actor
}

func apiKeyQuotaAlertDedupeKey(apiKeyID string, now time.Time) string {
	return "api_key_quota:" + strings.TrimSpace(apiKeyID) + ":" + alertMonthKey(now)
}

func alertMonthKey(now time.Time) string {
	utc := now.UTC()
	return fmt.Sprintf("%04d-%02d", utc.Year(), utc.Month())
}

func formatInt(value int) string {
	return strconv.FormatInt(int64(value), 10)
}

func formatDuration(value time.Duration) string {
	minutes := int(value.Minutes())
	if minutes > 0 && value%time.Minute == 0 {
		return fmt.Sprintf("%d minutes", minutes)
	}
	return value.String()
}

func percentCeil(part int, total int) int {
	if total <= 0 || part <= 0 {
		return 0
	}
	return int(math.Ceil(float64(part) * 100 / float64(total)))
}

func cloneStringMap(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}
