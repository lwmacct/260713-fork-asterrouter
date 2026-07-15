package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/gin-gonic/gin"
)

const (
	gatewayRequestBodyLimit  = 16 << 20
	gatewayUpstreamBodyLimit = 16 << 20
	// failureBodyPreviewLimit bounds how much of a failed candidate's
	// response body is read for temp-unschedulable keyword matching. The
	// body is discarded either way (that candidate's response is never
	// returned to the caller), so this only needs to be large enough to
	// contain a typical error message.
	failureBodyPreviewLimit = 4 << 10
)

var (
	errGatewayRequestTooLarge   = errors.New("gateway request body is too large")
	errUpstreamResponseTooLarge = errors.New("upstream response body is too large")
	errGatewaySSEIncomplete     = errors.New("upstream sse stream ended without a terminal event")
	errNoSchedulableSlot        = errors.New("no schedulable provider account slot is available")
)

const (
	gatewayOperationContextKey   = "gateway_operation_id"
	gatewayAttemptContextKey     = "gateway_attempt_id"
	gatewayFingerprintContextKey = "gateway_request_fingerprint"
)

func registerGatewayRoutes(r *gin.Engine, control *controlplane.Service, durableJobs DurableAIJobAdmission, directAI controlplane.DirectAIProviderAdapter) {
	registerGatewayJobRoutes(r, control, durableJobs)
	registerGatewayMediaJobRoutes(r, control, durableJobs, directAI)
	registerGatewayImageRoutes(r, control, durableJobs, directAI)

	r.GET("/v1/models", func(c *gin.Context) {
		if control == nil {
			openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
			return
		}
		credential, err := gatewaycore.ExtractCredential(c.Request, gatewaycore.ProtocolOpenAIModels)
		if err != nil {
			writeGatewayError(c, controlplane.ErrGatewayUnauthorized)
			return
		}
		request, err := gatewaycore.CanonicalizeOpenAIModels(c.Request.Header)
		if err != nil {
			openAIError(c, http.StatusBadRequest, "invalid_request_error", "invalid models request")
			return
		}
		request.SourceIP = gatewaySourceIP(c.Request)
		auth, _, err := control.AuthorizeCanonicalGatewayRequest(c.Request.Context(), credential, request)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		models, err := control.GatewayModelsForAuth(c.Request.Context(), auth)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		data := make([]gin.H, 0, len(models))
		for _, model := range models {
			data = append(data, gin.H{"id": model, "object": "model", "owned_by": "asterrouter"})
		}
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
	})

	r.POST("/v1/chat/completions", func(c *gin.Context) {
		if control == nil {
			openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
			return
		}
		req, err := parseCanonicalChatCompletionRequest(c)
		if err != nil {
			if errors.Is(err, errGatewayRequestTooLarge) {
				openAIError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body exceeds 16 MiB limit")
				return
			}
			openAIError(c, http.StatusBadRequest, "invalid_request_error", "invalid chat completion payload")
			return
		}
		credential, err := gatewaycore.ExtractCredential(c.Request, gatewaycore.ProtocolOpenAIChat)
		if err != nil {
			writeGatewayError(c, controlplane.ErrGatewayUnauthorized)
			return
		}
		auth, canonicalAuth, err := control.AuthorizeCanonicalGatewayRequest(c.Request.Context(), credential, req)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		startedAt := time.Now()
		if err := control.EnforceGatewayPolicy(c.Request.Context(), auth); err != nil {
			errorType := gatewayPolicyErrorType(err)
			_ = control.RecordGatewayCall(c.Request.Context(), auth, req.Model, "policy_rejected", err.Error())
			_ = recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
				Model: req.Model, Protocol: string(req.Protocol), Status: "error", ErrorType: errorType,
				LatencyMS: time.Since(startedAt).Milliseconds(),
			})
			recordGatewayTrace(control, c, auth, gatewayTraceInput(req, controlplane.GatewayProvider{}, "error", http.StatusTooManyRequests, errorType, time.Since(startedAt).Milliseconds(), 0, 0, err.Error(), ""))
			writeGatewayError(c, err)
			return
		}
		plan, err := control.PlanCanonicalGatewayRequest(c.Request.Context(), canonicalAuth, req)
		if err != nil {
			_ = recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
				Model: req.Model, Protocol: string(req.Protocol), Status: "error", ErrorType: "provider_selection_error",
				LatencyMS: time.Since(startedAt).Milliseconds(),
			})
			recordGatewayTrace(control, c, auth, gatewayTraceInput(req, controlplane.GatewayProvider{}, "error", 0, "provider_selection_error", time.Since(startedAt).Milliseconds(), 0, 0, err.Error(), ""))
			writeGatewayError(c, err)
			return
		}
		if len(plan.Candidates) == 0 {
			routeErr := controlplane.ErrGatewayRouteUnavailable
			_ = control.RecordGatewayCall(c.Request.Context(), auth, req.Model, "policy_rejected", routeErr.Error())
			_ = recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
				Model: req.Model, Protocol: string(req.Protocol), Status: "error", ErrorType: "route_unavailable",
				LatencyMS: time.Since(startedAt).Milliseconds(),
			})
			recordGatewayTrace(control, c, auth, gatewayTraceInput(req, controlplane.GatewayProvider{}, "error", http.StatusServiceUnavailable, "route_unavailable", time.Since(startedAt).Milliseconds(), 0, 0, routeErr.Error(), marshalRouteEvidence(plan.Exclusions, nil)))
			writeGatewayError(c, routeErr)
			return
		}
		operation, created, err := control.BeginCanonicalOperation(c.Request.Context(), canonicalAuth, req)
		if err != nil {
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
		operationCompleted := false
		completeOperation := func(status, errorType string) error {
			if operationCompleted {
				return nil
			}
			if completeErr := control.CompleteAIOperation(c.Request.Context(), operation.ID, status, errorType); completeErr != nil {
				return completeErr
			}
			operationCompleted = true
			return nil
		}
		defer func() {
			if !operationCompleted {
				_ = control.CompleteAIOperation(c.Request.Context(), operation.ID, controlplane.AIOperationStatusFailed, "request_aborted")
			}
		}()
		if len(plan.Candidates) > 0 {
			if err := control.MarkAIOperationRunning(c.Request.Context(), operation.ID); err != nil {
				_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "operation_start_failed")
				_ = completeOperation(controlplane.AIOperationStatusFailed, "operation_transition_error")
				openAIError(c, http.StatusInternalServerError, "server_error", "failed to start gateway operation")
				return
			}
			credentialPermit, capacityReason, capacityAcquired, err := control.TryAcquireGatewayCredentialPermit(c.Request.Context(), canonicalAuth, estimateGatewayRequestTokens(req.Payload))
			if err != nil {
				_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "credential_capacity_error")
				_ = completeOperation(controlplane.AIOperationStatusFailed, "credential_capacity_error")
				openAIError(c, http.StatusInternalServerError, "server_error", "failed to reserve gateway credential capacity")
				return
			}
			if !capacityAcquired {
				_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "credential_capacity_rejected")
				_ = recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{Model: req.Model, Protocol: string(req.Protocol), Status: "error", ErrorType: capacityReason, LatencyMS: time.Since(startedAt).Milliseconds()})
				recordGatewayTrace(control, c, auth, gatewayTraceInput(req, controlplane.GatewayProvider{}, "error", http.StatusTooManyRequests, capacityReason, time.Since(startedAt).Milliseconds(), 0, 0, "gateway credential capacity rejected the request", ""))
				_ = completeOperation(controlplane.AIOperationStatusFailed, capacityReason)
				writeGatewayError(c, controlplane.ErrGatewayCapacityLimited)
				return
			}
			defer credentialPermit.Release()
			affinity := controlplane.GatewayAffinityInput{
				TenantID: canonicalAuth.TenantID, PrincipalID: canonicalAuth.PrincipalID, CredentialID: canonicalAuth.CredentialID,
				Model: req.Model, Protocol: string(req.Protocol), RouteGroup: plan.RouteGroup, StickyKey: req.StickyKey,
				PolicyVersion: canonicalAuth.PolicyVersion,
			}
			cohortKey := control.GatewayEffectivePricingCohortKey(affinity)
			pricedCandidates := control.OrderGatewayCandidatesByEffectivePricing(c.Request.Context(), req.Model, string(req.Protocol), cohortKey, plan.Candidates)
			candidates := control.PreferGatewayCandidatesWithAffinity(c.Request.Context(), affinity, pricedCandidates)
			resp, provider, release, attempts, attemptErr := attemptGatewayCandidates(c, control, operation.ID, affinity, candidates, req.Payload, req.Stream)
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
				_ = control.RecordGatewayCall(c.Request.Context(), auth, req.Model, "upstream_error", attemptErr.Error())
				_ = recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
					UsageSource:       "gateway_observation",
					Model:             req.Model,
					UpstreamModel:     provider.UpstreamModel,
					Protocol:          string(req.Protocol),
					ProviderID:        provider.ID,
					ProviderAccountID: provider.AccountID,
					Status:            "upstream_error",
					ErrorType:         "transport_error",
					LatencyMS:         time.Since(startedAt).Milliseconds(),
				})
				recordGatewayTrace(control, c, auth, gatewayTraceInput(req, provider, "upstream_error", 0, "transport_error", time.Since(startedAt).Milliseconds(), 0, 0, attemptErr.Error(), routeAttempts))
				_ = completeOperation(controlplane.AIOperationStatusFailed, "transport_error")
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
			summary := gatewayRouteSummary(req.Model, provider)
			if req.Stream {
				if err := control.RecordGatewayCall(c.Request.Context(), auth, req.Model, status, summary); err != nil {
					_ = control.DisputeBillingHold(c.Request.Context(), operation.ID, "provider_response_not_accounted")
					_ = control.CompleteAIAttempt(c.Request.Context(), provider.AttemptID, controlplane.AIAttemptStatusFailed, "audit_error")
					_ = completeOperation(controlplane.AIOperationStatusFailed, "audit_error")
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
				usageErr := recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
					UsageSource:              usageSource,
					Model:                    req.Model,
					UpstreamModel:            provider.UpstreamModel,
					Protocol:                 string(req.Protocol),
					ProviderID:               provider.ID,
					ProviderAccountID:        provider.AccountID,
					Status:                   usageStatus,
					ErrorType:                errorType,
					LatencyMS:                time.Since(startedAt).Milliseconds(),
					TTFTMS:                   ttftMS,
					InputTokens:              streamUsage.InputTokens,
					OutputTokens:             streamUsage.OutputTokens,
					TotalInputTokens:         streamUsage.TotalInputTokens,
					UncachedInputTokens:      streamUsage.UncachedInputTokens,
					CacheReadTokens:          streamUsage.CacheReadTokens,
					CacheWrite5mTokens:       streamUsage.CacheWrite5mTokens,
					CacheWrite1hTokens:       streamUsage.CacheWrite1hTokens,
					CacheFieldsPresent:       streamUsage.CacheFieldsPresent,
					UsageNormalizationStatus: streamUsage.UsageNormalizationStatus,
					UpstreamRequestID:        upstreamRequestID(resp),
				})
				if usageErr != nil {
					_ = completeOperation(controlplane.AIOperationStatusFailed, "usage_ledger_error")
					_ = c.Error(usageErr)
				} else {
					_ = completeOperation(operationStatus, errorType)
				}
				recordGatewayTrace(control, c, auth, gatewayTraceInput(req, provider, usageStatus, resp.StatusCode, errorType, time.Since(startedAt).Milliseconds(), streamUsage.InputTokens, streamUsage.OutputTokens, responseSummary, routeAttempts))
				if streamErr != nil && !c.Writer.Written() {
					openAIError(c, http.StatusBadGateway, "upstream_error", streamErr.Error())
				}
				return
			}

			contentType, upstreamBody, ttftMS, err := readUpstreamResponse(resp, startedAt)
			if err != nil {
				_ = control.DisputeBillingHold(c.Request.Context(), operation.ID, "provider_response_read_failed")
				_ = control.CompleteAIAttempt(c.Request.Context(), provider.AttemptID, controlplane.AIAttemptStatusFailed, "response_read_error")
				_ = completeOperation(controlplane.AIOperationStatusFailed, "response_read_error")
				_ = control.RecordGatewayCall(c.Request.Context(), auth, req.Model, "upstream_error", err.Error())
				_ = recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
					UsageSource:       "gateway_observation",
					Model:             req.Model,
					UpstreamModel:     provider.UpstreamModel,
					Protocol:          string(req.Protocol),
					ProviderID:        provider.ID,
					ProviderAccountID: provider.AccountID,
					Status:            "upstream_error",
					ErrorType:         "response_read_error",
					LatencyMS:         time.Since(startedAt).Milliseconds(),
				})
				recordGatewayTrace(control, c, auth, gatewayTraceInput(req, provider, "upstream_error", resp.StatusCode, "response_read_error", time.Since(startedAt).Milliseconds(), 0, 0, err.Error(), routeAttempts))
				openAIError(c, http.StatusBadGateway, "upstream_error", err.Error())
				return
			}
			if err := control.RecordGatewayCall(c.Request.Context(), auth, req.Model, status, summary); err != nil {
				_ = control.DisputeBillingHold(c.Request.Context(), operation.ID, "provider_response_not_accounted")
				_ = control.CompleteAIAttempt(c.Request.Context(), provider.AttemptID, controlplane.AIAttemptStatusFailed, "audit_error")
				_ = completeOperation(controlplane.AIOperationStatusFailed, "audit_error")
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
			usageErr := recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
				UsageSource:              usageSource,
				Model:                    req.Model,
				UpstreamModel:            provider.UpstreamModel,
				Protocol:                 string(req.Protocol),
				ProviderID:               provider.ID,
				ProviderAccountID:        provider.AccountID,
				Status:                   status,
				ErrorType:                errorType,
				LatencyMS:                time.Since(startedAt).Milliseconds(),
				TTFTMS:                   ttftMS,
				InputTokens:              usage.InputTokens,
				OutputTokens:             usage.OutputTokens,
				TotalInputTokens:         usage.TotalInputTokens,
				UncachedInputTokens:      usage.UncachedInputTokens,
				CacheReadTokens:          usage.CacheReadTokens,
				CacheWrite5mTokens:       usage.CacheWrite5mTokens,
				CacheWrite1hTokens:       usage.CacheWrite1hTokens,
				CacheFieldsPresent:       usage.CacheFieldsPresent,
				UsageNormalizationStatus: usage.UsageNormalizationStatus,
				UpstreamRequestID:        upstreamRequestID(resp),
			})
			if usageErr != nil {
				_ = completeOperation(controlplane.AIOperationStatusFailed, "usage_ledger_error")
				recordGatewayTrace(control, c, auth, gatewayTraceInput(req, provider, "error", http.StatusInternalServerError, "usage_ledger_error", time.Since(startedAt).Milliseconds(), usage.InputTokens, usage.OutputTokens, usageErr.Error(), routeAttempts))
				openAIError(c, http.StatusInternalServerError, "server_error", "failed to record gateway usage")
				return
			}
			_ = completeOperation(operationStatus, errorType)
			recordGatewayTrace(control, c, auth, gatewayTraceInput(req, provider, status, resp.StatusCode, errorType, time.Since(startedAt).Milliseconds(), usage.InputTokens, usage.OutputTokens, upstreamResponseSummary(resp.StatusCode, upstreamBody), routeAttempts))
			c.Data(resp.StatusCode, contentType, upstreamBody)
			return
		}
	})
}

func parseCanonicalChatCompletionRequest(c *gin.Context) (gatewaycore.CanonicalRequest, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, gatewayRequestBodyLimit)
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return gatewaycore.CanonicalRequest{}, errGatewayRequestTooLarge
		}
		return gatewaycore.CanonicalRequest{}, err
	}
	request, err := gatewaycore.CanonicalizeOpenAIChat(rawBody, c.Request.Header)
	if err != nil {
		return gatewaycore.CanonicalRequest{}, err
	}
	request.SourceIP = gatewaySourceIP(c.Request)
	return request, nil
}

func gatewaySourceIP(request *http.Request) string {
	if request == nil {
		return ""
	}
	remoteAddress := strings.TrimSpace(request.RemoteAddr)
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		return strings.TrimSpace(host)
	}
	return remoteAddress
}

// gatewayRouteAttempt records what happened when the gateway tried a single
// candidate route while resolving a chat completion request. It is
// serialized into GatewayTrace.RouteAttempts so operators can see which
// candidates were skipped or failed, and why, without needing verbose logs.
type gatewayRouteAttempt struct {
	AttemptID  string `json:"attempt_id,omitempty"`
	AccountID  string `json:"account_id,omitempty"`
	ProviderID string `json:"provider_id,omitempty"`
	RouteID    string `json:"route_id,omitempty"`
	RouteGroup string `json:"route_group,omitempty"`
	Model      string `json:"upstream_model,omitempty"`
	Outcome    string `json:"outcome"`
	Detail     string `json:"detail,omitempty"`
}

func marshalRouteAttempts(attempts []gatewayRouteAttempt) string {
	if len(attempts) == 0 {
		return "[]"
	}
	data, err := json.Marshal(attempts)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func marshalRouteEvidence(exclusions []controlplane.GatewayCandidateExclusion, attempts []gatewayRouteAttempt) string {
	evidence := make([]gatewayRouteAttempt, 0, len(exclusions)+len(attempts))
	for _, exclusion := range exclusions {
		evidence = append(evidence, gatewayRouteAttempt{
			AccountID: exclusion.ProviderAccountID, ProviderID: exclusion.ProviderID, RouteID: exclusion.RouteID,
			Model: exclusion.UpstreamModel, Outcome: "excluded", Detail: exclusion.Reason,
		})
	}
	evidence = append(evidence, attempts...)
	return marshalRouteAttempts(evidence)
}

// isProviderAccountFailureStatus reports whether an upstream HTTP status
// indicates an account-side problem (auth revoked, rate limited, or upstream
// server error) rather than a problem with the request itself. Candidates
// that are not the last one tried are retried against the next candidate
// when they return one of these statuses.
func isProviderAccountFailureStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		return true
	default:
		return statusCode >= 500
	}
}

// attemptGatewayCandidates tries each candidate route in order until one
// produces a response the caller should use. A candidate is skipped without
// being attempted if its distributed capacity lease cannot be acquired. A candidate that
// fails at the transport level, or that is not the last candidate and
// returns an account-side failure status, is recorded as a failure (cooling
// the underlying provider account down) and the loop moves to the next
// candidate. The last candidate's response is always accepted as-is, even on
// a failure status, matching the existing behavior of passing upstream error
// responses through to the caller when no better alternative exists.
//
// On success, the returned release func must be called by the caller once
// the response body has been fully consumed (streamed or read). Losing
// candidates' leases are released internally and must not be released again.
func attemptGatewayCandidates(c *gin.Context, control *controlplane.Service, operationID string, affinityInput controlplane.GatewayAffinityInput, candidates []controlplane.GatewayProvider, rawBody []byte, stream bool) (resp *http.Response, provider controlplane.GatewayProvider, release func(), attempts []gatewayRouteAttempt, transportErr error) {
	estimatedTokens := estimateGatewayRequestTokens(rawBody)
	for i, candidate := range candidates {
		attempt, err := control.BeginAIAttempt(c.Request.Context(), operationID, i+1, candidate)
		if err != nil {
			return nil, candidate, nil, attempts, err
		}
		candidate.AttemptID = attempt.ID
		provider = candidate
		permit, reason, acquired, capacityErr := control.TryAcquireProviderAccountPermitContext(c.Request.Context(), candidate, estimatedTokens, "provider_lease_"+attempt.ID)
		if capacityErr != nil {
			if err := control.CompleteAIAttempt(c.Request.Context(), attempt.ID, controlplane.AIAttemptStatusFailed, "capacity_store_error"); err != nil {
				return nil, candidate, nil, attempts, err
			}
			return nil, candidate, nil, attempts, capacityErr
		}
		if !acquired {
			if err := control.CompleteAIAttempt(c.Request.Context(), attempt.ID, controlplane.AIAttemptStatusSkipped, reason); err != nil {
				return nil, candidate, nil, attempts, err
			}
			attempts = append(attempts, gatewayRouteAttempt{AttemptID: attempt.ID, AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "skipped", Detail: reason})
			continue
		}
		upstreamAffinity, affinityApplied, affinityErr := control.ResolveGatewayUpstreamAffinity(c.Request.Context(), affinityInput, candidate)
		if affinityErr != nil {
			candidate.SelectionReason = appendGatewaySelectionReason(candidate.SelectionReason, "upstream cache affinity unavailable")
		} else if affinityApplied {
			candidate.SelectionReason = appendGatewaySelectionReason(candidate.SelectionReason, "verified upstream cache affinity injected")
		}
		candidateResp, err := forwardChatCompletion(c, candidate, rawBody, stream, upstreamAffinity)
		if err != nil {
			permit.Release()
			if candidate.AccountID != "" {
				_ = control.RecordProviderAccountFailure(c.Request.Context(), candidate.AccountID, 0, err.Error())
			}
			if completeErr := control.CompleteAIAttempt(c.Request.Context(), attempt.ID, controlplane.AIAttemptStatusFailed, "transport_error"); completeErr != nil {
				return nil, candidate, nil, attempts, completeErr
			}
			attempts = append(attempts, gatewayRouteAttempt{AttemptID: attempt.ID, AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "failed", Detail: err.Error()})
			billingErr := control.DisputeBillingHold(c.Request.Context(), operationID, "provider_transport_unknown")
			transportErr = errors.Join(err, billingErr)
			if billingErr != nil {
				return nil, candidate, nil, attempts, transportErr
			}
			continue
		}
		isLast := i == len(candidates)-1
		if !isLast && isProviderAccountFailureStatus(candidateResp.StatusCode) {
			bodyPreview, _ := io.ReadAll(io.LimitReader(candidateResp.Body, failureBodyPreviewLimit))
			_ = candidateResp.Body.Close()
			permit.Release()
			if candidate.AccountID != "" {
				_ = control.RecordProviderAccountFailure(c.Request.Context(), candidate.AccountID, candidateResp.StatusCode, string(bodyPreview))
			}
			detail := fmt.Sprintf("upstream http status %d", candidateResp.StatusCode)
			if completeErr := control.CompleteAIAttempt(c.Request.Context(), attempt.ID, controlplane.AIAttemptStatusFailed, "upstream_status"); completeErr != nil {
				return nil, candidate, nil, attempts, completeErr
			}
			attempts = append(attempts, gatewayRouteAttempt{AttemptID: attempt.ID, AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "failed", Detail: detail})
			billingErr := control.DisputeBillingHold(c.Request.Context(), operationID, "provider_attempt_usage_unconfirmed")
			transportErr = errors.Join(errors.New(detail), billingErr)
			if billingErr != nil {
				return nil, candidate, nil, attempts, transportErr
			}
			continue
		}
		if billingErr := control.CommitBillingHold(c.Request.Context(), operationID, "provider_response_received"); billingErr != nil {
			_ = candidateResp.Body.Close()
			permit.Release()
			_ = control.CompleteAIAttempt(c.Request.Context(), attempt.ID, controlplane.AIAttemptStatusFailed, "billing_hold_error")
			attempts = append(attempts, gatewayRouteAttempt{AttemptID: attempt.ID, AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "failed", Detail: "provider response received but billing hold commit failed"})
			disputeErr := control.DisputeBillingHold(c.Request.Context(), operationID, "provider_response_billing_unknown")
			return nil, candidate, nil, attempts, errors.Join(billingErr, disputeErr)
		}
		if isProviderAccountFailureStatus(candidateResp.StatusCode) {
			if candidate.AccountID != "" {
				_ = control.RecordProviderAccountFailure(c.Request.Context(), candidate.AccountID, candidateResp.StatusCode, "")
			}
		} else if candidateResp.StatusCode >= 200 && candidateResp.StatusCode < 400 {
			_ = control.RecordProviderAccountSuccess(c.Request.Context(), candidate.AccountID)
		}
		if candidate.AccountID != "" {
			_ = control.TouchProviderAccountUsage(c.Request.Context(), candidate.AccountID)
		}
		attempts = append(attempts, gatewayRouteAttempt{AttemptID: attempt.ID, AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "selected"})
		return candidateResp, candidate, permit.Release, attempts, nil
	}
	return nil, provider, nil, attempts, transportErr
}

func gatewayAttemptsBillingUncertain(attempts []gatewayRouteAttempt) bool {
	for _, attempt := range attempts {
		if attempt.Outcome == "failed" {
			return true
		}
	}
	return false
}

func gatewayUsageObservationFinal(usage gatewayUsageObservation) bool {
	switch usage.UsageNormalizationStatus {
	case usageNormalizationOpenAI, usageNormalizationAnthropic, usageNormalizationGeneric:
		return true
	default:
		return false
	}
}

func appendGatewaySelectionReason(current, reason string) string {
	current = strings.TrimSpace(current)
	if current == "" {
		return reason
	}
	return current + "; " + reason
}

func estimateGatewayRequestTokens(rawBody []byte) int {
	if len(rawBody) == 0 {
		return 0
	}
	var limits struct {
		MaxTokens           int `json:"max_tokens"`
		MaxCompletionTokens int `json:"max_completion_tokens"`
	}
	_ = json.Unmarshal(rawBody, &limits)
	completionTokens := limits.MaxTokens
	if limits.MaxCompletionTokens > completionTokens {
		completionTokens = limits.MaxCompletionTokens
	}
	return (len(rawBody)+3)/4 + completionTokens
}

func forwardChatCompletion(c *gin.Context, provider controlplane.GatewayProvider, rawBody []byte, stream bool, affinity controlplane.GatewayUpstreamAffinity) (*http.Response, error) {
	upstreamBody, err := rewriteGatewayRequest(rawBody, provider.UpstreamModel, affinity)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(provider.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, endpoint, bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	req.Header.Set("Content-Type", "application/json")
	if affinity.HeaderName != "" && affinity.Value != "" {
		req.Header.Set(affinity.HeaderName, affinity.Value)
	}
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	return gatewayHTTPClient(stream).Do(req)
}

func rewriteGatewayModel(rawBody []byte, upstreamModel string) ([]byte, error) {
	return rewriteGatewayRequest(rawBody, upstreamModel, controlplane.GatewayUpstreamAffinity{})
}

func rewriteGatewayRequest(rawBody []byte, upstreamModel string, affinity controlplane.GatewayUpstreamAffinity) ([]byte, error) {
	upstreamModel = strings.TrimSpace(upstreamModel)
	if upstreamModel == "" {
		return nil, errors.New("model route upstream_model is empty")
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return nil, err
	}
	payload["model"] = upstreamModel
	if affinity.BodyField != "" && affinity.Value != "" {
		payload[affinity.BodyField] = affinity.Value
	}
	if affinity.PromptCacheKey && affinity.Value != "" {
		payload["prompt_cache_key"] = affinity.Value
	}
	return json.Marshal(payload)
}

func gatewayRouteSummary(model string, provider controlplane.GatewayProvider) string {
	summary := fmt.Sprintf("Forwarded chat completion request for model %s to provider %s", model, provider.ID)
	if provider.AccountID != "" {
		summary += fmt.Sprintf(" account %s", provider.AccountID)
	}
	if provider.UpstreamModel != "" && provider.UpstreamModel != model {
		summary += fmt.Sprintf(" upstream_model %s", provider.UpstreamModel)
	}
	if provider.SelectionReason != "" {
		summary += "; " + provider.SelectionReason
	}
	return summary
}

func gatewayTraceInput(req gatewaycore.CanonicalRequest, provider controlplane.GatewayProvider, status string, httpStatus int, errorType string, latencyMS int64, inputTokens int, outputTokens int, responseSummary string, routeAttempts string) controlplane.GatewayTraceInput {
	return controlplane.GatewayTraceInput{
		Model:             req.Model,
		Stream:            req.Stream,
		MessageCount:      req.MessageCount,
		ProviderID:        provider.ID,
		ProviderAccountID: provider.AccountID,
		GatewayModelID:    provider.GatewayModelID,
		RouteID:           provider.RouteID,
		RouteGroup:        provider.RouteGroup,
		UpstreamModel:     provider.UpstreamModel,
		RouteSource:       provider.Source,
		RouteReason:       provider.SelectionReason,
		Status:            status,
		HTTPStatus:        httpStatus,
		ErrorType:         errorType,
		LatencyMS:         latencyMS,
		InputTokens:       inputTokens,
		OutputTokens:      outputTokens,
		RequestSummary:    fmt.Sprintf("chat.completions stream=%t messages=%d", req.Stream, req.MessageCount),
		ResponseSummary:   responseSummary,
		RouteAttempts:     routeAttempts,
	}
}

func gatewayPolicyErrorType(err error) string {
	switch {
	case errors.Is(err, controlplane.ErrGatewayRateLimited):
		return "rate_limit_exceeded"
	case errors.Is(err, controlplane.ErrGatewayQuotaExceeded):
		return "quota_exceeded"
	case errors.Is(err, controlplane.ErrGatewayBudgetExceeded):
		return "budget_exceeded"
	default:
		return "policy_error"
	}
}

func upstreamResponseSummary(statusCode int, body []byte) string {
	var payload struct {
		ID     string `json:"id"`
		Object string `json:"object"`
		Error  struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)
	parts := []string{fmt.Sprintf("http=%d", statusCode), fmt.Sprintf("bytes=%d", len(body))}
	if payload.ID != "" {
		parts = append(parts, "id="+payload.ID)
	}
	if payload.Object != "" {
		parts = append(parts, "object="+payload.Object)
	}
	if payload.Error.Type != "" {
		parts = append(parts, "error_type="+payload.Error.Type)
	}
	return strings.Join(parts, " ")
}

var gatewayHTTPClient = func(stream bool) *http.Client {
	if stream {
		return &http.Client{}
	}
	return &http.Client{Timeout: 120 * time.Second}
}

func readUpstreamResponse(resp *http.Response, startedAt time.Time) (string, []byte, *int64, error) {
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	reader := io.LimitReader(resp.Body, gatewayUpstreamBodyLimit+1)
	var body bytes.Buffer
	buffer := make([]byte, 32*1024)
	var ttftMS *int64
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			if ttftMS == nil {
				value := time.Since(startedAt).Milliseconds()
				ttftMS = &value
			}
			_, _ = body.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", nil, ttftMS, readErr
		}
	}
	if body.Len() > gatewayUpstreamBodyLimit {
		return "", nil, ttftMS, errUpstreamResponseTooLarge
	}
	return contentType, body.Bytes(), ttftMS, nil
}

func parseUpstreamUsage(body []byte) (int, int) {
	usage := parseGatewayUsage(body)
	return usage.InputTokens, usage.OutputTokens
}

func recordGatewayUsage(control *controlplane.Service, c *gin.Context, auth controlplane.GatewayAuthContext, input controlplane.GatewayUsageInput) error {
	if control == nil {
		return nil
	}
	input.OperationID = c.GetString(gatewayOperationContextKey)
	input.AttemptID = c.GetString(gatewayAttemptContextKey)
	input.RequestFingerprint = c.GetString(gatewayFingerprintContextKey)
	return control.RecordGatewayUsage(c.Request.Context(), auth, input)
}

func recordGatewayTrace(control *controlplane.Service, c *gin.Context, auth controlplane.GatewayAuthContext, input controlplane.GatewayTraceInput) {
	if control != nil {
		if value := c.GetString(gatewayOperationContextKey); value != "" {
			input.OperationID = value
		}
		if value := c.GetString(gatewayAttemptContextKey); value != "" {
			input.AttemptID = value
		}
		if value := c.GetString(gatewayFingerprintContextKey); value != "" {
			input.RequestFingerprint = value
		}
		_ = control.RecordGatewayTrace(c.Request.Context(), auth, input)
	}
}

func streamUpstreamResponse(c *gin.Context, resp *http.Response, startedAt time.Time) (gatewayUsageObservation, *int64, error) {
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/event-stream"
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(resp.StatusCode)

	buf := make([]byte, 32*1024)
	collector := gatewaySSEUsageCollector{}
	var ttftMS *int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if ttftMS == nil {
				value := time.Since(startedAt).Milliseconds()
				ttftMS = &value
			}
			collector.Write(buf[:n])
			if _, err := c.Writer.Write(buf[:n]); err != nil {
				return collector.Observation(), ttftMS, err
			}
			c.Writer.Flush()
		}
		if readErr == io.EOF {
			if !collector.Completed() {
				return collector.Observation(), ttftMS, errGatewaySSEIncomplete
			}
			return collector.Observation(), ttftMS, nil
		}
		if readErr != nil {
			return collector.Observation(), ttftMS, readErr
		}
	}
}

func upstreamRequestID(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	for _, key := range []string{"X-Request-ID", "Request-ID", "Anthropic-Request-ID", "X-Request-Id"} {
		if value := strings.TrimSpace(resp.Header.Get(key)); value != "" {
			return value
		}
	}
	return ""
}
