package controlplane

import (
	"context"
	"math"
	"strings"
	"time"
)

type gatewayProcurementCostEstimate struct {
	CostMicros int64
	Currency   string
	Source     string
	Confidence string
	PriceID    string
}

func (s *Service) estimateGatewayProcurementCost(ctx context.Context, input GatewayUsageInput, at time.Time) (gatewayProcurementCostEstimate, bool, error) {
	if strings.TrimSpace(input.ProviderAccountID) == "" || strings.TrimSpace(input.UpstreamModel) == "" || strings.TrimSpace(input.Protocol) == "" {
		return gatewayProcurementCostEstimate{}, false, nil
	}
	prices, err := s.repo.ListProcurementPrices(ctx)
	if err != nil {
		return gatewayProcurementCostEstimate{}, false, err
	}
	price, found := activeProcurementPrice(prices, input, at)
	if !found {
		return gatewayProcurementCostEstimate{}, false, nil
	}
	totalInputTokens := nonNegative(input.InputTokens)
	if input.TotalInputTokens != nil {
		totalInputTokens = nonNegative(*input.TotalInputTokens)
	}
	uncachedInputTokens := totalInputTokens
	cacheReadTokens := 0
	cacheWrite5mTokens := 0
	cacheWrite1hTokens := 0
	if input.CacheFieldsPresent {
		if input.UncachedInputTokens != nil {
			uncachedInputTokens = nonNegative(*input.UncachedInputTokens)
		}
		if input.CacheReadTokens != nil {
			cacheReadTokens = nonNegative(*input.CacheReadTokens)
		}
		if input.CacheWrite5mTokens != nil {
			cacheWrite5mTokens = nonNegative(*input.CacheWrite5mTokens)
		}
		if input.CacheWrite1hTokens != nil {
			cacheWrite1hTokens = nonNegative(*input.CacheWrite1hTokens)
		}
	}
	costMicros := price.RequestMicros
	components := []struct {
		tokens int
		rate   int64
	}{
		{uncachedInputTokens, price.UncachedInputMicrosPer1MTokens},
		{cacheReadTokens, price.CacheReadMicrosPer1MTokens},
		{cacheWrite5mTokens, price.CacheWrite5mMicrosPer1MTokens},
		{cacheWrite1hTokens, price.CacheWrite1hMicrosPer1MTokens},
		{nonNegative(input.OutputTokens), price.OutputMicrosPer1MTokens},
	}
	for _, component := range components {
		value, ok := tokenCostMicros(component.tokens, component.rate)
		if !ok || value > math.MaxInt64-costMicros {
			return gatewayProcurementCostEstimate{}, false, nil
		}
		costMicros += value
	}
	confidence := strings.TrimSpace(price.Confidence)
	if confidence == "" {
		confidence = ProcurementCostConfidenceEstimated
	}
	currency := strings.ToUpper(strings.TrimSpace(price.Currency))
	if currency == "" {
		currency = "USD"
	}
	return gatewayProcurementCostEstimate{
		CostMicros: costMicros,
		Currency:   currency,
		Source:     "procurement_price",
		Confidence: confidence,
		PriceID:    price.ID,
	}, true, nil
}

func activeProcurementPrice(prices []ProcurementPrice, input GatewayUsageInput, at time.Time) (ProcurementPrice, bool) {
	var selected ProcurementPrice
	found := false
	for _, price := range prices {
		if price.Status != ProcurementPriceStatusActive || price.ProviderAccountID != strings.TrimSpace(input.ProviderAccountID) || price.UpstreamModel != strings.TrimSpace(input.UpstreamModel) || price.Protocol != strings.TrimSpace(input.Protocol) {
			continue
		}
		if providerID := strings.TrimSpace(input.ProviderID); providerID != "" && price.ProviderID != providerID {
			continue
		}
		if price.EffectiveFrom.After(at) || (price.ExpiresAt != nil && !price.ExpiresAt.After(at)) {
			continue
		}
		if !found || price.EffectiveFrom.After(selected.EffectiveFrom) {
			selected = price
			found = true
		}
	}
	return selected, found
}

func tokenCostMicros(tokens int, microsPerMillion int64) (int64, bool) {
	if tokens < 0 || microsPerMillion < 0 {
		return 0, false
	}
	if tokens == 0 || microsPerMillion == 0 {
		return 0, true
	}
	if int64(tokens) > math.MaxInt64/microsPerMillion {
		return 0, false
	}
	product := int64(tokens) * microsPerMillion
	if product > math.MaxInt64-500_000 {
		return 0, false
	}
	return (product + 500_000) / 1_000_000, true
}

func nonNegativeIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	normalized := nonNegative(*value)
	return &normalized
}

func nonNegativeInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	normalized := *value
	if normalized < 0 {
		normalized = 0
	}
	return &normalized
}
