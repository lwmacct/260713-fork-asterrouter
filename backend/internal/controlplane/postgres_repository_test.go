package controlplane

import (
	"context"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestPostgresRepositoryPersistsCoreRecordsAcrossRestart(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	repo, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("NewPostgresRepository(): %v", err)
	}
	provider := ProviderConnection{
		ID: "provider-postgres", Name: "Postgres Provider", Type: "openai_compatible",
		BaseURL: "https://provider.test/v1", Status: ProviderStatusActive, Models: []string{"model-a"},
		SecretConfigured: true, SecretHint: "...test", SecretCiphertext: "ciphertext", CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.SaveProvider(ctx, provider); err != nil {
		t.Fatalf("SaveProvider(): %v", err)
	}
	key := APIKeyRecord{
		ID: "key-postgres", Name: "Postgres Key", KeyHash: "hash-postgres", Fingerprint: "fingerprint",
		Prefix: "ast_test", Status: APIKeyStatusActive, KeyType: APIKeyTypeWorkspace,
		ModelAllowlist: []string{"model-a"}, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.SaveAPIKey(ctx, key); err != nil {
		t.Fatalf("SaveAPIKey(): %v", err)
	}
	if err := repo.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	reopened, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("reopen NewPostgresRepository(): %v", err)
	}
	defer reopened.Close()
	providers, err := reopened.ListProviders(ctx)
	if err != nil {
		t.Fatalf("ListProviders(): %v", err)
	}
	if len(providers) != 1 || providers[0].ID != provider.ID || providers[0].SecretCiphertext != "ciphertext" {
		t.Fatalf("persisted providers = %#v", providers)
	}
	found, ok, err := reopened.FindAPIKeyByHash(ctx, key.KeyHash)
	if err != nil {
		t.Fatalf("FindAPIKeyByHash(): %v", err)
	}
	if !ok || found.ID != key.ID || len(found.ModelAllowlist) != 1 || found.ModelAllowlist[0] != "model-a" {
		t.Fatalf("persisted key ok=%t key=%#v", ok, found)
	}
}
