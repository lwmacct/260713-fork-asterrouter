package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

const providerAdapterResponseLimit = 2 << 20

var (
	ErrProviderAdapterUnavailable = errors.New("provider adapter is unavailable")
	ErrProviderAdapterResponse    = errors.New("provider adapter returned an invalid response")
)

var (
	_ controlplane.DurableAIJobAdapter                     = (*Service)(nil)
	_ controlplane.DurableAIJobAdapterSelector             = (*Service)(nil)
	_ controlplane.DurableAIJobAdapterSelectionExplainer   = (*Service)(nil)
	_ controlplane.DurableAIJobAdapterCanceler             = (*Service)(nil)
	_ controlplane.DurableAIJobAdapterCancellationSelector = (*Service)(nil)
	_ controlplane.DurableAIJobOutputReader                = (*Service)(nil)
	_ controlplane.DirectAIProviderAdapter                 = (*Service)(nil)
	_ controlplane.DirectAIProviderReconciler              = (*Service)(nil)
)

func (s *Service) SelectDirectAIAdapter(ctx context.Context, provider controlplane.GatewayProvider, request gatewaycore.CanonicalRequest, artifactPolicy string) (string, bool, error) {
	if s == nil || request.PreviewMode == "required" {
		return "", false, nil
	}
	plugins, err := s.repo.ListPlugins(ctx)
	if err != nil {
		return "", false, err
	}
	job := directAIJobSnapshot(controlplane.AIOperation{ArtifactPolicy: artifactPolicy}, request)
	if request.Protocol == gatewaycore.ProtocolOpenAIImages && request.Modality == "image" && request.Operation == "image_generation" {
		if !supportsBuiltinOpenAIImageAdapter(provider, job) {
			return "", false, nil
		}
		for _, plugin := range plugins {
			if plugin.ID == OpenAICompatibleProviderPluginID && plugin.Status == StatusEnabled {
				return plugin.ID, true, nil
			}
		}
		return "", false, nil
	}
	if request.Protocol != gatewaycore.ProtocolOpenAIMedia || (request.Modality != controlplane.GatewayModalityVideo && request.Modality != controlplane.GatewayModalityAudio) {
		return "", false, nil
	}
	for _, plugin := range plugins {
		if plugin.Status != StatusEnabled {
			continue
		}
		manifest, available := s.providerAdapterManifest(ctx, plugin.ID)
		if available && manifestSupportsProviderJob(manifest, provider, job) {
			return plugin.ID, true, nil
		}
	}
	return "", false, nil
}

func (s *Service) DispatchDirectAI(ctx context.Context, provider controlplane.GatewayProvider, operation controlplane.AIOperation, attempt controlplane.AIAttempt, request gatewaycore.CanonicalRequest, command controlplane.ProviderDispatchCommand) (controlplane.ProviderDispatchResult, error) {
	if strings.TrimSpace(attempt.ProviderAdapterID) == "" || provider.AdapterID != attempt.ProviderAdapterID || command.Intent.ProviderAdapterID != attempt.ProviderAdapterID {
		return controlplane.ProviderDispatchResult{}, ErrProviderAdapterUnavailable
	}
	if attempt.ProviderAdapterID == OpenAICompatibleProviderPluginID {
		return s.dispatchBuiltinOpenAIImage(ctx, provider, directAIJobSnapshot(operation, request), attempt, command)
	}
	requestPayload := providerAdapterDispatchRequest{
		Provider: providerAdapterProviderValue(provider), Job: providerAdapterJobValue(directAIJobSnapshot(operation, request)), Attempt: providerAdapterAttemptValue(attempt),
		Intent: command.Intent, Payload: append(json.RawMessage(nil), command.Payload...),
	}
	var result controlplane.ProviderDispatchResult
	if err := s.callProviderAdapterJSON(ctx, attempt.ProviderAdapterID, "/v1/provider-adapter/dispatch", requestPayload, &result); err != nil {
		return controlplane.ProviderDispatchResult{}, err
	}
	return result, nil
}

func (s *Service) OpenDirectAIOutput(ctx context.Context, provider controlplane.GatewayProvider, operation controlplane.AIOperation, attempt controlplane.AIAttempt, request gatewaycore.CanonicalRequest, result controlplane.ProviderDispatchResult, output controlplane.ProviderOutputDescriptor) (io.ReadCloser, error) {
	if attempt.ProviderAdapterID == OpenAICompatibleProviderPluginID {
		return s.openBuiltinOpenAIImageOutput(result.Task.ProviderTaskID, output)
	}
	job := directAIJobSnapshot(operation, request)
	return s.OpenProviderOutput(ctx, provider, job, attempt, output)
}

func (s *Service) ReconcileDirectAI(ctx context.Context, provider controlplane.GatewayProvider, operation controlplane.AIOperation, attempt controlplane.AIAttempt, intent controlplane.ProviderDispatchIntent, task controlplane.ProviderTaskReference) (controlplane.ProviderDispatchResult, error) {
	request := gatewaycore.CanonicalRequest{
		ClientRequestID: operation.ClientRequestID, Fingerprint: operation.RequestFingerprint,
		IdempotencyKey: operation.IdempotencyKey, Protocol: gatewaycore.Protocol(operation.Protocol),
		Operation: operation.Operation, Modality: operation.Modality, Lane: gatewaycore.LaneDirect, Model: operation.Model,
	}
	job := directAIJobSnapshot(operation, request)
	return s.ReconcileProviderTask(ctx, provider, job, attempt, intent, task)
}

func directAIJobSnapshot(operation controlplane.AIOperation, request gatewaycore.CanonicalRequest) controlplane.AIJob {
	return controlplane.AIJob{
		OperationID: operation.ID, ProfileScope: operation.ProfileScope, TenantID: operation.TenantID,
		CredentialID: operation.CredentialID, CredentialSource: operation.CredentialSource,
		IntegrationID: operation.IntegrationID, PrincipalType: operation.PrincipalType, PrincipalID: operation.PrincipalID,
		ExternalSubjectReference: operation.ExternalSubjectReference, Protocol: string(request.Protocol),
		Operation: request.Operation, Modality: request.Modality, Model: request.Model,
		ArtifactPolicy: operation.ArtifactPolicy, ArtifactSinkID: operation.ArtifactSinkID,
	}
}

type providerAdapterProvider struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	BaseURL       string `json:"base_url"`
	APIKey        string `json:"api_key"`
	AccountID     string `json:"account_id"`
	UpstreamModel string `json:"upstream_model"`
}

type providerAdapterJob struct {
	ID             string `json:"id"`
	OperationID    string `json:"operation_id"`
	Protocol       string `json:"protocol"`
	Operation      string `json:"operation"`
	Modality       string `json:"modality"`
	Model          string `json:"model"`
	ArtifactPolicy string `json:"artifact_policy"`
}

type providerAdapterAttempt struct {
	ID                string `json:"id"`
	AttemptNumber     int    `json:"attempt_number"`
	ProviderAdapterID string `json:"provider_adapter_id"`
}

type providerAdapterDispatchRequest struct {
	Provider providerAdapterProvider             `json:"provider"`
	Job      providerAdapterJob                  `json:"job"`
	Attempt  providerAdapterAttempt              `json:"attempt"`
	Intent   controlplane.ProviderDispatchIntent `json:"intent"`
	Payload  json.RawMessage                     `json:"payload"`
}

type providerAdapterReconcileRequest struct {
	Provider providerAdapterProvider             `json:"provider"`
	Job      providerAdapterJob                  `json:"job"`
	Attempt  providerAdapterAttempt              `json:"attempt"`
	Intent   controlplane.ProviderDispatchIntent `json:"intent"`
	Task     controlplane.ProviderTaskReference  `json:"task"`
}

type providerAdapterCancelRequest = providerAdapterReconcileRequest

type providerAdapterOutputRequest struct {
	Provider providerAdapterProvider               `json:"provider"`
	Job      providerAdapterJob                    `json:"job"`
	Attempt  providerAdapterAttempt                `json:"attempt"`
	Task     controlplane.ProviderTaskReference    `json:"task"`
	Output   controlplane.ProviderOutputDescriptor `json:"output"`
}

func (s *Service) SelectDurableAIJobAdapter(ctx context.Context, provider controlplane.GatewayProvider, job controlplane.AIJob) (string, bool, error) {
	adapterID, supported, _, err := s.ExplainDurableAIJobAdapterSelection(ctx, provider, job)
	return adapterID, supported, err
}

func (s *Service) ExplainDurableAIJobAdapterSelection(ctx context.Context, provider controlplane.GatewayProvider, job controlplane.AIJob) (string, bool, string, error) {
	if s == nil {
		return "", false, controlplane.DurableAIJobCapabilityAdapterUnavailable, nil
	}
	plugins, err := s.repo.ListPlugins(ctx)
	if err != nil {
		return "", false, "", err
	}
	if supportsBuiltinOpenAIImageAdapter(provider, job) {
		for _, plugin := range plugins {
			if plugin.ID == OpenAICompatibleProviderPluginID && plugin.Status == StatusEnabled {
				return plugin.ID, true, "", nil
			}
		}
	}
	reason := builtinProviderJobExclusionReason(provider, job)
	sort.SliceStable(plugins, func(left, right int) bool { return plugins[left].ID < plugins[right].ID })
	for _, plugin := range plugins {
		if plugin.Status != StatusEnabled {
			continue
		}
		manifest, available := s.providerAdapterManifest(ctx, plugin.ID)
		if !available {
			continue
		}
		supported, candidateReason := manifestProviderJobSupport(manifest, provider, job)
		if supported {
			return plugin.ID, true, "", nil
		}
		if providerAdapterExclusionRank(candidateReason) > providerAdapterExclusionRank(reason) {
			reason = candidateReason
		}
	}
	if reason == "" {
		reason = controlplane.DurableAIJobCapabilityAdapterUnavailable
	}
	return "", false, reason, nil
}

func (s *Service) DispatchProviderTask(ctx context.Context, provider controlplane.GatewayProvider, job controlplane.AIJob, attempt controlplane.AIAttempt, command controlplane.ProviderDispatchCommand) (controlplane.ProviderDispatchResult, error) {
	adapterID, err := selectedProviderAdapterID(provider, attempt, command.Intent)
	if err != nil {
		return controlplane.ProviderDispatchResult{}, err
	}
	if !json.Valid(command.Payload) {
		return controlplane.ProviderDispatchResult{}, ErrProviderAdapterResponse
	}
	if adapterID == OpenAICompatibleProviderPluginID {
		return s.dispatchBuiltinOpenAIImage(ctx, provider, job, attempt, command)
	}
	request := providerAdapterDispatchRequest{
		Provider: providerAdapterProviderValue(provider), Job: providerAdapterJobValue(job), Attempt: providerAdapterAttemptValue(attempt),
		Intent: command.Intent, Payload: append(json.RawMessage(nil), command.Payload...),
	}
	var result controlplane.ProviderDispatchResult
	if err := s.callProviderAdapterJSON(ctx, adapterID, "/v1/provider-adapter/dispatch", request, &result); err != nil {
		return controlplane.ProviderDispatchResult{}, err
	}
	return result, nil
}

func (s *Service) ReconcileProviderTask(ctx context.Context, provider controlplane.GatewayProvider, job controlplane.AIJob, attempt controlplane.AIAttempt, intent controlplane.ProviderDispatchIntent, task controlplane.ProviderTaskReference) (controlplane.ProviderDispatchResult, error) {
	adapterID, err := selectedProviderAdapterID(provider, attempt, intent)
	if err != nil {
		return controlplane.ProviderDispatchResult{}, err
	}
	if adapterID == OpenAICompatibleProviderPluginID {
		return s.reconcileBuiltinOpenAIImage(task)
	}
	request := providerAdapterReconcileRequest{
		Provider: providerAdapterProviderValue(provider), Job: providerAdapterJobValue(job), Attempt: providerAdapterAttemptValue(attempt),
		Intent: intent, Task: task,
	}
	var result controlplane.ProviderDispatchResult
	if err := s.callProviderAdapterJSON(ctx, adapterID, "/v1/provider-adapter/reconcile", request, &result); err != nil {
		return controlplane.ProviderDispatchResult{}, err
	}
	return result, nil
}

func (s *Service) SupportsDurableAIJobCancellation(ctx context.Context, provider controlplane.GatewayProvider, job controlplane.AIJob, attempt controlplane.AIAttempt) (bool, error) {
	adapterID := strings.TrimSpace(attempt.ProviderAdapterID)
	if adapterID == "" || adapterID == OpenAICompatibleProviderPluginID {
		return false, nil
	}
	manifest, available := s.providerAdapterManifest(ctx, adapterID)
	if !available {
		return false, nil
	}
	for index := range manifest.ProviderAdapters {
		capability := manifest.ProviderAdapters[index]
		supported, _ := manifestProviderJobSupport(sidecarManifest{ProviderAdapters: []providerAdapterManifestCapability{capability}}, provider, job)
		if supported && capability.SupportsCancellation {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) CancelProviderTask(ctx context.Context, provider controlplane.GatewayProvider, job controlplane.AIJob, attempt controlplane.AIAttempt, intent controlplane.ProviderDispatchIntent, task controlplane.ProviderTaskReference) (controlplane.ProviderDispatchResult, error) {
	adapterID, err := selectedProviderAdapterID(provider, attempt, intent)
	if err != nil {
		return controlplane.ProviderDispatchResult{}, err
	}
	if adapterID == OpenAICompatibleProviderPluginID {
		return controlplane.ProviderDispatchResult{}, controlplane.ErrDurableAIJobCancellationUnsupported
	}
	request := providerAdapterCancelRequest{
		Provider: providerAdapterProviderValue(provider), Job: providerAdapterJobValue(job), Attempt: providerAdapterAttemptValue(attempt),
		Intent: intent, Task: task,
	}
	var result controlplane.ProviderDispatchResult
	if err := s.callProviderAdapterJSON(ctx, adapterID, "/v1/provider-adapter/cancel", request, &result); err != nil {
		return controlplane.ProviderDispatchResult{}, err
	}
	return result, nil
}

func (s *Service) OpenProviderOutput(ctx context.Context, provider controlplane.GatewayProvider, job controlplane.AIJob, attempt controlplane.AIAttempt, output controlplane.ProviderOutputDescriptor) (io.ReadCloser, error) {
	adapterID, err := selectedProviderAdapterID(provider, attempt, controlplane.ProviderDispatchIntent{ProviderAdapterID: attempt.ProviderAdapterID})
	if err != nil {
		return nil, err
	}
	if adapterID == OpenAICompatibleProviderPluginID {
		return s.openBuiltinOpenAIImageOutput(attempt.ProviderTaskID, output)
	}
	request := providerAdapterOutputRequest{
		Provider: providerAdapterProviderValue(provider), Job: providerAdapterJobValue(job), Attempt: providerAdapterAttemptValue(attempt),
		Task: controlplane.ProviderTaskReference{
			ProviderTaskID: attempt.ProviderTaskID, ProviderRequestID: attempt.ProviderRequestID,
			Status: attempt.ProviderTaskStatus,
		},
		Output: output,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	response, err := s.doProviderAdapterRequest(ctx, adapterID, "/v1/provider-adapter/output", payload)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

func (s *Service) providerAdapterManifest(ctx context.Context, pluginID string) (sidecarManifest, bool) {
	installation, state, err := s.sidecarTargetState(ctx, pluginID)
	if err != nil || state != sidecarTargetReady {
		return sidecarManifest{}, false
	}
	activeDir, err := s.activePackageDir(pluginID, installation.Version)
	if err != nil {
		return sidecarManifest{}, false
	}
	manifest, err := readSidecarManifest(filepath.Join(activeDir, "plugin.json"))
	if err != nil || len(manifest.ProviderAdapters) == 0 {
		return sidecarManifest{}, false
	}
	return manifest, true
}

func manifestSupportsProviderJob(manifest sidecarManifest, provider controlplane.GatewayProvider, job controlplane.AIJob) bool {
	supported, _ := manifestProviderJobSupport(manifest, provider, job)
	return supported
}

func manifestProviderJobSupport(manifest sidecarManifest, provider controlplane.GatewayProvider, job controlplane.AIJob) (bool, string) {
	providerType := strings.ToLower(strings.TrimSpace(provider.Type))
	modality := strings.ToLower(strings.TrimSpace(job.Modality))
	operation := strings.ToLower(strings.TrimSpace(job.Operation))
	providerMatched := false
	modalityMatched := false
	operationMatched := false
	for _, capability := range manifest.ProviderAdapters {
		if !containsString(capability.ProviderTypes, providerType) {
			continue
		}
		providerMatched = true
		if !containsString(capability.Modalities, modality) {
			continue
		}
		modalityMatched = true
		if !containsString(capability.Operations, operation) {
			continue
		}
		operationMatched = true
		// Older manifests did not declare artifact policy support. Preserve
		// their behavior; a non-empty declaration is an explicit allowlist.
		if len(capability.ArtifactPolicies) > 0 && !containsString(capability.ArtifactPolicies, strings.ToLower(strings.TrimSpace(job.ArtifactPolicy))) {
			continue
		}
		return true, ""
	}
	switch {
	case operationMatched:
		return false, controlplane.DurableAIJobCapabilityArtifactPolicyUnsupported
	case modalityMatched:
		return false, controlplane.DurableAIJobCapabilityOperationUnsupported
	case providerMatched:
		return false, controlplane.DurableAIJobCapabilityModalityUnsupported
	default:
		return false, controlplane.DurableAIJobCapabilityProviderTypeUnsupported
	}
}

func builtinProviderJobExclusionReason(provider controlplane.GatewayProvider, job controlplane.AIJob) string {
	if strings.EqualFold(strings.TrimSpace(provider.Type), "openai_compatible") &&
		strings.EqualFold(strings.TrimSpace(job.Modality), controlplane.GatewayModalityImage) &&
		strings.EqualFold(strings.TrimSpace(job.Operation), controlplane.GatewayOperationImageGeneration) {
		if job.ArtifactPolicy != controlplane.GatewayArtifactPolicyTemporary && job.ArtifactPolicy != controlplane.GatewayArtifactPolicyManaged && job.ArtifactPolicy != controlplane.GatewayArtifactPolicyCustomerSink {
			return controlplane.DurableAIJobCapabilityArtifactPolicyUnsupported
		}
	}
	return controlplane.DurableAIJobCapabilityAdapterUnavailable
}

func providerAdapterExclusionRank(reason string) int {
	switch reason {
	case controlplane.DurableAIJobCapabilityArtifactPolicyUnsupported:
		return 4
	case controlplane.DurableAIJobCapabilityOperationUnsupported:
		return 3
	case controlplane.DurableAIJobCapabilityModalityUnsupported:
		return 2
	case controlplane.DurableAIJobCapabilityProviderTypeUnsupported:
		return 1
	default:
		return 0
	}
}

func selectedProviderAdapterID(provider controlplane.GatewayProvider, attempt controlplane.AIAttempt, intent controlplane.ProviderDispatchIntent) (string, error) {
	adapterID := strings.TrimSpace(attempt.ProviderAdapterID)
	if adapterID == "" || strings.TrimSpace(provider.AdapterID) != adapterID || strings.TrimSpace(intent.ProviderAdapterID) != adapterID {
		return "", ErrProviderAdapterUnavailable
	}
	return adapterID, nil
}

func providerAdapterProviderValue(provider controlplane.GatewayProvider) providerAdapterProvider {
	return providerAdapterProvider{
		ID: provider.ID, Type: provider.Type, BaseURL: provider.BaseURL, APIKey: provider.APIKey,
		AccountID: provider.AccountID, UpstreamModel: provider.UpstreamModel,
	}
}

func providerAdapterJobValue(job controlplane.AIJob) providerAdapterJob {
	return providerAdapterJob{
		ID: job.ID, OperationID: job.OperationID, Protocol: job.Protocol, Operation: job.Operation,
		Modality: job.Modality, Model: job.Model, ArtifactPolicy: job.ArtifactPolicy,
	}
}

func providerAdapterAttemptValue(attempt controlplane.AIAttempt) providerAdapterAttempt {
	return providerAdapterAttempt{ID: attempt.ID, AttemptNumber: attempt.AttemptNumber, ProviderAdapterID: attempt.ProviderAdapterID}
}

func (s *Service) callProviderAdapterJSON(ctx context.Context, adapterID, targetPath string, request any, result any) error {
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	response, err := s.doProviderAdapterRequest(ctx, adapterID, targetPath, payload)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(response.Body, providerAdapterResponseLimit+1))
	if err := decoder.Decode(result); err != nil {
		return fmt.Errorf("%w: decode response", ErrProviderAdapterResponse)
	}
	return nil
}

func (s *Service) doProviderAdapterRequest(ctx context.Context, adapterID, targetPath string, payload []byte) (*http.Response, error) {
	if err := s.ensureSidecarSupervisor(ctx, adapterID); err != nil {
		return nil, errors.Join(ErrProviderAdapterUnavailable, err)
	}
	process, err := s.waitSidecar(ctx, adapterID, 6*time.Second)
	if err != nil {
		return nil, errors.Join(ErrProviderAdapterUnavailable, err)
	}
	targetURL, err := sidecarProxyURL(process.Endpoint, targetPath, "")
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+process.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := s.providerAdapterHTTPClient.Do(request)
	if err != nil {
		s.removeSidecarProcess(adapterID, process)
		_ = process.stop(context.Background())
		s.wakeSidecarSupervisor(adapterID)
		return nil, fmt.Errorf("%w: sidecar transport failed", ErrProviderAdapterUnavailable)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_ = response.Body.Close()
		return nil, fmt.Errorf("%w: sidecar status %d", ErrProviderAdapterResponse, response.StatusCode)
	}
	return response, nil
}
