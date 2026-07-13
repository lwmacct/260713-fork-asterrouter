package server

import (
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func requireSurfaceAccess(control *controlplane.Service, surface string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if control == nil {
			c.Next()
			return
		}
		allowed, err := control.ActorCanSurface(c.Request.Context(), actor(c), surface)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1452, err.Error())
			c.Abort()
			return
		}
		if !allowed {
			httpx.Error(c, http.StatusForbidden, 1453, "surface access denied")
			c.Abort()
			return
		}
		c.Next()
	}
}
