package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *Service) ListGatewayModels(ctx context.Context) ([]GatewayModel, error) {
	return s.repo.ListGatewayModels(ctx)
}

func (s *Service) CreateGatewayModel(ctx context.Context, actor string, req GatewayModelRequest) (GatewayModel, error) {
	model, err := gatewayModelFromRequest(req, time.Now().UTC())
	if err != nil {
		return GatewayModel{}, err
	}
	if err := s.ensureGatewayModelIDUnique(ctx, model.ModelID, ""); err != nil {
		return GatewayModel{}, err
	}
	model.ID = "gmodel_" + randomID(10)
	if err := s.repo.SaveGatewayModel(ctx, model); err != nil {
		return GatewayModel{}, err
	}
	if err := s.audit(ctx, actor, "create", "gateway_model", model.ID, fmt.Sprintf("Created gateway model %s", model.ModelID)); err != nil {
		return GatewayModel{}, err
	}
	_ = s.PublishCustomerBroadcast(ctx, CustomerNotificationModelUpdate, "新模型已上架", fmt.Sprintf("模型 %s 已加入可用模型目录。", model.ModelID), "/customer/integration", "model:create:"+model.ID)
	return model, nil
}

func (s *Service) UpdateGatewayModel(ctx context.Context, actor string, id string, req GatewayModelRequest) (GatewayModel, error) {
	existing, err := s.gatewayModelByID(ctx, id)
	if err != nil {
		return GatewayModel{}, err
	}
	model, err := gatewayModelFromRequest(req, existing.CreatedAt)
	if err != nil {
		return GatewayModel{}, err
	}
	if err := s.ensureGatewayModelIDUnique(ctx, model.ModelID, existing.ID); err != nil {
		return GatewayModel{}, err
	}
	model.ID = existing.ID
	model.CreatedAt = existing.CreatedAt
	model.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveGatewayModel(ctx, model); err != nil {
		return GatewayModel{}, err
	}
	if err := s.audit(ctx, actor, "update", "gateway_model", model.ID, fmt.Sprintf("Updated gateway model %s", model.ModelID)); err != nil {
		return GatewayModel{}, err
	}
	_ = s.PublishCustomerBroadcast(ctx, CustomerNotificationModelUpdate, "模型信息已更新", fmt.Sprintf("模型 %s 的可用状态或接入信息已更新。", model.ModelID), "/customer/integration", "model:update:"+model.ID+":"+model.UpdatedAt.Format(time.RFC3339Nano))
	return model, nil
}

func (s *Service) DeleteGatewayModel(ctx context.Context, actor string, id string) error {
	model, err := s.gatewayModelByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteGatewayModel(ctx, model.ID); err != nil {
		return err
	}
	if err := s.audit(ctx, actor, "delete", "gateway_model", model.ID, fmt.Sprintf("Deleted gateway model %s and its routes", model.ModelID)); err != nil {
		return err
	}
	_ = s.PublishCustomerBroadcast(ctx, CustomerNotificationModelUpdate, "模型已下架", fmt.Sprintf("模型 %s 已从可用模型目录移除。", model.ModelID), "/customer/integration", "model:delete:"+model.ID+":"+time.Now().UTC().Format(time.RFC3339Nano))
	return nil
}

func (s *Service) ListModelRoutes(ctx context.Context) ([]ModelRoute, error) {
	return s.repo.ListModelRoutes(ctx)
}

func (s *Service) CreateModelRoute(ctx context.Context, actor string, req ModelRouteRequest) (ModelRoute, error) {
	route, err := s.modelRouteFromRequest(ctx, req, time.Now().UTC())
	if err != nil {
		return ModelRoute{}, err
	}
	if err := s.ensureModelRouteUnique(ctx, route, ""); err != nil {
		return ModelRoute{}, err
	}
	route.ID = "mroute_" + randomID(10)
	if err := s.repo.SaveModelRoute(ctx, route); err != nil {
		return ModelRoute{}, err
	}
	if err := s.audit(ctx, actor, "create", "model_route", route.ID, fmt.Sprintf("Created model route to %s", route.UpstreamModel)); err != nil {
		return ModelRoute{}, err
	}
	return route, nil
}

func (s *Service) UpdateModelRoute(ctx context.Context, actor string, id string, req ModelRouteRequest) (ModelRoute, error) {
	existing, err := s.modelRouteByID(ctx, id)
	if err != nil {
		return ModelRoute{}, err
	}
	route, err := s.modelRouteFromRequest(ctx, req, existing.CreatedAt)
	if err != nil {
		return ModelRoute{}, err
	}
	if err := s.ensureModelRouteUnique(ctx, route, existing.ID); err != nil {
		return ModelRoute{}, err
	}
	route.ID = existing.ID
	route.CreatedAt = existing.CreatedAt
	route.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveModelRoute(ctx, route); err != nil {
		return ModelRoute{}, err
	}
	if err := s.audit(ctx, actor, "update", "model_route", route.ID, fmt.Sprintf("Updated model route to %s", route.UpstreamModel)); err != nil {
		return ModelRoute{}, err
	}
	return route, nil
}

func (s *Service) DeleteModelRoute(ctx context.Context, actor string, id string) error {
	route, err := s.modelRouteByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteModelRoute(ctx, route.ID); err != nil {
		return err
	}
	return s.audit(ctx, actor, "delete", "model_route", route.ID, fmt.Sprintf("Deleted model route to %s", route.UpstreamModel))
}

func (s *Service) ResolveGatewayModel(ctx context.Context, requestedID string) (ResolvedGatewayModel, bool, error) {
	requestedID = strings.TrimSpace(requestedID)
	if requestedID == "" {
		return ResolvedGatewayModel{}, false, nil
	}
	models, err := s.repo.ListGatewayModels(ctx)
	if err != nil {
		return ResolvedGatewayModel{}, false, err
	}
	for _, model := range models {
		if model.Status == GatewayModelStatusActive && model.ModelID == requestedID {
			return ResolvedGatewayModel{GatewayModel: model, RequestedID: requestedID, RouteGroup: model.DefaultRouteGroup}, true, nil
		}
	}
	separator := strings.LastIndex(requestedID, ":")
	if separator <= 0 || separator == len(requestedID)-1 {
		return ResolvedGatewayModel{}, false, nil
	}
	modelID := requestedID[:separator]
	routeGroup := requestedID[separator+1:]
	for _, model := range models {
		if model.Status == GatewayModelStatusActive && model.ModelID == modelID {
			return ResolvedGatewayModel{GatewayModel: model, RequestedID: requestedID, RouteGroup: routeGroup}, true, nil
		}
	}
	return ResolvedGatewayModel{}, false, nil
}

func gatewayModelFromRequest(req GatewayModelRequest, now time.Time) (GatewayModel, error) {
	modelID := strings.TrimSpace(req.ModelID)
	name := strings.TrimSpace(req.Name)
	modality := strings.TrimSpace(req.Modality)
	defaultRouteGroup := strings.TrimSpace(req.DefaultRouteGroup)
	status := strings.TrimSpace(req.Status)
	if modelID == "" {
		return GatewayModel{}, errors.New("model_id is required")
	}
	if strings.ContainsAny(modelID, " \t\r\n") {
		return GatewayModel{}, errors.New("model_id must not contain whitespace")
	}
	if name == "" {
		name = modelID
	}
	if modality == "" {
		modality = "chat"
	}
	if !oneOf(modality, "chat", "embedding", "image", "video", "audio", "multimodal") {
		return GatewayModel{}, errors.New("modality must be chat, embedding, image, video, audio, or multimodal")
	}
	if defaultRouteGroup == "" {
		defaultRouteGroup = DefaultModelRouteGroup
	}
	if strings.ContainsAny(defaultRouteGroup, " :\t\r\n") {
		return GatewayModel{}, errors.New("default_route_group must not contain whitespace or colon")
	}
	if status == "" {
		status = GatewayModelStatusActive
	}
	if !oneOf(status, GatewayModelStatusActive, GatewayModelStatusDisabled) {
		return GatewayModel{}, errors.New("status must be active or disabled")
	}
	stickyTTLSeconds := req.StickyTTLSeconds
	if stickyTTLSeconds == 0 {
		stickyTTLSeconds = 1800
	}
	if stickyTTLSeconds < 60 || stickyTTLSeconds > 604800 {
		return GatewayModel{}, errors.New("sticky_ttl_seconds must be between 60 and 604800")
	}
	return GatewayModel{
		ModelID:           modelID,
		Name:              name,
		Description:       strings.TrimSpace(req.Description),
		Modality:          modality,
		DefaultRouteGroup: defaultRouteGroup,
		StickyEnabled:     req.StickyEnabled,
		StickyTTLSeconds:  stickyTTLSeconds,
		Status:            status,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

func (s *Service) modelRouteFromRequest(ctx context.Context, req ModelRouteRequest, now time.Time) (ModelRoute, error) {
	gatewayModelID := strings.TrimSpace(req.GatewayModelID)
	routeGroup := strings.TrimSpace(req.RouteGroup)
	providerAccountID := strings.TrimSpace(req.ProviderAccountID)
	upstreamModel := strings.TrimSpace(req.UpstreamModel)
	status := strings.TrimSpace(req.Status)
	if _, err := s.gatewayModelByID(ctx, gatewayModelID); err != nil {
		return ModelRoute{}, err
	}
	account, err := s.providerAccountByID(ctx, providerAccountID)
	if err != nil {
		return ModelRoute{}, err
	}
	if routeGroup == "" {
		routeGroup = DefaultModelRouteGroup
	}
	if strings.ContainsAny(routeGroup, " :\t\r\n") {
		return ModelRoute{}, errors.New("route_group must not contain whitespace or colon")
	}
	if upstreamModel == "" {
		return ModelRoute{}, errors.New("upstream_model is required")
	}
	if !contains(account.Models, upstreamModel) {
		return ModelRoute{}, fmt.Errorf("provider account %q does not expose upstream model %q", account.ID, upstreamModel)
	}
	if req.Priority < 0 {
		return ModelRoute{}, errors.New("priority must be greater than or equal to 0")
	}
	weight := req.Weight
	if weight == 0 {
		weight = 100
	}
	if weight < 1 || weight > 10000 {
		return ModelRoute{}, errors.New("weight must be between 1 and 10000")
	}
	if status == "" {
		status = ModelRouteStatusActive
	}
	if !oneOf(status, ModelRouteStatusActive, ModelRouteStatusDisabled) {
		return ModelRoute{}, errors.New("status must be active or disabled")
	}
	return ModelRoute{
		GatewayModelID:    gatewayModelID,
		RouteGroup:        routeGroup,
		ProviderAccountID: providerAccountID,
		UpstreamModel:     upstreamModel,
		Priority:          req.Priority,
		Weight:            weight,
		Status:            status,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

func (s *Service) gatewayModelByID(ctx context.Context, id string) (GatewayModel, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return GatewayModel{}, errors.New("gateway model id is required")
	}
	models, err := s.repo.ListGatewayModels(ctx)
	if err != nil {
		return GatewayModel{}, err
	}
	for _, model := range models {
		if model.ID == id {
			return model, nil
		}
	}
	return GatewayModel{}, fmt.Errorf("gateway model %q not found", id)
}

func (s *Service) modelRouteByID(ctx context.Context, id string) (ModelRoute, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ModelRoute{}, errors.New("model route id is required")
	}
	routes, err := s.repo.ListModelRoutes(ctx)
	if err != nil {
		return ModelRoute{}, err
	}
	for _, route := range routes {
		if route.ID == id {
			return route, nil
		}
	}
	return ModelRoute{}, fmt.Errorf("model route %q not found", id)
}

func (s *Service) ensureGatewayModelIDUnique(ctx context.Context, modelID string, exceptID string) error {
	models, err := s.repo.ListGatewayModels(ctx)
	if err != nil {
		return err
	}
	for _, model := range models {
		if model.ID != exceptID && model.ModelID == modelID {
			return fmt.Errorf("gateway model_id %q already exists", modelID)
		}
	}
	return nil
}

func (s *Service) ensureModelRouteUnique(ctx context.Context, candidate ModelRoute, exceptID string) error {
	routes, err := s.repo.ListModelRoutes(ctx)
	if err != nil {
		return err
	}
	for _, route := range routes {
		if route.ID == exceptID {
			continue
		}
		if route.GatewayModelID == candidate.GatewayModelID && route.RouteGroup == candidate.RouteGroup && route.ProviderAccountID == candidate.ProviderAccountID && route.UpstreamModel == candidate.UpstreamModel {
			return errors.New("an equivalent model route already exists")
		}
	}
	return nil
}

type rankedModelRouteCandidate struct {
	route        ModelRoute
	account      ProviderAccount
	provider     ProviderConnection
	loadRatio    float64
	headroom     float64
	weightScore  float64
	circuitState string
	circuitProbe bool
}

func (s *Service) rankedModelRouteCandidates(ctx context.Context, resolved ResolvedGatewayModel) ([]rankedModelRouteCandidate, bool, error) {
	routes, err := s.repo.ListModelRoutes(ctx)
	if err != nil {
		return nil, false, err
	}
	matchingRoutes := make([]ModelRoute, 0)
	for _, route := range routes {
		if route.GatewayModelID == resolved.GatewayModel.ID && route.RouteGroup == resolved.RouteGroup {
			matchingRoutes = append(matchingRoutes, route)
		}
	}
	if len(matchingRoutes) == 0 {
		return nil, false, nil
	}
	accounts, err := s.repo.ListProviderAccounts(ctx)
	if err != nil {
		return nil, true, err
	}
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return nil, true, err
	}
	accountsByID := make(map[string]ProviderAccount, len(accounts))
	for _, account := range accounts {
		accountsByID[account.ID] = account
	}
	providersByID := providerByIDMap(providers)
	now := time.Now().UTC()
	billingHealthByAccount, _ := s.providerBillingRoutingHealthByAccount(ctx, now)
	candidates := make([]rankedModelRouteCandidate, 0, len(matchingRoutes))
	for _, route := range matchingRoutes {
		if route.Status != ModelRouteStatusActive {
			continue
		}
		account, ok := accountsByID[route.ProviderAccountID]
		if !ok || !accountEligibleForRouting(account, route.UpstreamModel, now) {
			continue
		}
		if health, found := billingHealthByAccount[account.ID]; found && health.HardBlocked {
			continue
		}
		provider, ok := providersByID[account.ProviderID]
		if !ok || provider.Status == ProviderStatusDisabled || !validHTTPURL(provider.BaseURL) {
			continue
		}
		circuitState, circuitProbe, eligible := effectiveCircuitState(account, now)
		if !eligible {
			continue
		}
		loadRatio := float64(s.providerAccountSlotUsage(account.ID)) / float64(account.EffectiveLoadFactor())
		headroom := s.providerAccountRateHeadroom(account, now)
		candidates = append(candidates, rankedModelRouteCandidate{
			route: route, account: account, provider: provider, loadRatio: loadRatio,
			headroom: headroom, weightScore: weightedCandidateScore(route.Weight * account.Weight),
			circuitState: circuitState, circuitProbe: circuitProbe,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.route.Priority != right.route.Priority {
			return left.route.Priority < right.route.Priority
		}
		if left.account.Priority != right.account.Priority {
			return left.account.Priority < right.account.Priority
		}
		if left.circuitProbe != right.circuitProbe {
			return !left.circuitProbe
		}
		if left.headroom != right.headroom {
			return left.headroom > right.headroom
		}
		if left.loadRatio != right.loadRatio {
			return left.loadRatio < right.loadRatio
		}
		if left.weightScore != right.weightScore {
			return left.weightScore < right.weightScore
		}
		if left.account.RateMultiplier != right.account.RateMultiplier {
			return left.account.RateMultiplier < right.account.RateMultiplier
		}
		return left.route.ID < right.route.ID
	})
	return candidates, true, nil
}

func effectiveCircuitState(account ProviderAccount, now time.Time) (state string, probe bool, eligible bool) {
	state = account.CircuitState
	if state == "" {
		state = CircuitStateClosed
	}
	switch state {
	case CircuitStateClosed:
		return state, false, true
	case CircuitStateOpen:
		if account.CircuitOpenedUntil != nil && !now.Before(*account.CircuitOpenedUntil) {
			return CircuitStateHalfOpen, true, true
		}
		return state, false, false
	case CircuitStateHalfOpen:
		return state, true, true
	default:
		return state, false, false
	}
}
