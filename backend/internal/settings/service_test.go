package settings

import (
	"context"
	"testing"
)

func TestServiceDefaults(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
	got, err := svc.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin() error = %v", err)
	}
	if got.SiteName != "AsterRouter" {
		t.Fatalf("SiteName = %q", got.SiteName)
	}
	if got.DefaultLocale != "en-US" {
		t.Fatalf("DefaultLocale = %q", got.DefaultLocale)
	}
	if got.GatewayBasePath != "/v1" {
		t.Fatalf("GatewayBasePath = %q", got.GatewayBasePath)
	}
}

func TestApplyProfiles(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
	got, err := svc.ApplyProfiles(context.Background(), []string{"enterprise", "personal"}, "personal")
	if err != nil {
		t.Fatalf("ApplyProfiles() error = %v", err)
	}
	if !got.SetupCompleted || got.DefaultProfile != "personal" || len(got.EnabledProfiles) != 2 {
		t.Fatalf("profiles not applied: %+v", got)
	}
}

func TestUpdateValidatesLocale(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
	_, err := svc.Update(context.Background(), AdminSettings{
		PublicSettings: PublicSettings{
			SiteName:          "AsterRouter",
			DefaultLocale:     "ja-JP",
			EnabledLocales:    []string{"en-US"},
			GatewayBasePath:   "/v1",
			ServiceCenterMode: "disabled",
		},
		DataRetentionDays: 30,
		PromptLoggingMode: "metadata_only",
		UpdateChannel:     "stable",
	})
	if err == nil {
		t.Fatal("Update() error = nil, want validation error")
	}
}
