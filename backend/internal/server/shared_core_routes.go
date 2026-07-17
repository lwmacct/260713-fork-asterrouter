package server

import (
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerSharedCoreRoutes(group *gin.RouterGroup, control *controlplane.Service, includeUsage bool) {
	if control == nil {
		return
	}
	registerProviderAdminRoutes(group, control)
	registerRoutingAdminRoutes(group, control)
	registerGatewayModelAdminRoutes(group, control)
	registerAPIKeyAdminRoutes(group, control)
	if includeUsage {
		group.GET("/usage", func(c *gin.Context) {
			data, err := control.UsageReportQuery(c.Request.Context(), usageQuery(c))
			sharedCoreResponse(c, data, err)
		})
	}
	group.GET("/gateway-traces", func(c *gin.Context) {
		data, err := control.ListGatewayTracesQuery(c.Request.Context(), gatewayTraceQuery(c))
		sharedCoreResponse(c, data, err)
	})
}

func sharedCoreResponse(c *gin.Context, data any, err error) {
	if err != nil {
		httpx.Error(c, http.StatusBadRequest, 1521, err.Error())
		return
	}
	httpx.OK(c, data)
}
