package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestAdminAPIKeyRotationAcceptsGracePeriod(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "secret"})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name: "grace route", ModelAllowlist: []string{"model-a"},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/api-keys/"+created.Record.ID+"/rotate", bytes.NewBufferString(`{"grace_period_seconds":300}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Data controlplane.APIKeyCreateResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Record.ReplacesKeyID != created.Record.ID || response.Data.Key == "" {
		t.Fatalf("rotation response=%+v", response.Data)
	}
	if _, err := control.AuthenticateGatewayKey(context.Background(), created.Key); err != nil {
		t.Fatalf("previous key rejected during grace: %v", err)
	}
	keys, err := control.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	previous := apiKeyRecordByIDForRouteTest(keys, created.Record.ID)
	if previous.ReplacedByKeyID != response.Data.Record.ID || previous.RotationGraceExpiresAt == nil || time.Until(*previous.RotationGraceExpiresAt) < 4*time.Minute {
		t.Fatalf("previous key=%+v", previous)
	}
}

func TestAdminAPIKeyRotationRejectsInvalidGracePeriodWithoutMutation(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "secret"})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name: "invalid grace route", ModelAllowlist: []string{"model-a"},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/api-keys/"+created.Record.ID+"/rotate", bytes.NewBufferString(`{"grace_period_seconds":86401}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("rotate status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := control.AuthenticateGatewayKey(context.Background(), created.Key); err != nil {
		t.Fatalf("original key changed after rejected rotation: %v", err)
	}
	keys, err := control.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if previous := apiKeyRecordByIDForRouteTest(keys, created.Record.ID); previous.ReplacedByKeyID != "" {
		t.Fatalf("original key mutated=%+v", previous)
	}
}

func apiKeyRecordByIDForRouteTest(keys []controlplane.APIKeyRecord, id string) controlplane.APIKeyRecord {
	for _, key := range keys {
		if key.ID == id {
			return key
		}
	}
	return controlplane.APIKeyRecord{}
}
