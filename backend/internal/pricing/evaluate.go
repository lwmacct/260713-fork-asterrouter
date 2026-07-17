package pricing

import (
	"fmt"
	"math"
	"strings"

	"github.com/expr-lang/expr"
)

func (e *Engine) Evaluate(rule CompiledRule, facts Facts) (Result, error) {
	program, err := compiledProgram(rule)
	if err != nil {
		return Result{}, err
	}
	if err := validateFacts(facts, rule.Analysis.RequiredFacts); err != nil {
		return Result{}, err
	}
	trace := evaluationTrace{lineCodes: make(map[string]struct{})}
	env := evaluationEnvironment(facts, &trace)
	out, err := expr.Run(program, env)
	if err != nil {
		return Result{}, classifyRuntimeError(err)
	}
	amount, ok := out.(int64)
	if !ok {
		return Result{}, errorf(ErrorAmountOutOfRange, "expression returned %T instead of int64", out)
	}
	if amount < 0 {
		return Result{}, errorf(ErrorAmountOutOfRange, "expression returned a negative amount")
	}
	if trace.matchedTier == "" {
		trace.matchedTier = "base"
	} else if trace.tierAmount != amount {
		return Result{}, errorf(ErrorTierInvalid, "tier amount does not equal expression result")
	}
	var lineTotal int64
	for _, line := range trace.lines {
		lineTotal, err = checkedAdd(lineTotal, line.AmountMicros)
		if err != nil {
			return Result{}, err
		}
	}
	if lineTotal != amount || (amount > 0 && len(trace.lines) == 0) {
		return Result{}, errorf(ErrorBreakdownInvalid, "line item total %d does not equal result %d", lineTotal, amount)
	}
	factsHash, err := facts.Hash()
	if err != nil {
		return Result{}, errorf(ErrorFactInvalid, "canonicalize facts: %v", err)
	}
	return Result{AmountMicros: amount, Currency: CurrencyUSD, MatchedTier: trace.matchedTier, Lines: trace.lines, EngineVersion: rule.EngineVersion, ExpressionHash: rule.ExpressionHash, FactsHash: factsHash}, nil
}

type evaluationTrace struct {
	matchedTier string
	tierAmount  int64
	lines       []PricingLine
	lineCodes   map[string]struct{}
}

func (t *evaluationTrace) addLine(line PricingLine) (int64, error) {
	line.Code = strings.TrimSpace(line.Code)
	line.Unit = strings.TrimSpace(line.Unit)
	if line.Code == "" || line.Unit == "" || !validShortString(line.Code) || !validShortString(line.Unit) {
		return 0, errorf(ErrorBreakdownInvalid, "line code and unit are required")
	}
	if len(t.lines) >= maxStaticLines {
		return 0, errorf(ErrorBreakdownInvalid, "too many line items")
	}
	if _, duplicate := t.lineCodes[line.Code]; duplicate {
		return 0, errorf(ErrorBreakdownInvalid, "duplicate line code %q", line.Code)
	}
	if line.Quantity < 0 || line.UnitsPerBlock <= 0 || line.RateMicros < 0 || line.MultiplierBPS < 0 || line.AmountMicros < 0 {
		return 0, errorf(ErrorBreakdownInvalid, "line item contains invalid values")
	}
	t.lineCodes[line.Code] = struct{}{}
	t.lines = append(t.lines, line)
	return line.AmountMicros, nil
}

func evaluationEnvironment(facts Facts, trace *evaluationTrace) map[string]any {
	env := factsEnvironment(facts)
	env["token_cost"] = numeric2(tokenCost)
	env["unit_cost"] = numeric2(unitCost)
	env["block_cost"] = numeric3(blockCost)
	env["multiply_bps"] = numeric2(multiplyBPS)
	env["token_line"] = func(code string, quantityValue, rateValue any) (int64, error) {
		quantity, err := pricingInteger(quantityValue)
		if err != nil {
			return 0, err
		}
		rate, err := pricingInteger(rateValue)
		if err != nil {
			return 0, err
		}
		amount, err := tokenCost(quantity, rate)
		if err != nil {
			return 0, err
		}
		return trace.addLine(PricingLine{Code: code, Quantity: quantity, Unit: "token", UnitsPerBlock: 1_000_000, RateMicros: rate, MultiplierBPS: basisPointsOne, AmountMicros: amount})
	}
	env["adjusted_token_line"] = func(code string, quantityValue, rateValue, bpsValue any) (int64, error) {
		quantity, err := pricingInteger(quantityValue)
		if err != nil {
			return 0, err
		}
		rate, err := pricingInteger(rateValue)
		if err != nil {
			return 0, err
		}
		bps, err := pricingInteger(bpsValue)
		if err != nil {
			return 0, err
		}
		base, err := tokenCost(quantity, rate)
		if err != nil {
			return 0, err
		}
		amount, err := multiplyBPS(base, bps)
		if err != nil {
			return 0, err
		}
		return trace.addLine(PricingLine{Code: code, Quantity: quantity, Unit: "token", UnitsPerBlock: 1_000_000, RateMicros: rate, MultiplierBPS: bps, AmountMicros: amount})
	}
	env["unit_line"] = func(code string, quantityValue any, unit string, rateValue any) (int64, error) {
		quantity, err := pricingInteger(quantityValue)
		if err != nil {
			return 0, err
		}
		rate, err := pricingInteger(rateValue)
		if err != nil {
			return 0, err
		}
		amount, err := unitCost(quantity, rate)
		if err != nil {
			return 0, err
		}
		return trace.addLine(PricingLine{Code: code, Quantity: quantity, Unit: unit, UnitsPerBlock: 1, RateMicros: rate, MultiplierBPS: basisPointsOne, AmountMicros: amount})
	}
	env["adjusted_unit_line"] = func(code string, quantityValue any, unit string, rateValue, bpsValue any) (int64, error) {
		quantity, err := pricingInteger(quantityValue)
		if err != nil {
			return 0, err
		}
		rate, err := pricingInteger(rateValue)
		if err != nil {
			return 0, err
		}
		bps, err := pricingInteger(bpsValue)
		if err != nil {
			return 0, err
		}
		base, err := unitCost(quantity, rate)
		if err != nil {
			return 0, err
		}
		amount, err := multiplyBPS(base, bps)
		if err != nil {
			return 0, err
		}
		return trace.addLine(PricingLine{Code: code, Quantity: quantity, Unit: unit, UnitsPerBlock: 1, RateMicros: rate, MultiplierBPS: bps, AmountMicros: amount})
	}
	env["block_line"] = func(code string, quantityValue any, unit string, unitsPerBlockValue, rateValue any) (int64, error) {
		quantity, err := pricingInteger(quantityValue)
		if err != nil {
			return 0, err
		}
		unitsPerBlock, err := pricingInteger(unitsPerBlockValue)
		if err != nil {
			return 0, err
		}
		rate, err := pricingInteger(rateValue)
		if err != nil {
			return 0, err
		}
		amount, err := blockCost(quantity, unitsPerBlock, rate)
		if err != nil {
			return 0, err
		}
		return trace.addLine(PricingLine{Code: code, Quantity: quantity, Unit: unit, UnitsPerBlock: unitsPerBlock, RateMicros: rate, MultiplierBPS: basisPointsOne, AmountMicros: amount})
	}
	env["adjusted_block_line"] = func(code string, quantityValue any, unit string, unitsPerBlockValue, rateValue, bpsValue any) (int64, error) {
		quantity, err := pricingInteger(quantityValue)
		if err != nil {
			return 0, err
		}
		unitsPerBlock, err := pricingInteger(unitsPerBlockValue)
		if err != nil {
			return 0, err
		}
		rate, err := pricingInteger(rateValue)
		if err != nil {
			return 0, err
		}
		bps, err := pricingInteger(bpsValue)
		if err != nil {
			return 0, err
		}
		base, err := blockCost(quantity, unitsPerBlock, rate)
		if err != nil {
			return 0, err
		}
		amount, err := multiplyBPS(base, bps)
		if err != nil {
			return 0, err
		}
		return trace.addLine(PricingLine{Code: code, Quantity: quantity, Unit: unit, UnitsPerBlock: unitsPerBlock, RateMicros: rate, MultiplierBPS: bps, AmountMicros: amount})
	}
	env["fixed_line"] = func(code, unit string, amountValue any) (int64, error) {
		amount, err := pricingInteger(amountValue)
		if err != nil {
			return 0, err
		}
		return trace.addLine(PricingLine{Code: code, Quantity: 1, Unit: unit, UnitsPerBlock: 1, RateMicros: amount, MultiplierBPS: basisPointsOne, AmountMicros: amount})
	}
	env["tier"] = func(name string, amountValue any) (int64, error) {
		amount, err := pricingInteger(amountValue)
		if err != nil {
			return 0, err
		}
		name = strings.TrimSpace(name)
		if name == "" || !validShortString(name) || trace.matchedTier != "" {
			return 0, errorf(ErrorTierInvalid, "exactly one valid tier may be selected")
		}
		if amount < 0 {
			return 0, errorf(ErrorTierInvalid, "tier amount must be non-negative")
		}
		trace.matchedTier, trace.tierAmount = name, amount
		return amount, nil
	}
	env["min"] = func(leftValue, rightValue any) (int64, error) {
		left, err := pricingInteger(leftValue)
		if err != nil {
			return 0, err
		}
		right, err := pricingInteger(rightValue)
		if err != nil {
			return 0, err
		}
		if left < right {
			return left, nil
		}
		return right, nil
	}
	env["max"] = func(leftValue, rightValue any) (int64, error) {
		left, err := pricingInteger(leftValue)
		if err != nil {
			return 0, err
		}
		right, err := pricingInteger(rightValue)
		if err != nil {
			return 0, err
		}
		if left > right {
			return left, nil
		}
		return right, nil
	}
	return env
}

func factsEnvironment(f Facts) map[string]any {
	return map[string]any{
		"total_input_tokens": f.TotalInputTokens, "uncached_input_tokens": f.UncachedInputTokens,
		"cache_read_tokens": f.CacheReadTokens, "cache_write_5m_tokens": f.CacheWrite5mTokens,
		"cache_write_1h_tokens": f.CacheWrite1hTokens, "output_tokens": f.OutputTokens,
		"cache_fields_present": f.CacheFieldsPresent, "input_images": f.InputImages, "output_images": f.OutputImages,
		"partial_images": f.PartialImages, "input_video_milliseconds": f.InputVideoMilliseconds,
		"output_video_milliseconds": f.OutputVideoMilliseconds, "input_audio_milliseconds": f.InputAudioMilliseconds,
		"output_audio_milliseconds": f.OutputAudioMilliseconds, "realtime_audio_milliseconds": f.RealtimeAudioMilliseconds,
		"input_characters": f.InputCharacters, "actions": f.Actions, "batch_items": f.BatchItems,
		"input_bytes": f.InputBytes, "output_bytes": f.OutputBytes, "transfer_bytes": f.TransferBytes,
		"session_milliseconds": f.SessionMilliseconds, "protocol": f.Protocol, "operation": f.Operation,
		"modality": f.Modality, "lane": f.Lane, "stream": f.Stream, "output_count": f.OutputCount, "service_tier": f.ServiceTier,
	}
}

func validateFacts(f Facts, required []string) error {
	values := []int64{
		f.TotalInputTokens, f.UncachedInputTokens, f.CacheReadTokens, f.CacheWrite5mTokens, f.CacheWrite1hTokens,
		f.OutputTokens, f.InputImages, f.OutputImages, f.PartialImages, f.InputVideoMilliseconds,
		f.OutputVideoMilliseconds, f.InputAudioMilliseconds, f.OutputAudioMilliseconds, f.RealtimeAudioMilliseconds,
		f.InputCharacters, f.Actions, f.BatchItems, f.InputBytes, f.OutputBytes, f.TransferBytes, f.SessionMilliseconds, f.OutputCount,
	}
	for _, value := range values {
		if value < 0 {
			return errorf(ErrorFactInvalid, "pricing facts must be non-negative")
		}
	}
	if f.UncachedInputTokens > f.TotalInputTokens && f.TotalInputTokens > 0 {
		return errorf(ErrorFactInvalid, "uncached input exceeds total input")
	}
	for _, value := range []string{f.Protocol, f.Operation, f.Modality, f.Lane, f.ServiceTier, f.NormalizationStatus} {
		if !validShortString(value) {
			return errorf(ErrorFactInvalid, "pricing fact string is invalid or too long")
		}
	}
	if f.Phase != "" && f.Phase != PhaseEstimate && f.Phase != PhaseSettlement && f.Phase != PhaseReplay {
		return errorf(ErrorFactInvalid, "pricing phase is invalid")
	}
	if len(f.AvailableFacts) > 0 {
		for _, name := range required {
			if !f.AvailableFacts[name] {
				return errorf(ErrorFactMissing, "required fact %q is unavailable", name)
			}
		}
	}
	return nil
}

func classifyRuntimeError(err error) error {
	var pricingError *Error
	if strings.Contains(err.Error(), ErrorArithmeticOverflow) {
		return errorf(ErrorArithmeticOverflow, "%v", err)
	}
	if strings.Contains(err.Error(), ErrorBreakdownInvalid) {
		return errorf(ErrorBreakdownInvalid, "%v", err)
	}
	if strings.Contains(err.Error(), ErrorTierInvalid) {
		return errorf(ErrorTierInvalid, "%v", err)
	}
	if ok := asPricingError(err, &pricingError); ok {
		return pricingError
	}
	return errorf(ErrorAmountOutOfRange, "expression execution failed: %v", err)
}

func asPricingError(err error, target **Error) bool {
	for err != nil {
		if value, ok := err.(*Error); ok {
			*target = value
			return true
		}
		type unwrapper interface{ Unwrap() error }
		value, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = value.Unwrap()
	}
	return false
}

var _ = fmt.Sprintf
var _ = math.MaxInt64
