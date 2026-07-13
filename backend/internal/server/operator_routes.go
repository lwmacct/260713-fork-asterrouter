package server

import (
	"context"
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	operatorcore "github.com/astercloud/asterrouter/backend/internal/operator"
	"github.com/gin-gonic/gin"
)

func registerOperatorRoutes(group *gin.RouterGroup, svc *operatorcore.Service) {
	if svc == nil {
		return
	}
	group.GET("/dashboard", func(c *gin.Context) { data, err := svc.Dashboard(c.Request.Context()); operatorResponse(c, data, err) })
	registerOperatorCRUD(group, "customer-groups", svc.ListGroups, svc.SaveGroup, svc.DeleteGroup)
	registerOperatorCRUD(group, "customers", svc.ListCustomers, svc.SaveCustomer, svc.DeleteCustomer)
	registerOperatorCRUD(group, "plans", svc.ListPlans, svc.SavePlan, svc.DeletePlan)
	registerOperatorCRUD(group, "pricing-rules", svc.ListPricingRules, svc.SavePricingRule, svc.DeletePricingRule)
	registerOperatorCRUD(group, "risk-rules", svc.ListRiskRules, svc.SaveRiskRule, svc.DeleteRiskRule)
	group.GET("/risk-blocks", func(c *gin.Context) {
		data, err := svc.ListRiskBlocks(c.Request.Context())
		operatorResponse(c, data, err)
	})
	group.DELETE("/risk-blocks/:id", func(c *gin.Context) {
		err := svc.ClearRiskBlock(c.Request.Context(), actor(c), c.Param("id"))
		operatorResponse(c, gin.H{"status": "cleared"}, err)
	})
	registerOperatorCRUD(group, "notices", svc.ListNotices, svc.SaveNotice, svc.DeleteNotice)
	group.GET("/balance-entries", func(c *gin.Context) {
		data, err := svc.ListBalanceEntries(c.Request.Context())
		operatorResponse(c, data, err)
	})
	group.POST("/balance-entries", func(c *gin.Context) {
		var req operatorcore.BalanceEntryRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1600, "invalid balance entry payload")
			return
		}
		data, err := svc.ApplyBalanceEntry(c.Request.Context(), actor(c), req)
		operatorResponse(c, data, err)
	})
	group.GET("/customer-keys", func(c *gin.Context) {
		var data []controlplane.APIKeyRecord
		var err error
		if customerID := c.Query("customer_id"); customerID != "" {
			data, err = svc.ListCustomerKeys(c.Request.Context(), customerID)
		} else {
			data, err = svc.ListCustomerKeys(c.Request.Context())
		}
		operatorResponse(c, data, err)
	})
	group.POST("/customer-keys/:id/rotate", func(c *gin.Context) {
		data, err := svc.RotateCustomerKey(c.Request.Context(), actor(c), c.Param("id"))
		operatorResponse(c, data, err)
	})
	group.POST("/customer-keys/:id/disable", func(c *gin.Context) {
		err := svc.DisableCustomerKey(c.Request.Context(), actor(c), c.Param("id"))
		operatorResponse(c, gin.H{"status": "disabled"}, err)
	})
	group.GET("/usage", func(c *gin.Context) {
		data, err := svc.Usage(c.Request.Context(), controlplane.UsageQuery{
			Limit: intQuery(c, "limit", 50), Offset: intQuery(c, "offset", 0), Search: c.Query("q"),
			CustomerID: c.Query("customer_id"), APIKeyID: c.Query("api_key_id"), Model: c.Query("model"), Status: c.Query("status"),
		})
		operatorResponse(c, data, err)
	})
	group.POST("/customers/:id/keys", func(c *gin.Context) {
		var req controlplane.APIKeyCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1600, "invalid customer key payload")
			return
		}
		data, err := svc.CreateCustomerKey(c.Request.Context(), actor(c), c.Param("id"), req)
		operatorResponse(c, data, err)
	})
}

func registerOperatorCRUD[T any, R any](group *gin.RouterGroup, path string, list func(context.Context) ([]T, error), save func(context.Context, string, R) (T, error), remove func(context.Context, string) error) {
	group.GET("/"+path, func(c *gin.Context) { data, err := list(c.Request.Context()); operatorResponse(c, data, err) })
	group.POST("/"+path, func(c *gin.Context) {
		var req R
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1600, "invalid operator payload")
			return
		}
		data, err := save(c.Request.Context(), "", req)
		operatorResponse(c, data, err)
	})
	group.PUT("/"+path+"/:id", func(c *gin.Context) {
		var req R
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1600, "invalid operator payload")
			return
		}
		data, err := save(c.Request.Context(), c.Param("id"), req)
		operatorResponse(c, data, err)
	})
	group.DELETE("/"+path+"/:id", func(c *gin.Context) {
		err := remove(c.Request.Context(), c.Param("id"))
		operatorResponse(c, gin.H{"status": "deleted"}, err)
	})
}

func operatorResponse(c *gin.Context, data any, err error) {
	if err != nil {
		httpx.Error(c, http.StatusBadRequest, 1601, err.Error())
		return
	}
	httpx.OK(c, data)
}
