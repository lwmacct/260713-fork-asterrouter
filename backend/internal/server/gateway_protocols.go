package server

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/gin-gonic/gin"
)

// registerGatewayProtocolRoutes exposes provider-native text protocols while
// keeping admission, capacity, billing, usage and trace ownership in Core.
func registerGatewayProtocolRoutes(r *gin.Engine, control *controlplane.Service) {
	r.POST("/v1/responses", func(c *gin.Context) {
		request, err := readGatewayProtocolBody(c, func(raw []byte, header http.Header) (gatewaycore.CanonicalRequest, error) {
			return gatewaycore.CanonicalizeOpenAIResponses(raw, header)
		})
		if err != nil {
			writeGatewayProtocolParseError(c, err, "invalid responses payload")
			return
		}
		handleGatewayProtocolRequest(c, control, gatewaycore.ProtocolOpenAIResponses, request)
	})

	r.POST("/v1/messages", func(c *gin.Context) {
		request, err := readGatewayProtocolBody(c, func(raw []byte, header http.Header) (gatewaycore.CanonicalRequest, error) {
			return gatewaycore.CanonicalizeAnthropicMessages(raw, header)
		})
		if err != nil {
			writeGatewayProtocolParseError(c, err, "invalid Anthropic Messages payload")
			return
		}
		handleGatewayProtocolRequest(c, control, gatewaycore.ProtocolAnthropicMessages, request)
	})

	// Gemini puts the operation suffix after the model path segment. Gin keeps
	// the complete segment in :model, so split and validate it before parsing.
	r.POST("/v1beta/models/:model", func(c *gin.Context) {
		modelPath := strings.TrimSpace(c.Param("model"))
		stream := false
		switch {
		case strings.HasSuffix(modelPath, ":streamGenerateContent"):
			stream = true
			modelPath = strings.TrimSuffix(modelPath, ":streamGenerateContent")
		case strings.HasSuffix(modelPath, ":generateContent"):
			modelPath = strings.TrimSuffix(modelPath, ":generateContent")
		default:
			openAIError(c, http.StatusNotFound, "resource_not_found", "gemini method not found")
			return
		}
		request, err := readGatewayProtocolBody(c, func(raw []byte, header http.Header) (gatewaycore.CanonicalRequest, error) {
			return gatewaycore.CanonicalizeGeminiGenerate(raw, header, modelPath, stream)
		})
		if err != nil {
			writeGatewayProtocolParseError(c, err, "invalid Gemini generate content payload")
			return
		}
		handleGatewayProtocolRequest(c, control, gatewaycore.ProtocolGeminiGenerate, request)
	})
}

func readGatewayProtocolBody(c *gin.Context, canonicalize func([]byte, http.Header) (gatewaycore.CanonicalRequest, error)) (gatewaycore.CanonicalRequest, error) {
	if c == nil || c.Request == nil {
		return gatewaycore.CanonicalRequest{}, gatewaycore.ErrInvalidCanonicalRequest
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, gatewayRequestBodyLimit)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return gatewaycore.CanonicalRequest{}, errGatewayRequestTooLarge
		}
		return gatewaycore.CanonicalRequest{}, err
	}
	request, err := canonicalize(raw, c.Request.Header)
	if err != nil {
		return gatewaycore.CanonicalRequest{}, err
	}
	request.SourceIP = gatewaySourceIP(c.Request)
	return request, nil
}

func writeGatewayProtocolParseError(c *gin.Context, err error, message string) {
	if errors.Is(err, errGatewayRequestTooLarge) {
		openAIError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body exceeds 16 MiB limit")
		return
	}
	if errors.Is(err, gatewaycore.ErrInvalidCanonicalRequest) {
		openAIError(c, http.StatusBadRequest, "invalid_request_error", message)
		return
	}
	openAIError(c, http.StatusBadRequest, "invalid_request_error", message)
}

func handleGatewayProtocolRequest(c *gin.Context, control *controlplane.Service, protocol gatewaycore.Protocol, request gatewaycore.CanonicalRequest) {
	if control == nil {
		openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
		return
	}
	credential, err := gatewaycore.ExtractCredential(c.Request, protocol)
	if err != nil {
		writeGatewayError(c, controlplane.ErrGatewayUnauthorized)
		return
	}
	auth, canonicalAuth, err := control.AuthorizeCanonicalGatewayRequest(c.Request.Context(), credential, request)
	if err != nil {
		writeGatewayError(c, err)
		return
	}
	executeGatewayProtocolDirect(c, control, auth, canonicalAuth, request)
}

func executeGatewayProtocolDirect(c *gin.Context, control *controlplane.Service, auth controlplane.GatewayAuthContext, canonicalAuth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) {
	startedAt := time.Now()
	if err := control.EnforceGatewayPolicy(c.Request.Context(), auth); err != nil {
		errorType := gatewayPolicyErrorType(err)
		_ = control.RecordGatewayCall(c.Request.Context(), auth, request.Model, "policy_rejected", err.Error())
		_ = recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{Model: request.Model, Protocol: string(request.Protocol), Status: "error", ErrorType: errorType, LatencyMS: time.Since(startedAt).Milliseconds()})
		recordGatewayTrace(control, c, auth, gatewayTraceInput(request, controlplane.GatewayProvider{}, "error", http.StatusTooManyRequests, errorType, time.Since(startedAt).Milliseconds(), 0, 0, err.Error(), ""))
		writeGatewayError(c, err)
		return
	}
	if request.Stream && gatewayAudioProtocol(request.Protocol) && canonicalAuth.ArtifactPolicy != controlplane.GatewayArtifactPolicyProxyOnly {
		openAIError(c, http.StatusBadRequest, "unsupported_artifact_policy", "streaming audio currently requires artifact_policy=proxy_only")
		return
	}
	plan, err := control.PlanCanonicalGatewayRequest(c.Request.Context(), canonicalAuth, request)
	if err != nil {
		_ = recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{Model: request.Model, Protocol: string(request.Protocol), Status: "error", ErrorType: "provider_selection_error", LatencyMS: time.Since(startedAt).Milliseconds()})
		recordGatewayTrace(control, c, auth, gatewayTraceInput(request, controlplane.GatewayProvider{}, "error", 0, "provider_selection_error", time.Since(startedAt).Milliseconds(), 0, 0, err.Error(), ""))
		writeGatewayError(c, err)
		return
	}
	if len(plan.Candidates) == 0 {
		routeErr := controlplane.ErrGatewayRouteUnavailable
		_ = control.RecordGatewayCall(c.Request.Context(), auth, request.Model, "policy_rejected", routeErr.Error())
		_ = recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{Model: request.Model, Protocol: string(request.Protocol), Status: "error", ErrorType: "route_unavailable", LatencyMS: time.Since(startedAt).Milliseconds()})
		recordGatewayTrace(control, c, auth, gatewayTraceInput(request, controlplane.GatewayProvider{}, "error", http.StatusServiceUnavailable, "route_unavailable", time.Since(startedAt).Milliseconds(), 0, 0, routeErr.Error(), marshalRouteEvidence(plan.Exclusions, nil)))
		writeGatewayError(c, routeErr)
		return
	}
	operation, created, err := control.BeginCanonicalOperation(c.Request.Context(), canonicalAuth, request)
	if err != nil {
		recordGatewayAdmissionRejected(c, control, auth, request, startedAt, err)
		writeGatewayError(c, err)
		return
	}
	if !created {
		writeGatewayError(c, controlplane.ErrGatewayIdempotencyReplay)
		return
	}
	c.Set(gatewayOperationContextKey, operation.ID)
	c.Set(gatewayFingerprintContextKey, operation.RequestFingerprint)
	c.Header("X-AsterRouter-Operation-ID", operation.ID)
	completed := false
	complete := func(status, errorType string) error {
		if completed {
			return nil
		}
		if err := control.CompleteAIOperation(c.Request.Context(), operation.ID, status, errorType); err != nil {
			return err
		}
		completed = true
		return nil
	}
	defer func() {
		if !completed {
			_ = control.CompleteAIOperation(c.Request.Context(), operation.ID, controlplane.AIOperationStatusFailed, "request_aborted")
		}
	}()
	if request.InputArtifact != nil {
		artifact, artifactErr := persistGatewayInputArtifact(c.Request.Context(), control, operation, request)
		if artifactErr != nil {
			_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "input_artifact_failed")
			_ = complete(controlplane.AIOperationStatusFailed, "input_artifact_failed")
			writeGatewayError(c, artifactErr)
			return
		}
		c.Header("X-AsterRouter-Input-Artifact-ID", artifact.ID)
	}
	if err := control.MarkAIOperationRunning(c.Request.Context(), operation.ID); err != nil {
		_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "operation_start_failed")
		_ = complete(controlplane.AIOperationStatusFailed, "operation_transition_error")
		openAIError(c, http.StatusInternalServerError, "server_error", "failed to start gateway operation")
		return
	}
	permit, capacityReason, acquired, err := control.TryAcquireGatewayCredentialPermit(c.Request.Context(), canonicalAuth, estimateGatewayRequestTokens(request.Payload))
	if err != nil {
		_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "credential_capacity_error")
		_ = complete(controlplane.AIOperationStatusFailed, "credential_capacity_error")
		openAIError(c, http.StatusInternalServerError, "server_error", "failed to reserve gateway credential capacity")
		return
	}
	if !acquired {
		_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "credential_capacity_rejected")
		_ = recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{Model: request.Model, Protocol: string(request.Protocol), Status: "error", ErrorType: capacityReason, LatencyMS: time.Since(startedAt).Milliseconds()})
		recordGatewayTrace(control, c, auth, gatewayTraceInput(request, controlplane.GatewayProvider{}, "error", http.StatusTooManyRequests, capacityReason, time.Since(startedAt).Milliseconds(), 0, 0, "gateway credential capacity rejected the request", ""))
		_ = complete(controlplane.AIOperationStatusFailed, capacityReason)
		writeGatewayError(c, controlplane.ErrGatewayCapacityLimited)
		return
	}
	defer permit.Release()
	affinity := controlplane.GatewayAffinityInput{TenantID: canonicalAuth.TenantID, PrincipalID: canonicalAuth.PrincipalID, CredentialID: canonicalAuth.CredentialID, Model: request.Model, Protocol: string(request.Protocol), RouteGroup: plan.RouteGroup, StickyKey: request.StickyKey, PolicyVersion: canonicalAuth.PolicyVersion}
	cohortKey := control.GatewayEffectivePricingCohortKey(affinity)
	candidates := control.PreferGatewayCandidatesWithAffinity(c.Request.Context(), affinity, control.OrderGatewayCandidatesByEffectivePricing(c.Request.Context(), request.Model, string(request.Protocol), cohortKey, plan.Candidates))
	resp, provider, release, attempts, attemptErr := attemptGatewayCandidatesForCanonicalRequest(c, control, operation.ID, affinity, candidates, request)
	routeAttempts := marshalRouteEvidence(plan.Exclusions, attempts)
	if provider.AttemptID != "" {
		c.Set(gatewayAttemptContextKey, provider.AttemptID)
	}
	if resp == nil {
		if attemptErr == nil {
			attemptErr = errNoSchedulableSlot
		}
		if !gatewayAttemptsBillingUncertain(attempts) {
			_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "provider_capacity_unavailable")
		}
		_ = control.RecordGatewayCall(c.Request.Context(), auth, request.Model, "upstream_error", attemptErr.Error())
		_ = recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{UsageSource: "gateway_observation", Model: request.Model, UpstreamModel: provider.UpstreamModel, Protocol: string(request.Protocol), ProviderID: provider.ID, ProviderAccountID: provider.AccountID, Status: "upstream_error", ErrorType: "transport_error", LatencyMS: time.Since(startedAt).Milliseconds()})
		recordGatewayTrace(control, c, auth, gatewayTraceInput(request, provider, "upstream_error", 0, "transport_error", time.Since(startedAt).Milliseconds(), 0, 0, attemptErr.Error(), routeAttempts))
		_ = complete(controlplane.AIOperationStatusFailed, "transport_error")
		openAIError(c, http.StatusBadGateway, "upstream_error", attemptErr.Error())
		return
	}
	defer resp.Body.Close()
	defer release()
	status := "forwarded"
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status = "upstream_error"
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		_ = control.BindGatewayCandidateAffinity(c.Request.Context(), affinity, provider)
	}
	summary := gatewayProtocolRouteSummary(request, provider)
	if request.Stream {
		if err := control.RecordGatewayCall(c.Request.Context(), auth, request.Model, status, summary); err != nil {
			_ = control.DisputeBillingHold(c.Request.Context(), operation.ID, "provider_response_not_accounted")
			_ = control.CompleteAIAttempt(c.Request.Context(), provider.AttemptID, controlplane.AIAttemptStatusFailed, "audit_error")
			_ = complete(controlplane.AIOperationStatusFailed, "audit_error")
			openAIError(c, http.StatusInternalServerError, "server_error", err.Error())
			return
		}
		streamUsage, ttftMS, streamErr := streamUpstreamResponse(c, resp, startedAt)
		errorType := ""
		usageStatus := status
		if streamErr != nil {
			errorType = "stream_error"
			usageStatus = "upstream_error"
		}
		attemptStatus := controlplane.AIAttemptStatusSucceeded
		operationStatus := controlplane.AIOperationStatusSucceeded
		if streamErr != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			attemptStatus = controlplane.AIAttemptStatusFailed
			operationStatus = controlplane.AIOperationStatusFailed
			if errorType == "" {
				errorType = "upstream_status"
			}
		}
		usageSource := "gateway_final"
		if streamErr != nil || gatewayAttemptsBillingUncertain(attempts) || !gatewayUsageObservationFinal(streamUsage) {
			usageSource = "gateway_observation"
			_ = control.DisputeBillingHold(c.Request.Context(), operation.ID, "provider_usage_unconfirmed")
		}
		_ = control.CompleteAIAttempt(c.Request.Context(), provider.AttemptID, attemptStatus, errorType)
		responseSummary := "stream completed"
		if streamErr != nil {
			responseSummary = streamErr.Error()
		}
		usageInput := gatewayUsageInputForProtocol(request, provider, usageSource, usageStatus, errorType, time.Since(startedAt).Milliseconds(), ttftMS, streamUsage, upstreamRequestID(resp))
		if gatewayAudioProtocol(request.Protocol) {
			usageInput.UsageDimensions = gatewayAudioUsageDimensions(request, 0)
		}
		usageErr := recordGatewayUsage(control, c, auth, usageInput)
		if usageErr != nil {
			_ = complete(controlplane.AIOperationStatusFailed, "usage_ledger_error")
		} else {
			_ = complete(operationStatus, errorType)
		}
		recordGatewayTrace(control, c, auth, gatewayTraceInput(request, provider, usageStatus, resp.StatusCode, errorType, time.Since(startedAt).Milliseconds(), streamUsage.InputTokens, streamUsage.OutputTokens, responseSummary, routeAttempts))
		if streamErr != nil && !c.Writer.Written() {
			openAIError(c, http.StatusBadGateway, "upstream_error", streamErr.Error())
		}
		return
	}
	responseLimit := int64(gatewayUpstreamBodyLimit)
	if gatewayAudioProtocol(request.Protocol) {
		responseLimit = directMediaInlineLimit
	}
	contentType, upstreamBody, ttftMS, err := readUpstreamResponseLimit(resp, startedAt, responseLimit)
	if err != nil {
		_ = control.DisputeBillingHold(c.Request.Context(), operation.ID, "provider_response_read_failed")
		_ = control.CompleteAIAttempt(c.Request.Context(), provider.AttemptID, controlplane.AIAttemptStatusFailed, "response_read_error")
		_ = complete(controlplane.AIOperationStatusFailed, "response_read_error")
		_ = control.RecordGatewayCall(c.Request.Context(), auth, request.Model, "upstream_error", err.Error())
		_ = recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{UsageSource: "gateway_observation", Model: request.Model, UpstreamModel: provider.UpstreamModel, Protocol: string(request.Protocol), ProviderID: provider.ID, ProviderAccountID: provider.AccountID, Status: "upstream_error", ErrorType: "response_read_error", LatencyMS: time.Since(startedAt).Milliseconds()})
		recordGatewayTrace(control, c, auth, gatewayTraceInput(request, provider, "upstream_error", resp.StatusCode, "response_read_error", time.Since(startedAt).Milliseconds(), 0, 0, err.Error(), routeAttempts))
		openAIError(c, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	if status == "forwarded" && gatewayAudioProtocol(request.Protocol) {
		attempt, found, attemptErr := control.AIAttempt(c.Request.Context(), provider.AttemptID)
		if attemptErr != nil || !found {
			if attemptErr == nil {
				attemptErr = controlplane.ErrAIAttemptNotFound
			}
			_ = control.DisputeBillingHold(c.Request.Context(), operation.ID, "response_artifact_attempt_missing")
			_ = complete(controlplane.AIOperationStatusFailed, "response_artifact_failed")
			writeGatewayError(c, attemptErr)
			return
		}
		artifact, artifactErr := control.StoreDirectResponseArtifact(c.Request.Context(), operation, attempt, "response", contentType, upstreamBody)
		if artifactErr != nil {
			_ = control.DisputeBillingHold(c.Request.Context(), operation.ID, "response_artifact_failed")
			_ = control.CompleteAIAttempt(c.Request.Context(), provider.AttemptID, controlplane.AIAttemptStatusFailed, "response_artifact_failed")
			_ = complete(controlplane.AIOperationStatusFailed, "response_artifact_failed")
			writeDirectMediaArtifactError(c, artifactErr)
			return
		}
		if artifact.ID != "" {
			c.Header("X-AsterRouter-Output-Artifact-ID", artifact.ID)
		}
	}
	if err := control.RecordGatewayCall(c.Request.Context(), auth, request.Model, status, summary); err != nil {
		_ = control.DisputeBillingHold(c.Request.Context(), operation.ID, "provider_response_not_accounted")
		_ = control.CompleteAIAttempt(c.Request.Context(), provider.AttemptID, controlplane.AIAttemptStatusFailed, "audit_error")
		_ = complete(controlplane.AIOperationStatusFailed, "audit_error")
		openAIError(c, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	usage := parseGatewayUsage(upstreamBody)
	usageSource := "gateway_final"
	if gatewayAttemptsBillingUncertain(attempts) || !gatewayUsageObservationFinal(usage) {
		usageSource = "gateway_observation"
		_ = control.DisputeBillingHold(c.Request.Context(), operation.ID, "provider_usage_unconfirmed")
	}
	errorType := ""
	if status == "upstream_error" {
		errorType = "upstream_status"
	}
	attemptStatus := controlplane.AIAttemptStatusSucceeded
	operationStatus := controlplane.AIOperationStatusSucceeded
	if status == "upstream_error" {
		attemptStatus = controlplane.AIAttemptStatusFailed
		operationStatus = controlplane.AIOperationStatusFailed
	}
	_ = control.CompleteAIAttempt(c.Request.Context(), provider.AttemptID, attemptStatus, errorType)
	usageInput := gatewayUsageInputForProtocol(request, provider, usageSource, status, errorType, time.Since(startedAt).Milliseconds(), ttftMS, usage, upstreamRequestID(resp))
	if gatewayAudioProtocol(request.Protocol) {
		usageInput.UsageDimensions = gatewayAudioUsageDimensions(request, int64(len(upstreamBody)))
	}
	usageErr := recordGatewayUsage(control, c, auth, usageInput)
	if usageErr != nil {
		_ = complete(controlplane.AIOperationStatusFailed, "usage_ledger_error")
		recordGatewayTrace(control, c, auth, gatewayTraceInput(request, provider, "error", http.StatusInternalServerError, "usage_ledger_error", time.Since(startedAt).Milliseconds(), usage.InputTokens, usage.OutputTokens, usageErr.Error(), routeAttempts))
		openAIError(c, http.StatusInternalServerError, "server_error", "failed to record gateway usage")
		return
	}
	_ = complete(operationStatus, errorType)
	recordGatewayTrace(control, c, auth, gatewayTraceInput(request, provider, status, resp.StatusCode, errorType, time.Since(startedAt).Milliseconds(), usage.InputTokens, usage.OutputTokens, upstreamResponseSummary(resp.StatusCode, upstreamBody), routeAttempts))
	c.Data(resp.StatusCode, contentType, upstreamBody)
}

func gatewayUsageInputForProtocol(request gatewaycore.CanonicalRequest, provider controlplane.GatewayProvider, usageSource, status, errorType string, latencyMS int64, ttftMS *int64, usage gatewayUsageObservation, requestID string) controlplane.GatewayUsageInput {
	return controlplane.GatewayUsageInput{
		UsageSource: usageSource, Model: request.Model, UpstreamModel: provider.UpstreamModel, Protocol: string(request.Protocol), ProviderID: provider.ID, ProviderAccountID: provider.AccountID,
		Status: status, ErrorType: errorType, LatencyMS: latencyMS, TTFTMS: ttftMS, InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, TotalInputTokens: usage.TotalInputTokens,
		UncachedInputTokens: usage.UncachedInputTokens, CacheReadTokens: usage.CacheReadTokens, CacheWrite5mTokens: usage.CacheWrite5mTokens, CacheWrite1hTokens: usage.CacheWrite1hTokens,
		CacheFieldsPresent: usage.CacheFieldsPresent, UsageNormalizationStatus: usage.UsageNormalizationStatus, UpstreamRequestID: requestID,
	}
}
