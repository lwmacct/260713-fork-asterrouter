package operator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

type Service struct {
	repo               Repository
	control            *controlplane.Service
	riskConfigProvider func(context.Context) (RiskRuntimeConfig, error)
}

type RiskRuntimeConfig struct {
	Enabled      bool
	AutoBlock    bool
	BlockTimeout time.Duration
}

func NewService(repo Repository, control *controlplane.Service) *Service {
	return &Service{repo: repo, control: control}
}

func (s *Service) SetRiskConfigProvider(provider func(context.Context) (RiskRuntimeConfig, error)) {
	s.riskConfigProvider = provider
}
func (s *Service) Health(ctx context.Context) error { return s.repo.Health(ctx) }

func (s *Service) Dashboard(ctx context.Context) (Dashboard, error) {
	customers, err := s.repo.ListCustomers(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	plans, err := s.repo.ListPlans(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	risks, err := s.repo.ListRiskRules(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	notices, err := s.repo.ListNotices(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	var out Dashboard
	out.Customers = len(customers)
	out.Plans = len(plans)
	out.RiskRules = len(risks)
	for _, v := range customers {
		out.BalanceMicros += v.BalanceMicros
		if v.Status == StatusActive {
			out.ActiveCustomers++
		}
	}
	for _, v := range notices {
		if v.Status == "published" {
			out.PublishedNotice++
		}
	}
	return out, nil
}

func (s *Service) ListGroups(ctx context.Context) ([]CustomerGroup, error) {
	return s.repo.ListGroups(ctx)
}
func (s *Service) SaveGroup(ctx context.Context, id string, req CustomerGroupRequest) (CustomerGroup, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return CustomerGroup{}, errors.New("name is required")
	}
	status, err := normalizeStatus(req.Status)
	if err != nil {
		return CustomerGroup{}, err
	}
	now := time.Now().UTC()
	v := CustomerGroup{ID: id, Name: name, Description: strings.TrimSpace(req.Description), Status: status, CreatedAt: now, UpdatedAt: now}
	if id == "" {
		v.ID = "ocg_" + randomID()
	} else {
		items, err := s.repo.ListGroups(ctx)
		if err != nil {
			return CustomerGroup{}, err
		}
		old, err := findByID(items, id)
		if err != nil {
			return CustomerGroup{}, err
		}
		v.CreatedAt = old.CreatedAt
	}
	if err := s.repo.SaveGroup(ctx, v); err != nil {
		return CustomerGroup{}, err
	}
	s.audit(ctx, "operator_group", v.ID, "Saved customer group "+v.Name)
	return v, nil
}
func (s *Service) DeleteGroup(ctx context.Context, id string) error {
	customers, err := s.repo.ListCustomers(ctx)
	if err != nil {
		return err
	}
	for _, v := range customers {
		if v.GroupID == id {
			return errors.New("customer group is still assigned to customers")
		}
	}
	if err := s.repo.DeleteGroup(ctx, id); err != nil {
		return err
	}
	s.audit(ctx, "operator_group", id, "Deleted customer group")
	return nil
}

func (s *Service) ListCustomers(ctx context.Context) ([]Customer, error) {
	return s.repo.ListCustomers(ctx)
}
func (s *Service) SaveCustomer(ctx context.Context, id string, req CustomerRequest) (Customer, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return Customer{}, errors.New("name is required")
	}
	status, err := normalizeStatus(req.Status)
	if err != nil {
		return Customer{}, err
	}
	if req.CreditMicros < 0 {
		return Customer{}, errors.New("credit_micros must be greater than or equal to 0")
	}
	if req.GroupID != "" {
		items, err := s.repo.ListGroups(ctx)
		if err != nil {
			return Customer{}, err
		}
		if _, err := findByID(items, req.GroupID); err != nil {
			return Customer{}, err
		}
	}
	if req.PlanID != "" {
		items, err := s.repo.ListPlans(ctx)
		if err != nil {
			return Customer{}, err
		}
		if _, err := findByID(items, req.PlanID); err != nil {
			return Customer{}, err
		}
	}
	now := time.Now().UTC()
	v := Customer{ID: id, Name: name, Email: strings.TrimSpace(req.Email), GroupID: strings.TrimSpace(req.GroupID), PlanID: strings.TrimSpace(req.PlanID), Status: status, CreditMicros: req.CreditMicros, Notes: strings.TrimSpace(req.Notes), CreatedAt: now, UpdatedAt: now}
	if id == "" {
		v.ID = "cust_" + randomID()
	} else {
		items, err := s.repo.ListCustomers(ctx)
		if err != nil {
			return Customer{}, err
		}
		old, err := findByID(items, id)
		if err != nil {
			return Customer{}, err
		}
		v.CreatedAt = old.CreatedAt
		v.BalanceMicros = old.BalanceMicros
	}
	if err := s.repo.SaveCustomer(ctx, v); err != nil {
		return Customer{}, err
	}
	s.audit(ctx, "operator_customer", v.ID, "Saved customer "+v.Name)
	return v, nil
}
func (s *Service) DeleteCustomer(ctx context.Context, id string) error {
	keys, err := s.control.ListAPIKeys(ctx)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if key.CustomerID == id {
			return errors.New("customer still owns workspace keys")
		}
	}
	if err := s.repo.DeleteCustomer(ctx, id); err != nil {
		return err
	}
	s.audit(ctx, "operator_customer", id, "Deleted customer")
	return nil
}

func (s *Service) CreateCustomerKey(ctx context.Context, actor, customerID string, req controlplane.APIKeyCreateRequest) (controlplane.APIKeyCreateResponse, error) {
	customers, err := s.repo.ListCustomers(ctx)
	if err != nil {
		return controlplane.APIKeyCreateResponse{}, err
	}
	customer, err := findByID(customers, customerID)
	if err != nil {
		return controlplane.APIKeyCreateResponse{}, err
	}
	if customer.Status != StatusActive {
		return controlplane.APIKeyCreateResponse{}, errors.New("customer is not active")
	}
	req.KeyType = controlplane.APIKeyTypeCustomer
	req.CustomerID = customer.ID
	return s.control.CreateAPIKey(ctx, actor, req)
}
func (s *Service) ListCustomerKeys(ctx context.Context, customerID ...string) ([]controlplane.APIKeyRecord, error) {
	keys, err := s.control.ListAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	out := []controlplane.APIKeyRecord{}
	for _, key := range keys {
		if key.KeyType == controlplane.APIKeyTypeCustomer && (len(customerID) == 0 || key.CustomerID == customerID[0]) {
			out = append(out, key)
		}
	}
	return out, nil
}

func (s *Service) RotateCustomerKey(ctx context.Context, actor, id string) (controlplane.APIKeyCreateResponse, error) {
	return s.RotateCustomerKeyWithGrace(ctx, actor, id, 0)
}

func (s *Service) RotateCustomerKeyWithGrace(ctx context.Context, actor, id string, gracePeriodSeconds int) (controlplane.APIKeyCreateResponse, error) {
	key, err := s.customerKey(ctx, id)
	if err != nil {
		return controlplane.APIKeyCreateResponse{}, err
	}
	return s.control.RotateAPIKeyWithGrace(ctx, actor, key.ID, gracePeriodSeconds)
}

func (s *Service) DisableCustomerKey(ctx context.Context, actor, id string) error {
	key, err := s.customerKey(ctx, id)
	if err != nil {
		return err
	}
	return s.control.DisableAPIKey(ctx, actor, key.ID)
}

func (s *Service) customerKey(ctx context.Context, id string) (controlplane.APIKeyRecord, error) {
	keys, err := s.ListCustomerKeys(ctx)
	if err != nil {
		return controlplane.APIKeyRecord{}, err
	}
	for _, key := range keys {
		if key.ID == id {
			return key, nil
		}
	}
	return controlplane.APIKeyRecord{}, fmt.Errorf("customer key %q not found", id)
}

func (s *Service) Usage(ctx context.Context, query controlplane.UsageQuery) (controlplane.UsageReport, error) {
	return s.control.UsageReportQuery(ctx, query)
}

// EvaluateUsageRisk evaluates durable Usage events without calculating prices or writing balances.
func (s *Service) EvaluateUsageRisk(ctx context.Context, record controlplane.UsageRecord) error {
	if err := s.evaluateRiskRules(ctx, record); err != nil {
		return err
	}
	return nil
}

func (s *Service) evaluateRiskRules(ctx context.Context, record controlplane.UsageRecord) error {
	if s.riskConfigProvider == nil || s.control == nil || record.APIKeyID == "" {
		return nil
	}
	config, err := s.riskConfigProvider(ctx)
	if err != nil || !config.Enabled {
		return err
	}
	rules, err := s.repo.ListRiskRules(ctx)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if rule.Status != StatusActive || rule.Threshold <= 0 || rule.WindowMins <= 0 {
			continue
		}
		report, err := s.control.UsageReportQuery(ctx, controlplane.UsageQuery{APIKeyID: record.APIKeyID, CreatedFrom: time.Now().UTC().Add(-time.Duration(rule.WindowMins) * time.Minute)})
		if err != nil {
			return err
		}
		value, supported := riskRuleValue(rule.RuleType, rule.WindowMins, report)
		if rule.RuleType == "error_rate" && report.TotalRequests < 10 {
			continue
		}
		if supported && value >= rule.Threshold {
			reason := fmt.Sprintf("%s reached %.2f (threshold %.2f)", rule.RuleType, value, rule.Threshold)
			if rule.Action == "review" {
				if err := s.control.RecordRiskRuleAlert(ctx, record.APIKeyID, rule.ID, rule.Name, reason, value, rule.Threshold); err != nil {
					return err
				}
				continue
			}
			if rule.Action == "block" && config.AutoBlock && config.BlockTimeout > 0 {
				if err := s.control.BlockAPIKey(ctx, record.APIKeyID, rule.ID, reason, time.Now().UTC().Add(config.BlockTimeout)); err != nil {
					return err
				}
				s.audit(ctx, "risk_block", record.APIKeyID, reason)
				return nil
			}
		}
	}
	return nil
}

func riskRuleValue(ruleType string, windowMins int, report controlplane.UsageReport) (float64, bool) {
	switch strings.TrimSpace(ruleType) {
	case "rpm":
		return float64(report.TotalRequests) / float64(windowMins), true
	case "tokens":
		return float64(report.TotalTokens), true
	case "spend":
		return float64(report.TotalUsageCostMicros), true
	case "error_rate":
		if report.TotalRequests == 0 {
			return 0, true
		}
		return float64(report.ErrorRequests) * 100 / float64(report.TotalRequests), true
	default:
		return 0, false
	}
}

func maxInt(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}

func (s *Service) ListPlans(ctx context.Context) ([]Plan, error) { return s.repo.ListPlans(ctx) }
func (s *Service) SavePlan(ctx context.Context, id string, req PlanRequest) (Plan, error) {
	if strings.TrimSpace(req.Name) == "" {
		return Plan{}, errors.New("name is required")
	}
	if req.MonthlyFeeMicros < 0 || req.IncludedTokens < 0 || req.MonthlyLimitMicros < 0 {
		return Plan{}, errors.New("plan limits must be greater than or equal to 0")
	}
	if req.MonthlyFeeMicros != 0 {
		return Plan{}, errors.New("recurring fees are not supported; use enterprise budget limits instead")
	}
	status, err := normalizeStatus(req.Status)
	if err != nil {
		return Plan{}, err
	}
	now := time.Now().UTC()
	v := Plan{ID: id, Name: strings.TrimSpace(req.Name), Description: strings.TrimSpace(req.Description), MonthlyFeeMicros: req.MonthlyFeeMicros, IncludedTokens: req.IncludedTokens, MonthlyLimitMicros: req.MonthlyLimitMicros, Status: status, CreatedAt: now, UpdatedAt: now}
	if id == "" {
		v.ID = "plan_" + randomID()
	} else {
		items, err := s.repo.ListPlans(ctx)
		if err != nil {
			return Plan{}, err
		}
		old, err := findByID(items, id)
		if err != nil {
			return Plan{}, err
		}
		v.CreatedAt = old.CreatedAt
	}
	if err := s.repo.SavePlan(ctx, v); err != nil {
		return Plan{}, err
	}
	s.audit(ctx, "operator_plan", v.ID, "Saved plan "+v.Name)
	return v, nil
}
func (s *Service) DeletePlan(ctx context.Context, id string) error {
	customers, err := s.repo.ListCustomers(ctx)
	if err != nil {
		return err
	}
	for _, v := range customers {
		if v.PlanID == id {
			return errors.New("plan is still assigned to customers")
		}
	}
	rules, err := s.control.ListPricingRules(ctx, controlplane.PricingRuleQuery{Purpose: controlplane.PricingPurposeCustomerCharge, ScopeType: controlplane.PricingScopeOperatorPlan, ScopeID: id})
	if err != nil {
		return err
	}
	for _, rule := range rules {
		versions, detailErr := s.control.PricingRuleDetail(ctx, rule.ID)
		if detailErr != nil {
			return detailErr
		}
		for _, version := range versions.Versions {
			if version.State == controlplane.PricingVersionStatePublished {
				return errors.New("plan is referenced by a published pricing rule")
			}
		}
	}
	return s.repo.DeletePlan(ctx, id)
}

func (s *Service) ResolveCustomerPricingContext(ctx context.Context, customerID string) (controlplane.CustomerPricingContext, error) {
	customers, err := s.repo.ListCustomers(ctx)
	if err != nil {
		return controlplane.CustomerPricingContext{}, err
	}
	customer, err := findByID(customers, strings.TrimSpace(customerID))
	if err != nil {
		return controlplane.CustomerPricingContext{}, err
	}
	if customer.Status != StatusActive || customer.PlanID == "" {
		return controlplane.CustomerPricingContext{}, errors.New("customer pricing context is inactive")
	}
	if err := s.ValidatePricingPlan(ctx, customer.PlanID); err != nil {
		return controlplane.CustomerPricingContext{}, err
	}
	return controlplane.CustomerPricingContext{CustomerID: customer.ID, PlanID: customer.PlanID, Status: customer.Status, Currency: "USD"}, nil
}

func (s *Service) ValidatePricingPlan(ctx context.Context, planID string) error {
	plans, err := s.repo.ListPlans(ctx)
	if err != nil {
		return err
	}
	plan, err := findByID(plans, strings.TrimSpace(planID))
	if err != nil {
		return err
	}
	if plan.Status != StatusActive {
		return errors.New("pricing plan is inactive")
	}
	return nil
}

func (s *Service) ListBalanceEntries(ctx context.Context) ([]BalanceEntry, error) {
	return s.repo.ListBalanceEntries(ctx)
}
func (s *Service) ApplyBalanceEntry(ctx context.Context, actor string, req BalanceEntryRequest) (BalanceEntry, error) {
	customers, err := s.repo.ListCustomers(ctx)
	if err != nil {
		return BalanceEntry{}, err
	}
	if _, err := findByID(customers, req.CustomerID); err != nil {
		return BalanceEntry{}, err
	}
	if req.AmountMicros == 0 {
		return BalanceEntry{}, errors.New("amount_micros must not be zero")
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = "cost_correction"
	}
	switch kind {
	case "allocation_increase":
		if req.AmountMicros < 0 {
			return BalanceEntry{}, errors.New("allocation_increase amount must be positive")
		}
	case "allocation_decrease":
		if req.AmountMicros > 0 {
			return BalanceEntry{}, errors.New("allocation_decrease amount must be negative")
		}
	case "cost_correction":
	default:
		return BalanceEntry{}, errors.New("kind must be allocation_increase, allocation_decrease, or cost_correction")
	}
	v := BalanceEntry{ID: "bal_" + randomID(), CustomerID: req.CustomerID, Kind: kind, AmountMicros: req.AmountMicros, Currency: "USD", Reference: strings.TrimSpace(req.Reference), Note: strings.TrimSpace(req.Note), Actor: actor, CreatedAt: time.Now().UTC()}
	result, err := s.repo.ApplyBalanceEntry(ctx, v)
	if err != nil {
		return BalanceEntry{}, err
	}
	s.audit(ctx, "operator_balance", result.ID, fmt.Sprintf("Applied balance entry %d to customer %s", result.AmountMicros, result.CustomerID))
	return result, nil
}

func (s *Service) PublishTransactionalOutbox(ctx context.Context, event controlplane.TransactionalOutboxEvent) error {
	switch event.EventType {
	case controlplane.OutboxEventUsageRecorded:
		var payload controlplane.UsageRecordedEvent
		if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
			return err
		}
		return s.EvaluateUsageRisk(ctx, controlplane.UsageRecord{ID: payload.UsageRecordID, OperationID: payload.OperationID, AttemptID: payload.AttemptID, UsageVersion: payload.UsageVersion, APIKeyID: payload.APIKeyID, CustomerID: payload.CustomerID, InputTokens: payload.InputTokens, OutputTokens: payload.OutputTokens, UsageDimensions: payload.UsageDimensions, UsageCostMicros: payload.UsageCostMicros, PricingStatus: payload.PricingStatus, Status: payload.Status})
	case controlplane.OutboxEventCustomerChargePosted:
		var payload controlplane.CustomerChargePostedEvent
		if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
			return err
		}
		if payload.BillingLedgerID == "" || payload.CustomerID == "" || payload.AmountMicros < 0 || payload.Currency != "USD" || payload.IdempotencyKey != "customer_charge:"+payload.BillingLedgerID {
			return errors.New("invalid customer charge event")
		}
		entry := BalanceEntry{
			ID: "bal_" + randomID(), CustomerID: payload.CustomerID, Kind: "usage_charge", AmountMicros: -payload.AmountMicros,
			Currency: payload.Currency, BillingLedgerID: payload.BillingLedgerID, Reference: payload.IdempotencyKey,
			Note: "Usage charge", Actor: "customer_charge_consumer", CreatedAt: time.Now().UTC(),
		}
		_, err := s.repo.ApplyBalanceEntry(ctx, entry)
		return err
	default:
		return nil
	}
}

func (s *Service) ListRiskRules(ctx context.Context) ([]RiskRule, error) {
	return s.repo.ListRiskRules(ctx)
}
func (s *Service) SaveRiskRule(ctx context.Context, id string, req RiskRuleRequest) (RiskRule, error) {
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.RuleType) == "" || strings.TrimSpace(req.Action) == "" {
		return RiskRule{}, errors.New("name, rule_type, and action are required")
	}
	if !allowedValue(strings.TrimSpace(req.RuleType), "rpm", "tokens", "spend", "error_rate") {
		return RiskRule{}, errors.New("rule_type must be rpm, tokens, spend, or error_rate")
	}
	if !allowedValue(strings.TrimSpace(req.Action), "review", "block") {
		return RiskRule{}, errors.New("action must be review or block")
	}
	if req.Threshold < 0 || req.WindowMins < 1 {
		return RiskRule{}, errors.New("threshold must be non-negative and window_minutes must be positive")
	}
	status, err := normalizeStatus(req.Status)
	if err != nil {
		return RiskRule{}, err
	}
	now := time.Now().UTC()
	v := RiskRule{ID: id, Name: strings.TrimSpace(req.Name), RuleType: strings.TrimSpace(req.RuleType), Threshold: req.Threshold, WindowMins: req.WindowMins, Action: strings.TrimSpace(req.Action), Description: strings.TrimSpace(req.Description), Status: status, CreatedAt: now, UpdatedAt: now}
	if id == "" {
		v.ID = "risk_" + randomID()
	} else {
		items, err := s.repo.ListRiskRules(ctx)
		if err != nil {
			return RiskRule{}, err
		}
		old, err := findByID(items, id)
		if err != nil {
			return RiskRule{}, err
		}
		v.CreatedAt = old.CreatedAt
	}
	if err := s.repo.SaveRiskRule(ctx, v); err != nil {
		return RiskRule{}, err
	}
	return v, nil
}
func (s *Service) DeleteRiskRule(ctx context.Context, id string) error {
	return s.repo.DeleteRiskRule(ctx, id)
}

func (s *Service) ListRiskBlocks(ctx context.Context) ([]controlplane.GatewayRiskBlock, error) {
	return s.control.ListActiveGatewayRiskBlocks(ctx)
}

func (s *Service) ClearRiskBlock(ctx context.Context, actor, apiKeyID string) error {
	return s.control.ClearGatewayRiskBlock(ctx, actor, apiKeyID)
}

func (s *Service) ListNotices(ctx context.Context) ([]Notice, error) { return s.repo.ListNotices(ctx) }
func (s *Service) SaveNotice(ctx context.Context, id string, req NoticeRequest) (Notice, error) {
	if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Content) == "" {
		return Notice{}, errors.New("title and content are required")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "draft"
	}
	if status != "draft" && status != "published" && status != "archived" {
		return Notice{}, errors.New("status must be draft, published, or archived")
	}
	var publishAt *time.Time
	if strings.TrimSpace(req.PublishAt) != "" {
		parsed, err := time.Parse(time.RFC3339, req.PublishAt)
		if err != nil {
			return Notice{}, errors.New("publish_at must be RFC3339")
		}
		publishAt = &parsed
	}
	now := time.Now().UTC()
	wasPublished := false
	v := Notice{ID: id, Title: strings.TrimSpace(req.Title), Content: strings.TrimSpace(req.Content), Audience: strings.TrimSpace(req.Audience), Status: status, PublishAt: publishAt, CreatedAt: now, UpdatedAt: now}
	if v.Audience == "" {
		v.Audience = "all"
	}
	if id == "" {
		v.ID = "notice_" + randomID()
	} else {
		items, err := s.repo.ListNotices(ctx)
		if err != nil {
			return Notice{}, err
		}
		old, err := findByID(items, id)
		if err != nil {
			return Notice{}, err
		}
		v.CreatedAt = old.CreatedAt
		wasPublished = old.Status == "published"
	}
	if err := s.repo.SaveNotice(ctx, v); err != nil {
		return Notice{}, err
	}
	if s.control != nil && v.Status == "published" && !wasPublished {
		eventType := controlplane.CustomerNotificationAnnouncement
		switch strings.ToLower(v.Audience) {
		case "marketing":
			eventType = controlplane.CustomerNotificationMarketing
		case "product", "product_update":
			eventType = controlplane.CustomerNotificationProductUpdate
		}
		_ = s.control.PublishCustomerBroadcast(ctx, eventType, v.Title, v.Content, "/customer/notifications", v.ID+":"+v.UpdatedAt.Format(time.RFC3339Nano))
	}
	return v, nil
}
func (s *Service) DeleteNotice(ctx context.Context, id string) error {
	return s.repo.DeleteNotice(ctx, id)
}

func normalizeStatus(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = StatusActive
	}
	if value != StatusActive && value != StatusDisabled {
		return "", errors.New("status must be active or disabled")
	}
	return value, nil
}

func allowedValue(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
func findByID[T interface {
	CustomerGroup | Customer | Plan | RiskRule | Notice
}](items []T, id string) (T, error) {
	var zero T
	for _, item := range items {
		switch v := any(item).(type) {
		case CustomerGroup:
			if v.ID == id {
				return item, nil
			}
		case Customer:
			if v.ID == id {
				return item, nil
			}
		case Plan:
			if v.ID == id {
				return item, nil
			}
		case RiskRule:
			if v.ID == id {
				return item, nil
			}
		case Notice:
			if v.ID == id {
				return item, nil
			}
		}
	}
	return zero, fmt.Errorf("resource %q not found", id)
}
func randomID() string {
	var value [10]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value[:])
}
func (s *Service) audit(ctx context.Context, resourceType, id, summary string) {
	if s.control != nil {
		_ = s.control.RecordSystemEvent(ctx, "operator", resourceType, id, summary)
	}
}
