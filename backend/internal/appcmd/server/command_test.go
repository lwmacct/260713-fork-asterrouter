package server

import (
	"slices"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/urfave/cli/v3"
)

func TestCommandDoesNotExposeSecretsAsFlags(t *testing.T) {
	root := &cli.Command{Name: "asterrouter", Commands: []*cli.Command{Command}}
	config.Manager.MustConfigure(root)

	var names []string
	for _, flag := range Command.Flags {
		names = append(names, flag.Names()...)
	}
	for _, forbidden := range []string{
		"security.admin.password",
		"security.admin.token",
		"security.secret-key",
		"storage.database-url",
		"storage.redis.url",
		"artifacts.s3.access-key",
		"artifacts.s3.secret-key",
	} {
		if slices.Contains(names, forbidden) {
			t.Fatalf("secret flag %q was exposed: %v", forbidden, names)
		}
	}
	for _, required := range []string{"http.listen", "bootstrap.deployment-role", "jobs.queue.driver"} {
		if !slices.Contains(names, required) {
			t.Fatalf("expected generated flag %q: %v", required, names)
		}
	}
}

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
