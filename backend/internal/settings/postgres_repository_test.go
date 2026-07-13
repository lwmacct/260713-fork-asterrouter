package settings

import (
	"context"
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
