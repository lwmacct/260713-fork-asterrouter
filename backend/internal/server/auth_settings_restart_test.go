package server

import (
	"slices"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/settings"
)

func TestAuthenticationRestartReasonsOnlyReportRuntimeProviderChanges(t *testing.T) {
	base := settings.AdminSettings{}
	base.SiteName = "AsterRouter"
	base.PublicBaseURL = "https://router.example.test"
	base.OIDCIssuerURL = "https://id.example.test"
	base.OIDCClientID = "client"
	base.FeishuRegion = "cn"

	ordinary := base
	ordinary.SiteName = "Enterprise Router"
	if reasons := authenticationRestartReasons(base, ordinary); len(reasons) != 0 {
		t.Fatalf("ordinary settings reasons=%v", reasons)
	}

	changed := base
	changed.PublicBaseURL = "https://new-router.example.test"
	changed.OIDCEnabled = true
	changed.FeishuRegion = "intl"
	changed.DingTalkEnabled = true
	changed.GitHubOAuthEnabled = true
	changed.GoogleOAuthEnabled = true
	reasons := authenticationRestartReasons(base, changed)
	for _, expected := range []string{"public_base_url", "oidc", "feishu", "dingtalk", "github", "google"} {
		if !slices.Contains(reasons, expected) {
			t.Fatalf("missing %q in reasons=%v", expected, reasons)
		}
	}
}
