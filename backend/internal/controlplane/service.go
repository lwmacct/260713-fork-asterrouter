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
)

const systemActor = "system"

const (
	defaultWorkspaceProjectID     = "proj_platform"
	defaultWorkspaceApplicationID = "app_internal_sandbox"
)

const (
	providerProbeTimeout   = 15 * time.Second
	providerProbeBodyLimit = 2 << 20
)

var (
	ErrGatewayUnauthorized     = errors.New("invalid gateway api key")
	ErrGatewayForbidden        = errors.New("gateway api key is not allowed to use this model")
	ErrGatewayRouteUnavailable = errors.New("no schedulable gateway route is available for this model")
	ErrGatewayRateLimited      = errors.New("gateway api key qps limit exceeded")
	ErrGatewayQuotaExceeded    = errors.New("gateway api key monthly token quota exceeded")
	ErrGatewayBudgetExceeded   = errors.New("project monthly budget exceeded")
)

type Service struct {
	repo            Repository
	gatewayPath     string
	secretKey       string
	alertDispatcher AlertDispatcher
	rateMu          sync.Mutex
	rateWindows     map[string][]time.Time
}

type AlertDispatcher interface {
	DispatchAlert(ctx context.Context, event AlertEvent) error
}

func NewService(repo Repository, gatewayPath string, secretKey ...string) *Service {
	if gatewayPath == "" {
		gatewayPath = "/v1"
	}
	key := "asterrouter-local-development-secret"
	if len(secretKey) > 0 && strings.TrimSpace(secretKey[0]) != "" {
		key = strings.TrimSpace(secretKey[0])
	}
	return &Service{repo: repo, gatewayPath: gatewayPath, secretKey: key, rateWindows: map[string][]time.Time{}}
}

func (s *Service) SetAlertDispatcher(dispatcher AlertDispatcher) {
	s.alertDispatcher = dispatcher
}

func (s *Service) EnsureSeedData(ctx context.Context) error {
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return err
	}
	projects, err := s.repo.ListProjects(ctx)
	if err != nil {
		return err
	}
	if len(providers) > 0 || len(projects) > 0 {
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
	project := Project{
		ID:                 defaultWorkspaceProjectID,
		Name:               "Workspace Default",
		Description:        "Hidden default boundary for workspace keys.",
		CostCenter:         "WORKSPACE",
		MonthlyBudgetCents: 50000,
		Status:             ProjectStatusActive,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	app := Application{
		ID:          defaultWorkspaceApplicationID,
		ProjectID:   project.ID,
		Name:        "Workspace Gateway",
		Environment: "default",
		Owner:       "workspace",
		Status:      ApplicationStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.SaveProvider(ctx, provider); err != nil {
		return err
	}
	if err := s.repo.SaveProject(ctx, project); err != nil {
		return err
	}
	if err := s.repo.SaveApplication(ctx, app); err != nil {
		return err
	}
	return s.audit(ctx, systemActor, "seed", "control_plane", "product_baseline", "Seeded product baseline provider, project, and application")
}

func (s *Service) Dashboard(ctx context.Context) (Dashboard, error) {
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	projects, err := s.repo.ListProjects(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	apps, err := s.repo.ListApplications(ctx, "")
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
	modelSet := map[string]struct{}{}
	for _, provider := range providers {
		if provider.Status == ProviderStatusActive || provider.Status == ProviderStatusNeedsSecret {
			activeProviders++
		}
		for _, model := range provider.Models {
			modelSet[model] = struct{}{}
		}
	}
	for _, key := range keys {
		if key.Status == APIKeyStatusActive {
			activeKeys++
		}
	}
	models := make([]string, 0, len(modelSet))
	for model := range modelSet {
		models = append(models, model)
	}
	sort.Strings(models)

	return Dashboard{
		ProviderCount:       len(providers),
		ActiveProviderCount: activeProviders,
		ProjectCount:        len(projects),
		ApplicationCount:    len(apps),
		APIKeyCount:         len(keys),
		ActiveAPIKeyCount:   activeKeys,
		Models:              models,
		RecentAudit:         audit,
	}, nil
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

func (s *Service) ListProjects(ctx context.Context) ([]Project, error) {
	projects, err := s.repo.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	return s.enrichProjectBudgets(ctx, projects)
}

func (s *Service) CreateProject(ctx context.Context, actor string, req ProjectRequest) (Project, error) {
	now := time.Now().UTC()
	project, err := projectFromRequest(req, now)
	if err != nil {
		return Project{}, err
	}
	if err := s.validateGovernancePolicyReference(ctx, project.PolicyID); err != nil {
		return Project{}, err
	}
	project.ID = "proj_" + randomID(10)
	if err := s.repo.SaveProject(ctx, project); err != nil {
		return Project{}, err
	}
	if err := s.audit(ctx, actor, "create", "project", project.ID, fmt.Sprintf("Created project %s", project.Name)); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (s *Service) UpdateProject(ctx context.Context, actor string, id string, req ProjectRequest) (Project, error) {
	existing, err := s.projectByID(ctx, id)
	if err != nil {
		return Project{}, err
	}
	project, err := projectFromRequest(req, existing.CreatedAt)
	if err != nil {
		return Project{}, err
	}
	project.ID = existing.ID
	project.CreatedAt = existing.CreatedAt
	project.UpdatedAt = time.Now().UTC()
	if err := s.validateGovernancePolicyReference(ctx, project.PolicyID); err != nil {
		return Project{}, err
	}
	if err := s.repo.SaveProject(ctx, project); err != nil {
		return Project{}, err
	}
	if err := s.audit(ctx, actor, "update", "project", project.ID, fmt.Sprintf("Updated project %s", project.Name)); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (s *Service) ListApplications(ctx context.Context, projectID string) ([]Application, error) {
	return s.repo.ListApplications(ctx, projectID)
}

func (s *Service) CreateApplication(ctx context.Context, actor string, req ApplicationRequest) (Application, error) {
	if err := s.projectExists(ctx, req.ProjectID); err != nil {
		return Application{}, err
	}
	now := time.Now().UTC()
	app, err := applicationFromRequest(req, now)
	if err != nil {
		return Application{}, err
	}
	app.ID = "app_" + randomID(10)
	if err := s.repo.SaveApplication(ctx, app); err != nil {
		return Application{}, err
	}
	if err := s.audit(ctx, actor, "create", "application", app.ID, fmt.Sprintf("Created application %s", app.Name)); err != nil {
		return Application{}, err
	}
	return app, nil
}

func (s *Service) UpdateApplication(ctx context.Context, actor string, id string, req ApplicationRequest) (Application, error) {
	if err := s.projectExists(ctx, req.ProjectID); err != nil {
		return Application{}, err
	}
	existing, err := s.applicationByID(ctx, id)
	if err != nil {
		return Application{}, err
	}
	app, err := applicationFromRequest(req, existing.CreatedAt)
	if err != nil {
		return Application{}, err
	}
	app.ID = existing.ID
	app.CreatedAt = existing.CreatedAt
	app.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveApplication(ctx, app); err != nil {
		return Application{}, err
	}
	if err := s.audit(ctx, actor, "update", "application", app.ID, fmt.Sprintf("Updated application %s", app.Name)); err != nil {
		return Application{}, err
	}
	return app, nil
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
	return s.repo.ListAPIKeys(ctx)
}

func (s *Service) CreateAPIKey(ctx context.Context, actor string, req APIKeyCreateRequest) (APIKeyCreateResponse, error) {
	projectID, applicationID, err := s.resolveAPIKeyBoundary(ctx, actor, req.ProjectID, req.ApplicationID)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return APIKeyCreateResponse{}, errors.New("name is required")
	}
	models := cleanStringList(req.ModelAllowlist)
	if len(models) == 0 {
		return APIKeyCreateResponse{}, errors.New("model_allowlist must not be empty")
	}
	if req.QPSLimit < 0 || req.MonthlyTokenLimit < 0 {
		return APIKeyCreateResponse{}, errors.New("limits must be greater than or equal to 0")
	}
	if err := s.validateGovernancePolicyReference(ctx, req.PolicyID); err != nil {
		return APIKeyCreateResponse{}, err
	}
	expiresAt, err := parseOptionalDate(req.ExpiresAt)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}

	rawKey := "ar_" + randomToken(32)
	hash := hashAPIKey(rawKey)
	now := time.Now().UTC()
	record := APIKeyRecord{
		ID:                "key_" + randomID(10),
		ProjectID:         projectID,
		ApplicationID:     applicationID,
		Name:              name,
		KeyHash:           hash,
		Fingerprint:       fingerprint(hash),
		Prefix:            prefix(rawKey, 10),
		Status:            APIKeyStatusActive,
		PolicyID:          strings.TrimSpace(req.PolicyID),
		ModelAllowlist:    models,
		QPSLimit:          req.QPSLimit,
		MonthlyTokenLimit: req.MonthlyTokenLimit,
		ExpiresAt:         expiresAt,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.repo.SaveAPIKey(ctx, record); err != nil {
		return APIKeyCreateResponse{}, err
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
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return APIKeyRecord{}, errors.New("name is required")
	}
	models := cleanStringList(req.ModelAllowlist)
	if len(models) == 0 {
		return APIKeyRecord{}, errors.New("model_allowlist must not be empty")
	}
	if req.QPSLimit < 0 || req.MonthlyTokenLimit < 0 {
		return APIKeyRecord{}, errors.New("limits must be greater than or equal to 0")
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
	key.Name = name
	key.PolicyID = strings.TrimSpace(req.PolicyID)
	key.ModelAllowlist = models
	key.QPSLimit = req.QPSLimit
	key.MonthlyTokenLimit = req.MonthlyTokenLimit
	key.ExpiresAt = expiresAt
	key.Status = status
	key.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveAPIKey(ctx, key); err != nil {
		return APIKeyRecord{}, err
	}
	if err := s.audit(ctx, actor, "update", "api_key", key.ID, fmt.Sprintf("Updated API key %s policy", key.Name)); err != nil {
		return APIKeyRecord{}, err
	}
	return key, nil
}

func (s *Service) resolveAPIKeyBoundary(ctx context.Context, actor string, projectID string, applicationID string) (string, string, error) {
	projectID = strings.TrimSpace(projectID)
	applicationID = strings.TrimSpace(applicationID)
	if projectID == "" && applicationID == "" {
		return s.ensureDefaultWorkspaceBoundary(ctx, actor)
	}
	if applicationID != "" {
		app, err := s.applicationByID(ctx, applicationID)
		if err != nil {
			return "", "", err
		}
		if projectID == "" {
			projectID = app.ProjectID
		}
		if projectID != app.ProjectID {
			return "", "", errors.New("application does not belong to project")
		}
	}
	if err := s.projectExists(ctx, projectID); err != nil {
		return "", "", err
	}
	if applicationID == "" {
		appID, err := s.ensureDefaultWorkspaceApplicationForProject(ctx, actor, projectID)
		if err != nil {
			return "", "", err
		}
		applicationID = appID
	}
	return projectID, applicationID, nil
}

func (s *Service) ensureDefaultWorkspaceBoundary(ctx context.Context, actor string) (string, string, error) {
	now := time.Now().UTC()
	if _, err := s.projectByID(ctx, defaultWorkspaceProjectID); err != nil {
		project := Project{
			ID:                 defaultWorkspaceProjectID,
			Name:               "Workspace Default",
			Description:        "Hidden default boundary for workspace keys.",
			CostCenter:         "WORKSPACE",
			MonthlyBudgetCents: 0,
			Status:             ProjectStatusActive,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if err := s.repo.SaveProject(ctx, project); err != nil {
			return "", "", err
		}
		_ = s.audit(ctx, actorOrSystem(actor), "seed", "project", project.ID, "Created hidden workspace default project")
	}
	if _, err := s.applicationByID(ctx, defaultWorkspaceApplicationID); err != nil {
		app := Application{
			ID:          defaultWorkspaceApplicationID,
			ProjectID:   defaultWorkspaceProjectID,
			Name:        "Workspace Gateway",
			Environment: "default",
			Owner:       "workspace",
			Status:      ApplicationStatusActive,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := s.repo.SaveApplication(ctx, app); err != nil {
			return "", "", err
		}
		_ = s.audit(ctx, actorOrSystem(actor), "seed", "application", app.ID, "Created hidden workspace default application")
	}
	return defaultWorkspaceProjectID, defaultWorkspaceApplicationID, nil
}

func (s *Service) ensureDefaultWorkspaceApplicationForProject(ctx context.Context, actor string, projectID string) (string, error) {
	if projectID == defaultWorkspaceProjectID {
		_, appID, err := s.ensureDefaultWorkspaceBoundary(ctx, actor)
		return appID, err
	}
	appID := defaultWorkspaceApplicationIDForProject(projectID)
	if _, err := s.applicationByID(ctx, appID); err == nil {
		return appID, nil
	}
	now := time.Now().UTC()
	app := Application{
		ID:          appID,
		ProjectID:   projectID,
		Name:        "Workspace Gateway",
		Environment: "default",
		Owner:       "workspace",
		Status:      ApplicationStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.SaveApplication(ctx, app); err != nil {
		return "", err
	}
	_ = s.audit(ctx, actorOrSystem(actor), "seed", "application", app.ID, "Created hidden workspace default application")
	return app.ID, nil
}

func defaultWorkspaceApplicationIDForProject(projectID string) string {
	sum := sha256.Sum256([]byte(projectID))
	return "app_workspace_" + hex.EncodeToString(sum[:])[:12]
}

func (s *Service) RotateAPIKey(ctx context.Context, actor string, id string) (APIKeyCreateResponse, error) {
	key, err := s.apiKeyByID(ctx, id)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	rawKey := "ar_" + randomToken(32)
	hash := hashAPIKey(rawKey)
	now := time.Now().UTC()
	key.KeyHash = hash
	key.Fingerprint = fingerprint(hash)
	key.Prefix = prefix(rawKey, 10)
	key.Status = APIKeyStatusActive
	key.LastUsedAt = nil
	key.UpdatedAt = now
	if err := s.repo.SaveAPIKey(ctx, key); err != nil {
		return APIKeyCreateResponse{}, err
	}
	if err := s.audit(ctx, actor, "rotate", "api_key", key.ID, fmt.Sprintf("Rotated API key %s", key.Name)); err != nil {
		return APIKeyCreateResponse{}, err
	}
	return APIKeyCreateResponse{Record: key, Key: rawKey}, nil
}

func (s *Service) DisableAPIKey(ctx context.Context, actor string, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("api key id is required")
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
	now := time.Now().UTC()
	if key.ExpiresAt != nil && now.After(*key.ExpiresAt) {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	project, err := s.projectByID(ctx, key.ProjectID)
	if err != nil {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	if project.Status != ProjectStatusActive {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	app, err := s.applicationByID(ctx, key.ApplicationID)
	if err != nil {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	if app.Status != ApplicationStatusActive {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	if err := s.repo.UpdateAPIKeyLastUsed(ctx, key.ID, now); err != nil {
		return GatewayAuthContext{}, err
	}
	key.LastUsedAt = &now
	key.UpdatedAt = now
	policy, policySource, err := s.effectiveGatewayPolicy(ctx, key, project)
	if err != nil {
		return GatewayAuthContext{}, err
	}
	return GatewayAuthContext{APIKey: key, Project: project, Application: app, Policy: policy, PolicySource: policySource}, nil
}

func (s *Service) AuthorizeGatewayModel(ctx context.Context, rawKey string, model string) (GatewayAuthContext, error) {
	auth, err := s.AuthenticateGatewayKey(ctx, rawKey)
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
	currentMonth := monthStart(time.Now().UTC())
	monthlyTokenLimit := auth.effectiveMonthlyTokenLimit()
	if monthlyTokenLimit > 0 {
		used, err := s.repo.SumUsageTokensByAPIKeySince(ctx, auth.APIKey.ID, currentMonth)
		if err != nil {
			return err
		}
		if used >= monthlyTokenLimit {
			_ = s.syncAPIKeyQuotaAlert(ctx, auth, used)
			if auth.shouldBlockOverage() {
				return ErrGatewayQuotaExceeded
			}
		}
	}
	monthlyBudgetCents := auth.effectiveMonthlyBudgetCents()
	if monthlyBudgetCents > 0 {
		used, err := s.repo.SumUsageCostCentsByProjectSince(ctx, auth.Project.ID, currentMonth)
		if err != nil {
			return err
		}
		if used >= monthlyBudgetCents {
			if auth.shouldBlockOverage() {
				return ErrGatewayBudgetExceeded
			}
		}
	}
	qpsLimit := auth.effectiveQPSLimit()
	if qpsLimit > 0 && !s.allowGatewayRequest(auth.APIKey.ID, qpsLimit, time.Now().UTC()) {
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
	auth, err := s.AuthenticateGatewayKey(ctx, rawKey)
	if err != nil {
		return nil, err
	}
	return append([]string(nil), auth.APIKey.ModelAllowlist...), nil
}

func (s *Service) RecordGatewayCall(ctx context.Context, auth GatewayAuthContext, model string, status string, summary string) error {
	if summary == "" {
		summary = fmt.Sprintf("Gateway call for model %s", model)
	}
	actor := "api_key:" + auth.APIKey.Fingerprint
	resourceID := "call_" + randomID(12)
	return s.repo.AddAuditLog(ctx, AuditLog{
		ID:           "audit_" + randomID(12),
		Actor:        actor,
		Action:       "invoke",
		ResourceType: "gateway_call",
		ResourceID:   resourceID,
		Summary:      fmt.Sprintf("%s; project=%s application=%s status=%s", summary, auth.Project.ID, auth.Application.ID, status),
		CreatedAt:    time.Now().UTC(),
	})
}

type GatewayUsageInput struct {
	Model             string
	ProviderID        string
	ProviderAccountID string
	Status            string
	ErrorType         string
	LatencyMS         int64
	InputTokens       int
	OutputTokens      int
	CostCents         int
}

type GatewayTraceInput struct {
	Model             string
	Stream            bool
	MessageCount      int
	ProviderID        string
	ProviderAccountID string
	RouteSource       string
	RouteReason       string
	Status            string
	HTTPStatus        int
	ErrorType         string
	LatencyMS         int64
	InputTokens       int
	OutputTokens      int
	RequestSummary    string
	ResponseSummary   string
}

func (s *Service) RecordGatewayUsage(ctx context.Context, auth GatewayAuthContext, in GatewayUsageInput) error {
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
	if err := s.repo.SaveUsageRecord(ctx, UsageRecord{
		ID:                "usage_" + randomID(12),
		ProjectID:         auth.Project.ID,
		ApplicationID:     auth.Application.ID,
		APIKeyID:          auth.APIKey.ID,
		APIFingerprint:    auth.APIKey.Fingerprint,
		Model:             strings.TrimSpace(in.Model),
		ProviderID:        strings.TrimSpace(in.ProviderID),
		ProviderAccountID: strings.TrimSpace(in.ProviderAccountID),
		Status:            status,
		ErrorType:         strings.TrimSpace(in.ErrorType),
		LatencyMS:         in.LatencyMS,
		InputTokens:       nonNegative(in.InputTokens),
		OutputTokens:      nonNegative(in.OutputTokens),
		CostCents:         costCents,
		CreatedAt:         time.Now().UTC(),
	}); err != nil {
		return err
	}
	if costCents > 0 {
		_ = s.refreshProjectBudgetAlert(ctx, auth.Project.ID)
	}
	if auth.APIKey.MonthlyTokenLimit > 0 && (in.InputTokens > 0 || in.OutputTokens > 0) {
		_ = s.syncAPIKeyQuotaAlertForAuth(ctx, auth)
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
	return s.repo.SaveGatewayTrace(ctx, GatewayTrace{
		ID:                "trace_" + randomID(12),
		ProjectID:         auth.Project.ID,
		ApplicationID:     auth.Application.ID,
		APIKeyID:          auth.APIKey.ID,
		APIFingerprint:    auth.APIKey.Fingerprint,
		Model:             strings.TrimSpace(in.Model),
		Stream:            in.Stream,
		MessageCount:      nonNegative(in.MessageCount),
		ProviderID:        strings.TrimSpace(in.ProviderID),
		ProviderAccountID: strings.TrimSpace(in.ProviderAccountID),
		RouteSource:       strings.TrimSpace(in.RouteSource),
		RouteReason:       strings.TrimSpace(in.RouteReason),
		PolicyID:          policyID,
		PolicyName:        policyName,
		PolicySource:      policySource,
		PolicyVersion:     policyVersion,
		PolicySnapshot:    policySnapshot,
		Status:            status,
		HTTPStatus:        nonNegative(in.HTTPStatus),
		ErrorType:         strings.TrimSpace(in.ErrorType),
		LatencyMS:         in.LatencyMS,
		InputTokens:       nonNegative(in.InputTokens),
		OutputTokens:      nonNegative(in.OutputTokens),
		RequestSummary:    strings.TrimSpace(in.RequestSummary),
		ResponseSummary:   strings.TrimSpace(in.ResponseSummary),
		CreatedAt:         time.Now().UTC(),
	})
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
		TotalRequests:  aggregate.TotalRequests,
		ErrorRequests:  aggregate.ErrorRequests,
		TotalTokens:    aggregate.TotalTokens,
		TotalCostCents: aggregate.TotalCostCents,
		AvgLatencyMS:   aggregate.AvgLatencyMS,
		ByModel:        aggregate.ByModel,
		Recent:         records,
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
	model        string
	requests     int
	errors       int
	tokens       int
	costCents    int
	latencyTotal int64
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
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	accounts, err := s.repo.ListProviderAccounts(ctx)
	if err != nil {
		return nil, err
	}
	providersByID := providerByIDMap(providers)
	now := time.Now().UTC()
	modelSet := map[string]struct{}{}
	for _, provider := range providers {
		if provider.Status == ProviderStatusDisabled {
			continue
		}
		for _, model := range provider.Models {
			modelSet[model] = struct{}{}
		}
	}
	for _, account := range accounts {
		if !accountEligibleForRouting(account, "", now) {
			continue
		}
		provider, ok := providersByID[account.ProviderID]
		if !ok || provider.Status == ProviderStatusDisabled {
			continue
		}
		for _, model := range account.Models {
			modelSet[model] = struct{}{}
		}
	}
	models := make([]string, 0, len(modelSet))
	for model := range modelSet {
		models = append(models, model)
	}
	sort.Strings(models)
	return models, nil
}

func (s *Service) GatewayProviderForModel(ctx context.Context, model string) (GatewayProvider, bool, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return GatewayProvider{}, false, nil
	}
	accountRoute, hasAccountPool, ok, err := s.gatewayProviderAccountForModel(ctx, model)
	if err != nil {
		return GatewayProvider{}, false, err
	}
	if ok {
		return accountRoute, true, nil
	}
	if hasAccountPool {
		return GatewayProvider{}, false, ErrGatewayRouteUnavailable
	}
	return s.gatewayProviderConnectionForModel(ctx, model)
}

func (s *Service) gatewayProviderAccountForModel(ctx context.Context, model string) (GatewayProvider, bool, bool, error) {
	accounts, err := s.repo.ListProviderAccounts(ctx)
	if err != nil {
		return GatewayProvider{}, false, false, err
	}
	if len(accounts) == 0 {
		return GatewayProvider{}, false, false, nil
	}
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return GatewayProvider{}, true, false, err
	}
	providersByID := providerByIDMap(providers)
	now := time.Now().UTC()
	candidates := make([]ProviderAccount, 0, len(accounts))
	for _, account := range accounts {
		if !accountEligibleForRouting(account, model, now) {
			continue
		}
		provider, ok := providersByID[account.ProviderID]
		if !ok || provider.Status == ProviderStatusDisabled || !validHTTPURL(provider.BaseURL) {
			continue
		}
		candidates = append(candidates, account)
	}
	if len(candidates) == 0 {
		return GatewayProvider{}, true, false, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.Priority != right.Priority {
			return left.Priority < right.Priority
		}
		if left.RateMultiplier != right.RateMultiplier {
			return left.RateMultiplier < right.RateMultiplier
		}
		return left.Name < right.Name
	})
	selected := candidates[0]
	provider := providersByID[selected.ProviderID]
	secret, err := decryptSecret(s.secretKey, selected.SecretCiphertext)
	if err != nil {
		return GatewayProvider{}, true, false, err
	}
	selected.LastUsedAt = &now
	selected.UpdatedAt = now
	if err := s.repo.SaveProviderAccount(ctx, selected); err != nil {
		return GatewayProvider{}, true, false, err
	}
	return GatewayProvider{
		ID:              provider.ID,
		Name:            provider.Name,
		BaseURL:         provider.BaseURL,
		APIKey:          secret,
		AccountID:       selected.ID,
		AccountName:     selected.Name,
		Source:          "provider_account",
		SelectionReason: fmt.Sprintf("selected account %s by priority=%d rate_multiplier=%.4g", selected.ID, selected.Priority, selected.RateMultiplier),
	}, true, true, nil
}

func (s *Service) gatewayProviderConnectionForModel(ctx context.Context, model string) (GatewayProvider, bool, error) {
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return GatewayProvider{}, false, err
	}
	for _, provider := range providers {
		if provider.Status != ProviderStatusActive || !provider.SecretConfigured || provider.SecretCiphertext == "" {
			continue
		}
		if !contains(provider.Models, model) {
			continue
		}
		secret, err := decryptSecret(s.secretKey, provider.SecretCiphertext)
		if err != nil {
			return GatewayProvider{}, false, err
		}
		return GatewayProvider{
			ID:              provider.ID,
			Name:            provider.Name,
			BaseURL:         provider.BaseURL,
			APIKey:          secret,
			Source:          "provider_connection",
			SelectionReason: fmt.Sprintf("selected provider %s by priority=%d", provider.ID, provider.Priority),
		}, true, nil
	}
	return GatewayProvider{}, false, nil
}

func (s *Service) Health(ctx context.Context) error {
	return s.repo.Health(ctx)
}

func (s *Service) audit(ctx context.Context, actor, action, resourceType, resourceID, summary string) error {
	if strings.TrimSpace(actor) == "" {
		actor = "local-admin"
	}
	return s.repo.AddAuditLog(ctx, AuditLog{
		ID:           "audit_" + randomID(12),
		Actor:        actor,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Summary:      summary,
		CreatedAt:    time.Now().UTC(),
	})
}

func (s *Service) enrichProjectBudgets(ctx context.Context, projects []Project) ([]Project, error) {
	month := monthStart(time.Now().UTC())
	out := append([]Project(nil), projects...)
	for i := range out {
		used, err := s.repo.SumUsageCostCentsByProjectSince(ctx, out[i].ID, month)
		if err != nil {
			return nil, err
		}
		out[i].CurrentMonthCostCents = used
		out[i].BudgetRemainingCents = out[i].MonthlyBudgetCents - used
		out[i].BudgetStatus = projectBudgetStatus(out[i].MonthlyBudgetCents, used)
		if out[i].MonthlyBudgetCents > 0 {
			out[i].BudgetUsedPercent = percent(used, out[i].MonthlyBudgetCents)
		}
		if err := s.syncProjectBudgetAlert(ctx, out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func projectBudgetStatus(budgetCents int, usedCents int) string {
	if budgetCents <= 0 {
		return "unlimited"
	}
	if usedCents >= budgetCents {
		return "exceeded"
	}
	if percent(usedCents, budgetCents) >= 80 {
		return "warning"
	}
	return "ok"
}

func (s *Service) projectExists(ctx context.Context, id string) error {
	_, err := s.projectByID(ctx, id)
	return err
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

func (s *Service) projectByID(ctx context.Context, id string) (Project, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Project{}, errors.New("project_id is required")
	}
	projects, err := s.repo.ListProjects(ctx)
	if err != nil {
		return Project{}, err
	}
	for _, project := range projects {
		if project.ID == id {
			return project, nil
		}
	}
	return Project{}, fmt.Errorf("project %q not found", id)
}

func (s *Service) applicationExists(ctx context.Context, id string) error {
	_, err := s.applicationByID(ctx, id)
	return err
}

func (s *Service) applicationByID(ctx context.Context, id string) (Application, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Application{}, errors.New("application_id is required")
	}
	apps, err := s.repo.ListApplications(ctx, "")
	if err != nil {
		return Application{}, err
	}
	for _, app := range apps {
		if app.ID == id {
			return app, nil
		}
	}
	return Application{}, fmt.Errorf("application %q not found", id)
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

func projectFromRequest(req ProjectRequest, now time.Time) (Project, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return Project{}, errors.New("name is required")
	}
	status := req.Status
	if status == "" {
		status = ProjectStatusActive
	}
	if !oneOf(status, ProjectStatusActive, ProjectStatusArchived) {
		return Project{}, errors.New("status must be active or archived")
	}
	if req.MonthlyBudgetCents < 0 {
		return Project{}, errors.New("monthly_budget_cents must be greater than or equal to 0")
	}
	return Project{
		Name:               name,
		Description:        strings.TrimSpace(req.Description),
		CostCenter:         strings.TrimSpace(req.CostCenter),
		MonthlyBudgetCents: req.MonthlyBudgetCents,
		PolicyID:           strings.TrimSpace(req.PolicyID),
		Status:             status,
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func applicationFromRequest(req ApplicationRequest, now time.Time) (Application, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return Application{}, errors.New("name is required")
	}
	environment := strings.TrimSpace(req.Environment)
	if environment == "" {
		environment = "dev"
	}
	status := req.Status
	if status == "" {
		status = ApplicationStatusActive
	}
	if !oneOf(status, ApplicationStatusActive, ApplicationStatusDisabled) {
		return Application{}, errors.New("status must be active or disabled")
	}
	return Application{
		ProjectID:   strings.TrimSpace(req.ProjectID),
		Name:        name,
		Environment: environment,
		Owner:       strings.TrimSpace(req.Owner),
		Status:      status,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func routingGroupFromRequest(req RoutingGroupRequest, now time.Time) (RoutingGroup, error) {
	name := strings.TrimSpace(req.Name)
	platform := strings.TrimSpace(req.Platform)
	if name == "" {
		return RoutingGroup{}, errors.New("name is required")
	}
	if platform == "" {
		return RoutingGroup{}, errors.New("platform is required")
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
	return RoutingGroup{
		Name:           name,
		Description:    strings.TrimSpace(req.Description),
		Platform:       platform,
		RateMultiplier: rateMultiplier,
		Status:         status,
		SortOrder:      req.SortOrder,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
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
	if !oneOf(authType, "api_key", "oauth", "session", "cookie", "service_account", "custom") {
		return ProviderAccount{}, errors.New("auth_type must be api_key, oauth, session, cookie, service_account, or custom")
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
	priority := req.Priority
	if priority <= 0 {
		priority = 50
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
	schedulable := defaultSchedulable
	if req.Schedulable != nil {
		schedulable = *req.Schedulable
	}
	return ProviderAccount{
		ProviderID:       providerID,
		Name:             name,
		Platform:         platform,
		AuthType:         authType,
		Status:           status,
		Schedulable:      schedulable,
		Priority:         priority,
		Concurrency:      concurrency,
		RateMultiplier:   rateMultiplier,
		Models:           models,
		GroupIDs:         cleanStringList(req.GroupIDs),
		SecretConfigured: strings.TrimSpace(req.Secret) != "",
		SecretHint:       maskSecret(req.Secret),
		ExpiresAt:        expiresAt,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
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
			Model:      value.model,
			Requests:   value.requests,
			Errors:     value.errors,
			Tokens:     value.tokens,
			CostCents:  value.costCents,
			AvgLatency: avgLatency,
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
	block, err := aes.NewCipher(secretKeyBytes(secretKey))
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
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func decryptSecret(secretKey string, ciphertext string) (string, error) {
	ciphertext = strings.TrimSpace(ciphertext)
	if ciphertext == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(secretKeyBytes(secretKey))
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

func secretKeyBytes(secretKey string) []byte {
	sum := sha256.Sum256([]byte(secretKey))
	return sum[:]
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
