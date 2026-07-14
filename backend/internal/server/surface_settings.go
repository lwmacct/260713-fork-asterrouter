package server

import (
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/gin-gonic/gin"
)

func registerSurfaceSettings(group *gin.RouterGroup, svc *settings.Service, control *controlplane.Service) {
	group.GET("/settings/email-templates/defaults", func(c *gin.Context) {
		httpx.OK(c, auth.DefaultEmailTemplates())
	})
	group.GET("/settings", func(c *gin.Context) {
		data, err := svc.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1004, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	group.PUT("/settings", func(c *gin.Context) {
		var req settings.AdminSettings
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1402, "invalid settings payload")
			return
		}
		previous, err := svc.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1004, err.Error())
			return
		}
		if !requireProfileBundleChange(c, control, previous, req) {
			return
		}
		data, err := svc.Update(c.Request.Context(), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1403, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}
