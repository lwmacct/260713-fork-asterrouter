package server

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func registerCustomerRoutes(customer *gin.RouterGroup, control *controlplane.Service) {
	if control == nil {
		return
	}
	customer.GET("/billing", func(c *gin.Context) {
		data, err := control.CustomerBillingOverview(c.Request.Context(), actor(c))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1260, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	customer.GET("/billing/entries", func(c *gin.Context) {
		query, err := customerBillingQuery(c, 20)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1261, err.Error())
			return
		}
		data, err := control.CustomerBillingEntries(c.Request.Context(), actor(c), query)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1261, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	customer.GET("/billing/entries/export", func(c *gin.Context) {
		query, err := customerBillingQuery(c, 10000)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1262, err.Error())
			return
		}
		query.Offset = 0
		query.Limit = 10000
		data, err := control.CustomerBillingEntries(c.Request.Context(), actor(c), query)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1262, err.Error())
			return
		}
		filename := fmt.Sprintf("billing-%s.csv", time.Now().UTC().Format("20060102-150405"))
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
		c.Status(http.StatusOK)
		_, _ = c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
		writer := csv.NewWriter(c.Writer)
		_ = writer.Write([]string{"时间", "类型", "金额（微美元）", "余额（微美元）", "说明", "参考号"})
		for _, entry := range data.Items {
			_ = writer.Write([]string{
				entry.CreatedAt.Format(time.RFC3339), entry.Kind, strconv.FormatInt(entry.AmountMicros, 10),
				strconv.FormatInt(entry.BalanceAfterMicros, 10), entry.Description, entry.Reference,
			})
		}
		writer.Flush()
	})
	customer.POST("/billing/redeem", func(c *gin.Context) {
		var request controlplane.CustomerRedeemRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1263, "兑换码请求无效")
			return
		}
		data, err := control.RedeemCustomerCode(c.Request.Context(), actor(c), request)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, controlplane.ErrCustomerCodeAlreadyUsed) || errors.Is(err, controlplane.ErrCustomerCodeUnavailable) {
				status = http.StatusConflict
			}
			httpx.Error(c, status, 1263, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	customer.POST("/billing/recharge-orders", func(c *gin.Context) {
		var request controlplane.CustomerRechargeRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1264, "充值请求无效")
			return
		}
		data, err := control.CreateCustomerRechargeOrder(c.Request.Context(), actor(c), request)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, controlplane.ErrCustomerPaymentUnavailable) {
				status = http.StatusServiceUnavailable
			}
			httpx.Error(c, status, 1264, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	customer.GET("/notification-settings", func(c *gin.Context) {
		data, err := control.CustomerNotificationSettings(c.Request.Context(), actor(c))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1265, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	customer.PUT("/notification-settings", func(c *gin.Context) {
		var request controlplane.CustomerNotificationSettingsRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1266, "通知设置请求无效")
			return
		}
		data, err := control.UpdateCustomerNotificationSettings(c.Request.Context(), actor(c), request)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1266, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	customer.GET("/notifications", func(c *gin.Context) {
		limit, err := strconv.Atoi(defaultString(c.Query("limit"), "20"))
		if err != nil || limit <= 0 {
			httpx.Error(c, http.StatusBadRequest, 1267, "limit 必须是正整数")
			return
		}
		offset, err := strconv.Atoi(defaultString(c.Query("offset"), "0"))
		if err != nil || offset < 0 {
			httpx.Error(c, http.StatusBadRequest, 1267, "offset 必须是非负整数")
			return
		}
		data, err := control.CustomerNotifications(c.Request.Context(), actor(c), controlplane.CustomerNotificationQuery{Limit: limit, Offset: offset})
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1267, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	markRead := func(c *gin.Context) {
		if err := control.MarkCustomerNotificationRead(c.Request.Context(), actor(c), c.Param("id")); err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, controlplane.ErrCustomerNotificationNotFound) {
				status = http.StatusNotFound
			}
			httpx.Error(c, status, 1268, err.Error())
			return
		}
		httpx.OK(c, gin.H{"read": true})
	}
	customer.POST("/notifications/:id/read", markRead)
	customer.PATCH("/notifications/:id/read", markRead)
	customer.POST("/notifications/read-all", func(c *gin.Context) {
		updated, err := control.MarkAllCustomerNotificationsRead(c.Request.Context(), actor(c))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1269, err.Error())
			return
		}
		httpx.OK(c, gin.H{"updated": updated})
	})
}

func customerBillingQuery(c *gin.Context, defaultLimit int) (controlplane.CustomerBillingQuery, error) {
	limit, err := strconv.Atoi(defaultString(c.Query("limit"), strconv.Itoa(defaultLimit)))
	if err != nil || limit <= 0 {
		return controlplane.CustomerBillingQuery{}, errors.New("limit 必须是正整数")
	}
	offset, err := strconv.Atoi(defaultString(c.Query("offset"), "0"))
	if err != nil || offset < 0 {
		return controlplane.CustomerBillingQuery{}, errors.New("offset 必须是非负整数")
	}
	query := controlplane.CustomerBillingQuery{Kind: strings.TrimSpace(c.Query("kind")), Limit: limit, Offset: offset}
	if value := strings.TrimSpace(c.Query("from")); value != "" {
		parsed, parseErr := time.Parse("2006-01-02", value)
		if parseErr != nil {
			return controlplane.CustomerBillingQuery{}, errors.New("from 日期格式无效")
		}
		query.From = &parsed
	}
	if value := strings.TrimSpace(c.Query("to")); value != "" {
		parsed, parseErr := time.Parse("2006-01-02", value)
		if parseErr != nil {
			return controlplane.CustomerBillingQuery{}, errors.New("to 日期格式无效")
		}
		endOfDay := parsed.Add(24*time.Hour - time.Nanosecond)
		query.To = &endOfDay
	}
	return query, nil
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
