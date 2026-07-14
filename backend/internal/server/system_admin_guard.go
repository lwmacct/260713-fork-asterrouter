package server

import (
	"net/http"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/gin-gonic/gin"
)

func requireSystemAdministrator(control *controlplane.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if control == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1454, "control service is not available")
			c.Abort()
			return
		}
		allowed, err := control.ActorIsSystemAdministrator(c.Request.Context(), actor(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1454, err.Error())
			c.Abort()
			return
		}
		if !allowed {
			httpx.Error(c, http.StatusForbidden, 1455, "system administrator access required")
			c.Abort()
			return
		}
		c.Next()
	}
}

func requireProfileBundleChange(c *gin.Context, control *controlplane.Service, previous, next settings.AdminSettings) bool {
	if !profileBundleChanged(previous, next) {
		return true
	}
	httpx.Error(c, http.StatusConflict, 1455, "change the active deployment profile from the deployment switcher")
	return false
}

func profileBundleChanged(previous, next settings.AdminSettings) bool {
	if strings.TrimSpace(previous.DefaultProfile) != strings.TrimSpace(next.DefaultProfile) {
		return true
	}
	if len(previous.EnabledProfiles) != len(next.EnabledProfiles) {
		return true
	}
	for index, profile := range previous.EnabledProfiles {
		if strings.TrimSpace(profile) != strings.TrimSpace(next.EnabledProfiles[index]) {
			return true
		}
	}
	return false
}
