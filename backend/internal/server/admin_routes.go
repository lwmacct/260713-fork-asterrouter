package server

import (
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service, exportJobs CSVExportJobStore) {
	if control == nil {
		return
	}
	registerDashboardAdminRoutes(admin, control)
	registerProviderAdminRoutes(admin, control)
	registerIdentityAdminRoutes(admin, control)
	registerDepartmentAdminRoutes(admin, control)
	registerGovernancePolicyAdminRoutes(admin, control)
	registerRoutingAdminRoutes(admin, control)
	registerGatewayModelAdminRoutes(admin, control)
	registerAPIKeyAdminRoutes(admin, control)
	registerModelPricingAdminRoutes(admin, control)
	registerObservabilityAdminRoutes(admin, control)
	registerAlertAdminRoutes(admin, control)
	registerCSVExportJobRoutes(admin.Group("/export-jobs"), control, exportJobs)
}

func registerGatewayModelAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.POST("/gateway-simulator", func(c *gin.Context) {
		var req controlplane.GatewaySimulationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1519, "invalid gateway simulation payload")
			return
		}
		data, err := control.SimulateGatewayRouting(c.Request.Context(), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1520, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/gateway-models", func(c *gin.Context) {
		data, err := control.ListGatewayModels(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1112, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/gateway-models", func(c *gin.Context) {
		var req controlplane.GatewayModelRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1515, "invalid gateway model payload")
			return
		}
		data, err := control.CreateGatewayModel(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1516, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/gateway-models/:id", func(c *gin.Context) {
		var req controlplane.GatewayModelRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1515, "invalid gateway model payload")
			return
		}
		data, err := control.UpdateGatewayModel(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1516, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.DELETE("/gateway-models/:id", func(c *gin.Context) {
		if err := control.DeleteGatewayModel(c.Request.Context(), actor(c), c.Param("id")); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1516, err.Error())
			return
		}
		httpx.OK(c, gin.H{"status": "deleted"})
	})

	admin.GET("/model-routes", func(c *gin.Context) {
		data, err := control.ListModelRoutes(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1113, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/model-routes", func(c *gin.Context) {
		var req controlplane.ModelRouteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1517, "invalid model route payload")
			return
		}
		data, err := control.CreateModelRoute(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1518, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/model-routes/:id", func(c *gin.Context) {
		var req controlplane.ModelRouteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1517, "invalid model route payload")
			return
		}
		data, err := control.UpdateModelRoute(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1518, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.DELETE("/model-routes/:id", func(c *gin.Context) {
		if err := control.DeleteModelRoute(c.Request.Context(), actor(c), c.Param("id")); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1518, err.Error())
			return
		}
		httpx.OK(c, gin.H{"status": "deleted"})
	})
}

func registerDashboardAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/dashboard", func(c *gin.Context) {
		data, err := control.Dashboard(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1100, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}

func registerProviderAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/providers", func(c *gin.Context) {
		data, err := control.ListProviders(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1101, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/provider-health-checks", func(c *gin.Context) {
		data, err := control.ListProviderHealthChecks(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1110, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/providers", func(c *gin.Context) {
		var req controlplane.ProviderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1500, "invalid provider payload")
			return
		}
		data, err := control.CreateProvider(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1501, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/providers/:id", func(c *gin.Context) {
		var req controlplane.ProviderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1500, "invalid provider payload")
			return
		}
		data, err := control.UpdateProvider(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1501, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/providers/:id/check", func(c *gin.Context) {
		data, err := control.CheckProvider(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1501, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}

func registerRoutingAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/routing-groups", func(c *gin.Context) {
		data, err := control.ListRoutingGroups(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1108, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/routing-groups", func(c *gin.Context) {
		var req controlplane.RoutingGroupRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1510, "invalid routing group payload")
			return
		}
		data, err := control.CreateRoutingGroup(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1511, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/routing-groups/:id", func(c *gin.Context) {
		var req controlplane.RoutingGroupRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1510, "invalid routing group payload")
			return
		}
		data, err := control.UpdateRoutingGroup(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1511, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/provider-accounts", func(c *gin.Context) {
		data, err := control.ListProviderAccounts(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1109, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/provider-account-health-checks", func(c *gin.Context) {
		data, err := control.ListProviderAccountHealthChecks(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1111, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/provider-accounts", func(c *gin.Context) {
		var req controlplane.ProviderAccountRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1512, "invalid provider account payload")
			return
		}
		data, err := control.CreateProviderAccount(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1513, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/provider-accounts/:id", func(c *gin.Context) {
		var req controlplane.ProviderAccountRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1512, "invalid provider account payload")
			return
		}
		data, err := control.UpdateProviderAccount(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1513, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/provider-accounts/:id/check", func(c *gin.Context) {
		data, err := control.CheckProviderAccount(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1513, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/provider-accounts/:id/clear-cooldown", func(c *gin.Context) {
		data, err := control.ClearProviderAccountCooldown(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1514, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}

func registerAPIKeyAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/api-keys", func(c *gin.Context) {
		data, err := control.ListAPIKeys(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1105, err.Error())
			return
		}
		users, err := control.ListWorkspaceUsers(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1105, err.Error())
			return
		}
		httpx.OK(c, filterAPIKeysForAccess(data, users, principalAccess(c)))
	})
	admin.GET("/api-keys/:id/policy-explanation", func(c *gin.Context) {
		if err := requireAPIKeyInAccess(c.Request.Context(), control, c.Param("id"), principalAccess(c)); err != nil {
			httpx.Error(c, http.StatusForbidden, 1451, err.Error())
			return
		}
		data, err := control.ExplainGatewayPolicyForAPIKey(c.Request.Context(), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1507, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/api-keys", func(c *gin.Context) {
		var req controlplane.APIKeyCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1506, "invalid api key payload")
			return
		}
		if access := principalAccess(c); !access.Global && len(access.DepartmentIDs) > 0 {
			if req.KeyType != controlplane.APIKeyTypeUser || req.OwnerUserID == "" {
				httpx.Error(c, http.StatusForbidden, 1451, "department-scoped administrators can only create owned user keys")
				return
			}
			if err := requireUserInAccess(c.Request.Context(), control, req.OwnerUserID, access); err != nil {
				httpx.Error(c, http.StatusForbidden, 1451, err.Error())
				return
			}
		}
		data, err := control.CreateAPIKey(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1507, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/api-keys/:id", func(c *gin.Context) {
		if err := requireAPIKeyInAccess(c.Request.Context(), control, c.Param("id"), principalAccess(c)); err != nil {
			httpx.Error(c, http.StatusForbidden, 1451, err.Error())
			return
		}
		var req controlplane.APIKeyUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1506, "invalid api key payload")
			return
		}
		data, err := control.UpdateAPIKey(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1507, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/api-keys/:id/rotate", func(c *gin.Context) {
		if err := requireAPIKeyInAccess(c.Request.Context(), control, c.Param("id"), principalAccess(c)); err != nil {
			httpx.Error(c, http.StatusForbidden, 1451, err.Error())
			return
		}
		data, err := control.RotateAPIKey(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1507, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/api-keys/:id/disable", func(c *gin.Context) {
		if err := requireAPIKeyInAccess(c.Request.Context(), control, c.Param("id"), principalAccess(c)); err != nil {
			httpx.Error(c, http.StatusForbidden, 1451, err.Error())
			return
		}
		if err := control.DisableAPIKey(c.Request.Context(), actor(c), c.Param("id")); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1508, err.Error())
			return
		}
		httpx.OK(c, gin.H{"status": "disabled"})
	})
}

func registerObservabilityAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/audit-logs", func(c *gin.Context) {
		data, err := control.ListAuditLogsQuery(c.Request.Context(), auditLogQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1106, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/audit-logs/summary", func(c *gin.Context) {
		data, err := control.AuditLogSummaryQuery(c.Request.Context(), auditLogQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1106, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/audit-logs/export", func(c *gin.Context) {
		data, err := collectAuditLogsForExport(c, control)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1106, err.Error())
			return
		}
		writeCSV(c, "audit-logs.csv", auditLogCSVRows(data))
	})
	admin.GET("/usage", func(c *gin.Context) {
		query, err := scopeUsageQuery(c.Request.Context(), control, principalAccess(c), usageQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1107, err.Error())
			return
		}
		data, err := control.UsageReportQuery(c.Request.Context(), query)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1107, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/usage/export", func(c *gin.Context) {
		query, err := scopeUsageQuery(c.Request.Context(), control, principalAccess(c), usageQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1107, err.Error())
			return
		}
		totalLimit, baseOffset := exportWindow(c)
		data, err := collectUsageRecordsForExportQuery(c.Request.Context(), control, query, totalLimit, baseOffset)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1107, err.Error())
			return
		}
		writeCSV(c, "usage-records.csv", usageCSVRows(data))
	})
	admin.GET("/cost-allocation", func(c *gin.Context) {
		query, err := scopeUsageQuery(c.Request.Context(), control, principalAccess(c), usageQuery(c))
		if err != nil {
			writeCostAllocationError(c, err)
			return
		}
		data, err := control.CostAllocationReportQuery(c.Request.Context(), c.Query("dimension"), query)
		if err != nil {
			writeCostAllocationError(c, err)
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/cost-allocation/export", func(c *gin.Context) {
		query, err := scopeUsageQuery(c.Request.Context(), control, principalAccess(c), usageQuery(c))
		if err != nil {
			writeCostAllocationError(c, err)
			return
		}
		query.Limit, query.Offset = exportWindow(c)
		data, err := control.CostAllocationReportQuery(c.Request.Context(), c.Query("dimension"), query)
		if err != nil {
			writeCostAllocationError(c, err)
			return
		}
		writeCSV(c, "cost-allocation.csv", costAllocationCSVRows(data))
	})
	admin.GET("/gateway-traces", func(c *gin.Context) {
		query, err := scopeGatewayTraceQuery(c.Request.Context(), control, principalAccess(c), gatewayTraceQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1109, err.Error())
			return
		}
		data, err := control.ListGatewayTracesQuery(c.Request.Context(), query)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1109, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/gateway-traces/summary", func(c *gin.Context) {
		query, err := scopeGatewayTraceQuery(c.Request.Context(), control, principalAccess(c), gatewayTraceQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1109, err.Error())
			return
		}
		data, err := control.GatewayTraceSummaryQuery(c.Request.Context(), query)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1109, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/gateway-traces/export", func(c *gin.Context) {
		query, err := scopeGatewayTraceQuery(c.Request.Context(), control, principalAccess(c), gatewayTraceQuery(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1109, err.Error())
			return
		}
		totalLimit, baseOffset := exportWindow(c)
		data, err := collectGatewayTracesForExportQuery(c.Request.Context(), control, query, totalLimit, baseOffset)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1109, err.Error())
			return
		}
		writeCSV(c, "gateway-traces.csv", gatewayTraceCSVRows(data))
	})
}
