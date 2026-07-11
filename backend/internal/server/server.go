package server

import (
	"errors"
	"net/http"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
	"github.com/gin-gonic/gin"
)

type Options struct {
	Config          config.Config
	AuthService     *auth.Service
	SettingsService *settings.Service
	ControlService  *controlplane.Service
	PluginService   *plugins.Service
	SystemService   *system.Service
	ExportJobStore  CSVExportJobStore
}

func New(opts Options) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	exportJobStore := opts.ExportJobStore
	if exportJobStore == nil {
		exportJobStore = newCSVExportJobStore()
	}
	if opts.ControlService != nil && opts.PluginService != nil {
		opts.ControlService.SetAlertDispatcher(opts.PluginService)
	}

	r.GET("/health", func(c *gin.Context) {
		httpx.OK(c, gin.H{"status": "ok"})
	})

	r.GET("/ready", func(c *gin.Context) {
		if err := opts.SettingsService.Health(c.Request.Context()); err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
			return
		}
		if opts.ControlService != nil {
			if err := opts.ControlService.Health(c.Request.Context()); err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
				return
			}
		}
		if opts.PluginService != nil {
			if err := opts.PluginService.Health(c.Request.Context()); err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
				return
			}
		}
		if exportJobStore != nil {
			if err := exportJobStore.Health(c.Request.Context()); err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
				return
			}
		}
		httpx.OK(c, gin.H{"status": "ready"})
	})

	api := r.Group("/api/v1")
	api.GET("/settings/public", func(c *gin.Context) {
		data, err := opts.SettingsService.Public(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1002, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	api.GET("/i18n/locales", func(c *gin.Context) {
		httpx.OK(c, settings.SupportedLocales)
	})
	api.GET("/setup/status", func(c *gin.Context) {
		data, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1003, err.Error())
			return
		}
		httpx.OK(c, gin.H{
			"default_profile":  data.DefaultProfile,
			"enabled_profiles": data.EnabledProfiles,
			"setup_completed":  data.SetupCompleted,
		})
	})
	api.POST("/setup/profiles", func(c *gin.Context) {
		var req struct {
			Profiles       []string `json:"profiles"`
			DefaultProfile string   `json:"default_profile"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		data, err := opts.SettingsService.ApplyProfiles(c.Request.Context(), req.Profiles, req.DefaultProfile)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1401, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	api.POST("/auth/login", func(c *gin.Context) {
		if opts.AuthService == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1300, "auth service is not available")
			return
		}
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1301, "invalid login payload")
			return
		}
		result, err := opts.AuthService.Login(c.Request.Context(), req.Username, req.Password)
		if err != nil {
			if errors.Is(err, auth.ErrInvalidCredentials) {
				httpx.Error(c, http.StatusUnauthorized, 1302, "invalid username or password")
				return
			}
			httpx.Error(c, http.StatusInternalServerError, 1303, err.Error())
			return
		}
		httpx.OK(c, result)
	})
	api.GET("/auth/me", requireAdminAuth(opts.Config.AdminToken, opts.AuthService), func(c *gin.Context) {
		httpx.OK(c, gin.H{
			"username": actor(c),
			"role":     "super_admin",
		})
	})

	admin := api.Group("/admin")
	admin.Use(requireAdminAuth(opts.Config.AdminToken, opts.AuthService))
	admin.Use(requireRBAC(opts.ControlService))
	registerAdminRoutes(admin, opts.ControlService, exportJobStore)
	registerPluginRoutes(admin.Group("/plugins"), opts.PluginService, opts.ControlService)
	registerSystemRoutes(admin.Group("/system"), opts.SystemService, opts.SettingsService, opts.ControlService)
	admin.GET("/settings", func(c *gin.Context) {
		data, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1004, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/settings", func(c *gin.Context) {
		var req settings.AdminSettings
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1402, "invalid settings payload")
			return
		}
		data, err := opts.SettingsService.Update(c.Request.Context(), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1403, err.Error())
			return
		}
		httpx.OK(c, data)
	})

	portal := api.Group("/portal")
	portal.Use(requireAdminAuth(opts.Config.AdminToken, opts.AuthService))
	registerPortalRoutes(portal, opts.ControlService)

	registerGatewayRoutes(r, opts.ControlService)

	serveSPA(r, opts.Config.FrontendDir)
	return r
}
