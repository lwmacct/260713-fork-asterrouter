package controlplane

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
)

const (
	UsageDimensionInputImages               = "input_images"
	UsageDimensionOutputImages              = "output_images"
	UsageDimensionPartialImages             = "partial_images"
	UsageDimensionInputVideoMilliseconds    = "input_video_milliseconds"
	UsageDimensionOutputVideoMilliseconds   = "output_video_milliseconds"
	UsageDimensionInputAudioMilliseconds    = "input_audio_milliseconds"
	UsageDimensionOutputAudioMilliseconds   = "output_audio_milliseconds"
	UsageDimensionRealtimeAudioMilliseconds = "realtime_audio_milliseconds"
	UsageDimensionInputCharacters           = "input_characters"
	UsageDimensionActions                   = "actions"
	UsageDimensionBatchItems                = "batch_items"
	UsageDimensionInputBytes                = "input_bytes"
	UsageDimensionOutputBytes               = "output_bytes"
	UsageDimensionTransferBytes             = "transfer_bytes"
	UsageDimensionSessionMilliseconds       = "session_milliseconds"

	UsageUnitCount       = "count"
	UsageUnitMillisecond = "millisecond"
	UsageUnitCharacter   = "character"
	UsageUnitByte        = "byte"

	UsageConfidenceEstimated  = "estimated"
	UsageConfidenceObserved   = "observed"
	UsageConfidenceReported   = "reported"
	UsageConfidenceReconciled = "reconciled"
)

var ErrUsageDimensionsInvalid = errors.New("usage dimensions are invalid")

var usageDimensionUnits = map[string]string{
	UsageDimensionInputImages:               UsageUnitCount,
	UsageDimensionOutputImages:              UsageUnitCount,
	UsageDimensionPartialImages:             UsageUnitCount,
	UsageDimensionInputVideoMilliseconds:    UsageUnitMillisecond,
	UsageDimensionOutputVideoMilliseconds:   UsageUnitMillisecond,
	UsageDimensionInputAudioMilliseconds:    UsageUnitMillisecond,
	UsageDimensionOutputAudioMilliseconds:   UsageUnitMillisecond,
	UsageDimensionRealtimeAudioMilliseconds: UsageUnitMillisecond,
	UsageDimensionInputCharacters:           UsageUnitCharacter,
	UsageDimensionActions:                   UsageUnitCount,
	UsageDimensionBatchItems:                UsageUnitCount,
	UsageDimensionInputBytes:                UsageUnitByte,
	UsageDimensionOutputBytes:               UsageUnitByte,
	UsageDimensionTransferBytes:             UsageUnitByte,
	UsageDimensionSessionMilliseconds:       UsageUnitMillisecond,
}

type UsageDimension struct {
	Quantity                   int64             `json:"quantity"`
	Unit                       string            `json:"unit"`
	Source                     string            `json:"source"`
	Confidence                 string            `json:"confidence"`
	ProcurementPriceSnapshotID string            `json:"procurement_price_snapshot_id,omitempty"`
	Attributes                 map[string]string `json:"attributes,omitempty"`
}

type UsageDimensions map[string]UsageDimension

type UsageDimensionTotals struct {
	OutputImages      int64 `json:"output_images"`
	VideoMilliseconds int64 `json:"video_milliseconds"`
	AudioMilliseconds int64 `json:"audio_milliseconds"`
}

func NormalizeUsageDimensions(values UsageDimensions) (UsageDimensions, error) {
	if len(values) == 0 {
		return UsageDimensions{}, nil
	}
	normalized := make(UsageDimensions, len(values))
	for rawName, rawValue := range values {
		name := strings.ToLower(strings.TrimSpace(rawName))
		if _, duplicate := normalized[name]; duplicate {
			return nil, ErrUsageDimensionsInvalid
		}
		expectedUnit, supported := usageDimensionUnits[name]
		value := rawValue
		value.Unit = strings.ToLower(strings.TrimSpace(value.Unit))
		value.Source = strings.ToLower(strings.TrimSpace(value.Source))
		value.Confidence = strings.ToLower(strings.TrimSpace(value.Confidence))
		value.ProcurementPriceSnapshotID = strings.TrimSpace(value.ProcurementPriceSnapshotID)
		if !supported || value.Quantity < 0 || value.Unit != expectedUnit || !validUsageDimensionToken(value.Source) ||
			!oneOf(value.Confidence, UsageConfidenceEstimated, UsageConfidenceObserved, UsageConfidenceReported, UsageConfidenceReconciled) ||
			len(value.ProcurementPriceSnapshotID) > 160 {
			return nil, ErrUsageDimensionsInvalid
		}
		attributes, err := normalizeUsageDimensionAttributes(value.Attributes)
		if err != nil {
			return nil, err
		}
		value.Attributes = attributes
		normalized[name] = value
	}
	return normalized, nil
}

func UsageDimensionsJSON(values UsageDimensions) (string, error) {
	normalized, err := NormalizeUsageDimensions(values)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func usageDimensionsEqual(left, right UsageDimensions) bool {
	leftJSON, leftErr := UsageDimensionsJSON(left)
	rightJSON, rightErr := UsageDimensionsJSON(right)
	return leftErr == nil && rightErr == nil && leftJSON == rightJSON
}

func ParseUsageDimensionsJSON(payload string) (UsageDimensions, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "null" {
		return UsageDimensions{}, nil
	}
	var values UsageDimensions
	if err := json.Unmarshal([]byte(payload), &values); err != nil {
		return nil, ErrUsageDimensionsInvalid
	}
	return NormalizeUsageDimensions(values)
}

func UsageDimensionsTotals(values UsageDimensions) UsageDimensionTotals {
	return UsageDimensionTotals{
		OutputImages: usageDimensionQuantity(values, UsageDimensionOutputImages),
		VideoMilliseconds: saturatingUsageAdd(
			usageDimensionQuantity(values, UsageDimensionInputVideoMilliseconds),
			usageDimensionQuantity(values, UsageDimensionOutputVideoMilliseconds),
		),
		AudioMilliseconds: saturatingUsageAdd(
			saturatingUsageAdd(
				usageDimensionQuantity(values, UsageDimensionInputAudioMilliseconds),
				usageDimensionQuantity(values, UsageDimensionOutputAudioMilliseconds),
			),
			usageDimensionQuantity(values, UsageDimensionRealtimeAudioMilliseconds),
		),
	}
}

func MergeUsageDimensions(left, right UsageDimensions) (UsageDimensions, error) {
	left, err := NormalizeUsageDimensions(left)
	if err != nil {
		return nil, err
	}
	right, err = NormalizeUsageDimensions(right)
	if err != nil {
		return nil, err
	}
	merged := make(UsageDimensions, len(left)+len(right))
	for name, value := range left {
		merged[name] = cloneUsageDimension(value)
	}
	for name, value := range right {
		if current, exists := merged[name]; exists && current.Unit != value.Unit {
			return nil, ErrUsageDimensionsInvalid
		}
		merged[name] = cloneUsageDimension(value)
	}
	return merged, nil
}

func usageDimensionQuantity(values UsageDimensions, name string) int64 {
	value, found := values[name]
	if !found || value.Quantity < 0 {
		return 0
	}
	return value.Quantity
}

func saturatingUsageAdd(left, right int64) int64 {
	if left < 0 {
		left = 0
	}
	if right < 0 {
		right = 0
	}
	if left > math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}

func cloneUsageDimension(value UsageDimension) UsageDimension {
	if len(value.Attributes) == 0 {
		value.Attributes = nil
		return value
	}
	attributes := make(map[string]string, len(value.Attributes))
	for key, item := range value.Attributes {
		attributes[key] = item
	}
	value.Attributes = attributes
	return value
}

func validUsageDimensionToken(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			continue
		}
		if index > 0 && (char == '_' || char == '-' || char == ':') {
			continue
		}
		return false
	}
	return true
}

func normalizeUsageDimensionAttributes(values map[string]string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > 16 {
		return nil, ErrUsageDimensionsInvalid
	}
	normalized := make(map[string]string, len(values))
	for rawKey, rawValue := range values {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		value := strings.TrimSpace(rawValue)
		if !validUsageDimensionToken(key) || value == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n\x00") {
			return nil, ErrUsageDimensionsInvalid
		}
		if _, duplicate := normalized[key]; duplicate {
			return nil, ErrUsageDimensionsInvalid
		}
		normalized[key] = value
	}
	return normalized, nil
}
