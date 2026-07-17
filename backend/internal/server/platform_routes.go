package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/gin-gonic/gin"
)

// registerPlatformRoutes exposes the trusted gateway Core through the
// Platform surface. It intentionally excludes Enterprise identity and Relay
// billing resources: a platform operator configures AI delivery, not an
// external product's users, sessions, subscriptions, or balances.
func registerPlatformRoutes(platform *gin.RouterGroup, control *controlplane.Service, pluginService *plugins.Service, runtime AIJobRuntimeStatusProvider) {
	if control == nil {
		return
	}
	platform.Use(func(c *gin.Context) {
		if err := control.EnsurePlatformBootstrap(c.Request.Context()); err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1510, err.Error())
			c.Abort()
			return
		}
		c.Next()
	})
	registerPlatformDashboardRoutes(platform, control)
	registerPlatformDomainRoutes(platform, control)
	registerProviderAdminRoutes(platform, control)
	registerRoutingAdminRoutes(platform, control)
	registerGatewayModelAdminRoutes(platform, control)
	registerGovernancePolicyAdminRoutes(platform, control)
	registerPricingRuleRoutes(platform, control, PricingSurfacePlatform)
	registerAIJobAdminRoutes(platform, control, runtime, controlplane.ProfileScopePlatform)
	registerArtifactAdminRoutes(platform, control, controlplane.ProfileScopePlatform)
	registerPlatformAPIKeyRoutes(platform, control)
	registerExternalAuthIntegrationRoutes(platform, control)
	registerPlatformUsageSinkRoutes(platform, control)
	registerObservabilityAdminRoutesForScope(platform, control, controlplane.ProfileScopePlatform)
	registerAlertAdminRoutesForScope(platform, control, controlplane.ProfileScopePlatform)
	registerPluginRoutes(platform.Group("/plugins"), pluginService, control, "platform")
}

func registerPlatformUsageSinkRoutes(platform *gin.RouterGroup, control *controlplane.Service) {
	platform.GET("/usage-sinks", func(c *gin.Context) {
		data, err := control.ListPlatformUsageSinks(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1516, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.POST("/usage-sinks", func(c *gin.Context) {
		var req controlplane.PlatformUsageSinkRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1517, "invalid platform usage sink payload")
			return
		}
		data, err := control.CreatePlatformUsageSink(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1517, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.PUT("/usage-sinks/:id", func(c *gin.Context) {
		var req controlplane.PlatformUsageSinkRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1517, "invalid platform usage sink payload")
			return
		}
		data, err := control.UpdatePlatformUsageSink(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1517, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.POST("/usage-sinks/:id/rotate-endpoint", func(c *gin.Context) {
		var req struct {
			EndpointURL   string `json:"endpoint_url"`
			SigningSecret string `json:"signing_secret"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1517, "invalid platform usage sink rotation payload")
			return
		}
		data, err := control.RotatePlatformUsageSinkEndpoint(c.Request.Context(), actor(c), c.Param("id"), req.EndpointURL, req.SigningSecret)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1517, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.GET("/usage-sinks/:id/deliveries", func(c *gin.Context) {
		data, err := control.ListPlatformUsageDeliveryEvents(c.Request.Context(), controlplane.PlatformUsageDeliveryQuery{SinkID: c.Param("id"), Status: c.Query("status")})
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1518, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.POST("/usage-sinks/:id/deliveries/:deliveryID/requeue", func(c *gin.Context) {
		if err := control.RequeuePlatformUsageDeliveryEvent(c.Request.Context(), actor(c), c.Param("id"), c.Param("deliveryID")); err != nil {
			if errors.Is(err, controlplane.ErrPlatformUsageDeliveryNotFound) {
				httpx.Error(c, http.StatusNotFound, 1518, "platform usage delivery event not found")
				return
			}
			httpx.Error(c, http.StatusBadRequest, 1518, err.Error())
			return
		}
		httpx.OK(c, gin.H{"status": "pending"})
	})
}

func registerExternalAuthIntegrationRoutes(platform *gin.RouterGroup, control *controlplane.Service) {
	platform.GET("/external-auth-integrations", func(c *gin.Context) {
		data, err := control.ListExternalAuthIntegrations(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1514, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.POST("/external-auth-integrations", func(c *gin.Context) {
		var req controlplane.ExternalAuthIntegrationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1515, "invalid external auth integration payload")
			return
		}
		data, err := control.CreateExternalAuthIntegration(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1515, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.PUT("/external-auth-integrations/:id", func(c *gin.Context) {
		var req controlplane.ExternalAuthIntegrationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1515, "invalid external auth integration payload")
			return
		}
		data, err := control.UpdateExternalAuthIntegration(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1515, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.POST("/external-auth-integrations/:id/rotate-secret", func(c *gin.Context) {
		data, err := control.RotateExternalAuthIntegrationSecret(c.Request.Context(), actor(c), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1515, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}

func registerPlatformAPIKeyRoutes(platform *gin.RouterGroup, control *controlplane.Service) {
	platform.GET("/api-keys", func(c *gin.Context) {
		data, err := control.ListAPIKeys(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1105, err.Error())
			return
		}
		httpx.OK(c, platformAPIKeys(data))
	})
	platform.GET("/api-keys/:id/policy-explanation", func(c *gin.Context) {
		if err := requirePlatformAPIKey(c.Request.Context(), control, c.Param("id")); err != nil {
			httpx.Error(c, http.StatusNotFound, 1507, "platform api key not found")
			return
		}
		data, err := control.ExplainGatewayPolicyForAPIKey(c.Request.Context(), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1507, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.POST("/api-keys", func(c *gin.Context) {
		var req controlplane.APIKeyCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1506, "invalid api key payload")
			return
		}
		if err := validatePlatformAPIKeyType(req.KeyType, req.CustomerID, req.OwnerUserID); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1507, err.Error())
			return
		}
		if strings.TrimSpace(req.KeyType) == "" {
			req.KeyType = controlplane.APIKeyTypeWorkspace
		}
		data, err := control.CreatePlatformAPIKey(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1507, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.PUT("/api-keys/:id", func(c *gin.Context) {
		if err := requirePlatformAPIKey(c.Request.Context(), control, c.Param("id")); err != nil {
			httpx.Error(c, http.StatusNotFound, 1507, "platform api key not found")
			return
		}
		var req controlplane.APIKeyUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1506, "invalid api key payload")
			return
		}
		if err := validatePlatformAPIKeyType(req.KeyType, req.CustomerID, req.OwnerUserID); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1507, err.Error())
			return
		}
		data, err := control.UpdatePlatformAPIKey(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1507, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.POST("/api-keys/:id/rotate", func(c *gin.Context) {
		if err := requirePlatformAPIKey(c.Request.Context(), control, c.Param("id")); err != nil {
			httpx.Error(c, http.StatusNotFound, 1507, "platform api key not found")
			return
		}
		req, err := bindAPIKeyRotateRequest(c)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1506, "invalid api key rotation payload")
			return
		}
		data, err := control.RotatePlatformAPIKeyWithGrace(c.Request.Context(), actor(c), c.Param("id"), req.GracePeriodSeconds)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1507, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.POST("/api-keys/:id/disable", func(c *gin.Context) {
		if err := requirePlatformAPIKey(c.Request.Context(), control, c.Param("id")); err != nil {
			httpx.Error(c, http.StatusNotFound, 1508, "platform api key not found")
			return
		}
		if err := control.DisablePlatformAPIKey(c.Request.Context(), actor(c), c.Param("id")); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1508, err.Error())
			return
		}
		httpx.OK(c, gin.H{"status": "disabled"})
	})
}

func platformAPIKeys(keys []controlplane.APIKeyRecord) []controlplane.APIKeyRecord {
	filtered := make([]controlplane.APIKeyRecord, 0, len(keys))
	for _, key := range keys {
		if key.ProfileScope == controlplane.ProfileScopePlatform && (key.KeyType == controlplane.APIKeyTypeWorkspace || key.KeyType == controlplane.APIKeyTypeService) {
			filtered = append(filtered, key)
		}
	}
	return filtered
}

func registerPlatformDashboardRoutes(platform *gin.RouterGroup, control *controlplane.Service) {
	platform.GET("/dashboard", func(c *gin.Context) {
		data, err := control.PlatformDashboard(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1100, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}

func registerPlatformDomainRoutes(platform *gin.RouterGroup, control *controlplane.Service) {
	ensurePlatformDomain := func(c *gin.Context) bool {
		if err := control.EnsurePlatformBootstrap(c.Request.Context()); err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1510, err.Error())
			return false
		}
		return true
	}
	platform.GET("/tenants", func(c *gin.Context) {
		if !ensurePlatformDomain(c) {
			return
		}
		data, err := control.ListPlatformTenants(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1510, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.POST("/tenants", func(c *gin.Context) {
		var req controlplane.PlatformTenantRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1511, "invalid platform tenant payload")
			return
		}
		data, err := control.CreatePlatformTenant(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1511, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.PUT("/tenants/:id", func(c *gin.Context) {
		var req controlplane.PlatformTenantRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1511, "invalid platform tenant payload")
			return
		}
		data, err := control.UpdatePlatformTenant(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1511, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.GET("/gateway-principals", func(c *gin.Context) {
		if !ensurePlatformDomain(c) {
			return
		}
		data, err := control.ListGatewayPrincipals(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1512, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.POST("/gateway-principals", func(c *gin.Context) {
		var req controlplane.GatewayPrincipalRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1513, "invalid gateway principal payload")
			return
		}
		data, err := control.CreateGatewayPrincipal(c.Request.Context(), actor(c), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1513, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	platform.PUT("/gateway-principals/:id", func(c *gin.Context) {
		var req controlplane.GatewayPrincipalRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1513, "invalid gateway principal payload")
			return
		}
		data, err := control.UpdateGatewayPrincipal(c.Request.Context(), actor(c), c.Param("id"), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1513, err.Error())
			return
		}
		httpx.OK(c, data)
	})
}

func requirePlatformAPIKey(ctx context.Context, control *controlplane.Service, id string) error {
	keys, err := control.ListAPIKeys(ctx)
	if err != nil {
		return err
	}
	for _, key := range platformAPIKeys(keys) {
		if key.ID == id {
			return nil
		}
	}
	return httpxError("platform api key not found")
}

func validatePlatformAPIKeyType(keyType, customerID, ownerUserID string) error {
	keyType = strings.TrimSpace(keyType)
	if keyType == "" || keyType == controlplane.APIKeyTypeWorkspace || keyType == controlplane.APIKeyTypeService {
		if strings.TrimSpace(customerID) != "" || strings.TrimSpace(ownerUserID) != "" {
			return httpxError("platform API keys cannot reference relay customers or enterprise users")
		}
		return nil
	}
	return httpxError("platform API keys must use workspace or service ownership")
}

type httpxError string

func (e httpxError) Error() string { return string(e) }
