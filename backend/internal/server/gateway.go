package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	errNoSchedulableSlot        = errors.New("no schedulable provider account slot is available")
)

func registerGatewayRoutes(r *gin.Engine, control *controlplane.Service) {
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
		models, err := control.GatewayModelsForCredential(c.Request.Context(), credential.BearerToken, credential.SignedContext)
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
			recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
				Model:     req.Model,
				Status:    "error",
				ErrorType: errorType,
				LatencyMS: time.Since(startedAt).Milliseconds(),
			})
			recordGatewayTrace(control, c, auth, gatewayTraceInput(req, controlplane.GatewayProvider{}, "error", http.StatusTooManyRequests, errorType, time.Since(startedAt).Milliseconds(), 0, 0, err.Error(), ""))
			writeGatewayError(c, err)
			return
		}
		plan, err := control.PlanCanonicalGatewayRequest(c.Request.Context(), canonicalAuth, req)
		if err != nil {
			recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
				Model:     req.Model,
				Status:    "error",
				ErrorType: "provider_selection_error",
				LatencyMS: time.Since(startedAt).Milliseconds(),
			})
			recordGatewayTrace(control, c, auth, gatewayTraceInput(req, controlplane.GatewayProvider{}, "error", 0, "provider_selection_error", time.Since(startedAt).Milliseconds(), 0, 0, err.Error(), ""))
			writeGatewayError(c, err)
			return
		}
		if len(plan.Candidates) == 0 {
			routeErr := controlplane.ErrGatewayRouteUnavailable
			_ = control.RecordGatewayCall(c.Request.Context(), auth, req.Model, "policy_rejected", routeErr.Error())
			recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
				Model:     req.Model,
				Status:    "error",
				ErrorType: "route_unavailable",
				LatencyMS: time.Since(startedAt).Milliseconds(),
			})
			recordGatewayTrace(control, c, auth, gatewayTraceInput(req, controlplane.GatewayProvider{}, "error", http.StatusServiceUnavailable, "route_unavailable", time.Since(startedAt).Milliseconds(), 0, 0, routeErr.Error(), ""))
			writeGatewayError(c, routeErr)
			return
		}
		if len(plan.Candidates) > 0 {
			candidates := control.PreferStickyGatewayCandidate(canonicalAuth.CredentialID, req.Model, string(req.Protocol), req.StickyKey, plan.Candidates)
			resp, provider, release, attempts, attemptErr := attemptGatewayCandidates(c, control, candidates, req.Payload, req.Stream)
			routeAttempts := marshalRouteAttempts(attempts)
			if resp == nil {
				if attemptErr == nil {
					attemptErr = errNoSchedulableSlot
				}
				_ = control.RecordGatewayCall(c.Request.Context(), auth, req.Model, "upstream_error", attemptErr.Error())
				recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
					Model:             req.Model,
					UpstreamModel:     provider.UpstreamModel,
					ProviderID:        provider.ID,
					ProviderAccountID: provider.AccountID,
					Status:            "upstream_error",
					ErrorType:         "transport_error",
					LatencyMS:         time.Since(startedAt).Milliseconds(),
				})
				recordGatewayTrace(control, c, auth, gatewayTraceInput(req, provider, "upstream_error", 0, "transport_error", time.Since(startedAt).Milliseconds(), 0, 0, attemptErr.Error(), routeAttempts))
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
				control.BindStickyGatewayCandidate(canonicalAuth.CredentialID, req.Model, string(req.Protocol), req.StickyKey, provider)
			}
			summary := gatewayRouteSummary(req.Model, provider)
			if req.Stream {
				if err := control.RecordGatewayCall(c.Request.Context(), auth, req.Model, status, summary); err != nil {
					openAIError(c, http.StatusInternalServerError, "server_error", err.Error())
					return
				}
				streamErr := streamUpstreamResponse(c, resp)
				errorType := ""
				usageStatus := status
				if streamErr != nil {
					errorType = "stream_error"
					usageStatus = "upstream_error"
				}
				responseSummary := "stream completed"
				if streamErr != nil {
					responseSummary = streamErr.Error()
				}
				recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
					Model:             req.Model,
					UpstreamModel:     provider.UpstreamModel,
					ProviderID:        provider.ID,
					ProviderAccountID: provider.AccountID,
					Status:            usageStatus,
					ErrorType:         errorType,
					LatencyMS:         time.Since(startedAt).Milliseconds(),
				})
				recordGatewayTrace(control, c, auth, gatewayTraceInput(req, provider, usageStatus, resp.StatusCode, errorType, time.Since(startedAt).Milliseconds(), 0, 0, responseSummary, routeAttempts))
				if streamErr != nil && !c.Writer.Written() {
					openAIError(c, http.StatusBadGateway, "upstream_error", streamErr.Error())
				}
				return
			}

			contentType, upstreamBody, err := readUpstreamResponse(resp)
			if err != nil {
				_ = control.RecordGatewayCall(c.Request.Context(), auth, req.Model, "upstream_error", err.Error())
				recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
					Model:             req.Model,
					UpstreamModel:     provider.UpstreamModel,
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
				openAIError(c, http.StatusInternalServerError, "server_error", err.Error())
				return
			}
			inputTokens, outputTokens := parseUpstreamUsage(upstreamBody)
			errorType := ""
			if status == "upstream_error" {
				errorType = "upstream_status"
			}
			recordGatewayUsage(control, c, auth, controlplane.GatewayUsageInput{
				Model:             req.Model,
				UpstreamModel:     provider.UpstreamModel,
				ProviderID:        provider.ID,
				ProviderAccountID: provider.AccountID,
				Status:            status,
				ErrorType:         errorType,
				LatencyMS:         time.Since(startedAt).Milliseconds(),
				InputTokens:       inputTokens,
				OutputTokens:      outputTokens,
			})
			recordGatewayTrace(control, c, auth, gatewayTraceInput(req, provider, status, resp.StatusCode, errorType, time.Since(startedAt).Milliseconds(), inputTokens, outputTokens, upstreamResponseSummary(resp.StatusCode, upstreamBody), routeAttempts))
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
	return gatewaycore.CanonicalizeOpenAIChat(rawBody, c.Request.Header)
}

// gatewayRouteAttempt records what happened when the gateway tried a single
// candidate route while resolving a chat completion request. It is
// serialized into GatewayTrace.RouteAttempts so operators can see which
// candidates were skipped or failed, and why, without needing verbose logs.
type gatewayRouteAttempt struct {
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
// being attempted if its concurrency slot is exhausted. A candidate that
// fails at the transport level, or that is not the last candidate and
// returns an account-side failure status, is recorded as a failure (cooling
// the underlying provider account down) and the loop moves to the next
// candidate. The last candidate's response is always accepted as-is, even on
// a failure status, matching the existing behavior of passing upstream error
// responses through to the caller when no better alternative exists.
//
// On success, the returned release func must be called by the caller once
// the response body has been fully consumed (streamed or read). Losing
// candidates' slots are released internally and must not be released again.
func attemptGatewayCandidates(c *gin.Context, control *controlplane.Service, candidates []controlplane.GatewayProvider, rawBody []byte, stream bool) (resp *http.Response, provider controlplane.GatewayProvider, release func(), attempts []gatewayRouteAttempt, transportErr error) {
	estimatedTokens := estimateGatewayRequestTokens(rawBody)
	for i, candidate := range candidates {
		slotRelease, acquired := control.TryAcquireProviderAccountSlot(candidate.AccountID, candidate.Concurrency)
		if !acquired {
			attempts = append(attempts, gatewayRouteAttempt{AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "skipped", Detail: "at_capacity"})
			continue
		}
		permit, reason, permitted := control.TryAcquireProviderAccountPermit(candidate, estimatedTokens)
		if !permitted {
			slotRelease()
			attempts = append(attempts, gatewayRouteAttempt{AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "skipped", Detail: reason})
			continue
		}
		candidateResp, err := forwardChatCompletion(c, candidate, rawBody, stream)
		if err != nil {
			permit.Release()
			slotRelease()
			if candidate.AccountID != "" {
				_ = control.RecordProviderAccountFailure(c.Request.Context(), candidate.AccountID, 0, err.Error())
			}
			attempts = append(attempts, gatewayRouteAttempt{AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "failed", Detail: err.Error()})
			transportErr = err
			continue
		}
		isLast := i == len(candidates)-1
		if !isLast && isProviderAccountFailureStatus(candidateResp.StatusCode) {
			bodyPreview, _ := io.ReadAll(io.LimitReader(candidateResp.Body, failureBodyPreviewLimit))
			_ = candidateResp.Body.Close()
			permit.Release()
			slotRelease()
			if candidate.AccountID != "" {
				_ = control.RecordProviderAccountFailure(c.Request.Context(), candidate.AccountID, candidateResp.StatusCode, string(bodyPreview))
			}
			detail := fmt.Sprintf("upstream http status %d", candidateResp.StatusCode)
			attempts = append(attempts, gatewayRouteAttempt{AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "failed", Detail: detail})
			transportErr = errors.New(detail)
			continue
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
		attempts = append(attempts, gatewayRouteAttempt{AccountID: candidate.AccountID, ProviderID: candidate.ID, RouteID: candidate.RouteID, RouteGroup: candidate.RouteGroup, Model: candidate.UpstreamModel, Outcome: "selected"})
		return candidateResp, candidate, func() { permit.Release(); slotRelease() }, attempts, nil
	}
	return nil, controlplane.GatewayProvider{}, nil, attempts, transportErr
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

func forwardChatCompletion(c *gin.Context, provider controlplane.GatewayProvider, rawBody []byte, stream bool) (*http.Response, error) {
	upstreamBody, err := rewriteGatewayModel(rawBody, provider.UpstreamModel)
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
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	return gatewayHTTPClient(stream).Do(req)
}

func rewriteGatewayModel(rawBody []byte, upstreamModel string) ([]byte, error) {
	upstreamModel = strings.TrimSpace(upstreamModel)
	if upstreamModel == "" {
		return nil, errors.New("model route upstream_model is empty")
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return nil, err
	}
	payload["model"] = upstreamModel
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

func readUpstreamResponse(resp *http.Response) (string, []byte, error) {
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, gatewayUpstreamBodyLimit+1))
	if err != nil {
		return "", nil, err
	}
	if len(body) > gatewayUpstreamBodyLimit {
		return "", nil, errUpstreamResponseTooLarge
	}
	return contentType, body, nil
}

func parseUpstreamUsage(body []byte) (int, int) {
	var payload struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, 0
	}
	input := payload.Usage.PromptTokens
	if input == 0 {
		input = payload.Usage.InputTokens
	}
	output := payload.Usage.CompletionTokens
	if output == 0 {
		output = payload.Usage.OutputTokens
	}
	return input, output
}

func recordGatewayUsage(control *controlplane.Service, c *gin.Context, auth controlplane.GatewayAuthContext, input controlplane.GatewayUsageInput) {
	if control != nil {
		_ = control.RecordGatewayUsage(c.Request.Context(), auth, input)
	}
}

func recordGatewayTrace(control *controlplane.Service, c *gin.Context, auth controlplane.GatewayAuthContext, input controlplane.GatewayTraceInput) {
	if control != nil {
		_ = control.RecordGatewayTrace(c.Request.Context(), auth, input)
	}
}

func streamUpstreamResponse(c *gin.Context, resp *http.Response) error {
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/event-stream"
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(resp.StatusCode)

	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := c.Writer.Write(buf[:n]); err != nil {
				return err
			}
			c.Writer.Flush()
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
