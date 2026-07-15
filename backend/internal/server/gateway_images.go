package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/gin-gonic/gin"
)

const directImageInlineLimit = 64 << 20

var (
	errDirectImageAdapterUnavailable  = errors.New("no direct image provider adapter is available")
	errDirectImageCapacityUnavailable = errors.New("direct image provider capacity is unavailable")
	errDirectImageProviderUnknown     = errors.New("direct image provider submission state is unknown")
	errDirectImageProviderFailed      = errors.New("direct image provider did not complete successfully")
	errDirectImageProviderInvalid     = errors.New("direct image provider returned an invalid accepted response")
)

type directImageResponse struct {
	Created     int64                     `json:"created"`
	OperationID string                    `json:"operation_id"`
	Data        []directImageResponseItem `json:"data"`
}

type directImageResponseItem struct {
	Index      int    `json:"index"`
	B64JSON    string `json:"b64_json,omitempty"`
	ArtifactID string `json:"artifact_id"`
	URL        string `json:"url,omitempty"`
	MediaType  string `json:"media_type,omitempty"`
	Status     string `json:"status"`
}

type directImageExecution struct {
	Provider controlplane.GatewayProvider
	Attempt  controlplane.AIAttempt
	Result   controlplane.ProviderDispatchResult
	Release  func()
	Attempts []gatewayRouteAttempt
}

func registerGatewayImageRoutes(r *gin.Engine, control *controlplane.Service, durableJobs DurableAIJobAdmission, adapter controlplane.DirectAIProviderAdapter) {
	r.POST("/v1/images/generations", func(c *gin.Context) {
		startedAt := time.Now()
		if control == nil {
			openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
			return
		}
		request, err := parseCanonicalImageGenerationRequest(c)
		if err != nil {
			if errors.Is(err, errGatewayRequestTooLarge) {
				openAIError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body exceeds 16 MiB limit")
				return
			}
			writeGatewayError(c, err)
			return
		}
		credential, err := gatewaycore.ExtractCredential(c.Request, gatewaycore.ProtocolOpenAIImages)
		if err != nil {
			writeGatewayError(c, controlplane.ErrGatewayUnauthorized)
			return
		}
		auth, canonicalAuth, err := control.AuthorizeCanonicalGatewayRequest(c.Request.Context(), credential, request)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		if err := control.EnforceGatewayPolicy(c.Request.Context(), auth); err != nil {
			writeGatewayError(c, err)
			return
		}
		if err := validateImageDeliveryContract(request, canonicalAuth); err != nil {
			writeGatewayError(c, err)
			return
		}
		if request.Lane == gatewaycore.LaneDurable {
			acceptImageDurableJob(c, control, durableJobs, auth, canonicalAuth, request, startedAt)
			return
		}
		executeDirectImage(c, control, adapter, auth, canonicalAuth, request)
	})
}

func parseCanonicalImageGenerationRequest(c *gin.Context) (gatewaycore.CanonicalRequest, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, gatewayRequestBodyLimit)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return gatewaycore.CanonicalRequest{}, errGatewayRequestTooLarge
		}
		return gatewaycore.CanonicalRequest{}, err
	}
	request, err := gatewaycore.CanonicalizeOpenAIImageGeneration(raw, c.Request.Header)
	if err != nil {
		return gatewaycore.CanonicalRequest{}, err
	}
	request.SourceIP = gatewaySourceIP(c.Request)
	return request, nil
}

func validateImageDeliveryContract(request gatewaycore.CanonicalRequest, auth gatewaycore.CanonicalAuthContext) error {
	policy := strings.TrimSpace(auth.ArtifactPolicy)
	switch request.DeliveryMode {
	case "inline", "artifact":
		if policy != controlplane.GatewayArtifactPolicyTemporary && policy != controlplane.GatewayArtifactPolicyManaged {
			return fmt.Errorf("%w: delivery mode is incompatible with the credential artifact policy", gatewaycore.ErrInvalidCanonicalRequest)
		}
	case "customer_sink":
		if policy != controlplane.GatewayArtifactPolicyCustomerSink || strings.TrimSpace(auth.ArtifactSinkID) == "" {
			return fmt.Errorf("%w: customer_sink delivery requires an approved artifact sink", gatewaycore.ErrInvalidCanonicalRequest)
		}
	default:
		return gatewaycore.ErrInvalidCanonicalRequest
	}
	return nil
}

func acceptImageDurableJob(c *gin.Context, control *controlplane.Service, durableJobs DurableAIJobAdmission, legacyAuth controlplane.GatewayAuthContext, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest, startedAt time.Time) {
	evaluation, err := evaluateDurableAIJobAdmission(c.Request.Context(), durableJobs, auth, request)
	if err != nil {
		recordDurableAIJobCapabilityRejection(control, c, legacyAuth, request, controlplane.DurableAIJobSupportEvaluation{RejectionReason: controlplane.DurableAIJobCapabilityEvaluationError}, startedAt)
		openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "image job runtime capability check failed")
		return
	}
	if !evaluation.Supported {
		recordDurableAIJobCapabilityRejection(control, c, legacyAuth, request, evaluation, startedAt)
		openAIError(c, http.StatusServiceUnavailable, "unsupported_capability", "no executable provider adapter is available for this image job")
		return
	}
	job, created, err := control.BeginDurableAIJob(c.Request.Context(), auth, request)
	if err != nil {
		writeGatewayError(c, err)
		return
	}
	c.Header("Location", "/v1/jobs/"+job.ID)
	c.Header("X-AsterRouter-Operation-ID", job.OperationID)
	status := http.StatusAccepted
	if !created {
		status = http.StatusOK
		c.Header("Idempotent-Replayed", "true")
	}
	if !aiJobPublicTerminal(job.Status) {
		c.Header("Retry-After", strconv.Itoa(controlplane.AIJobDefaultPollAfter))
	}
	c.JSON(status, newPublicAIJobResponse(job))
}

func executeDirectImage(c *gin.Context, control *controlplane.Service, adapter controlplane.DirectAIProviderAdapter, auth controlplane.GatewayAuthContext, canonicalAuth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) {
	startedAt := time.Now()
	plan, err := control.PlanCanonicalGatewayRequest(c.Request.Context(), canonicalAuth, request)
	if err != nil {
		writeGatewayError(c, err)
		return
	}
	if len(plan.Candidates) == 0 {
		writeGatewayError(c, controlplane.ErrGatewayRouteUnavailable)
		return
	}
	operation, created, err := control.BeginCanonicalOperation(c.Request.Context(), canonicalAuth, request)
	if err != nil {
		writeGatewayError(c, err)
		return
	}
	c.Set(gatewayOperationContextKey, operation.ID)
	c.Set(gatewayFingerprintContextKey, operation.RequestFingerprint)
	c.Header("X-AsterRouter-Operation-ID", operation.ID)
	if !created {
		if operation.Status != controlplane.AIOperationStatusSucceeded {
			writeGatewayError(c, controlplane.ErrGatewayIdempotencyReplay)
			return
		}
		artifacts, replayErr := control.DirectArtifactsForAuth(c.Request.Context(), canonicalAuth, operation.ID)
		if replayErr != nil || len(artifacts) == 0 {
			writeGatewayError(c, controlplane.ErrGatewayIdempotencyReplay)
			return
		}
		response, responseErr := buildDirectImageResponse(c.Request.Context(), control, canonicalAuth, request, operation, artifacts)
		if responseErr != nil {
			writeGatewayError(c, responseErr)
			return
		}
		c.Header("Idempotent-Replayed", "true")
		writeDirectImageResponse(c, request, response)
		return
	}
	completed := false
	complete := func(status, errorType string) {
		if !completed {
			_ = control.CompleteAIOperation(c.Request.Context(), operation.ID, status, errorType)
			completed = true
		}
	}
	defer func() {
		if !completed {
			complete(controlplane.AIOperationStatusFailed, "request_aborted")
		}
	}()
	if err := control.MarkAIOperationRunning(c.Request.Context(), operation.ID); err != nil {
		_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "operation_start_failed")
		complete(controlplane.AIOperationStatusFailed, "operation_transition_error")
		openAIError(c, http.StatusInternalServerError, "server_error", "failed to start image operation")
		return
	}
	credentialPermit, capacityReason, acquired, err := control.TryAcquireGatewayCredentialPermit(c.Request.Context(), canonicalAuth, estimateGatewayRequestTokens(request.Payload))
	if err != nil {
		_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "credential_capacity_error")
		complete(controlplane.AIOperationStatusFailed, "credential_capacity_error")
		openAIError(c, http.StatusInternalServerError, "server_error", "failed to reserve gateway credential capacity")
		return
	}
	if !acquired {
		_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "credential_capacity_rejected")
		complete(controlplane.AIOperationStatusFailed, capacityReason)
		writeGatewayError(c, controlplane.ErrGatewayCapacityLimited)
		return
	}
	defer credentialPermit.Release()
	affinity := controlplane.GatewayAffinityInput{
		TenantID: canonicalAuth.TenantID, PrincipalID: canonicalAuth.PrincipalID, CredentialID: canonicalAuth.CredentialID,
		Model: request.Model, Protocol: string(request.Protocol), RouteGroup: plan.RouteGroup, StickyKey: request.StickyKey,
		PolicyVersion: canonicalAuth.PolicyVersion,
	}
	cohortKey := control.GatewayEffectivePricingCohortKey(affinity)
	candidates := control.PreferGatewayCandidatesWithAffinity(c.Request.Context(), affinity,
		control.OrderGatewayCandidatesByEffectivePricing(c.Request.Context(), request.Model, string(request.Protocol), cohortKey, plan.Candidates))
	execution, err := attemptDirectImageCandidates(c.Request.Context(), control, adapter, operation, request, candidates)
	routeAttempts := marshalRouteEvidence(plan.Exclusions, execution.Attempts)
	if execution.Attempt.ID != "" {
		c.Set(gatewayAttemptContextKey, execution.Attempt.ID)
	}
	if err != nil {
		handleDirectImageExecutionError(c, control, auth, request, operation, execution.Provider, err, routeAttempts, startedAt, complete)
		return
	}
	defer execution.Release()
	artifacts, err := control.IngestDirectAIProviderOutputs(c.Request.Context(), execution.Provider, operation, execution.Attempt, request, execution.Result, adapter)
	if err != nil || countFinalArtifacts(artifacts) != request.OutputCount {
		if err == nil {
			err = controlplane.ErrProviderOutputsRequired
		}
		_ = control.DisputeBillingHold(c.Request.Context(), operation.ID, "provider_output_unavailable")
		_ = control.CompleteAIAttempt(c.Request.Context(), execution.Attempt.ID, controlplane.AIAttemptStatusFailed, "artifact_delivery_error")
		complete(controlplane.AIOperationStatusFailed, "artifact_delivery_error")
		recordImageTrace(control, c, auth, request, execution.Provider, "upstream_error", http.StatusBadGateway, "artifact_delivery_error", startedAt, "provider image output delivery failed", routeAttempts)
		writeDirectImageArtifactError(c, err)
		return
	}
	response, err := buildDirectImageResponse(c.Request.Context(), control, canonicalAuth, request, operation, artifacts)
	if err != nil {
		_ = control.DisputeBillingHold(c.Request.Context(), operation.ID, "image_response_unavailable")
		_ = control.CompleteAIAttempt(c.Request.Context(), execution.Attempt.ID, controlplane.AIAttemptStatusFailed, "response_build_error")
		complete(controlplane.AIOperationStatusFailed, "response_build_error")
		writeGatewayError(c, err)
		return
	}
	_ = control.RecordProviderAccountSuccess(c.Request.Context(), execution.Provider.AccountID)
	_ = control.TouchProviderAccountUsage(c.Request.Context(), execution.Provider.AccountID)
	_ = control.RecordGatewayCall(c.Request.Context(), auth, request.Model, "forwarded", fmt.Sprintf("Generated %d image output(s) through provider %s", len(response.Data), execution.Provider.ID))
	_ = control.CompleteAIAttempt(c.Request.Context(), execution.Attempt.ID, controlplane.AIAttemptStatusSucceeded, "")
	usageDimensions := directMediaUsageDimensions(request, len(response.Data), finalArtifactBytes(artifacts))
	if err := control.RecordDirectAIProviderUsage(c.Request.Context(), operation, execution.Attempt, execution.Result, controlplane.GatewayUsageInput{
		UsageSource: "gateway_final", Model: request.Model, UpstreamModel: execution.Provider.UpstreamModel,
		Protocol: string(request.Protocol), ProviderID: execution.Provider.ID, ProviderAccountID: execution.Provider.AccountID,
		Status: "forwarded", LatencyMS: time.Since(startedAt).Milliseconds(), UsageNormalizationStatus: "normalized_media_outputs",
		UsageDimensions: usageDimensions, UpstreamRequestID: execution.Result.Task.ProviderRequestID,
	}); err != nil {
		_ = control.DisputeBillingHold(c.Request.Context(), operation.ID, "usage_ledger_error")
		complete(controlplane.AIOperationStatusFailed, "usage_ledger_error")
		writeGatewayError(c, err)
		return
	}
	complete(controlplane.AIOperationStatusSucceeded, "")
	recordImageTrace(control, c, auth, request, execution.Provider, "forwarded", http.StatusOK, "", startedAt, fmt.Sprintf("%s=%d", request.Modality, len(response.Data)), routeAttempts)
	writeDirectImageResponse(c, request, response)
}

func attemptDirectImageCandidates(ctx context.Context, control *controlplane.Service, adapter controlplane.DirectAIProviderAdapter, operation controlplane.AIOperation, request gatewaycore.CanonicalRequest, candidates []controlplane.GatewayProvider) (directImageExecution, error) {
	execution := directImageExecution{Attempts: []gatewayRouteAttempt{}}
	if adapter == nil {
		return execution, errDirectImageAdapterUnavailable
	}
	adapterSupported := false
	capacityDenied := false
	for index, provider := range candidates {
		adapterID, supported, err := adapter.SelectDirectAIAdapter(ctx, provider, request, operation.ArtifactPolicy)
		if err != nil {
			return execution, err
		}
		if !supported {
			execution.Attempts = append(execution.Attempts, gatewayRouteAttempt{AccountID: provider.AccountID, ProviderID: provider.ID, RouteID: provider.RouteID, Model: provider.UpstreamModel, Outcome: "excluded", Detail: "direct_adapter_unsupported"})
			continue
		}
		adapterSupported = true
		provider.AdapterID = adapterID
		attempt, err := control.BeginAIAttempt(ctx, operation.ID, index+1, provider)
		if err != nil {
			return execution, err
		}
		permit, reason, acquired, err := control.TryAcquireProviderAccountPermitContext(ctx, provider, estimateGatewayRequestTokens(request.Payload), "provider_lease_"+attempt.ID)
		if err != nil {
			_ = control.CompleteAIAttempt(ctx, attempt.ID, controlplane.AIAttemptStatusFailed, "capacity_store_error")
			return execution, err
		}
		if !acquired {
			capacityDenied = true
			_ = control.CompleteAIAttempt(ctx, attempt.ID, controlplane.AIAttemptStatusSkipped, reason)
			execution.Attempts = append(execution.Attempts, gatewayRouteAttempt{AttemptID: attempt.ID, AccountID: provider.AccountID, ProviderID: provider.ID, RouteID: provider.RouteID, Model: provider.UpstreamModel, Outcome: "skipped", Detail: reason})
			continue
		}
		executor := directImageDispatchExecutor{adapter: adapter, provider: provider, operation: operation, attempt: attempt, request: request}
		updated, result, dispatchErr := control.ExecuteAIAttemptDispatch(ctx, attempt.ID, request.Payload, executor)
		execution.Provider, execution.Attempt, execution.Result = provider, updated, result
		switch updated.DispatchState {
		case controlplane.AIAttemptDispatchProvenNotCreated:
			permit.Release()
			_ = control.CompleteAIAttempt(ctx, attempt.ID, controlplane.AIAttemptStatusFailed, "provider_rejected")
			execution.Attempts = append(execution.Attempts, gatewayRouteAttempt{AttemptID: attempt.ID, AccountID: provider.AccountID, ProviderID: provider.ID, RouteID: provider.RouteID, Model: provider.UpstreamModel, Outcome: "failed", Detail: "proven_not_created"})
			continue
		case controlplane.AIAttemptDispatchAccepted:
			if err := control.CommitBillingHold(ctx, operation.ID, "provider_response_received"); err != nil {
				permit.Release()
				return execution, err
			}
			if dispatchErr != nil {
				_ = permit.Retain(ctx, 10*time.Minute)
				_ = control.DisputeBillingHold(ctx, operation.ID, "provider_response_ambiguous")
				return execution, errors.Join(errDirectImageProviderInvalid, dispatchErr)
			}
			status := strings.ToLower(strings.TrimSpace(result.Task.Status))
			if status == "failed" || status == "error" || status == "canceled" || status == "cancelled" {
				terminalStatus := controlplane.AIJobStatusFailed
				attemptStatus := controlplane.AIAttemptStatusFailed
				if status == "canceled" || status == "cancelled" {
					terminalStatus = controlplane.AIJobStatusCanceled
					attemptStatus = controlplane.AIAttemptStatusCanceled
				}
				resolved, billingErr := control.FinalizeDirectAIProviderTerminalBilling(ctx, operation, updated, terminalStatus, result)
				permit.Release()
				if !resolved {
					_ = control.DisputeBillingHold(ctx, operation.ID, "provider_billing_unresolved")
				} else {
					_ = control.CompleteAIAttempt(ctx, attempt.ID, attemptStatus, status)
				}
				execution.Attempts = append(execution.Attempts, gatewayRouteAttempt{AttemptID: attempt.ID, AccountID: provider.AccountID, ProviderID: provider.ID, RouteID: provider.RouteID, Model: provider.UpstreamModel, Outcome: "failed", Detail: status})
				return execution, errors.Join(errDirectImageProviderFailed, billingErr)
			}
			if status != "succeeded" && status != "completed" || len(result.Outputs) == 0 {
				permit.Release()
				_ = control.DisputeBillingHold(ctx, operation.ID, "provider_response_invalid")
				_ = control.CompleteAIAttempt(ctx, attempt.ID, controlplane.AIAttemptStatusFailed, "provider_not_terminal")
				return execution, errDirectImageProviderInvalid
			}
			execution.Attempts = append(execution.Attempts, gatewayRouteAttempt{AttemptID: attempt.ID, AccountID: provider.AccountID, ProviderID: provider.ID, RouteID: provider.RouteID, Model: provider.UpstreamModel, Outcome: "selected"})
			execution.Release = permit.Release
			return execution, dispatchErr
		default:
			_ = permit.Retain(ctx, 10*time.Minute)
			_ = control.DisputeBillingHold(ctx, operation.ID, "provider_status_unknown")
			execution.Attempts = append(execution.Attempts, gatewayRouteAttempt{AttemptID: attempt.ID, AccountID: provider.AccountID, ProviderID: provider.ID, RouteID: provider.RouteID, Model: provider.UpstreamModel, Outcome: "unknown", Detail: "provider_submission_unknown"})
			return execution, errors.Join(errDirectImageProviderUnknown, dispatchErr)
		}
	}
	if !adapterSupported {
		return execution, errDirectImageAdapterUnavailable
	}
	if capacityDenied {
		return execution, errDirectImageCapacityUnavailable
	}
	return execution, errDirectImageProviderFailed
}

type directImageDispatchExecutor struct {
	adapter   controlplane.DirectAIProviderAdapter
	provider  controlplane.GatewayProvider
	operation controlplane.AIOperation
	attempt   controlplane.AIAttempt
	request   gatewaycore.CanonicalRequest
}

func (executor directImageDispatchExecutor) DispatchProviderTask(ctx context.Context, command controlplane.ProviderDispatchCommand) (controlplane.ProviderDispatchResult, error) {
	return executor.adapter.DispatchDirectAI(ctx, executor.provider, executor.operation, executor.attempt, executor.request, command)
}

func handleDirectImageExecutionError(c *gin.Context, control *controlplane.Service, auth controlplane.GatewayAuthContext, request gatewaycore.CanonicalRequest, operation controlplane.AIOperation, provider controlplane.GatewayProvider, err error, routeAttempts string, startedAt time.Time, complete func(string, string)) {
	statusCode := http.StatusBadGateway
	errorType := "upstream_error"
	message := "image provider request failed"
	switch {
	case errors.Is(err, errDirectImageAdapterUnavailable):
		statusCode, errorType = http.StatusServiceUnavailable, "unsupported_capability"
		message = "no executable provider adapter is available for this image request"
		_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "direct_adapter_unavailable")
	case errors.Is(err, errDirectImageCapacityUnavailable):
		statusCode, errorType = http.StatusTooManyRequests, "direct_capacity_unavailable"
		message = "image provider capacity is temporarily unavailable"
		c.Header("Retry-After", "2")
		_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "provider_capacity_unavailable")
	case errors.Is(err, errDirectImageProviderUnknown):
		errorType = "provider_status_unknown"
		message = "image provider submission status is unknown; retry with the same idempotency key only"
	case errors.Is(err, errDirectImageProviderInvalid):
		errorType = "provider_response_invalid"
		message = "image provider accepted the request but did not return a valid final result"
		_ = control.DisputeBillingHold(c.Request.Context(), operation.ID, "provider_response_invalid")
	case errors.Is(err, errDirectImageProviderFailed):
		errorType = "provider_terminal_failure"
		message = "image provider reported a terminal failure"
	default:
		_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "provider_request_failed")
	}
	_ = recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
		UsageVersion: 1, UsageSource: "gateway_observation", Model: request.Model, UpstreamModel: provider.UpstreamModel,
		Protocol: string(request.Protocol), ProviderID: provider.ID, ProviderAccountID: provider.AccountID,
		Status: "upstream_error", ErrorType: errorType, LatencyMS: time.Since(startedAt).Milliseconds(),
	})
	complete(controlplane.AIOperationStatusFailed, errorType)
	recordImageTrace(control, c, auth, request, provider, "upstream_error", statusCode, errorType, startedAt, message, routeAttempts)
	openAIError(c, statusCode, errorType, message)
}

func writeDirectImageArtifactError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, controlplane.ErrArtifactStateConflict),
		errors.Is(err, controlplane.ErrArtifactTooLarge),
		errors.Is(err, controlplane.ErrArtifactIntegrity),
		errors.Is(err, controlplane.ErrArtifactUnavailable),
		errors.Is(err, controlplane.ErrArtifactStoreRequired),
		errors.Is(err, controlplane.ErrArtifactProxyRequired),
		errors.Is(err, controlplane.ErrArtifactSinkRequired):
		writeGatewayError(c, err)
	default:
		openAIError(c, http.StatusBadGateway, "artifact_delivery_error", "provider image output could not be delivered")
	}
}

func buildDirectImageResponse(ctx context.Context, control *controlplane.Service, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest, operation controlplane.AIOperation, artifacts []controlplane.Artifact) (directImageResponse, error) {
	finals := make([]controlplane.Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.Role == controlplane.ArtifactRoleFinal && (artifact.Status == controlplane.ArtifactStatusReady || artifact.Status == controlplane.ArtifactStatusDelivered) {
			finals = append(finals, artifact)
		}
	}
	sort.SliceStable(finals, func(i, j int) bool { return finals[i].ID < finals[j].ID })
	if len(finals) == 0 {
		return directImageResponse{}, controlplane.ErrProviderOutputsRequired
	}
	response := directImageResponse{Created: operation.CreatedAt.Unix(), OperationID: operation.ID, Data: make([]directImageResponseItem, 0, len(finals))}
	var inlineBytes int64
	for index, artifact := range finals {
		item := directImageResponseItem{Index: index, ArtifactID: artifact.ID, MediaType: artifact.MediaType, Status: artifact.Status}
		switch request.DeliveryMode {
		case "inline":
			_, opened, found, err := control.OpenArtifactForAuth(ctx, auth, artifact.ID, nil)
			if err != nil || !found || opened.Body == nil {
				return directImageResponse{}, errors.Join(controlplane.ErrArtifactUnavailable, err)
			}
			data, readErr := io.ReadAll(io.LimitReader(opened.Body, directImageInlineLimit-inlineBytes+1))
			closeErr := opened.Body.Close()
			inlineBytes += int64(len(data))
			if readErr != nil || closeErr != nil || inlineBytes > directImageInlineLimit {
				return directImageResponse{}, errors.Join(controlplane.ErrArtifactTooLarge, readErr, closeErr)
			}
			item.B64JSON = base64.StdEncoding.EncodeToString(data)
		case "artifact":
			item.URL = "/v1/artifacts/" + artifact.ID + "/content"
		case "customer_sink":
			if artifact.Status != controlplane.ArtifactStatusDelivered {
				return directImageResponse{}, controlplane.ErrArtifactUnavailable
			}
		}
		response.Data = append(response.Data, item)
	}
	return response, nil
}

func writeDirectImageResponse(c *gin.Context, request gatewaycore.CanonicalRequest, response directImageResponse) {
	if request.ResponseMode != "stream" {
		c.JSON(http.StatusOK, response)
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)
	eventPrefix := strings.TrimSpace(request.Modality)
	if eventPrefix == "" {
		eventPrefix = "media"
	}
	for _, item := range response.Data {
		eventPayload := gin.H{"type": eventPrefix + ".final", "operation_id": response.OperationID}
		if request.Modality == controlplane.GatewayModalityImage {
			eventPayload["image"] = item
		} else {
			eventPayload["media"] = item
		}
		payload, _ := json.Marshal(eventPayload)
		_, _ = fmt.Fprintf(c.Writer, "id: %d\nevent: %s.final\ndata: %s\n\n", item.Index+1, eventPrefix, payload)
	}
	usage := gin.H{"type": "usage.finalized", "operation_id": response.OperationID}
	switch request.Modality {
	case controlplane.GatewayModalityVideo:
		usage[controlplane.UsageDimensionOutputVideoMilliseconds] = request.VideoDurationMS
	case controlplane.GatewayModalityAudio:
		usage[controlplane.UsageDimensionOutputAudioMilliseconds] = request.AudioDurationMS
	default:
		usage[controlplane.UsageDimensionOutputImages] = len(response.Data)
	}
	usagePayload, _ := json.Marshal(usage)
	_, _ = fmt.Fprintf(c.Writer, "id: %d\nevent: usage.finalized\ndata: %s\n\n", len(response.Data)+1, usagePayload)
	_, _ = fmt.Fprint(c.Writer, "event: done\ndata: [DONE]\n\n")
	c.Writer.Flush()
}

func countFinalArtifacts(artifacts []controlplane.Artifact) int {
	count := 0
	for _, artifact := range artifacts {
		if artifact.Role == controlplane.ArtifactRoleFinal && (artifact.Status == controlplane.ArtifactStatusReady || artifact.Status == controlplane.ArtifactStatusDelivered) {
			count++
		}
	}
	return count
}

func finalArtifactBytes(artifacts []controlplane.Artifact) int64 {
	var total int64
	for _, artifact := range artifacts {
		if artifact.Role != controlplane.ArtifactRoleFinal || (artifact.Status != controlplane.ArtifactStatusReady && artifact.Status != controlplane.ArtifactStatusDelivered) || artifact.SizeBytes <= 0 {
			continue
		}
		if total > math.MaxInt64-artifact.SizeBytes {
			return math.MaxInt64
		}
		total += artifact.SizeBytes
	}
	return total
}

func recordImageTrace(control *controlplane.Service, c *gin.Context, auth controlplane.GatewayAuthContext, request gatewaycore.CanonicalRequest, provider controlplane.GatewayProvider, status string, httpStatus int, errorType string, startedAt time.Time, summary, routeAttempts string) {
	recordGatewayTrace(control, c, auth, controlplane.GatewayTraceInput{
		Model: request.Model, Stream: request.Stream, ProviderID: provider.ID, ProviderAccountID: provider.AccountID,
		GatewayModelID: provider.GatewayModelID, RouteID: provider.RouteID, RouteGroup: provider.RouteGroup,
		UpstreamModel: provider.UpstreamModel, RouteSource: provider.Source, RouteReason: provider.SelectionReason,
		Status: status, HTTPStatus: httpStatus, ErrorType: errorType, LatencyMS: time.Since(startedAt).Milliseconds(),
		RequestSummary:  fmt.Sprintf("%s.generate response_mode=%s preview_mode=%s delivery_mode=%s n=%d", request.Modality, request.ResponseMode, request.PreviewMode, request.DeliveryMode, request.OutputCount),
		ResponseSummary: summary, RouteAttempts: routeAttempts,
	})
}

func directMediaUsageDimensions(request gatewaycore.CanonicalRequest, outputCount int, outputBytes int64) controlplane.UsageDimensions {
	dimensions := make(controlplane.UsageDimensions)
	switch request.Modality {
	case controlplane.GatewayModalityVideo:
		dimensions[controlplane.UsageDimensionOutputVideoMilliseconds] = controlplane.UsageDimension{
			Quantity: request.VideoDurationMS, Unit: controlplane.UsageUnitMillisecond,
			Source: "request", Confidence: controlplane.UsageConfidenceEstimated,
		}
	case controlplane.GatewayModalityAudio:
		dimensions[controlplane.UsageDimensionOutputAudioMilliseconds] = controlplane.UsageDimension{
			Quantity: request.AudioDurationMS, Unit: controlplane.UsageUnitMillisecond,
			Source: "request", Confidence: controlplane.UsageConfidenceEstimated,
		}
	default:
		dimensions[controlplane.UsageDimensionOutputImages] = controlplane.UsageDimension{
			Quantity: int64(outputCount), Unit: controlplane.UsageUnitCount,
			Source: "core_artifact", Confidence: controlplane.UsageConfidenceObserved,
		}
	}
	if outputBytes > 0 {
		dimensions[controlplane.UsageDimensionOutputBytes] = controlplane.UsageDimension{
			Quantity: outputBytes, Unit: controlplane.UsageUnitByte,
			Source: "core_artifact", Confidence: controlplane.UsageConfidenceObserved,
		}
	}
	return dimensions
}
