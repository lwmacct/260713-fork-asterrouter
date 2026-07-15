package server

import (
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

// registerGatewayMediaJobRoutes exposes protocol-friendly video/audio entry
// points. Async is the default and reuses the durable Job admission, queue,
// artifact and billing pipeline. Direct modes are recognized by the canonical
// contract, but fail closed here until a media-capable direct adapter is wired
// in; they must never create a Job.
func registerGatewayMediaJobRoutes(r *gin.Engine, control *controlplane.Service, durableJobs DurableAIJobAdmission, directAI controlplane.DirectAIProviderAdapter) {
	for _, route := range []struct {
		path      string
		modality  string
		operation string
	}{
		{path: "/v1/videos/generations", modality: controlplane.GatewayModalityVideo, operation: controlplane.GatewayOperationVideoGeneration},
		{path: "/v1/audio/generations", modality: controlplane.GatewayModalityAudio, operation: controlplane.GatewayOperationAudioGeneration},
	} {
		route := route
		r.POST(route.path, func(c *gin.Context) {
			startedAt := time.Now()
			if control == nil {
				openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
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
				openAIError(c, http.StatusBadRequest, "invalid_request_error", "invalid media generation payload")
				return
			}
			request, err := gatewaycore.CanonicalizeOpenAIMediaJob(raw, c.Request.Header, route.modality, route.operation)
			if err != nil {
				writeGatewayError(c, err)
				return
			}
			request.SourceIP = gatewaySourceIP(c.Request)
			credential, err := gatewaycore.ExtractCredential(c.Request, gatewaycore.ProtocolOpenAIMedia)
			if err != nil {
				writeGatewayError(c, controlplane.ErrGatewayUnauthorized)
				return
			}
			legacyAuth, auth, err := control.AuthorizeCanonicalGatewayRequest(c.Request.Context(), credential, request)
			if err != nil {
				writeGatewayError(c, err)
				return
			}
			if err := control.EnforceGatewayPolicy(c.Request.Context(), legacyAuth); err != nil {
				writeGatewayError(c, err)
				return
			}
			if request.Lane == gatewaycore.LaneDirect {
				if directAI != nil {
					if err := validateImageDeliveryContract(request, auth); err != nil {
						writeGatewayError(c, err)
						return
					}
					executeDirectImage(c, control, directAI, legacyAuth, auth, request)
					return
				}
				recordGatewayTrace(control, c, legacyAuth, controlplane.GatewayTraceInput{
					RequestFingerprint: request.Fingerprint, Model: request.Model, Stream: request.Stream,
					Status: "error", HTTPStatus: http.StatusServiceUnavailable, ErrorType: "unsupported_capability",
					LatencyMS:       time.Since(startedAt).Milliseconds(),
					RequestSummary:  fmt.Sprintf("media.generate modality=%s operation=%s response_mode=%s", request.Modality, request.Operation, request.ResponseMode),
					ResponseSummary: "direct media adapter is not configured; no durable job was created",
				})
				openAIError(c, http.StatusServiceUnavailable, "unsupported_capability", "no direct provider adapter is available for this media response mode")
				return
			}
			evaluation, err := evaluateDurableAIJobAdmission(c.Request.Context(), durableJobs, auth, request)
			if err != nil {
				recordDurableAIJobCapabilityRejection(control, c, legacyAuth, request, controlplane.DurableAIJobSupportEvaluation{RejectionReason: controlplane.DurableAIJobCapabilityEvaluationError}, startedAt)
				openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "media job runtime capability check failed")
				return
			}
			if !evaluation.Supported {
				recordDurableAIJobCapabilityRejection(control, c, legacyAuth, request, evaluation, startedAt)
				openAIError(c, http.StatusServiceUnavailable, "unsupported_capability", "no executable provider adapter is available for this media job")
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
		})
	}
}
