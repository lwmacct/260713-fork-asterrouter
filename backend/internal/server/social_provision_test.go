package server

import (
	"context"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/settings"
)

func TestAuthorizeSocialProvisionHonorsRegistrationPolicyForNewIdentities(t *testing.T) {
	ctx := context.Background()
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{})
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")

	if err := authorizeSocialProvision(ctx, settingsService, control, "github", "new-subject", "dev@example.com"); err == nil {
		t.Fatal("new social identity must be rejected when registration is disabled")
	}
	if _, err := control.ProvisionOIDCUser(ctx, "github", "existing-subject", "existing@example.com", "Existing", ""); err != nil {
		t.Fatalf("ProvisionOIDCUser(): %v", err)
	}
	if err := authorizeSocialProvision(ctx, settingsService, control, "github", "existing-subject", "existing@example.com"); err != nil {
		t.Fatalf("existing identity must remain able to log in: %v", err)
	}

	admin, err := settingsService.Admin(ctx)
	if err != nil {
		t.Fatalf("Admin(): %v", err)
	}
	admin.RegistrationEnabled = true
	admin.AllowedEmailDomains = []string{"example.com"}
	if _, err := settingsService.Update(ctx, admin); err != nil {
		t.Fatalf("Update(): %v", err)
	}
	if err := authorizeSocialProvision(ctx, settingsService, control, "google", "allowed", "dev@Team.Example.com"); err != nil {
		t.Fatalf("allowed enterprise domain was rejected: %v", err)
	}
	if err := authorizeSocialProvision(ctx, settingsService, control, "google", "denied", "dev@personal.test"); err == nil {
		t.Fatal("email outside the enterprise allowlist must be rejected")
	}

	admin, _ = settingsService.Admin(ctx)
	admin.InvitationRequired = true
	admin.InvitationCodes = []string{"INVITE"}
	if _, err := settingsService.Update(ctx, admin); err != nil {
		t.Fatalf("Update(invitation): %v", err)
	}
	if err := authorizeSocialProvision(ctx, settingsService, control, "github", "invited", "dev@example.com"); err == nil {
		t.Fatal("social auto-provision must not bypass required invitations")
	}
}

func TestEmailDomainAllowedUsesLabelBoundaries(t *testing.T) {
	allowed := []string{"*.Example.COM"}
	for _, domain := range []string{"example.com", "engineering.example.com", "deep.team.example.com"} {
		if !emailDomainAllowed(allowed, domain) {
			t.Fatalf("expected %q to be allowed", domain)
		}
	}
	for _, domain := range []string{"evilexample.com", "example.com.attacker.test", ""} {
		if emailDomainAllowed(allowed, domain) {
			t.Fatalf("expected %q to be rejected", domain)
		}
	}
}
