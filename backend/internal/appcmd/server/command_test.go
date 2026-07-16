package server

import (
	"testing"
)

func TestNewAppRejectsNilConfiguration(t *testing.T) {
	if err := NewApp(nil).Run(t.Context()); err == nil {
		t.Fatal("NewApp(nil).Run() accepted a nil configuration")
	}
}

func TestEphemeralSecretIsRandomAndNonEmpty(t *testing.T) {
	first, err := ephemeralSecret()
	if err != nil {
		t.Fatal(err)
	}
	second, err := ephemeralSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 40 || len(second) < 40 || first == second {
		t.Fatalf("unexpected ephemeral secrets: lengths %d/%d equal=%t", len(first), len(second), first == second)
	}
}
