package controlplane

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

func TestAuthorizeCanonicalGatewayRequestProducesSecretFreeContext(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name: "canonical", KeyType: APIKeyTypeService, ModelAllowlist: []string{"model-a"}, QPSLimit: 3, MonthlyTokenLimit: 100,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	request := gatewaycore.CanonicalRequest{
		ID: "op_test", Protocol: gatewaycore.ProtocolOpenAIChat, Operation: "chat_completion", Modality: "text", Lane: gatewaycore.LaneDirect, Model: "model-a",
	}
	legacy, canonical, err := svc.AuthorizeCanonicalGatewayRequest(ctx, gatewaycore.CredentialEnvelope{BearerToken: created.Key}, request)
	if err != nil {
		t.Fatalf("AuthorizeCanonicalGatewayRequest(): %v", err)
	}
	if legacy.APIKey.ID != created.Record.ID || canonical.CredentialSource != gatewaycore.CredentialSourceAPIKey || canonical.CredentialID != created.Record.ID || canonical.PrincipalType != APIKeyTypeService || canonical.Limits.QPSLimit != 3 || canonical.Limits.MonthlyTokenLimit != 100 {
		t.Fatalf("canonical auth = %+v", canonical)
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), created.Key) || strings.Contains(string(encoded), legacy.APIKey.KeyHash) {
		t.Fatalf("canonical auth serialized credential material: %s", encoded)
	}
}

func TestPlanCanonicalGatewayRequestRejectsCapabilityMismatch(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	model, err := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{ModelID: "image-model", Name: "Image", Modality: "image", Status: GatewayModelStatusActive})
	if err != nil {
		t.Fatalf("CreateGatewayModel(): %v", err)
	}
	if model.Modality != "image" {
		t.Fatalf("model = %+v", model)
	}
	plan, err := svc.PlanCanonicalGatewayRequest(ctx, gatewaycore.CanonicalAuthContext{CredentialID: "key-1"}, gatewaycore.CanonicalRequest{
		Protocol: gatewaycore.ProtocolOpenAIChat, Operation: "chat_completion", Modality: "text", Lane: gatewaycore.LaneDirect, Model: "image-model",
	})
	if err != nil {
		t.Fatalf("PlanCanonicalGatewayRequest() error = %v", err)
	}
	if plan.RejectionReason != "capability_mismatch" || len(plan.Candidates) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestPlanCanonicalGatewayRequestSupportsAsterJobsByModality(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	model, err := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{
		ModelID: "video-job-model", Name: "Video job", Modality: GatewayModalityVideo, Status: GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := gatewaycore.CanonicalAuthContext{CredentialID: "job-planner-key"}
	plan, err := svc.PlanCanonicalGatewayRequest(ctx, auth, gatewaycore.CanonicalRequest{
		Protocol: gatewaycore.ProtocolAsterJobs, Operation: GatewayOperationVideoGeneration,
		Modality: GatewayModalityVideo, Lane: gatewaycore.LaneDurable, Model: "video-job-model",
	})
	if err != nil || plan.GatewayModelID != model.ID || plan.RejectionReason != "" {
		t.Fatalf("matching plan=%+v err=%v", plan, err)
	}
	mismatch, err := svc.PlanCanonicalGatewayRequest(ctx, auth, gatewaycore.CanonicalRequest{
		Protocol: gatewaycore.ProtocolAsterJobs, Operation: GatewayOperationAudioGeneration,
		Modality: GatewayModalityAudio, Lane: gatewaycore.LaneDurable, Model: "video-job-model",
	})
	if err != nil || mismatch.RejectionReason != "capability_mismatch" || len(mismatch.Candidates) != 0 {
		t.Fatalf("mismatch plan=%+v err=%v", mismatch, err)
	}
}

func TestPlanCanonicalGatewayRequestSupportsOpenAIMediaByModality(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	model, err := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{
		ModelID: "openai-video-model", Name: "OpenAI video", Modality: GatewayModalityVideo, Status: GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.PlanCanonicalGatewayRequest(ctx, gatewaycore.CanonicalAuthContext{CredentialID: "media-key"}, gatewaycore.CanonicalRequest{
		Protocol: gatewaycore.ProtocolOpenAIMedia, Operation: GatewayOperationVideoGeneration,
		Modality: GatewayModalityVideo, Lane: gatewaycore.LaneDurable, Model: "openai-video-model",
	})
	if err != nil || plan.GatewayModelID != model.ID || plan.RejectionReason != "" {
		t.Fatalf("matching plan=%+v err=%v", plan, err)
	}
}

func TestCanonicalAuthContextKeepsExternalSubjectsCredentialScoped(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	integration := ExternalAuthIntegration{ID: "integration-1", Protocol: ExternalAuthIntegrationProtocolHMAC}
	canonical := svc.canonicalAuthContext(GatewayAuthContext{
		APIKey: APIKeyRecord{
			ID: "eai_subject_1", Fingerprint: "fingerprint-1", KeyType: APIKeyTypeService,
			ProfileScope: ProfileScopePlatform, PlatformTenantID: "tenant-1", GatewayPrincipalID: "principal-1",
			ModelAllowlist: []string{"model-a"},
		},
		ExternalAuthIntegration:  &integration,
		ExternalSubjectReference: "opaque-subject-1",
	})
	if canonical.CredentialSource != gatewaycore.CredentialSourceHMACContext || canonical.CredentialID != "eai_subject_1" || canonical.IntegrationID != integration.ID || canonical.ExternalSubjectReference != "opaque-subject-1" {
		t.Fatalf("canonical auth = %+v", canonical)
	}
}

func TestPlanCanonicalGatewayRequestRecordsCandidateExclusions(t *testing.T) {
	tests := []struct {
		name           string
		providerStatus string
		accountStatus  string
		routeStatus    string
		wantReason     string
		mutate         func(*MemoryRepository, string)
	}{
		{name: "route disabled", providerStatus: ProviderStatusActive, accountStatus: AccountStatusActive, routeStatus: ModelRouteStatusDisabled, wantReason: "route_disabled"},
		{name: "account disabled", providerStatus: ProviderStatusActive, accountStatus: AccountStatusDisabled, routeStatus: ModelRouteStatusActive, wantReason: "account_disabled"},
		{name: "provider disabled", providerStatus: ProviderStatusDisabled, accountStatus: AccountStatusActive, routeStatus: ModelRouteStatusActive, wantReason: "provider_disabled"},
		{name: "account expired", providerStatus: ProviderStatusActive, accountStatus: AccountStatusActive, routeStatus: ModelRouteStatusActive, wantReason: "account_expired", mutate: func(repo *MemoryRepository, accountID string) {
			account := repo.accounts[accountID]
			expiredAt := time.Now().UTC().Add(-time.Minute)
			account.ExpiresAt = &expiredAt
			repo.accounts[accountID] = account
		}},
		{name: "account cooling down", providerStatus: ProviderStatusActive, accountStatus: AccountStatusActive, routeStatus: ModelRouteStatusActive, wantReason: "account_cooling_down", mutate: func(repo *MemoryRepository, accountID string) {
			account := repo.accounts[accountID]
			cooldownUntil := time.Now().UTC().Add(time.Minute)
			account.CooldownUntil = &cooldownUntil
			repo.accounts[accountID] = account
		}},
		{name: "circuit open", providerStatus: ProviderStatusActive, accountStatus: AccountStatusActive, routeStatus: ModelRouteStatusActive, wantReason: "circuit_open", mutate: func(repo *MemoryRepository, accountID string) {
			account := repo.accounts[accountID]
			openedUntil := time.Now().UTC().Add(time.Minute)
			account.CircuitState = CircuitStateOpen
			account.CircuitOpenedUntil = &openedUntil
			account.CooldownUntil = nil
			repo.accounts[accountID] = account
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repo := NewMemoryRepository()
			svc := NewService(repo, "/v1", "planner-secret")
			provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
				Name: "planner provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1",
				Status: test.providerStatus, Models: []string{"upstream-model"}, APIKey: "provider-secret",
			})
			if err != nil {
				t.Fatalf("CreateProvider(): %v", err)
			}
			account, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{
				ProviderID: provider.ID, Name: "planner account", Platform: "openai_compatible", AuthType: "api_key",
				Status: test.accountStatus, Models: []string{"upstream-model"}, Secret: "account-secret",
			})
			if err != nil {
				t.Fatalf("CreateProviderAccount(): %v", err)
			}
			model, err := svc.CreateGatewayModel(ctx, "tester", GatewayModelRequest{ModelID: "public-model", Name: "Public", Modality: "chat", Status: GatewayModelStatusActive})
			if err != nil {
				t.Fatalf("CreateGatewayModel(): %v", err)
			}
			route, err := svc.CreateModelRoute(ctx, "tester", ModelRouteRequest{
				GatewayModelID: model.ID, RouteGroup: DefaultModelRouteGroup, ProviderAccountID: account.ID,
				UpstreamModel: "upstream-model", Status: test.routeStatus,
			})
			if err != nil {
				t.Fatalf("CreateModelRoute(): %v", err)
			}
			if test.mutate != nil {
				repo.mu.Lock()
				test.mutate(repo, account.ID)
				repo.mu.Unlock()
			}
			plan, err := svc.PlanCanonicalGatewayRequest(ctx, gatewaycore.CanonicalAuthContext{CredentialID: "key-planner"}, gatewaycore.CanonicalRequest{
				Protocol: gatewaycore.ProtocolOpenAIChat, Operation: GatewayOperationChatCompletion,
				Modality: GatewayModalityText, Lane: gatewaycore.LaneDirect, Model: "public-model",
			})
			if err != nil {
				t.Fatalf("PlanCanonicalGatewayRequest(): %v", err)
			}
			if !plan.HasRoutes || len(plan.Candidates) != 0 || plan.RejectionReason != "all_candidates_excluded" || len(plan.Exclusions) != 1 || plan.Exclusions[0].RouteID != route.ID || plan.Exclusions[0].Reason != test.wantReason {
				t.Fatalf("plan = %+v", plan)
			}
		})
	}
}
