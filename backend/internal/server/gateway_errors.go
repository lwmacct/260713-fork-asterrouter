package server

import (
	"errors"
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/gin-gonic/gin"
)

func writeGatewayError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, controlplane.ErrGatewayUnauthorized):
		openAIError(c, http.StatusUnauthorized, "invalid_api_key", "invalid or missing gateway api key")
	case errors.Is(err, controlplane.ErrGatewayForbidden):
		openAIError(c, http.StatusForbidden, "model_not_allowed", "gateway api key is not allowed to use this model")
	case errors.Is(err, controlplane.ErrGatewayRouteUnavailable):
		openAIError(c, http.StatusServiceUnavailable, "route_unavailable", "no schedulable provider account is available for this model")
	case errors.Is(err, controlplane.ErrGatewayRateLimited):
		openAIError(c, http.StatusTooManyRequests, "rate_limit_exceeded", "gateway api key qps limit exceeded")
	case errors.Is(err, controlplane.ErrGatewayQuotaExceeded):
		openAIError(c, http.StatusTooManyRequests, "insufficient_quota", "gateway api key monthly token quota exceeded")
	case errors.Is(err, controlplane.ErrGatewayBudgetExceeded):
		openAIError(c, http.StatusTooManyRequests, "insufficient_quota", "workspace key monthly budget exceeded")
	case errors.Is(err, controlplane.ErrGatewayRiskBlocked):
		openAIError(c, http.StatusTooManyRequests, "risk_control_blocked", "gateway api key is temporarily blocked by risk control")
	default:
		openAIError(c, http.StatusInternalServerError, "server_error", err.Error())
	}
}

func openAIError(c *gin.Context, status int, errorType string, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errorType,
		},
	})
}
