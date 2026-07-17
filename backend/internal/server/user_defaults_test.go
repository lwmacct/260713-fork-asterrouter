package server

import (
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/settings"
)

func TestWorkspaceUserDefaultsUsesSourceOverrideAndGlobalFallback(t *testing.T) {
	admin := settings.AdminSettings{DefaultBalanceMicros: 100, DefaultConcurrency: 5, DefaultRPM: 60, AuthSourceDefaults: map[string]settings.AuthSourceDefault{"oidc": {Enabled: true, BalanceMicros: 900, Concurrency: 12, RPM: 300}}}
	oidc := workspaceUserDefaults(admin, "oidc")
	if oidc.BalanceMicros != 900 || oidc.ConcurrencyLimit != 12 || oidc.RPMLimit != 300 {
		t.Fatalf("OIDC defaults = %+v", oidc)
	}
	feishu := workspaceUserDefaults(admin, "feishu")
	if feishu.BalanceMicros != 100 || feishu.ConcurrencyLimit != 5 || feishu.RPMLimit != 60 {
		t.Fatalf("fallback defaults = %+v", feishu)
	}
}
