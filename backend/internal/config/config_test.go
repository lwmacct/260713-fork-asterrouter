package config

import "testing"

func TestValidateRuntimeAllowsSourceBuildWithoutDatabase(t *testing.T) {
	cfg := Config{BuildType: "source"}

	if err := ValidateRuntime(cfg); err != nil {
		t.Fatalf("ValidateRuntime() = %v, want nil", err)
	}
}

func TestValidateRuntimeRequiresReleaseDatabase(t *testing.T) {
	cfg := Config{
		BuildType:     "release",
		SecretKey:     "stable-secret",
		AdminPassword: "change-me",
	}

	if err := ValidateRuntime(cfg); err == nil {
		t.Fatalf("ValidateRuntime() = nil, want error")
	}
}

func TestValidateRuntimeAllowsDemoModeAsReleaseAuthMechanism(t *testing.T) {
	cfg := Config{
		BuildType:   "release",
		DatabaseURL: "postgres://asterrouter:pass@localhost:5432/asterrouter?sslmode=disable",
		SecretKey:   "stable-secret",
		DemoMode:    true,
	}

	if err := ValidateRuntime(cfg); err != nil {
		t.Fatalf("ValidateRuntime() = %v, want nil", err)
	}
}

func TestValidateRuntimeRequiresProductionSecret(t *testing.T) {
	cfg := Config{
		BuildType:     "release",
		DatabaseURL:   "postgres://asterrouter:pass@localhost:5432/asterrouter?sslmode=disable",
		SecretKey:     localDevelopmentSecret,
		AdminPassword: "change-me",
	}

	if err := ValidateRuntime(cfg); err == nil {
		t.Fatalf("ValidateRuntime() = nil, want error")
	}
}

func TestValidateRuntimeRequiresCatalogTrustMaterialWhenOnline(t *testing.T) {
	cfg := Config{
		BuildType:     "release",
		DatabaseURL:   "postgres://asterrouter:pass@localhost:5432/asterrouter?sslmode=disable",
		SecretKey:     "stable-secret",
		AdminPassword: "change-me",
		CatalogMode:   "online",
		CatalogURL:    "https://catalog.example.test/official/v1/catalog/index",
	}

	if err := ValidateRuntime(cfg); err == nil {
		t.Fatalf("ValidateRuntime() = nil, want error")
	}
}

func TestValidateRuntimeRequiresCatalogTrustMaterialWhenPrivateMirror(t *testing.T) {
	cfg := Config{
		BuildType:     "release",
		DatabaseURL:   "postgres://asterrouter:pass@localhost:5432/asterrouter?sslmode=disable",
		SecretKey:     "stable-secret",
		AdminPassword: "change-me",
		CatalogMode:   "private_mirror",
		CatalogURL:    "https://mirror.example.test/official/v1/catalog/index",
	}

	if err := ValidateRuntime(cfg); err == nil {
		t.Fatalf("ValidateRuntime() = nil, want error")
	}
}

func TestValidateRuntimeRequiresCatalogTrustMaterialWhenOffline(t *testing.T) {
	cfg := Config{
		BuildType:     "release",
		DatabaseURL:   "postgres://asterrouter:pass@localhost:5432/asterrouter?sslmode=disable",
		SecretKey:     "stable-secret",
		AdminPassword: "change-me",
		CatalogMode:   "offline",
	}

	if err := ValidateRuntime(cfg); err == nil {
		t.Fatalf("ValidateRuntime() = nil, want error")
	}
}

func TestDefaultPluginHostURLUsesLoopbackForWildcardListenAddress(t *testing.T) {
	if got := defaultPluginHostURL(":8080"); got != "http://127.0.0.1:8080/api/v1/plugin-host" {
		t.Fatalf("defaultPluginHostURL(:8080) = %q", got)
	}
	if got := defaultPluginHostURL("0.0.0.0:18090"); got != "http://127.0.0.1:18090/api/v1/plugin-host" {
		t.Fatalf("defaultPluginHostURL(0.0.0.0:18090) = %q", got)
	}
}

func TestNormalizeProfilesPreservesUnknownValuesForValidation(t *testing.T) {
	profiles := normalizeProfiles("platform, enterprise, platform, unknown")
	want := []string{"platform", "enterprise", "unknown"}
	if len(profiles) != len(want) {
		t.Fatalf("normalizeProfiles() = %v, want %v", profiles, want)
	}
	for index, profile := range want {
		if profiles[index] != profile {
			t.Fatalf("normalizeProfiles() = %v, want %v", profiles, want)
		}
	}
}

func TestValidateRuntimeRejectsUnknownLegacyProfiles(t *testing.T) {
	for _, cfg := range []Config{
		{BuildType: "source", Profiles: []string{"unknown"}, DefaultProfile: "unknown"},
		{BuildType: "source", DefaultProfile: "unknown"},
	} {
		if err := ValidateRuntime(cfg); err == nil {
			t.Fatalf("ValidateRuntime(%+v) accepted an unknown legacy profile", cfg)
		}
	}
}

func TestLoadPreservesUnknownLegacyProfileForValidation(t *testing.T) {
	t.Setenv("ASTER_DEPLOYMENT_ROLE", "")
	t.Setenv("ASTER_PROFILES", "unknown")
	t.Setenv("ASTER_DEFAULT_PROFILE", "")

	cfg := Load()
	if len(cfg.Profiles) != 1 || cfg.Profiles[0] != "unknown" || cfg.DefaultProfile != "unknown" {
		t.Fatalf("Load() silently discarded an unknown legacy profile: %+v", cfg)
	}
	if err := ValidateRuntime(cfg); err == nil {
		t.Fatal("ValidateRuntime() accepted the unknown legacy profile loaded from the environment")
	}
}

func TestValidateRuntimeRejectsMultipleBootstrapProfiles(t *testing.T) {
	cfg := Config{BuildType: "source", Profiles: []string{"enterprise", "platform"}}
	if err := ValidateRuntime(cfg); err == nil {
		t.Fatal("ValidateRuntime() accepted multiple bootstrap profiles")
	}
}

func TestValidateRuntimeAcceptsDeploymentRoleAndRejectsLegacyMismatch(t *testing.T) {
	valid := Config{
		BuildType:      "source",
		DeploymentRole: "platform",
		Profiles:       []string{"platform"},
		DefaultProfile: "platform",
	}
	if err := ValidateRuntime(valid); err != nil {
		t.Fatalf("ValidateRuntime() deployment role: %v", err)
	}

	mismatched := valid
	mismatched.Profiles = []string{"enterprise"}
	mismatched.DefaultProfile = "enterprise"
	if err := ValidateRuntime(mismatched); err == nil {
		t.Fatal("ValidateRuntime() accepted mismatched deployment role and legacy profile")
	}

	invalid := valid
	invalid.DeploymentRole = "unknown"
	if err := ValidateRuntime(invalid); err == nil {
		t.Fatal("ValidateRuntime() accepted an unknown deployment role")
	}
}

func TestLoadDeploymentRoleBootstrapsCompatibleProfiles(t *testing.T) {
	t.Setenv("ASTER_DEPLOYMENT_ROLE", "platform")
	t.Setenv("ASTER_PROFILES", "")
	t.Setenv("ASTER_DEFAULT_PROFILE", "")

	cfg := Load()
	if cfg.DeploymentRole != "platform" || len(cfg.Profiles) != 1 || cfg.Profiles[0] != "platform" || cfg.DefaultProfile != "platform" {
		t.Fatalf("Load() deployment role configuration = %+v", cfg)
	}
	if err := ValidateRuntime(cfg); err != nil {
		t.Fatalf("ValidateRuntime() loaded deployment role: %v", err)
	}
}
