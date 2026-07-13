package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const pricingUnitTokens = 1_000_000

func (s *Service) ListModelPricings(ctx context.Context) ([]ModelPricing, error) {
	return s.repo.ListModelPricings(ctx)
}

func (s *Service) CreateModelPricing(ctx context.Context, actor string, req ModelPricingRequest) (ModelPricing, error) {
	now := time.Now().UTC()
	pricing, err := modelPricingFromRequest(req, now)
	if err != nil {
		return ModelPricing{}, err
	}
	if err := s.ensureModelPricingModelAvailable(ctx, "", pricing.Model); err != nil {
		return ModelPricing{}, err
	}
	pricing.ID = "price_" + randomID(10)
	if err := s.repo.SaveModelPricing(ctx, pricing); err != nil {
		return ModelPricing{}, err
	}
	if err := s.audit(ctx, actor, "create", "model_pricing", pricing.ID, fmt.Sprintf("Created pricing for model %s", pricing.Model)); err != nil {
		return ModelPricing{}, err
	}
	_ = s.PublishCustomerBroadcast(ctx, CustomerNotificationModelUpdate, "模型价格已发布", fmt.Sprintf("模型 %s 的价格规则已发布。", pricing.Model), "/customer/integration", "pricing:create:"+pricing.ID)
	return pricing, nil
}

func (s *Service) UpdateModelPricing(ctx context.Context, actor string, id string, req ModelPricingRequest) (ModelPricing, error) {
	existing, err := s.modelPricingByID(ctx, id)
	if err != nil {
		return ModelPricing{}, err
	}
	pricing, err := modelPricingFromRequest(req, existing.CreatedAt)
	if err != nil {
		return ModelPricing{}, err
	}
	pricing.ID = existing.ID
	pricing.CreatedAt = existing.CreatedAt
	pricing.UpdatedAt = time.Now().UTC()
	if err := s.ensureModelPricingModelAvailable(ctx, existing.ID, pricing.Model); err != nil {
		return ModelPricing{}, err
	}
	if err := s.repo.SaveModelPricing(ctx, pricing); err != nil {
		return ModelPricing{}, err
	}
	if err := s.audit(ctx, actor, "update", "model_pricing", pricing.ID, fmt.Sprintf("Updated pricing for model %s", pricing.Model)); err != nil {
		return ModelPricing{}, err
	}
	_ = s.PublishCustomerBroadcast(ctx, CustomerNotificationModelUpdate, "模型价格已调整", fmt.Sprintf("模型 %s 的价格规则已更新。", pricing.Model), "/customer/integration", "pricing:update:"+pricing.ID+":"+pricing.UpdatedAt.Format(time.RFC3339Nano))
	return pricing, nil
}

func (s *Service) EstimateModelUsageCostCents(ctx context.Context, model string, inputTokens int, outputTokens int) (int, bool, error) {
	pricing, ok, err := s.modelPricingForModel(ctx, model)
	if err != nil || !ok {
		return 0, ok, err
	}
	return estimateCostCents(pricing, inputTokens, outputTokens), true, nil
}

func (s *Service) ensureModelPricingModelAvailable(ctx context.Context, currentID string, model string) error {
	pricings, err := s.repo.ListModelPricings(ctx)
	if err != nil {
		return err
	}
	for _, pricing := range pricings {
		if pricing.Model == model && pricing.ID != currentID {
			return errors.New("model pricing already exists")
		}
	}
	return nil
}

func (s *Service) modelPricingByID(ctx context.Context, id string) (ModelPricing, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ModelPricing{}, errors.New("model pricing id is required")
	}
	pricings, err := s.repo.ListModelPricings(ctx)
	if err != nil {
		return ModelPricing{}, err
	}
	for _, pricing := range pricings {
		if pricing.ID == id {
			return pricing, nil
		}
	}
	return ModelPricing{}, errors.New("model pricing not found")
}

func (s *Service) modelPricingForModel(ctx context.Context, model string) (ModelPricing, bool, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return ModelPricing{}, false, nil
	}
	pricings, err := s.repo.ListModelPricings(ctx)
	if err != nil {
		return ModelPricing{}, false, err
	}
	for _, pricing := range pricings {
		if pricing.Model == model && pricing.Status == ModelPricingStatusActive {
			return pricing, true, nil
		}
	}
	return ModelPricing{}, false, nil
}

func modelPricingFromRequest(req ModelPricingRequest, createdAt time.Time) (ModelPricing, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return ModelPricing{}, errors.New("model is required")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = ModelPricingStatusActive
	}
	if status != ModelPricingStatusActive && status != ModelPricingStatusDisabled {
		return ModelPricing{}, errors.New("invalid model pricing status")
	}
	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if currency == "" {
		currency = "USD"
	}
	if len(currency) != 3 {
		return ModelPricing{}, errors.New("currency must be a 3-letter code")
	}
	if req.InputPriceCentsPer1MTokens < 0 || req.OutputPriceCentsPer1MTokens < 0 {
		return ModelPricing{}, errors.New("model pricing cannot be negative")
	}
	now := time.Now().UTC()
	if createdAt.IsZero() {
		createdAt = now
	}
	return ModelPricing{
		Model:                       model,
		Currency:                    currency,
		InputPriceCentsPer1MTokens:  req.InputPriceCentsPer1MTokens,
		OutputPriceCentsPer1MTokens: req.OutputPriceCentsPer1MTokens,
		Status:                      status,
		CreatedAt:                   createdAt,
		UpdatedAt:                   now,
	}, nil
}

func estimateCostCents(pricing ModelPricing, inputTokens int, outputTokens int) int {
	inputTokens = nonNegative(inputTokens)
	outputTokens = nonNegative(outputTokens)
	total := inputTokens*nonNegative(pricing.InputPriceCentsPer1MTokens) + outputTokens*nonNegative(pricing.OutputPriceCentsPer1MTokens)
	if total <= 0 {
		return 0
	}
	return (total + pricingUnitTokens - 1) / pricingUnitTokens
}
