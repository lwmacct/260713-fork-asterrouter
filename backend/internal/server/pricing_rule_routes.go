package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/pricing"
	"github.com/gin-gonic/gin"
)

type pricingSurface string

const (
	PricingSurfaceAdmin    pricingSurface = "admin"
	PricingSurfacePlatform pricingSurface = "platform"
	PricingSurfaceOperator pricingSurface = "operator"
)

func registerPricingRuleRoutes(group *gin.RouterGroup, control *controlplane.Service, surface pricingSurface) {
	if control == nil {
		return
	}
	group.GET("/pricing-rules", func(c *gin.Context) {
		query := controlplane.PricingRuleQuery{Purpose: c.Query("purpose"), ScopeType: c.Query("scope_type"), ScopeID: c.Query("scope_id"), Model: c.Query("model"), Status: c.Query("status")}
		if surface == PricingSurfaceOperator {
			query.Purpose = controlplane.PricingPurposeCustomerCharge
		} else if surface == PricingSurfacePlatform {
			query.Purpose = controlplane.PricingPurposeUsageCost
			query.ScopeType = controlplane.PricingScopeGlobal
			query.ScopeID = ""
		}
		data, err := control.ListPricingRules(c.Request.Context(), query)
		pricingResponse(c, data, err)
	})
	group.POST("/pricing-rules", func(c *gin.Context) {
		var request controlplane.PricingRuleCreateRequest
		if err := bindStrictJSON(c, &request); err != nil {
			pricingResponseError(c, http.StatusBadRequest, err)
			return
		}
		if surface == PricingSurfaceOperator {
			if request.Purpose != "" && request.Purpose != controlplane.PricingPurposeCustomerCharge {
				pricingResponseError(c, http.StatusBadRequest, errors.New("operator can only create customer_charge rules"))
				return
			}
			request.Purpose = controlplane.PricingPurposeCustomerCharge
		}
		if surface == PricingSurfacePlatform {
			request.Purpose = controlplane.PricingPurposeUsageCost
			request.ScopeType = controlplane.PricingScopeGlobal
			request.ScopeID = ""
		}
		data, err := control.CreatePricingRule(c.Request.Context(), actor(c), request)
		pricingResponse(c, data, err)
	})
	group.GET("/pricing-rules/:id", func(c *gin.Context) {
		data, err := control.PricingRuleDetail(c.Request.Context(), c.Param("id"))
		if err == nil && !pricingRuleVisibleOnSurface(data.Rule, surface) {
			err = controlplane.ErrPricingRuleNotFound
		}
		pricingResponse(c, data, err)
	})
	group.PUT("/pricing-rules/:id/draft", func(c *gin.Context) {
		if !authorizePricingRuleMutation(c, control, surface) {
			return
		}
		var request controlplane.PricingDraftUpdateRequest
		if err := bindStrictJSON(c, &request); err != nil {
			pricingResponseError(c, http.StatusBadRequest, err)
			return
		}
		data, err := control.UpdatePricingRuleDraft(c.Request.Context(), actor(c), c.Param("id"), request)
		pricingResponse(c, data, err)
	})
	group.POST("/pricing-rules/validate", func(c *gin.Context) {
		var request struct {
			Expression string                             `json:"expression"`
			TestCases  []controlplane.PricingRuleTestCase `json:"test_cases"`
		}
		if err := bindStrictJSON(c, &request); err != nil {
			pricingResponseError(c, http.StatusBadRequest, err)
			return
		}
		result := control.ValidatePricingRule(c.Request.Context(), request.Expression, request.TestCases)
		status := http.StatusOK
		if !result.Valid {
			status = http.StatusUnprocessableEntity
		}
		c.JSON(status, gin.H{"data": result})
	})
	group.POST("/pricing-rules/simulate", func(c *gin.Context) {
		var request controlplane.PricingSimulationRequest
		if err := bindStrictJSON(c, &request); err != nil {
			pricingResponseError(c, http.StatusBadRequest, err)
			return
		}
		if request.RuleVersionID != "" {
			rule, _, err := control.PricingRuleVersionDetail(c.Request.Context(), request.RuleVersionID)
			if err != nil || !pricingRuleVisibleOnSurface(rule, surface) {
				pricingResponse(c, nil, controlplane.ErrPricingVersionNotFound)
				return
			}
		}
		data, err := control.SimulatePricing(c.Request.Context(), request)
		pricingResponse(c, data, err)
	})
	group.POST("/pricing-rules/:id/publish", func(c *gin.Context) {
		if !authorizePricingRuleMutation(c, control, surface) {
			return
		}
		var request controlplane.PricingPublishRequest
		if err := bindStrictJSON(c, &request); err != nil {
			pricingResponseError(c, http.StatusBadRequest, err)
			return
		}
		data, err := control.PublishPricingRule(c.Request.Context(), actor(c), c.Param("id"), request)
		pricingResponse(c, data, err)
	})
	group.POST("/pricing-rules/:id/activate/:version_id", func(c *gin.Context) {
		if !authorizePricingRuleMutation(c, control, surface) {
			return
		}
		var request controlplane.PricingActivateRequest
		if err := bindStrictJSON(c, &request); err != nil {
			pricingResponseError(c, http.StatusBadRequest, err)
			return
		}
		err := control.ActivatePricingRuleVersion(c.Request.Context(), actor(c), c.Param("id"), c.Param("version_id"), request.ExpectedLockVersion)
		pricingResponse(c, gin.H{"status": "active"}, err)
	})
	group.POST("/pricing-rules/:id/disable", func(c *gin.Context) {
		if !authorizePricingRuleMutation(c, control, surface) {
			return
		}
		var request PricingDisableRequest
		if err := bindStrictJSON(c, &request); err != nil {
			pricingResponseError(c, http.StatusBadRequest, err)
			return
		}
		err := control.DisablePricingRule(c.Request.Context(), actor(c), c.Param("id"), request.ExpectedLockVersion)
		pricingResponse(c, gin.H{"status": "disabled"}, err)
	})
	group.GET("/pricing-rules/:id/versions", func(c *gin.Context) {
		data, err := control.PricingRuleDetail(c.Request.Context(), c.Param("id"))
		if err == nil && !pricingRuleVisibleOnSurface(data.Rule, surface) {
			err = controlplane.ErrPricingRuleNotFound
		}
		if err == nil {
			pricingResponse(c, data.Versions, nil)
			return
		}
		pricingResponse(c, nil, err)
	})
	group.GET("/pricing-evaluations/:id", func(c *gin.Context) {
		data, found, err := control.PricingEvaluation(c.Request.Context(), c.Param("id"))
		if err == nil && found {
			rule, _, findErr := control.PricingRuleVersionDetail(c.Request.Context(), data.PricingRuleVersionID)
			if findErr != nil || !pricingRuleVisibleOnSurface(rule, surface) {
				found = false
			}
		}
		if err == nil && !found {
			err = controlplane.ErrPricingVersionNotFound
		}
		pricingResponse(c, data, err)
	})
}

func authorizePricingRuleMutation(c *gin.Context, control *controlplane.Service, surface pricingSurface) bool {
	detail, err := control.PricingRuleDetail(c.Request.Context(), c.Param("id"))
	if err == nil && !pricingRuleVisibleOnSurface(detail.Rule, surface) {
		err = controlplane.ErrPricingRuleNotFound
	}
	if err != nil {
		pricingResponse(c, nil, err)
		return false
	}
	return true
}

func pricingRuleVisibleOnSurface(rule controlplane.PricingRule, surface pricingSurface) bool {
	switch surface {
	case PricingSurfacePlatform:
		return rule.Purpose == controlplane.PricingPurposeUsageCost && rule.ScopeType == controlplane.PricingScopeGlobal
	case PricingSurfaceOperator:
		return rule.Purpose == controlplane.PricingPurposeCustomerCharge
	default:
		return true
	}
}

type PricingDisableRequest struct {
	ExpectedLockVersion int64 `json:"expected_lock_version"`
}

func bindStrictJSON(c *gin.Context, target any) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid JSON payload: " + err.Error())
	}
	return nil
}

func pricingResponse(c *gin.Context, data any, err error) {
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"data": data})
		return
	}
	status := http.StatusBadRequest
	if errors.Is(err, controlplane.ErrPricingCASConflict) {
		status = http.StatusConflict
	}
	if errors.Is(err, controlplane.ErrPricingRuleNotFound) || errors.Is(err, controlplane.ErrPricingVersionNotFound) {
		status = http.StatusNotFound
	}
	var pricingErr *pricing.Error
	if errors.As(err, &pricingErr) && pricingErr.Code == pricing.ErrorFactMissing {
		status = http.StatusUnprocessableEntity
	}
	c.JSON(status, gin.H{"error": gin.H{"code": pricingErrorCode(err), "message": err.Error()}})
}

func pricingResponseError(c *gin.Context, status int, err error) {
	c.JSON(status, gin.H{"error": gin.H{"code": "pricing_invalid_request", "message": err.Error()}})
}

func pricingErrorCode(err error) string {
	var value *pricing.Error
	if errors.As(err, &value) {
		return value.Code
	}
	if errors.Is(err, controlplane.ErrPricingCASConflict) {
		return "pricing_rule_version_conflict"
	}
	if errors.Is(err, controlplane.ErrPricingRuleNotFound) {
		return "pricing_rule_not_found"
	}
	if errors.Is(err, controlplane.ErrPricingVersionNotFound) {
		return "pricing_version_not_found"
	}
	return strconv.Itoa(http.StatusBadRequest)
}
