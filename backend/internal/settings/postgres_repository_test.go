package settings

import (
	"context"
	"errors"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestPostgresRepositoryPersistsSettingsAcrossRestart(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()

	repo, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("NewPostgresRepository(): %v", err)
	}
	if err := repo.SetMultiple(ctx, map[string]string{"site_name": "Test Router", "default_locale": "zh-CN"}); err != nil {
		t.Fatalf("SetMultiple(): %v", err)
	}
	if err := repo.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	reopened, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("reopen NewPostgresRepository(): %v", err)
	}
	defer reopened.Close()
	values, err := reopened.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll(): %v", err)
	}
	if values["site_name"] != "Test Router" || values["default_locale"] != "zh-CN" {
		t.Fatalf("persisted settings = %#v", values)
	}
}

func TestPostgresRepositorySerializesConflictingDeploymentProfiles(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	repositories := make([]*PostgresRepository, 2)
	for index := range repositories {
		repo, err := NewPostgresRepository(ctx, schema.URL)
		if err != nil {
			t.Fatalf("NewPostgresRepository(%d): %v", index, err)
		}
		repositories[index] = repo
	}
	profiles := []string{"enterprise", "platform"}
	start := make(chan struct{})
	results := make(chan error, len(repositories))
	for index, repo := range repositories {
		go func(repository *PostgresRepository, profile string) {
			<-start
			results <- repository.InitializeDeploymentProfile(ctx, profile)
		}(repo, profiles[index])
	}
	close(start)

	succeeded := 0
	conflicted := 0
	for range repositories {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrDeploymentProfileInitialized):
			conflicted++
		default:
			t.Fatalf("InitializeDeploymentProfile() unexpected error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent initialization results: succeeded=%d conflicted=%d", succeeded, conflicted)
	}
	for index, repo := range repositories {
		if err := repo.Close(); err != nil {
			t.Fatalf("Close(%d): %v", index, err)
		}
	}

	reopened, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("reopen NewPostgresRepository(): %v", err)
	}
	defer reopened.Close()
	values, err := reopened.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll(): %v", err)
	}
	persistedProfiles := normalizeProfiles(parseStringList(values[KeyEnabledProfiles], nil))
	if !parseBool(values[KeySetupCompleted]) || len(persistedProfiles) != 1 || values[KeyDefaultProfile] != persistedProfiles[0] {
		t.Fatalf("persisted deployment profile is inconsistent: %#v", values)
	}
	if persistedProfiles[0] != "enterprise" && persistedProfiles[0] != "platform" {
		t.Fatalf("persisted unexpected deployment profile: %#v", values)
	}
}
