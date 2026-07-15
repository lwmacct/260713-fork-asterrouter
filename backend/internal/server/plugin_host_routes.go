package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/gin-gonic/gin"
)

func registerPluginHostRoutes(group *gin.RouterGroup, svc *plugins.Service, control *controlplane.Service) {
	group.GET("/:plugin_id/feeds/:service_key", func(c *gin.Context) {
		if !requestFromLoopback(c.Request) {
			httpx.Error(c, http.StatusForbidden, 1790, "plugin host API is only available on loopback")
			return
		}
		if svc == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin service is not available")
			return
		}
		token := bearerToken(c)
		payload, err := svc.SidecarFeedPayload(c.Request.Context(), c.Param("plugin_id"), token, c.Param("service_key"))
		if err != nil {
			writePluginHostError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		if control != nil {
			_ = control.RecordPluginEvent(c.Request.Context(), "plugin:"+c.Param("plugin_id"), "feed_read", c.Param("service_key"), fmt.Sprintf("Plugin %s read official feed %s", c.Param("plugin_id"), c.Param("service_key")))
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
	})

	providerCallbackHandler := func(c *gin.Context) {
		if !requestFromLoopback(c.Request) {
			httpx.Error(c, http.StatusForbidden, 1790, "plugin host API is only available on loopback")
			return
		}
		if svc == nil || control == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1700, "plugin callback service is not available")
			return
		}
		pluginID := strings.TrimSpace(c.Param("plugin_id"))
		if err := svc.AuthorizeSidecarProviderCallback(c.Request.Context(), pluginID, bearerToken(c)); err != nil {
			writePluginHostError(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
		var callback controlplane.ProviderCallback
		if err := json.NewDecoder(c.Request.Body).Decode(&callback); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1796, "invalid provider callback")
			return
		}
		if strings.TrimSpace(callback.AdapterID) != pluginID {
			// The runtime token identifies the adapter; accepting a different ID
			// would let one sidecar submit events for another adapter.
			httpx.Error(c, http.StatusForbidden, 1797, "provider callback adapter binding failed")
			return
		}
		result, err := control.ProcessProviderCallback(c.Request.Context(), callback, svc)
		if err != nil {
			writeProviderCallbackError(c, err)
			return
		}
		httpx.OK(c, result)
	}
	group.POST("/:plugin_id/provider-callback", providerCallbackHandler)
	group.POST("/:plugin_id/provider-callbacks", providerCallbackHandler)
}

func writePluginHostError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, plugins.ErrPluginHostUnauthorized):
		httpx.Error(c, http.StatusUnauthorized, 1791, err.Error())
	case errors.Is(err, plugins.ErrPluginHostPermission), errors.Is(err, plugins.ErrOfficialFeedEntitlement), errors.Is(err, plugins.ErrOfficialFeedBinding):
		httpx.Error(c, http.StatusForbidden, 1792, err.Error())
	case errors.Is(err, plugins.ErrOfficialFeedNotFound), errors.Is(err, plugins.ErrLicenseNotFound):
		httpx.Error(c, http.StatusNotFound, 1793, err.Error())
	case errors.Is(err, plugins.ErrOfficialFeedExpired):
		httpx.Error(c, http.StatusConflict, 1794, err.Error())
	default:
		httpx.Error(c, http.StatusInternalServerError, 1795, err.Error())
	}
}

func writeProviderCallbackError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, controlplane.ErrProviderCallbackInvalid):
		httpx.Error(c, http.StatusBadRequest, 1796, "invalid provider callback")
	case errors.Is(err, controlplane.ErrProviderCallbackBinding):
		// Do not disclose whether another tenant's attempt or task exists.
		httpx.Error(c, http.StatusNotFound, 1798, "provider callback target was not found")
	case errors.Is(err, controlplane.ErrProviderCallbackReplayConflict):
		httpx.Error(c, http.StatusConflict, 1799, "provider callback event conflicts with a previous event")
	default:
		httpx.Error(c, http.StatusInternalServerError, 1800, "provider callback processing failed")
	}
}

func requestFromLoopback(request *http.Request) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err != nil {
		return false
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
