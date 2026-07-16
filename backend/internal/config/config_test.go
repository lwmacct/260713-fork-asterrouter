package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lwmacct/251207-go-pkg-cfgm/pkg/cfgm"
)

var files = cfgm.ConfigFiles[Config]{
	Manager:     Manager,
	ExampleFile: "config/config.example.yaml",
	RuntimeFile: "config/config.yaml",
}

func TestWriteConfigExample(t *testing.T)     { files.WriteExample(t) }
func TestRuntimeConfigKeysValid(t *testing.T) { files.ValidateRuntimeConfig(t) }

func TestManagerLoadsStrictServerHierarchy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "server:\n  http:\n    listen: 127.0.0.1:19090\n  jobs:\n    queue:\n      limits:\n        tenant: 25\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Manager.Load(t.Context(), cfgm.File(path))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.HTTP.Listen != "127.0.0.1:19090" || loaded.Server.Jobs.Queue.Limits.Tenant != 25 {
		t.Fatalf("unexpected loaded config: %#v", loaded.Server)
	}

	if err := os.WriteFile(path, []byte("http:\n  listen: :9999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = Manager.Load(t.Context(), cfgm.File(path)); err == nil || !strings.Contains(err.Error(), "http") {
		t.Fatalf("root-level legacy config must be rejected, got %v", err)
	}
}

func TestManagerLoadsCanonicalEnvironment(t *testing.T) {
	t.Setenv("ASTERROUTER_SERVER_HTTP_LISTEN", "127.0.0.1:18081")
	t.Setenv("ASTERROUTER_SERVER_JOBS_QUEUE_LIMITS_PROFILE", "12")
	t.Setenv("ASTERROUTER_SERVER_ARTIFACTS_S3_PATH_STYLE", "true")
	loaded, err := Manager.Load(t.Context(), cfgm.Env("ASTERROUTER_"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.HTTP.Listen != "127.0.0.1:18081" || loaded.Server.Jobs.Queue.Limits.Profile != 12 || !loaded.Server.Artifacts.S3.PathStyle {
		t.Fatalf("unexpected environment config: %#v", loaded.Server)
	}
}

func TestManagerDoesNotReadLegacyEnvironment(t *testing.T) {
	t.Setenv("ASTER_ADDR", "127.0.0.1:19999")
	t.Setenv("DATABASE_URL", "postgres://legacy.example/ignored")
	loaded, err := Manager.Load(t.Context(), cfgm.Env("ASTERROUTER_"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.HTTP.Listen != ":8080" || loaded.Server.Storage.DatabaseURL != "" {
		t.Fatalf("legacy environment affected config: %#v", loaded.Server)
	}
}

func TestValidateNormalizesDerivedValues(t *testing.T) {
	cfg := DefaultConfig().Server
	cfg.HTTP.Listen = " 0.0.0.0:18090 "
	cfg.Plugins.CacheDir = " /var/lib/asterrouter/plugins/cache "
	cfg.Plugins.ActiveDir = ""
	cfg.Plugins.HostURL = ""
	validated, err := Validate(cfg, "source")
	if err != nil {
		t.Fatal(err)
	}
	if validated.Plugins.ActiveDir != "/var/lib/asterrouter/plugins/plugin-active" {
		t.Fatalf("active dir = %q", validated.Plugins.ActiveDir)
	}
	if validated.Plugins.HostURL != "http://127.0.0.1:18090/api/v1/plugin-host" {
		t.Fatalf("host URL = %q", validated.Plugins.HostURL)
	}
}

func TestValidateReleaseFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Server)
		want error
	}{
		{name: "database", edit: func(cfg *Server) { cfg.Storage.DatabaseURL = "" }, want: ErrInvalidStorage},
		{name: "secret", edit: func(cfg *Server) { cfg.Security.SecretKey = "" }, want: ErrInvalidSecurity},
		{name: "admin", edit: func(cfg *Server) { cfg.Security.Admin.Password = "" }, want: ErrInvalidSecurity},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validReleaseConfig()
			test.edit(&cfg)
			_, err := Validate(cfg, "release")
			if !errors.Is(err, test.want) {
				t.Fatalf("Validate() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestValidateRequiresSecretForPersistentSourceBuild(t *testing.T) {
	cfg := DefaultConfig().Server
	cfg.Storage.DatabaseURL = "postgres://asterrouter:test@localhost/asterrouter"
	if _, err := Validate(cfg, "source"); !errors.Is(err, ErrInvalidSecurity) {
		t.Fatalf("Validate() error = %v, want security error", err)
	}
}

func TestValidateJobsAndArtifacts(t *testing.T) {
	cfg := DefaultConfig().Server
	cfg.Jobs.Queue.Driver = "redis"
	if _, err := Validate(cfg, "source"); !errors.Is(err, ErrInvalidJobs) {
		t.Fatalf("missing Redis error = %v", err)
	}
	cfg.Storage.Redis.URL = "redis://127.0.0.1:6379/0"
	cfg.Artifacts.Driver = "s3"
	if _, err := Validate(cfg, "source"); !errors.Is(err, ErrInvalidArtifacts) {
		t.Fatalf("missing S3 credentials error = %v", err)
	}
	cfg.Artifacts.S3.Bucket = "artifacts"
	cfg.Artifacts.S3.AccessKey = "access"
	cfg.Artifacts.S3.SecretKey = "secret"
	if _, err := Validate(cfg, "source"); err != nil {
		t.Fatalf("valid Redis/S3 config: %v", err)
	}
}

func TestValidateOfficialTrustModes(t *testing.T) {
	cfg := DefaultConfig().Server
	cfg.Official.Catalog.Mode = "offline"
	if _, err := Validate(cfg, "source"); !errors.Is(err, ErrInvalidOfficial) {
		t.Fatalf("missing offline trust material error = %v", err)
	}
	cfg.Official.Catalog.KeyID = "catalog-v1"
	cfg.Official.Catalog.PublicKey = "public-key"
	if _, err := Validate(cfg, "source"); err != nil {
		t.Fatalf("valid offline config: %v", err)
	}
}

func validReleaseConfig() Server {
	cfg := DefaultConfig().Server
	cfg.Storage.DatabaseURL = "postgres://asterrouter:test@localhost/asterrouter"
	cfg.Security.SecretKey = "stable-secret"
	cfg.Security.Admin.Password = "change-me"
	return cfg
}
