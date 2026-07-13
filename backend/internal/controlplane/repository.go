package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

type Repository interface {
	ListProviders(ctx context.Context) ([]ProviderConnection, error)
	SaveProvider(ctx context.Context, provider ProviderConnection) error
	ListLatestProviderHealthChecks(ctx context.Context) ([]ProviderHealthCheck, error)
	SaveProviderHealthCheck(ctx context.Context, check ProviderHealthCheck) error
	ListDepartments(ctx context.Context) ([]Department, error)
	SaveDepartment(ctx context.Context, department Department) error
	ListOrganizationGroups(ctx context.Context) ([]OrganizationGroup, error)
	SaveOrganizationGroup(ctx context.Context, group OrganizationGroup) error
	DeleteOrganizationGroup(ctx context.Context, id string) error
	ListGovernancePolicies(ctx context.Context) ([]GovernancePolicy, error)
	SaveGovernancePolicy(ctx context.Context, policy GovernancePolicy) error
	ListWorkspaceUsers(ctx context.Context) ([]WorkspaceUser, error)
	SaveWorkspaceUser(ctx context.Context, user WorkspaceUser) error
	GetCustomerWallet(ctx context.Context, userID string) (CustomerWallet, error)
	ListCustomerBillingEntries(ctx context.Context, query CustomerBillingQuery) ([]CustomerBillingEntry, int, error)
	ListAvailableCustomerVouchers(ctx context.Context, userID string, now time.Time) ([]CustomerVoucher, error)
	RedeemCustomerCode(ctx context.Context, request CustomerCodeRedemption) (CustomerBillingEntry, error)
	SaveCustomerRedemptionCode(ctx context.Context, code CustomerRedemptionCode) error
	ListAuthIdentities(ctx context.Context, userID string) ([]AuthIdentity, error)
	FindAuthIdentity(ctx context.Context, issuer, subject string) (AuthIdentity, bool, error)
	SaveAuthIdentity(ctx context.Context, identity AuthIdentity) error
	DeleteAuthIdentity(ctx context.Context, id string) error
	ListRoleBindings(ctx context.Context) ([]RoleBinding, error)
	SaveRoleBinding(ctx context.Context, binding RoleBinding) error
	DeleteRoleBinding(ctx context.Context, id string) error
	ListRoutingGroups(ctx context.Context) ([]RoutingGroup, error)
	SaveRoutingGroup(ctx context.Context, group RoutingGroup) error
	ListProviderAccounts(ctx context.Context) ([]ProviderAccount, error)
	SaveProviderAccount(ctx context.Context, account ProviderAccount) error
	ListGatewayModels(ctx context.Context) ([]GatewayModel, error)
	SaveGatewayModel(ctx context.Context, model GatewayModel) error
	DeleteGatewayModel(ctx context.Context, id string) error
	ListModelRoutes(ctx context.Context) ([]ModelRoute, error)
	SaveModelRoute(ctx context.Context, route ModelRoute) error
	DeleteModelRoute(ctx context.Context, id string) error
	ListLatestProviderAccountHealthChecks(ctx context.Context) ([]ProviderAccountHealthCheck, error)
	SaveProviderAccountHealthCheck(ctx context.Context, check ProviderAccountHealthCheck) error
	ListModelPricings(ctx context.Context) ([]ModelPricing, error)
	SaveModelPricing(ctx context.Context, pricing ModelPricing) error
	ListAPIKeys(ctx context.Context) ([]APIKeyRecord, error)
	FindAPIKeyByHash(ctx context.Context, hash string) (APIKeyRecord, bool, error)
	SaveAPIKey(ctx context.Context, key APIKeyRecord) error
	FindActiveGatewayRiskBlock(ctx context.Context, apiKeyID string, now time.Time) (GatewayRiskBlock, bool, error)
	ListActiveGatewayRiskBlocks(ctx context.Context, now time.Time) ([]GatewayRiskBlock, error)
	SaveGatewayRiskBlock(ctx context.Context, block GatewayRiskBlock) error
	DeleteGatewayRiskBlock(ctx context.Context, apiKeyID string) error
	DisableAPIKey(ctx context.Context, id string, updatedAt time.Time) error
	UpdateAPIKeyLastUsed(ctx context.Context, id string, lastUsedAt time.Time) error
	SaveUsageRecord(ctx context.Context, record UsageRecord) error
	ListUsageRecords(ctx context.Context, limit int) ([]UsageRecord, error)
	QueryUsageRecords(ctx context.Context, query UsageQuery) ([]UsageRecord, error)
	SummarizeUsageRecords(ctx context.Context, query UsageQuery) (UsageAggregate, error)
	SummarizeCostAllocation(ctx context.Context, dimension string, query UsageQuery) ([]CostAllocationRollup, error)
	SumUsageTokensByAPIKeySince(ctx context.Context, apiKeyID string, since time.Time) (int, error)
	SumUsageCostCentsByAPIKeySince(ctx context.Context, apiKeyID string, since time.Time) (int, error)
	SaveGatewayTrace(ctx context.Context, trace GatewayTrace) error
	ListGatewayTraces(ctx context.Context, limit int) ([]GatewayTrace, error)
	QueryGatewayTraces(ctx context.Context, query GatewayTraceQuery) ([]GatewayTrace, error)
	SummarizeGatewayTraces(ctx context.Context, query GatewayTraceQuery) (GatewayTraceSummary, error)
	ListAuditLogs(ctx context.Context, limit int) ([]AuditLog, error)
	QueryAuditLogs(ctx context.Context, query AuditLogQuery) ([]AuditLog, error)
	SummarizeAuditLogs(ctx context.Context, query AuditLogQuery) (AuditLogSummary, error)
	AddAuditLog(ctx context.Context, event AuditLog) error
	QueryAlertEvents(ctx context.Context, query AlertQuery) ([]AlertEvent, error)
	SummarizeAlertEvents(ctx context.Context, query AlertQuery) (AlertSummary, error)
	FindAlertEvent(ctx context.Context, id string) (AlertEvent, bool, error)
	FindAlertByDedupeKey(ctx context.Context, dedupeKey string) (AlertEvent, bool, error)
	SaveAlertEvent(ctx context.Context, event AlertEvent) error
	CleanupRetainedData(ctx context.Context, before time.Time) (RetentionCleanupResult, error)
	Health(ctx context.Context) error
	Close() error
}

func NewRepository(ctx context.Context, databaseURL string) (Repository, string, error) {
	if databaseURL == "" {
		return NewMemoryRepository(), "memory", nil
	}
	repo, err := NewPostgresRepository(ctx, databaseURL)
	if err != nil {
		return nil, "", err
	}
	return repo, "postgres", nil
}

type MemoryRepository struct {
	mu                  sync.RWMutex
	providers           map[string]ProviderConnection
	healthChecks        map[string]ProviderHealthCheck
	departments         map[string]Department
	organizationGroups  map[string]OrganizationGroup
	governancePolicies  map[string]GovernancePolicy
	workspaceUsers      map[string]WorkspaceUser
	customerWallets     map[string]CustomerWallet
	customerEntries     map[string]CustomerBillingEntry
	customerCodes       map[string]CustomerRedemptionCode
	customerRedemptions map[string]CustomerRedemption
	customerVouchers    map[string]CustomerVoucher
	authIdentities      map[string]AuthIdentity
	roleBindings        map[string]RoleBinding
	groups              map[string]RoutingGroup
	accounts            map[string]ProviderAccount
	gatewayModels       map[string]GatewayModel
	modelRoutes         map[string]ModelRoute
	accountHealthChecks map[string]ProviderAccountHealthCheck
	modelPricings       map[string]ModelPricing
	apiKeys             map[string]APIKeyRecord
	riskBlocks          map[string]GatewayRiskBlock
	usageRecords        map[string]UsageRecord
	gatewayTraces       map[string]GatewayTrace
	auditLogs           map[string]AuditLog
	alertEvents         map[string]AlertEvent
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		providers:           map[string]ProviderConnection{},
		healthChecks:        map[string]ProviderHealthCheck{},
		departments:         map[string]Department{},
		organizationGroups:  map[string]OrganizationGroup{},
		governancePolicies:  map[string]GovernancePolicy{},
		workspaceUsers:      map[string]WorkspaceUser{},
		customerWallets:     map[string]CustomerWallet{},
		customerEntries:     map[string]CustomerBillingEntry{},
		customerCodes:       map[string]CustomerRedemptionCode{},
		customerRedemptions: map[string]CustomerRedemption{},
		customerVouchers:    map[string]CustomerVoucher{},
		authIdentities:      map[string]AuthIdentity{},
		roleBindings:        map[string]RoleBinding{},
		groups:              map[string]RoutingGroup{},
		accounts:            map[string]ProviderAccount{},
		gatewayModels:       map[string]GatewayModel{},
		modelRoutes:         map[string]ModelRoute{},
		accountHealthChecks: map[string]ProviderAccountHealthCheck{},
		modelPricings:       map[string]ModelPricing{},
		apiKeys:             map[string]APIKeyRecord{},
		riskBlocks:          map[string]GatewayRiskBlock{},
		usageRecords:        map[string]UsageRecord{},
		gatewayTraces:       map[string]GatewayTrace{},
		auditLogs:           map[string]AuditLog{},
		alertEvents:         map[string]AlertEvent{},
	}
}

func (r *MemoryRepository) ListProviders(context.Context) ([]ProviderConnection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderConnection, 0, len(r.providers))
	for _, provider := range r.providers {
		out = append(out, provider)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority == out[j].Priority {
			return out[i].Name < out[j].Name
		}
		return out[i].Priority < out[j].Priority
	})
	return out, nil
}

func (r *MemoryRepository) SaveProvider(_ context.Context, provider ProviderConnection) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[provider.ID] = provider
	return nil
}

func (r *MemoryRepository) ListLatestProviderHealthChecks(context.Context) ([]ProviderHealthCheck, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderHealthCheck, 0, len(r.healthChecks))
	for _, check := range r.healthChecks {
		out = append(out, check)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CheckedAt.After(out[j].CheckedAt) })
	return out, nil
}

func (r *MemoryRepository) SaveProviderHealthCheck(_ context.Context, check ProviderHealthCheck) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.healthChecks[check.ProviderID] = check
	return nil
}

func (r *MemoryRepository) ListRoutingGroups(context.Context) ([]RoutingGroup, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RoutingGroup, 0, len(r.groups))
	for _, group := range r.groups {
		group.AccountCount, group.ActiveAccounts = r.accountCountsForGroup(group.ID)
		out = append(out, group)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder == out[j].SortOrder {
			return out[i].Name < out[j].Name
		}
		return out[i].SortOrder < out[j].SortOrder
	})
	return out, nil
}

func (r *MemoryRepository) SaveRoutingGroup(_ context.Context, group RoutingGroup) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.groups[group.ID] = group
	return nil
}

func (r *MemoryRepository) ListProviderAccounts(context.Context) ([]ProviderAccount, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderAccount, 0, len(r.accounts))
	for _, account := range r.accounts {
		out = append(out, account)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority == out[j].Priority {
			return out[i].Name < out[j].Name
		}
		return out[i].Priority < out[j].Priority
	})
	return out, nil
}

func (r *MemoryRepository) SaveProviderAccount(_ context.Context, account ProviderAccount) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accounts[account.ID] = account
	return nil
}

func (r *MemoryRepository) ListLatestProviderAccountHealthChecks(context.Context) ([]ProviderAccountHealthCheck, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderAccountHealthCheck, 0, len(r.accountHealthChecks))
	for _, check := range r.accountHealthChecks {
		out = append(out, check)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CheckedAt.After(out[j].CheckedAt) })
	return out, nil
}

func (r *MemoryRepository) SaveProviderAccountHealthCheck(_ context.Context, check ProviderAccountHealthCheck) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accountHealthChecks[check.AccountID] = check
	return nil
}

func (r *MemoryRepository) ListModelPricings(context.Context) ([]ModelPricing, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ModelPricing, 0, len(r.modelPricings))
	for _, pricing := range r.modelPricings {
		out = append(out, pricing)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out, nil
}

func (r *MemoryRepository) SaveModelPricing(_ context.Context, pricing ModelPricing) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modelPricings[pricing.ID] = pricing
	return nil
}

func (r *MemoryRepository) accountCountsForGroup(groupID string) (int, int) {
	var total, active int
	for _, account := range r.accounts {
		if contains(account.GroupIDs, groupID) {
			total++
			if account.Status == AccountStatusActive && account.Schedulable {
				active++
			}
		}
	}
	return total, active
}

func (r *MemoryRepository) ListAPIKeys(context.Context) ([]APIKeyRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]APIKeyRecord, 0, len(r.apiKeys))
	for _, key := range r.apiKeys {
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (r *MemoryRepository) FindAPIKeyByHash(_ context.Context, hash string) (APIKeyRecord, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, key := range r.apiKeys {
		if key.KeyHash == hash {
			return key, true, nil
		}
	}
	return APIKeyRecord{}, false, nil
}

func (r *MemoryRepository) SaveAPIKey(_ context.Context, key APIKeyRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.apiKeys[key.ID] = key
	return nil
}

func (r *MemoryRepository) FindActiveGatewayRiskBlock(_ context.Context, apiKeyID string, now time.Time) (GatewayRiskBlock, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	block, ok := r.riskBlocks[apiKeyID]
	return block, ok && block.ExpiresAt.After(now), nil
}

func (r *MemoryRepository) SaveGatewayRiskBlock(_ context.Context, block GatewayRiskBlock) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.riskBlocks[block.APIKeyID]; !ok || block.ExpiresAt.After(current.ExpiresAt) {
		r.riskBlocks[block.APIKeyID] = block
	}
	return nil
}

func (r *MemoryRepository) ListActiveGatewayRiskBlocks(_ context.Context, now time.Time) ([]GatewayRiskBlock, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]GatewayRiskBlock, 0, len(r.riskBlocks))
	for _, block := range r.riskBlocks {
		if block.ExpiresAt.After(now) {
			out = append(out, block)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExpiresAt.After(out[j].ExpiresAt) })
	return out, nil
}

func (r *MemoryRepository) DeleteGatewayRiskBlock(_ context.Context, apiKeyID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.riskBlocks, apiKeyID)
	return nil
}

func (r *MemoryRepository) UpdateAPIKeyLastUsed(_ context.Context, id string, lastUsedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.apiKeys[id]
	if !ok {
		return nil
	}
	key.LastUsedAt = &lastUsedAt
	key.UpdatedAt = lastUsedAt
	r.apiKeys[id] = key
	return nil
}

func (r *MemoryRepository) DisableAPIKey(_ context.Context, id string, updatedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.apiKeys[id]
	if !ok {
		return nil
	}
	key.Status = APIKeyStatusDisabled
	key.UpdatedAt = updatedAt
	r.apiKeys[id] = key
	return nil
}

func (r *MemoryRepository) SaveUsageRecord(_ context.Context, record UsageRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.usageRecords[record.ID] = record
	return nil
}

func (r *MemoryRepository) ListUsageRecords(_ context.Context, limit int) ([]UsageRecord, error) {
	return r.QueryUsageRecords(context.Background(), UsageQuery{Limit: limit})
}

func (r *MemoryRepository) QueryUsageRecords(_ context.Context, query UsageQuery) ([]UsageRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]UsageRecord, 0, len(r.usageRecords))
	for _, record := range r.usageRecords {
		if memoryUsageRecordMatches(record, query) {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 100, 500)
	if offset >= len(out) {
		return []UsageRecord{}, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (r *MemoryRepository) SummarizeUsageRecords(_ context.Context, query UsageQuery) (UsageAggregate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	records := make([]UsageRecord, 0, len(r.usageRecords))
	for _, record := range r.usageRecords {
		if memoryUsageRecordMatches(record, query) {
			records = append(records, record)
		}
	}
	return usageAggregateFromRecords(records), nil
}

func (r *MemoryRepository) SumUsageTokensByAPIKeySince(_ context.Context, apiKeyID string, since time.Time) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var total int
	for _, record := range r.usageRecords {
		if record.APIKeyID == apiKeyID && !record.CreatedAt.Before(since) {
			total += record.InputTokens + record.OutputTokens
		}
	}
	return total, nil
}

func (r *MemoryRepository) SumUsageCostCentsByAPIKeySince(_ context.Context, apiKeyID string, since time.Time) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var total int
	for _, record := range r.usageRecords {
		if record.APIKeyID == apiKeyID && !record.CreatedAt.Before(since) {
			total += record.CostCents
		}
	}
	return total, nil
}

func (r *MemoryRepository) SaveGatewayTrace(_ context.Context, trace GatewayTrace) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gatewayTraces[trace.ID] = trace
	return nil
}

func (r *MemoryRepository) ListGatewayTraces(_ context.Context, limit int) ([]GatewayTrace, error) {
	return r.QueryGatewayTraces(context.Background(), GatewayTraceQuery{Limit: limit})
}

func (r *MemoryRepository) QueryGatewayTraces(_ context.Context, query GatewayTraceQuery) ([]GatewayTrace, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]GatewayTrace, 0, len(r.gatewayTraces))
	for _, trace := range r.gatewayTraces {
		if memoryGatewayTraceMatches(trace, query) {
			out = append(out, trace)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 100, 500)
	if offset >= len(out) {
		return []GatewayTrace{}, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (r *MemoryRepository) SummarizeGatewayTraces(_ context.Context, query GatewayTraceQuery) (GatewayTraceSummary, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var summary GatewayTraceSummary
	var latencyTotal int64
	for _, trace := range r.gatewayTraces {
		if !memoryGatewayTraceMatches(trace, query) {
			continue
		}
		summary.Total++
		if trace.ProviderID != "" || trace.ProviderAccountID != "" {
			summary.Routed++
		}
		if trace.Status == "upstream_error" || trace.Status == "error" || trace.ErrorType != "" {
			summary.Errors++
		}
		summary.Tokens += trace.InputTokens + trace.OutputTokens
		latencyTotal += trace.LatencyMS
	}
	if summary.Total > 0 {
		summary.AvgLatencyMS = latencyTotal / int64(summary.Total)
	}
	return summary, nil
}

func (r *MemoryRepository) ListAuditLogs(_ context.Context, limit int) ([]AuditLog, error) {
	return r.QueryAuditLogs(context.Background(), AuditLogQuery{Limit: limit})
}

func (r *MemoryRepository) QueryAuditLogs(_ context.Context, query AuditLogQuery) ([]AuditLog, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AuditLog, 0, len(r.auditLogs))
	for _, event := range r.auditLogs {
		if memoryAuditLogMatches(event, query) {
			out = append(out, event)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 50, 500)
	if offset >= len(out) {
		return []AuditLog{}, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (r *MemoryRepository) SummarizeAuditLogs(_ context.Context, query AuditLogQuery) (AuditLogSummary, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	actors := map[string]struct{}{}
	resources := map[string]struct{}{}
	actions := map[string]struct{}{}
	var summary AuditLogSummary
	for _, event := range r.auditLogs {
		if !memoryAuditLogMatches(event, query) {
			continue
		}
		summary.Total++
		if event.Actor != "" {
			actors[event.Actor] = struct{}{}
		}
		if event.ResourceType != "" {
			resources[event.ResourceType] = struct{}{}
		}
		if event.Action != "" {
			actions[event.Action] = struct{}{}
		}
	}
	summary.Actors = len(actors)
	summary.Resources = len(resources)
	summary.Actions = len(actions)
	return summary, nil
}

func (r *MemoryRepository) AddAuditLog(_ context.Context, event AuditLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.auditLogs[event.ID] = event
	return nil
}

func (r *MemoryRepository) Health(context.Context) error {
	return nil
}

func (r *MemoryRepository) Close() error {
	return nil
}

func memoryUsageRecordMatches(record UsageRecord, query UsageQuery) bool {
	if query.APIKeyID != "" && record.APIKeyID != query.APIKeyID {
		return false
	}
	if len(query.APIKeyIDs) > 0 && !contains(query.APIKeyIDs, record.APIKeyID) {
		return false
	}
	if query.CustomerID != "" && record.CustomerID != query.CustomerID {
		return false
	}
	if query.Model != "" && record.Model != query.Model {
		return false
	}
	if query.ProviderID != "" && record.ProviderID != query.ProviderID {
		return false
	}
	if query.AccountID != "" && record.ProviderAccountID != query.AccountID {
		return false
	}
	if query.Status != "" && record.Status != query.Status {
		return false
	}
	if !query.CreatedFrom.IsZero() && record.CreatedAt.Before(query.CreatedFrom) {
		return false
	}
	if !query.CreatedTo.IsZero() && record.CreatedAt.After(query.CreatedTo) {
		return false
	}
	keyword := strings.ToLower(strings.TrimSpace(query.Search))
	if keyword == "" {
		return true
	}
	return anyContains(keyword, record.Model, record.Status, record.ErrorType, record.ProviderID, record.ProviderAccountID, record.APIKeyID, record.CustomerID, record.APIFingerprint)
}

func memoryGatewayTraceMatches(trace GatewayTrace, query GatewayTraceQuery) bool {
	if query.APIKeyID != "" && trace.APIKeyID != query.APIKeyID {
		return false
	}
	if len(query.APIKeyIDs) > 0 && !contains(query.APIKeyIDs, trace.APIKeyID) {
		return false
	}
	if query.Model != "" && trace.Model != query.Model {
		return false
	}
	if query.Status != "" && trace.Status != query.Status {
		return false
	}
	if !query.CreatedFrom.IsZero() && trace.CreatedAt.Before(query.CreatedFrom) {
		return false
	}
	if !query.CreatedTo.IsZero() && trace.CreatedAt.After(query.CreatedTo) {
		return false
	}
	keyword := strings.ToLower(strings.TrimSpace(query.Search))
	if keyword == "" {
		return true
	}
	return anyContains(keyword, trace.Model, trace.Status, trace.ErrorType, trace.ProviderID, trace.ProviderAccountID, trace.RouteSource, trace.RouteReason, trace.PolicyID, trace.PolicyName, trace.PolicySource, trace.PolicySnapshot, trace.APIKeyID, trace.APIFingerprint, trace.RequestSummary, trace.ResponseSummary)
}

func memoryAuditLogMatches(event AuditLog, query AuditLogQuery) bool {
	if query.Action != "" && event.Action != query.Action {
		return false
	}
	if query.ResourceType != "" && event.ResourceType != query.ResourceType {
		return false
	}
	if !query.CreatedFrom.IsZero() && event.CreatedAt.Before(query.CreatedFrom) {
		return false
	}
	if !query.CreatedTo.IsZero() && event.CreatedAt.After(query.CreatedTo) {
		return false
	}
	keyword := strings.ToLower(strings.TrimSpace(query.Search))
	if keyword == "" {
		return true
	}
	return anyContains(keyword, event.Actor, event.Action, event.ResourceType, event.ResourceID, event.Summary)
}

func anyContains(keyword string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), keyword) {
			return true
		}
	}
	return false
}

func normalizeListWindow(limit int, offset int, fallback int, max int) (int, int) {
	if limit <= 0 {
		limit = fallback
	}
	if limit > max {
		limit = max
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func appendExactFilter(clauses *[]string, args *[]any, column string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	*args = append(*args, value)
	*clauses = append(*clauses, fmt.Sprintf("%s = $%d", column, len(*args)))
}

func appendAnyExactFilter(clauses *[]string, args *[]any, column string, values []string) {
	values = cleanStringList(values)
	if len(values) == 0 {
		return
	}
	placeholders := make([]string, 0, len(values))
	for _, value := range values {
		*args = append(*args, value)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(*args)))
	}
	*clauses = append(*clauses, column+" IN ("+strings.Join(placeholders, ",")+")")
}

func appendTimeFilter(clauses *[]string, args *[]any, column string, operator string, value time.Time) {
	if value.IsZero() {
		return
	}
	*args = append(*args, value)
	*clauses = append(*clauses, fmt.Sprintf("%s %s $%d", column, operator, len(*args)))
}

func appendUsageRecordFilters(clauses *[]string, args *[]any, query UsageQuery) {
	appendExactFilter(clauses, args, "api_key_id", query.APIKeyID)
	appendAnyExactFilter(clauses, args, "api_key_id", query.APIKeyIDs)
	appendExactFilter(clauses, args, "customer_id", query.CustomerID)
	appendExactFilter(clauses, args, "model", query.Model)
	appendExactFilter(clauses, args, "provider_id", query.ProviderID)
	appendExactFilter(clauses, args, "provider_account_id", query.AccountID)
	appendExactFilter(clauses, args, "status", query.Status)
	appendTimeFilter(clauses, args, "created_at", ">=", query.CreatedFrom)
	appendTimeFilter(clauses, args, "created_at", "<=", query.CreatedTo)
	appendSearchFilter(clauses, args, query.Search, []string{"model", "status", "error_type", "provider_id", "provider_account_id", "api_key_id", "customer_id", "api_fingerprint"})
}

func appendGatewayTraceFilters(clauses *[]string, args *[]any, query GatewayTraceQuery) {
	appendExactFilter(clauses, args, "api_key_id", query.APIKeyID)
	appendAnyExactFilter(clauses, args, "api_key_id", query.APIKeyIDs)
	appendExactFilter(clauses, args, "model", query.Model)
	appendExactFilter(clauses, args, "status", query.Status)
	appendTimeFilter(clauses, args, "created_at", ">=", query.CreatedFrom)
	appendTimeFilter(clauses, args, "created_at", "<=", query.CreatedTo)
	appendSearchFilter(clauses, args, query.Search, []string{"model", "status", "error_type", "provider_id", "provider_account_id", "route_source", "route_reason", "policy_id", "policy_name", "policy_source", "policy_snapshot", "api_key_id", "api_fingerprint", "request_summary", "response_summary"})
}

func appendAuditLogFilters(clauses *[]string, args *[]any, query AuditLogQuery) {
	appendExactFilter(clauses, args, "action", query.Action)
	appendExactFilter(clauses, args, "resource_type", query.ResourceType)
	appendTimeFilter(clauses, args, "created_at", ">=", query.CreatedFrom)
	appendTimeFilter(clauses, args, "created_at", "<=", query.CreatedTo)
	appendSearchFilter(clauses, args, query.Search, []string{"actor", "action", "resource_type", "resource_id", "summary"})
}

func appendSearchFilter(clauses *[]string, args *[]any, value string, columns []string) {
	value = strings.TrimSpace(value)
	if value == "" || len(columns) == 0 {
		return
	}
	*args = append(*args, "%"+value+"%")
	placeholder := fmt.Sprintf("$%d", len(*args))
	parts := make([]string, 0, len(columns))
	for _, column := range columns {
		parts = append(parts, column+" ILIKE "+placeholder)
	}
	*clauses = append(*clauses, "("+strings.Join(parts, " OR ")+")")
}

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(ctx context.Context, databaseURL string) (*PostgresRepository, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	repo := &PostgresRepository{db: db}
	if err := repo.Health(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := repo.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repo, nil
}

func (r *PostgresRepository) migrate(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS provider_connections (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  base_url TEXT NOT NULL,
  status TEXT NOT NULL,
  models TEXT NOT NULL DEFAULT '[]',
  priority INTEGER NOT NULL DEFAULT 100,
  secret_configured BOOLEAN NOT NULL DEFAULT false,
  secret_hint TEXT NOT NULL DEFAULT '',
  secret_ciphertext TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE provider_connections ADD COLUMN IF NOT EXISTS secret_ciphertext TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS provider_health_checks (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  latency_ms BIGINT NOT NULL DEFAULT 0,
  message TEXT NOT NULL DEFAULT '',
  models TEXT NOT NULL DEFAULT '[]',
  checked_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS provider_health_checks_provider_checked_idx
  ON provider_health_checks(provider_id, checked_at DESC);

CREATE TABLE IF NOT EXISTS departments (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  code TEXT NOT NULL UNIQUE,
  parent_id TEXT NOT NULL DEFAULT '',
  cost_center TEXT NOT NULL DEFAULT '',
  monthly_budget_cents INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS departments_parent_idx
  ON departments(parent_id);

CREATE INDEX IF NOT EXISTS departments_cost_center_idx
  ON departments(cost_center);

CREATE TABLE IF NOT EXISTS workspace_users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  role TEXT NOT NULL DEFAULT 'developer',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS external_issuer TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS external_subject TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS department_id TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS totp_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS totp_secret_ciphertext TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS totp_recovery_hashes TEXT NOT NULL DEFAULT '[]';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS password_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS email_verify_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS email_verify_expires_at TIMESTAMPTZ;
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS password_reset_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS password_reset_expires_at TIMESTAMPTZ;
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS balance_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS concurrency_limit INTEGER NOT NULL DEFAULT 5;
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS rpm_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS avatar_data_url TEXT NOT NULL DEFAULT '';

DO $$ BEGIN
  ALTER TABLE workspace_users ADD CONSTRAINT workspace_users_avatar_size CHECK (octet_length(avatar_data_url) <= 262144);
EXCEPTION
  WHEN duplicate_object THEN NULL;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS workspace_users_external_identity_unique
  ON workspace_users(external_issuer, external_subject)
  WHERE external_issuer <> '' AND external_subject <> '';

CREATE TABLE IF NOT EXISTS organization_groups (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS organization_groups_name_idx ON organization_groups(lower(name));

CREATE TABLE IF NOT EXISTS organization_group_members (
  group_id TEXT NOT NULL REFERENCES organization_groups(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY(group_id,user_id),
  UNIQUE(user_id)
);

CREATE INDEX IF NOT EXISTS organization_group_members_group_idx ON organization_group_members(group_id,created_at);

CREATE TABLE IF NOT EXISTS auth_identities (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  issuer TEXT NOT NULL,
  subject TEXT NOT NULL,
  email TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(issuer, subject),
  UNIQUE(user_id, issuer)
);

INSERT INTO auth_identities(id,user_id,issuer,subject,email,created_at,updated_at)
SELECT 'aid_' || md5(id || ':' || external_issuer || ':' || external_subject), id, external_issuer, external_subject, email, created_at, updated_at
FROM workspace_users
WHERE external_issuer <> '' AND external_subject <> ''
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS role_bindings (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS role_bindings_unique_scope_idx
  ON role_bindings(user_id, role, scope_type, scope_id);

CREATE INDEX IF NOT EXISTS role_bindings_scope_idx
  ON role_bindings(scope_type, scope_id);

CREATE TABLE IF NOT EXISTS customer_wallets (
  user_id TEXT PRIMARY KEY REFERENCES workspace_users(id) ON DELETE CASCADE,
  gift_balance_cents INTEGER NOT NULL DEFAULT 0,
  profit_balance_cents INTEGER NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS customer_billing_entries (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  amount_cents INTEGER NOT NULL,
  balance_after_cents INTEGER NOT NULL,
  reference TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS customer_billing_entries_user_created_idx
  ON customer_billing_entries(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS customer_redemption_codes (
  id TEXT PRIMARY KEY,
  code_hash TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL DEFAULT '',
  amount_cents INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  max_redemptions INTEGER NOT NULL DEFAULT 1,
  redeemed_count INTEGER NOT NULL DEFAULT 0,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS customer_redemptions (
  code_id TEXT NOT NULL REFERENCES customer_redemption_codes(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  entry_id TEXT NOT NULL REFERENCES customer_billing_entries(id) ON DELETE CASCADE,
  redeemed_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY(code_id, user_id)
);

CREATE TABLE IF NOT EXISTS customer_vouchers (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  amount_cents INTEGER NOT NULL,
  minimum_recharge_cents INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active',
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS customer_vouchers_user_status_idx
  ON customer_vouchers(user_id, status, expires_at);

CREATE TABLE IF NOT EXISTS routing_groups (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  platform TEXT NOT NULL,
  group_type TEXT NOT NULL DEFAULT 'standard',
  rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1,
  rpm_limit INTEGER NOT NULL DEFAULT 0,
  is_exclusive BOOLEAN NOT NULL DEFAULT false,
  daily_budget_cents INTEGER NOT NULL DEFAULT 0,
  weekly_budget_cents INTEGER NOT NULL DEFAULT 0,
  monthly_budget_cents INTEGER NOT NULL DEFAULT 0,
  image_enabled BOOLEAN NOT NULL DEFAULT false,
  batch_image_enabled BOOLEAN NOT NULL DEFAULT false,
  image_rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1,
  batch_image_discount_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1,
  image_price_1k_cents INTEGER NOT NULL DEFAULT 0,
  image_price_2k_cents INTEGER NOT NULL DEFAULT 0,
  image_price_4k_cents INTEGER NOT NULL DEFAULT 0,
  video_enabled BOOLEAN NOT NULL DEFAULT false,
  video_rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1,
  video_price_480p_cents INTEGER NOT NULL DEFAULT 0,
  video_price_720p_cents INTEGER NOT NULL DEFAULT 0,
  video_price_1080p_cents INTEGER NOT NULL DEFAULT 0,
  peak_rate_enabled BOOLEAN NOT NULL DEFAULT false,
  peak_start TEXT NOT NULL DEFAULT '',
  peak_end TEXT NOT NULL DEFAULT '',
  peak_rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1,
  status TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS group_type TEXT NOT NULL DEFAULT 'standard';
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS rpm_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS is_exclusive BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS daily_budget_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS weekly_budget_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS monthly_budget_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS image_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS batch_image_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS image_rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS batch_image_discount_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS image_price_1k_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS image_price_2k_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS image_price_4k_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS video_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS video_rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS video_price_480p_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS video_price_720p_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS video_price_1080p_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS peak_rate_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS peak_start TEXT NOT NULL DEFAULT '';
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS peak_end TEXT NOT NULL DEFAULT '';
ALTER TABLE routing_groups ADD COLUMN IF NOT EXISTS peak_rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS provider_accounts (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  platform TEXT NOT NULL,
  auth_type TEXT NOT NULL,
  status TEXT NOT NULL,
  schedulable BOOLEAN NOT NULL DEFAULT true,
  priority INTEGER NOT NULL DEFAULT 50,
  weight INTEGER NOT NULL DEFAULT 100,
  concurrency INTEGER NOT NULL DEFAULT 3,
  rpm_limit INTEGER NOT NULL DEFAULT 0,
  tpm_limit INTEGER NOT NULL DEFAULT 0,
  load_factor INTEGER,
  rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1,
  models TEXT NOT NULL DEFAULT '[]',
  group_ids TEXT NOT NULL DEFAULT '[]',
  secret_configured BOOLEAN NOT NULL DEFAULT false,
  secret_hint TEXT NOT NULL DEFAULT '',
  secret_ciphertext TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  last_used_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  cooldown_until TIMESTAMPTZ,
  circuit_state TEXT NOT NULL DEFAULT 'closed',
  circuit_failure_threshold INTEGER NOT NULL DEFAULT 5,
  circuit_open_seconds INTEGER NOT NULL DEFAULT 60,
  consecutive_failures INTEGER NOT NULL DEFAULT 0,
  circuit_opened_until TIMESTAMPTZ,
  last_failure_at TIMESTAMPTZ,
  temp_unschedulable_rules TEXT NOT NULL DEFAULT '[]',
  temp_unschedulable_reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS provider_id TEXT NOT NULL DEFAULT '';
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS load_factor INTEGER;
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS weight INTEGER NOT NULL DEFAULT 100;
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS rpm_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS tpm_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS cooldown_until TIMESTAMPTZ;
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS circuit_state TEXT NOT NULL DEFAULT 'closed';
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS circuit_failure_threshold INTEGER NOT NULL DEFAULT 5;
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS circuit_open_seconds INTEGER NOT NULL DEFAULT 60;
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS consecutive_failures INTEGER NOT NULL DEFAULT 0;
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS circuit_opened_until TIMESTAMPTZ;
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS last_failure_at TIMESTAMPTZ;
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS temp_unschedulable_rules TEXT NOT NULL DEFAULT '[]';
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS temp_unschedulable_reason TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS provider_account_health_checks (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
  provider_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  latency_ms BIGINT NOT NULL DEFAULT 0,
  message TEXT NOT NULL DEFAULT '',
  models TEXT NOT NULL DEFAULT '[]',
  checked_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS provider_account_health_checks_account_checked_idx
  ON provider_account_health_checks(account_id, checked_at DESC);

CREATE TABLE IF NOT EXISTS gateway_models (
  id TEXT PRIMARY KEY,
  model_id TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  modality TEXT NOT NULL DEFAULT 'chat',
  default_route_group TEXT NOT NULL DEFAULT 'default',
  sticky_enabled BOOLEAN NOT NULL DEFAULT false,
  sticky_ttl_seconds INTEGER NOT NULL DEFAULT 1800,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS gateway_models_status_model_idx
  ON gateway_models(status, model_id);

ALTER TABLE gateway_models ADD COLUMN IF NOT EXISTS sticky_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE gateway_models ADD COLUMN IF NOT EXISTS sticky_ttl_seconds INTEGER NOT NULL DEFAULT 1800;

CREATE TABLE IF NOT EXISTS model_routes (
  id TEXT PRIMARY KEY,
  gateway_model_id TEXT NOT NULL REFERENCES gateway_models(id) ON DELETE CASCADE,
  route_group TEXT NOT NULL DEFAULT 'default',
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
  upstream_model TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 100,
  weight INTEGER NOT NULL DEFAULT 100,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(gateway_model_id, route_group, provider_account_id, upstream_model)
);

CREATE INDEX IF NOT EXISTS model_routes_resolution_idx
  ON model_routes(gateway_model_id, route_group, status, priority);

CREATE INDEX IF NOT EXISTS model_routes_account_idx
  ON model_routes(provider_account_id);

CREATE TABLE IF NOT EXISTS model_pricings (
  id TEXT PRIMARY KEY,
  model TEXT NOT NULL UNIQUE,
  currency TEXT NOT NULL DEFAULT 'USD',
  input_price_cents_per_1m_tokens INTEGER NOT NULL DEFAULT 0,
  output_price_cents_per_1m_tokens INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS governance_policies (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  scope_type TEXT NOT NULL DEFAULT 'global',
  scope_id TEXT NOT NULL DEFAULT '',
  model_allowlist TEXT NOT NULL DEFAULT '[]',
  model_denylist TEXT NOT NULL DEFAULT '[]',
  qps_limit INTEGER NOT NULL DEFAULT 0,
  monthly_token_limit INTEGER NOT NULL DEFAULT 0,
  monthly_budget_cents INTEGER NOT NULL DEFAULT 0,
  overage_action TEXT NOT NULL DEFAULT 'block',
  prompt_logging_mode TEXT NOT NULL DEFAULT 'metadata_only',
  retention_days INTEGER NOT NULL DEFAULT 30,
  tool_call_allowed BOOLEAN NOT NULL DEFAULT true,
  image_input_allowed BOOLEAN NOT NULL DEFAULT true,
  web_access_allowed BOOLEAN NOT NULL DEFAULT false,
  status TEXT NOT NULL DEFAULT 'active',
  version INTEGER NOT NULL DEFAULT 1,
  last_updated_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE governance_policies ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE governance_policies ADD COLUMN IF NOT EXISTS last_updated_by TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS governance_policies_scope_idx
  ON governance_policies(scope_type, scope_id);

CREATE INDEX IF NOT EXISTS governance_policies_status_idx
  ON governance_policies(status);

CREATE TABLE IF NOT EXISTS api_keys (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  key_hash TEXT NOT NULL UNIQUE,
  fingerprint TEXT NOT NULL,
  prefix TEXT NOT NULL,
  status TEXT NOT NULL,
  key_type TEXT NOT NULL DEFAULT 'workspace',
  customer_id TEXT NOT NULL DEFAULT '',
  owner_user_id TEXT NOT NULL DEFAULT '',
  policy_id TEXT NOT NULL DEFAULT '',
  model_allowlist TEXT NOT NULL DEFAULT '[]',
  qps_limit INTEGER NOT NULL DEFAULT 0,
  monthly_token_limit INTEGER NOT NULL DEFAULT 0,
  expires_at TIMESTAMPTZ,
  last_used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS policy_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS key_type TEXT NOT NULL DEFAULT 'workspace';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS customer_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS owner_user_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS api_keys_owner_user_idx
  ON api_keys(owner_user_id, status)
  WHERE owner_user_id <> '';

CREATE TABLE IF NOT EXISTS gateway_risk_blocks (
  api_key_id TEXT PRIMARY KEY,
  rule_id TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS gateway_risk_blocks_expiry_idx ON gateway_risk_blocks(expires_at);

CREATE TABLE IF NOT EXISTS audit_logs (
  id TEXT PRIMARY KEY,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  summary TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS usage_records (
  id TEXT PRIMARY KEY,
  api_key_id TEXT NOT NULL,
  customer_id TEXT NOT NULL DEFAULT '',
  api_fingerprint TEXT NOT NULL,
  model TEXT NOT NULL,
  upstream_model TEXT NOT NULL DEFAULT '',
  provider_id TEXT NOT NULL DEFAULT '',
  provider_account_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error_type TEXT NOT NULL DEFAULT '',
  latency_ms BIGINT NOT NULL DEFAULT 0,
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  cost_cents INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS provider_account_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS customer_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS upstream_model TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS gateway_traces (
  id TEXT PRIMARY KEY,
  api_key_id TEXT NOT NULL,
  api_fingerprint TEXT NOT NULL,
  model TEXT NOT NULL,
  stream BOOLEAN NOT NULL DEFAULT false,
  message_count INTEGER NOT NULL DEFAULT 0,
  provider_id TEXT NOT NULL DEFAULT '',
  provider_account_id TEXT NOT NULL DEFAULT '',
  gateway_model_id TEXT NOT NULL DEFAULT '',
  route_id TEXT NOT NULL DEFAULT '',
  route_group TEXT NOT NULL DEFAULT '',
  upstream_model TEXT NOT NULL DEFAULT '',
  route_source TEXT NOT NULL DEFAULT '',
  route_reason TEXT NOT NULL DEFAULT '',
  policy_id TEXT NOT NULL DEFAULT '',
  policy_name TEXT NOT NULL DEFAULT '',
  policy_source TEXT NOT NULL DEFAULT '',
  policy_version INTEGER NOT NULL DEFAULT 0,
  policy_snapshot TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  http_status INTEGER NOT NULL DEFAULT 0,
  error_type TEXT NOT NULL DEFAULT '',
  latency_ms BIGINT NOT NULL DEFAULT 0,
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  request_summary TEXT NOT NULL DEFAULT '',
  response_summary TEXT NOT NULL DEFAULT '',
  route_attempts TEXT NOT NULL DEFAULT '[]',
  created_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS policy_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS gateway_model_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS route_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS route_group TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS upstream_model TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS policy_name TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS policy_source TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS policy_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS policy_snapshot TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS route_attempts TEXT NOT NULL DEFAULT '[]';

CREATE INDEX IF NOT EXISTS gateway_traces_created_idx
  ON gateway_traces(created_at DESC);

CREATE INDEX IF NOT EXISTS gateway_traces_route_idx
  ON gateway_traces(provider_id, provider_account_id, created_at DESC);

CREATE INDEX IF NOT EXISTS gateway_traces_policy_idx
  ON gateway_traces(policy_id, created_at DESC);

CREATE TABLE IF NOT EXISTS alert_events (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  severity TEXT NOT NULL,
  status TEXT NOT NULL,
  title TEXT NOT NULL,
  summary TEXT NOT NULL,
  resource_type TEXT NOT NULL DEFAULT '',
  resource_id TEXT NOT NULL DEFAULT '',
  dedupe_key TEXT NOT NULL UNIQUE,
  metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  first_seen_at TIMESTAMPTZ NOT NULL,
  last_seen_at TIMESTAMPTZ NOT NULL,
  acknowledged_at TIMESTAMPTZ,
  acknowledged_by TEXT NOT NULL DEFAULT '',
  resolved_at TIMESTAMPTZ,
  resolved_by TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS alert_events_status_last_seen_idx
  ON alert_events(status, last_seen_at DESC);

CREATE INDEX IF NOT EXISTS alert_events_resource_idx
  ON alert_events(resource_type, resource_id, last_seen_at DESC);

`)
	return err
}

func (r *PostgresRepository) ListProviders(ctx context.Context) ([]ProviderConnection, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, name, type, base_url, status, models, priority, secret_configured, secret_hint, secret_ciphertext, created_at, updated_at
FROM provider_connections
ORDER BY priority ASC, name ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderConnection
	for rows.Next() {
		var provider ProviderConnection
		var models string
		if err := rows.Scan(&provider.ID, &provider.Name, &provider.Type, &provider.BaseURL, &provider.Status, &models, &provider.Priority, &provider.SecretConfigured, &provider.SecretHint, &provider.SecretCiphertext, &provider.CreatedAt, &provider.UpdatedAt); err != nil {
			return nil, err
		}
		provider.Models = parseStringList(models)
		out = append(out, provider)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveProvider(ctx context.Context, provider ProviderConnection) error {
	models := marshalStringList(provider.Models)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO provider_connections(id, name, type, base_url, status, models, priority, secret_configured, secret_hint, secret_ciphertext, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT(id) DO UPDATE SET
  name = EXCLUDED.name,
  type = EXCLUDED.type,
  base_url = EXCLUDED.base_url,
  status = EXCLUDED.status,
  models = EXCLUDED.models,
  priority = EXCLUDED.priority,
  secret_configured = EXCLUDED.secret_configured,
  secret_hint = EXCLUDED.secret_hint,
  secret_ciphertext = EXCLUDED.secret_ciphertext,
  updated_at = EXCLUDED.updated_at
`, provider.ID, provider.Name, provider.Type, provider.BaseURL, provider.Status, models, provider.Priority, provider.SecretConfigured, provider.SecretHint, provider.SecretCiphertext, provider.CreatedAt, provider.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListLatestProviderHealthChecks(ctx context.Context) ([]ProviderHealthCheck, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT DISTINCT ON (provider_id) id, provider_id, status, latency_ms, message, models, checked_at
FROM provider_health_checks
ORDER BY provider_id, checked_at DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderHealthCheck
	for rows.Next() {
		var check ProviderHealthCheck
		var models string
		if err := rows.Scan(&check.ID, &check.ProviderID, &check.Status, &check.LatencyMS, &check.Message, &models, &check.CheckedAt); err != nil {
			return nil, err
		}
		check.Models = parseStringList(models)
		out = append(out, check)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CheckedAt.After(out[j].CheckedAt) })
	return out, rows.Err()
}

func (r *PostgresRepository) SaveProviderHealthCheck(ctx context.Context, check ProviderHealthCheck) error {
	models := marshalStringList(check.Models)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO provider_health_checks(id, provider_id, status, latency_ms, message, models, checked_at)
VALUES($1,$2,$3,$4,$5,$6,$7)
`, check.ID, check.ProviderID, check.Status, check.LatencyMS, check.Message, models, check.CheckedAt)
	return err
}

func (r *PostgresRepository) ListRoutingGroups(ctx context.Context) ([]RoutingGroup, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, name, description, platform, group_type, rate_multiplier, rpm_limit, is_exclusive,
  daily_budget_cents, weekly_budget_cents, monthly_budget_cents,
  image_enabled, batch_image_enabled, image_rate_multiplier, batch_image_discount_multiplier,
  image_price_1k_cents, image_price_2k_cents, image_price_4k_cents,
  video_enabled, video_rate_multiplier, video_price_480p_cents, video_price_720p_cents, video_price_1080p_cents,
  peak_rate_enabled, peak_start, peak_end, peak_rate_multiplier,
  status, sort_order, created_at, updated_at
FROM routing_groups
ORDER BY sort_order ASC, name ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RoutingGroup
	for rows.Next() {
		var group RoutingGroup
		if err := rows.Scan(
			&group.ID,
			&group.Name,
			&group.Description,
			&group.Platform,
			&group.GroupType,
			&group.RateMultiplier,
			&group.RPMLimit,
			&group.IsExclusive,
			&group.DailyBudgetCents,
			&group.WeeklyBudgetCents,
			&group.MonthlyBudgetCents,
			&group.ImageEnabled,
			&group.BatchImageEnabled,
			&group.ImageRateMultiplier,
			&group.BatchImageDiscountMultiplier,
			&group.ImagePrice1KCents,
			&group.ImagePrice2KCents,
			&group.ImagePrice4KCents,
			&group.VideoEnabled,
			&group.VideoRateMultiplier,
			&group.VideoPrice480PCents,
			&group.VideoPrice720PCents,
			&group.VideoPrice1080PCents,
			&group.PeakRateEnabled,
			&group.PeakStart,
			&group.PeakEnd,
			&group.PeakRateMultiplier,
			&group.Status,
			&group.SortOrder,
			&group.CreatedAt,
			&group.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	counts, activeCounts, err := r.routingGroupAccountCounts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].AccountCount = counts[out[i].ID]
		out[i].ActiveAccounts = activeCounts[out[i].ID]
	}
	return out, nil
}

func (r *PostgresRepository) SaveRoutingGroup(ctx context.Context, group RoutingGroup) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO routing_groups(
  id, name, description, platform, group_type, rate_multiplier, rpm_limit, is_exclusive,
  daily_budget_cents, weekly_budget_cents, monthly_budget_cents,
  image_enabled, batch_image_enabled, image_rate_multiplier, batch_image_discount_multiplier,
  image_price_1k_cents, image_price_2k_cents, image_price_4k_cents,
  video_enabled, video_rate_multiplier, video_price_480p_cents, video_price_720p_cents, video_price_1080p_cents,
  peak_rate_enabled, peak_start, peak_end, peak_rate_multiplier,
  status, sort_order, created_at, updated_at
)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31)
ON CONFLICT(id) DO UPDATE SET
  name = EXCLUDED.name,
  description = EXCLUDED.description,
  platform = EXCLUDED.platform,
  group_type = EXCLUDED.group_type,
  rate_multiplier = EXCLUDED.rate_multiplier,
  rpm_limit = EXCLUDED.rpm_limit,
  is_exclusive = EXCLUDED.is_exclusive,
  daily_budget_cents = EXCLUDED.daily_budget_cents,
  weekly_budget_cents = EXCLUDED.weekly_budget_cents,
  monthly_budget_cents = EXCLUDED.monthly_budget_cents,
  image_enabled = EXCLUDED.image_enabled,
  batch_image_enabled = EXCLUDED.batch_image_enabled,
  image_rate_multiplier = EXCLUDED.image_rate_multiplier,
  batch_image_discount_multiplier = EXCLUDED.batch_image_discount_multiplier,
  image_price_1k_cents = EXCLUDED.image_price_1k_cents,
  image_price_2k_cents = EXCLUDED.image_price_2k_cents,
  image_price_4k_cents = EXCLUDED.image_price_4k_cents,
  video_enabled = EXCLUDED.video_enabled,
  video_rate_multiplier = EXCLUDED.video_rate_multiplier,
  video_price_480p_cents = EXCLUDED.video_price_480p_cents,
  video_price_720p_cents = EXCLUDED.video_price_720p_cents,
  video_price_1080p_cents = EXCLUDED.video_price_1080p_cents,
  peak_rate_enabled = EXCLUDED.peak_rate_enabled,
  peak_start = EXCLUDED.peak_start,
  peak_end = EXCLUDED.peak_end,
  peak_rate_multiplier = EXCLUDED.peak_rate_multiplier,
  status = EXCLUDED.status,
  sort_order = EXCLUDED.sort_order,
  updated_at = EXCLUDED.updated_at
`,
		group.ID,
		group.Name,
		group.Description,
		group.Platform,
		group.GroupType,
		group.RateMultiplier,
		group.RPMLimit,
		group.IsExclusive,
		group.DailyBudgetCents,
		group.WeeklyBudgetCents,
		group.MonthlyBudgetCents,
		group.ImageEnabled,
		group.BatchImageEnabled,
		group.ImageRateMultiplier,
		group.BatchImageDiscountMultiplier,
		group.ImagePrice1KCents,
		group.ImagePrice2KCents,
		group.ImagePrice4KCents,
		group.VideoEnabled,
		group.VideoRateMultiplier,
		group.VideoPrice480PCents,
		group.VideoPrice720PCents,
		group.VideoPrice1080PCents,
		group.PeakRateEnabled,
		group.PeakStart,
		group.PeakEnd,
		group.PeakRateMultiplier,
		group.Status,
		group.SortOrder,
		group.CreatedAt,
		group.UpdatedAt,
	)
	return err
}

func (r *PostgresRepository) ListProviderAccounts(ctx context.Context) ([]ProviderAccount, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, provider_id, name, platform, auth_type, status, schedulable, priority, weight, concurrency, rpm_limit, tpm_limit, load_factor, rate_multiplier, models, group_ids, secret_configured, secret_hint, secret_ciphertext, error_message, last_used_at, expires_at, cooldown_until, circuit_state, circuit_failure_threshold, circuit_open_seconds, consecutive_failures, circuit_opened_until, last_failure_at, temp_unschedulable_rules, temp_unschedulable_reason, created_at, updated_at
FROM provider_accounts
ORDER BY priority ASC, name ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProviderAccount
	for rows.Next() {
		var account ProviderAccount
		var models, groupIDs, tempUnschedulableRules string
		var loadFactor sql.NullInt64
		var lastUsedAt, expiresAt, cooldownUntil, circuitOpenedUntil, lastFailureAt sql.NullTime
		if err := rows.Scan(&account.ID, &account.ProviderID, &account.Name, &account.Platform, &account.AuthType, &account.Status, &account.Schedulable, &account.Priority, &account.Weight, &account.Concurrency, &account.RPMLimit, &account.TPMLimit, &loadFactor, &account.RateMultiplier, &models, &groupIDs, &account.SecretConfigured, &account.SecretHint, &account.SecretCiphertext, &account.ErrorMessage, &lastUsedAt, &expiresAt, &cooldownUntil, &account.CircuitState, &account.CircuitFailureThreshold, &account.CircuitOpenSeconds, &account.ConsecutiveFailures, &circuitOpenedUntil, &lastFailureAt, &tempUnschedulableRules, &account.TempUnschedulableReason, &account.CreatedAt, &account.UpdatedAt); err != nil {
			return nil, err
		}
		account.Models = parseStringList(models)
		account.GroupIDs = parseStringList(groupIDs)
		account.TempUnschedulableRules = parseTempUnschedulableRules(tempUnschedulableRules)
		if loadFactor.Valid {
			v := int(loadFactor.Int64)
			account.LoadFactor = &v
		}
		if lastUsedAt.Valid {
			account.LastUsedAt = &lastUsedAt.Time
		}
		if expiresAt.Valid {
			account.ExpiresAt = &expiresAt.Time
		}
		if cooldownUntil.Valid {
			account.CooldownUntil = &cooldownUntil.Time
		}
		if circuitOpenedUntil.Valid {
			account.CircuitOpenedUntil = &circuitOpenedUntil.Time
		}
		if lastFailureAt.Valid {
			account.LastFailureAt = &lastFailureAt.Time
		}
		out = append(out, account)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveProviderAccount(ctx context.Context, account ProviderAccount) error {
	models := marshalStringList(account.Models)
	groupIDs := marshalStringList(account.GroupIDs)
	tempUnschedulableRules := marshalTempUnschedulableRules(account.TempUnschedulableRules)
	var loadFactor sql.NullInt64
	if account.LoadFactor != nil {
		loadFactor = sql.NullInt64{Int64: int64(*account.LoadFactor), Valid: true}
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO provider_accounts(id, provider_id, name, platform, auth_type, status, schedulable, priority, weight, concurrency, rpm_limit, tpm_limit, load_factor, rate_multiplier, models, group_ids, secret_configured, secret_hint, secret_ciphertext, error_message, last_used_at, expires_at, cooldown_until, circuit_state, circuit_failure_threshold, circuit_open_seconds, consecutive_failures, circuit_opened_until, last_failure_at, temp_unschedulable_rules, temp_unschedulable_reason, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33)
ON CONFLICT(id) DO UPDATE SET
  provider_id = EXCLUDED.provider_id,
  name = EXCLUDED.name,
  platform = EXCLUDED.platform,
  auth_type = EXCLUDED.auth_type,
  status = EXCLUDED.status,
  schedulable = EXCLUDED.schedulable,
  priority = EXCLUDED.priority,
  weight = EXCLUDED.weight,
  concurrency = EXCLUDED.concurrency,
  rpm_limit = EXCLUDED.rpm_limit,
  tpm_limit = EXCLUDED.tpm_limit,
  load_factor = EXCLUDED.load_factor,
  rate_multiplier = EXCLUDED.rate_multiplier,
  models = EXCLUDED.models,
  group_ids = EXCLUDED.group_ids,
  secret_configured = EXCLUDED.secret_configured,
  secret_hint = EXCLUDED.secret_hint,
  secret_ciphertext = EXCLUDED.secret_ciphertext,
  error_message = EXCLUDED.error_message,
  last_used_at = EXCLUDED.last_used_at,
  expires_at = EXCLUDED.expires_at,
  cooldown_until = EXCLUDED.cooldown_until,
  circuit_state = EXCLUDED.circuit_state,
  circuit_failure_threshold = EXCLUDED.circuit_failure_threshold,
  circuit_open_seconds = EXCLUDED.circuit_open_seconds,
  consecutive_failures = EXCLUDED.consecutive_failures,
  circuit_opened_until = EXCLUDED.circuit_opened_until,
  last_failure_at = EXCLUDED.last_failure_at,
  temp_unschedulable_rules = EXCLUDED.temp_unschedulable_rules,
  temp_unschedulable_reason = EXCLUDED.temp_unschedulable_reason,
  updated_at = EXCLUDED.updated_at
`, account.ID, account.ProviderID, account.Name, account.Platform, account.AuthType, account.Status, account.Schedulable, account.Priority, account.Weight, account.Concurrency, account.RPMLimit, account.TPMLimit, loadFactor, account.RateMultiplier, models, groupIDs, account.SecretConfigured, account.SecretHint, account.SecretCiphertext, account.ErrorMessage, account.LastUsedAt, account.ExpiresAt, account.CooldownUntil, account.CircuitState, account.CircuitFailureThreshold, account.CircuitOpenSeconds, account.ConsecutiveFailures, account.CircuitOpenedUntil, account.LastFailureAt, tempUnschedulableRules, account.TempUnschedulableReason, account.CreatedAt, account.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListLatestProviderAccountHealthChecks(ctx context.Context) ([]ProviderAccountHealthCheck, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT DISTINCT ON (account_id) id, account_id, provider_id, status, latency_ms, message, models, checked_at
FROM provider_account_health_checks
ORDER BY account_id, checked_at DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderAccountHealthCheck
	for rows.Next() {
		var check ProviderAccountHealthCheck
		var models string
		if err := rows.Scan(&check.ID, &check.AccountID, &check.ProviderID, &check.Status, &check.LatencyMS, &check.Message, &models, &check.CheckedAt); err != nil {
			return nil, err
		}
		check.Models = parseStringList(models)
		out = append(out, check)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CheckedAt.After(out[j].CheckedAt) })
	return out, rows.Err()
}

func (r *PostgresRepository) SaveProviderAccountHealthCheck(ctx context.Context, check ProviderAccountHealthCheck) error {
	models := marshalStringList(check.Models)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO provider_account_health_checks(id, account_id, provider_id, status, latency_ms, message, models, checked_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)
`, check.ID, check.AccountID, check.ProviderID, check.Status, check.LatencyMS, check.Message, models, check.CheckedAt)
	return err
}

func (r *PostgresRepository) ListModelPricings(ctx context.Context) ([]ModelPricing, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, model, currency, input_price_cents_per_1m_tokens, output_price_cents_per_1m_tokens, status, created_at, updated_at
FROM model_pricings
ORDER BY model ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelPricing
	for rows.Next() {
		var pricing ModelPricing
		if err := rows.Scan(&pricing.ID, &pricing.Model, &pricing.Currency, &pricing.InputPriceCentsPer1MTokens, &pricing.OutputPriceCentsPer1MTokens, &pricing.Status, &pricing.CreatedAt, &pricing.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, pricing)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveModelPricing(ctx context.Context, pricing ModelPricing) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO model_pricings(id, model, currency, input_price_cents_per_1m_tokens, output_price_cents_per_1m_tokens, status, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(id) DO UPDATE SET
  model = EXCLUDED.model,
  currency = EXCLUDED.currency,
  input_price_cents_per_1m_tokens = EXCLUDED.input_price_cents_per_1m_tokens,
  output_price_cents_per_1m_tokens = EXCLUDED.output_price_cents_per_1m_tokens,
  status = EXCLUDED.status,
  updated_at = EXCLUDED.updated_at
`, pricing.ID, pricing.Model, pricing.Currency, pricing.InputPriceCentsPer1MTokens, pricing.OutputPriceCentsPer1MTokens, pricing.Status, pricing.CreatedAt, pricing.UpdatedAt)
	return err
}

func (r *PostgresRepository) routingGroupAccountCounts(ctx context.Context) (map[string]int, map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT status, schedulable, group_ids
FROM provider_accounts
`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	activeCounts := map[string]int{}
	for rows.Next() {
		var status, groupIDs string
		var schedulable bool
		if err := rows.Scan(&status, &schedulable, &groupIDs); err != nil {
			return nil, nil, err
		}
		for _, groupID := range parseStringList(groupIDs) {
			counts[groupID]++
			if status == AccountStatusActive && schedulable {
				activeCounts[groupID]++
			}
		}
	}
	return counts, activeCounts, rows.Err()
}

func (r *PostgresRepository) ListAPIKeys(ctx context.Context) ([]APIKeyRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, name, key_hash, fingerprint, prefix, status, key_type, customer_id, owner_user_id, policy_id, model_allowlist, qps_limit, monthly_token_limit, expires_at, last_used_at, created_at, updated_at
FROM api_keys
ORDER BY created_at DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKeyRecord
	for rows.Next() {
		var key APIKeyRecord
		var allowlist string
		var expiresAt, lastUsedAt sql.NullTime
		if err := rows.Scan(&key.ID, &key.Name, &key.KeyHash, &key.Fingerprint, &key.Prefix, &key.Status, &key.KeyType, &key.CustomerID, &key.OwnerUserID, &key.PolicyID, &allowlist, &key.QPSLimit, &key.MonthlyTokenLimit, &expiresAt, &lastUsedAt, &key.CreatedAt, &key.UpdatedAt); err != nil {
			return nil, err
		}
		key.ModelAllowlist = parseStringList(allowlist)
		if expiresAt.Valid {
			key.ExpiresAt = &expiresAt.Time
		}
		if lastUsedAt.Valid {
			key.LastUsedAt = &lastUsedAt.Time
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) FindAPIKeyByHash(ctx context.Context, hash string) (APIKeyRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, name, key_hash, fingerprint, prefix, status, key_type, customer_id, owner_user_id, policy_id, model_allowlist, qps_limit, monthly_token_limit, expires_at, last_used_at, created_at, updated_at
FROM api_keys
WHERE key_hash = $1
`, hash)
	var key APIKeyRecord
	var allowlist string
	var expiresAt, lastUsedAt sql.NullTime
	if err := row.Scan(&key.ID, &key.Name, &key.KeyHash, &key.Fingerprint, &key.Prefix, &key.Status, &key.KeyType, &key.CustomerID, &key.OwnerUserID, &key.PolicyID, &allowlist, &key.QPSLimit, &key.MonthlyTokenLimit, &expiresAt, &lastUsedAt, &key.CreatedAt, &key.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return APIKeyRecord{}, false, nil
		}
		return APIKeyRecord{}, false, err
	}
	key.ModelAllowlist = parseStringList(allowlist)
	if expiresAt.Valid {
		key.ExpiresAt = &expiresAt.Time
	}
	if lastUsedAt.Valid {
		key.LastUsedAt = &lastUsedAt.Time
	}
	return key, true, nil
}

func (r *PostgresRepository) SaveAPIKey(ctx context.Context, key APIKeyRecord) error {
	allowlist := marshalStringList(key.ModelAllowlist)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO api_keys(id, name, key_hash, fingerprint, prefix, status, key_type, customer_id, owner_user_id, policy_id, model_allowlist, qps_limit, monthly_token_limit, expires_at, last_used_at, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
ON CONFLICT(id) DO UPDATE SET
  name = EXCLUDED.name,
  key_hash = EXCLUDED.key_hash,
  fingerprint = EXCLUDED.fingerprint,
  prefix = EXCLUDED.prefix,
  status = EXCLUDED.status,
  key_type = EXCLUDED.key_type,
  customer_id = EXCLUDED.customer_id,
  owner_user_id = EXCLUDED.owner_user_id,
  policy_id = EXCLUDED.policy_id,
  model_allowlist = EXCLUDED.model_allowlist,
  qps_limit = EXCLUDED.qps_limit,
  monthly_token_limit = EXCLUDED.monthly_token_limit,
  expires_at = EXCLUDED.expires_at,
  last_used_at = EXCLUDED.last_used_at,
  updated_at = EXCLUDED.updated_at
`, key.ID, key.Name, key.KeyHash, key.Fingerprint, key.Prefix, key.Status, key.KeyType, key.CustomerID, key.OwnerUserID, key.PolicyID, allowlist, key.QPSLimit, key.MonthlyTokenLimit, key.ExpiresAt, key.LastUsedAt, key.CreatedAt, key.UpdatedAt)
	return err
}

func (r *PostgresRepository) UpdateAPIKeyLastUsed(ctx context.Context, id string, lastUsedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = $1, updated_at = $1 WHERE id = $2`, lastUsedAt, id)
	return err
}

func (r *PostgresRepository) DisableAPIKey(ctx context.Context, id string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE api_keys SET status = $1, updated_at = $2 WHERE id = $3`, APIKeyStatusDisabled, updatedAt, id)
	return err
}

func (r *PostgresRepository) SaveUsageRecord(ctx context.Context, record UsageRecord) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO usage_records(id, api_key_id, customer_id, api_fingerprint, model, upstream_model, provider_id, provider_account_id, status, error_type, latency_ms, input_tokens, output_tokens, cost_cents, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
`, record.ID, record.APIKeyID, record.CustomerID, record.APIFingerprint, record.Model, record.UpstreamModel, record.ProviderID, record.ProviderAccountID, record.Status, record.ErrorType, record.LatencyMS, record.InputTokens, record.OutputTokens, record.CostCents, record.CreatedAt)
	return err
}

func (r *PostgresRepository) ListUsageRecords(ctx context.Context, limit int) ([]UsageRecord, error) {
	return r.QueryUsageRecords(ctx, UsageQuery{Limit: limit})
}

func (r *PostgresRepository) QueryUsageRecords(ctx context.Context, query UsageQuery) ([]UsageRecord, error) {
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 100, 500)
	clauses := []string{}
	args := []any{}
	appendUsageRecordFilters(&clauses, &args, query)
	sqlText := `
SELECT id, api_key_id, customer_id, api_fingerprint, model, upstream_model, provider_id, provider_account_id, status, error_type, latency_ms, input_tokens, output_tokens, cost_cents, created_at
FROM usage_records`
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit, offset)
	sqlText += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageRecord
	for rows.Next() {
		var record UsageRecord
		if err := rows.Scan(&record.ID, &record.APIKeyID, &record.CustomerID, &record.APIFingerprint, &record.Model, &record.UpstreamModel, &record.ProviderID, &record.ProviderAccountID, &record.Status, &record.ErrorType, &record.LatencyMS, &record.InputTokens, &record.OutputTokens, &record.CostCents, &record.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SummarizeUsageRecords(ctx context.Context, query UsageQuery) (UsageAggregate, error) {
	clauses := []string{}
	args := []any{}
	appendUsageRecordFilters(&clauses, &args, query)
	sqlText := `
SELECT model,
       COUNT(*),
       COALESCE(SUM(CASE WHEN status IN ('upstream_error', 'error') OR error_type <> '' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(input_tokens + output_tokens), 0),
       COALESCE(SUM(cost_cents), 0),
       COALESCE(SUM(latency_ms), 0)
FROM usage_records`
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	sqlText += " GROUP BY model ORDER BY model ASC"
	rows, err := r.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return UsageAggregate{}, err
	}
	defer rows.Close()
	var aggregate UsageAggregate
	var latencyTotal int64
	for rows.Next() {
		var model string
		var requests int64
		var errors int64
		var tokens int64
		var costCents int64
		var modelLatencyTotal int64
		if err := rows.Scan(&model, &requests, &errors, &tokens, &costCents, &modelLatencyTotal); err != nil {
			return UsageAggregate{}, err
		}
		avgLatency := int64(0)
		if requests > 0 {
			avgLatency = modelLatencyTotal / requests
		}
		aggregate.ByModel = append(aggregate.ByModel, UsageModelSummary{
			Model:      model,
			Requests:   int(requests),
			Errors:     int(errors),
			Tokens:     int(tokens),
			CostCents:  int(costCents),
			AvgLatency: avgLatency,
		})
		aggregate.TotalRequests += int(requests)
		aggregate.ErrorRequests += int(errors)
		aggregate.TotalTokens += int(tokens)
		aggregate.TotalCostCents += int(costCents)
		latencyTotal += modelLatencyTotal
	}
	if err := rows.Err(); err != nil {
		return UsageAggregate{}, err
	}
	if aggregate.TotalRequests > 0 {
		aggregate.AvgLatencyMS = latencyTotal / int64(aggregate.TotalRequests)
	}
	return aggregate, nil
}

func (r *PostgresRepository) SumUsageTokensByAPIKeySince(ctx context.Context, apiKeyID string, since time.Time) (int, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(input_tokens + output_tokens), 0)
FROM usage_records
WHERE api_key_id = $1 AND created_at >= $2
`, apiKeyID, since)
	var total int
	if err := row.Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (r *PostgresRepository) SumUsageCostCentsByAPIKeySince(ctx context.Context, apiKeyID string, since time.Time) (int, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(cost_cents), 0)
FROM usage_records
WHERE api_key_id = $1 AND created_at >= $2
`, apiKeyID, since)
	var total int
	if err := row.Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (r *PostgresRepository) SaveGatewayTrace(ctx context.Context, trace GatewayTrace) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO gateway_traces(id, api_key_id, api_fingerprint, model, stream, message_count, provider_id, provider_account_id, gateway_model_id, route_id, route_group, upstream_model, route_source, route_reason, policy_id, policy_name, policy_source, policy_version, policy_snapshot, status, http_status, error_type, latency_ms, input_tokens, output_tokens, request_summary, response_summary, route_attempts, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29)
`, trace.ID, trace.APIKeyID, trace.APIFingerprint, trace.Model, trace.Stream, trace.MessageCount, trace.ProviderID, trace.ProviderAccountID, trace.GatewayModelID, trace.RouteID, trace.RouteGroup, trace.UpstreamModel, trace.RouteSource, trace.RouteReason, trace.PolicyID, trace.PolicyName, trace.PolicySource, trace.PolicyVersion, trace.PolicySnapshot, trace.Status, trace.HTTPStatus, trace.ErrorType, trace.LatencyMS, trace.InputTokens, trace.OutputTokens, trace.RequestSummary, trace.ResponseSummary, defaultJSONArray(trace.RouteAttempts), trace.CreatedAt)
	return err
}

func defaultJSONArray(value string) string {
	if strings.TrimSpace(value) == "" {
		return "[]"
	}
	return value
}

func (r *PostgresRepository) ListGatewayTraces(ctx context.Context, limit int) ([]GatewayTrace, error) {
	return r.QueryGatewayTraces(ctx, GatewayTraceQuery{Limit: limit})
}

func (r *PostgresRepository) QueryGatewayTraces(ctx context.Context, query GatewayTraceQuery) ([]GatewayTrace, error) {
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 100, 500)
	clauses := []string{}
	args := []any{}
	appendGatewayTraceFilters(&clauses, &args, query)
	sqlText := `
SELECT id, api_key_id, api_fingerprint, model, stream, message_count, provider_id, provider_account_id, gateway_model_id, route_id, route_group, upstream_model, route_source, route_reason, policy_id, policy_name, policy_source, policy_version, policy_snapshot, status, http_status, error_type, latency_ms, input_tokens, output_tokens, request_summary, response_summary, route_attempts, created_at
FROM gateway_traces`
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit, offset)
	sqlText += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GatewayTrace
	for rows.Next() {
		var trace GatewayTrace
		if err := rows.Scan(&trace.ID, &trace.APIKeyID, &trace.APIFingerprint, &trace.Model, &trace.Stream, &trace.MessageCount, &trace.ProviderID, &trace.ProviderAccountID, &trace.GatewayModelID, &trace.RouteID, &trace.RouteGroup, &trace.UpstreamModel, &trace.RouteSource, &trace.RouteReason, &trace.PolicyID, &trace.PolicyName, &trace.PolicySource, &trace.PolicyVersion, &trace.PolicySnapshot, &trace.Status, &trace.HTTPStatus, &trace.ErrorType, &trace.LatencyMS, &trace.InputTokens, &trace.OutputTokens, &trace.RequestSummary, &trace.ResponseSummary, &trace.RouteAttempts, &trace.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, trace)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SummarizeGatewayTraces(ctx context.Context, query GatewayTraceQuery) (GatewayTraceSummary, error) {
	clauses := []string{}
	args := []any{}
	appendGatewayTraceFilters(&clauses, &args, query)
	sqlText := `
SELECT COUNT(*),
       COALESCE(SUM(CASE WHEN provider_id <> '' OR provider_account_id <> '' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN status IN ('upstream_error', 'error') OR error_type <> '' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(input_tokens + output_tokens), 0),
       COALESCE(SUM(latency_ms), 0)
FROM gateway_traces`
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	var total int64
	var routed int64
	var errors int64
	var tokens int64
	var latencyTotal int64
	if err := r.db.QueryRowContext(ctx, sqlText, args...).Scan(&total, &routed, &errors, &tokens, &latencyTotal); err != nil {
		return GatewayTraceSummary{}, err
	}
	summary := GatewayTraceSummary{Total: int(total), Routed: int(routed), Errors: int(errors), Tokens: int(tokens)}
	if total > 0 {
		summary.AvgLatencyMS = latencyTotal / total
	}
	return summary, nil
}

func (r *PostgresRepository) ListAuditLogs(ctx context.Context, limit int) ([]AuditLog, error) {
	return r.QueryAuditLogs(ctx, AuditLogQuery{Limit: limit})
}

func (r *PostgresRepository) QueryAuditLogs(ctx context.Context, query AuditLogQuery) ([]AuditLog, error) {
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 50, 500)
	clauses := []string{}
	args := []any{}
	appendAuditLogFilters(&clauses, &args, query)
	sqlText := `
SELECT id, actor, action, resource_type, resource_id, summary, created_at
FROM audit_logs`
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit, offset)
	sqlText += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditLog
	for rows.Next() {
		var event AuditLog
		if err := rows.Scan(&event.ID, &event.Actor, &event.Action, &event.ResourceType, &event.ResourceID, &event.Summary, &event.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SummarizeAuditLogs(ctx context.Context, query AuditLogQuery) (AuditLogSummary, error) {
	clauses := []string{}
	args := []any{}
	appendAuditLogFilters(&clauses, &args, query)
	sqlText := `
SELECT COUNT(*),
       COUNT(DISTINCT NULLIF(actor, '')),
       COUNT(DISTINCT NULLIF(resource_type, '')),
       COUNT(DISTINCT NULLIF(action, ''))
FROM audit_logs`
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	var total int64
	var actors int64
	var resources int64
	var actions int64
	if err := r.db.QueryRowContext(ctx, sqlText, args...).Scan(&total, &actors, &resources, &actions); err != nil {
		return AuditLogSummary{}, err
	}
	return AuditLogSummary{Total: int(total), Actors: int(actors), Resources: int(resources), Actions: int(actions)}, nil
}

func (r *PostgresRepository) AddAuditLog(ctx context.Context, event AuditLog) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO audit_logs(id, actor, action, resource_type, resource_id, summary, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7)
`, event.ID, event.Actor, event.Action, event.ResourceType, event.ResourceID, event.Summary, event.CreatedAt)
	return err
}

func (r *PostgresRepository) Health(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func (r *PostgresRepository) Close() error {
	return r.db.Close()
}

func marshalStringList(values []string) string {
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func parseStringList(value string) []string {
	var out []string
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return []string{}
	}
	return out
}

func marshalTempUnschedulableRules(rules []ProviderAccountTempUnschedulableRule) string {
	raw, err := json.Marshal(rules)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func parseTempUnschedulableRules(value string) []ProviderAccountTempUnschedulableRule {
	var out []ProviderAccountTempUnschedulableRule
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return []ProviderAccountTempUnschedulableRule{}
	}
	return out
}
