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
	Exclusions      []GatewayCandidateExclusion      `json:"exclusions"`
	HasRoutes       bool                             `json:"has_routes"`
	RejectionReason string                           `json:"rejection_reason,omitempty"`
}

type GatewayCandidateExclusion struct {
	RouteID           string `json:"route_id"`
	ProviderID        string `json:"provider_id,omitempty"`
	ProviderAccountID string `json:"provider_account_id,omitempty"`
	UpstreamModel     string `json:"upstream_model,omitempty"`
	Reason            string `json:"reason"`
}

func (s *Service) AuthorizeCanonicalGatewayRequest(ctx context.Context, credential gatewaycore.CredentialEnvelope, request gatewaycore.CanonicalRequest) (GatewayAuthContext, gatewaycore.CanonicalAuthContext, error) {
	return s.authorizeCanonicalGatewayRequest(ctx, credential, request, true)
}

// RevalidateCanonicalGatewayRequest repeats credential and canonical policy
// checks for a live connection without updating API key LastUsedAt.
func (s *Service) RevalidateCanonicalGatewayRequest(ctx context.Context, credential gatewaycore.CredentialEnvelope, request gatewaycore.CanonicalRequest) (GatewayAuthContext, gatewaycore.CanonicalAuthContext, error) {
	return s.authorizeCanonicalGatewayRequest(ctx, credential, request, false)
}

func (s *Service) authorizeCanonicalGatewayRequest(ctx context.Context, credential gatewaycore.CredentialEnvelope, request gatewaycore.CanonicalRequest, recordLastUsed bool) (GatewayAuthContext, gatewaycore.CanonicalAuthContext, error) {
	if request.Protocol == "" || request.Operation == "" || request.Modality == "" || request.Lane == "" {
		return GatewayAuthContext{}, gatewaycore.CanonicalAuthContext{}, gatewaycore.ErrInvalidCanonicalRequest
	}
	if request.Protocol != gatewaycore.ProtocolOpenAIModels && strings.TrimSpace(request.Model) == "" {
		return GatewayAuthContext{}, gatewaycore.CanonicalAuthContext{}, gatewaycore.ErrInvalidCanonicalRequest
	}
	var auth GatewayAuthContext
	var err error
	auth, err = s.authenticateGatewayCredential(ctx, credential.BearerToken, credential.SignedContext, recordLastUsed)
	if err == nil && request.Model != "" && !s.gatewayModelAllowed(auth, request.Model) {
		err = ErrGatewayForbidden
	}
	if err != nil {
		return GatewayAuthContext{}, gatewaycore.CanonicalAuthContext{}, err
	}
	if !apiKeyAllowsCanonicalRequest(auth.APIKey, request) {
		return GatewayAuthContext{}, gatewaycore.CanonicalAuthContext{}, ErrGatewayPolicyForbidden
	}
	return auth, s.canonicalAuthContext(auth), nil
}

func (s *Service) PlanCanonicalGatewayRequest(ctx context.Context, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) (GatewayExecutionPlan, error) {
	if strings.TrimSpace(auth.CredentialID) == "" || strings.TrimSpace(request.Model) == "" {
		return GatewayExecutionPlan{}, ErrGatewayUnauthorized
	}
	return s.planCanonicalGatewayRequest(ctx, auth, request)
}

func (s *Service) planCanonicalGatewayRequest(ctx context.Context, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) (GatewayExecutionPlan, error) {
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
	exclusions, err := s.gatewayCandidateExclusions(ctx, resolved, candidates)
	if err != nil {
		return GatewayExecutionPlan{}, err
	}
	rejectionReason := ""
	if len(candidates) == 0 && hasRoutes {
		rejectionReason = "all_candidates_excluded"
	}
	return GatewayExecutionPlan{
		Request:         request,
		Auth:            auth,
		GatewayModelID:  resolved.GatewayModel.ID,
		RouteGroup:      resolved.RouteGroup,
		Candidates:      candidates,
		Exclusions:      exclusions,
		HasRoutes:       hasRoutes,
		RejectionReason: rejectionReason,
	}, nil
}

func (s *Service) gatewayCandidateExclusions(ctx context.Context, resolved ResolvedGatewayModel, candidates []GatewayProvider) ([]GatewayCandidateExclusion, error) {
	included := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.RouteID != "" {
			included[candidate.RouteID] = struct{}{}
		}
	}
	skipped, err := s.skippedSimulationCandidates(ctx, resolved, included, 1)
	if err != nil {
		return nil, err
	}
	exclusions := make([]GatewayCandidateExclusion, 0, len(skipped))
	for _, candidate := range skipped {
		exclusions = append(exclusions, GatewayCandidateExclusion{
			RouteID: candidate.RouteID, ProviderID: candidate.ProviderID, ProviderAccountID: candidate.ProviderAccountID,
			UpstreamModel: candidate.UpstreamModel, Reason: candidate.Reason,
		})
	}
	return exclusions, nil
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
	tenantID := strings.TrimSpace(auth.APIKey.TenantID)
	if tenantID == "" {
		tenantID = gatewayDefaultTenantID
	}
	if auth.APIKey.CustomerID != "" && auth.APIKey.TenantID == "" {
		tenantID = auth.APIKey.CustomerID
	}
	principalType := strings.TrimSpace(auth.APIKey.PrincipalType)
	if principalType == "" {
		principalType = strings.TrimSpace(auth.APIKey.KeyType)
	}
	principalID := strings.TrimSpace(auth.APIKey.PrincipalReference)
	if principalID == "" {
		principalID = strings.TrimSpace(auth.APIKey.ID)
	}
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
	keyPolicy := effectiveAPIKeyPolicy(auth.APIKey)
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
		Scopes:                   append([]string(nil), keyPolicy.scopes...),
		AllowedModels:            allowedModels,
		AllowedModalities:        append([]string(nil), keyPolicy.allowedModalities...),
		AllowedOperations:        append([]string(nil), keyPolicy.allowedOperations...),
		AllowedCIDRs:             append([]string(nil), keyPolicy.allowedCIDRs...),
		Limits: gatewaycore.CanonicalLimits{
			QPSLimit:                 auth.effectiveQPSLimit(),
			RPMLimit:                 auth.APIKey.RPMLimit,
			TPMLimit:                 auth.APIKey.TPMLimit,
			ConcurrencyLimit:         auth.APIKey.ConcurrencyLimit,
			MonthlyTokenLimit:        auth.effectiveMonthlyTokenLimit(),
			MonthlyBudgetMicros:      auth.effectiveMonthlyBudgetMicros(),
			MonthlyImageLimit:        auth.APIKey.MonthlyImageLimit,
			MonthlyVideoSecondsLimit: auth.APIKey.MonthlyVideoSecondsLimit,
			MonthlyAudioSecondsLimit: auth.APIKey.MonthlyAudioSecondsLimit,
		},
		LanePolicy:     keyPolicy.lanePolicy,
		ArtifactPolicy: keyPolicy.artifactPolicy,
		ArtifactSinkID: keyPolicy.artifactSinkID,
	}
}

func gatewayModelSupportsCanonicalRequest(model GatewayModel, request gatewaycore.CanonicalRequest) bool {
	switch request.Protocol {
	case gatewaycore.ProtocolOpenAIChat:
		return model.Modality == "chat" || model.Modality == "multimodal"
	case gatewaycore.ProtocolOpenAIResponses, gatewaycore.ProtocolAnthropicMessages, gatewaycore.ProtocolGeminiGenerate:
		return model.Modality == "chat" || model.Modality == "multimodal"
	case gatewaycore.ProtocolOpenAIImages:
		return model.Modality == GatewayModalityImage || model.Modality == "multimodal"
	case gatewaycore.ProtocolOpenAIMedia:
		return model.Modality == request.Modality || model.Modality == "multimodal"
	case gatewaycore.ProtocolOpenAIAudioTranscriptions, gatewaycore.ProtocolOpenAIAudioTranslations, gatewaycore.ProtocolOpenAIAudioSpeech:
		return model.Modality == GatewayModalityAudio || model.Modality == "multimodal"
	case gatewaycore.ProtocolRealtime:
		return request.Operation == GatewayOperationRealtimeSession && (model.Modality == GatewayModalityAudio || model.Modality == "multimodal")
	case gatewaycore.ProtocolAsterJobs:
		return model.Modality == request.Modality || model.Modality == "multimodal"
	default:
		return false
	}
}
