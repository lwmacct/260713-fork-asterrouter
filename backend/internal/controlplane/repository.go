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
	ListPlatformTenants(ctx context.Context) ([]PlatformTenant, error)
	SavePlatformTenant(ctx context.Context, tenant PlatformTenant) error
	ListGatewayPrincipals(ctx context.Context) ([]GatewayPrincipal, error)
	SaveGatewayPrincipal(ctx context.Context, principal GatewayPrincipal) error
	ListExternalAuthIntegrations(ctx context.Context) ([]ExternalAuthIntegration, error)
	SaveExternalAuthIntegration(ctx context.Context, integration ExternalAuthIntegration) error
	ListPlatformUsageSinks(ctx context.Context) ([]PlatformUsageSink, error)
	SavePlatformUsageSink(ctx context.Context, sink PlatformUsageSink) error
	QueryPlatformUsageDeliveryEvents(ctx context.Context, query PlatformUsageDeliveryQuery) ([]PlatformUsageDeliveryEvent, error)
	SaveUsageRecordAndEnqueuePlatformUsage(ctx context.Context, record UsageRecord, events []PlatformUsageDeliveryEvent) error
	ClaimDuePlatformUsageDeliveryEvents(ctx context.Context, now, leaseUntil time.Time, leaseToken string, limit int) ([]PlatformUsageDeliveryEvent, error)
	CompletePlatformUsageDeliveryEvent(ctx context.Context, id, leaseToken string, deliveredAt time.Time, httpStatus int) error
	ReschedulePlatformUsageDeliveryEvent(ctx context.Context, id, leaseToken string, nextAttemptAt time.Time, httpStatus int, lastError string, deadLetter bool, updatedAt time.Time) error
	RequeuePlatformUsageDeliveryEvent(ctx context.Context, id string, nextAttemptAt time.Time) error
	CreateAIOperation(ctx context.Context, operation AIOperation) (AIOperation, bool, error)
	CreateAIOperationWithBillingHold(ctx context.Context, operation AIOperation, admission BillingHoldAdmission) (AIOperation, bool, error)
	FindAIOperation(ctx context.Context, id string) (AIOperation, bool, error)
	MarkAIOperationRunning(ctx context.Context, id string, updatedAt time.Time) (bool, error)
	CompleteAIOperation(ctx context.Context, id, status, errorType string, completedAt time.Time) (bool, error)
	CreateOrGetRealtimeSession(ctx context.Context, session RealtimeSession) (RealtimeSession, bool, error)
	FindRealtimeSession(ctx context.Context, id string) (RealtimeSession, bool, error)
	UpdateRealtimeSession(ctx context.Context, id string, expectedVersion int, update RealtimeSessionUpdate) (RealtimeSession, bool, error)
	CreateDurableAIJob(ctx context.Context, operation AIOperation, job AIJob, event AIJobEvent, outbox TransactionalOutboxEvent, limits AIJobAdmissionLimits, billing BillingHoldAdmission) (AIJob, bool, error)
	FindAIJob(ctx context.Context, id string) (AIJob, bool, error)
	FindAIJobByOperationID(ctx context.Context, operationID string) (AIJob, bool, error)
	FindOwnedAIJob(ctx context.Context, id string, owner AIJobOwner) (AIJob, bool, error)
	RequestAIJobCancellation(ctx context.Context, id string, owner AIJobOwner, requestedAt time.Time) (AIJob, bool, bool, error)
	QueryAIJobs(ctx context.Context, query AIJobQuery) ([]AIJob, error)
	SummarizeAIJobs(ctx context.Context, query AIJobQuery) (AIJobSummary, error)
	RequestAIJobAdminCancellation(ctx context.Context, id string, requestedAt time.Time, audit AuditLog) (AIJob, bool, bool, error)
	ClaimQueuedAIJobs(ctx context.Context, now, leaseUntil time.Time, workerID, leaseToken string, limit int) ([]AIJob, error)
	ClaimAIJobsByReadyReferences(ctx context.Context, references []AIJobReadyReference, now, leaseUntil time.Time, workerID, leaseToken string, limit int) ([]AIJob, error)
	ListAIJobsForReadyIndex(ctx context.Context, limit int) ([]AIJob, error)
	ListAIJobsForDeliveryRebuild(ctx context.Context, now time.Time, limit int) ([]AIJob, error)
	ExtendAIJobQueueLease(ctx context.Context, id string, expectedVersion int, fenceToken int64, leaseToken string, leaseUntil, extendedAt time.Time) (AIJob, bool, error)
	TransitionAIJob(ctx context.Context, id string, expectedVersion int, fenceToken int64, toStatus, reason string, transitionedAt time.Time) (AIJob, bool, error)
	RequeueAIJob(ctx context.Context, id string, expectedVersion int, fenceToken int64, reason string, nextEligibleAt, transitionedAt time.Time) (AIJob, bool, error)
	ListAIJobEvents(ctx context.Context, jobID string) ([]AIJobEvent, error)
	AppendAIJobProgressEvent(ctx context.Context, event AIJobProgressEvent) (AIJobProgressEvent, bool, error)
	ListAIJobProgressEvents(ctx context.Context, jobID string) ([]AIJobProgressEvent, error)
	CreateArtifact(ctx context.Context, artifact Artifact, event ArtifactEvent, outbox TransactionalOutboxEvent) error
	FindArtifact(ctx context.Context, id string) (Artifact, bool, error)
	FindOwnedArtifact(ctx context.Context, id string, owner ArtifactOwner) (Artifact, bool, error)
	QueryArtifacts(ctx context.Context, query ArtifactQuery) ([]Artifact, error)
	SummarizeArtifacts(ctx context.Context, query ArtifactQuery) (ArtifactSummary, error)
	TransitionArtifact(ctx context.Context, input ArtifactTransitionInput, transitionedAt time.Time) (Artifact, bool, error)
	ListArtifactEvents(ctx context.Context, artifactID string) ([]ArtifactEvent, error)
	CreateAIAttempt(ctx context.Context, attempt AIAttempt) error
	CreateOrGetAIAttempt(ctx context.Context, attempt AIAttempt) (AIAttempt, bool, error)
	FindAIAttempt(ctx context.Context, id string) (AIAttempt, bool, error)
	ListAIAttemptsByOperationID(ctx context.Context, operationID string) ([]AIAttempt, error)
	UpdateAIAttemptDispatch(ctx context.Context, attempt AIAttempt, expectedVersion int) (AIAttempt, bool, error)
	ScheduleAIAttemptReconciliation(ctx context.Context, id string, expectedVersion int, scheduledAt time.Time, audit AuditLog) (AIAttempt, bool, error)
	ScheduleArtifactDeliveryRetry(ctx context.Context, artifactID string, attempt AIAttempt, expectedVersion int, audit AuditLog) (AIAttempt, bool, error)
	ListAIAttemptsForReconciliation(ctx context.Context, now time.Time, limit int) ([]AIAttempt, error)
	ListDirectAIAttemptsForReconciliation(ctx context.Context, now time.Time, limit int) ([]AIAttempt, error)
	ListDurableAIAttemptsForReconciliation(ctx context.Context, now time.Time, limit int) ([]AIAttempt, error)
	CompleteAIAttempt(ctx context.Context, id, status, errorType string, completedAt time.Time) (bool, error)
	FindProviderCallbackReceipt(ctx context.Context, eventID string) (ProviderCallbackReceipt, bool, error)
	CreateOrGetProviderCallbackReceipt(ctx context.Context, receipt ProviderCallbackReceipt) (ProviderCallbackReceipt, bool, error)
	CompleteProviderCallbackReceipt(ctx context.Context, eventID, status, errorType string, processedAt time.Time) error
	FindBillingHoldByOperationID(ctx context.Context, operationID string) (BillingHold, bool, error)
	TransitionBillingHold(ctx context.Context, operationID string, expectedVersion int, toStatus string, settledAmount int, reason string, transitionedAt time.Time) (BillingHold, bool, error)
	ListBillingHolds(ctx context.Context) ([]BillingHold, error)
	ApplyUsageLedger(ctx context.Context, record UsageRecord, billing BillingLedgerEntry, outbox TransactionalOutboxEvent, events []PlatformUsageDeliveryEvent) (bool, error)
	ListBillingLedgerEntries(ctx context.Context, operationID string) ([]BillingLedgerEntry, error)
	ListTransactionalOutboxEvents(ctx context.Context, aggregateID string) ([]TransactionalOutboxEvent, error)
	ClaimDueTransactionalOutboxEvents(ctx context.Context, now, leaseUntil time.Time, leaseToken string, limit int) ([]TransactionalOutboxEvent, error)
	CompleteTransactionalOutboxEvent(ctx context.Context, id, leaseToken string, publishedAt time.Time) error
	RescheduleTransactionalOutboxEvent(ctx context.Context, id, leaseToken string, nextAttemptAt time.Time, lastError string, deadLetter bool, updatedAt time.Time) error
	RequeueTransactionalOutboxEvent(ctx context.Context, id string, nextAttemptAt time.Time) error
	GetCustomerWallet(ctx context.Context, userID string) (CustomerWallet, error)
	ListCustomerBillingEntries(ctx context.Context, query CustomerBillingQuery) ([]CustomerBillingEntry, int, error)
	ListAvailableCustomerVouchers(ctx context.Context, userID string, now time.Time) ([]CustomerVoucher, error)
	RedeemCustomerCode(ctx context.Context, request CustomerCodeRedemption) (CustomerBillingEntry, error)
	SaveCustomerRedemptionCode(ctx context.Context, code CustomerRedemptionCode) error
	GetCustomerNotificationPreferences(ctx context.Context, userID string) ([]CustomerNotificationPreference, error)
	SaveCustomerNotificationPreferences(ctx context.Context, userID string, preferences []CustomerNotificationPreference, updatedAt time.Time) error
	CreateCustomerNotification(ctx context.Context, notification CustomerNotification) (bool, error)
	ListCustomerNotifications(ctx context.Context, query CustomerNotificationQuery) ([]CustomerNotification, int, int, error)
	MarkCustomerNotificationRead(ctx context.Context, userID, id string, readAt time.Time) (bool, error)
	MarkAllCustomerNotificationsRead(ctx context.Context, userID string, readAt time.Time) (int, error)
	SaveCustomerNotificationDelivery(ctx context.Context, delivery CustomerNotificationDelivery) error
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
	ListProviderAccountModels(ctx context.Context, accountID string) ([]ProviderAccountModel, error)
	SaveProviderAccountWithModels(ctx context.Context, account ProviderAccount, models []ProviderAccountModel) error
	ListGatewayModels(ctx context.Context) ([]GatewayModel, error)
	SaveGatewayModel(ctx context.Context, model GatewayModel) error
	DeleteGatewayModel(ctx context.Context, id string) error
	ListModelRoutes(ctx context.Context) ([]ModelRoute, error)
	SaveModelRoute(ctx context.Context, route ModelRoute) error
	SaveModelRoutes(ctx context.Context, routes []ModelRoute) error
	DeleteModelRoute(ctx context.Context, id string) error
	ListLatestProviderAccountHealthChecks(ctx context.Context) ([]ProviderAccountHealthCheck, error)
	SaveProviderAccountHealthCheck(ctx context.Context, check ProviderAccountHealthCheck) error
	ListModelPricings(ctx context.Context) ([]ModelPricing, error)
	SaveModelPricing(ctx context.Context, pricing ModelPricing) error
	ListProcurementPrices(ctx context.Context) ([]ProcurementPrice, error)
	SaveProcurementPrice(ctx context.Context, price ProcurementPrice) error
	ListProviderBillingLines(ctx context.Context) ([]ProviderBillingLine, error)
	SaveProviderBillingLine(ctx context.Context, line ProviderBillingLine) error
	SaveProviderBillingLineAndReconcileUsage(ctx context.Context, line ProviderBillingLine, record UsageRecord) error
	ListProviderBillingSources(ctx context.Context) ([]ProviderBillingSource, error)
	FindProviderBillingSource(ctx context.Context, idOrAccountID string) (ProviderBillingSource, bool, error)
	UpsertProviderBillingSource(ctx context.Context, source ProviderBillingSource, expectedVersion *int64) (bool, error)
	ClaimProviderBillingSources(ctx context.Context, request ProviderBillingSourceClaimRequest) ([]ProviderBillingSourceClaim, error)
	CommitProviderBillingSync(ctx context.Context, commit ProviderBillingSyncCommit) (bool, error)
	ListProviderBillingSyncRuns(ctx context.Context, sourceID string, limit int) ([]ProviderBillingSyncRun, error)
	ListProviderBalanceSnapshots(ctx context.Context, sourceID string, limit int) ([]ProviderBalanceSnapshotRecord, error)
	ListLatestProviderBalanceSnapshots(ctx context.Context) ([]ProviderBalanceSnapshotRecord, error)
	ListProviderUsageAggregateSnapshots(ctx context.Context, sourceID string, limit int) ([]ProviderUsageAggregateSnapshot, error)
	ListProviderCacheCapabilities(ctx context.Context) ([]ProviderCacheCapability, error)
	FindProviderCacheCapability(ctx context.Context, providerAccountID, upstreamModel, protocol string) (ProviderCacheCapability, bool, error)
	SaveProviderCacheCapability(ctx context.Context, capability ProviderCacheCapability) error
	UpsertProviderCacheProductionMetrics(ctx context.Context, metrics ProviderCacheProductionMetrics) error
	ListProviderCacheProbeRuns(ctx context.Context, limit int) ([]ProviderCacheProbeRun, error)
	ReserveProviderCacheProbeRun(ctx context.Context, run ProviderCacheProbeRun, limits CacheProbeReservationLimits) (bool, string, error)
	SaveProviderCacheProbeRun(ctx context.Context, run ProviderCacheProbeRun) error
	GetEffectivePricingPolicy(ctx context.Context) (EffectivePricingPolicy, bool, error)
	SaveEffectivePricingPolicy(ctx context.Context, policy EffectivePricingPolicy) error
	ListEffectivePriceSnapshots(ctx context.Context) ([]EffectivePriceSnapshot, error)
	SaveEffectivePriceSnapshot(ctx context.Context, snapshot EffectivePriceSnapshot) error
	ListEffectivePricingDecisions(ctx context.Context) ([]EffectivePricingDecision, error)
	SaveEffectivePricingDecision(ctx context.Context, decision EffectivePricingDecision) error
	UpdateEffectivePricingDecision(ctx context.Context, decision EffectivePricingDecision, expectedStatus string, expectedUpdatedAt time.Time) (bool, error)
	ListEffectivePricingDecisionEvaluations(ctx context.Context, decisionID string, limit int) ([]EffectivePricingDecisionEvaluation, error)
	CommitEffectivePricingDecisionEvaluation(ctx context.Context, commit EffectivePricingDecisionEvaluationCommit) (bool, error)
	FindRoutingAffinityBinding(ctx context.Context, scopeKey string, now time.Time) (RoutingAffinityBinding, bool, error)
	SaveRoutingAffinityBinding(ctx context.Context, binding RoutingAffinityBinding) error
	DeleteRoutingAffinityBinding(ctx context.Context, scopeKey string) error
	SummarizeEffectivePricingUsage(ctx context.Context, from, to time.Time) ([]EffectivePricingUsageAggregate, error)
	ListAPIKeys(ctx context.Context) ([]APIKeyRecord, error)
	FindAPIKeyByHash(ctx context.Context, hash string) (APIKeyRecord, bool, error)
	SaveAPIKey(ctx context.Context, key APIKeyRecord) error
	RotateAPIKeyPair(ctx context.Context, previous APIKeyRecord, replacement APIKeyRecord, audit AuditLog, expectedUpdatedAt time.Time) error
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
	mu                              sync.RWMutex
	providers                       map[string]ProviderConnection
	healthChecks                    map[string]ProviderHealthCheck
	departments                     map[string]Department
	organizationGroups              map[string]OrganizationGroup
	governancePolicies              map[string]GovernancePolicy
	workspaceUsers                  map[string]WorkspaceUser
	platformTenants                 map[string]PlatformTenant
	gatewayPrincipals               map[string]GatewayPrincipal
	externalAuthIntegrations        map[string]ExternalAuthIntegration
	platformUsageSinks              map[string]PlatformUsageSink
	platformUsageDeliveryEvents     map[string]PlatformUsageDeliveryEvent
	customerWallets                 map[string]CustomerWallet
	customerEntries                 map[string]CustomerBillingEntry
	customerCodes                   map[string]CustomerRedemptionCode
	customerRedemptions             map[string]CustomerRedemption
	customerVouchers                map[string]CustomerVoucher
	customerNotificationPreferences map[string]map[string]CustomerNotificationPreference
	customerNotifications           map[string]CustomerNotification
	customerNotificationDeliveries  map[string]CustomerNotificationDelivery
	authIdentities                  map[string]AuthIdentity
	roleBindings                    map[string]RoleBinding
	groups                          map[string]RoutingGroup
	accounts                        map[string]ProviderAccount
	accountModels                   map[string]map[string]ProviderAccountModel
	gatewayModels                   map[string]GatewayModel
	modelRoutes                     map[string]ModelRoute
	accountHealthChecks             map[string]ProviderAccountHealthCheck
	modelPricings                   map[string]ModelPricing
	procurementPrices               map[string]ProcurementPrice
	providerBillingLines            map[string]ProviderBillingLine
	providerBillingSources          map[string]ProviderBillingSource
	providerBillingSyncRuns         map[string]ProviderBillingSyncRun
	providerBalanceSnapshots        map[string]ProviderBalanceSnapshotRecord
	providerUsageAggregateSnapshots map[string]ProviderUsageAggregateSnapshot
	providerCacheCapabilities       map[string]ProviderCacheCapability
	providerCacheProbeRuns          map[string]ProviderCacheProbeRun
	effectivePricingPolicies        map[string]EffectivePricingPolicy
	effectivePriceSnapshots         map[string]EffectivePriceSnapshot
	effectivePricingDecisions       map[string]EffectivePricingDecision
	effectivePricingEvaluations     map[string]EffectivePricingDecisionEvaluation
	routingAffinityBindings         map[string]RoutingAffinityBinding
	apiKeys                         map[string]APIKeyRecord
	aiOperations                    map[string]AIOperation
	realtimeSessions                map[string]RealtimeSession
	aiJobs                          map[string]AIJob
	aiJobEvents                     map[string]AIJobEvent
	aiJobProgressEvents             map[string]AIJobProgressEvent
	artifacts                       map[string]Artifact
	artifactEvents                  map[string]ArtifactEvent
	aiAttempts                      map[string]AIAttempt
	providerCallbackReceipts        map[string]ProviderCallbackReceipt
	billingHolds                    map[string]BillingHold
	billingLedgerEntries            map[string]BillingLedgerEntry
	transactionalOutboxEvents       map[string]TransactionalOutboxEvent
	credentialRateSamples           map[string][]credentialRateSample
	credentialCapacityLeases        map[string]CredentialCapacityLease
	riskBlocks                      map[string]GatewayRiskBlock
	usageRecords                    map[string]UsageRecord
	gatewayTraces                   map[string]GatewayTrace
	auditLogs                       map[string]AuditLog
	alertEvents                     map[string]AlertEvent
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		providers:                       map[string]ProviderConnection{},
		healthChecks:                    map[string]ProviderHealthCheck{},
		departments:                     map[string]Department{},
		organizationGroups:              map[string]OrganizationGroup{},
		governancePolicies:              map[string]GovernancePolicy{},
		workspaceUsers:                  map[string]WorkspaceUser{},
		platformTenants:                 map[string]PlatformTenant{},
		gatewayPrincipals:               map[string]GatewayPrincipal{},
		externalAuthIntegrations:        map[string]ExternalAuthIntegration{},
		platformUsageSinks:              map[string]PlatformUsageSink{},
		platformUsageDeliveryEvents:     map[string]PlatformUsageDeliveryEvent{},
		customerWallets:                 map[string]CustomerWallet{},
		customerEntries:                 map[string]CustomerBillingEntry{},
		customerCodes:                   map[string]CustomerRedemptionCode{},
		customerRedemptions:             map[string]CustomerRedemption{},
		customerVouchers:                map[string]CustomerVoucher{},
		customerNotificationPreferences: map[string]map[string]CustomerNotificationPreference{},
		customerNotifications:           map[string]CustomerNotification{},
		customerNotificationDeliveries:  map[string]CustomerNotificationDelivery{},
		authIdentities:                  map[string]AuthIdentity{},
		roleBindings:                    map[string]RoleBinding{},
		groups:                          map[string]RoutingGroup{},
		accounts:                        map[string]ProviderAccount{},
		accountModels:                   map[string]map[string]ProviderAccountModel{},
		gatewayModels:                   map[string]GatewayModel{},
		modelRoutes:                     map[string]ModelRoute{},
		accountHealthChecks:             map[string]ProviderAccountHealthCheck{},
		modelPricings:                   map[string]ModelPricing{},
		procurementPrices:               map[string]ProcurementPrice{},
		providerBillingLines:            map[string]ProviderBillingLine{},
		providerBillingSources:          map[string]ProviderBillingSource{},
		providerBillingSyncRuns:         map[string]ProviderBillingSyncRun{},
		providerBalanceSnapshots:        map[string]ProviderBalanceSnapshotRecord{},
		providerUsageAggregateSnapshots: map[string]ProviderUsageAggregateSnapshot{},
		providerCacheCapabilities:       map[string]ProviderCacheCapability{},
		providerCacheProbeRuns:          map[string]ProviderCacheProbeRun{},
		effectivePricingPolicies:        map[string]EffectivePricingPolicy{},
		effectivePriceSnapshots:         map[string]EffectivePriceSnapshot{},
		effectivePricingDecisions:       map[string]EffectivePricingDecision{},
		effectivePricingEvaluations:     map[string]EffectivePricingDecisionEvaluation{},
		routingAffinityBindings:         map[string]RoutingAffinityBinding{},
		apiKeys:                         map[string]APIKeyRecord{},
		aiOperations:                    map[string]AIOperation{},
		realtimeSessions:                map[string]RealtimeSession{},
		aiJobs:                          map[string]AIJob{},
		aiJobEvents:                     map[string]AIJobEvent{},
		aiJobProgressEvents:             map[string]AIJobProgressEvent{},
		artifacts:                       map[string]Artifact{},
		artifactEvents:                  map[string]ArtifactEvent{},
		aiAttempts:                      map[string]AIAttempt{},
		providerCallbackReceipts:        map[string]ProviderCallbackReceipt{},
		billingHolds:                    map[string]BillingHold{},
		billingLedgerEntries:            map[string]BillingLedgerEntry{},
		transactionalOutboxEvents:       map[string]TransactionalOutboxEvent{},
		credentialRateSamples:           map[string][]credentialRateSample{},
		credentialCapacityLeases:        map[string]CredentialCapacityLease{},
		riskBlocks:                      map[string]GatewayRiskBlock{},
		usageRecords:                    map[string]UsageRecord{},
		gatewayTraces:                   map[string]GatewayTrace{},
		auditLogs:                       map[string]AuditLog{},
		alertEvents:                     map[string]AlertEvent{},
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

func (r *MemoryRepository) ListProviderAccountModels(_ context.Context, accountID string) ([]ProviderAccountModel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stored := r.accountModels[accountID]
	out := make([]ProviderAccountModel, 0, len(stored))
	for _, model := range stored {
		out = append(out, model)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModelID < out[j].ModelID })
	return out, nil
}

func (r *MemoryRepository) SaveProviderAccountWithModels(_ context.Context, account ProviderAccount, models []ProviderAccountModel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accounts[account.ID] = account
	stored := make(map[string]ProviderAccountModel, len(models))
	for _, model := range models {
		stored[model.ModelID] = model
	}
	r.accountModels[account.ID] = stored
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

func (r *MemoryRepository) RotateAPIKeyPair(_ context.Context, previous APIKeyRecord, replacement APIKeyRecord, audit AuditLog, expectedUpdatedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, found := r.apiKeys[previous.ID]
	if !found {
		return fmt.Errorf("api key %q not found", previous.ID)
	}
	if current.ReplacedByKeyID != "" {
		return ErrAPIKeyAlreadyRotated
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return ErrAPIKeyChangedDuringRotation
	}
	if _, exists := r.apiKeys[replacement.ID]; exists {
		return fmt.Errorf("replacement api key %q already exists", replacement.ID)
	}
	if _, exists := r.auditLogs[audit.ID]; exists {
		return fmt.Errorf("audit log %q already exists", audit.ID)
	}
	r.apiKeys[previous.ID] = previous
	r.apiKeys[replacement.ID] = replacement
	r.auditLogs[audit.ID] = audit
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
	usageDimensions, err := NormalizeUsageDimensions(record.UsageDimensions)
	if err != nil {
		return err
	}
	record.UsageDimensions = usageDimensions
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
	if query.ID != "" && record.ID != query.ID {
		return false
	}
	if query.APIKeyID != "" && record.APIKeyID != query.APIKeyID {
		return false
	}
	if len(query.APIKeyIDs) > 0 && !contains(query.APIKeyIDs, record.APIKeyID) {
		return false
	}
	if query.CustomerID != "" && record.CustomerID != query.CustomerID {
		return false
	}
	if query.ProfileScope != "" && record.ProfileScope != query.ProfileScope {
		return false
	}
	if query.PlatformTenantID != "" && record.PlatformTenantID != query.PlatformTenantID {
		return false
	}
	if query.GatewayPrincipalID != "" && record.GatewayPrincipalID != query.GatewayPrincipalID {
		return false
	}
	if query.ExternalAuthIntegrationID != "" && record.ExternalAuthIntegrationID != query.ExternalAuthIntegrationID {
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
	if query.UpstreamRequestID != "" && record.UpstreamRequestID != query.UpstreamRequestID {
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
	return anyContains(keyword, record.Model, record.Status, record.ErrorType, record.ProviderID, record.ProviderAccountID, record.APIKeyID, record.CustomerID, record.APIFingerprint, record.ProfileScope, record.PlatformTenantID, record.PlatformTenantName, record.GatewayPrincipalID, record.GatewayPrincipalName, record.ExternalAuthIntegrationID, record.ExternalSubjectReference)
}

func memoryGatewayTraceMatches(trace GatewayTrace, query GatewayTraceQuery) bool {
	if query.APIKeyID != "" && trace.APIKeyID != query.APIKeyID {
		return false
	}
	if len(query.APIKeyIDs) > 0 && !contains(query.APIKeyIDs, trace.APIKeyID) {
		return false
	}
	if query.ProfileScope != "" && trace.ProfileScope != query.ProfileScope {
		return false
	}
	if query.PlatformTenantID != "" && trace.PlatformTenantID != query.PlatformTenantID {
		return false
	}
	if query.GatewayPrincipalID != "" && trace.GatewayPrincipalID != query.GatewayPrincipalID {
		return false
	}
	if query.ExternalAuthIntegrationID != "" && trace.ExternalAuthIntegrationID != query.ExternalAuthIntegrationID {
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
	return anyContains(keyword, trace.Model, trace.Status, trace.ErrorType, trace.ProviderID, trace.ProviderAccountID, trace.RouteSource, trace.RouteReason, trace.PolicyID, trace.PolicyName, trace.PolicySource, trace.PolicySnapshot, trace.APIKeyID, trace.APIFingerprint, trace.RequestSummary, trace.ResponseSummary, trace.ProfileScope, trace.PlatformTenantID, trace.PlatformTenantName, trace.GatewayPrincipalID, trace.GatewayPrincipalName, trace.ExternalAuthIntegrationID, trace.ExternalSubjectReference)
}

func memoryAuditLogMatches(event AuditLog, query AuditLogQuery) bool {
	if query.Action != "" && event.Action != query.Action {
		return false
	}
	if query.ResourceType != "" && event.ResourceType != query.ResourceType {
		return false
	}
	if query.ProfileScope != "" && event.ProfileScope != query.ProfileScope {
		return false
	}
	if query.PlatformTenantID != "" && event.PlatformTenantID != query.PlatformTenantID {
		return false
	}
	if query.GatewayPrincipalID != "" && event.GatewayPrincipalID != query.GatewayPrincipalID {
		return false
	}
	if query.ExternalAuthIntegrationID != "" && event.ExternalAuthIntegrationID != query.ExternalAuthIntegrationID {
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
	return anyContains(keyword, event.Actor, event.Action, event.ResourceType, event.ResourceID, event.Summary, event.ProfileScope, event.PlatformTenantID, event.PlatformTenantName, event.GatewayPrincipalID, event.GatewayPrincipalName, event.ExternalAuthIntegrationID, event.ExternalSubjectReference)
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
	appendExactFilter(clauses, args, "id", query.ID)
	appendExactFilter(clauses, args, "api_key_id", query.APIKeyID)
	appendAnyExactFilter(clauses, args, "api_key_id", query.APIKeyIDs)
	appendExactFilter(clauses, args, "customer_id", query.CustomerID)
	appendExactFilter(clauses, args, "profile_scope", query.ProfileScope)
	appendExactFilter(clauses, args, "platform_tenant_id", query.PlatformTenantID)
	appendExactFilter(clauses, args, "gateway_principal_id", query.GatewayPrincipalID)
	appendExactFilter(clauses, args, "external_auth_integration_id", query.ExternalAuthIntegrationID)
	appendExactFilter(clauses, args, "model", query.Model)
	appendExactFilter(clauses, args, "provider_id", query.ProviderID)
	appendExactFilter(clauses, args, "provider_account_id", query.AccountID)
	appendExactFilter(clauses, args, "upstream_request_id", query.UpstreamRequestID)
	appendExactFilter(clauses, args, "status", query.Status)
	appendTimeFilter(clauses, args, "created_at", ">=", query.CreatedFrom)
	appendTimeFilter(clauses, args, "created_at", "<=", query.CreatedTo)
	appendSearchFilter(clauses, args, query.Search, []string{"model", "status", "error_type", "provider_id", "provider_account_id", "api_key_id", "customer_id", "api_fingerprint", "profile_scope", "platform_tenant_id", "platform_tenant_name", "gateway_principal_id", "gateway_principal_name", "external_auth_integration_id", "external_subject_reference"})
}

func appendGatewayTraceFilters(clauses *[]string, args *[]any, query GatewayTraceQuery) {
	appendExactFilter(clauses, args, "api_key_id", query.APIKeyID)
	appendAnyExactFilter(clauses, args, "api_key_id", query.APIKeyIDs)
	appendExactFilter(clauses, args, "profile_scope", query.ProfileScope)
	appendExactFilter(clauses, args, "platform_tenant_id", query.PlatformTenantID)
	appendExactFilter(clauses, args, "gateway_principal_id", query.GatewayPrincipalID)
	appendExactFilter(clauses, args, "external_auth_integration_id", query.ExternalAuthIntegrationID)
	appendExactFilter(clauses, args, "model", query.Model)
	appendExactFilter(clauses, args, "status", query.Status)
	appendTimeFilter(clauses, args, "created_at", ">=", query.CreatedFrom)
	appendTimeFilter(clauses, args, "created_at", "<=", query.CreatedTo)
	appendSearchFilter(clauses, args, query.Search, []string{"model", "status", "error_type", "provider_id", "provider_account_id", "route_source", "route_reason", "policy_id", "policy_name", "policy_source", "policy_snapshot", "api_key_id", "api_fingerprint", "request_summary", "response_summary", "profile_scope", "platform_tenant_id", "platform_tenant_name", "gateway_principal_id", "gateway_principal_name", "external_auth_integration_id", "external_subject_reference"})
}

func appendAuditLogFilters(clauses *[]string, args *[]any, query AuditLogQuery) {
	appendExactFilter(clauses, args, "action", query.Action)
	appendExactFilter(clauses, args, "resource_type", query.ResourceType)
	appendExactFilter(clauses, args, "profile_scope", query.ProfileScope)
	appendExactFilter(clauses, args, "platform_tenant_id", query.PlatformTenantID)
	appendExactFilter(clauses, args, "gateway_principal_id", query.GatewayPrincipalID)
	appendExactFilter(clauses, args, "external_auth_integration_id", query.ExternalAuthIntegrationID)
	appendTimeFilter(clauses, args, "created_at", ">=", query.CreatedFrom)
	appendTimeFilter(clauses, args, "created_at", "<=", query.CreatedTo)
	appendSearchFilter(clauses, args, query.Search, []string{"actor", "action", "resource_type", "resource_id", "summary", "profile_scope", "platform_tenant_id", "platform_tenant_name", "gateway_principal_id", "gateway_principal_name", "external_auth_integration_id", "external_subject_reference"})
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
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS session_version BIGINT NOT NULL DEFAULT 1;

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

CREATE TABLE IF NOT EXISTS customer_notification_preferences (
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  channels TEXT NOT NULL DEFAULT '[]',
  threshold DOUBLE PRECISION,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY(user_id, event_type),
  CHECK (threshold IS NULL OR threshold >= 0)
);

CREATE TABLE IF NOT EXISTS customer_notifications (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  title TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  link TEXT NOT NULL DEFAULT '',
  dedupe_key TEXT NOT NULL DEFAULT '',
  visible_in_app BOOLEAN NOT NULL DEFAULT TRUE,
  is_read BOOLEAN NOT NULL DEFAULT FALSE,
  read_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS customer_notifications_user_dedupe_idx
  ON customer_notifications(user_id, dedupe_key)
  WHERE dedupe_key <> '';

CREATE INDEX IF NOT EXISTS customer_notifications_user_created_idx
  ON customer_notifications(user_id, visible_in_app, created_at DESC);

CREATE INDEX IF NOT EXISTS customer_notifications_user_unread_idx
  ON customer_notifications(user_id, is_read, created_at DESC)
  WHERE visible_in_app = TRUE;

CREATE TABLE IF NOT EXISTS customer_notification_deliveries (
  id TEXT PRIMARY KEY,
  notification_id TEXT NOT NULL REFERENCES customer_notifications(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  channel TEXT NOT NULL,
  status TEXT NOT NULL,
  error_message TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS customer_notification_deliveries_notification_idx
  ON customer_notification_deliveries(notification_id, channel);

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
  auto_enable_new_models BOOLEAN NOT NULL DEFAULT false,
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
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS auto_enable_new_models BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS provider_account_models (
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
  model_id TEXT NOT NULL,
  source TEXT NOT NULL CHECK (source IN ('discovered', 'manual')),
  enabled BOOLEAN NOT NULL DEFAULT false,
  availability TEXT NOT NULL CHECK (availability IN ('available', 'missing', 'unverified')),
  first_seen_at TIMESTAMPTZ NOT NULL,
  last_seen_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (provider_account_id, model_id)
);

CREATE INDEX IF NOT EXISTS provider_account_models_account_enabled_idx
  ON provider_account_models(provider_account_id, enabled, model_id);

CREATE INDEX IF NOT EXISTS provider_account_models_availability_idx
  ON provider_account_models(availability, last_seen_at DESC);

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

CREATE TABLE IF NOT EXISTS platform_tenants (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  entitlement_reference TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS gateway_principals (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES platform_tenants(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  principal_type TEXT NOT NULL DEFAULT 'service',
  external_subject_reference TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(tenant_id, name)
);

CREATE INDEX IF NOT EXISTS gateway_principals_tenant_status_idx
  ON gateway_principals(tenant_id, status);

CREATE TABLE IF NOT EXISTS external_auth_integrations (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES platform_tenants(id) ON DELETE RESTRICT,
  gateway_principal_id TEXT NOT NULL REFERENCES gateway_principals(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  protocol TEXT NOT NULL DEFAULT 'hmac_signed_context',
  key_id TEXT NOT NULL,
  secret_configured BOOLEAN NOT NULL DEFAULT false,
  secret_hint TEXT NOT NULL DEFAULT '',
  secret_ciphertext TEXT NOT NULL DEFAULT '',
  issuer TEXT NOT NULL DEFAULT '',
  jwks_url TEXT NOT NULL DEFAULT '',
  subject_claim TEXT NOT NULL DEFAULT '',
  models_claim TEXT NOT NULL DEFAULT '',
  qps_limit_claim TEXT NOT NULL DEFAULT '',
  monthly_token_limit_claim TEXT NOT NULL DEFAULT '',
  audience TEXT NOT NULL DEFAULT '',
  policy_id TEXT NOT NULL DEFAULT '',
  model_allowlist TEXT NOT NULL DEFAULT '[]',
  qps_limit INTEGER NOT NULL DEFAULT 0,
  monthly_token_limit INTEGER NOT NULL DEFAULT 0,
  max_ttl_seconds INTEGER NOT NULL DEFAULT 300,
  status TEXT NOT NULL DEFAULT 'disabled',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(tenant_id, name)
);

ALTER TABLE external_auth_integrations ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE external_auth_integrations ADD COLUMN IF NOT EXISTS issuer TEXT NOT NULL DEFAULT '';
ALTER TABLE external_auth_integrations ADD COLUMN IF NOT EXISTS jwks_url TEXT NOT NULL DEFAULT '';
ALTER TABLE external_auth_integrations ADD COLUMN IF NOT EXISTS subject_claim TEXT NOT NULL DEFAULT '';
ALTER TABLE external_auth_integrations ALTER COLUMN subject_claim SET DEFAULT '';
ALTER TABLE external_auth_integrations ADD COLUMN IF NOT EXISTS models_claim TEXT NOT NULL DEFAULT '';
ALTER TABLE external_auth_integrations ADD COLUMN IF NOT EXISTS qps_limit_claim TEXT NOT NULL DEFAULT '';
ALTER TABLE external_auth_integrations ADD COLUMN IF NOT EXISTS monthly_token_limit_claim TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS external_auth_integrations_tenant_status_idx
  ON external_auth_integrations(tenant_id, status);

CREATE UNIQUE INDEX IF NOT EXISTS external_auth_integrations_key_id_idx
  ON external_auth_integrations(key_id);

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
  profile_scope TEXT NOT NULL DEFAULT '',
  platform_tenant_id TEXT NOT NULL DEFAULT '',
  gateway_principal_id TEXT NOT NULL DEFAULT '',
  tenant_id TEXT NOT NULL DEFAULT '',
  principal_type TEXT NOT NULL DEFAULT '',
  principal_reference TEXT NOT NULL DEFAULT '',
  policy_id TEXT NOT NULL DEFAULT '',
  scopes TEXT NOT NULL DEFAULT '["gateway:invoke","models:read"]',
  model_allowlist TEXT NOT NULL DEFAULT '[]',
  allowed_modalities TEXT NOT NULL DEFAULT '["metadata","text"]',
  allowed_operations TEXT NOT NULL DEFAULT '["list_models","chat_completion"]',
  qps_limit INTEGER NOT NULL DEFAULT 0,
  rpm_limit INTEGER NOT NULL DEFAULT 0,
  tpm_limit INTEGER NOT NULL DEFAULT 0,
  concurrency_limit INTEGER NOT NULL DEFAULT 0,
  monthly_token_limit INTEGER NOT NULL DEFAULT 0,
  monthly_budget_cents INTEGER NOT NULL DEFAULT 0,
  monthly_image_limit INTEGER NOT NULL DEFAULT 0,
  monthly_video_seconds_limit INTEGER NOT NULL DEFAULT 0,
  monthly_audio_seconds_limit INTEGER NOT NULL DEFAULT 0,
  allowed_cidrs TEXT NOT NULL DEFAULT '[]',
	  lane_policy TEXT NOT NULL DEFAULT 'direct_only',
	  artifact_policy TEXT NOT NULL DEFAULT 'proxy_only',
	  artifact_sink_id TEXT NOT NULL DEFAULT '',
	  rotation_family_id TEXT NOT NULL DEFAULT '',
  replaces_key_id TEXT NOT NULL DEFAULT '',
  replaced_by_key_id TEXT NOT NULL DEFAULT '',
  rotation_grace_expires_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  last_used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS policy_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS key_type TEXT NOT NULL DEFAULT 'workspace';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS customer_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS owner_user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS profile_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS platform_tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS principal_type TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS principal_reference TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS scopes TEXT NOT NULL DEFAULT '["gateway:invoke","models:read"]';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS allowed_modalities TEXT NOT NULL DEFAULT '["metadata","text"]';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS allowed_operations TEXT NOT NULL DEFAULT '["list_models","chat_completion"]';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS rpm_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS tpm_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS concurrency_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS monthly_budget_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS monthly_image_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS monthly_video_seconds_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS monthly_audio_seconds_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS allowed_cidrs TEXT NOT NULL DEFAULT '[]';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS lane_policy TEXT NOT NULL DEFAULT 'direct_only';
	ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS artifact_policy TEXT NOT NULL DEFAULT 'proxy_only';
	ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS artifact_sink_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS rotation_family_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS replaces_key_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS replaced_by_key_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS rotation_grace_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS api_keys_owner_user_idx
  ON api_keys(owner_user_id, status)
  WHERE owner_user_id <> '';

CREATE INDEX IF NOT EXISTS api_keys_customer_idx
  ON api_keys(customer_id, status)
  WHERE customer_id <> '';

CREATE INDEX IF NOT EXISTS api_keys_platform_scope_idx
  ON api_keys(profile_scope, platform_tenant_id, gateway_principal_id, status)
  WHERE profile_scope = 'platform';

CREATE INDEX IF NOT EXISTS api_keys_tenant_principal_idx
  ON api_keys(profile_scope, tenant_id, principal_reference, status);

CREATE INDEX IF NOT EXISTS api_keys_rotation_family_idx
  ON api_keys(rotation_family_id)
  WHERE rotation_family_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS api_keys_replaces_key_idx
  ON api_keys(replaces_key_id)
  WHERE replaces_key_id <> '';

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
  profile_scope TEXT NOT NULL DEFAULT '',
  platform_tenant_id TEXT NOT NULL DEFAULT '',
  platform_tenant_name TEXT NOT NULL DEFAULT '',
  gateway_principal_id TEXT NOT NULL DEFAULT '',
  gateway_principal_name TEXT NOT NULL DEFAULT '',
	 external_auth_integration_id TEXT NOT NULL DEFAULT '',
	 external_subject_reference TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS profile_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS platform_tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS platform_tenant_name TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS gateway_principal_name TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS external_auth_integration_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS external_subject_reference TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS audit_logs_platform_scope_created_idx
  ON audit_logs(profile_scope, platform_tenant_id, gateway_principal_id, created_at DESC)
  WHERE profile_scope = 'platform';

CREATE INDEX IF NOT EXISTS audit_logs_external_auth_created_idx
  ON audit_logs(profile_scope, external_auth_integration_id, external_subject_reference, created_at DESC)
  WHERE external_auth_integration_id <> '';

CREATE TABLE IF NOT EXISTS ai_operations (
  id TEXT PRIMARY KEY,
  profile_scope TEXT NOT NULL DEFAULT '',
  tenant_id TEXT NOT NULL,
  credential_id TEXT NOT NULL,
  credential_source TEXT NOT NULL,
  integration_id TEXT NOT NULL DEFAULT '',
  principal_type TEXT NOT NULL DEFAULT '',
  principal_id TEXT NOT NULL DEFAULT '',
  external_subject_reference TEXT NOT NULL DEFAULT '',
  client_request_id TEXT NOT NULL DEFAULT '',
  request_fingerprint TEXT NOT NULL,
  idempotency_key TEXT NOT NULL DEFAULT '',
  protocol TEXT NOT NULL,
  operation TEXT NOT NULL,
  modality TEXT NOT NULL,
  lane TEXT NOT NULL,
	  model TEXT NOT NULL DEFAULT '',
	  artifact_policy TEXT NOT NULL DEFAULT 'proxy_only',
	  artifact_sink_id TEXT NOT NULL DEFAULT '',
	  status TEXT NOT NULL,
  error_type TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ
);

	ALTER TABLE ai_operations ADD COLUMN IF NOT EXISTS artifact_policy TEXT NOT NULL DEFAULT 'proxy_only';
	ALTER TABLE ai_operations ADD COLUMN IF NOT EXISTS artifact_sink_id TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS ai_operations_idempotency_scope_idx
  ON ai_operations(profile_scope, tenant_id, credential_source, credential_id, integration_id, principal_type, principal_id, external_subject_reference, operation, idempotency_key)
  WHERE idempotency_key <> '';

DROP INDEX IF EXISTS ai_operations_idempotency_idx;

CREATE INDEX IF NOT EXISTS ai_operations_tenant_created_idx
  ON ai_operations(profile_scope, tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS billing_holds (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL UNIQUE REFERENCES ai_operations(id) ON DELETE RESTRICT,
  profile_scope TEXT NOT NULL DEFAULT '',
  tenant_id TEXT NOT NULL DEFAULT '',
  credential_id TEXT NOT NULL,
  credential_source TEXT NOT NULL,
  integration_id TEXT NOT NULL DEFAULT '',
  principal_type TEXT NOT NULL DEFAULT '',
  principal_id TEXT NOT NULL DEFAULT '',
  external_subject_reference TEXT NOT NULL DEFAULT '',
  request_fingerprint TEXT NOT NULL,
  status TEXT NOT NULL,
  version INTEGER NOT NULL,
  reserved_amount_cents INTEGER NOT NULL DEFAULT 0,
  reserved_usage_dimensions JSONB NOT NULL DEFAULT '{}'::jsonb,
  settled_amount_cents INTEGER NOT NULL DEFAULT 0,
  currency TEXT NOT NULL DEFAULT 'USD',
  price_snapshot_id TEXT NOT NULL DEFAULT '',
  estimate_source TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  budget_period_start TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  settled_at TIMESTAMPTZ,
  released_at TIMESTAMPTZ,
  CHECK (status IN ('reserved', 'committed', 'settled', 'released', 'disputed')),
  CHECK (version > 0),
  CHECK (reserved_amount_cents >= 0),
  CHECK (settled_amount_cents >= 0),
  CHECK (char_length(currency) = 3)
);

CREATE INDEX IF NOT EXISTS billing_holds_budget_idx
  ON billing_holds(profile_scope, tenant_id, credential_id, budget_period_start, status);

CREATE INDEX IF NOT EXISTS billing_holds_expiry_idx
  ON billing_holds(status, expires_at);

ALTER TABLE billing_holds ADD COLUMN IF NOT EXISTS reserved_usage_dimensions JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE IF NOT EXISTS ai_jobs (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL UNIQUE REFERENCES ai_operations(id) ON DELETE RESTRICT,
  profile_scope TEXT NOT NULL DEFAULT '',
  tenant_id TEXT NOT NULL,
  credential_id TEXT NOT NULL,
  credential_source TEXT NOT NULL,
  integration_id TEXT NOT NULL DEFAULT '',
  principal_type TEXT NOT NULL DEFAULT '',
  principal_id TEXT NOT NULL DEFAULT '',
  external_subject_reference TEXT NOT NULL DEFAULT '',
  request_fingerprint TEXT NOT NULL,
  idempotency_key TEXT NOT NULL DEFAULT '',
  protocol TEXT NOT NULL,
  operation TEXT NOT NULL,
  modality TEXT NOT NULL,
	  model TEXT NOT NULL,
	  artifact_policy TEXT NOT NULL,
	  artifact_sink_id TEXT NOT NULL DEFAULT '',
	  request_payload_ciphertext TEXT NOT NULL,
  status TEXT NOT NULL,
  status_version INTEGER NOT NULL,
  priority INTEGER NOT NULL DEFAULT 0,
  next_eligible_at TIMESTAMPTZ NOT NULL,
  queue_lease_until TIMESTAMPTZ,
  queue_lease_token TEXT NOT NULL DEFAULT '',
  queue_worker_id TEXT NOT NULL DEFAULT '',
  fence_token BIGINT NOT NULL DEFAULT 0,
  error_type TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS ai_jobs_idempotency_idx
  ON ai_jobs(profile_scope, tenant_id, credential_source, credential_id, integration_id, principal_type, principal_id, external_subject_reference, operation, idempotency_key)
  WHERE idempotency_key <> '';

CREATE INDEX IF NOT EXISTS ai_jobs_owner_created_idx
  ON ai_jobs(profile_scope, tenant_id, integration_id, principal_type, principal_id, external_subject_reference, created_at DESC);

CREATE INDEX IF NOT EXISTS ai_jobs_ready_idx
  ON ai_jobs(status, next_eligible_at, priority DESC, created_at);

CREATE TABLE IF NOT EXISTS ai_job_events (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES ai_jobs(id) ON DELETE RESTRICT,
  version INTEGER NOT NULL,
  event_type TEXT NOT NULL,
  from_status TEXT NOT NULL DEFAULT '',
  to_status TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE(job_id, version)
);

CREATE INDEX IF NOT EXISTS ai_job_events_job_version_idx
  ON ai_job_events(job_id, version);

CREATE TABLE IF NOT EXISTS ai_attempts (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL REFERENCES ai_operations(id) ON DELETE RESTRICT,
  attempt_number INTEGER NOT NULL,
  provider_id TEXT NOT NULL DEFAULT '',
  provider_account_id TEXT NOT NULL DEFAULT '',
  provider_adapter_id TEXT NOT NULL DEFAULT '',
  route_id TEXT NOT NULL DEFAULT '',
  upstream_model TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error_type TEXT NOT NULL DEFAULT '',
  dispatch_state TEXT NOT NULL DEFAULT 'pending',
  dispatch_version INTEGER NOT NULL DEFAULT 0,
  dispatch_key TEXT NOT NULL DEFAULT '',
  dispatch_intent_json TEXT NOT NULL DEFAULT '',
  dispatch_submitted_at TIMESTAMPTZ,
  provider_task_id TEXT NOT NULL DEFAULT '',
  provider_request_id TEXT NOT NULL DEFAULT '',
  provider_task_status TEXT NOT NULL DEFAULT '',
  provider_accepted_at TIMESTAMPTZ,
  last_reconciled_at TIMESTAMPTZ,
  reconcile_after TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ,
  UNIQUE(operation_id, attempt_number)
);

ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS dispatch_state TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS provider_adapter_id TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS dispatch_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS dispatch_key TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS dispatch_intent_json TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS dispatch_submitted_at TIMESTAMPTZ;
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS provider_task_id TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS provider_request_id TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS provider_task_status TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS provider_accepted_at TIMESTAMPTZ;
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS last_reconciled_at TIMESTAMPTZ;
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS reconcile_after TIMESTAMPTZ;

UPDATE ai_attempts SET dispatch_key = id WHERE dispatch_key = '';

CREATE INDEX IF NOT EXISTS ai_attempts_operation_created_idx
  ON ai_attempts(operation_id, created_at);

CREATE INDEX IF NOT EXISTS ai_attempts_reconciliation_idx
  ON ai_attempts(dispatch_state, reconcile_after, updated_at)
  WHERE status = 'running' AND dispatch_state IN ('submitted', 'accepted', 'unknown');

CREATE UNIQUE INDEX IF NOT EXISTS ai_attempts_provider_task_idx
  ON ai_attempts(provider_account_id, provider_task_id)
  WHERE provider_task_id <> '';

CREATE TABLE IF NOT EXISTS realtime_sessions (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL UNIQUE REFERENCES ai_operations(id) ON DELETE RESTRICT,
  attempt_id TEXT NOT NULL UNIQUE REFERENCES ai_attempts(id) ON DELETE RESTRICT,
  profile_scope TEXT NOT NULL DEFAULT '',
  tenant_id TEXT NOT NULL DEFAULT '',
  credential_id TEXT NOT NULL,
  principal_type TEXT NOT NULL DEFAULT '',
  principal_id TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL,
  provider_id TEXT NOT NULL DEFAULT '',
  provider_account_id TEXT NOT NULL DEFAULT '',
  upstream_model TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  version INTEGER NOT NULL,
  input_audio_bytes BIGINT NOT NULL DEFAULT 0,
  output_audio_bytes BIGINT NOT NULL DEFAULT 0,
  client_message_count BIGINT NOT NULL DEFAULT 0,
  provider_message_count BIGINT NOT NULL DEFAULT 0,
  transfer_bytes BIGINT NOT NULL DEFAULT 0,
  usage_version INTEGER NOT NULL DEFAULT 0,
  session_duration_ms BIGINT NOT NULL DEFAULT 0,
  error_type TEXT NOT NULL DEFAULT '',
  connected_at TIMESTAMPTZ,
  closed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  CHECK (status IN ('connecting', 'connected', 'completed', 'failed', 'canceled')),
  CHECK (version > 0),
  CHECK (input_audio_bytes >= 0 AND output_audio_bytes >= 0),
  CHECK (client_message_count >= 0 AND provider_message_count >= 0),
  CHECK (transfer_bytes >= 0 AND usage_version >= 0 AND session_duration_ms >= 0)
);

CREATE INDEX IF NOT EXISTS realtime_sessions_tenant_created_idx
  ON realtime_sessions(profile_scope, tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS realtime_sessions_status_updated_idx
  ON realtime_sessions(status, updated_at);

CREATE TABLE IF NOT EXISTS ai_job_progress_events (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES ai_jobs(id) ON DELETE RESTRICT,
  attempt_id TEXT NOT NULL REFERENCES ai_attempts(id) ON DELETE RESTRICT,
  provider_task_id TEXT NOT NULL,
  provider_sequence BIGINT NOT NULL,
  percent INTEGER,
  stage TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE(attempt_id, provider_sequence),
  CHECK (provider_sequence > 0),
  CHECK (percent IS NULL OR (percent >= 0 AND percent <= 100)),
  CHECK (percent IS NOT NULL OR stage <> ''),
  CHECK (char_length(stage) <= 64),
  CHECK (stage = lower(stage)),
  CHECK (stage = '' OR stage ~ '^[a-z0-9][a-z0-9._-]*$')
);

CREATE INDEX IF NOT EXISTS ai_job_progress_events_job_created_idx
  ON ai_job_progress_events(job_id, created_at, id);

CREATE TABLE IF NOT EXISTS provider_callback_receipts (
  event_id TEXT PRIMARY KEY,
  adapter_id TEXT NOT NULL,
  attempt_id TEXT NOT NULL REFERENCES ai_attempts(id) ON DELETE CASCADE,
  provider_id TEXT NOT NULL,
  provider_account_id TEXT NOT NULL,
  provider_task_id TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  status TEXT NOT NULL,
  error_type TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  processed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS provider_callback_receipts_attempt_idx
  ON provider_callback_receipts(attempt_id, created_at DESC);

CREATE TABLE IF NOT EXISTS artifacts (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL REFERENCES ai_operations(id) ON DELETE RESTRICT,
  job_id TEXT REFERENCES ai_jobs(id) ON DELETE RESTRICT,
  attempt_id TEXT REFERENCES ai_attempts(id) ON DELETE RESTRICT,
  source_artifact_id TEXT REFERENCES artifacts(id) ON DELETE RESTRICT,
  profile_scope TEXT NOT NULL DEFAULT '',
  tenant_id TEXT NOT NULL DEFAULT '',
  integration_id TEXT NOT NULL DEFAULT '',
  principal_type TEXT NOT NULL DEFAULT '',
  principal_id TEXT NOT NULL DEFAULT '',
  external_subject_reference TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL,
  policy TEXT NOT NULL,
  status TEXT NOT NULL,
  status_version INTEGER NOT NULL,
  media_type TEXT NOT NULL DEFAULT '',
  size_bytes BIGINT NOT NULL DEFAULT 0,
  sha256 TEXT NOT NULL DEFAULT '',
  store_driver TEXT NOT NULL DEFAULT 'none',
  store_key TEXT NOT NULL DEFAULT '',
  external_reference TEXT NOT NULL DEFAULT '',
  error_type TEXT NOT NULL DEFAULT '',
  retain_until TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  ready_at TIMESTAMPTZ,
  delivered_at TIMESTAMPTZ,
  deleted_at TIMESTAMPTZ,
  CHECK (status_version > 0),
  CHECK (size_bytes >= 0)
);

CREATE INDEX IF NOT EXISTS artifacts_owner_created_idx
  ON artifacts(profile_scope, tenant_id, integration_id, principal_type, principal_id, external_subject_reference, created_at DESC);

CREATE INDEX IF NOT EXISTS artifacts_job_created_idx
  ON artifacts(job_id, created_at);

CREATE INDEX IF NOT EXISTS artifacts_retention_idx
  ON artifacts(status, retain_until);

CREATE INDEX IF NOT EXISTS artifacts_deletion_idx
  ON artifacts(status, updated_at)
  WHERE status IN ('delete_requested', 'delete_failed');

CREATE TABLE IF NOT EXISTS artifact_events (
  id TEXT PRIMARY KEY,
  artifact_id TEXT NOT NULL REFERENCES artifacts(id) ON DELETE RESTRICT,
  version INTEGER NOT NULL,
  event_type TEXT NOT NULL,
  from_status TEXT NOT NULL DEFAULT '',
  to_status TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE(artifact_id, version)
);

CREATE INDEX IF NOT EXISTS artifact_events_artifact_version_idx
  ON artifact_events(artifact_id, version);

CREATE TABLE IF NOT EXISTS gateway_credential_rate_samples (
  id TEXT PRIMARY KEY,
  profile_scope TEXT NOT NULL DEFAULT '',
  tenant_id TEXT NOT NULL DEFAULT '',
  credential_id TEXT NOT NULL,
  estimated_tokens INTEGER NOT NULL DEFAULT 0,
  occurred_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS gateway_credential_rate_samples_window_idx
  ON gateway_credential_rate_samples(profile_scope, tenant_id, credential_id, occurred_at);

CREATE TABLE IF NOT EXISTS gateway_credential_capacity_leases (
  id TEXT PRIMARY KEY,
  profile_scope TEXT NOT NULL DEFAULT '',
  tenant_id TEXT NOT NULL DEFAULT '',
  credential_id TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS gateway_credential_capacity_leases_expiry_idx
  ON gateway_credential_capacity_leases(profile_scope, tenant_id, credential_id, expires_at);

CREATE TABLE IF NOT EXISTS billing_ledger_entries (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL REFERENCES ai_operations(id) ON DELETE RESTRICT,
  attempt_id TEXT NOT NULL DEFAULT '',
  usage_version INTEGER NOT NULL,
  usage_record_id TEXT NOT NULL,
  request_fingerprint TEXT NOT NULL,
  entry_type TEXT NOT NULL,
  amount_cents INTEGER NOT NULL DEFAULT 0,
  currency TEXT NOT NULL DEFAULT 'USD',
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE(operation_id, attempt_id, usage_version)
);

CREATE INDEX IF NOT EXISTS billing_ledger_operation_created_idx
  ON billing_ledger_entries(operation_id, created_at);

CREATE TABLE IF NOT EXISTS transactional_outbox (
  id TEXT PRIMARY KEY,
  aggregate_type TEXT NOT NULL,
  aggregate_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  event_version INTEGER NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'pending',
  available_at TIMESTAMPTZ NOT NULL,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 20,
  lease_until TIMESTAMPTZ,
  lease_token TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  published_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(aggregate_type, aggregate_id, event_type, event_version)
);

ALTER TABLE transactional_outbox ADD COLUMN IF NOT EXISTS max_attempts INTEGER NOT NULL DEFAULT 20;
ALTER TABLE transactional_outbox ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ;
ALTER TABLE transactional_outbox ADD COLUMN IF NOT EXISTS lease_token TEXT NOT NULL DEFAULT '';
ALTER TABLE transactional_outbox ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS transactional_outbox_due_idx
  ON transactional_outbox(status, available_at, created_at);

CREATE TABLE IF NOT EXISTS usage_records (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL DEFAULT '',
  attempt_id TEXT NOT NULL DEFAULT '',
  usage_version INTEGER NOT NULL DEFAULT 0,
  usage_source TEXT NOT NULL DEFAULT '',
  request_fingerprint TEXT NOT NULL DEFAULT '',
  api_key_id TEXT NOT NULL,
  customer_id TEXT NOT NULL DEFAULT '',
  profile_scope TEXT NOT NULL DEFAULT '',
  platform_tenant_id TEXT NOT NULL DEFAULT '',
  platform_tenant_name TEXT NOT NULL DEFAULT '',
  gateway_principal_id TEXT NOT NULL DEFAULT '',
  gateway_principal_name TEXT NOT NULL DEFAULT '',
	 external_auth_integration_id TEXT NOT NULL DEFAULT '',
	 external_subject_reference TEXT NOT NULL DEFAULT '',
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
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS profile_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS platform_tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS platform_tenant_name TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS gateway_principal_name TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS external_auth_integration_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS external_subject_reference TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS operation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS attempt_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS usage_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS usage_source TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS request_fingerprint TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS usage_records_customer_created_idx
  ON usage_records(customer_id, created_at DESC)
  WHERE customer_id <> '';

CREATE INDEX IF NOT EXISTS usage_records_platform_scope_created_idx
  ON usage_records(profile_scope, platform_tenant_id, gateway_principal_id, created_at DESC)
  WHERE profile_scope = 'platform';

CREATE INDEX IF NOT EXISTS usage_records_external_auth_created_idx
  ON usage_records(profile_scope, external_auth_integration_id, external_subject_reference, created_at DESC)
  WHERE external_auth_integration_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS usage_records_ledger_identity_idx
  ON usage_records(operation_id, attempt_id, usage_version)
  WHERE operation_id <> '';

CREATE TABLE IF NOT EXISTS platform_usage_sinks (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES platform_tenants(id) ON DELETE RESTRICT,
  external_auth_integration_id TEXT NOT NULL REFERENCES external_auth_integrations(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  endpoint_url_ciphertext TEXT NOT NULL DEFAULT '',
  endpoint_url_hint TEXT NOT NULL DEFAULT '',
  signing_secret_ciphertext TEXT NOT NULL DEFAULT '',
  signing_secret_hint TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'disabled',
  max_attempts INTEGER NOT NULL DEFAULT 10,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(external_auth_integration_id, name)
);

CREATE INDEX IF NOT EXISTS platform_usage_sinks_integration_status_idx
  ON platform_usage_sinks(external_auth_integration_id, status);

CREATE TABLE IF NOT EXISTS platform_usage_delivery_events (
  id TEXT PRIMARY KEY,
  sink_id TEXT NOT NULL REFERENCES platform_usage_sinks(id) ON DELETE RESTRICT,
  usage_record_id TEXT NOT NULL REFERENCES usage_records(id) ON DELETE RESTRICT,
  event_id TEXT NOT NULL UNIQUE,
  payload_json TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempt_count INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 10,
  next_attempt_at TIMESTAMPTZ NOT NULL,
  lease_until TIMESTAMPTZ NULL,
  lease_token TEXT NOT NULL DEFAULT '',
  delivered_at TIMESTAMPTZ NULL,
  last_http_status INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  target_hint TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(sink_id, usage_record_id)
);

CREATE INDEX IF NOT EXISTS platform_usage_delivery_due_idx
  ON platform_usage_delivery_events(status, next_attempt_at, lease_until);

CREATE TABLE IF NOT EXISTS gateway_traces (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL DEFAULT '',
  attempt_id TEXT NOT NULL DEFAULT '',
  request_fingerprint TEXT NOT NULL DEFAULT '',
  api_key_id TEXT NOT NULL,
  api_fingerprint TEXT NOT NULL,
  profile_scope TEXT NOT NULL DEFAULT '',
  platform_tenant_id TEXT NOT NULL DEFAULT '',
  platform_tenant_name TEXT NOT NULL DEFAULT '',
  gateway_principal_id TEXT NOT NULL DEFAULT '',
  gateway_principal_name TEXT NOT NULL DEFAULT '',
	 external_auth_integration_id TEXT NOT NULL DEFAULT '',
	 external_subject_reference TEXT NOT NULL DEFAULT '',
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
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS profile_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS platform_tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS platform_tenant_name TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS gateway_principal_name TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS external_auth_integration_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS external_subject_reference TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS operation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS attempt_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS request_fingerprint TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS gateway_traces_created_idx
  ON gateway_traces(created_at DESC);

CREATE INDEX IF NOT EXISTS gateway_traces_route_idx
  ON gateway_traces(provider_id, provider_account_id, created_at DESC);

CREATE INDEX IF NOT EXISTS gateway_traces_policy_idx
  ON gateway_traces(policy_id, created_at DESC);

CREATE INDEX IF NOT EXISTS gateway_traces_platform_scope_created_idx
  ON gateway_traces(profile_scope, platform_tenant_id, gateway_principal_id, created_at DESC)
  WHERE profile_scope = 'platform';

CREATE INDEX IF NOT EXISTS gateway_traces_external_auth_created_idx
  ON gateway_traces(profile_scope, external_auth_integration_id, external_subject_reference, created_at DESC)
  WHERE external_auth_integration_id <> '';

CREATE TABLE IF NOT EXISTS alert_events (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  severity TEXT NOT NULL,
  status TEXT NOT NULL,
  title TEXT NOT NULL,
  summary TEXT NOT NULL,
  resource_type TEXT NOT NULL DEFAULT '',
  resource_id TEXT NOT NULL DEFAULT '',
  profile_scope TEXT NOT NULL DEFAULT '',
  platform_tenant_id TEXT NOT NULL DEFAULT '',
  platform_tenant_name TEXT NOT NULL DEFAULT '',
  gateway_principal_id TEXT NOT NULL DEFAULT '',
  gateway_principal_name TEXT NOT NULL DEFAULT '',
	 external_auth_integration_id TEXT NOT NULL DEFAULT '',
	 external_subject_reference TEXT NOT NULL DEFAULT '',
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

ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS profile_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS platform_tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS platform_tenant_name TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS gateway_principal_name TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS external_auth_integration_id TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS external_subject_reference TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS alert_events_platform_scope_last_seen_idx
  ON alert_events(profile_scope, platform_tenant_id, gateway_principal_id, last_seen_at DESC)
  WHERE profile_scope = 'platform';

CREATE INDEX IF NOT EXISTS alert_events_external_auth_last_seen_idx
  ON alert_events(profile_scope, external_auth_integration_id, external_subject_reference, last_seen_at DESC)
  WHERE external_auth_integration_id <> '';

ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS protocol TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS ttft_ms BIGINT;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS total_input_tokens INTEGER;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS uncached_input_tokens INTEGER;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS cache_read_tokens INTEGER;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS cache_write_5m_tokens INTEGER;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS cache_write_1h_tokens INTEGER;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS cache_fields_present BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS usage_dimensions JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS usage_normalization_status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS upstream_request_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS procurement_cost_micros BIGINT;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS procurement_cost_currency TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS procurement_cost_source TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS procurement_cost_confidence TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS procurement_price_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS provider_billing_line_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS usage_records_effective_pricing_idx
  ON usage_records(provider_account_id, upstream_model, protocol, created_at DESC)
  WHERE provider_account_id <> '';

CREATE INDEX IF NOT EXISTS usage_records_upstream_request_idx
  ON usage_records(provider_account_id, upstream_request_id)
  WHERE upstream_request_id <> '';

CREATE TABLE IF NOT EXISTS procurement_prices (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE RESTRICT,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  upstream_model TEXT NOT NULL,
  protocol TEXT NOT NULL,
  currency TEXT NOT NULL DEFAULT 'USD',
  uncached_input_micros_per_1m_tokens BIGINT NOT NULL DEFAULT 0,
  cache_read_micros_per_1m_tokens BIGINT NOT NULL DEFAULT 0,
  cache_write_5m_micros_per_1m_tokens BIGINT NOT NULL DEFAULT 0,
  cache_write_1h_micros_per_1m_tokens BIGINT NOT NULL DEFAULT 0,
  output_micros_per_1m_tokens BIGINT NOT NULL DEFAULT 0,
  request_micros BIGINT NOT NULL DEFAULT 0,
  reference_input_micros_per_1m_tokens BIGINT NOT NULL DEFAULT 0,
  reference_output_micros_per_1m_tokens BIGINT NOT NULL DEFAULT 0,
  quoted_multiplier DOUBLE PRECISION NOT NULL DEFAULT 0,
  recharge_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1,
  source_kind TEXT NOT NULL DEFAULT 'manual',
  source_reference TEXT NOT NULL DEFAULT '',
  evidence_hash TEXT NOT NULL DEFAULT '',
  confidence TEXT NOT NULL DEFAULT 'estimated',
  status TEXT NOT NULL DEFAULT 'active',
  effective_from TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS procurement_prices_lookup_idx
  ON procurement_prices(provider_account_id, upstream_model, protocol, status, effective_from DESC);

CREATE TABLE IF NOT EXISTS provider_billing_lines (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE RESTRICT,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  external_line_id TEXT NOT NULL DEFAULT '',
  external_request_id TEXT NOT NULL DEFAULT '',
  usage_record_id TEXT NOT NULL DEFAULT '',
  upstream_model TEXT NOT NULL DEFAULT '',
  currency TEXT NOT NULL DEFAULT 'USD',
  amount_micros BIGINT NOT NULL DEFAULT 0,
  input_cost_micros BIGINT,
  output_cost_micros BIGINT,
  cache_read_cost_micros BIGINT,
  cache_write_cost_micros BIGINT,
  source_kind TEXT NOT NULL DEFAULT 'manual',
  confidence TEXT NOT NULL DEFAULT 'unknown',
  reconciliation_status TEXT NOT NULL DEFAULT 'pending',
  raw_payload_hash TEXT NOT NULL DEFAULT '',
  usage_started_at TIMESTAMPTZ,
  usage_ended_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS provider_billing_lines_external_unique_idx
  ON provider_billing_lines(provider_account_id, external_line_id)
  WHERE external_line_id <> '';

CREATE INDEX IF NOT EXISTS provider_billing_lines_request_idx
  ON provider_billing_lines(provider_account_id, external_request_id)
  WHERE external_request_id <> '';

CREATE TABLE IF NOT EXISTS provider_billing_sources (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE RESTRICT,
  provider_account_id TEXT NOT NULL UNIQUE REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  adapter_id TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'observe_only' CHECK (status IN ('observe_only','active','disabled')),
  automatic_sync_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  sync_interval_seconds INTEGER NOT NULL DEFAULT 3600 CHECK (sync_interval_seconds BETWEEN 60 AND 86400),
  cursor TEXT NOT NULL DEFAULT '',
  usage_cost_lines BOOLEAN NOT NULL DEFAULT FALSE,
  aggregate_usage BOOLEAN NOT NULL DEFAULT FALSE,
  balance_supported BOOLEAN NOT NULL DEFAULT FALSE,
  incremental_sync BOOLEAN NOT NULL DEFAULT FALSE,
  price_feed BOOLEAN NOT NULL DEFAULT FALSE,
  detection_status TEXT NOT NULL DEFAULT '',
  contract_version TEXT NOT NULL DEFAULT '',
  evidence_hash TEXT NOT NULL DEFAULT '',
  warnings TEXT NOT NULL DEFAULT '[]',
  next_sync_at TIMESTAMPTZ,
  last_sync_started_at TIMESTAMPTZ,
  last_sync_completed_at TIMESTAMPTZ,
  last_success_at TIMESTAMPTZ,
  consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
  last_error_code TEXT NOT NULL DEFAULT '',
  lease_token TEXT NOT NULL DEFAULT '',
  lease_expires_at TIMESTAMPTZ,
  version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
  created_by TEXT NOT NULL DEFAULT '',
  updated_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS provider_billing_sources_due_idx
  ON provider_billing_sources(next_sync_at, id)
  WHERE automatic_sync_enabled = TRUE AND status <> 'disabled';

CREATE INDEX IF NOT EXISTS provider_billing_sources_lease_idx
  ON provider_billing_sources(lease_expires_at)
  WHERE lease_token <> '';

CREATE TABLE IF NOT EXISTS provider_billing_sync_runs (
  id TEXT PRIMARY KEY,
  source_id TEXT NOT NULL REFERENCES provider_billing_sources(id) ON DELETE CASCADE,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE RESTRICT,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  trigger TEXT NOT NULL CHECK (trigger IN ('manual','scheduled')),
  triggered_by TEXT NOT NULL DEFAULT '',
  adapter_id TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('running','succeeded','failed','lease_expired')),
  usage_cost_lines BOOLEAN NOT NULL DEFAULT FALSE,
  aggregate_usage BOOLEAN NOT NULL DEFAULT FALSE,
  balance_supported BOOLEAN NOT NULL DEFAULT FALSE,
  incremental_sync BOOLEAN NOT NULL DEFAULT FALSE,
  price_feed BOOLEAN NOT NULL DEFAULT FALSE,
  detection_status TEXT NOT NULL DEFAULT '',
  contract_version TEXT NOT NULL DEFAULT '',
  discovered_lines INTEGER NOT NULL DEFAULT 0 CHECK (discovered_lines >= 0),
  imported_lines INTEGER NOT NULL DEFAULT 0 CHECK (imported_lines >= 0),
  skipped_lines INTEGER NOT NULL DEFAULT 0 CHECK (skipped_lines >= 0),
  evidence_hash TEXT NOT NULL DEFAULT '',
  warnings TEXT NOT NULL DEFAULT '[]',
  error_code TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ NOT NULL,
  finished_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS provider_billing_sync_runs_source_idx
  ON provider_billing_sync_runs(source_id, started_at DESC);

CREATE INDEX IF NOT EXISTS provider_billing_sync_runs_status_idx
  ON provider_billing_sync_runs(status, started_at DESC);

CREATE TABLE IF NOT EXISTS provider_balance_snapshots (
  id TEXT PRIMARY KEY,
  source_id TEXT NOT NULL REFERENCES provider_billing_sources(id) ON DELETE CASCADE,
  sync_run_id TEXT NOT NULL REFERENCES provider_billing_sync_runs(id) ON DELETE CASCADE,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL CHECK (kind IN ('wallet_balance','api_key_quota_remaining','subscription_period_remaining')),
  amount_micros BIGINT NOT NULL,
  unlimited BOOLEAN NOT NULL DEFAULT FALSE,
  currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
  evidence_hash TEXT NOT NULL DEFAULT '',
  observed_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE(sync_run_id, kind)
);

CREATE INDEX IF NOT EXISTS provider_balance_snapshots_source_idx
  ON provider_balance_snapshots(source_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS provider_usage_aggregate_snapshots (
  id TEXT PRIMARY KEY,
  source_id TEXT NOT NULL REFERENCES provider_billing_sources(id) ON DELETE CASCADE,
  sync_run_id TEXT NOT NULL REFERENCES provider_billing_sync_runs(id) ON DELETE CASCADE,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  scope TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  request_count BIGINT NOT NULL DEFAULT 0 CHECK (request_count >= 0),
  input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
  output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
  cache_creation_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_creation_tokens >= 0),
  cache_read_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0),
  list_cost_micros BIGINT CHECK (list_cost_micros IS NULL OR list_cost_micros >= 0),
  actual_cost_micros BIGINT CHECK (actual_cost_micros IS NULL OR actual_cost_micros >= 0),
  currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
  evidence_hash TEXT NOT NULL DEFAULT '',
  observed_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE(sync_run_id, scope, model)
);

CREATE INDEX IF NOT EXISTS provider_usage_aggregate_snapshots_source_idx
  ON provider_usage_aggregate_snapshots(source_id, observed_at DESC);

CREATE INDEX IF NOT EXISTS provider_usage_aggregate_snapshots_model_idx
  ON provider_usage_aggregate_snapshots(provider_account_id, model, observed_at DESC)
  WHERE model <> '';

CREATE TABLE IF NOT EXISTS provider_cache_capabilities (
  id TEXT PRIMARY KEY,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
  upstream_model TEXT NOT NULL,
  protocol TEXT NOT NULL,
  support_status TEXT NOT NULL DEFAULT 'unknown',
  pool_affinity_grade TEXT NOT NULL DEFAULT 'unknown',
  affinity_transport TEXT NOT NULL DEFAULT 'none',
  affinity_field TEXT NOT NULL DEFAULT '',
  cache_control_mode TEXT NOT NULL DEFAULT 'passthrough_if_present',
  usage_schema TEXT NOT NULL DEFAULT 'auto',
  metrics_coverage DOUBLE PRECISION NOT NULL DEFAULT 0,
  eligible_request_hit_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  cache_token_hit_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  cache_write_read_ratio DOUBLE PRECISION NOT NULL DEFAULT 0,
  affinity_consistency_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  billing_consistency_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  production_sample_count BIGINT NOT NULL DEFAULT 0,
  probe_sample_count BIGINT NOT NULL DEFAULT 0,
  degraded_reason TEXT NOT NULL DEFAULT '',
  last_observed_at TIMESTAMPTZ,
  last_verified_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(provider_account_id, upstream_model, protocol)
);

CREATE TABLE IF NOT EXISTS provider_cache_probe_runs (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE RESTRICT,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  upstream_model TEXT NOT NULL,
  protocol TEXT NOT NULL,
  probe_series_id TEXT NOT NULL,
  session_hash TEXT NOT NULL,
  prefix_fingerprint TEXT NOT NULL,
  prefix_tokens BIGINT NOT NULL DEFAULT 0,
  warm_cache_read_tokens BIGINT NOT NULL DEFAULT 0,
  warm_cache_write_tokens BIGINT NOT NULL DEFAULT 0,
  warm_ttft_ms BIGINT NOT NULL DEFAULT 0,
  warm_upstream_request_id TEXT NOT NULL DEFAULT '',
  reuse_cache_read_tokens BIGINT NOT NULL DEFAULT 0,
  reuse_cache_write_tokens BIGINT NOT NULL DEFAULT 0,
  reuse_ttft_ms BIGINT NOT NULL DEFAULT 0,
  reuse_upstream_request_id TEXT NOT NULL DEFAULT '',
  control_cache_read_tokens BIGINT NOT NULL DEFAULT 0,
  control_cache_write_tokens BIGINT NOT NULL DEFAULT 0,
  control_ttft_ms BIGINT NOT NULL DEFAULT 0,
  control_upstream_request_id TEXT NOT NULL DEFAULT '',
  cache_fields_present BOOLEAN NOT NULL DEFAULT FALSE,
  estimated_cost_micros BIGINT NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  failure_reason TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ NOT NULL,
  finished_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE provider_cache_probe_runs ADD COLUMN IF NOT EXISTS warm_upstream_request_id TEXT NOT NULL DEFAULT '';
ALTER TABLE provider_cache_probe_runs ADD COLUMN IF NOT EXISTS reuse_upstream_request_id TEXT NOT NULL DEFAULT '';
ALTER TABLE provider_cache_probe_runs ADD COLUMN IF NOT EXISTS control_upstream_request_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS provider_cache_probe_runs_account_started_idx
  ON provider_cache_probe_runs(provider_account_id, upstream_model, protocol, started_at DESC);

CREATE TABLE IF NOT EXISTS effective_pricing_policies (
  id TEXT PRIMARY KEY,
  mode TEXT NOT NULL DEFAULT 'observe_only',
  window_hours INTEGER NOT NULL DEFAULT 24,
  min_sample_count BIGINT NOT NULL DEFAULT 200,
  min_metrics_coverage DOUBLE PRECISION NOT NULL DEFAULT 0.8,
  min_billing_consistency DOUBLE PRECISION NOT NULL DEFAULT 0.95,
  min_cost_improvement DOUBLE PRECISION NOT NULL DEFAULT 0.08,
  min_cache_hit_rate_improvement DOUBLE PRECISION NOT NULL DEFAULT 0.10,
  min_affinity_improvement DOUBLE PRECISION NOT NULL DEFAULT 0.10,
  max_cache_tiebreak_cost_regression DOUBLE PRECISION NOT NULL DEFAULT 0.02,
  max_error_rate_regression DOUBLE PRECISION NOT NULL DEFAULT 0.005,
  max_p95_latency_regression DOUBLE PRECISION NOT NULL DEFAULT 0.2,
  canary_percent INTEGER NOT NULL DEFAULT 5,
  supplier_affinity_ttl_seconds INTEGER NOT NULL DEFAULT 86400,
  account_affinity_ttl_seconds INTEGER NOT NULL DEFAULT 1800,
	automatic_actions_enabled BOOLEAN NOT NULL DEFAULT FALSE,
	evaluation_interval_minutes INTEGER NOT NULL DEFAULT 60,
	promotion_window_count INTEGER NOT NULL DEFAULT 3,
	degradation_window_count INTEGER NOT NULL DEFAULT 2,
  probe_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  probe_daily_token_budget BIGINT NOT NULL DEFAULT 100000,
  probe_daily_cost_budget_micros BIGINT NOT NULL DEFAULT 10000000,
  probe_cooldown_seconds INTEGER NOT NULL DEFAULT 1800,
  updated_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE effective_pricing_policies ADD COLUMN IF NOT EXISTS min_cache_hit_rate_improvement DOUBLE PRECISION NOT NULL DEFAULT 0.10;
ALTER TABLE effective_pricing_policies ADD COLUMN IF NOT EXISTS min_affinity_improvement DOUBLE PRECISION NOT NULL DEFAULT 0.10;
ALTER TABLE effective_pricing_policies ADD COLUMN IF NOT EXISTS max_cache_tiebreak_cost_regression DOUBLE PRECISION NOT NULL DEFAULT 0.02;
ALTER TABLE effective_pricing_policies ADD COLUMN IF NOT EXISTS automatic_actions_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE effective_pricing_policies ADD COLUMN IF NOT EXISTS evaluation_interval_minutes INTEGER NOT NULL DEFAULT 60;
ALTER TABLE effective_pricing_policies ADD COLUMN IF NOT EXISTS promotion_window_count INTEGER NOT NULL DEFAULT 3;
ALTER TABLE effective_pricing_policies ADD COLUMN IF NOT EXISTS degradation_window_count INTEGER NOT NULL DEFAULT 2;

CREATE TABLE IF NOT EXISTS effective_price_snapshots (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE RESTRICT,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  upstream_model TEXT NOT NULL,
  protocol TEXT NOT NULL,
  currency TEXT NOT NULL,
  effective_cost_micros_per_1m BIGINT NOT NULL DEFAULT 0,
  effective_multiplier DOUBLE PRECISION NOT NULL DEFAULT 0,
  quoted_multiplier DOUBLE PRECISION NOT NULL DEFAULT 0,
  cache_token_hit_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  metrics_coverage DOUBLE PRECISION NOT NULL DEFAULT 0,
  billing_consistency_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  request_count BIGINT NOT NULL DEFAULT 0,
  cost_confidence TEXT NOT NULL DEFAULT 'unknown',
  price_id TEXT NOT NULL DEFAULT '',
  window_start TIMESTAMPTZ NOT NULL,
  window_end TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS effective_price_snapshots_lookup_idx
  ON effective_price_snapshots(provider_account_id, upstream_model, protocol, created_at DESC);

CREATE TABLE IF NOT EXISTS effective_pricing_decisions (
  id TEXT PRIMARY KEY,
  model TEXT NOT NULL,
  protocol TEXT NOT NULL,
  current_provider_account_id TEXT NOT NULL DEFAULT '',
  candidate_provider_account_id TEXT NOT NULL DEFAULT '',
  current_snapshot_id TEXT NOT NULL DEFAULT '',
  candidate_snapshot_id TEXT NOT NULL DEFAULT '',
  current_cost_micros_per_1m BIGINT NOT NULL DEFAULT 0,
  candidate_cost_micros_per_1m BIGINT NOT NULL DEFAULT 0,
  cost_improvement DOUBLE PRECISION NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'hold',
  reason_codes TEXT NOT NULL DEFAULT '[]',
  canary_percent INTEGER NOT NULL DEFAULT 0,
  sample_count BIGINT NOT NULL DEFAULT 0,
  confidence TEXT NOT NULL DEFAULT 'unknown',
	healthy_window_count INTEGER NOT NULL DEFAULT 0,
	degraded_window_count INTEGER NOT NULL DEFAULT 0,
	last_evaluation_id TEXT NOT NULL DEFAULT '',
	last_evaluation_verdict TEXT NOT NULL DEFAULT '',
	last_evaluation_reason_codes TEXT NOT NULL DEFAULT '[]',
	last_evaluated_window_end TIMESTAMPTZ,
	monitoring_started_at TIMESTAMPTZ,
	last_healthy_at TIMESTAMPTZ,
	last_automatic_action TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS effective_pricing_decisions_model_idx
  ON effective_pricing_decisions(model, protocol, updated_at DESC);

ALTER TABLE effective_pricing_decisions ADD COLUMN IF NOT EXISTS healthy_window_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE effective_pricing_decisions ADD COLUMN IF NOT EXISTS degraded_window_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE effective_pricing_decisions ADD COLUMN IF NOT EXISTS last_evaluation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE effective_pricing_decisions ADD COLUMN IF NOT EXISTS last_evaluation_verdict TEXT NOT NULL DEFAULT '';
ALTER TABLE effective_pricing_decisions ADD COLUMN IF NOT EXISTS last_evaluation_reason_codes TEXT NOT NULL DEFAULT '[]';
ALTER TABLE effective_pricing_decisions ADD COLUMN IF NOT EXISTS last_evaluated_window_end TIMESTAMPTZ;
ALTER TABLE effective_pricing_decisions ADD COLUMN IF NOT EXISTS monitoring_started_at TIMESTAMPTZ;
ALTER TABLE effective_pricing_decisions ADD COLUMN IF NOT EXISTS last_healthy_at TIMESTAMPTZ;
ALTER TABLE effective_pricing_decisions ADD COLUMN IF NOT EXISTS last_automatic_action TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS effective_pricing_decision_evaluations (
	id TEXT PRIMARY KEY,
	decision_id TEXT NOT NULL REFERENCES effective_pricing_decisions(id) ON DELETE CASCADE,
	window_start TIMESTAMPTZ NOT NULL,
	window_end TIMESTAMPTZ NOT NULL,
	verdict TEXT NOT NULL,
	reason_codes TEXT NOT NULL DEFAULT '[]',
	current_snapshot_id TEXT NOT NULL DEFAULT '',
	candidate_snapshot_id TEXT NOT NULL DEFAULT '',
	current_request_count BIGINT NOT NULL DEFAULT 0,
	candidate_request_count BIGINT NOT NULL DEFAULT 0,
	current_cost_micros_per_1m BIGINT NOT NULL DEFAULT 0,
	candidate_cost_micros_per_1m BIGINT NOT NULL DEFAULT 0,
	cost_improvement DOUBLE PRECISION NOT NULL DEFAULT 0,
	current_cache_token_hit_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
	candidate_cache_token_hit_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
	current_cache_savings_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
	candidate_cache_savings_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
	current_affinity_consistency_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
	candidate_affinity_consistency_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
	current_error_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
	candidate_error_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
	current_p95_latency_ms BIGINT NOT NULL DEFAULT 0,
	candidate_p95_latency_ms BIGINT NOT NULL DEFAULT 0,
	current_metrics_coverage DOUBLE PRECISION NOT NULL DEFAULT 0,
	candidate_metrics_coverage DOUBLE PRECISION NOT NULL DEFAULT 0,
	current_billing_consistency_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
	candidate_billing_consistency_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
	automatic_action TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	UNIQUE(decision_id, window_end)
);

CREATE INDEX IF NOT EXISTS effective_pricing_decision_evaluations_lookup_idx
	ON effective_pricing_decision_evaluations(decision_id, window_end DESC);

CREATE TABLE IF NOT EXISTS routing_affinity_bindings (
  scope_key TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  provider_id TEXT NOT NULL DEFAULT '',
  provider_account_id TEXT NOT NULL DEFAULT '',
  route_id TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL,
  protocol TEXT NOT NULL,
  policy_version INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  last_reused_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS routing_affinity_bindings_expiry_idx
  ON routing_affinity_bindings(expires_at);

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
	out := make([]ProviderConnection, 0)
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
	out := make([]ProviderHealthCheck, 0)
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

	out := make([]RoutingGroup, 0)
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
	SELECT id, provider_id, name, platform, auth_type, status, schedulable, priority, weight, concurrency, rpm_limit, tpm_limit, load_factor, rate_multiplier, models, auto_enable_new_models, group_ids, secret_configured, secret_hint, secret_ciphertext, error_message, last_used_at, expires_at, cooldown_until, circuit_state, circuit_failure_threshold, circuit_open_seconds, consecutive_failures, circuit_opened_until, last_failure_at, temp_unschedulable_rules, temp_unschedulable_reason, created_at, updated_at
FROM provider_accounts
ORDER BY priority ASC, name ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ProviderAccount, 0)
	for rows.Next() {
		var account ProviderAccount
		var models, groupIDs, tempUnschedulableRules string
		var loadFactor sql.NullInt64
		var lastUsedAt, expiresAt, cooldownUntil, circuitOpenedUntil, lastFailureAt sql.NullTime
		if err := rows.Scan(&account.ID, &account.ProviderID, &account.Name, &account.Platform, &account.AuthType, &account.Status, &account.Schedulable, &account.Priority, &account.Weight, &account.Concurrency, &account.RPMLimit, &account.TPMLimit, &loadFactor, &account.RateMultiplier, &models, &account.AutoEnableNewModels, &groupIDs, &account.SecretConfigured, &account.SecretHint, &account.SecretCiphertext, &account.ErrorMessage, &lastUsedAt, &expiresAt, &cooldownUntil, &account.CircuitState, &account.CircuitFailureThreshold, &account.CircuitOpenSeconds, &account.ConsecutiveFailures, &circuitOpenedUntil, &lastFailureAt, &tempUnschedulableRules, &account.TempUnschedulableReason, &account.CreatedAt, &account.UpdatedAt); err != nil {
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
	return saveProviderAccount(ctx, r.db, account)
}

type providerAccountExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func saveProviderAccount(ctx context.Context, executor providerAccountExecutor, account ProviderAccount) error {
	models := marshalStringList(account.Models)
	groupIDs := marshalStringList(account.GroupIDs)
	tempUnschedulableRules := marshalTempUnschedulableRules(account.TempUnschedulableRules)
	var loadFactor sql.NullInt64
	if account.LoadFactor != nil {
		loadFactor = sql.NullInt64{Int64: int64(*account.LoadFactor), Valid: true}
	}
	_, err := executor.ExecContext(ctx, `
INSERT INTO provider_accounts(id, provider_id, name, platform, auth_type, status, schedulable, priority, weight, concurrency, rpm_limit, tpm_limit, load_factor, rate_multiplier, models, auto_enable_new_models, group_ids, secret_configured, secret_hint, secret_ciphertext, error_message, last_used_at, expires_at, cooldown_until, circuit_state, circuit_failure_threshold, circuit_open_seconds, consecutive_failures, circuit_opened_until, last_failure_at, temp_unschedulable_rules, temp_unschedulable_reason, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34)
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
  auto_enable_new_models = EXCLUDED.auto_enable_new_models,
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
`, account.ID, account.ProviderID, account.Name, account.Platform, account.AuthType, account.Status, account.Schedulable, account.Priority, account.Weight, account.Concurrency, account.RPMLimit, account.TPMLimit, loadFactor, account.RateMultiplier, models, account.AutoEnableNewModels, groupIDs, account.SecretConfigured, account.SecretHint, account.SecretCiphertext, account.ErrorMessage, account.LastUsedAt, account.ExpiresAt, account.CooldownUntil, account.CircuitState, account.CircuitFailureThreshold, account.CircuitOpenSeconds, account.ConsecutiveFailures, account.CircuitOpenedUntil, account.LastFailureAt, tempUnschedulableRules, account.TempUnschedulableReason, account.CreatedAt, account.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListProviderAccountModels(ctx context.Context, accountID string) ([]ProviderAccountModel, error) {
	rows, err := r.db.QueryContext(ctx, `
	SELECT provider_account_id, model_id, source, enabled, availability, first_seen_at, last_seen_at, updated_at
	FROM provider_account_models
	WHERE provider_account_id = $1
	ORDER BY model_id ASC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ProviderAccountModel, 0)
	for rows.Next() {
		var model ProviderAccountModel
		var lastSeenAt sql.NullTime
		if err := rows.Scan(&model.ProviderAccountID, &model.ModelID, &model.Source, &model.Enabled, &model.Availability, &model.FirstSeenAt, &lastSeenAt, &model.UpdatedAt); err != nil {
			return nil, err
		}
		if lastSeenAt.Valid {
			model.LastSeenAt = &lastSeenAt.Time
		}
		out = append(out, model)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveProviderAccountWithModels(ctx context.Context, account ProviderAccount, models []ProviderAccountModel) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := saveProviderAccount(ctx, tx, account); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM provider_account_models WHERE provider_account_id = $1`, account.ID); err != nil {
		return err
	}
	for _, model := range models {
		if _, err := tx.ExecContext(ctx, `
		INSERT INTO provider_account_models(provider_account_id, model_id, source, enabled, availability, first_seen_at, last_seen_at, updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)
		`, model.ProviderAccountID, model.ModelID, model.Source, model.Enabled, model.Availability, model.FirstSeenAt, model.LastSeenAt, model.UpdatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
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
	out := make([]ProviderAccountHealthCheck, 0)
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
	out := make([]ModelPricing, 0)
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

const apiKeySelectColumns = `id, name, key_hash, fingerprint, prefix, status, key_type, customer_id, owner_user_id,
profile_scope, platform_tenant_id, gateway_principal_id, tenant_id, principal_type, principal_reference,
policy_id, scopes, model_allowlist, allowed_modalities, allowed_operations,
qps_limit, rpm_limit, tpm_limit, concurrency_limit, monthly_token_limit, monthly_budget_cents,
monthly_image_limit, monthly_video_seconds_limit, monthly_audio_seconds_limit,
allowed_cidrs, lane_policy, artifact_policy, artifact_sink_id, rotation_family_id,
replaces_key_id, replaced_by_key_id, rotation_grace_expires_at,
expires_at, last_used_at, created_at, updated_at`

type apiKeyScanner interface {
	Scan(dest ...any) error
}

func scanAPIKey(scanner apiKeyScanner) (APIKeyRecord, error) {
	var key APIKeyRecord
	var scopes, allowlist, modalities, operations, allowedCIDRs string
	var rotationGraceExpiresAt, expiresAt, lastUsedAt sql.NullTime
	err := scanner.Scan(
		&key.ID, &key.Name, &key.KeyHash, &key.Fingerprint, &key.Prefix, &key.Status, &key.KeyType, &key.CustomerID, &key.OwnerUserID,
		&key.ProfileScope, &key.PlatformTenantID, &key.GatewayPrincipalID, &key.TenantID, &key.PrincipalType, &key.PrincipalReference,
		&key.PolicyID, &scopes, &allowlist, &modalities, &operations,
		&key.QPSLimit, &key.RPMLimit, &key.TPMLimit, &key.ConcurrencyLimit, &key.MonthlyTokenLimit, &key.MonthlyBudgetCents,
		&key.MonthlyImageLimit, &key.MonthlyVideoSecondsLimit, &key.MonthlyAudioSecondsLimit,
		&allowedCIDRs, &key.LanePolicy, &key.ArtifactPolicy, &key.ArtifactSinkID, &key.RotationFamilyID,
		&key.ReplacesKeyID, &key.ReplacedByKeyID, &rotationGraceExpiresAt,
		&expiresAt, &lastUsedAt, &key.CreatedAt, &key.UpdatedAt,
	)
	if err != nil {
		return APIKeyRecord{}, err
	}
	key.Scopes = parseStringList(scopes)
	key.ModelAllowlist = parseStringList(allowlist)
	key.AllowedModalities = parseStringList(modalities)
	key.AllowedOperations = parseStringList(operations)
	key.AllowedCIDRs = parseStringList(allowedCIDRs)
	if rotationGraceExpiresAt.Valid {
		key.RotationGraceExpiresAt = &rotationGraceExpiresAt.Time
	}
	if expiresAt.Valid {
		key.ExpiresAt = &expiresAt.Time
	}
	if lastUsedAt.Valid {
		key.LastUsedAt = &lastUsedAt.Time
	}
	return key, nil
}

func (r *PostgresRepository) ListAPIKeys(ctx context.Context) ([]APIKeyRecord, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+apiKeySelectColumns+`
FROM api_keys
ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]APIKeyRecord, 0)
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) FindAPIKeyByHash(ctx context.Context, hash string) (APIKeyRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+apiKeySelectColumns+`
FROM api_keys
WHERE key_hash = $1`, hash)
	key, err := scanAPIKey(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return APIKeyRecord{}, false, nil
		}
		return APIKeyRecord{}, false, err
	}
	return key, true, nil
}

func (r *PostgresRepository) SaveAPIKey(ctx context.Context, key APIKeyRecord) error {
	return saveAPIKey(ctx, r.db, key)
}

const apiKeyInsertStatement = `
INSERT INTO api_keys(
  id, name, key_hash, fingerprint, prefix, status, key_type, customer_id, owner_user_id,
  profile_scope, platform_tenant_id, gateway_principal_id, tenant_id, principal_type, principal_reference,
  policy_id, scopes, model_allowlist, allowed_modalities, allowed_operations,
  qps_limit, rpm_limit, tpm_limit, concurrency_limit, monthly_token_limit, monthly_budget_cents,
  monthly_image_limit, monthly_video_seconds_limit, monthly_audio_seconds_limit,
  allowed_cidrs, lane_policy, artifact_policy, artifact_sink_id, rotation_family_id,
  replaces_key_id, replaced_by_key_id, rotation_grace_expires_at,
  expires_at, last_used_at, created_at, updated_at
)
VALUES(
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,
  $21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39,$40,$41
)
`

const apiKeyUpsertClause = `
ON CONFLICT(id) DO UPDATE SET
  name = EXCLUDED.name,
  key_hash = EXCLUDED.key_hash,
  fingerprint = EXCLUDED.fingerprint,
  prefix = EXCLUDED.prefix,
  status = EXCLUDED.status,
  key_type = EXCLUDED.key_type,
  customer_id = EXCLUDED.customer_id,
  owner_user_id = EXCLUDED.owner_user_id,
  profile_scope = EXCLUDED.profile_scope,
  platform_tenant_id = EXCLUDED.platform_tenant_id,
  gateway_principal_id = EXCLUDED.gateway_principal_id,
  tenant_id = EXCLUDED.tenant_id,
  principal_type = EXCLUDED.principal_type,
  principal_reference = EXCLUDED.principal_reference,
  policy_id = EXCLUDED.policy_id,
  scopes = EXCLUDED.scopes,
  model_allowlist = EXCLUDED.model_allowlist,
  allowed_modalities = EXCLUDED.allowed_modalities,
  allowed_operations = EXCLUDED.allowed_operations,
  qps_limit = EXCLUDED.qps_limit,
  rpm_limit = EXCLUDED.rpm_limit,
  tpm_limit = EXCLUDED.tpm_limit,
  concurrency_limit = EXCLUDED.concurrency_limit,
  monthly_token_limit = EXCLUDED.monthly_token_limit,
  monthly_budget_cents = EXCLUDED.monthly_budget_cents,
  monthly_image_limit = EXCLUDED.monthly_image_limit,
  monthly_video_seconds_limit = EXCLUDED.monthly_video_seconds_limit,
  monthly_audio_seconds_limit = EXCLUDED.monthly_audio_seconds_limit,
  allowed_cidrs = EXCLUDED.allowed_cidrs,
  lane_policy = EXCLUDED.lane_policy,
  artifact_policy = EXCLUDED.artifact_policy,
  artifact_sink_id = EXCLUDED.artifact_sink_id,
  rotation_family_id = EXCLUDED.rotation_family_id,
  replaces_key_id = EXCLUDED.replaces_key_id,
  replaced_by_key_id = EXCLUDED.replaced_by_key_id,
  rotation_grace_expires_at = EXCLUDED.rotation_grace_expires_at,
  expires_at = EXCLUDED.expires_at,
  last_used_at = EXCLUDED.last_used_at,
  updated_at = EXCLUDED.updated_at
`

func apiKeyWriteArgs(key APIKeyRecord) []any {
	scopes := marshalStringList(key.Scopes)
	allowlist := marshalStringList(key.ModelAllowlist)
	modalities := marshalStringList(key.AllowedModalities)
	operations := marshalStringList(key.AllowedOperations)
	allowedCIDRs := marshalStringList(key.AllowedCIDRs)
	return []any{
		key.ID, key.Name, key.KeyHash, key.Fingerprint, key.Prefix, key.Status, key.KeyType, key.CustomerID, key.OwnerUserID,
		key.ProfileScope, key.PlatformTenantID, key.GatewayPrincipalID, key.TenantID, key.PrincipalType, key.PrincipalReference,
		key.PolicyID, scopes, allowlist, modalities, operations,
		key.QPSLimit, key.RPMLimit, key.TPMLimit, key.ConcurrencyLimit, key.MonthlyTokenLimit, key.MonthlyBudgetCents,
		key.MonthlyImageLimit, key.MonthlyVideoSecondsLimit, key.MonthlyAudioSecondsLimit,
		allowedCIDRs, key.LanePolicy, key.ArtifactPolicy, key.ArtifactSinkID, key.RotationFamilyID,
		key.ReplacesKeyID, key.ReplacedByKeyID, key.RotationGraceExpiresAt,
		key.ExpiresAt, key.LastUsedAt, key.CreatedAt, key.UpdatedAt,
	}
}

func saveAPIKey(ctx context.Context, executor usageRecordExecutor, key APIKeyRecord) error {
	_, err := executor.ExecContext(ctx, apiKeyInsertStatement+apiKeyUpsertClause, apiKeyWriteArgs(key)...)
	return err
}

func insertAPIKey(ctx context.Context, executor usageRecordExecutor, key APIKeyRecord) error {
	_, err := executor.ExecContext(ctx, apiKeyInsertStatement, apiKeyWriteArgs(key)...)
	return err
}

func (r *PostgresRepository) RotateAPIKeyPair(ctx context.Context, previous APIKeyRecord, replacement APIKeyRecord, audit AuditLog, expectedUpdatedAt time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
UPDATE api_keys
SET status=$1, rotation_family_id=$2, replaced_by_key_id=$3, rotation_grace_expires_at=$4, updated_at=$5
WHERE id=$6 AND replaced_by_key_id='' AND updated_at=$7
`, previous.Status, previous.RotationFamilyID, previous.ReplacedByKeyID, previous.RotationGraceExpiresAt, previous.UpdatedAt, previous.ID, expectedUpdatedAt)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		var replacedBy string
		var updatedAt time.Time
		if err := tx.QueryRowContext(ctx, `SELECT replaced_by_key_id, updated_at FROM api_keys WHERE id=$1 FOR UPDATE`, previous.ID).Scan(&replacedBy, &updatedAt); err != nil {
			return err
		}
		if replacedBy != "" {
			return ErrAPIKeyAlreadyRotated
		}
		return ErrAPIKeyChangedDuringRotation
	}
	if err := insertAPIKey(ctx, tx, replacement); err != nil {
		return err
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit()
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
	return saveUsageRecord(ctx, r.db, record)
}

type usageRecordExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func saveUsageRecord(ctx context.Context, executor usageRecordExecutor, record UsageRecord) error {
	usageDimensionsJSON, err := UsageDimensionsJSON(record.UsageDimensions)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `
INSERT INTO usage_records(id, operation_id, attempt_id, usage_version, usage_source, request_fingerprint, api_key_id, customer_id, profile_scope, platform_tenant_id, platform_tenant_name, gateway_principal_id, gateway_principal_name, external_auth_integration_id, external_subject_reference, api_fingerprint, model, upstream_model, protocol, provider_id, provider_account_id, status, error_type, latency_ms, ttft_ms, input_tokens, output_tokens, total_input_tokens, uncached_input_tokens, cache_read_tokens, cache_write_5m_tokens, cache_write_1h_tokens, cache_fields_present, usage_dimensions, usage_normalization_status, upstream_request_id, procurement_cost_micros, procurement_cost_currency, procurement_cost_source, procurement_cost_confidence, procurement_price_id, provider_billing_line_id, cost_cents, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34::jsonb,$35,$36,$37,$38,$39,$40,$41,$42,$43,$44)
`, record.ID, record.OperationID, record.AttemptID, record.UsageVersion, record.UsageSource, record.RequestFingerprint, record.APIKeyID, record.CustomerID, record.ProfileScope, record.PlatformTenantID, record.PlatformTenantName, record.GatewayPrincipalID, record.GatewayPrincipalName, record.ExternalAuthIntegrationID, record.ExternalSubjectReference, record.APIFingerprint, record.Model, record.UpstreamModel, record.Protocol, record.ProviderID, record.ProviderAccountID, record.Status, record.ErrorType, record.LatencyMS, record.TTFTMS, record.InputTokens, record.OutputTokens, record.TotalInputTokens, record.UncachedInputTokens, record.CacheReadTokens, record.CacheWrite5mTokens, record.CacheWrite1hTokens, record.CacheFieldsPresent, usageDimensionsJSON, record.UsageNormalizationStatus, record.UpstreamRequestID, record.ProcurementCostMicros, record.ProcurementCostCurrency, record.ProcurementCostSource, record.ProcurementCostConfidence, record.ProcurementPriceID, record.ProviderBillingLineID, record.CostCents, record.CreatedAt)
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
SELECT id, operation_id, attempt_id, usage_version, usage_source, request_fingerprint, api_key_id, customer_id, profile_scope, platform_tenant_id, platform_tenant_name, gateway_principal_id, gateway_principal_name, external_auth_integration_id, external_subject_reference, api_fingerprint, model, upstream_model, protocol, provider_id, provider_account_id, status, error_type, latency_ms, ttft_ms, input_tokens, output_tokens, total_input_tokens, uncached_input_tokens, cache_read_tokens, cache_write_5m_tokens, cache_write_1h_tokens, cache_fields_present, usage_dimensions, usage_normalization_status, upstream_request_id, procurement_cost_micros, procurement_cost_currency, procurement_cost_source, procurement_cost_confidence, procurement_price_id, provider_billing_line_id, cost_cents, created_at
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
	out := make([]UsageRecord, 0)
	for rows.Next() {
		var record UsageRecord
		var usageDimensionsJSON []byte
		if err := rows.Scan(&record.ID, &record.OperationID, &record.AttemptID, &record.UsageVersion, &record.UsageSource, &record.RequestFingerprint, &record.APIKeyID, &record.CustomerID, &record.ProfileScope, &record.PlatformTenantID, &record.PlatformTenantName, &record.GatewayPrincipalID, &record.GatewayPrincipalName, &record.ExternalAuthIntegrationID, &record.ExternalSubjectReference, &record.APIFingerprint, &record.Model, &record.UpstreamModel, &record.Protocol, &record.ProviderID, &record.ProviderAccountID, &record.Status, &record.ErrorType, &record.LatencyMS, &record.TTFTMS, &record.InputTokens, &record.OutputTokens, &record.TotalInputTokens, &record.UncachedInputTokens, &record.CacheReadTokens, &record.CacheWrite5mTokens, &record.CacheWrite1hTokens, &record.CacheFieldsPresent, &usageDimensionsJSON, &record.UsageNormalizationStatus, &record.UpstreamRequestID, &record.ProcurementCostMicros, &record.ProcurementCostCurrency, &record.ProcurementCostSource, &record.ProcurementCostConfidence, &record.ProcurementPriceID, &record.ProviderBillingLineID, &record.CostCents, &record.CreatedAt); err != nil {
			return nil, err
		}
		record.UsageDimensions, err = ParseUsageDimensionsJSON(string(usageDimensionsJSON))
		if err != nil {
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
	       COALESCE(SUM(COALESCE((usage_dimensions->'output_images'->>'quantity')::BIGINT, 0)), 0),
	       COALESCE(SUM(COALESCE((usage_dimensions->'input_video_milliseconds'->>'quantity')::BIGINT, 0) + COALESCE((usage_dimensions->'output_video_milliseconds'->>'quantity')::BIGINT, 0)), 0),
	       COALESCE(SUM(COALESCE((usage_dimensions->'input_audio_milliseconds'->>'quantity')::BIGINT, 0) + COALESCE((usage_dimensions->'output_audio_milliseconds'->>'quantity')::BIGINT, 0)), 0),
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
		var outputImages int64
		var videoMilliseconds int64
		var audioMilliseconds int64
		var costCents int64
		var modelLatencyTotal int64
		if err := rows.Scan(&model, &requests, &errors, &tokens, &outputImages, &videoMilliseconds, &audioMilliseconds, &costCents, &modelLatencyTotal); err != nil {
			return UsageAggregate{}, err
		}
		avgLatency := int64(0)
		if requests > 0 {
			avgLatency = modelLatencyTotal / requests
		}
		aggregate.ByModel = append(aggregate.ByModel, UsageModelSummary{
			Model:             model,
			Requests:          int(requests),
			Errors:            int(errors),
			Tokens:            int(tokens),
			OutputImages:      outputImages,
			VideoMilliseconds: videoMilliseconds,
			AudioMilliseconds: audioMilliseconds,
			CostCents:         int(costCents),
			AvgLatency:        avgLatency,
		})
		aggregate.TotalRequests += int(requests)
		aggregate.ErrorRequests += int(errors)
		aggregate.TotalTokens += int(tokens)
		aggregate.TotalOutputImages = saturatingUsageAdd(aggregate.TotalOutputImages, outputImages)
		aggregate.TotalVideoDuration = saturatingUsageAdd(aggregate.TotalVideoDuration, videoMilliseconds)
		aggregate.TotalAudioDuration = saturatingUsageAdd(aggregate.TotalAudioDuration, audioMilliseconds)
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
INSERT INTO gateway_traces(id, operation_id, attempt_id, request_fingerprint, api_key_id, api_fingerprint, profile_scope, platform_tenant_id, platform_tenant_name, gateway_principal_id, gateway_principal_name, external_auth_integration_id, external_subject_reference, model, stream, message_count, provider_id, provider_account_id, gateway_model_id, route_id, route_group, upstream_model, route_source, route_reason, policy_id, policy_name, policy_source, policy_version, policy_snapshot, status, http_status, error_type, latency_ms, input_tokens, output_tokens, request_summary, response_summary, route_attempts, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39)
`, trace.ID, trace.OperationID, trace.AttemptID, trace.RequestFingerprint, trace.APIKeyID, trace.APIFingerprint, trace.ProfileScope, trace.PlatformTenantID, trace.PlatformTenantName, trace.GatewayPrincipalID, trace.GatewayPrincipalName, trace.ExternalAuthIntegrationID, trace.ExternalSubjectReference, trace.Model, trace.Stream, trace.MessageCount, trace.ProviderID, trace.ProviderAccountID, trace.GatewayModelID, trace.RouteID, trace.RouteGroup, trace.UpstreamModel, trace.RouteSource, trace.RouteReason, trace.PolicyID, trace.PolicyName, trace.PolicySource, trace.PolicyVersion, trace.PolicySnapshot, trace.Status, trace.HTTPStatus, trace.ErrorType, trace.LatencyMS, trace.InputTokens, trace.OutputTokens, trace.RequestSummary, trace.ResponseSummary, defaultJSONArray(trace.RouteAttempts), trace.CreatedAt)
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
	SELECT id, operation_id, attempt_id, request_fingerprint, api_key_id, api_fingerprint, profile_scope, platform_tenant_id, platform_tenant_name, gateway_principal_id, gateway_principal_name, external_auth_integration_id, external_subject_reference, model, stream, message_count, provider_id, provider_account_id, gateway_model_id, route_id, route_group, upstream_model, route_source, route_reason, policy_id, policy_name, policy_source, policy_version, policy_snapshot, status, http_status, error_type, latency_ms, input_tokens, output_tokens, request_summary, response_summary, route_attempts, created_at
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
	out := make([]GatewayTrace, 0)
	for rows.Next() {
		var trace GatewayTrace
		if err := rows.Scan(&trace.ID, &trace.OperationID, &trace.AttemptID, &trace.RequestFingerprint, &trace.APIKeyID, &trace.APIFingerprint, &trace.ProfileScope, &trace.PlatformTenantID, &trace.PlatformTenantName, &trace.GatewayPrincipalID, &trace.GatewayPrincipalName, &trace.ExternalAuthIntegrationID, &trace.ExternalSubjectReference, &trace.Model, &trace.Stream, &trace.MessageCount, &trace.ProviderID, &trace.ProviderAccountID, &trace.GatewayModelID, &trace.RouteID, &trace.RouteGroup, &trace.UpstreamModel, &trace.RouteSource, &trace.RouteReason, &trace.PolicyID, &trace.PolicyName, &trace.PolicySource, &trace.PolicyVersion, &trace.PolicySnapshot, &trace.Status, &trace.HTTPStatus, &trace.ErrorType, &trace.LatencyMS, &trace.InputTokens, &trace.OutputTokens, &trace.RequestSummary, &trace.ResponseSummary, &trace.RouteAttempts, &trace.CreatedAt); err != nil {
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
SELECT id, actor, action, resource_type, resource_id, summary, profile_scope, platform_tenant_id, platform_tenant_name, gateway_principal_id, gateway_principal_name, external_auth_integration_id, external_subject_reference, created_at
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
	out := make([]AuditLog, 0)
	for rows.Next() {
		var event AuditLog
		if err := rows.Scan(&event.ID, &event.Actor, &event.Action, &event.ResourceType, &event.ResourceID, &event.Summary, &event.ProfileScope, &event.PlatformTenantID, &event.PlatformTenantName, &event.GatewayPrincipalID, &event.GatewayPrincipalName, &event.ExternalAuthIntegrationID, &event.ExternalSubjectReference, &event.CreatedAt); err != nil {
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
	return insertAuditLog(ctx, r.db, event)
}

func insertAuditLog(ctx context.Context, executor usageRecordExecutor, event AuditLog) error {
	_, err := executor.ExecContext(ctx, `
INSERT INTO audit_logs(id, actor, action, resource_type, resource_id, summary, profile_scope, platform_tenant_id, platform_tenant_name, gateway_principal_id, gateway_principal_name, external_auth_integration_id, external_subject_reference, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
`, event.ID, event.Actor, event.Action, event.ResourceType, event.ResourceID, event.Summary, event.ProfileScope, event.PlatformTenantID, event.PlatformTenantName, event.GatewayPrincipalID, event.GatewayPrincipalName, event.ExternalAuthIntegrationID, event.ExternalSubjectReference, event.CreatedAt)
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
	if out == nil {
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
	if out == nil {
		return []ProviderAccountTempUnschedulableRule{}
	}
	return out
}
