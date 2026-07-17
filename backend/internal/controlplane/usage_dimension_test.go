package controlplane

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestUsageDimensionsNormalizeRoundTripAndMerge(t *testing.T) {
	input := UsageDimensions{
		" OUTPUT_IMAGES ": {
			Quantity: 2, Unit: " COUNT ", Source: " Provider_Adapter ", Confidence: " OBSERVED ",
			ProcurementPriceSnapshotID: " price-image ", Attributes: map[string]string{" Quality ": "hd"},
		},
	}
	normalized, err := NormalizeUsageDimensions(input)
	if err != nil {
		t.Fatal(err)
	}
	want := UsageDimensions{UsageDimensionOutputImages: {
		Quantity: 2, Unit: UsageUnitCount, Source: "provider_adapter", Confidence: UsageConfidenceObserved,
		ProcurementPriceSnapshotID: "price-image", Attributes: map[string]string{"quality": "hd"},
	}}
	if !reflect.DeepEqual(normalized, want) {
		t.Fatalf("normalized=%+v want=%+v", normalized, want)
	}
	payload, err := UsageDimensionsJSON(normalized)
	if err != nil || !json.Valid([]byte(payload)) {
		t.Fatalf("payload=%q err=%v", payload, err)
	}
	parsed, err := ParseUsageDimensionsJSON(payload)
	if err != nil || !reflect.DeepEqual(parsed, want) {
		t.Fatalf("parsed=%+v err=%v", parsed, err)
	}
	merged, err := MergeUsageDimensions(parsed, UsageDimensions{UsageDimensionOutputImages: {
		Quantity: 3, Unit: UsageUnitCount, Source: "core_artifact", Confidence: UsageConfidenceObserved,
	}})
	if err != nil || merged[UsageDimensionOutputImages].Quantity != 3 || parsed[UsageDimensionOutputImages].Quantity != 2 {
		t.Fatalf("merged=%+v parsed=%+v err=%v", merged, parsed, err)
	}
}

func TestUsageDimensionsRejectInvalidContracts(t *testing.T) {
	tests := []UsageDimensions{
		{"unknown": {Quantity: 1, Unit: UsageUnitCount, Source: "core", Confidence: UsageConfidenceObserved}},
		{UsageDimensionOutputImages: {Quantity: -1, Unit: UsageUnitCount, Source: "core", Confidence: UsageConfidenceObserved}},
		{UsageDimensionOutputImages: {Quantity: 1, Unit: UsageUnitByte, Source: "core", Confidence: UsageConfidenceObserved}},
		{UsageDimensionOutputImages: {Quantity: 1, Unit: UsageUnitCount, Source: "INVALID SOURCE", Confidence: UsageConfidenceObserved}},
		{UsageDimensionOutputImages: {Quantity: 1, Unit: UsageUnitCount, Source: "core", Confidence: "guessed"}},
		{UsageDimensionOutputImages: {Quantity: 1, Unit: UsageUnitCount, Source: "core", Confidence: UsageConfidenceObserved, Attributes: map[string]string{"bad": "line\nbreak"}}},
		{
			"output_images":  {Quantity: 1, Unit: UsageUnitCount, Source: "core", Confidence: UsageConfidenceObserved},
			"OUTPUT_IMAGES ": {Quantity: 2, Unit: UsageUnitCount, Source: "core", Confidence: UsageConfidenceObserved},
		},
	}
	for index, input := range tests {
		if _, err := NormalizeUsageDimensions(input); !errors.Is(err, ErrUsageDimensionsInvalid) {
			t.Fatalf("case %d error=%v", index, err)
		}
	}
	if _, err := ParseUsageDimensionsJSON(`{"output_images":{"quantity":1}}`); !errors.Is(err, ErrUsageDimensionsInvalid) {
		t.Fatalf("partial json error=%v", err)
	}
}

func TestUsageDimensionTotalsSaturate(t *testing.T) {
	totals := UsageDimensionsTotals(UsageDimensions{
		UsageDimensionOutputImages:              {Quantity: math.MaxInt64, Unit: UsageUnitCount},
		UsageDimensionInputVideoMilliseconds:    {Quantity: math.MaxInt64, Unit: UsageUnitMillisecond},
		UsageDimensionOutputVideoMilliseconds:   {Quantity: 1, Unit: UsageUnitMillisecond},
		UsageDimensionInputAudioMilliseconds:    {Quantity: 2, Unit: UsageUnitMillisecond},
		UsageDimensionOutputAudioMilliseconds:   {Quantity: 3, Unit: UsageUnitMillisecond},
		UsageDimensionRealtimeAudioMilliseconds: {Quantity: 7, Unit: UsageUnitMillisecond},
	})
	if totals.OutputImages != math.MaxInt64 || totals.VideoMilliseconds != math.MaxInt64 || totals.AudioMilliseconds != 12 {
		t.Fatalf("totals=%+v", totals)
	}
}
