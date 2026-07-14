package controlplane

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
