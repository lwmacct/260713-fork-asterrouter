package controlplane

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/cryptoutil"
)

const systemActor = "system"

const (
	providerProbeTimeout   = 15 * time.Second
	providerProbeBodyLimit = 2 << 20
)

var (
	ErrGatewayUnauthorized     = errors.New("invalid gateway api key")
	ErrGatewayForbidden        = errors.New("gateway api key is not allowed to use this model")
	ErrGatewayPolicyForbidden  = errors.New("gateway credential policy does not allow this request")
	ErrGatewayRouteUnavailable = errors.New("no schedulable gateway route is available for this model")
	ErrGatewayRateLimited      = errors.New("gateway api key qps limit exceeded")
	ErrGatewayQuotaExceeded    = errors.New("gateway api key monthly token quota exceeded")
	ErrGatewayBudgetExceeded   = errors.New("workspace key monthly budget exceeded")
	ErrGatewayCapacityLimited  = errors.New("gateway credential capacity limit exceeded")
)

type Service struct {
	repo                           Repository
	gatewayPath                    string
	secretKey                      string
	now                            func() time.Time
	alertDispatcher                AlertDispatcher
	customerNotificationDispatcher CustomerNotificationDispatcher
	usageObserver                  UsageObserver
	credentialCapacityStore        CredentialCapacityStore
	providerCapacityMu             sync.RWMutex
	providerCapacityStore          ProviderCapacityStore
	routingAffinityMu              sync.RWMutex
	routingAffinityCoordinator     RoutingAffinityCoordinator
	aiJobRuntimeMu                 sync.RWMutex
	aiJobReadyIndex                AIJobReadyIndex
	aiJobAdmissionLimits           AIJobAdmissionLimits
	artifactStoreMu                sync.RWMutex
	artifactStores                 map[string]ArtifactStore
	artifactPrimaryDriver          string
	artifactSinkMu                 sync.RWMutex
	artifactSinks                  map[string]ArtifactSink
	artifactProxyMu                sync.RWMutex
	artifactProxies                map[string]ArtifactProxy
	outboxPublisherMu              sync.RWMutex
	outboxPublisher                TransactionalOutboxPublisher
	rateMu                         sync.Mutex
	rateWindows                    map[string][]time.Time
	jwksMu                         sync.Mutex
	externalAuthJWKSFetcher        externalAuthJWKSFetcher
	externalAuthJWKSCache          map[string]externalAuthJWKSCacheEntry
	platformUsageHTTPClient        *http.Client
	providerCacheProbeHTTPClient   *http.Client
	providerBillingHTTPClient      *http.Client
	providerBillingAdapters        *ProviderBillingAdapterRegistry
	slotMu                         sync.Mutex
	accountSlots                   map[string]int
	scheduler                      *gatewayScheduler
}

type AlertDispatcher interface {
	DispatchAlert(ctx context.Context, event AlertEvent) error
}

type CustomerNotificationDispatcher interface {
	DispatchCustomerNotification(ctx context.Context, user WorkspaceUser, notification CustomerNotification) error
}

// UsageObserver receives a usage record after it has been durably saved by the
// control plane. Implementations must be idempotent because delivery can be
// retried after a process restart.
type UsageObserver interface {
	OnGatewayUsage(ctx context.Context, record UsageRecord) error
}

func NewService(repo Repository, gatewayPath string, secretKey ...string) *Service {
	if gatewayPath == "" {
		gatewayPath = "/v1"
	}
	key := "asterrouter-local-development-secret"
	if len(secretKey) > 0 && strings.TrimSpace(secretKey[0]) != "" {
		key = strings.TrimSpace(secretKey[0])
	}
	capacityStore, _ := repo.(CredentialCapacityStore)
	return &Service{repo: repo, gatewayPath: gatewayPath, secretKey: key, now: time.Now, rateWindows: map[string][]time.Time{}, credentialCapacityStore: capacityStore, externalAuthJWKSFetcher: fetchExternalAuthJWKS, externalAuthJWKSCache: map[string]externalAuthJWKSCacheEntry{}, platformUsageHTTPClient: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("platform usage sink redirects are not allowed")
	}}, providerCacheProbeHTTPClient: &http.Client{Timeout: providerProbeTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("provider cache probe redirects are not allowed")
	}}, providerBillingHTTPClient: &http.Client{Timeout: providerBillingRequestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("provider billing redirects are not allowed")
	}}, providerBillingAdapters: NewProviderBillingAdapterRegistry(), accountSlots: map[string]int{}, scheduler: newGatewayScheduler(), providerCapacityStore: NewMemoryProviderCapacityStore(), artifactStores: map[string]ArtifactStore{}, artifactSinks: map[string]ArtifactSink{}, artifactProxies: map[string]ArtifactProxy{}}
}

func (s *Service) nowUTC() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

// TryAcquireProviderAccountSlot is the compatibility API for callers that only
// need a concurrency reservation. The configured ProviderCapacityStore remains
// authoritative; accountSlots is only a local ranking shadow.
func (s *Service) TryAcquireProviderAccountSlot(accountID string, limit int) (release func(), ok bool) {
	if limit <= 0 {
		return func() {}, true
	}
	permit, _, acquired, err := s.TryAcquireProviderAccountPermitContext(
		context.Background(), GatewayProvider{AccountID: strings.TrimSpace(accountID), Concurrency: limit, CircuitState: CircuitStateClosed}, 0, "",
	)
	if err != nil || !acquired {
		return func() {}, false
	}
	return permit.Release, true
}

// providerAccountSlotUsage returns the current in-process concurrency usage
// for accountID, used to compute the load ratio during candidate ranking.
func (s *Service) providerAccountSlotUsage(accountID string) int {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	return s.accountSlots[accountID]
}

func (s *Service) trackProviderAccountSlot(accountID string) func() {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return func() {}
	}
	s.slotMu.Lock()
	s.accountSlots[accountID]++
	s.slotMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.slotMu.Lock()
			defer s.slotMu.Unlock()
			if s.accountSlots[accountID] > 0 {
				s.accountSlots[accountID]--
			}
		})
	}
}

func (s *Service) SetAlertDispatcher(dispatcher AlertDispatcher) {
	s.alertDispatcher = dispatcher
}

func (s *Service) SetCustomerNotificationDispatcher(dispatcher CustomerNotificationDispatcher) {
	s.customerNotificationDispatcher = dispatcher
}

func (s *Service) SetUsageObserver(observer UsageObserver) {
	s.usageObserver = observer
}

func (s *Service) SetRoutingAffinityCoordinator(coordinator RoutingAffinityCoordinator) {
	if s == nil {
		return
	}
	s.routingAffinityMu.Lock()
	s.routingAffinityCoordinator = coordinator
	s.routingAffinityMu.Unlock()
}

func (s *Service) routingAffinityCoordinatorValue() RoutingAffinityCoordinator {
	if s == nil {
		return nil
	}
	s.routingAffinityMu.RLock()
	defer s.routingAffinityMu.RUnlock()
	return s.routingAffinityCoordinator
}

func (s *Service) EnsureSeedData(ctx context.Context) error {
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return err
	}
	if len(providers) > 0 {
		return nil
	}

	now := time.Now().UTC()
	provider := ProviderConnection{
		ID:               "prov_openai_compatible",
		Name:             "OpenAI-compatible Provider",
		Type:             "openai_compatible",
		BaseURL:          "https://api.openai.com/v1",
		Status:           ProviderStatusNeedsSecret,
		Models:           []string{"gpt-4o-mini", "gpt-4.1-mini"},
		Priority:         100,
		SecretConfigured: false,
		SecretHint:       "",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.repo.SaveProvider(ctx, provider); err != nil {
		return err
	}
	return s.audit(ctx, systemActor, "seed", "control_plane", "product_baseline", "Seeded product baseline provider")
}

func (s *Service) Dashboard(ctx context.Context) (Dashboard, error) {
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	keys, err := s.repo.ListAPIKeys(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	audit, err := s.repo.ListAuditLogs(ctx, 8)
	if err != nil {
		return Dashboard{}, err
	}

	var activeProviders, activeKeys int
	for _, provider := range providers {
		if provider.Status == ProviderStatusActive || provider.Status == ProviderStatusNeedsSecret {
			activeProviders++
		}
	}
	for _, key := range presentAPIKeys(keys, s.nowUTC()) {
		if key.LifecycleStatus == APIKeyLifecycleActive {
			activeKeys++
		}
	}
	models, err := s.GatewayModels(ctx)
	if err != nil {
		return Dashboard{}, err
	}

	return Dashboard{
		ProviderCount:       len(providers),
		ActiveProviderCount: activeProviders,
		APIKeyCount:         len(keys),
		ActiveAPIKeyCount:   activeKeys,
		Models:              models,
		RecentAudit:         audit,
	}, nil
}

func (s *Service) PlatformDashboard(ctx context.Context) (Dashboard, error) {
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	keys, err := s.repo.ListAPIKeys(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	audit, err := s.repo.QueryAuditLogs(ctx, AuditLogQuery{Limit: 8, ProfileScope: ProfileScopePlatform})
	if err != nil {
		return Dashboard{}, err
	}
	var activeProviders, activeKeys int
	for _, provider := range providers {
		if provider.Status == ProviderStatusActive || provider.Status == ProviderStatusNeedsSecret {
			activeProviders++
		}
	}
	for _, key := range presentAPIKeys(keys, s.nowUTC()) {
		if key.ProfileScope == ProfileScopePlatform {
			if key.LifecycleStatus == APIKeyLifecycleActive {
				activeKeys++
			}
		}
	}
	models, err := s.GatewayModels(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	return Dashboard{
		ProviderCount:       len(providers),
		ActiveProviderCount: activeProviders,
		APIKeyCount:         countPlatformKeys(keys),
		ActiveAPIKeyCount:   activeKeys,
		Models:              models,
		RecentAudit:         audit,
	}, nil
}

func countPlatformKeys(keys []APIKeyRecord) int {
	count := 0
	for _, key := range keys {
		if key.ProfileScope == ProfileScopePlatform {
			count++
		}
	}
	return count
}

func (s *Service) ListProviders(ctx context.Context) ([]ProviderConnection, error) {
	return s.repo.ListProviders(ctx)
}

func (s *Service) ListProviderHealthChecks(ctx context.Context) ([]ProviderHealthCheck, error) {
	return s.repo.ListLatestProviderHealthChecks(ctx)
}

func (s *Service) CreateProvider(ctx context.Context, actor string, req ProviderRequest) (ProviderConnection, error) {
	now := time.Now().UTC()
	provider, err := providerFromRequest(req, now)
	if err != nil {
		return ProviderConnection{}, err
	}
	provider.ID = "prov_" + randomID(10)
	if strings.TrimSpace(req.APIKey) != "" {
		ciphertext, err := encryptSecret(s.secretKey, req.APIKey)
		if err != nil {
			return ProviderConnection{}, err
		}
		provider.SecretCiphertext = ciphertext
	}
	if err := s.repo.SaveProvider(ctx, provider); err != nil {
		return ProviderConnection{}, err
	}
	if err := s.audit(ctx, actor, "create", "provider", provider.ID, fmt.Sprintf("Created provider %s", provider.Name)); err != nil {
		return ProviderConnection{}, err
	}
	return provider, nil
}

func (s *Service) UpdateProvider(ctx context.Context, actor string, id string, req ProviderRequest) (ProviderConnection, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ProviderConnection{}, errors.New("provider id is required")
	}
	existing, err := s.providerByID(ctx, id)
	if err != nil {
		return ProviderConnection{}, err
	}
	now := time.Now().UTC()
	provider, err := providerFromRequest(req, existing.CreatedAt)
	if err != nil {
		return ProviderConnection{}, err
	}
	provider.ID = existing.ID
	provider.CreatedAt = existing.CreatedAt
	provider.UpdatedAt = now
	provider.SecretCiphertext = existing.SecretCiphertext
	provider.SecretConfigured = existing.SecretConfigured
	provider.SecretHint = existing.SecretHint
	if strings.TrimSpace(req.APIKey) != "" {
		ciphertext, err := encryptSecret(s.secretKey, req.APIKey)
		if err != nil {
			return ProviderConnection{}, err
		}
		provider.SecretCiphertext = ciphertext
		provider.SecretConfigured = true
		provider.SecretHint = maskSecret(req.APIKey)
	}
	if req.Status == ProviderStatusActive && provider.SecretConfigured {
		provider.Status = ProviderStatusActive
	}
	if provider.Status == ProviderStatusActive && !provider.SecretConfigured {
		provider.Status = ProviderStatusNeedsSecret
	}
	if err := s.repo.SaveProvider(ctx, provider); err != nil {
		return ProviderConnection{}, err
	}
	if err := s.audit(ctx, actor, "update", "provider", provider.ID, fmt.Sprintf("Updated provider %s", provider.Name)); err != nil {
		return ProviderConnection{}, err
	}
	return provider, nil
}

func (s *Service) CheckProvider(ctx context.Context, actor string, id string) (ProviderHealthCheck, error) {
	provider, err := s.providerByID(ctx, id)
	if err != nil {
		return ProviderHealthCheck{}, err
	}
	started := time.Now()
	status, message, models := s.checkProviderConfiguration(provider)
	if status == "ok" && provider.Type == "openai_compatible" {
		discovered, probeMessage, probeErr := s.probeOpenAICompatibleModels(ctx, provider)
		if probeErr != nil {
			status = "error"
			message = probeMessage
		} else if len(discovered) == 0 {
			status = "warning"
			message = "Provider /models endpoint responded without models"
		} else {
			models = discovered
			message = fmt.Sprintf("Provider is reachable; discovered %d models", len(discovered))
			nextModels := mergeStringLists(provider.Models, discovered)
			if !sameStringList(provider.Models, nextModels) {
				provider.Models = nextModels
				provider.UpdatedAt = time.Now().UTC()
				if err := s.repo.SaveProvider(ctx, provider); err != nil {
					return ProviderHealthCheck{}, err
				}
			}
		}
	} else if status == "ok" {
		status = "warning"
		message = fmt.Sprintf("Provider type %s does not support automatic /models probe yet", provider.Type)
	}
	checkedAt := time.Now().UTC()
	result := ProviderHealthCheck{
		ID:         "phc_" + randomID(12),
		ProviderID: provider.ID,
		Status:     status,
		LatencyMS:  time.Since(started).Milliseconds(),
		Message:    message,
		Models:     models,
		CheckedAt:  checkedAt,
	}
	if err := s.repo.SaveProviderHealthCheck(ctx, result); err != nil {
		return ProviderHealthCheck{}, err
	}
	if err := s.syncProviderHealthAlert(ctx, provider, result); err != nil {
		return ProviderHealthCheck{}, err
	}
	_ = s.audit(ctx, actor, "check", "provider", provider.ID, fmt.Sprintf("Checked provider %s: %s", provider.Name, message))
	return result, nil
}

func (s *Service) checkProviderConfiguration(provider ProviderConnection) (string, string, []string) {
	if provider.Status == ProviderStatusDisabled {
		return "disabled", "Provider is disabled", provider.Models
	}
	if !validHTTPURL(provider.BaseURL) {
		return "error", "Provider base URL is invalid", provider.Models
	}
	if !provider.SecretConfigured || provider.SecretCiphertext == "" {
		return "warning", "Provider secret is not configured", provider.Models
	}
	return "ok", "Provider configuration is ready", provider.Models
}

func (s *Service) probeOpenAICompatibleModels(ctx context.Context, provider ProviderConnection) ([]string, string, error) {
	apiKey, err := decryptSecret(s.secretKey, provider.SecretCiphertext)
	if err != nil {
		return nil, "Provider secret cannot be decrypted", err
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, "Provider secret is empty", errors.New("provider secret is empty")
	}
	return probeOpenAICompatibleModelsWithKey(ctx, provider.BaseURL, apiKey, "Provider")
}

func probeOpenAICompatibleModelsWithKey(ctx context.Context, baseURL string, apiKey string, label string) ([]string, string, error) {
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	if label == "" {
		label = "Provider"
	}
	if apiKey == "" {
		return nil, label + " secret is empty", errors.New("provider secret is empty")
	}
	probeCtx, cancel := context.WithTimeout(ctx, providerProbeTimeout)
	defer cancel()

	endpoint := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, label + " /models request cannot be created", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Timeout: providerProbeTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Sprintf("%s /models request failed: %s", label, err.Error()), err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, providerProbeBodyLimit+1))
	if err != nil {
		return nil, label + " /models response cannot be read", err
	}
	if len(body) > providerProbeBodyLimit {
		return nil, label + " /models response is too large", errors.New("provider /models response is too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Sprintf("%s /models returned HTTP %d", label, resp.StatusCode), fmt.Errorf("provider /models returned HTTP %d", resp.StatusCode)
	}
	models, err := parseOpenAICompatibleModels(body)
	if err != nil {
		return nil, label + " /models response is not a supported model list", err
	}
	return models, "", nil
}

func parseOpenAICompatibleModels(body []byte) ([]string, error) {
	var objectPayload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &objectPayload); err == nil {
		models := make([]string, 0, len(objectPayload.Data)+len(objectPayload.Models))
		for _, item := range objectPayload.Data {
			models = append(models, item.ID)
		}
		for _, item := range objectPayload.Models {
			models = append(models, item.ID)
		}
		if cleaned := cleanStringList(models); len(cleaned) > 0 {
			return cleaned, nil
		}
	}

	var arrayPayload []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &arrayPayload); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(arrayPayload))
	for _, item := range arrayPayload {
		models = append(models, item.ID)
	}
	return cleanStringList(models), nil
}

func (s *Service) ListRoutingGroups(ctx context.Context) ([]RoutingGroup, error) {
	return s.repo.ListRoutingGroups(ctx)
}

func (s *Service) CreateRoutingGroup(ctx context.Context, actor string, req RoutingGroupRequest) (RoutingGroup, error) {
	now := time.Now().UTC()
	group, err := routingGroupFromRequest(req, now)
	if err != nil {
		return RoutingGroup{}, err
	}
	group.ID = "rg_" + randomID(10)
	if err := s.repo.SaveRoutingGroup(ctx, group); err != nil {
		return RoutingGroup{}, err
	}
	if err := s.audit(ctx, actor, "create", "routing_group", group.ID, fmt.Sprintf("Created routing group %s", group.Name)); err != nil {
		return RoutingGroup{}, err
	}
	return group, nil
}

func (s *Service) UpdateRoutingGroup(ctx context.Context, actor string, id string, req RoutingGroupRequest) (RoutingGroup, error) {
	existing, err := s.routingGroupByID(ctx, id)
	if err != nil {
		return RoutingGroup{}, err
	}
	group, err := routingGroupFromRequest(req, existing.CreatedAt)
	if err != nil {
		return RoutingGroup{}, err
	}
	group.ID = existing.ID
	group.CreatedAt = existing.CreatedAt
	group.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveRoutingGroup(ctx, group); err != nil {
		return RoutingGroup{}, err
	}
	if err := s.audit(ctx, actor, "update", "routing_group", group.ID, fmt.Sprintf("Updated routing group %s", group.Name)); err != nil {
		return RoutingGroup{}, err
	}
	return group, nil
}

func (s *Service) ListProviderAccounts(ctx context.Context) ([]ProviderAccount, error) {
	return s.repo.ListProviderAccounts(ctx)
}

func (s *Service) ListProviderAccountHealthChecks(ctx context.Context) ([]ProviderAccountHealthCheck, error) {
	return s.repo.ListLatestProviderAccountHealthChecks(ctx)
}

func (s *Service) CreateProviderAccount(ctx context.Context, actor string, req ProviderAccountRequest) (ProviderAccount, error) {
	if err := s.validateRoutingGroups(ctx, req.GroupIDs); err != nil {
		return ProviderAccount{}, err
	}
	provider, err := s.providerByID(ctx, req.ProviderID)
	if err != nil {
		return ProviderAccount{}, err
	}
	if strings.TrimSpace(req.Platform) == "" {
		req.Platform = provider.Type
	}
	now := time.Now().UTC()
	account, err := providerAccountFromRequest(req, now, true)
	if err != nil {
		return ProviderAccount{}, err
	}
	account.ID = "acct_" + randomID(10)
	if strings.TrimSpace(req.Secret) != "" {
		ciphertext, err := encryptSecret(s.secretKey, req.Secret)
		if err != nil {
			return ProviderAccount{}, err
		}
		account.SecretCiphertext = ciphertext
	}
	if err := s.repo.SaveProviderAccount(ctx, account); err != nil {
		return ProviderAccount{}, err
	}
	if err := s.audit(ctx, actor, "create", "provider_account", account.ID, fmt.Sprintf("Created provider account %s", account.Name)); err != nil {
		return ProviderAccount{}, err
	}
	return account, nil
}

func (s *Service) UpdateProviderAccount(ctx context.Context, actor string, id string, req ProviderAccountRequest) (ProviderAccount, error) {
	if err := s.validateRoutingGroups(ctx, req.GroupIDs); err != nil {
		return ProviderAccount{}, err
	}
	provider, err := s.providerByID(ctx, req.ProviderID)
	if err != nil {
		return ProviderAccount{}, err
	}
	if strings.TrimSpace(req.Platform) == "" {
		req.Platform = provider.Type
	}
	existing, err := s.providerAccountByID(ctx, id)
	if err != nil {
		return ProviderAccount{}, err
	}
	account, err := providerAccountFromRequest(req, existing.CreatedAt, existing.Schedulable)
	if err != nil {
		return ProviderAccount{}, err
	}
	account.ID = existing.ID
	account.CreatedAt = existing.CreatedAt
	account.UpdatedAt = time.Now().UTC()
	account.SecretConfigured = existing.SecretConfigured
	account.SecretHint = existing.SecretHint
	account.SecretCiphertext = existing.SecretCiphertext
	account.ErrorMessage = existing.ErrorMessage
	account.LastUsedAt = existing.LastUsedAt
	account.CooldownUntil = existing.CooldownUntil
	account.CircuitState = existing.CircuitState
	account.ConsecutiveFailures = existing.ConsecutiveFailures
	account.CircuitOpenedUntil = existing.CircuitOpenedUntil
	account.LastFailureAt = existing.LastFailureAt
	account.TempUnschedulableReason = existing.TempUnschedulableReason
	if strings.TrimSpace(req.Secret) != "" {
		ciphertext, err := encryptSecret(s.secretKey, req.Secret)
		if err != nil {
			return ProviderAccount{}, err
		}
		account.SecretCiphertext = ciphertext
		account.SecretConfigured = true
		account.SecretHint = maskSecret(req.Secret)
	}
	if err := s.repo.SaveProviderAccount(ctx, account); err != nil {
		return ProviderAccount{}, err
	}
	if err := s.audit(ctx, actor, "update", "provider_account", account.ID, fmt.Sprintf("Updated provider account %s", account.Name)); err != nil {
		return ProviderAccount{}, err
	}
	return account, nil
}

func (s *Service) CheckProviderAccount(ctx context.Context, actor string, id string) (ProviderAccountHealthCheck, error) {
	account, err := s.providerAccountByID(ctx, id)
	if err != nil {
		return ProviderAccountHealthCheck{}, err
	}
	provider, err := s.providerByID(ctx, account.ProviderID)
	if err != nil {
		return ProviderAccountHealthCheck{}, err
	}

	started := time.Now()
	status, message, models := s.checkProviderAccountConfiguration(account, provider)
	if status == "ok" && provider.Type == "openai_compatible" {
		apiKey, err := decryptSecret(s.secretKey, account.SecretCiphertext)
		if err != nil {
			status = "error"
			message = "Provider account secret cannot be decrypted"
		} else {
			discovered, probeMessage, probeErr := probeOpenAICompatibleModelsWithKey(ctx, provider.BaseURL, apiKey, "Provider account")
			if probeErr != nil {
				status = "error"
				message = probeMessage
			} else if len(discovered) == 0 {
				status = "warning"
				message = "Provider account /models endpoint responded without models"
			} else {
				models = discovered
				message = fmt.Sprintf("Provider account is reachable; discovered %d models", len(discovered))
				nextModels := mergeStringLists(account.Models, discovered)
				if !sameStringList(account.Models, nextModels) || account.Status == AccountStatusError || account.ErrorMessage != "" {
					account.Models = nextModels
					account.Status = AccountStatusActive
					account.ErrorMessage = ""
					account.UpdatedAt = time.Now().UTC()
					if err := s.repo.SaveProviderAccount(ctx, account); err != nil {
						return ProviderAccountHealthCheck{}, err
					}
				}
			}
		}
	} else if status == "ok" {
		status = "warning"
		message = fmt.Sprintf("Provider type %s does not support automatic account /models probe yet", provider.Type)
	}

	if status == "error" && account.Status != AccountStatusDisabled {
		account.Status = AccountStatusError
		account.ErrorMessage = message
		account.UpdatedAt = time.Now().UTC()
		if err := s.repo.SaveProviderAccount(ctx, account); err != nil {
			return ProviderAccountHealthCheck{}, err
		}
	}

	result := ProviderAccountHealthCheck{
		ID:         "pahc_" + randomID(12),
		AccountID:  account.ID,
		ProviderID: provider.ID,
		Status:     status,
		LatencyMS:  time.Since(started).Milliseconds(),
		Message:    message,
		Models:     models,
		CheckedAt:  time.Now().UTC(),
	}
	if err := s.repo.SaveProviderAccountHealthCheck(ctx, result); err != nil {
		return ProviderAccountHealthCheck{}, err
	}
	if err := s.syncProviderAccountHealthAlert(ctx, account, provider, result); err != nil {
		return ProviderAccountHealthCheck{}, err
	}
	_ = s.audit(ctx, actor, "check", "provider_account", account.ID, fmt.Sprintf("Checked provider account %s: %s", account.Name, message))
	return result, nil
}

func (s *Service) checkProviderAccountConfiguration(account ProviderAccount, provider ProviderConnection) (string, string, []string) {
	if account.Status == AccountStatusDisabled {
		return "disabled", "Provider account is disabled", account.Models
	}
	if provider.Status == ProviderStatusDisabled {
		return "disabled", "Provider connection is disabled", account.Models
	}
	if !validHTTPURL(provider.BaseURL) {
		return "error", "Provider base URL is invalid", account.Models
	}
	if account.ProviderID == "" {
		return "error", "Provider account is not bound to a provider connection", account.Models
	}
	if !account.SecretConfigured || account.SecretCiphertext == "" {
		return "warning", "Provider account secret is not configured", account.Models
	}
	if account.AuthType != "api_key" {
		return "warning", fmt.Sprintf("Provider account auth type %s does not support automatic probe yet", account.AuthType), account.Models
	}
	return "ok", "Provider account configuration is ready", account.Models
}

func (s *Service) ListAPIKeys(ctx context.Context) ([]APIKeyRecord, error) {
	keys, err := s.repo.ListAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	return presentAPIKeys(keys, s.nowUTC()), nil
}

func (s *Service) CreateAPIKey(ctx context.Context, actor string, req APIKeyCreateRequest) (APIKeyCreateResponse, error) {
	if strings.TrimSpace(req.PlatformTenantID) != "" || strings.TrimSpace(req.GatewayPrincipalID) != "" {
		return APIKeyCreateResponse{}, errors.New("platform API keys must be created through the platform control plane")
	}
	return s.createAPIKey(ctx, actor, req, nil)
}

func (s *Service) createAPIKey(ctx context.Context, actor string, req APIKeyCreateRequest, platformIdentity *platformCredentialIdentity) (APIKeyCreateResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return APIKeyCreateResponse{}, errors.New("name is required")
	}
	models := cleanStringList(req.ModelAllowlist)
	if len(models) == 0 {
		return APIKeyCreateResponse{}, errors.New("model_allowlist must not be empty")
	}
	keyPolicy, err := normalizeAPIKeyPolicy(apiKeyPolicyFromCreateRequest(req))
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	if err := s.validateGovernancePolicyReference(ctx, req.PolicyID); err != nil {
		return APIKeyCreateResponse{}, err
	}
	expiresAt, err := parseOptionalDate(req.ExpiresAt)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	keyType, customerID, ownerUserID, err := normalizeAPIKeyOwnership(req.KeyType, req.CustomerID, req.OwnerUserID)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	if err := s.validateAPIKeyOwner(ctx, keyType, ownerUserID); err != nil {
		return APIKeyCreateResponse{}, err
	}

	rawKey := "ar_" + randomToken(32)
	hash := hashAPIKey(rawKey)
	now := time.Now().UTC()
	record := APIKeyRecord{
		ID:             "key_" + randomID(10),
		Name:           name,
		KeyHash:        hash,
		Fingerprint:    fingerprint(hash),
		Prefix:         prefix(rawKey, 10),
		Status:         APIKeyStatusActive,
		KeyType:        keyType,
		CustomerID:     customerID,
		OwnerUserID:    ownerUserID,
		PolicyID:       strings.TrimSpace(req.PolicyID),
		ModelAllowlist: models,
		ExpiresAt:      expiresAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	record.LifecycleStatus = APIKeyLifecycleActive
	applyAPIKeyPolicy(&record, keyPolicy)
	applyAPIKeyPrincipal(&record, platformIdentity)
	if err := s.repo.SaveAPIKey(ctx, record); err != nil {
		return APIKeyCreateResponse{}, err
	}
	if platformIdentity != nil {
		if err := s.auditPlatform(ctx, actor, "create", "api_key", record.ID, fmt.Sprintf("Created platform API key %s", record.Name), &platformIdentity.tenant, &platformIdentity.principal); err != nil {
			return APIKeyCreateResponse{}, err
		}
		return APIKeyCreateResponse{Record: record, Key: rawKey}, nil
	}
	if err := s.audit(ctx, actor, "create", "api_key", record.ID, fmt.Sprintf("Created API key %s", record.Name)); err != nil {
		return APIKeyCreateResponse{}, err
	}
	return APIKeyCreateResponse{Record: record, Key: rawKey}, nil
}

func (s *Service) UpdateAPIKey(ctx context.Context, actor string, id string, req APIKeyUpdateRequest) (APIKeyRecord, error) {
	key, err := s.apiKeyByID(ctx, id)
	if err != nil {
		return APIKeyRecord{}, err
	}
	if key.ProfileScope == ProfileScopePlatform {
		return APIKeyRecord{}, errors.New("platform API keys must be updated through the platform control plane")
	}
	if strings.TrimSpace(req.PlatformTenantID) != "" || strings.TrimSpace(req.GatewayPrincipalID) != "" {
		return APIKeyRecord{}, errors.New("platform API key ownership is immutable")
	}
	return s.updateAPIKey(ctx, actor, key, req)
}

func (s *Service) updateAPIKey(ctx context.Context, actor string, key APIKeyRecord, req APIKeyUpdateRequest) (APIKeyRecord, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return APIKeyRecord{}, errors.New("name is required")
	}
	models := cleanStringList(req.ModelAllowlist)
	if len(models) == 0 {
		return APIKeyRecord{}, errors.New("model_allowlist must not be empty")
	}
	keyPolicy, err := normalizeAPIKeyPolicy(apiKeyPolicyForUpdate(key, req))
	if err != nil {
		return APIKeyRecord{}, err
	}
	if err := s.validateGovernancePolicyReference(ctx, req.PolicyID); err != nil {
		return APIKeyRecord{}, err
	}
	status := req.Status
	if status == "" {
		status = key.Status
	}
	if !oneOf(status, APIKeyStatusActive, APIKeyStatusDisabled) {
		return APIKeyRecord{}, errors.New("status must be active or disabled")
	}
	expiresAt, err := parseOptionalDate(req.ExpiresAt)
	if err != nil {
		return APIKeyRecord{}, err
	}
	keyType := req.KeyType
	if strings.TrimSpace(keyType) == "" {
		keyType = key.KeyType
	}
	customerID := req.CustomerID
	if strings.TrimSpace(customerID) == "" && keyType == APIKeyTypeCustomer {
		customerID = key.CustomerID
	}
	ownerUserID := req.OwnerUserID
	if strings.TrimSpace(ownerUserID) == "" && keyType == APIKeyTypeUser {
		ownerUserID = key.OwnerUserID
	}
	keyType, customerID, ownerUserID, err = normalizeAPIKeyOwnership(keyType, customerID, ownerUserID)
	if err != nil {
		return APIKeyRecord{}, err
	}
	if err := s.validateAPIKeyOwner(ctx, keyType, ownerUserID); err != nil {
		return APIKeyRecord{}, err
	}
	key.Name = name
	key.PolicyID = strings.TrimSpace(req.PolicyID)
	key.ModelAllowlist = models
	key.ExpiresAt = expiresAt
	key.Status = status
	key.KeyType = keyType
	key.CustomerID = customerID
	key.OwnerUserID = ownerUserID
	applyAPIKeyPolicy(&key, keyPolicy)
	var platformIdentity *platformCredentialIdentity
	if key.ProfileScope == ProfileScopePlatform {
		identity, identityErr := s.activePlatformCredentialIdentity(ctx, key.PlatformTenantID, key.GatewayPrincipalID)
		if identityErr != nil {
			return APIKeyRecord{}, identityErr
		}
		platformIdentity = &identity
	}
	applyAPIKeyPrincipal(&key, platformIdentity)
	key.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveAPIKey(ctx, key); err != nil {
		return APIKeyRecord{}, err
	}
	if platformIdentity != nil {
		if err := s.auditPlatform(ctx, actor, "update", "api_key", key.ID, fmt.Sprintf("Updated platform API key %s policy", key.Name), &platformIdentity.tenant, &platformIdentity.principal); err != nil {
			return APIKeyRecord{}, err
		}
		return key, nil
	}
	if err := s.audit(ctx, actor, "update", "api_key", key.ID, fmt.Sprintf("Updated API key %s policy", key.Name)); err != nil {
		return APIKeyRecord{}, err
	}
	return key, nil
}

func normalizeAPIKeyOwnership(keyType string, customerID string, ownerUserID string) (string, string, string, error) {
	keyType = strings.TrimSpace(keyType)
	if keyType == "" {
		keyType = APIKeyTypeWorkspace
	}
	if !oneOf(keyType, APIKeyTypeWorkspace, APIKeyTypeUser, APIKeyTypeCustomer, APIKeyTypeService) {
		return "", "", "", errors.New("key_type must be workspace, user, customer, or service")
	}
	customerID = strings.TrimSpace(customerID)
	if keyType == APIKeyTypeCustomer && customerID == "" {
		return "", "", "", errors.New("customer_id is required for customer keys")
	}
	if keyType != APIKeyTypeCustomer {
		customerID = ""
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	if keyType == APIKeyTypeUser && ownerUserID == "" {
		return "", "", "", errors.New("owner_user_id is required for user keys")
	}
	if keyType != APIKeyTypeUser {
		ownerUserID = ""
	}
	return keyType, customerID, ownerUserID, nil
}

func (s *Service) validateAPIKeyOwner(ctx context.Context, keyType string, ownerUserID string) error {
	if keyType != APIKeyTypeUser || isLocalAdminActor(ownerUserID) {
		return nil
	}
	user, err := s.workspaceUserByID(ctx, ownerUserID)
	if err != nil {
		return err
	}
	if user.Status != WorkspaceUserStatusActive {
		return errors.New("owner workspace user must be active")
	}
	return nil
}

func (s *Service) RotateAPIKey(ctx context.Context, actor string, id string) (APIKeyCreateResponse, error) {
	return s.RotateAPIKeyWithGrace(ctx, actor, id, 0)
}

func (s *Service) RotateAPIKeyWithGrace(ctx context.Context, actor string, id string, gracePeriodSeconds int) (APIKeyCreateResponse, error) {
	if gracePeriodSeconds < 0 || gracePeriodSeconds > 86400 {
		return APIKeyCreateResponse{}, errors.New("grace_period_seconds must be between 0 and 86400")
	}
	key, err := s.apiKeyByID(ctx, id)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	if key.ProfileScope == ProfileScopePlatform {
		return APIKeyCreateResponse{}, errors.New("platform API keys must be rotated through the platform control plane")
	}
	return s.rotateAPIKey(ctx, actor, key, nil, time.Duration(gracePeriodSeconds)*time.Second)
}

func (s *Service) rotateAPIKey(ctx context.Context, actor string, key APIKeyRecord, platformIdentity *platformCredentialIdentity, gracePeriod time.Duration) (APIKeyCreateResponse, error) {
	if key.ReplacedByKeyID != "" {
		return APIKeyCreateResponse{}, ErrAPIKeyAlreadyRotated
	}
	rawKey := "ar_" + randomToken(32)
	hash := hashAPIKey(rawKey)
	now := s.nowUTC()
	expectedUpdatedAt := key.UpdatedAt
	if key.RotationFamilyID == "" {
		key.RotationFamilyID = "key_family_" + randomID(10)
	}
	replacement := key
	replacement.ID = "key_" + randomID(10)
	replacement.KeyHash = hash
	replacement.Fingerprint = fingerprint(hash)
	replacement.Prefix = prefix(rawKey, 10)
	replacement.Status = APIKeyStatusActive
	replacement.ReplacesKeyID = key.ID
	replacement.ReplacedByKeyID = ""
	replacement.RotationGraceExpiresAt = nil
	replacement.LastUsedAt = nil
	replacement.LifecycleStatus = APIKeyLifecycleActive
	replacement.CreatedAt = now
	replacement.UpdatedAt = now
	graceExpiresAt := now.Add(gracePeriod)
	key.ReplacedByKeyID = replacement.ID
	key.RotationGraceExpiresAt = &graceExpiresAt
	key.UpdatedAt = now
	if gracePeriod <= 0 {
		key.Status = APIKeyStatusDisabled
	}
	audit := s.newAuditLog(actor, "rotate", "api_key", replacement.ID, fmt.Sprintf("Rotated API key %s from %s", replacement.Name, key.ID))
	if platformIdentity != nil {
		audit = s.newPlatformAuditLog(actor, "rotate", "api_key", replacement.ID, fmt.Sprintf("Rotated platform API key %s from %s", replacement.Name, key.ID), &platformIdentity.tenant, &platformIdentity.principal)
	}
	if err := s.repo.RotateAPIKeyPair(ctx, key, replacement, audit, expectedUpdatedAt); err != nil {
		return APIKeyCreateResponse{}, err
	}
	return APIKeyCreateResponse{Record: replacement, Key: rawKey}, nil
}

func presentAPIKeys(keys []APIKeyRecord, now time.Time) []APIKeyRecord {
	for index := range keys {
		keys[index].LifecycleStatus = apiKeyLifecycleStatus(keys[index], now)
	}
	return keys
}

func apiKeyLifecycleStatus(key APIKeyRecord, now time.Time) string {
	if key.ReplacedByKeyID != "" {
		if key.RotationGraceExpiresAt != nil && now.Before(*key.RotationGraceExpiresAt) {
			return APIKeyLifecycleRetiring
		}
		return APIKeyLifecycleRetired
	}
	if key.Status == APIKeyStatusActive {
		return APIKeyLifecycleActive
	}
	return APIKeyLifecycleDisabled
}

func (s *Service) DisableAPIKey(ctx context.Context, actor string, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("api key id is required")
	}
	key, err := s.apiKeyByID(ctx, id)
	if err != nil {
		return err
	}
	if key.ProfileScope == ProfileScopePlatform {
		return errors.New("platform API keys must be disabled through the platform control plane")
	}
	if err := s.repo.DisableAPIKey(ctx, id, time.Now().UTC()); err != nil {
		return err
	}
	return s.audit(ctx, actor, "disable", "api_key", id, "Disabled API key")
}

func (s *Service) ListAuditLogs(ctx context.Context, limit int) ([]AuditLog, error) {
	return s.ListAuditLogsQuery(ctx, AuditLogQuery{Limit: limit})
}

func (s *Service) ListAuditLogsQuery(ctx context.Context, query AuditLogQuery) ([]AuditLog, error) {
	return s.repo.QueryAuditLogs(ctx, query)
}

func (s *Service) AuditLogSummaryQuery(ctx context.Context, query AuditLogQuery) (AuditLogSummary, error) {
	return s.repo.SummarizeAuditLogs(ctx, query)
}

func (s *Service) AuthenticateGatewayKey(ctx context.Context, rawKey string) (GatewayAuthContext, error) {
	return s.authenticateGatewayKey(ctx, rawKey, true)
}

func (s *Service) authenticateGatewayKey(ctx context.Context, rawKey string, recordLastUsed bool) (GatewayAuthContext, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	key, ok, err := s.repo.FindAPIKeyByHash(ctx, hashAPIKey(rawKey))
	if err != nil {
		return GatewayAuthContext{}, err
	}
	if !ok || key.Status != APIKeyStatusActive {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	now := s.nowUTC()
	if key.ExpiresAt != nil && now.After(*key.ExpiresAt) {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	if key.ReplacedByKeyID != "" && (key.RotationGraceExpiresAt == nil || !now.Before(*key.RotationGraceExpiresAt)) {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	var platformTenant *PlatformTenant
	var gatewayPrincipal *GatewayPrincipal
	if key.ProfileScope == ProfileScopePlatform {
		identity, identityErr := s.activePlatformCredentialIdentity(ctx, key.PlatformTenantID, key.GatewayPrincipalID)
		if identityErr != nil {
			return GatewayAuthContext{}, ErrGatewayUnauthorized
		}
		platformTenant = &identity.tenant
		gatewayPrincipal = &identity.principal
	}
	if recordLastUsed {
		if err := s.repo.UpdateAPIKeyLastUsed(ctx, key.ID, now); err != nil {
			return GatewayAuthContext{}, err
		}
		key.LastUsedAt = &now
		key.UpdatedAt = now
	}
	policy, policySource, err := s.effectiveGatewayPolicy(ctx, key)
	if err != nil {
		return GatewayAuthContext{}, err
	}
	return GatewayAuthContext{APIKey: key, Policy: policy, PolicySource: policySource, PlatformTenant: platformTenant, GatewayPrincipal: gatewayPrincipal}, nil
}

func (s *Service) AuthorizeGatewayModel(ctx context.Context, rawKey string, model string) (GatewayAuthContext, error) {
	return s.AuthorizeGatewayCredential(ctx, rawKey, "", model)
}

// AuthenticateGatewayCredential accepts exactly one trusted public-gateway
// credential source. Control-plane routes intentionally do not use it.
func (s *Service) AuthenticateGatewayCredential(ctx context.Context, rawKey, signedContext string) (GatewayAuthContext, error) {
	return s.authenticateGatewayCredential(ctx, rawKey, signedContext, true)
}

func (s *Service) authenticateGatewayCredential(ctx context.Context, rawKey, signedContext string, recordLastUsed bool) (GatewayAuthContext, error) {
	if strings.TrimSpace(signedContext) != "" {
		if strings.TrimSpace(rawKey) != "" {
			return GatewayAuthContext{}, ErrGatewayUnauthorized
		}
		return s.AuthenticateExternalAuthContext(ctx, signedContext)
	}
	if isExternalJWTToken(rawKey) {
		return s.AuthenticateExternalJWT(ctx, rawKey)
	}
	return s.authenticateGatewayKey(ctx, rawKey, recordLastUsed)
}

func isExternalJWTToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > externalAuthContextMaxBytes || strings.Count(value, ".") != 2 {
		return false
	}
	parts := strings.Split(value, ".")
	return len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != ""
}

func (s *Service) AuthorizeGatewayCredential(ctx context.Context, rawKey, signedContext, model string) (GatewayAuthContext, error) {
	auth, err := s.AuthenticateGatewayCredential(ctx, rawKey, signedContext)
	if err != nil {
		return GatewayAuthContext{}, err
	}
	model = strings.TrimSpace(model)
	if model == "" || !s.gatewayModelAllowed(auth, model) {
		return GatewayAuthContext{}, ErrGatewayForbidden
	}
	return auth, nil
}

func (s *Service) EnforceGatewayPolicy(ctx context.Context, auth GatewayAuthContext) error {
	now := s.nowUTC()
	if _, blocked, err := s.repo.FindActiveGatewayRiskBlock(ctx, auth.APIKey.ID, now); err != nil {
		return err
	} else if blocked {
		return ErrGatewayRiskBlocked
	}
	currentMonth := monthStart(now)
	monthlyTokenLimit := auth.effectiveMonthlyTokenLimit()
	if monthlyTokenLimit > 0 {
		used, err := s.repo.SumUsageTokensByAPIKeySince(ctx, auth.APIKey.ID, currentMonth)
		if err != nil {
			return err
		}
		if used >= monthlyTokenLimit {
			_ = s.syncAPIKeyQuotaAlert(ctx, auth, used, now)
			if auth.shouldBlockOverage() {
				return ErrGatewayQuotaExceeded
			}
		}
	}
	monthlyBudgetCents := auth.effectiveMonthlyBudgetCents()
	if monthlyBudgetCents > 0 {
		used, err := s.repo.SumUsageCostCentsByAPIKeySince(ctx, auth.APIKey.ID, currentMonth)
		if err != nil {
			return err
		}
		if used >= monthlyBudgetCents {
			_ = s.syncAPIKeyBudgetAlert(ctx, auth, used, now)
			if auth.shouldBlockOverage() {
				return ErrGatewayBudgetExceeded
			}
		}
	}
	qpsLimit := auth.effectiveQPSLimit()
	if qpsLimit > 0 && !s.allowGatewayRequest(auth.APIKey.ID, qpsLimit, now) {
		if auth.shouldBlockOverage() {
			return ErrGatewayRateLimited
		}
	}
	return nil
}

func (s *Service) allowGatewayRequest(apiKeyID string, limit int, now time.Time) bool {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	cutoff := now.Add(-time.Second)
	window := s.rateWindows[apiKeyID]
	kept := window[:0]
	for _, ts := range window {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= limit {
		s.rateWindows[apiKeyID] = kept
		return false
	}
	kept = append(kept, now)
	s.rateWindows[apiKeyID] = kept
	return true
}

func (s *Service) GatewayModelsForKey(ctx context.Context, rawKey string) ([]string, error) {
	return s.GatewayModelsForCredential(ctx, rawKey, "")
}

func (s *Service) GatewayModelsForCredential(ctx context.Context, rawKey, signedContext string) ([]string, error) {
	auth, err := s.AuthenticateGatewayCredential(ctx, rawKey, signedContext)
	if err != nil {
		return nil, err
	}
	return s.GatewayModelsForAuth(ctx, auth)
}

func (s *Service) GatewayModelsForAuth(ctx context.Context, auth GatewayAuthContext) ([]string, error) {
	models, err := s.GatewayModels(ctx)
	if err != nil {
		return nil, err
	}
	allowed := make([]string, 0, len(models))
	for _, model := range models {
		if s.gatewayModelAllowed(auth, model) {
			allowed = append(allowed, model)
		}
	}
	return allowed, nil
}

func (s *Service) RecordGatewayCall(ctx context.Context, auth GatewayAuthContext, model string, status string, summary string) error {
	if summary == "" {
		summary = fmt.Sprintf("Gateway call for model %s", model)
	}
	actor := "api_key:" + auth.APIKey.Fingerprint
	credentialLabel := "workspace_key=" + auth.APIKey.ID
	if auth.ExternalAuthIntegration != nil {
		actor = "external_auth:" + auth.ExternalAuthIntegration.ID + ":" + auth.APIKey.Fingerprint
		credentialLabel = "external_auth_integration=" + auth.ExternalAuthIntegration.ID
	}
	resourceID := "call_" + randomID(12)
	event := AuditLog{
		ID:           "audit_" + randomID(12),
		Actor:        actor,
		Action:       "invoke",
		ResourceType: "gateway_call",
		ResourceID:   resourceID,
		Summary:      fmt.Sprintf("%s; %s status=%s", summary, credentialLabel, status),
		CreatedAt:    time.Now().UTC(),
	}
	applyGatewayPlatformSnapshotToAudit(&event, auth)
	return s.repo.AddAuditLog(ctx, event)
}

type GatewayUsageInput struct {
	OperationID                 string
	AttemptID                   string
	UsageVersion                int
	UsageSource                 string
	RequestFingerprint          string
	Model                       string
	UpstreamModel               string
	Protocol                    string
	ProviderID                  string
	ProviderAccountID           string
	Status                      string
	ErrorType                   string
	LatencyMS                   int64
	TTFTMS                      *int64
	InputTokens                 int
	OutputTokens                int
	TotalInputTokens            *int
	UncachedInputTokens         *int
	CacheReadTokens             *int
	CacheWrite5mTokens          *int
	CacheWrite1hTokens          *int
	CacheFieldsPresent          bool
	UsageDimensions             UsageDimensions
	UsageNormalizationStatus    string
	UpstreamRequestID           string
	ProcurementCostMicros       *int64
	ProcurementCostCurrency     string
	ProcurementCostSource       string
	ProcurementCostConfidence   string
	ProcurementPriceID          string
	ProviderBillingLineID       string
	SkipProcurementCostEstimate bool
	CostCents                   int
}

type GatewayTraceInput struct {
	OperationID        string
	AttemptID          string
	RequestFingerprint string
	Model              string
	Stream             bool
	MessageCount       int
	ProviderID         string
	ProviderAccountID  string
	GatewayModelID     string
	RouteID            string
	RouteGroup         string
	UpstreamModel      string
	RouteSource        string
	RouteReason        string
	Status             string
	HTTPStatus         int
	ErrorType          string
	LatencyMS          int64
	InputTokens        int
	OutputTokens       int
	RequestSummary     string
	ResponseSummary    string
	RouteAttempts      string
}

func (s *Service) RecordGatewayUsage(ctx context.Context, auth GatewayAuthContext, in GatewayUsageInput) error {
	normalizeUsageLedgerInput(&in)
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "accepted"
	}
	costCents := nonNegative(in.CostCents)
	if costCents == 0 && (in.InputTokens > 0 || in.OutputTokens > 0) {
		if estimated, ok, err := s.EstimateModelUsageCostCents(ctx, in.Model, in.InputTokens, in.OutputTokens); err == nil && ok {
			costCents = estimated
		}
	}
	procurementCostMicros := nonNegativeInt64Pointer(in.ProcurementCostMicros)
	procurementCostCurrency := strings.ToUpper(strings.TrimSpace(in.ProcurementCostCurrency))
	procurementCostSource := strings.TrimSpace(in.ProcurementCostSource)
	procurementCostConfidence := strings.TrimSpace(in.ProcurementCostConfidence)
	procurementPriceID := strings.TrimSpace(in.ProcurementPriceID)
	if procurementCostMicros == nil && !in.SkipProcurementCostEstimate {
		if estimate, ok, estimateErr := s.estimateGatewayProcurementCost(ctx, in, s.nowUTC()); estimateErr != nil {
			return estimateErr
		} else if ok {
			procurementCostMicros = &estimate.CostMicros
			procurementCostCurrency = estimate.Currency
			procurementCostSource = estimate.Source
			procurementCostConfidence = estimate.Confidence
			procurementPriceID = estimate.PriceID
		}
	}
	if procurementCostSource == "" {
		procurementCostSource = "unknown"
	}
	if procurementCostConfidence == "" {
		procurementCostConfidence = ProcurementCostConfidenceUnknown
	}
	usageNormalizationStatus := strings.TrimSpace(in.UsageNormalizationStatus)
	if usageNormalizationStatus == "" {
		usageNormalizationStatus = "unknown"
	}
	usageDimensions, err := NormalizeUsageDimensions(in.UsageDimensions)
	if err != nil {
		return err
	}
	record := UsageRecord{
		ID:                        "usage_" + randomID(12),
		OperationID:               strings.TrimSpace(in.OperationID),
		AttemptID:                 strings.TrimSpace(in.AttemptID),
		UsageVersion:              in.UsageVersion,
		UsageSource:               strings.TrimSpace(in.UsageSource),
		RequestFingerprint:        strings.TrimSpace(in.RequestFingerprint),
		APIKeyID:                  auth.APIKey.ID,
		CustomerID:                auth.APIKey.CustomerID,
		APIFingerprint:            auth.APIKey.Fingerprint,
		Model:                     strings.TrimSpace(in.Model),
		UpstreamModel:             strings.TrimSpace(in.UpstreamModel),
		Protocol:                  strings.TrimSpace(in.Protocol),
		ProviderID:                strings.TrimSpace(in.ProviderID),
		ProviderAccountID:         strings.TrimSpace(in.ProviderAccountID),
		Status:                    status,
		ErrorType:                 strings.TrimSpace(in.ErrorType),
		LatencyMS:                 in.LatencyMS,
		TTFTMS:                    nonNegativeInt64Pointer(in.TTFTMS),
		InputTokens:               nonNegative(in.InputTokens),
		OutputTokens:              nonNegative(in.OutputTokens),
		TotalInputTokens:          nonNegativeIntPointer(in.TotalInputTokens),
		UncachedInputTokens:       nonNegativeIntPointer(in.UncachedInputTokens),
		CacheReadTokens:           nonNegativeIntPointer(in.CacheReadTokens),
		CacheWrite5mTokens:        nonNegativeIntPointer(in.CacheWrite5mTokens),
		CacheWrite1hTokens:        nonNegativeIntPointer(in.CacheWrite1hTokens),
		CacheFieldsPresent:        in.CacheFieldsPresent,
		UsageDimensions:           usageDimensions,
		UsageNormalizationStatus:  usageNormalizationStatus,
		UpstreamRequestID:         strings.TrimSpace(in.UpstreamRequestID),
		ProcurementCostMicros:     procurementCostMicros,
		ProcurementCostCurrency:   procurementCostCurrency,
		ProcurementCostSource:     procurementCostSource,
		ProcurementCostConfidence: procurementCostConfidence,
		ProcurementPriceID:        procurementPriceID,
		ProviderBillingLineID:     strings.TrimSpace(in.ProviderBillingLineID),
		CostCents:                 costCents,
		CreatedAt:                 s.nowUTC(),
	}
	applyGatewayPlatformSnapshotToUsage(&record, auth)
	if record.OperationID != "" {
		record.ID = "usage_" + usageLedgerDigest(record)
	}
	events, err := s.platformUsageDeliveryEventsForRecord(ctx, record)
	if err != nil {
		return err
	}
	applied := true
	if record.OperationID != "" {
		billing, outbox, ledgerErr := usageLedgerRecords(record)
		if ledgerErr != nil {
			return ledgerErr
		}
		record.ID = billing.UsageRecordID
		applied, err = s.repo.ApplyUsageLedger(ctx, record, billing, outbox, events)
	} else if len(events) > 0 {
		err = s.repo.SaveUsageRecordAndEnqueuePlatformUsage(ctx, record, events)
	} else {
		err = s.repo.SaveUsageRecord(ctx, record)
	}
	if err != nil {
		return err
	}
	if !applied {
		return nil
	}
	if s.usageObserver != nil {
		if err := s.usageObserver.OnGatewayUsage(ctx, record); err != nil {
			_ = s.audit(ctx, systemActor, "usage_observer_error", "usage_record", record.ID, err.Error())
		}
	}
	_ = s.syncCustomerUsageNotifications(ctx, auth, record)
	if auth.effectiveMonthlyTokenLimit() > 0 && (in.InputTokens > 0 || in.OutputTokens > 0) {
		_ = s.syncAPIKeyQuotaAlertForAuth(ctx, auth, record.CreatedAt)
	}
	if auth.effectiveMonthlyBudgetCents() > 0 && costCents > 0 {
		_ = s.syncAPIKeyBudgetAlertForAuth(ctx, auth, record.CreatedAt)
	}
	_ = s.syncGatewayErrorRateAlert(ctx, auth)
	return nil
}

func (s *Service) RecordGatewayTrace(ctx context.Context, auth GatewayAuthContext, in GatewayTraceInput) error {
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "accepted"
	}
	policyID, policyName, policySource, policyVersion, policySnapshot := gatewayTracePolicyEvidence(auth)
	trace := GatewayTrace{
		ID:                 "trace_" + randomID(12),
		OperationID:        strings.TrimSpace(in.OperationID),
		AttemptID:          strings.TrimSpace(in.AttemptID),
		RequestFingerprint: strings.TrimSpace(in.RequestFingerprint),
		APIKeyID:           auth.APIKey.ID,
		APIFingerprint:     auth.APIKey.Fingerprint,
		Model:              strings.TrimSpace(in.Model),
		Stream:             in.Stream,
		MessageCount:       nonNegative(in.MessageCount),
		ProviderID:         strings.TrimSpace(in.ProviderID),
		ProviderAccountID:  strings.TrimSpace(in.ProviderAccountID),
		GatewayModelID:     strings.TrimSpace(in.GatewayModelID),
		RouteID:            strings.TrimSpace(in.RouteID),
		RouteGroup:         strings.TrimSpace(in.RouteGroup),
		UpstreamModel:      strings.TrimSpace(in.UpstreamModel),
		RouteSource:        strings.TrimSpace(in.RouteSource),
		RouteReason:        strings.TrimSpace(in.RouteReason),
		PolicyID:           policyID,
		PolicyName:         policyName,
		PolicySource:       policySource,
		PolicyVersion:      policyVersion,
		PolicySnapshot:     policySnapshot,
		Status:             status,
		HTTPStatus:         nonNegative(in.HTTPStatus),
		ErrorType:          strings.TrimSpace(in.ErrorType),
		LatencyMS:          in.LatencyMS,
		InputTokens:        nonNegative(in.InputTokens),
		OutputTokens:       nonNegative(in.OutputTokens),
		RequestSummary:     strings.TrimSpace(in.RequestSummary),
		ResponseSummary:    strings.TrimSpace(in.ResponseSummary),
		RouteAttempts:      strings.TrimSpace(in.RouteAttempts),
		CreatedAt:          time.Now().UTC(),
	}
	applyGatewayPlatformSnapshotToTrace(&trace, auth)
	return s.repo.SaveGatewayTrace(ctx, trace)
}

func applyGatewayPlatformSnapshotToAudit(event *AuditLog, auth GatewayAuthContext) {
	if event == nil || auth.APIKey.ProfileScope != ProfileScopePlatform || auth.PlatformTenant == nil || auth.GatewayPrincipal == nil {
		return
	}
	event.ProfileScope = ProfileScopePlatform
	event.PlatformTenantID = auth.PlatformTenant.ID
	event.PlatformTenantName = auth.PlatformTenant.Name
	event.GatewayPrincipalID = auth.GatewayPrincipal.ID
	event.GatewayPrincipalName = auth.GatewayPrincipal.Name
	if auth.ExternalAuthIntegration != nil {
		event.ExternalAuthIntegrationID = auth.ExternalAuthIntegration.ID
		event.ExternalSubjectReference = auth.ExternalSubjectReference
	}
}

func applyGatewayPlatformSnapshotToUsage(record *UsageRecord, auth GatewayAuthContext) {
	if record == nil || auth.APIKey.ProfileScope != ProfileScopePlatform || auth.PlatformTenant == nil || auth.GatewayPrincipal == nil {
		return
	}
	record.ProfileScope = ProfileScopePlatform
	record.PlatformTenantID = auth.PlatformTenant.ID
	record.PlatformTenantName = auth.PlatformTenant.Name
	record.GatewayPrincipalID = auth.GatewayPrincipal.ID
	record.GatewayPrincipalName = auth.GatewayPrincipal.Name
	if auth.ExternalAuthIntegration != nil {
		record.ExternalAuthIntegrationID = auth.ExternalAuthIntegration.ID
		record.ExternalSubjectReference = auth.ExternalSubjectReference
	}
}

func applyGatewayPlatformSnapshotToTrace(trace *GatewayTrace, auth GatewayAuthContext) {
	if trace == nil || auth.APIKey.ProfileScope != ProfileScopePlatform || auth.PlatformTenant == nil || auth.GatewayPrincipal == nil {
		return
	}
	trace.ProfileScope = ProfileScopePlatform
	trace.PlatformTenantID = auth.PlatformTenant.ID
	trace.PlatformTenantName = auth.PlatformTenant.Name
	trace.GatewayPrincipalID = auth.GatewayPrincipal.ID
	trace.GatewayPrincipalName = auth.GatewayPrincipal.Name
	if auth.ExternalAuthIntegration != nil {
		trace.ExternalAuthIntegrationID = auth.ExternalAuthIntegration.ID
		trace.ExternalSubjectReference = auth.ExternalSubjectReference
	}
}

type gatewayTracePolicySnapshot struct {
	Version             int    `json:"version"`
	QPSLimit            int    `json:"qps_limit"`
	MonthlyTokenLimit   int    `json:"monthly_token_limit"`
	MonthlyBudgetCents  int    `json:"monthly_budget_cents"`
	OverageAction       string `json:"overage_action"`
	PromptLoggingMode   string `json:"prompt_logging_mode"`
	RetentionDays       int    `json:"retention_days"`
	ModelAllowlistCount int    `json:"model_allowlist_count"`
	ModelDenylistCount  int    `json:"model_denylist_count"`
	ToolCallAllowed     bool   `json:"tool_call_allowed"`
	ImageInputAllowed   bool   `json:"image_input_allowed"`
	WebAccessAllowed    bool   `json:"web_access_allowed"`
}

func gatewayTracePolicyEvidence(auth GatewayAuthContext) (string, string, string, int, string) {
	if auth.Policy == nil {
		return "", "", "", 0, ""
	}
	version := governancePolicyVersion(*auth.Policy)
	snapshot := gatewayTracePolicySnapshot{
		Version:             version,
		QPSLimit:            auth.effectiveQPSLimit(),
		MonthlyTokenLimit:   auth.effectiveMonthlyTokenLimit(),
		MonthlyBudgetCents:  auth.effectiveMonthlyBudgetCents(),
		OverageAction:       strings.TrimSpace(auth.Policy.OverageAction),
		PromptLoggingMode:   strings.TrimSpace(auth.Policy.PromptLoggingMode),
		RetentionDays:       nonNegative(auth.Policy.RetentionDays),
		ModelAllowlistCount: len(auth.Policy.ModelAllowlist),
		ModelDenylistCount:  len(auth.Policy.ModelDenylist),
		ToolCallAllowed:     auth.Policy.ToolCallAllowed,
		ImageInputAllowed:   auth.Policy.ImageInputAllowed,
		WebAccessAllowed:    auth.Policy.WebAccessAllowed,
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return auth.Policy.ID, auth.Policy.Name, auth.PolicySource, version, ""
	}
	return auth.Policy.ID, auth.Policy.Name, auth.PolicySource, version, string(data)
}

func (s *Service) ListGatewayTraces(ctx context.Context, limit int) ([]GatewayTrace, error) {
	return s.ListGatewayTracesQuery(ctx, GatewayTraceQuery{Limit: limit})
}

func (s *Service) ListGatewayTracesQuery(ctx context.Context, query GatewayTraceQuery) ([]GatewayTrace, error) {
	return s.repo.QueryGatewayTraces(ctx, query)
}

func (s *Service) GatewayTraceSummaryQuery(ctx context.Context, query GatewayTraceQuery) (GatewayTraceSummary, error) {
	return s.repo.SummarizeGatewayTraces(ctx, query)
}

func (s *Service) UsageReport(ctx context.Context, limit int) (UsageReport, error) {
	return s.UsageReportQuery(ctx, UsageQuery{Limit: limit})
}

func (s *Service) UsageReportQuery(ctx context.Context, query UsageQuery) (UsageReport, error) {
	records, err := s.repo.QueryUsageRecords(ctx, query)
	if err != nil {
		return UsageReport{}, err
	}
	aggregate, err := s.repo.SummarizeUsageRecords(ctx, query)
	if err != nil {
		return UsageReport{}, err
	}
	return UsageReport{
		TotalRequests: aggregate.TotalRequests, ErrorRequests: aggregate.ErrorRequests, TotalTokens: aggregate.TotalTokens,
		TotalOutputImages: aggregate.TotalOutputImages, TotalVideoDuration: aggregate.TotalVideoDuration,
		TotalAudioDuration: aggregate.TotalAudioDuration, TotalCostCents: aggregate.TotalCostCents,
		AvgLatencyMS: aggregate.AvgLatencyMS, ByModel: aggregate.ByModel, Recent: records,
	}, nil
}

func usageAggregateFromRecords(records []UsageRecord) UsageAggregate {
	aggregate := UsageAggregate{}
	byModel := map[string]*usageAccumulator{}
	var latencyTotal int64
	for _, record := range records {
		aggregate.TotalRequests++
		if record.Status == "upstream_error" || record.Status == "error" || record.ErrorType != "" {
			aggregate.ErrorRequests++
		}
		tokens := record.InputTokens + record.OutputTokens
		aggregate.TotalTokens += tokens
		dimensions := UsageDimensionsTotals(record.UsageDimensions)
		aggregate.TotalOutputImages = saturatingUsageAdd(aggregate.TotalOutputImages, dimensions.OutputImages)
		aggregate.TotalVideoDuration = saturatingUsageAdd(aggregate.TotalVideoDuration, dimensions.VideoMilliseconds)
		aggregate.TotalAudioDuration = saturatingUsageAdd(aggregate.TotalAudioDuration, dimensions.AudioMilliseconds)
		aggregate.TotalCostCents += record.CostCents
		latencyTotal += record.LatencyMS
		acc := byModel[record.Model]
		if acc == nil {
			acc = &usageAccumulator{model: record.Model}
			byModel[record.Model] = acc
		}
		acc.requests++
		if record.Status == "upstream_error" || record.Status == "error" || record.ErrorType != "" {
			acc.errors++
		}
		acc.tokens += tokens
		acc.outputImages = saturatingUsageAdd(acc.outputImages, dimensions.OutputImages)
		acc.videoMilliseconds = saturatingUsageAdd(acc.videoMilliseconds, dimensions.VideoMilliseconds)
		acc.audioMilliseconds = saturatingUsageAdd(acc.audioMilliseconds, dimensions.AudioMilliseconds)
		acc.costCents += record.CostCents
		acc.latencyTotal += record.LatencyMS
	}
	if aggregate.TotalRequests > 0 {
		aggregate.AvgLatencyMS = latencyTotal / int64(aggregate.TotalRequests)
	}
	aggregate.ByModel = usageSummaries(byModel)
	return aggregate
}

type usageAccumulator struct {
	model             string
	requests          int
	errors            int
	tokens            int
	outputImages      int64
	videoMilliseconds int64
	audioMilliseconds int64
	costCents         int
	latencyTotal      int64
}

func (s *Service) RecordSystemEvent(ctx context.Context, actor string, action string, resourceID string, summary string) error {
	if strings.TrimSpace(resourceID) == "" {
		resourceID = "system"
	}
	if strings.TrimSpace(summary) == "" {
		summary = "System operation"
	}
	return s.audit(ctx, actor, action, "system", resourceID, summary)
}

func (s *Service) RecordPluginEvent(ctx context.Context, actor string, action string, pluginID string, summary string) error {
	if strings.TrimSpace(pluginID) == "" {
		pluginID = "plugin"
	}
	if strings.TrimSpace(summary) == "" {
		summary = "Plugin operation"
	}
	return s.audit(ctx, actor, action, "plugin", pluginID, summary)
}

func (s *Service) RecordExportEvent(ctx context.Context, actor string, action string, exportID string, summary string) error {
	if strings.TrimSpace(exportID) == "" {
		exportID = "export"
	}
	if strings.TrimSpace(summary) == "" {
		summary = "Export operation"
	}
	return s.audit(ctx, actor, action, "export_job", exportID, summary)
}

func (s *Service) GatewayModels(ctx context.Context) ([]string, error) {
	models, err := s.repo.ListGatewayModels(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(models))
	for _, model := range models {
		if model.Status == GatewayModelStatusActive {
			result = append(result, model.ModelID)
		}
	}
	sort.Strings(result)
	return result, nil
}

// providerAccountFailureCooldown is the default cooldown applied to an
// account after a request-level fallback triggers because that account
// failed to serve a request. It exists to avoid immediately re-selecting a
// just-failed account within the same burst of requests. Rule-based temporary
// unschedulability (error code + keyword matching) can override this with a
// longer, more specific duration.
const providerAccountFailureCooldown = 30 * time.Second

func (s *Service) GatewayProviderForModel(ctx context.Context, model string) (GatewayProvider, bool, error) {
	candidates, hasAccountPool, err := s.GatewayProviderCandidatesForModel(ctx, model)
	if err != nil {
		return GatewayProvider{}, false, err
	}
	if len(candidates) == 0 {
		if hasAccountPool {
			return GatewayProvider{}, false, ErrGatewayRouteUnavailable
		}
		return GatewayProvider{}, false, nil
	}
	selected := candidates[0]
	if selected.AccountID != "" {
		_ = s.TouchProviderAccountUsage(ctx, selected.AccountID)
	}
	return selected, true, nil
}

// GatewayProviderCandidatesForModel resolves an external model identifier and
// route group through explicit model_routes. Provider model arrays are only
// capability declarations; they never expose or route a gateway model by
// themselves.
func (s *Service) GatewayProviderCandidatesForModel(ctx context.Context, model string) ([]GatewayProvider, bool, error) {
	resolved, found, err := s.ResolveGatewayModel(ctx, model)
	if err != nil || !found {
		return nil, false, err
	}
	candidates, hasRoutes, err := s.rankedModelRouteCandidates(ctx, resolved)
	if err != nil {
		return nil, hasRoutes, err
	}
	if len(candidates) == 0 {
		return nil, hasRoutes, nil
	}
	routes := make([]GatewayProvider, 0, len(candidates))
	for _, entry := range candidates {
		secret, err := decryptSecret(s.secretKey, entry.account.SecretCiphertext)
		if err != nil {
			return nil, true, err
		}
		routes = append(routes, GatewayProvider{
			ID:               entry.provider.ID,
			Name:             entry.provider.Name,
			Type:             entry.provider.Type,
			BaseURL:          entry.provider.BaseURL,
			APIKey:           secret,
			AccountID:        entry.account.ID,
			AccountName:      entry.account.Name,
			Concurrency:      entry.account.Concurrency,
			GatewayModelID:   resolved.GatewayModel.ID,
			RequestedModel:   resolved.RequestedID,
			UpstreamModel:    entry.route.UpstreamModel,
			RouteID:          entry.route.ID,
			RouteGroup:       resolved.RouteGroup,
			RoutePriority:    entry.route.Priority,
			RouteWeight:      entry.route.Weight,
			AccountWeight:    entry.account.Weight,
			RPMLimit:         entry.account.RPMLimit,
			TPMLimit:         entry.account.TPMLimit,
			CircuitState:     entry.circuitState,
			CircuitProbe:     entry.circuitProbe,
			Headroom:         entry.headroom,
			StickyEnabled:    resolved.GatewayModel.StickyEnabled,
			StickyTTLSeconds: resolved.GatewayModel.StickyTTLSeconds,
			Source:           "model_route",
			SelectionReason:  fmt.Sprintf("selected route %s group=%s route_priority=%d account_priority=%d headroom=%.4g load_ratio=%.4g circuit=%s", entry.route.ID, resolved.RouteGroup, entry.route.Priority, entry.account.Priority, entry.headroom, entry.loadRatio, entry.circuitState),
		})
	}
	return routes, true, nil
}

type rankedProviderAccountCandidate struct {
	account   ProviderAccount
	provider  ProviderConnection
	loadRatio float64
}

// rankedProviderAccountCandidates returns the schedulable provider accounts
// for model, ordered by priority ascending, then current load ratio
// ascending (in-process concurrency usage / EffectiveLoadFactor), then
// billing rate multiplier ascending, then name. hasAccountPool reports
// whether any provider account exists at all (independent of eligibility),
// which callers use to decide whether falling back to a direct provider
// connection is allowed.
func (s *Service) rankedProviderAccountCandidates(ctx context.Context, model string) ([]rankedProviderAccountCandidate, bool, error) {
	accounts, err := s.repo.ListProviderAccounts(ctx)
	if err != nil {
		return nil, false, err
	}
	if len(accounts) == 0 {
		return nil, false, nil
	}
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return nil, true, err
	}
	providersByID := providerByIDMap(providers)
	now := time.Now().UTC()
	billingHealthByAccount, _ := s.providerBillingRoutingHealthByAccount(ctx, now)
	candidates := make([]rankedProviderAccountCandidate, 0, len(accounts))
	for _, account := range accounts {
		if !accountEligibleForRouting(account, model, now) {
			continue
		}
		if health, found := billingHealthByAccount[account.ID]; found && health.HardBlocked {
			continue
		}
		provider, ok := providersByID[account.ProviderID]
		if !ok || provider.Status == ProviderStatusDisabled || !validHTTPURL(provider.BaseURL) {
			continue
		}
		usage := s.providerAccountSlotUsage(account.ID)
		loadRatio := float64(usage) / float64(account.EffectiveLoadFactor())
		candidates = append(candidates, rankedProviderAccountCandidate{account: account, provider: provider, loadRatio: loadRatio})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.account.Priority != right.account.Priority {
			return left.account.Priority < right.account.Priority
		}
		if left.loadRatio != right.loadRatio {
			return left.loadRatio < right.loadRatio
		}
		if left.account.RateMultiplier != right.account.RateMultiplier {
			return left.account.RateMultiplier < right.account.RateMultiplier
		}
		return left.account.Name < right.account.Name
	})
	return candidates, true, nil
}

// TouchProviderAccountUsage records that accountID was just selected to serve
// a request, updating LastUsedAt for observability.
func (s *Service) TouchProviderAccountUsage(ctx context.Context, accountID string) error {
	account, err := s.providerAccountByID(ctx, accountID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	account.LastUsedAt = &now
	account.UpdatedAt = now
	return s.repo.SaveProviderAccount(ctx, account)
}

// RecordProviderAccountFailure applies a cooldown to accountID after a
// request-level fallback determined the account failed to serve a request
// (transport error, or an upstream status that indicates an account-side
// problem such as 401/403/429/5xx). httpStatus is 0 for transport-level
// failures (no upstream response was received). If any of the account's
// admin-configured temp-unschedulable rules match httpStatus and a keyword
// in responseBodyPreview, that rule's duration is used and the match is
// recorded in TempUnschedulableReason; otherwise a fixed short default
// cooldown is applied and any previous reason is cleared, since a bare
// transport hiccup is not the same as a diagnosed rule match.
func (s *Service) RecordProviderAccountFailure(ctx context.Context, accountID string, httpStatus int, responseBodyPreview string) error {
	account, err := s.providerAccountByID(ctx, accountID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	account.ConsecutiveFailures++
	account.LastFailureAt = &now
	threshold := account.CircuitFailureThreshold
	if threshold <= 0 {
		threshold = 5
	}
	openSeconds := account.CircuitOpenSeconds
	if openSeconds <= 0 {
		openSeconds = 60
	}
	if account.CircuitState == CircuitStateHalfOpen || account.ConsecutiveFailures >= threshold {
		openedUntil := now.Add(time.Duration(openSeconds) * time.Second)
		account.CircuitState = CircuitStateOpen
		account.CircuitOpenedUntil = &openedUntil
	}
	if rule, keyword, ok := matchTempUnschedulableRule(account.TempUnschedulableRules, httpStatus, responseBodyPreview); ok {
		cooldownUntil := now.Add(time.Duration(rule.DurationMinutes) * time.Minute)
		account.CooldownUntil = &cooldownUntil
		account.TempUnschedulableReason = fmt.Sprintf("matched rule status_code=%d keyword=%q duration_minutes=%d", rule.StatusCode, keyword, rule.DurationMinutes)
	} else {
		cooldownUntil := now.Add(providerAccountFailureCooldown)
		account.CooldownUntil = &cooldownUntil
		account.TempUnschedulableReason = ""
	}
	account.UpdatedAt = now
	return s.repo.SaveProviderAccount(ctx, account)
}

func (s *Service) RecordProviderAccountSuccess(ctx context.Context, accountID string) error {
	if strings.TrimSpace(accountID) == "" {
		return nil
	}
	account, err := s.providerAccountByID(ctx, accountID)
	if err != nil {
		return err
	}
	account.CircuitState = CircuitStateClosed
	account.ConsecutiveFailures = 0
	account.CircuitOpenedUntil = nil
	account.CooldownUntil = nil
	account.TempUnschedulableReason = ""
	account.UpdatedAt = time.Now().UTC()
	return s.repo.SaveProviderAccount(ctx, account)
}

// matchTempUnschedulableRule returns the first configured rule whose
// status code equals httpStatus and whose keyword list contains a
// case-insensitive substring match in responseBodyPreview, along with the
// matched keyword.
func matchTempUnschedulableRule(rules []ProviderAccountTempUnschedulableRule, httpStatus int, responseBodyPreview string) (ProviderAccountTempUnschedulableRule, string, bool) {
	if httpStatus == 0 || len(rules) == 0 {
		return ProviderAccountTempUnschedulableRule{}, "", false
	}
	body := strings.ToLower(responseBodyPreview)
	for _, rule := range rules {
		if rule.StatusCode != httpStatus {
			continue
		}
		for _, keyword := range rule.Keywords {
			if keyword == "" {
				continue
			}
			if strings.Contains(body, strings.ToLower(keyword)) {
				return rule, keyword, true
			}
		}
	}
	return ProviderAccountTempUnschedulableRule{}, "", false
}

// ClearProviderAccountCooldown removes any active cooldown (default or
// rule-matched) from accountID, making it immediately eligible for
// scheduling again subject to its other eligibility checks.
func (s *Service) ClearProviderAccountCooldown(ctx context.Context, actor string, accountID string) (ProviderAccount, error) {
	account, err := s.providerAccountByID(ctx, accountID)
	if err != nil {
		return ProviderAccount{}, err
	}
	account.CooldownUntil = nil
	account.CircuitState = CircuitStateClosed
	account.ConsecutiveFailures = 0
	account.CircuitOpenedUntil = nil
	account.TempUnschedulableReason = ""
	account.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveProviderAccount(ctx, account); err != nil {
		return ProviderAccount{}, err
	}
	if err := s.audit(ctx, actor, "clear_cooldown", "provider_account", account.ID, fmt.Sprintf("Cleared cooldown for provider account %s", account.Name)); err != nil {
		return ProviderAccount{}, err
	}
	return account, nil
}

func (s *Service) Health(ctx context.Context) error {
	return s.repo.Health(ctx)
}

func (s *Service) audit(ctx context.Context, actor, action, resourceType, resourceID, summary string) error {
	return s.repo.AddAuditLog(ctx, s.newAuditLog(actor, action, resourceType, resourceID, summary))
}

func (s *Service) newAuditLog(actor, action, resourceType, resourceID, summary string) AuditLog {
	if strings.TrimSpace(actor) == "" {
		actor = "local-admin"
	}
	return AuditLog{
		ID:           "audit_" + randomID(12),
		Actor:        actor,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Summary:      summary,
		CreatedAt:    s.nowUTC(),
	}
}

func (s *Service) providerByID(ctx context.Context, id string) (ProviderConnection, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ProviderConnection{}, errors.New("provider id is required")
	}
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return ProviderConnection{}, err
	}
	for _, provider := range providers {
		if provider.ID == id {
			return provider, nil
		}
	}
	return ProviderConnection{}, fmt.Errorf("provider %q not found", id)
}

func (s *Service) routingGroupByID(ctx context.Context, id string) (RoutingGroup, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return RoutingGroup{}, errors.New("routing group id is required")
	}
	groups, err := s.repo.ListRoutingGroups(ctx)
	if err != nil {
		return RoutingGroup{}, err
	}
	for _, group := range groups {
		if group.ID == id {
			return group, nil
		}
	}
	return RoutingGroup{}, fmt.Errorf("routing group %q not found", id)
}

func (s *Service) providerAccountByID(ctx context.Context, id string) (ProviderAccount, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ProviderAccount{}, errors.New("provider account id is required")
	}
	accounts, err := s.repo.ListProviderAccounts(ctx)
	if err != nil {
		return ProviderAccount{}, err
	}
	for _, account := range accounts {
		if account.ID == id {
			return account, nil
		}
	}
	return ProviderAccount{}, fmt.Errorf("provider account %q not found", id)
}

func providerByIDMap(providers []ProviderConnection) map[string]ProviderConnection {
	out := make(map[string]ProviderConnection, len(providers))
	for _, provider := range providers {
		out[provider.ID] = provider
	}
	return out
}

func accountEligibleForRouting(account ProviderAccount, model string, now time.Time) bool {
	if account.Status != AccountStatusActive || !account.Schedulable {
		return false
	}
	if account.AuthType != "api_key" {
		return false
	}
	if !account.SecretConfigured || account.SecretCiphertext == "" {
		return false
	}
	if account.ExpiresAt != nil && now.After(*account.ExpiresAt) {
		return false
	}
	if account.CooldownUntil != nil && now.Before(*account.CooldownUntil) {
		return false
	}
	model = strings.TrimSpace(model)
	if model != "" && !contains(account.Models, model) {
		return false
	}
	return true
}

func (s *Service) validateRoutingGroups(ctx context.Context, ids []string) error {
	cleanedIDs := cleanStringList(ids)
	if len(cleanedIDs) == 0 {
		return nil
	}
	groups, err := s.repo.ListRoutingGroups(ctx)
	if err != nil {
		return err
	}
	known := map[string]struct{}{}
	for _, group := range groups {
		known[group.ID] = struct{}{}
	}
	for _, id := range cleanedIDs {
		if _, ok := known[id]; !ok {
			return fmt.Errorf("routing group %q not found", id)
		}
	}
	return nil
}

func (s *Service) apiKeyByID(ctx context.Context, id string) (APIKeyRecord, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return APIKeyRecord{}, errors.New("api key id is required")
	}
	keys, err := s.repo.ListAPIKeys(ctx)
	if err != nil {
		return APIKeyRecord{}, err
	}
	for _, key := range keys {
		if key.ID == id {
			return key, nil
		}
	}
	return APIKeyRecord{}, fmt.Errorf("api key %q not found", id)
}

func providerFromRequest(req ProviderRequest, now time.Time) (ProviderConnection, error) {
	name := strings.TrimSpace(req.Name)
	providerType := strings.TrimSpace(req.Type)
	baseURL := strings.TrimSpace(req.BaseURL)
	if name == "" {
		return ProviderConnection{}, errors.New("name is required")
	}
	if !oneOf(providerType, "openai_compatible", "azure_openai", "anthropic", "gemini", "self_hosted") {
		return ProviderConnection{}, errors.New("type must be openai_compatible, azure_openai, anthropic, gemini, or self_hosted")
	}
	if !validHTTPURL(baseURL) {
		return ProviderConnection{}, errors.New("base_url must be an absolute http or https URL")
	}
	status := req.Status
	if status == "" {
		status = ProviderStatusActive
	}
	if req.APIKey == "" && status == ProviderStatusActive {
		status = ProviderStatusNeedsSecret
	}
	if !oneOf(status, ProviderStatusActive, ProviderStatusDisabled, ProviderStatusNeedsSecret) {
		return ProviderConnection{}, errors.New("status must be active, disabled, or needs_secret")
	}
	priority := req.Priority
	if priority <= 0 {
		priority = 100
	}
	return ProviderConnection{
		Name:             name,
		Type:             providerType,
		BaseURL:          strings.TrimRight(baseURL, "/"),
		Status:           status,
		Models:           cleanStringList(req.Models),
		Priority:         priority,
		SecretConfigured: strings.TrimSpace(req.APIKey) != "",
		SecretHint:       maskSecret(req.APIKey),
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

func routingGroupFromRequest(req RoutingGroupRequest, now time.Time) (RoutingGroup, error) {
	name := strings.TrimSpace(req.Name)
	platform := strings.TrimSpace(req.Platform)
	groupType := strings.TrimSpace(req.GroupType)
	if name == "" {
		return RoutingGroup{}, errors.New("name is required")
	}
	if platform == "" {
		return RoutingGroup{}, errors.New("platform is required")
	}
	if groupType == "" {
		groupType = RoutingGroupTypeStandard
	}
	if !oneOf(groupType, RoutingGroupTypeStandard, RoutingGroupTypeSubscription, RoutingGroupTypeExclusive, RoutingGroupTypeImageGeneration, RoutingGroupTypeVideoGeneration) {
		return RoutingGroup{}, errors.New("group_type must be standard, subscription, exclusive, image_generation, or video_generation")
	}
	status := req.Status
	if status == "" {
		status = RoutingGroupStatusActive
	}
	if !oneOf(status, RoutingGroupStatusActive, RoutingGroupStatusDisabled) {
		return RoutingGroup{}, errors.New("status must be active or disabled")
	}
	rateMultiplier := req.RateMultiplier
	if rateMultiplier == 0 {
		rateMultiplier = 1
	}
	if rateMultiplier < 0 {
		return RoutingGroup{}, errors.New("rate_multiplier must be greater than or equal to 0")
	}
	if req.RPMLimit < 0 {
		return RoutingGroup{}, errors.New("rpm_limit must be greater than or equal to 0")
	}
	if err := validateNonNegativeIntFields(map[string]int{
		"daily_budget_cents":      req.DailyBudgetCents,
		"weekly_budget_cents":     req.WeeklyBudgetCents,
		"monthly_budget_cents":    req.MonthlyBudgetCents,
		"image_price_1k_cents":    req.ImagePrice1KCents,
		"image_price_2k_cents":    req.ImagePrice2KCents,
		"image_price_4k_cents":    req.ImagePrice4KCents,
		"video_price_480p_cents":  req.VideoPrice480PCents,
		"video_price_720p_cents":  req.VideoPrice720PCents,
		"video_price_1080p_cents": req.VideoPrice1080PCents,
	}); err != nil {
		return RoutingGroup{}, err
	}
	imageRateMultiplier := req.ImageRateMultiplier
	if imageRateMultiplier == 0 {
		imageRateMultiplier = 1
	}
	videoRateMultiplier := req.VideoRateMultiplier
	if videoRateMultiplier == 0 {
		videoRateMultiplier = 1
	}
	batchImageDiscountMultiplier := req.BatchImageDiscountMultiplier
	if batchImageDiscountMultiplier == 0 {
		batchImageDiscountMultiplier = 1
	}
	peakRateMultiplier := req.PeakRateMultiplier
	if peakRateMultiplier == 0 {
		peakRateMultiplier = 1
	}
	if err := validateNonNegativeFloatFields(map[string]float64{
		"image_rate_multiplier":           imageRateMultiplier,
		"video_rate_multiplier":           videoRateMultiplier,
		"batch_image_discount_multiplier": batchImageDiscountMultiplier,
		"peak_rate_multiplier":            peakRateMultiplier,
	}); err != nil {
		return RoutingGroup{}, err
	}
	isExclusive := req.IsExclusive
	dailyBudgetCents := req.DailyBudgetCents
	weeklyBudgetCents := req.WeeklyBudgetCents
	monthlyBudgetCents := req.MonthlyBudgetCents
	imageEnabled := req.ImageEnabled
	batchImageEnabled := req.BatchImageEnabled
	imagePrice1KCents := req.ImagePrice1KCents
	imagePrice2KCents := req.ImagePrice2KCents
	imagePrice4KCents := req.ImagePrice4KCents
	videoEnabled := req.VideoEnabled
	videoPrice480PCents := req.VideoPrice480PCents
	videoPrice720PCents := req.VideoPrice720PCents
	videoPrice1080PCents := req.VideoPrice1080PCents
	peakRateEnabled := req.PeakRateEnabled
	switch groupType {
	case RoutingGroupTypeStandard:
		dailyBudgetCents = 0
		weeklyBudgetCents = 0
		monthlyBudgetCents = 0
		imageEnabled = false
		batchImageEnabled = false
		imagePrice1KCents = 0
		imagePrice2KCents = 0
		imagePrice4KCents = 0
		videoEnabled = false
		videoPrice480PCents = 0
		videoPrice720PCents = 0
		videoPrice1080PCents = 0
		peakRateEnabled = false
	case RoutingGroupTypeSubscription:
		if req.DailyBudgetCents == 0 && req.WeeklyBudgetCents == 0 && req.MonthlyBudgetCents == 0 {
			return RoutingGroup{}, errors.New("subscription groups require at least one budget limit")
		}
		imageEnabled = false
		batchImageEnabled = false
		imagePrice1KCents = 0
		imagePrice2KCents = 0
		imagePrice4KCents = 0
		videoEnabled = false
		videoPrice480PCents = 0
		videoPrice720PCents = 0
		videoPrice1080PCents = 0
	case RoutingGroupTypeExclusive:
		isExclusive = true
		dailyBudgetCents = 0
		weeklyBudgetCents = 0
		monthlyBudgetCents = 0
		imageEnabled = false
		batchImageEnabled = false
		imagePrice1KCents = 0
		imagePrice2KCents = 0
		imagePrice4KCents = 0
		videoEnabled = false
		videoPrice480PCents = 0
		videoPrice720PCents = 0
		videoPrice1080PCents = 0
		peakRateEnabled = false
	case RoutingGroupTypeImageGeneration:
		dailyBudgetCents = 0
		weeklyBudgetCents = 0
		monthlyBudgetCents = 0
		imageEnabled = true
		videoEnabled = false
		videoPrice480PCents = 0
		videoPrice720PCents = 0
		videoPrice1080PCents = 0
		peakRateEnabled = false
	case RoutingGroupTypeVideoGeneration:
		dailyBudgetCents = 0
		weeklyBudgetCents = 0
		monthlyBudgetCents = 0
		videoEnabled = true
		imageEnabled = false
		batchImageEnabled = false
		imagePrice1KCents = 0
		imagePrice2KCents = 0
		imagePrice4KCents = 0
		peakRateEnabled = false
	}
	peakStart := strings.TrimSpace(req.PeakStart)
	peakEnd := strings.TrimSpace(req.PeakEnd)
	if peakRateEnabled && (peakStart == "" || peakEnd == "") {
		return RoutingGroup{}, errors.New("peak_start and peak_end are required when peak_rate_enabled is true")
	}
	if !peakRateEnabled {
		peakStart = ""
		peakEnd = ""
		peakRateMultiplier = 1
	}
	return RoutingGroup{
		Name:                         name,
		Description:                  strings.TrimSpace(req.Description),
		Platform:                     platform,
		GroupType:                    groupType,
		RateMultiplier:               rateMultiplier,
		RPMLimit:                     req.RPMLimit,
		IsExclusive:                  isExclusive,
		DailyBudgetCents:             dailyBudgetCents,
		WeeklyBudgetCents:            weeklyBudgetCents,
		MonthlyBudgetCents:           monthlyBudgetCents,
		ImageEnabled:                 imageEnabled,
		BatchImageEnabled:            batchImageEnabled,
		ImageRateMultiplier:          imageRateMultiplier,
		BatchImageDiscountMultiplier: batchImageDiscountMultiplier,
		ImagePrice1KCents:            imagePrice1KCents,
		ImagePrice2KCents:            imagePrice2KCents,
		ImagePrice4KCents:            imagePrice4KCents,
		VideoEnabled:                 videoEnabled,
		VideoRateMultiplier:          videoRateMultiplier,
		VideoPrice480PCents:          videoPrice480PCents,
		VideoPrice720PCents:          videoPrice720PCents,
		VideoPrice1080PCents:         videoPrice1080PCents,
		PeakRateEnabled:              peakRateEnabled,
		PeakStart:                    peakStart,
		PeakEnd:                      peakEnd,
		PeakRateMultiplier:           peakRateMultiplier,
		Status:                       status,
		SortOrder:                    req.SortOrder,
		CreatedAt:                    now,
		UpdatedAt:                    now,
	}, nil
}

func validateNonNegativeIntFields(values map[string]int) error {
	for name, value := range values {
		if value < 0 {
			return fmt.Errorf("%s must be greater than or equal to 0", name)
		}
	}
	return nil
}

func validateNonNegativeFloatFields(values map[string]float64) error {
	for name, value := range values {
		if value < 0 {
			return fmt.Errorf("%s must be greater than or equal to 0", name)
		}
	}
	return nil
}

func providerAccountFromRequest(req ProviderAccountRequest, now time.Time, defaultSchedulable bool) (ProviderAccount, error) {
	providerID := strings.TrimSpace(req.ProviderID)
	name := strings.TrimSpace(req.Name)
	platform := strings.TrimSpace(req.Platform)
	authType := strings.TrimSpace(req.AuthType)
	if providerID == "" {
		return ProviderAccount{}, errors.New("provider_id is required")
	}
	if name == "" {
		return ProviderAccount{}, errors.New("name is required")
	}
	if platform == "" {
		return ProviderAccount{}, errors.New("platform is required")
	}
	if authType == "" {
		authType = "api_key"
	}
	if !oneOf(authType, "api_key") {
		return ProviderAccount{}, errors.New("auth_type must be api_key")
	}
	status := req.Status
	if status == "" {
		status = AccountStatusActive
	}
	if !oneOf(status, AccountStatusActive, AccountStatusError, AccountStatusDisabled) {
		return ProviderAccount{}, errors.New("status must be active, error, or disabled")
	}
	concurrency := req.Concurrency
	if concurrency == 0 {
		concurrency = 3
	}
	if concurrency < 0 {
		return ProviderAccount{}, errors.New("concurrency must be greater than or equal to 0")
	}
	var loadFactor *int
	if req.LoadFactor != nil {
		if *req.LoadFactor < 0 {
			return ProviderAccount{}, errors.New("load_factor must be greater than or equal to 0")
		}
		if *req.LoadFactor > 0 {
			v := *req.LoadFactor
			loadFactor = &v
		}
	}
	priority := req.Priority
	if priority <= 0 {
		priority = 50
	}
	weight := req.Weight
	if weight == 0 {
		weight = 100
	}
	if weight < 1 || weight > 10000 {
		return ProviderAccount{}, errors.New("weight must be between 1 and 10000")
	}
	if req.RPMLimit < 0 || req.TPMLimit < 0 {
		return ProviderAccount{}, errors.New("rpm_limit and tpm_limit must be greater than or equal to 0")
	}
	circuitFailureThreshold := req.CircuitFailureThreshold
	if circuitFailureThreshold == 0 {
		circuitFailureThreshold = 5
	}
	if circuitFailureThreshold < 1 || circuitFailureThreshold > 100 {
		return ProviderAccount{}, errors.New("circuit_failure_threshold must be between 1 and 100")
	}
	circuitOpenSeconds := req.CircuitOpenSeconds
	if circuitOpenSeconds == 0 {
		circuitOpenSeconds = 60
	}
	if circuitOpenSeconds < 1 || circuitOpenSeconds > 86400 {
		return ProviderAccount{}, errors.New("circuit_open_seconds must be between 1 and 86400")
	}
	rateMultiplier := req.RateMultiplier
	if rateMultiplier == 0 {
		rateMultiplier = 1
	}
	if rateMultiplier < 0 {
		return ProviderAccount{}, errors.New("rate_multiplier must be greater than or equal to 0")
	}
	models := cleanStringList(req.Models)
	if len(models) == 0 {
		return ProviderAccount{}, errors.New("models must not be empty")
	}
	expiresAt, err := parseOptionalDate(req.ExpiresAt)
	if err != nil {
		return ProviderAccount{}, err
	}
	tempUnschedulableRules, err := cleanTempUnschedulableRules(req.TempUnschedulableRules)
	if err != nil {
		return ProviderAccount{}, err
	}
	schedulable := defaultSchedulable
	if req.Schedulable != nil {
		schedulable = *req.Schedulable
	}
	return ProviderAccount{
		ProviderID:              providerID,
		Name:                    name,
		Platform:                platform,
		AuthType:                authType,
		Status:                  status,
		Schedulable:             schedulable,
		Priority:                priority,
		Weight:                  weight,
		Concurrency:             concurrency,
		RPMLimit:                req.RPMLimit,
		TPMLimit:                req.TPMLimit,
		LoadFactor:              loadFactor,
		RateMultiplier:          rateMultiplier,
		Models:                  models,
		GroupIDs:                cleanStringList(req.GroupIDs),
		SecretConfigured:        strings.TrimSpace(req.Secret) != "",
		SecretHint:              maskSecret(req.Secret),
		ExpiresAt:               expiresAt,
		CircuitState:            CircuitStateClosed,
		CircuitFailureThreshold: circuitFailureThreshold,
		CircuitOpenSeconds:      circuitOpenSeconds,
		TempUnschedulableRules:  tempUnschedulableRules,
		CreatedAt:               now,
		UpdatedAt:               now,
	}, nil
}

// cleanTempUnschedulableRules validates and normalizes admin-configured
// temporary-unschedulability rules. Every rule must have a plausible HTTP
// status code, at least one non-empty keyword, and a positive duration;
// malformed rules are rejected outright rather than silently dropped so
// admins get immediate feedback instead of a rule that quietly never fires.
func cleanTempUnschedulableRules(rules []ProviderAccountTempUnschedulableRule) ([]ProviderAccountTempUnschedulableRule, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	out := make([]ProviderAccountTempUnschedulableRule, 0, len(rules))
	for _, rule := range rules {
		if rule.StatusCode < 100 || rule.StatusCode > 599 {
			return nil, errors.New("temp_unschedulable_rules status_code must be a valid HTTP status code")
		}
		if rule.DurationMinutes <= 0 {
			return nil, errors.New("temp_unschedulable_rules duration_minutes must be greater than 0")
		}
		keywords := cleanStringList(rule.Keywords)
		if len(keywords) == 0 {
			return nil, errors.New("temp_unschedulable_rules keywords must not be empty")
		}
		out = append(out, ProviderAccountTempUnschedulableRule{
			StatusCode:      rule.StatusCode,
			Keywords:        keywords,
			DurationMinutes: rule.DurationMinutes,
		})
	}
	return out, nil
}

func cleanStringList(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func mergeStringLists(left []string, right []string) []string {
	merged := make([]string, 0, len(left)+len(right))
	merged = append(merged, left...)
	merged = append(merged, right...)
	return cleanStringList(merged)
}

func sameStringList(left []string, right []string) bool {
	left = cleanStringList(left)
	right = cleanStringList(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func usageSummaries(values map[string]*usageAccumulator) []UsageModelSummary {
	out := make([]UsageModelSummary, 0, len(values))
	for _, value := range values {
		avgLatency := int64(0)
		if value.requests > 0 {
			avgLatency = value.latencyTotal / int64(value.requests)
		}
		out = append(out, UsageModelSummary{
			Model:             value.model,
			Requests:          value.requests,
			Errors:            value.errors,
			Tokens:            value.tokens,
			OutputImages:      value.outputImages,
			VideoMilliseconds: value.videoMilliseconds,
			AudioMilliseconds: value.audioMilliseconds,
			CostCents:         value.costCents,
			AvgLatency:        avgLatency,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Requests == out[j].Requests {
			return out[i].Model < out[j].Model
		}
		return out[i].Requests > out[j].Requests
	})
	return out
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func monthStart(now time.Time) time.Time {
	utc := now.UTC()
	return time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func parseOptionalDate(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return &parsed, nil
	}
	day, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, errors.New("expires_at must be RFC3339 or YYYY-MM-DD")
	}
	return &day, nil
}

func hashAPIKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func fingerprint(hash string) string {
	return prefix(hash, 12)
}

func prefix(value string, length int) string {
	if len(value) <= length {
		return value
	}
	return value[:length]
}

func randomID(bytesLen int) string {
	raw := make([]byte, bytesLen)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}

func randomToken(bytesLen int) string {
	raw := make([]byte, bytesLen)
	if _, err := rand.Read(raw); err != nil {
		return randomID(bytesLen)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func maskSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	if len(secret) <= 8 {
		return "****"
	}
	return secret[:4] + "..." + secret[len(secret)-4:]
}

func encryptSecret(secretKey string, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	key, err := cryptoutil.DeriveKey(secretKey, "asterrouter:controlplane:secret-encryption:v2")
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(value), nil)
	return "v2:" + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func decryptSecret(secretKey string, ciphertext string) (string, error) {
	ciphertext = strings.TrimSpace(ciphertext)
	if ciphertext == "" {
		return "", nil
	}
	var key []byte
	if strings.HasPrefix(ciphertext, "v2:") {
		ciphertext = strings.TrimPrefix(ciphertext, "v2:")
		derivedKey, err := cryptoutil.DeriveKey(secretKey, "asterrouter:controlplane:secret-encryption:v2")
		if err != nil {
			return "", err
		}
		key = derivedKey
	} else {
		key = cryptoutil.LegacySHA256Key(secretKey)
	}
	raw, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("invalid provider secret ciphertext")
	}
	nonce := raw[:gcm.NonceSize()]
	body := raw[gcm.NonceSize():]
	opened, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return "", err
	}
	return string(opened), nil
}

func validHTTPURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
