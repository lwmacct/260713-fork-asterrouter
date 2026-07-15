package controlplane

import (
	"context"
	"errors"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

const (
	DurableAIJobCapabilityRuntimeUnavailable        = "runtime_unavailable"
	DurableAIJobCapabilityNoRoutesConfigured        = "no_routes_configured"
	DurableAIJobCapabilityAllAdaptersExcluded       = "all_adapters_excluded"
	DurableAIJobCapabilityAdapterUnavailable        = "adapter_unavailable"
	DurableAIJobCapabilityAdapterUnsupported        = "adapter_capability_unsupported"
	DurableAIJobCapabilityProviderTypeUnsupported   = "adapter_provider_type_unsupported"
	DurableAIJobCapabilityModalityUnsupported       = "adapter_modality_unsupported"
	DurableAIJobCapabilityOperationUnsupported      = "adapter_operation_unsupported"
	DurableAIJobCapabilityArtifactPolicyUnsupported = "adapter_artifact_policy_unsupported"
	DurableAIJobCapabilityEvaluationError           = "capability_evaluation_error"
)

type DurableAIJobSupportEvaluation struct {
	Supported       bool                        `json:"supported"`
	HasRoutes       bool                        `json:"has_routes"`
	GatewayModelID  string                      `json:"gateway_model_id,omitempty"`
	RouteGroup      string                      `json:"route_group,omitempty"`
	RejectionReason string                      `json:"rejection_reason,omitempty"`
	Exclusions      []GatewayCandidateExclusion `json:"exclusions"`
}

// DurableAIJobAdapterSelectionExplainer is optional. Core falls back to the
// legacy selector contract and a generic reason for third-party adapters that
// have not implemented detailed capability evidence yet.
type DurableAIJobAdapterSelectionExplainer interface {
	ExplainDurableAIJobAdapterSelection(context.Context, GatewayProvider, AIJob) (adapterID string, supported bool, reason string, err error)
}

func (runtime *DurableAIJobRuntime) EvaluateDurableAIJobSupport(ctx context.Context, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) (DurableAIJobSupportEvaluation, error) {
	if runtime == nil || runtime.service == nil || runtime.adapter == nil || !runtime.isRunning() {
		return rejectedDurableAIJobSupport(DurableAIJobCapabilityRuntimeUnavailable), nil
	}
	plan, err := runtime.service.planCanonicalGatewayRequest(ctx, auth, request)
	if err != nil {
		return DurableAIJobSupportEvaluation{}, err
	}
	evaluation := DurableAIJobSupportEvaluation{
		HasRoutes: plan.HasRoutes, GatewayModelID: plan.GatewayModelID, RouteGroup: plan.RouteGroup,
		RejectionReason: plan.RejectionReason, Exclusions: append([]GatewayCandidateExclusion(nil), plan.Exclusions...),
	}
	if plan.RejectionReason != "" && len(plan.Candidates) == 0 {
		if len(evaluation.Exclusions) == 0 {
			evaluation.Exclusions = append(evaluation.Exclusions, GatewayCandidateExclusion{Reason: plan.RejectionReason})
		}
		return evaluation, nil
	}
	if len(plan.Candidates) == 0 {
		evaluation.RejectionReason = DurableAIJobCapabilityNoRoutesConfigured
		if len(evaluation.Exclusions) == 0 {
			evaluation.Exclusions = append(evaluation.Exclusions, GatewayCandidateExclusion{Reason: evaluation.RejectionReason})
		}
		return evaluation, nil
	}
	job := AIJob{
		Protocol: string(request.Protocol), Operation: request.Operation, Modality: request.Modality, Model: request.Model,
		ArtifactPolicy: artifactPolicySnapshot(auth.ArtifactPolicy), ArtifactSinkID: artifactSinkSnapshot(auth.ArtifactPolicy, auth.ArtifactSinkID),
	}
	for _, provider := range plan.Candidates {
		_, supported, reason, selectErr := selectDurableAIJobAdapterWithEvidence(ctx, runtime.adapter, provider, job)
		if selectErr != nil {
			return DurableAIJobSupportEvaluation{}, selectErr
		}
		if supported {
			evaluation.Supported = true
			evaluation.RejectionReason = ""
			return evaluation, nil
		}
		evaluation.Exclusions = append(evaluation.Exclusions, GatewayCandidateExclusion{
			RouteID: provider.RouteID, ProviderID: provider.ID, ProviderAccountID: provider.AccountID,
			UpstreamModel: provider.UpstreamModel, Reason: reason,
		})
	}
	evaluation.RejectionReason = DurableAIJobCapabilityAllAdaptersExcluded
	return evaluation, nil
}

func rejectedDurableAIJobSupport(reason string) DurableAIJobSupportEvaluation {
	reason = normalizeDurableAIJobCapabilityReason(reason)
	return DurableAIJobSupportEvaluation{
		RejectionReason: reason,
		Exclusions:      []GatewayCandidateExclusion{{Reason: reason}},
	}
}

func selectDurableAIJobAdapterWithEvidence(ctx context.Context, adapter DurableAIJobAdapter, provider GatewayProvider, job AIJob) (string, bool, string, error) {
	if adapter == nil {
		return "", false, DurableAIJobCapabilityRuntimeUnavailable, ErrDurableAIJobAdapterRequired
	}
	if explainer, ok := adapter.(DurableAIJobAdapterSelectionExplainer); ok {
		adapterID, supported, reason, err := explainer.ExplainDurableAIJobAdapterSelection(ctx, provider, job)
		return validateDurableAIJobAdapterSelection(adapterID, supported, reason, err)
	}
	selector, ok := adapter.(DurableAIJobAdapterSelector)
	if !ok {
		return "", true, "", nil
	}
	adapterID, supported, err := selector.SelectDurableAIJobAdapter(ctx, provider, job)
	return validateDurableAIJobAdapterSelection(adapterID, supported, DurableAIJobCapabilityAdapterUnsupported, err)
}

func validateDurableAIJobAdapterSelection(adapterID string, supported bool, reason string, err error) (string, bool, string, error) {
	if err != nil {
		return "", false, "", err
	}
	adapterID = strings.TrimSpace(adapterID)
	if supported {
		if adapterID == "" {
			return "", false, "", errors.New("durable ai job adapter selector returned an empty adapter id")
		}
		return adapterID, true, "", nil
	}
	return "", false, normalizeDurableAIJobCapabilityReason(reason), nil
}

func normalizeDurableAIJobCapabilityReason(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" || len(reason) > 64 || !validGatewayPolicyToken(reason) {
		return DurableAIJobCapabilityAdapterUnsupported
	}
	return reason
}
