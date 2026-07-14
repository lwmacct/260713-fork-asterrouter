package controlplane

import (
	"context"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

type GatewayExecutionPlan struct {
	Request         gatewaycore.CanonicalRequest     `json:"request"`
	Auth            gatewaycore.CanonicalAuthContext `json:"auth"`
	GatewayModelID  string                           `json:"gateway_model_id"`
	RouteGroup      string                           `json:"route_group"`
	Candidates      []GatewayProvider                `json:"-"`
	HasRoutes       bool                             `json:"has_routes"`
	RejectionReason string                           `json:"rejection_reason,omitempty"`
}

func (s *Service) AuthorizeCanonicalGatewayRequest(ctx context.Context, credential gatewaycore.CredentialEnvelope, request gatewaycore.CanonicalRequest) (GatewayAuthContext, gatewaycore.CanonicalAuthContext, error) {
	if request.Protocol == "" || request.Operation == "" || request.Lane == "" || strings.TrimSpace(request.Model) == "" {
		return GatewayAuthContext{}, gatewaycore.CanonicalAuthContext{}, gatewaycore.ErrInvalidCanonicalRequest
	}
	if request.Lane != gatewaycore.LaneDirect {
		return GatewayAuthContext{}, gatewaycore.CanonicalAuthContext{}, ErrGatewayForbidden
	}
	auth, err := s.AuthorizeGatewayCredential(ctx, credential.BearerToken, credential.SignedContext, request.Model)
	if err != nil {
		return GatewayAuthContext{}, gatewaycore.CanonicalAuthContext{}, err
	}
	return auth, s.canonicalAuthContext(auth), nil
}

func (s *Service) PlanCanonicalGatewayRequest(ctx context.Context, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) (GatewayExecutionPlan, error) {
	if strings.TrimSpace(auth.CredentialID) == "" || strings.TrimSpace(request.Model) == "" {
		return GatewayExecutionPlan{}, ErrGatewayUnauthorized
	}
	resolved, found, err := s.ResolveGatewayModel(ctx, request.Model)
	if err != nil {
		return GatewayExecutionPlan{}, err
	}
	if !found {
		return GatewayExecutionPlan{Request: request, Auth: auth, RejectionReason: "model_not_found"}, nil
	}
	if !gatewayModelSupportsCanonicalRequest(resolved.GatewayModel, request) {
		return GatewayExecutionPlan{Request: request, Auth: auth, GatewayModelID: resolved.GatewayModel.ID, RouteGroup: resolved.RouteGroup, RejectionReason: "capability_mismatch"}, nil
	}
	candidates, hasRoutes, err := s.GatewayProviderCandidatesForModel(ctx, request.Model)
	if err != nil {
		return GatewayExecutionPlan{}, err
	}
	return GatewayExecutionPlan{
		Request:        request,
		Auth:           auth,
		GatewayModelID: resolved.GatewayModel.ID,
		RouteGroup:     resolved.RouteGroup,
		Candidates:     candidates,
		HasRoutes:      hasRoutes,
	}, nil
}

func (s *Service) canonicalAuthContext(auth GatewayAuthContext) gatewaycore.CanonicalAuthContext {
	source := gatewaycore.CredentialSourceAPIKey
	credentialID := auth.APIKey.ID
	integrationID := ""
	if auth.ExternalAuthIntegration != nil {
		integrationID = auth.ExternalAuthIntegration.ID
		switch auth.ExternalAuthIntegration.Protocol {
		case ExternalAuthIntegrationProtocolHMAC:
			source = gatewaycore.CredentialSourceHMACContext
		case ExternalAuthIntegrationProtocolJWT:
			source = gatewaycore.CredentialSourceJWTJWKS
		}
	}
	tenantID := strings.TrimSpace(auth.APIKey.CustomerID)
	principalType := strings.TrimSpace(auth.APIKey.KeyType)
	principalID := strings.TrimSpace(auth.APIKey.ID)
	if auth.APIKey.OwnerUserID != "" {
		principalID = auth.APIKey.OwnerUserID
	}
	if auth.APIKey.CustomerID != "" {
		principalID = auth.APIKey.CustomerID
	}
	if auth.PlatformTenant != nil {
		tenantID = auth.PlatformTenant.ID
	}
	if auth.GatewayPrincipal != nil {
		principalType = auth.GatewayPrincipal.PrincipalType
		principalID = auth.GatewayPrincipal.ID
	}
	policyID := ""
	policyVersion := 0
	if auth.Policy != nil {
		policyID = auth.Policy.ID
		policyVersion = governancePolicyVersion(*auth.Policy)
	}
	allowedModels := make([]string, 0, len(auth.APIKey.ModelAllowlist))
	for _, model := range auth.APIKey.ModelAllowlist {
		if model = strings.TrimSpace(model); model != "" && s.gatewayModelAllowed(auth, model) && !contains(allowedModels, model) {
			allowedModels = append(allowedModels, model)
		}
	}
	return gatewaycore.CanonicalAuthContext{
		CredentialSource:         source,
		CredentialID:             credentialID,
		CredentialFingerprint:    auth.APIKey.Fingerprint,
		IntegrationID:            integrationID,
		ProfileScope:             auth.APIKey.ProfileScope,
		TenantID:                 tenantID,
		PrincipalType:            principalType,
		PrincipalID:              principalID,
		ExternalSubjectReference: auth.ExternalSubjectReference,
		PolicyID:                 policyID,
		PolicyVersion:            policyVersion,
		AllowedModels:            allowedModels,
		Limits: gatewaycore.CanonicalLimits{
			QPSLimit:           auth.effectiveQPSLimit(),
			MonthlyTokenLimit:  auth.effectiveMonthlyTokenLimit(),
			MonthlyBudgetCents: auth.effectiveMonthlyBudgetCents(),
		},
		LanePolicy:     "direct_only",
		ArtifactPolicy: "proxy_only",
	}
}

func gatewayModelSupportsCanonicalRequest(model GatewayModel, request gatewaycore.CanonicalRequest) bool {
	if request.Protocol == gatewaycore.ProtocolOpenAIChat {
		return model.Modality == "chat" || model.Modality == "multimodal"
	}
	return false
}
