package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

const realtimeUpstreamHandshakeTimeout = 15 * time.Second

type realtimeCandidateConnection struct {
	upstream         *websocket.Conn
	provider         controlplane.GatewayProvider
	providerPermit   controlplane.ProviderAccountPermit
	session          controlplane.RealtimeSession
	attempts         []gatewayRouteAttempt
	billingUncertain bool
}

func registerGatewayRealtimeRoute(r *gin.Engine, control *controlplane.Service) {
	r.GET("/v1/realtime", func(c *gin.Context) {
		handleGatewayRealtime(c, control)
	})
}

func handleGatewayRealtime(c *gin.Context, control *controlplane.Service) {
	if control == nil {
		openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
		return
	}
	credential, err := gatewaycore.ExtractCredential(c.Request, gatewaycore.ProtocolRealtime)
	if err != nil {
		writeGatewayError(c, controlplane.ErrGatewayUnauthorized)
		return
	}
	models := c.Request.URL.Query()["model"]
	if len(models) != 1 {
		openAIError(c, http.StatusBadRequest, "invalid_request_error", "exactly one realtime model is required")
		return
	}
	request, err := gatewaycore.CanonicalizeRealtimeSession(c.Request.Header, models[0])
	if err != nil {
		openAIError(c, http.StatusBadRequest, "invalid_request_error", "invalid realtime session request")
		return
	}
	request.SourceIP = gatewaySourceIP(c.Request)
	auth, canonicalAuth, err := control.AuthorizeCanonicalGatewayRequest(c.Request.Context(), credential, request)
	if err != nil {
		writeGatewayError(c, err)
		return
	}
	startedAt := time.Now()
	if err := control.EnforceGatewayPolicy(c.Request.Context(), auth); err != nil {
		recordRealtimeRejected(c, control, auth, request, startedAt, gatewayPolicyErrorType(err), err)
		writeGatewayError(c, err)
		return
	}
	plan, err := control.PlanCanonicalGatewayRequest(c.Request.Context(), canonicalAuth, request)
	if err != nil {
		recordRealtimeRejected(c, control, auth, request, startedAt, "provider_selection_error", err)
		writeGatewayError(c, err)
		return
	}
	if len(plan.Candidates) == 0 {
		err = controlplane.ErrGatewayRouteUnavailable
		recordRealtimeRejected(c, control, auth, request, startedAt, "route_unavailable", err)
		writeGatewayError(c, err)
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
	operationCompleted := false
	defer func() {
		if operationCompleted {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = control.CompleteAIOperation(ctx, operation.ID, controlplane.AIOperationStatusFailed, "request_aborted")
	}()
	if err := control.MarkAIOperationRunning(c.Request.Context(), operation.ID); err != nil {
		_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "operation_start_failed")
		_ = completeRealtimeOperation(c.Request.Context(), control, operation.ID, controlplane.AIOperationStatusFailed, "operation_transition_error", &operationCompleted)
		openAIError(c, http.StatusInternalServerError, "server_error", "failed to start realtime operation")
		return
	}
	operation.Status = controlplane.AIOperationStatusRunning
	credentialPermit, capacityReason, acquired, err := control.TryAcquireGatewayCredentialPermit(c.Request.Context(), canonicalAuth, estimateGatewayRequestTokens(request.Payload))
	if err != nil {
		_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "credential_capacity_error")
		_ = completeRealtimeOperation(c.Request.Context(), control, operation.ID, controlplane.AIOperationStatusFailed, "credential_capacity_error", &operationCompleted)
		openAIError(c, http.StatusInternalServerError, "server_error", "failed to reserve realtime credential capacity")
		return
	}
	if !acquired {
		_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "credential_capacity_rejected")
		_ = completeRealtimeOperation(c.Request.Context(), control, operation.ID, controlplane.AIOperationStatusFailed, capacityReason, &operationCompleted)
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
	connection, err := dialRealtimeCandidates(c.Request.Context(), control, operation, canonicalAuth, candidates)
	routeAttempts := marshalRouteEvidence(plan.Exclusions, connection.attempts)
	if connection.provider.AttemptID != "" {
		c.Set(gatewayAttemptContextKey, connection.provider.AttemptID)
	}
	if err != nil || connection.upstream == nil {
		if !connection.billingUncertain {
			_ = control.ReleaseBillingHold(c.Request.Context(), operation.ID, "realtime_upstream_not_connected")
		} else {
			_ = control.DisputeBillingHold(c.Request.Context(), operation.ID, "realtime_upstream_handshake_unknown")
		}
		if err == nil {
			err = errNoSchedulableSlot
		}
		errorType := "upstream_handshake_error"
		if errors.Is(err, errNoSchedulableSlot) {
			errorType = "provider_capacity_exhausted"
		}
		_ = control.RecordGatewayCall(c.Request.Context(), auth, request.Model, "upstream_error", err.Error())
		_ = recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
			UsageSource: "gateway_observation", Model: request.Model, UpstreamModel: connection.provider.UpstreamModel,
			Protocol: string(request.Protocol), ProviderID: connection.provider.ID, ProviderAccountID: connection.provider.AccountID,
			Status: "upstream_error", ErrorType: errorType, LatencyMS: time.Since(startedAt).Milliseconds(),
		})
		recordGatewayTrace(control, c, auth, gatewayTraceInput(request, connection.provider, "upstream_error", 0, errorType, time.Since(startedAt).Milliseconds(), 0, 0, err.Error(), routeAttempts))
		_ = completeRealtimeOperation(c.Request.Context(), control, operation.ID, controlplane.AIOperationStatusFailed, errorType, &operationCompleted)
		if errors.Is(err, errNoSchedulableSlot) {
			writeGatewayError(c, controlplane.ErrGatewayCapacityLimited)
			return
		}
		openAIError(c, http.StatusBadGateway, "upstream_error", "failed to establish upstream realtime session")
		return
	}
	defer connection.providerPermit.Release()
	defer connection.upstream.CloseNow()
	c.Header("X-AsterRouter-Realtime-Session-ID", connection.session.ID)
	if err := control.RecordGatewayCall(c.Request.Context(), auth, request.Model, "forwarded", gatewayProtocolRouteSummary(request, connection.provider)); err != nil {
		failRealtimeBeforeClientUpgrade(control, operation, connection, "audit_error")
		_ = completeRealtimeOperation(c.Request.Context(), control, operation.ID, controlplane.AIOperationStatusFailed, "audit_error", &operationCompleted)
		openAIError(c, http.StatusInternalServerError, "server_error", "failed to record realtime routing")
		return
	}

	downstream, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		Subprotocols: []string{"realtime"}, InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		failRealtimeBeforeClientUpgrade(control, operation, connection, "client_upgrade_error")
		_ = completeRealtimeOperation(context.Background(), control, operation.ID, controlplane.AIOperationStatusFailed, "client_upgrade_error", &operationCompleted)
		return
	}
	defer downstream.CloseNow()
	connectedAt := time.Now().UTC()
	connection.session, err = control.UpdateRealtimeSession(context.Background(), connection.session.ID, connection.session.Version, controlplane.RealtimeSessionUpdate{
		Status: controlplane.RealtimeSessionStatusConnected, ConnectedAt: &connectedAt,
	})
	if err != nil {
		_ = downstream.Close(websocket.StatusInternalError, "session state error")
		failRealtimeBeforeClientUpgrade(control, operation, connection, "session_state_error")
		_ = completeRealtimeOperation(context.Background(), control, operation.ID, controlplane.AIOperationStatusFailed, "session_state_error", &operationCompleted)
		return
	}
	_ = control.BindGatewayCandidateAffinity(context.Background(), affinity, connection.provider)

	usageVersion := connection.session.UsageVersion
	totalInputTokens := 0
	totalOutputTokens := 0
	onUsage := func(ctx context.Context, usage gatewayUsageObservation, stats realtimeRelayStats) error {
		usageVersion++
		input := gatewayUsageInputForProtocol(request, connection.provider, "provider_incremental", "forwarded", "", time.Since(startedAt).Milliseconds(), nil, usage, "")
		input.OperationID = operation.ID
		input.AttemptID = connection.provider.AttemptID
		input.RequestFingerprint = operation.RequestFingerprint
		input.UsageVersion = usageVersion
		if err := control.RecordGatewayUsage(ctx, auth, input); err != nil {
			return err
		}
		if err := control.EnforceGatewayOngoingPolicy(ctx, auth); err != nil {
			switch {
			case errors.Is(err, controlplane.ErrGatewayQuotaExceeded):
				return errors.Join(errRealtimeQuotaExceeded, err)
			case errors.Is(err, controlplane.ErrGatewayBudgetExceeded):
				return errors.Join(errRealtimeBudgetExceeded, err)
			case errors.Is(err, controlplane.ErrGatewayRiskBlocked):
				return errors.Join(errRealtimeRiskBlocked, err)
			default:
				return err
			}
		}
		updated, err := control.UpdateRealtimeSession(ctx, connection.session.ID, connection.session.Version, realtimeSessionProgress(connectedAt, stats, usageVersion))
		if err != nil {
			return err
		}
		connection.session = updated
		totalInputTokens += usage.InputTokens
		totalOutputTokens += usage.OutputTokens
		return nil
	}
	revalidate := func(ctx context.Context) error {
		refreshed, _, err := control.RevalidateCanonicalGatewayRequest(ctx, credential, request)
		if err != nil {
			return classifyRealtimeRevalidationError(err)
		}
		if err := control.EnforceGatewayOngoingPolicy(ctx, refreshed); err != nil {
			return classifyRealtimeRevalidationError(err)
		}
		return nil
	}
	outcome := runRealtimeRelay(context.Background(), downstream, connection.upstream, connection.provider.UpstreamModel,
		credentialPermit.Lost(), connection.providerPermit.Lost(), realtimeRelayConfigForRequest(request), onUsage, revalidate)

	finalCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if outcome.Normal {
		usageVersion++
		if err := recordRealtimeFinalUsage(finalCtx, control, auth, operation, connection.provider, usageVersion, outcome.Stats, connection.session, "gateway_final", "forwarded", ""); err != nil {
			outcome.Normal = false
			outcome.ErrorType = "usage_ledger_error"
			outcome.Summary = err.Error()
			outcome.Err = err
		} else {
			connection.session.UsageVersion = usageVersion
		}
	}
	closedAt := time.Now().UTC()
	if outcome.Normal {
		connection.session, err = control.UpdateRealtimeSession(finalCtx, connection.session.ID, connection.session.Version, realtimeSessionTerminal(connectedAt, closedAt, outcome.Stats, usageVersion, controlplane.RealtimeSessionStatusCompleted, ""))
		if err == nil {
			_ = control.CompleteAIAttempt(finalCtx, connection.provider.AttemptID, controlplane.AIAttemptStatusSucceeded, "")
			err = completeRealtimeOperation(finalCtx, control, operation.ID, controlplane.AIOperationStatusSucceeded, "", &operationCompleted)
		}
		if err == nil {
			_ = downstream.Close(websocket.StatusNormalClosure, "session complete")
			_ = connection.upstream.Close(websocket.StatusNormalClosure, "session complete")
		}
	}
	if !outcome.Normal || err != nil {
		if outcome.ErrorType == "" {
			outcome.ErrorType = "session_state_error"
		}
		if outcome.Summary == "" {
			outcome.Summary = outcome.ErrorType
		}
		failureUsageVersion := usageVersion + 1
		if usageErr := recordRealtimeFinalUsage(finalCtx, control, auth, operation, connection.provider, failureUsageVersion, outcome.Stats, connection.session, "gateway_observation", "upstream_error", outcome.ErrorType); usageErr == nil {
			usageVersion = failureUsageVersion
		}
		_, _ = control.UpdateRealtimeSession(finalCtx, connection.session.ID, connection.session.Version, realtimeSessionTerminal(connectedAt, closedAt, outcome.Stats, usageVersion, controlplane.RealtimeSessionStatusFailed, outcome.ErrorType))
		_ = control.DisputeBillingHold(finalCtx, operation.ID, "realtime_session_incomplete")
		_ = control.CompleteAIAttempt(finalCtx, connection.provider.AttemptID, controlplane.AIAttemptStatusFailed, outcome.ErrorType)
		_ = completeRealtimeOperation(finalCtx, control, operation.ID, controlplane.AIOperationStatusFailed, outcome.ErrorType, &operationCompleted)
		_ = downstream.Close(realtimeCloseStatus(outcome.ErrorType), "realtime session terminated")
		_ = connection.upstream.Close(websocket.StatusGoingAway, "relay terminated")
	}
	trace := gatewayTraceInput(request, connection.provider, realtimeTraceStatus(outcome.Normal), http.StatusSwitchingProtocols, outcome.ErrorType,
		time.Since(startedAt).Milliseconds(), totalInputTokens, totalOutputTokens, outcome.Summary, routeAttempts)
	trace.OperationID = operation.ID
	trace.AttemptID = connection.provider.AttemptID
	trace.RequestFingerprint = operation.RequestFingerprint
	trace.MessageCount = boundedRealtimeMessageCount(outcome.Stats)
	_ = control.RecordGatewayTrace(finalCtx, auth, trace)
}

func dialRealtimeCandidates(ctx context.Context, control *controlplane.Service, operation controlplane.AIOperation, auth gatewaycore.CanonicalAuthContext, candidates []controlplane.GatewayProvider) (realtimeCandidateConnection, error) {
	result := realtimeCandidateConnection{}
	var lastErr error
	for index, candidate := range candidates {
		attempt, err := control.BeginAIAttempt(ctx, operation.ID, index+1, candidate)
		if err != nil {
			lastErr = err
			return result, err
		}
		candidate.AttemptID = attempt.ID
		result.provider = candidate
		permit, reason, acquired, err := control.TryAcquireProviderAccountPermitContext(ctx, candidate, 0, "provider_lease_"+attempt.ID)
		if err != nil {
			_ = control.CompleteAIAttempt(ctx, attempt.ID, controlplane.AIAttemptStatusFailed, "capacity_store_error")
			return result, err
		}
		if !acquired {
			_ = control.CompleteAIAttempt(ctx, attempt.ID, controlplane.AIAttemptStatusSkipped, reason)
			result.attempts = append(result.attempts, gatewayRouteAttempt{AttemptID: attempt.ID, AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "skipped", Detail: reason})
			continue
		}
		target, err := realtimeProviderURL(candidate)
		if err != nil {
			lastErr = err
			permit.Release()
			_ = control.CompleteAIAttempt(ctx, attempt.ID, controlplane.AIAttemptStatusFailed, "provider_url_error")
			result.attempts = append(result.attempts, gatewayRouteAttempt{AttemptID: attempt.ID, AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "failed", Detail: err.Error()})
			continue
		}
		dialCtx, cancel := context.WithTimeout(ctx, realtimeUpstreamHandshakeTimeout)
		upstream, response, dialErr := websocket.Dial(dialCtx, target, &websocket.DialOptions{
			HTTPClient: realtimeUpstreamHTTPClient(),
			HTTPHeader: http.Header{
				"Authorization":            []string{"Bearer " + candidate.APIKey},
				"OpenAI-Safety-Identifier": []string{realtimeSafetyIdentifier(auth)},
			},
			CompressionMode: websocket.CompressionDisabled,
		})
		cancel()
		if dialErr != nil {
			lastErr = dialErr
			permit.Release()
			statusCode := 0
			if response != nil {
				statusCode = response.StatusCode
			}
			if response != nil && response.StatusCode == http.StatusSwitchingProtocols {
				result.billingUncertain = true
			}
			if candidate.AccountID != "" && (statusCode == 0 || isProviderAccountFailureStatus(statusCode)) {
				_ = control.RecordProviderAccountFailure(ctx, candidate.AccountID, statusCode, dialErr.Error())
			}
			_ = control.CompleteAIAttempt(ctx, attempt.ID, controlplane.AIAttemptStatusFailed, "upstream_handshake_error")
			result.attempts = append(result.attempts, gatewayRouteAttempt{AttemptID: attempt.ID, AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "failed", Detail: fmt.Sprintf("websocket handshake status=%d: %v", statusCode, dialErr)})
			continue
		}
		session, created, err := control.BeginRealtimeSession(ctx, operation, attempt, candidate)
		if err != nil || !created {
			_ = upstream.Close(websocket.StatusInternalError, "session state error")
			permit.Release()
			_ = control.CompleteAIAttempt(ctx, attempt.ID, controlplane.AIAttemptStatusFailed, "session_state_error")
			result.billingUncertain = true
			if err == nil {
				err = controlplane.ErrRealtimeSessionStateConflict
			}
			return result, err
		}
		if err := control.CommitBillingHold(ctx, operation.ID, "realtime_upstream_connected"); err != nil {
			_ = upstream.Close(websocket.StatusInternalError, "billing state error")
			permit.Release()
			now := time.Now().UTC()
			_, _ = control.UpdateRealtimeSession(ctx, session.ID, session.Version, controlplane.RealtimeSessionUpdate{Status: controlplane.RealtimeSessionStatusFailed, ErrorType: "billing_hold_error", ClosedAt: &now})
			_ = control.CompleteAIAttempt(ctx, attempt.ID, controlplane.AIAttemptStatusFailed, "billing_hold_error")
			result.billingUncertain = true
			return result, err
		}
		_ = control.RecordProviderAccountSuccess(ctx, candidate.AccountID)
		_ = control.TouchProviderAccountUsage(ctx, candidate.AccountID)
		result.upstream = upstream
		result.provider = candidate
		result.providerPermit = permit
		result.session = session
		result.attempts = append(result.attempts, gatewayRouteAttempt{AttemptID: attempt.ID, AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "selected"})
		return result, nil
	}
	if lastErr != nil {
		return result, lastErr
	}
	return result, errNoSchedulableSlot
}

func realtimeProviderURL(provider controlplane.GatewayProvider) (string, error) {
	target, err := url.Parse(strings.TrimSpace(provider.BaseURL))
	if err != nil || target.Host == "" || target.User != nil {
		return "", errors.New("invalid realtime provider base URL")
	}
	switch strings.ToLower(target.Scheme) {
	case "http":
		target.Scheme = "ws"
	case "https":
		target.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", errors.New("realtime provider URL must use http, https, ws, or wss")
	}
	target.Path = strings.TrimRight(target.Path, "/") + "/realtime"
	target.RawPath = ""
	target.RawQuery = ""
	target.Fragment = ""
	query := target.Query()
	query.Set("model", provider.UpstreamModel)
	target.RawQuery = query.Encode()
	return target.String(), nil
}

func realtimeRelayConfigForRequest(request gatewaycore.CanonicalRequest) realtimeRelayConfig {
	config := defaultRealtimeRelayConfig()
	if request.AudioDurationMS > 0 {
		requested := time.Duration(request.AudioDurationMS) * time.Millisecond
		if requested < config.MaxSession {
			config.MaxSession = requested
		}
	}
	return config
}

func realtimeUpstreamHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       realtimeUpstreamHandshakeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func realtimeSafetyIdentifier(auth gatewaycore.CanonicalAuthContext) string {
	digest := sha256.Sum256([]byte(auth.ProfileScope + "\x00" + auth.TenantID + "\x00" + auth.PrincipalType + "\x00" + auth.PrincipalID))
	return "aster_" + hex.EncodeToString(digest[:16])
}

func realtimeSessionProgress(connectedAt time.Time, stats realtimeRelayStats, usageVersion int) controlplane.RealtimeSessionUpdate {
	return controlplane.RealtimeSessionUpdate{
		Status: controlplane.RealtimeSessionStatusConnected, ConnectedAt: &connectedAt,
		InputAudioBytes: stats.InputAudioBytes, OutputAudioBytes: stats.OutputAudioBytes,
		ClientMessageCount: stats.ClientMessageCount, ProviderMessageCount: stats.ProviderMessageCount,
		TransferBytes: stats.TransferBytes, UsageVersion: usageVersion,
	}
}

func realtimeSessionTerminal(connectedAt, closedAt time.Time, stats realtimeRelayStats, usageVersion int, status, errorType string) controlplane.RealtimeSessionUpdate {
	update := realtimeSessionProgress(connectedAt, stats, usageVersion)
	update.Status = status
	update.ErrorType = errorType
	update.ClosedAt = &closedAt
	return update
}

func recordRealtimeFinalUsage(ctx context.Context, control *controlplane.Service, auth controlplane.GatewayAuthContext, operation controlplane.AIOperation, provider controlplane.GatewayProvider, usageVersion int, stats realtimeRelayStats, session controlplane.RealtimeSession, source, status, errorType string) error {
	sessionDurationMS := session.SessionDurationMS
	if session.ConnectedAt != nil {
		sessionDurationMS = max(int64(0), time.Since(*session.ConnectedAt).Milliseconds())
	}
	dimensions := controlplane.UsageDimensions{
		controlplane.UsageDimensionInputBytes:                {Quantity: stats.InputAudioBytes, Unit: controlplane.UsageUnitByte, Source: "gateway_realtime", Confidence: controlplane.UsageConfidenceObserved},
		controlplane.UsageDimensionOutputBytes:               {Quantity: stats.OutputAudioBytes, Unit: controlplane.UsageUnitByte, Source: "gateway_realtime", Confidence: controlplane.UsageConfidenceObserved},
		controlplane.UsageDimensionTransferBytes:             {Quantity: stats.TransferBytes, Unit: controlplane.UsageUnitByte, Source: "gateway_realtime", Confidence: controlplane.UsageConfidenceObserved},
		controlplane.UsageDimensionSessionMilliseconds:       {Quantity: sessionDurationMS, Unit: controlplane.UsageUnitMillisecond, Source: "gateway_realtime", Confidence: controlplane.UsageConfidenceObserved},
		controlplane.UsageDimensionRealtimeAudioMilliseconds: {Quantity: sessionDurationMS, Unit: controlplane.UsageUnitMillisecond, Source: "gateway_realtime", Confidence: controlplane.UsageConfidenceObserved},
	}
	return control.RecordGatewayUsage(ctx, auth, controlplane.GatewayUsageInput{
		OperationID: operation.ID, AttemptID: provider.AttemptID, UsageVersion: usageVersion, UsageSource: source,
		RequestFingerprint: operation.RequestFingerprint, Model: operation.Model, UpstreamModel: provider.UpstreamModel,
		Protocol: operation.Protocol, ProviderID: provider.ID, ProviderAccountID: provider.AccountID,
		Status: status, ErrorType: errorType, UsageDimensions: dimensions, UsageNormalizationStatus: "realtime_session_final",
		SkipProcurementCostEstimate: true,
	})
}

func failRealtimeBeforeClientUpgrade(control *controlplane.Service, operation controlplane.AIOperation, connection realtimeCandidateConnection, errorType string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	_, _ = control.UpdateRealtimeSession(ctx, connection.session.ID, connection.session.Version, controlplane.RealtimeSessionUpdate{Status: controlplane.RealtimeSessionStatusFailed, ErrorType: errorType, ClosedAt: &now})
	_ = control.DisputeBillingHold(ctx, operation.ID, "realtime_provider_connected_without_client")
	_ = control.CompleteAIAttempt(ctx, connection.provider.AttemptID, controlplane.AIAttemptStatusFailed, errorType)
}

func completeRealtimeOperation(ctx context.Context, control *controlplane.Service, operationID, status, errorType string, completed *bool) error {
	if *completed {
		return nil
	}
	if err := control.CompleteAIOperation(ctx, operationID, status, errorType); err != nil {
		return err
	}
	*completed = true
	return nil
}

func recordRealtimeRejected(c *gin.Context, control *controlplane.Service, auth controlplane.GatewayAuthContext, request gatewaycore.CanonicalRequest, startedAt time.Time, errorType string, err error) {
	_ = control.RecordGatewayCall(c.Request.Context(), auth, request.Model, "policy_rejected", err.Error())
	_ = recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{Model: request.Model, Protocol: string(request.Protocol), Status: "error", ErrorType: errorType, LatencyMS: time.Since(startedAt).Milliseconds()})
	httpStatus := http.StatusTooManyRequests
	switch {
	case errors.Is(err, controlplane.ErrGatewayPolicyForbidden):
		httpStatus = http.StatusForbidden
	case errors.Is(err, controlplane.ErrGatewayRouteUnavailable):
		httpStatus = http.StatusServiceUnavailable
	}
	recordGatewayTrace(control, c, auth, gatewayTraceInput(request, controlplane.GatewayProvider{}, "error", httpStatus, errorType, time.Since(startedAt).Milliseconds(), 0, 0, err.Error(), ""))
}

func realtimeCloseStatus(errorType string) websocket.StatusCode {
	switch errorType {
	case "protocol_error", "message_rate_exceeded", "message_too_large", "quota_exceeded", "budget_exceeded", "risk_blocked", "credential_revoked", "policy_revoked":
		return websocket.StatusPolicyViolation
	default:
		return websocket.StatusInternalError
	}
}

func classifyRealtimeRevalidationError(err error) error {
	switch {
	case errors.Is(err, controlplane.ErrGatewayUnauthorized):
		return errors.Join(errRealtimeCredentialGone, err)
	case errors.Is(err, controlplane.ErrGatewayForbidden), errors.Is(err, controlplane.ErrGatewayPolicyForbidden):
		return errors.Join(errRealtimePolicyRevoked, err)
	case errors.Is(err, controlplane.ErrGatewayQuotaExceeded):
		return errors.Join(errRealtimeQuotaExceeded, err)
	case errors.Is(err, controlplane.ErrGatewayBudgetExceeded):
		return errors.Join(errRealtimeBudgetExceeded, err)
	case errors.Is(err, controlplane.ErrGatewayRiskBlocked):
		return errors.Join(errRealtimeRiskBlocked, err)
	default:
		return errors.Join(errRealtimeRevalidate, err)
	}
}

func realtimeTraceStatus(normal bool) string {
	if normal {
		return "forwarded"
	}
	return "upstream_error"
}

func boundedRealtimeMessageCount(stats realtimeRelayStats) int {
	total := stats.ClientMessageCount + stats.ProviderMessageCount
	if total > int64(math.MaxInt) {
		return math.MaxInt
	}
	return int(total)
}
