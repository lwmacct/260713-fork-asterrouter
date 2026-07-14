package settings

import (
	"context"
	"errors"
	"sync/atomic"
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

func TestApplyProfilesRequiresOneImmutableDeploymentProfile(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
	got, err := svc.ApplyProfiles(context.Background(), []string{"enterprise"}, "enterprise")
	if err != nil {
		t.Fatalf("ApplyProfiles() error = %v", err)
	}
	if !got.SetupCompleted || got.DefaultProfile != "enterprise" || len(got.EnabledProfiles) != 1 || got.EnabledProfiles[0] != "enterprise" {
		t.Fatalf("profiles not applied: %+v", got)
	}
	if _, err := svc.ApplyProfiles(context.Background(), []string{"enterprise"}, "enterprise"); err != nil {
		t.Fatalf("ApplyProfiles() same-profile retry error = %v", err)
	}
	if _, err := svc.ApplyProfiles(context.Background(), []string{"enterprise", "personal"}, "enterprise"); err == nil {
		t.Fatal("ApplyProfiles() accepted more than one deployment profile")
	}
	if _, err := svc.ApplyProfiles(context.Background(), []string{"platform"}, "platform"); err == nil {
		t.Fatal("ApplyProfiles() changed the installed deployment profile")
	}
}

func TestApplyInitialProfileRequiresOneFreshProfile(t *testing.T) {
	for _, profile := range []string{"personal", "relay_operator", "enterprise", "platform"} {
		t.Run(profile, func(t *testing.T) {
			svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
			got, err := svc.ApplyInitialProfile(context.Background(), profile)
			if err != nil {
				t.Fatalf("ApplyInitialProfile() error = %v", err)
			}
			if !got.SetupCompleted || got.DefaultProfile != profile || len(got.EnabledProfiles) != 1 || got.EnabledProfiles[0] != profile {
				t.Fatalf("initial deployment profile not applied: %+v", got)
			}
			if _, err := svc.ApplyInitialProfile(context.Background(), "platform"); err == nil {
				t.Fatal("ApplyInitialProfile() after setup = nil, want error")
			}
		})
	}
	if _, err := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"}).ApplyInitialProfile(context.Background(), "unknown"); err == nil {
		t.Fatal("ApplyInitialProfile() accepted an unknown profile")
	}
}

type coordinatedInitialProfileRepository struct {
	*MemoryRepository
	reads        atomic.Int32
	initialReads chan struct{}
	applyReads   chan struct{}
}

func newCoordinatedInitialProfileRepository() *coordinatedInitialProfileRepository {
	return &coordinatedInitialProfileRepository{
		MemoryRepository: NewMemoryRepository(),
		initialReads:     make(chan struct{}),
		applyReads:       make(chan struct{}),
	}
}

func (r *coordinatedInitialProfileRepository) GetAll(ctx context.Context) (map[string]string, error) {
	values, err := r.MemoryRepository.GetAll(ctx)
	read := r.reads.Add(1)
	var barrier chan struct{}
	switch read {
	case 1, 2:
		barrier = r.initialReads
	case 3, 4:
		barrier = r.applyReads
	}
	if barrier != nil {
		if read == 2 || read == 4 {
			close(barrier)
		}
		<-barrier
	}
	return values, err
}

func (r *coordinatedInitialProfileRepository) InitializeDeploymentProfile(ctx context.Context, profile string) error {
	// The atomic path bypasses both coordinated read phases used to reproduce the legacy race.
	r.reads.Store(4)
	return r.MemoryRepository.InitializeDeploymentProfile(ctx, profile)
}

func TestApplyInitialProfileSerializesConflictingConcurrentInstalls(t *testing.T) {
	repo := newCoordinatedInitialProfileRepository()
	services := []*Service{
		NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory"}),
		NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory"}),
	}
	profiles := []string{"enterprise", "platform"}
	start := make(chan struct{})
	results := make(chan error, len(services))
	for index, service := range services {
		go func(svc *Service, profile string) {
			<-start
			_, err := svc.ApplyInitialProfile(context.Background(), profile)
			results <- err
		}(service, profiles[index])
	}
	close(start)

	succeeded := 0
	conflicted := 0
	for range services {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrDeploymentProfileInitialized):
			conflicted++
		default:
			t.Fatalf("ApplyInitialProfile() unexpected error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent install results: succeeded=%d conflicted=%d", succeeded, conflicted)
	}

	current, err := services[0].Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin(): %v", err)
	}
	if !current.SetupCompleted || len(current.EnabledProfiles) != 1 || current.DefaultProfile != current.EnabledProfiles[0] {
		t.Fatalf("persisted deployment profile is inconsistent: %+v", current.PublicSettings)
	}
}

func TestApplyProfilesSerializesConflictingConcurrentInstalls(t *testing.T) {
	repo := newCoordinatedInitialProfileRepository()
	services := []*Service{
		NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory"}),
		NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory"}),
	}
	profiles := []string{"enterprise", "platform"}
	start := make(chan struct{})
	results := make(chan error, len(services))
	for index, service := range services {
		go func(svc *Service, profile string) {
			<-start
			_, err := svc.ApplyProfiles(context.Background(), []string{profile}, profile)
			results <- err
		}(service, profiles[index])
	}
	close(start)

	succeeded := 0
	conflicted := 0
	for range services {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrDeploymentProfileInitialized):
			conflicted++
		default:
			t.Fatalf("ApplyProfiles() unexpected error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent profile results: succeeded=%d conflicted=%d", succeeded, conflicted)
	}

	current, err := services[0].Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin(): %v", err)
	}
	if !current.SetupCompleted || len(current.EnabledProfiles) != 1 || current.DefaultProfile != current.EnabledProfiles[0] {
		t.Fatalf("persisted deployment profile is inconsistent: %+v", current.PublicSettings)
	}
}

func TestBootstrapProfilePersistsSingleConfiguredProfile(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, ServiceOptions{
		Version: "test", StorageMode: "memory", EnabledProfiles: []string{"platform"}, DefaultProfile: "platform",
	})
	if err := svc.BootstrapProfile(context.Background()); err != nil {
		t.Fatalf("BootstrapProfile() error = %v", err)
	}
	raw, err := repo.GetAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if raw[KeyEnabledProfiles] != `["platform"]` || raw[KeyDefaultProfile] != "platform" || raw[KeySetupCompleted] != "true" {
		t.Fatalf("bootstrap settings = %v", raw)
	}
	if err := svc.BootstrapProfile(context.Background()); err != nil {
		t.Fatalf("repeat BootstrapProfile() error = %v", err)
	}
}

func TestBootstrapProfileRejectsConfiguredRoleThatConflictsWithPersistedRole(t *testing.T) {
	repo := NewMemoryRepository()
	installed := NewService(repo, ServiceOptions{Version: "test", StorageMode: "memory"})
	if _, err := installed.ApplyInitialProfile(context.Background(), "enterprise"); err != nil {
		t.Fatalf("ApplyInitialProfile() error = %v", err)
	}

	configuredAsPlatform := NewService(repo, ServiceOptions{
		Version: "test", StorageMode: "memory", EnabledProfiles: []string{"platform"}, DefaultProfile: "platform",
	})
	if err := configuredAsPlatform.BootstrapProfile(context.Background()); err == nil {
		t.Fatal("BootstrapProfile() accepted a bootstrap role that conflicts with the persisted installation")
	}

	stored, err := installed.Admin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored.DefaultProfile != "enterprise" || len(stored.EnabledProfiles) != 1 || stored.EnabledProfiles[0] != "enterprise" {
		t.Fatalf("persisted deployment profile changed: %+v", stored.PublicSettings)
	}
}

func TestBootstrapProfileRejectsUnsupportedConfiguredRole(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, ServiceOptions{
		Version: "test", StorageMode: "memory", EnabledProfiles: []string{"unknown"}, DefaultProfile: "unknown",
	})
	if err := svc.BootstrapProfile(context.Background()); err == nil {
		t.Fatal("BootstrapProfile() accepted an unsupported configured role")
	}
	values, err := repo.GetAll(context.Background())
	if err != nil {
		t.Fatalf("GetAll(): %v", err)
	}
	if len(values) != 0 {
		t.Fatalf("unsupported bootstrap role mutated settings: %#v", values)
	}
}

func TestMemoryRepositoryRejectsUnsupportedDeploymentProfile(t *testing.T) {
	repo := NewMemoryRepository()
	if err := repo.InitializeDeploymentProfile(context.Background(), "unknown"); err == nil {
		t.Fatal("InitializeDeploymentProfile() accepted an unsupported role")
	}
	values, err := repo.GetAll(context.Background())
	if err != nil {
		t.Fatalf("GetAll(): %v", err)
	}
	if len(values) != 0 {
		t.Fatalf("unsupported role mutated settings: %#v", values)
	}
}

func TestDemoModeCompletesSetupWithAllProfiles(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory", DemoMode: true})
	got, err := svc.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin() error = %v", err)
	}
	if !got.SetupCompleted || !got.DemoMode || got.DefaultProfile != "personal" {
		t.Fatalf("demo settings not applied: %+v", got.PublicSettings)
	}
	if len(got.EnabledProfiles) != 4 {
		t.Fatalf("EnabledProfiles = %+v", got.EnabledProfiles)
	}
}

func TestDemoModeDoesNotOverrideConfiguredProfiles(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{
		Version:         "test",
		StorageMode:     "memory",
		DemoMode:        true,
		EnabledProfiles: []string{"enterprise"},
		DefaultProfile:  "enterprise",
	})
	got, err := svc.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin() error = %v", err)
	}
	if got.DefaultProfile != "enterprise" || len(got.EnabledProfiles) != 1 || got.EnabledProfiles[0] != "enterprise" {
		t.Fatalf("configured profiles overridden: %+v", got.PublicSettings)
	}
}

func TestUpdateCannotBypassDeploymentProfileInvariant(t *testing.T) {
	svc := NewService(NewMemoryRepository(), ServiceOptions{Version: "test", StorageMode: "memory"})
	current, err := svc.ApplyInitialProfile(context.Background(), "platform")
	if err != nil {
		t.Fatal(err)
	}
	current.EnabledProfiles = []string{"platform", "enterprise"}
	if _, err := svc.Update(context.Background(), current); err == nil {
		t.Fatal("Update() accepted multiple deployment profiles")
	}
	current.EnabledProfiles = []string{"enterprise"}
	current.DefaultProfile = "enterprise"
	if _, err := svc.Update(context.Background(), current); err == nil {
		t.Fatal("Update() changed the installed deployment profile")
	}
	stored, err := svc.Admin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored.DefaultProfile != "platform" || len(stored.EnabledProfiles) != 1 || stored.EnabledProfiles[0] != "platform" {
		t.Fatalf("stored deployment profile = %+v", stored.PublicSettings)
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

func TestValidateLegalDocumentsRejectsDuplicateSlug(t *testing.T) {
	err := validateLegalDocuments([]LegalDocument{
		{ID: "terms", Name: "Terms", Slug: "terms", Content: "one"},
		{ID: "privacy", Name: "Privacy", Slug: "terms", Content: "two"},
	}, true)
	if err == nil {
		t.Fatal("validateLegalDocuments() error = nil, want duplicate slug error")
	}
}

func TestParseIntListFallsBackOnInvalidJSON(t *testing.T) {
	fallback := []int{10, 20, 50}
	got := parseIntList("invalid", fallback)
	if len(got) != len(fallback) || got[1] != 20 {
		t.Fatalf("parseIntList() = %v, want %v", got, fallback)
	}
}
