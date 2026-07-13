package plugins

import (
	"context"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestPostgresRepositoryPersistsPluginAcrossRestart(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	repo, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("NewPostgresRepository(): %v", err)
	}
	plugin := Plugin{
		ID: "plugin-postgres", PluginID: "official.test", Name: "Postgres Plugin", Category: "testing",
		Type: "builtin", Tier: TierFreeCore, Version: "1.0.0", Vendor: "AsterRouter",
		Status: StatusEnabled, EntitlementStatus: EntitlementFree, Surfaces: []string{"admin", "console"},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.SavePlugin(ctx, plugin); err != nil {
		t.Fatalf("SavePlugin(): %v", err)
	}
	if err := repo.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	reopened, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("reopen NewPostgresRepository(): %v", err)
	}
	defer reopened.Close()
	found, ok, err := reopened.FindPlugin(ctx, plugin.ID)
	if err != nil {
		t.Fatalf("FindPlugin(): %v", err)
	}
	if !ok || found.PluginID != plugin.PluginID || len(found.Surfaces) != 2 || found.Status != StatusEnabled {
		t.Fatalf("persisted plugin ok=%t plugin=%#v", ok, found)
	}
}
