package server

import (
	"net/http"
	"strconv"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerEffectivePricingAdminRoutes(admin *gin.RouterGroup, control *controlplane.Service) {
	admin.GET("/effective-pricing/report", func(c *gin.Context) {
		data, err := control.EffectivePricingReport(c.Request.Context(), controlplane.EffectivePricingReportQuery{
			Model: c.Query("model"), Protocol: c.Query("protocol"), WindowHours: queryInt(c, "window_hours"),
		})
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1530, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/effective-pricing/policy", func(c *gin.Context) {
		data, err := control.EffectivePricingPolicy(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1531, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/effective-pricing/policy", func(c *gin.Context) {
		var request controlplane.EffectivePricingPolicyRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1532, "invalid effective pricing policy payload")
			return
		}
		data, err := control.UpdateEffectivePricingPolicy(c.Request.Context(), actor(c), request)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1533, err.Error())
			return
		}
		httpx.OK(c, data)
	})

	admin.GET("/procurement-prices", func(c *gin.Context) {
		data, err := control.ListProcurementPrices(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1534, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/procurement-prices", func(c *gin.Context) {
		var request controlplane.ProcurementPriceRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1535, "invalid procurement price payload")
			return
		}
		data, err := control.CreateProcurementPrice(c.Request.Context(), actor(c), request)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1536, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/procurement-prices/:id", func(c *gin.Context) {
		var request controlplane.ProcurementPriceRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1535, "invalid procurement price payload")
			return
		}
		data, err := control.UpdateProcurementPrice(c.Request.Context(), actor(c), c.Param("id"), request)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1536, err.Error())
			return
		}
		httpx.OK(c, data)
	})

	admin.GET("/provider-billing-lines", func(c *gin.Context) {
		data, err := control.ListProviderBillingLines(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1537, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/provider-billing-lines", func(c *gin.Context) {
		var request controlplane.ProviderBillingLineRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1538, "invalid provider billing line payload")
			return
		}
		data, err := control.ImportProviderBillingLine(c.Request.Context(), actor(c), request)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1539, err.Error())
			return
		}
		httpx.OK(c, data)
	})

	admin.GET("/provider-cache-capabilities", func(c *gin.Context) {
		data, err := control.ListProviderCacheCapabilities(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1540, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/provider-cache-capabilities", func(c *gin.Context) {
		var request controlplane.ProviderCacheCapabilityRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1541, "invalid provider cache capability payload")
			return
		}
		data, err := control.UpsertProviderCacheCapability(c.Request.Context(), actor(c), request)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1542, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.GET("/provider-cache-probes", func(c *gin.Context) {
		data, err := control.ListProviderCacheProbeRuns(c.Request.Context(), queryInt(c, "limit"))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1543, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/provider-cache-probes", func(c *gin.Context) {
		var request controlplane.CacheProbeRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1549, "invalid provider cache probe payload")
			return
		}
		data, err := control.RunProviderCacheProbe(c.Request.Context(), actor(c), request)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1550, err.Error())
			return
		}
		httpx.OK(c, data)
	})

	admin.GET("/effective-pricing/decisions", func(c *gin.Context) {
		data, err := control.ListEffectivePricingDecisions(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1544, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/effective-pricing/decisions/evaluate", func(c *gin.Context) {
		var request controlplane.EffectivePricingDecisionEvaluationRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1545, "invalid effective pricing evaluation payload")
			return
		}
		data, err := control.EvaluateEffectivePricingDecision(c.Request.Context(), actor(c), request)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1546, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.POST("/effective-pricing/decisions/:id/action", func(c *gin.Context) {
		var request controlplane.EffectivePricingDecisionActionRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1547, "invalid effective pricing decision action payload")
			return
		}
		data, err := control.ActOnEffectivePricingDecision(c.Request.Context(), actor(c), c.Param("id"), request)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1548, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}

func queryInt(c *gin.Context, key string) int {
	value, _ := strconv.Atoi(c.Query(key))
	return value
}
