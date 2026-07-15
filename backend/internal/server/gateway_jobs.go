package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/gin-gonic/gin"
)

type publicAIJobResponse struct {
	ID             string                `json:"id"`
	Object         string                `json:"object"`
	OperationID    string                `json:"operation_id"`
	Status         string                `json:"status"`
	StatusVersion  int                   `json:"status_version"`
	Capability     publicAIJobCapability `json:"capability"`
	ArtifactPolicy string                `json:"artifact_policy"`
	ArtifactSinkID string                `json:"artifact_sink_id,omitempty"`
	ErrorType      string                `json:"error_type,omitempty"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
	CompletedAt    *time.Time            `json:"completed_at,omitempty"`
	ExpiresAt      time.Time             `json:"expires_at"`
	Links          map[string]string     `json:"links"`
}

type publicAIJobCapability struct {
	Modality  string `json:"modality"`
	Operation string `json:"operation"`
	Model     string `json:"model"`
}

type DurableAIJobAdmission interface {
	SupportsDurableAIJob(context.Context, gatewaycore.CanonicalAuthContext, gatewaycore.CanonicalRequest) (bool, error)
}

type DurableAIJobAdmissionEvaluator interface {
	EvaluateDurableAIJobSupport(context.Context, gatewaycore.CanonicalAuthContext, gatewaycore.CanonicalRequest) (controlplane.DurableAIJobSupportEvaluation, error)
}

func evaluateDurableAIJobAdmission(ctx context.Context, admission DurableAIJobAdmission, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) (controlplane.DurableAIJobSupportEvaluation, error) {
	if admission == nil {
		return controlplane.DurableAIJobSupportEvaluation{
			RejectionReason: controlplane.DurableAIJobCapabilityRuntimeUnavailable,
			Exclusions:      []controlplane.GatewayCandidateExclusion{{Reason: controlplane.DurableAIJobCapabilityRuntimeUnavailable}},
		}, nil
	}
	if evaluator, ok := admission.(DurableAIJobAdmissionEvaluator); ok {
		evaluation, err := evaluator.EvaluateDurableAIJobSupport(ctx, auth, request)
		if err == nil && !evaluation.Supported && evaluation.RejectionReason == "" {
			evaluation.RejectionReason = controlplane.DurableAIJobCapabilityAdapterUnsupported
			evaluation.Exclusions = append(evaluation.Exclusions, controlplane.GatewayCandidateExclusion{Reason: evaluation.RejectionReason})
		}
		return evaluation, err
	}
	supported, err := admission.SupportsDurableAIJob(ctx, auth, request)
	evaluation := controlplane.DurableAIJobSupportEvaluation{Supported: supported}
	if !supported && err == nil {
		evaluation.RejectionReason = controlplane.DurableAIJobCapabilityAdapterUnsupported
		evaluation.Exclusions = []controlplane.GatewayCandidateExclusion{{Reason: evaluation.RejectionReason}}
	}
	return evaluation, err
}

func recordDurableAIJobCapabilityRejection(control *controlplane.Service, c *gin.Context, auth controlplane.GatewayAuthContext, request gatewaycore.CanonicalRequest, evaluation controlplane.DurableAIJobSupportEvaluation, startedAt time.Time) {
	reason := evaluation.RejectionReason
	if reason == "" {
		reason = controlplane.DurableAIJobCapabilityEvaluationError
	}
	exclusions := evaluation.Exclusions
	if len(exclusions) == 0 {
		exclusions = []controlplane.GatewayCandidateExclusion{{Reason: reason}}
	}
	errorType := "unsupported_capability"
	if reason == controlplane.DurableAIJobCapabilityEvaluationError {
		errorType = controlplane.DurableAIJobCapabilityEvaluationError
	}
	recordGatewayTrace(control, c, auth, controlplane.GatewayTraceInput{
		RequestFingerprint: request.Fingerprint, Model: request.Model, Stream: request.Stream,
		GatewayModelID: evaluation.GatewayModelID, RouteGroup: evaluation.RouteGroup, RouteReason: reason,
		Status: "error", HTTPStatus: http.StatusServiceUnavailable, ErrorType: errorType,
		LatencyMS:       time.Since(startedAt).Milliseconds(),
		RequestSummary:  fmt.Sprintf("durable job modality=%s operation=%s", request.Modality, request.Operation),
		ResponseSummary: "durable capability admission rejected", RouteAttempts: marshalRouteEvidence(exclusions, nil),
	})
}

func registerGatewayJobRoutes(r *gin.Engine, control *controlplane.Service, durableJobs DurableAIJobAdmission) {
	registerGatewayJobEventRoute(r, control)
	registerGatewayArtifactRoutes(r, control)

	r.POST("/v1/jobs", func(c *gin.Context) {
		startedAt := time.Now()
		if control == nil {
			openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
			return
		}
		request, err := parseCanonicalDurableJobRequest(c)
		if err != nil {
			if errors.Is(err, errGatewayRequestTooLarge) {
				openAIError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body exceeds 16 MiB limit")
				return
			}
			openAIError(c, http.StatusBadRequest, "invalid_request_error", "invalid durable job payload")
			return
		}
		credential, err := gatewaycore.ExtractCredential(c.Request, gatewaycore.ProtocolAsterJobs)
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
		evaluation, err := evaluateDurableAIJobAdmission(c.Request.Context(), durableJobs, canonicalAuth, request)
		if err != nil {
			recordDurableAIJobCapabilityRejection(control, c, auth, request, controlplane.DurableAIJobSupportEvaluation{RejectionReason: controlplane.DurableAIJobCapabilityEvaluationError}, startedAt)
			openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "durable job runtime capability check failed")
			return
		}
		if !evaluation.Supported {
			recordDurableAIJobCapabilityRejection(control, c, auth, request, evaluation, startedAt)
			openAIError(c, http.StatusServiceUnavailable, "unsupported_capability", "no executable provider adapter is available for this durable job")
			return
		}
		job, created, err := control.BeginDurableAIJob(c.Request.Context(), canonicalAuth, request)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		c.Header("Location", "/v1/jobs/"+job.ID)
		c.Header("X-AsterRouter-Operation-ID", job.OperationID)
		status := http.StatusAccepted
		if !created {
			c.Header("Idempotent-Replayed", "true")
			status = http.StatusOK
		}
		if !aiJobPublicTerminal(job.Status) {
			c.Header("Retry-After", strconv.Itoa(controlplane.AIJobDefaultPollAfter))
		}
		c.JSON(status, newPublicAIJobResponse(job))
	})

	r.GET("/v1/jobs/:job_id", func(c *gin.Context) {
		if control == nil {
			openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
			return
		}
		auth, ok := authorizePublicAIJobAction(c, control, controlplane.GatewayScopeJobsRead)
		if !ok {
			return
		}
		job, found, err := control.AIJobForAuth(c.Request.Context(), auth, c.Param("job_id"))
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		if !found {
			openAIError(c, http.StatusNotFound, "resource_not_found", "ai job not found")
			return
		}
		c.JSON(http.StatusOK, newPublicAIJobResponse(job))
	})

	r.POST("/v1/jobs/:job_id/cancel", func(c *gin.Context) {
		if control == nil {
			openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
			return
		}
		auth, ok := authorizePublicAIJobAction(c, control, controlplane.GatewayScopeJobsCancel)
		if !ok {
			return
		}
		job, found, err := control.CancelAIJobForAuth(c.Request.Context(), auth, c.Param("job_id"))
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		if !found {
			openAIError(c, http.StatusNotFound, "resource_not_found", "ai job not found")
			return
		}
		c.JSON(http.StatusOK, newPublicAIJobResponse(job))
	})

	r.POST("/v1/jobs/:job_id/actions", func(c *gin.Context) {
		startedAt := time.Now()
		if control == nil {
			openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
			return
		}
		credential, err := gatewaycore.ExtractCredential(c.Request, gatewaycore.ProtocolAsterJobs)
		if err != nil {
			writeGatewayError(c, controlplane.ErrGatewayUnauthorized)
			return
		}
		actionAuth, err := control.AuthorizeGatewayCredentialScope(c.Request.Context(), credential, gatewaySourceIP(c.Request), controlplane.GatewayScopeJobsActions)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		sourceJob, found, err := control.AIJobForAuth(c.Request.Context(), actionAuth, c.Param("job_id"))
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		if !found {
			openAIError(c, http.StatusNotFound, "resource_not_found", "ai job not found")
			return
		}
		if sourceJob.Status != controlplane.AIJobStatusSucceeded && sourceJob.Status != controlplane.AIJobStatusFailed && sourceJob.Status != controlplane.AIJobStatusUnknown {
			openAIError(c, http.StatusConflict, "job_action_conflict", "job actions require a terminal source job")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, gatewayRequestBodyLimit)
		raw, err := io.ReadAll(c.Request.Body)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				openAIError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body exceeds 16 MiB limit")
				return
			}
			openAIError(c, http.StatusBadRequest, "invalid_request_error", "invalid job action payload")
			return
		}
		request, err := gatewaycore.CanonicalizeAIJobAction(raw, c.Request.Header, sourceJob.ID, sourceJob.Model, sourceJob.Operation, sourceJob.Modality)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		request.SourceIP = gatewaySourceIP(c.Request)
		legacyAuth, auth, err := control.AuthorizeCanonicalGatewayRequest(c.Request.Context(), credential, request)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		if _, sourceStillOwned, ownerErr := control.AIJobForAuth(c.Request.Context(), auth, sourceJob.ID); ownerErr != nil {
			writeGatewayError(c, ownerErr)
			return
		} else if !sourceStillOwned {
			openAIError(c, http.StatusNotFound, "resource_not_found", "ai job not found")
			return
		}
		if err := control.EnforceGatewayPolicy(c.Request.Context(), legacyAuth); err != nil {
			writeGatewayError(c, err)
			return
		}
		evaluation, err := evaluateDurableAIJobAdmission(c.Request.Context(), durableJobs, auth, request)
		if err != nil {
			recordDurableAIJobCapabilityRejection(control, c, legacyAuth, request, controlplane.DurableAIJobSupportEvaluation{RejectionReason: controlplane.DurableAIJobCapabilityEvaluationError}, startedAt)
			openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "job action runtime capability check failed")
			return
		}
		if !evaluation.Supported {
			recordDurableAIJobCapabilityRejection(control, c, legacyAuth, request, evaluation, startedAt)
			openAIError(c, http.StatusServiceUnavailable, "unsupported_capability", "no executable provider adapter is available for this job action")
			return
		}
		job, created, err := control.BeginDurableAIJob(c.Request.Context(), auth, request)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		c.Header("Location", "/v1/jobs/"+job.ID)
		c.Header("X-AsterRouter-Operation-ID", job.OperationID)
		if !created {
			c.Header("Idempotent-Replayed", "true")
		}
		if !aiJobPublicTerminal(job.Status) {
			c.Header("Retry-After", strconv.Itoa(controlplane.AIJobDefaultPollAfter))
		}
		status := http.StatusAccepted
		if !created {
			status = http.StatusOK
		}
		c.JSON(status, newPublicAIJobResponse(job))
	})
}

func parseCanonicalDurableJobRequest(c *gin.Context) (gatewaycore.CanonicalRequest, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, gatewayRequestBodyLimit)
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return gatewaycore.CanonicalRequest{}, errGatewayRequestTooLarge
		}
		return gatewaycore.CanonicalRequest{}, err
	}
	request, err := gatewaycore.CanonicalizeDurableJob(rawBody, c.Request.Header)
	if err != nil {
		return gatewaycore.CanonicalRequest{}, err
	}
	request.SourceIP = gatewaySourceIP(c.Request)
	return request, nil
}

func authorizePublicAIJobAction(c *gin.Context, control *controlplane.Service, scope string) (gatewaycore.CanonicalAuthContext, bool) {
	credential, err := gatewaycore.ExtractCredential(c.Request, gatewaycore.ProtocolAsterJobs)
	if err != nil {
		writeGatewayError(c, controlplane.ErrGatewayUnauthorized)
		return gatewaycore.CanonicalAuthContext{}, false
	}
	auth, err := control.AuthorizeGatewayCredentialScope(c.Request.Context(), credential, gatewaySourceIP(c.Request), scope)
	if err != nil {
		writeGatewayError(c, err)
		return gatewaycore.CanonicalAuthContext{}, false
	}
	return auth, true
}

func newPublicAIJobResponse(job controlplane.AIJob) publicAIJobResponse {
	return publicAIJobResponse{
		ID: job.ID, Object: "ai_job", OperationID: job.OperationID, Status: job.Status, StatusVersion: job.StatusVersion,
		Capability:     publicAIJobCapability{Modality: job.Modality, Operation: job.Operation, Model: job.Model},
		ArtifactPolicy: job.ArtifactPolicy, ArtifactSinkID: job.ArtifactSinkID, ErrorType: job.ErrorType, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
		CompletedAt: job.CompletedAt, ExpiresAt: job.ExpiresAt,
		Links: map[string]string{"self": "/v1/jobs/" + job.ID, "events": "/v1/jobs/" + job.ID + "/events", "artifacts": "/v1/jobs/" + job.ID + "/artifacts"},
	}
}

func aiJobPublicTerminal(status string) bool {
	return status == controlplane.AIJobStatusSucceeded || status == controlplane.AIJobStatusFailed || status == controlplane.AIJobStatusCanceled || status == controlplane.AIJobStatusExpired
}
