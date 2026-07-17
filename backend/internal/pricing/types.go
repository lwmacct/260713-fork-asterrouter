package pricing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/expr-lang/expr/vm"
)

const (
	EngineVersionV1 = 1
	CurrencyUSD     = "USD"

	PhaseEstimate   = "estimate"
	PhaseSettlement = "settlement"
	PhaseReplay     = "replay"
)

const (
	ErrorExpressionEmpty    = "pricing_expr_empty"
	ErrorExpressionTooLarge = "pricing_expr_too_large"
	ErrorVersionUnsupported = "pricing_version_unsupported"
	ErrorCompileFailed      = "pricing_expr_compile_failed"
	ErrorForbiddenAST       = "pricing_expr_forbidden_ast"
	ErrorHashMismatch       = "pricing_expr_hash_mismatch"
	ErrorFactMissing        = "pricing_fact_missing"
	ErrorFactInvalid        = "pricing_fact_invalid"
	ErrorArithmeticOverflow = "pricing_arithmetic_overflow"
	ErrorAmountOutOfRange   = "pricing_amount_out_of_range"
	ErrorTierInvalid        = "pricing_tier_invalid"
	ErrorBreakdownInvalid   = "pricing_breakdown_invalid"
	ErrorRuleUnavailable    = "pricing_rule_unavailable"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Line > 0 {
		return fmt.Sprintf("%s at %d:%d: %s", e.Code, e.Line, e.Column, e.Message)
	}
	return e.Code + ": " + e.Message
}

func errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

type Facts struct {
	TotalInputTokens          int64           `json:"total_input_tokens"`
	UncachedInputTokens       int64           `json:"uncached_input_tokens"`
	CacheReadTokens           int64           `json:"cache_read_tokens"`
	CacheWrite5mTokens        int64           `json:"cache_write_5m_tokens"`
	CacheWrite1hTokens        int64           `json:"cache_write_1h_tokens"`
	OutputTokens              int64           `json:"output_tokens"`
	CacheFieldsPresent        bool            `json:"cache_fields_present"`
	InputImages               int64           `json:"input_images"`
	OutputImages              int64           `json:"output_images"`
	PartialImages             int64           `json:"partial_images"`
	InputVideoMilliseconds    int64           `json:"input_video_milliseconds"`
	OutputVideoMilliseconds   int64           `json:"output_video_milliseconds"`
	InputAudioMilliseconds    int64           `json:"input_audio_milliseconds"`
	OutputAudioMilliseconds   int64           `json:"output_audio_milliseconds"`
	RealtimeAudioMilliseconds int64           `json:"realtime_audio_milliseconds"`
	InputCharacters           int64           `json:"input_characters"`
	Actions                   int64           `json:"actions"`
	BatchItems                int64           `json:"batch_items"`
	InputBytes                int64           `json:"input_bytes"`
	OutputBytes               int64           `json:"output_bytes"`
	TransferBytes             int64           `json:"transfer_bytes"`
	SessionMilliseconds       int64           `json:"session_milliseconds"`
	Protocol                  string          `json:"protocol"`
	Operation                 string          `json:"operation"`
	Modality                  string          `json:"modality"`
	Lane                      string          `json:"lane"`
	Stream                    bool            `json:"stream"`
	OutputCount               int64           `json:"output_count"`
	ServiceTier               string          `json:"service_tier"`
	AvailableFacts            map[string]bool `json:"available_facts,omitempty"`
	NormalizationStatus       string          `json:"normalization_status"`
	Phase                     string          `json:"phase"`
	ObservedAt                time.Time       `json:"observed_at"`
}

type PricingLine struct {
	Code          string `json:"code"`
	Quantity      int64  `json:"quantity"`
	Unit          string `json:"unit"`
	UnitsPerBlock int64  `json:"units_per_block"`
	RateMicros    int64  `json:"rate_micros"`
	MultiplierBPS int64  `json:"multiplier_bps"`
	AmountMicros  int64  `json:"amount_micros"`
}

type Result struct {
	AmountMicros   int64         `json:"amount_micros"`
	Currency       string        `json:"currency"`
	MatchedTier    string        `json:"matched_tier"`
	Lines          []PricingLine `json:"lines"`
	EngineVersion  int           `json:"engine_version"`
	ExpressionHash string        `json:"expression_hash"`
	FactsHash      string        `json:"facts_hash"`
}

type TierAnalysis struct {
	Name       string   `json:"name"`
	Conditions []string `json:"conditions,omitempty"`
}

type RuleAnalysis struct {
	EngineVersion  int            `json:"engine_version"`
	RequiredFacts  []string       `json:"required_facts"`
	Tiers          []TierAnalysis `json:"tiers"`
	LineCodes      []string       `json:"line_codes"`
	VisualEditable bool           `json:"visual_editable"`
}

type CompiledRule struct {
	Source         string       `json:"source"`
	ExpressionHash string       `json:"expression_hash"`
	EngineVersion  int          `json:"engine_version"`
	Analysis       RuleAnalysis `json:"analysis"`
	body           string
	program        *vm.Program
}

func ExpressionHash(source string) string {
	digest := sha256.Sum256([]byte(source))
	return hex.EncodeToString(digest[:])
}

func (f Facts) Hash() (string, error) {
	payload, err := f.CanonicalJSON()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func (f Facts) CanonicalJSON() ([]byte, error) {
	canonical := f
	canonical.Phase = ""
	canonical.ObservedAt = time.Time{}
	return json.Marshal(canonical)
}

func validShortString(value string) bool {
	return len(value) <= 128 && !strings.ContainsAny(value, "\r\n\x00")
}
