package pricing

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestEvaluateTokenRuleProducesBalancedBreakdown(t *testing.T) {
	engine := NewEngine()
	rule, err := engine.Compile(`v1:tier("base", token_line("input", uncached_input_tokens, 3000000) + token_line("output", output_tokens, 15000000))`)
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	result, err := engine.Evaluate(rule, Facts{TotalInputTokens: 1000, UncachedInputTokens: 1000, OutputTokens: 10})
	if err != nil {
		t.Fatalf("Evaluate(): %v", err)
	}
	if result.AmountMicros != 3150 || result.MatchedTier != "base" || len(result.Lines) != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.ExpressionHash == "" || result.FactsHash == "" || result.Currency != CurrencyUSD {
		t.Fatalf("missing result evidence: %+v", result)
	}
}

func TestEvaluateTierBoundary(t *testing.T) {
	engine := NewEngine()
	rule, err := engine.Compile(`v1:total_input_tokens <= 200000 ? tier("standard", token_line("input", uncached_input_tokens, 3000000)) : tier("long", token_line("input", uncached_input_tokens, 6000000))`)
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	for _, test := range []struct {
		input int64
		tier  string
		cost  int64
	}{{200000, "standard", 600000}, {200001, "long", 1200006}} {
		result, evalErr := engine.Evaluate(rule, Facts{TotalInputTokens: test.input, UncachedInputTokens: test.input})
		if evalErr != nil {
			t.Fatalf("Evaluate(%d): %v", test.input, evalErr)
		}
		if result.MatchedTier != test.tier || result.AmountMicros != test.cost {
			t.Fatalf("Evaluate(%d)=%+v", test.input, result)
		}
	}
}

func TestCompileRejectsUnsafeExpressions(t *testing.T) {
	engine := NewEngine()
	tests := []struct {
		name   string
		source string
		code   string
	}{
		{"missing version", `token_line("input", uncached_input_tokens, 1)`, ErrorVersionUnsupported},
		{"float", `v1:1.5`, ErrorForbiddenAST},
		{"multiply", `v1:uncached_input_tokens * 2`, ErrorForbiddenAST},
		{"header", `v1:header("x")`, ErrorForbiddenAST},
		{"member", `v1:foo.bar`, ErrorForbiddenAST},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := engine.Compile(test.source)
			var pricingErr *Error
			if !errors.As(err, &pricingErr) || pricingErr.Code != test.code {
				t.Fatalf("Compile() error=%v, want %s", err, test.code)
			}
		})
	}
}

func TestCompileByHashRejectsMismatchBeforeCache(t *testing.T) {
	engine := NewEngine()
	source := `v1:fixed_line("request", "request", 1)`
	if _, err := engine.Compile(source); err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	_, err := engine.CompileByHash(source, ExpressionHash(source+" "))
	var pricingErr *Error
	if !errors.As(err, &pricingErr) || pricingErr.Code != ErrorHashMismatch {
		t.Fatalf("CompileByHash() error=%v", err)
	}
}

func TestEvaluateRejectsMissingFactAndUnbalancedCost(t *testing.T) {
	engine := NewEngine()
	rule, err := engine.Compile(`v1:token_line("input", uncached_input_tokens, 3000000)`)
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	_, err = engine.Evaluate(rule, Facts{AvailableFacts: map[string]bool{"uncached_input_tokens": false}})
	var pricingErr *Error
	if !errors.As(err, &pricingErr) || pricingErr.Code != ErrorFactMissing {
		t.Fatalf("Evaluate() error=%v", err)
	}

	rule, err = engine.Compile(`v1:token_cost(uncached_input_tokens, 3000000)`)
	if err != nil {
		t.Fatalf("Compile(cost): %v", err)
	}
	_, err = engine.Evaluate(rule, Facts{UncachedInputTokens: 1000})
	if !errors.As(err, &pricingErr) || pricingErr.Code != ErrorBreakdownInvalid {
		t.Fatalf("Evaluate(cost) error=%v", err)
	}
}

func TestPricingMathRejectsOverflow(t *testing.T) {
	if _, err := checkedMulDivRoundHalfUp(math.MaxInt64, math.MaxInt64, 1); err == nil {
		t.Fatal("checkedMulDivRoundHalfUp() accepted overflow")
	}
}

func TestCompileConcurrentReturnsSharedProgram(t *testing.T) {
	engine := NewEngine(2)
	source := `v1:fixed_line("request", "request", 1)`
	const workers = 64
	rules := make([]CompiledRule, workers)
	errs := make([]error, workers)
	var wait sync.WaitGroup
	for index := range rules {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			rules[index], errs[index] = engine.Compile(source)
		}(index)
	}
	wait.Wait()
	for index := range rules {
		if errs[index] != nil || rules[index].program != rules[0].program {
			t.Fatalf("compile[%d]=%v shared=%t", index, errs[index], rules[index].program == rules[0].program)
		}
	}
}

func FuzzCompileNeverPanics(f *testing.F) {
	f.Add(`v1:fixed_line("request", "request", 1)`)
	f.Add(`v1:header("authorization")`)
	engine := NewEngine(8)
	f.Fuzz(func(t *testing.T, source string) {
		_, _ = engine.Compile(source)
	})
}
