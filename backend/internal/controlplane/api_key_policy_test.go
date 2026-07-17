package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

func TestRevalidateGatewayCredentialScopeDoesNotUpdateLastUsed(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name: "long lived stream", ModelAllowlist: []string{"model-a"}, Scopes: []string{GatewayScopeJobsRead},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	credential := gatewaycore.CredentialEnvelope{BearerToken: created.Key}
	firstUse := time.Date(2026, time.July, 14, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return firstUse }
	if _, err := svc.AuthorizeGatewayCredentialScope(ctx, credential, "192.0.2.1", GatewayScopeJobsRead); err != nil {
		t.Fatalf("AuthorizeGatewayCredentialScope(): %v", err)
	}

	svc.now = func() time.Time { return firstUse.Add(time.Minute) }
	if _, err := svc.RevalidateGatewayCredentialScope(ctx, credential, "192.0.2.1", GatewayScopeJobsRead); err != nil {
		t.Fatalf("RevalidateGatewayCredentialScope(): %v", err)
	}
	found, ok, err := repo.FindAPIKeyByHash(ctx, hashAPIKey(created.Key))
	if err != nil || !ok {
		t.Fatalf("FindAPIKeyByHash(): found=%t err=%v", ok, err)
	}
	if found.LastUsedAt == nil || !found.LastUsedAt.Equal(firstUse) {
		t.Fatalf("last_used_at=%v, want %v", found.LastUsedAt, firstUse)
	}
	if err := svc.DisableAPIKey(ctx, "tester", created.Record.ID); err != nil {
		t.Fatalf("DisableAPIKey(): %v", err)
	}
	if _, err := svc.RevalidateGatewayCredentialScope(ctx, credential, "192.0.2.1", GatewayScopeJobsRead); !errors.Is(err, ErrGatewayUnauthorized) {
		t.Fatalf("disabled credential revalidation error=%v, want unauthorized", err)
	}
}

func TestRevalidateCanonicalGatewayRequestUsesCurrentCredentialAndPolicy(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name: "realtime session", ModelAllowlist: []string{"model-a"}, Scopes: []string{GatewayScopeInvoke},
		AllowedModalities: []string{GatewayModalityAudio}, AllowedOperations: []string{GatewayOperationRealtimeSession},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	credential := gatewaycore.CredentialEnvelope{BearerToken: created.Key}
	request := gatewaycore.CanonicalRequest{
		ID: "realtime-policy", Protocol: gatewaycore.ProtocolRealtime, Operation: GatewayOperationRealtimeSession,
		Modality: GatewayModalityAudio, Lane: gatewaycore.LaneDirect, Model: "model-a", SourceIP: "192.0.2.10",
	}
	firstUse := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return firstUse }
	if _, _, err := svc.AuthorizeCanonicalGatewayRequest(ctx, credential, request); err != nil {
		t.Fatalf("AuthorizeCanonicalGatewayRequest(): %v", err)
	}

	svc.now = func() time.Time { return firstUse.Add(time.Minute) }
	if _, _, err := svc.RevalidateCanonicalGatewayRequest(ctx, credential, request); err != nil {
		t.Fatalf("RevalidateCanonicalGatewayRequest(): %v", err)
	}
	found, ok, err := repo.FindAPIKeyByHash(ctx, hashAPIKey(created.Key))
	if err != nil || !ok || found.LastUsedAt == nil || !found.LastUsedAt.Equal(firstUse) {
		t.Fatalf("last_used_at=%v found=%t err=%v, want %v", found.LastUsedAt, ok, err, firstUse)
	}

	if _, err := svc.UpdateAPIKey(ctx, "tester", created.Record.ID, APIKeyUpdateRequest{
		Name: created.Record.Name, ModelAllowlist: []string{"model-a"}, Scopes: []string{GatewayScopeInvoke},
		AllowedModalities: []string{GatewayModalityAudio}, AllowedOperations: []string{GatewayOperationSpeechGeneration},
	}); err != nil {
		t.Fatalf("UpdateAPIKey(): %v", err)
	}
	if _, _, err := svc.RevalidateCanonicalGatewayRequest(ctx, credential, request); !errors.Is(err, ErrGatewayPolicyForbidden) {
		t.Fatalf("tightened policy revalidation error=%v, want policy forbidden", err)
	}
	if err := svc.DisableAPIKey(ctx, "tester", created.Record.ID); err != nil {
		t.Fatalf("DisableAPIKey(): %v", err)
	}
	if _, _, err := svc.RevalidateCanonicalGatewayRequest(ctx, credential, request); !errors.Is(err, ErrGatewayUnauthorized) {
		t.Fatalf("disabled credential revalidation error=%v, want unauthorized", err)
	}
}

func TestCreateAPIKeyNormalizesPrincipalAndExtendedPolicy(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name: "service principal", KeyType: APIKeyTypeService, ModelAllowlist: []string{"model-a"},
		Scopes: []string{GatewayScopeInvoke, GatewayScopeModelsRead}, AllowedModalities: []string{GatewayModalityText},
		AllowedOperations: []string{GatewayOperationChatCompletion}, QPSLimit: 2, RPMLimit: 30, TPMLimit: 5000,
		ConcurrencyLimit: 4, MonthlyTokenLimit: 1000, MonthlyBudgetMicros: 900,
		MonthlyImageLimit: 12, MonthlyVideoSecondsLimit: 60, MonthlyAudioSecondsLimit: 120,
		AllowedCIDRs: []string{"192.0.2.42", "2001:db8::/64"}, LanePolicy: GatewayLanePolicyDirectAndDurable,
		ArtifactPolicy: GatewayArtifactPolicyManaged,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	key := created.Record
	if key.TenantID != gatewayDefaultTenantID || key.PrincipalType != APIKeyTypeService || key.PrincipalReference != key.ID || key.RotationFamilyID == "" {
		t.Fatalf("principal = %+v", key)
	}
	if key.RPMLimit != 30 || key.TPMLimit != 5000 || key.ConcurrencyLimit != 4 || key.MonthlyBudgetMicros != 900 || key.LanePolicy != GatewayLanePolicyDirectAndDurable || key.ArtifactPolicy != GatewayArtifactPolicyManaged {
		t.Fatalf("extended policy = %+v", key)
	}
	if len(key.AllowedCIDRs) != 2 || key.AllowedCIDRs[0] != "192.0.2.42/32" || key.AllowedCIDRs[1] != "2001:db8::/64" {
		t.Fatalf("allowed CIDRs = %#v", key.AllowedCIDRs)
	}

	updated, err := svc.UpdateAPIKey(ctx, "tester", key.ID, APIKeyUpdateRequest{
		Name: "renamed", ModelAllowlist: []string{"model-a"}, QPSLimit: 3, MonthlyTokenLimit: 2000,
	})
	if err != nil {
		t.Fatalf("UpdateAPIKey(): %v", err)
	}
	if updated.RPMLimit != key.RPMLimit || updated.TPMLimit != key.TPMLimit || updated.LanePolicy != key.LanePolicy || len(updated.AllowedCIDRs) != 2 {
		t.Fatalf("legacy update cleared extended policy: %+v", updated)
	}
}

func TestCreateAPIKeyRejectsInvalidExtendedPolicy(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*APIKeyCreateRequest)
	}{
		{name: "negative limit", mutate: func(req *APIKeyCreateRequest) { req.RPMLimit = -1 }},
		{name: "invalid cidr", mutate: func(req *APIKeyCreateRequest) { req.AllowedCIDRs = []string{"not-an-address"} }},
		{name: "invalid lane", mutate: func(req *APIKeyCreateRequest) { req.LanePolicy = "automatic" }},
		{name: "invalid artifact", mutate: func(req *APIKeyCreateRequest) { req.ArtifactPolicy = "public_forever" }},
		{name: "customer sink missing id", mutate: func(req *APIKeyCreateRequest) { req.ArtifactPolicy = GatewayArtifactPolicyCustomerSink }},
		{name: "sink id with managed policy", mutate: func(req *APIKeyCreateRequest) {
			req.ArtifactPolicy = GatewayArtifactPolicyManaged
			req.ArtifactSinkID = "sink-managed"
		}},
		{name: "invalid sink id", mutate: func(req *APIKeyCreateRequest) {
			req.ArtifactPolicy = GatewayArtifactPolicyCustomerSink
			req.ArtifactSinkID = "sink/invalid"
		}},
		{name: "invalid scope", mutate: func(req *APIKeyCreateRequest) { req.Scopes = []string{"Gateway Invoke"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := APIKeyCreateRequest{Name: "invalid", ModelAllowlist: []string{"model-a"}}
			test.mutate(&req)
			if _, err := NewService(NewMemoryRepository(), "/v1").CreateAPIKey(context.Background(), "tester", req); err == nil {
				t.Fatal("CreateAPIKey() accepted invalid extended policy")
			}
		})
	}
}

func TestCreateAPIKeyFreezesCustomerArtifactSinkPolicy(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	created, err := svc.CreateAPIKey(context.Background(), "tester", APIKeyCreateRequest{
		Name: "customer sink", ModelAllowlist: []string{"image-model"}, LanePolicy: GatewayLanePolicyDurableOnly,
		ArtifactPolicy: GatewayArtifactPolicyCustomerSink, ArtifactSinkID: "sink-customer-images",
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	if created.Record.ArtifactPolicy != GatewayArtifactPolicyCustomerSink || created.Record.ArtifactSinkID != "sink-customer-images" {
		t.Fatalf("artifact sink policy=%+v", created.Record)
	}
}

func TestAuthorizeCanonicalGatewayRequestEnforcesKeyPolicy(t *testing.T) {
	tests := []struct {
		name    string
		request APIKeyCreateRequest
		invoke  gatewaycore.CanonicalRequest
	}{
		{
			name: "scope", request: APIKeyCreateRequest{Scopes: []string{GatewayScopeModelsRead}},
			invoke: canonicalPolicyTestRequest("192.0.2.10"),
		},
		{
			name: "operation", request: APIKeyCreateRequest{AllowedOperations: []string{GatewayOperationListModels}},
			invoke: canonicalPolicyTestRequest("192.0.2.10"),
		},
		{
			name: "modality", request: APIKeyCreateRequest{AllowedModalities: []string{"image"}},
			invoke: canonicalPolicyTestRequest("192.0.2.10"),
		},
		{
			name: "lane", request: APIKeyCreateRequest{LanePolicy: GatewayLanePolicyDurableOnly},
			invoke: canonicalPolicyTestRequest("192.0.2.10"),
		},
		{
			name: "network", request: APIKeyCreateRequest{AllowedCIDRs: []string{"203.0.113.0/24"}},
			invoke: canonicalPolicyTestRequest("192.0.2.10"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := NewService(NewMemoryRepository(), "/v1")
			test.request.Name = "restricted"
			test.request.ModelAllowlist = []string{"model-a"}
			created, err := svc.CreateAPIKey(context.Background(), "tester", test.request)
			if err != nil {
				t.Fatalf("CreateAPIKey(): %v", err)
			}
			_, _, err = svc.AuthorizeCanonicalGatewayRequest(context.Background(), gatewaycore.CredentialEnvelope{BearerToken: created.Key}, test.invoke)
			if !errors.Is(err, ErrGatewayPolicyForbidden) {
				t.Fatalf("error = %v, want ErrGatewayPolicyForbidden", err)
			}
		})
	}
}

func TestAuthorizeCanonicalGatewayRequestAllowsMatchingCIDR(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	created, err := svc.CreateAPIKey(context.Background(), "tester", APIKeyCreateRequest{
		Name: "network", ModelAllowlist: []string{"model-a"}, AllowedCIDRs: []string{"192.0.2.0/24"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	_, canonical, err := svc.AuthorizeCanonicalGatewayRequest(context.Background(), gatewaycore.CredentialEnvelope{BearerToken: created.Key}, canonicalPolicyTestRequest("192.0.2.10"))
	if err != nil {
		t.Fatalf("AuthorizeCanonicalGatewayRequest(): %v", err)
	}
	if canonical.TenantID != gatewayDefaultTenantID || canonical.PrincipalID != created.Record.ID || len(canonical.AllowedCIDRs) != 1 {
		t.Fatalf("canonical auth = %+v", canonical)
	}
}

func TestEnforceGatewayOngoingPolicyDoesNotConsumeQPSAdmission(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	now := time.Date(2026, time.July, 16, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	created, err := svc.CreateAPIKey(context.Background(), "tester", APIKeyCreateRequest{
		Name: "ongoing", ModelAllowlist: []string{"model-a"}, QPSLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth, err := svc.AuthenticateGatewayKey(context.Background(), created.Key)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.EnforceGatewayPolicy(context.Background(), auth); err != nil {
		t.Fatalf("initial admission: %v", err)
	}
	for index := 0; index < 3; index++ {
		if err := svc.EnforceGatewayOngoingPolicy(context.Background(), auth); err != nil {
			t.Fatalf("ongoing check %d: %v", index, err)
		}
	}
	if err := svc.EnforceGatewayPolicy(context.Background(), auth); !errors.Is(err, ErrGatewayRateLimited) {
		t.Fatalf("second admission error=%v", err)
	}
}

func canonicalPolicyTestRequest(sourceIP string) gatewaycore.CanonicalRequest {
	return gatewaycore.CanonicalRequest{
		ID: "op-policy", Protocol: gatewaycore.ProtocolOpenAIChat, Operation: GatewayOperationChatCompletion,
		Modality: GatewayModalityText, Lane: gatewaycore.LaneDirect, Model: "model-a", SourceIP: sourceIP,
	}
}
