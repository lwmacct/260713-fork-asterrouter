package server

import (
	"net/http"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerAlertAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/alerts", func(c *gin.Context) {
		query, err := scopeAlertQuery(c.Request.Context(), control, principalAccess(c), alertQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1112, err.Error())
			return
		}
		data, err := control.ListAlertEventsQuery(c.Request.Context(), query)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1112, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/alerts/summary", func(c *gin.Context) {
		query, err := scopeAlertQuery(c.Request.Context(), control, principalAccess(c), alertQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1112, err.Error())
			return
		}
		data, err := control.AlertSummaryQuery(c.Request.Context(), query)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1112, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/alerts/:id/acknowledge", func(c *gin.Context) {
		if err := requireAlertInAccess(c.Request.Context(), control, c.Param("id"), principalAccess(c)); err != nil {
			httpx.Error(c, http.StatusForbidden, 1451, err.Error())
			return
		}
		data, err := control.AcknowledgeAlert(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1520, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/alerts/:id/resolve", func(c *gin.Context) {
		if err := requireAlertInAccess(c.Request.Context(), control, c.Param("id"), principalAccess(c)); err != nil {
			httpx.Error(c, http.StatusForbidden, 1451, err.Error())
			return
		}
		data, err := control.ResolveAlert(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1521, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}

func alertQuery(c *gin.Context) controlplane.AlertQuery {
	return controlplane.AlertQuery{
		Limit:        intQuery(c, "limit", 50),
		Offset:       intQuery(c, "offset", 0),
		Search:       strings.TrimSpace(c.Query("q")),
		Type:         strings.TrimSpace(c.Query("type")),
		Severity:     strings.TrimSpace(c.Query("severity")),
		Status:       strings.TrimSpace(c.Query("status")),
		ResourceType: strings.TrimSpace(c.Query("resource_type")),
		CreatedFrom:  timeQuery(c, "from"),
		CreatedTo:    timeQuery(c, "to"),
	}
}
