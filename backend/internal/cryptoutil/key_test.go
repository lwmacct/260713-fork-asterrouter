package cryptoutil

import (
	"bytes"
	"testing"
)

func TestDeriveKeyIsDeterministicAndPurposeBound(t *testing.T) {
	first, err := DeriveKey("master-secret", "purpose-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := DeriveKey("master-secret", "purpose-a")
	if err != nil {
		t.Fatal(err)
	}
	other, err := DeriveKey("master-secret", "purpose-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != keySize || !bytes.Equal(first, second) || bytes.Equal(first, other) {
		t.Fatalf("derived keys are not deterministic and purpose-bound")
	}
}
