package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestManualRetentionCleanupEndpointReturnsEvidence(t *testing.T) {
	handler, _ := newTestRuntime(t, RuntimeConfig{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/settings/retention/cleanup", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cleanup status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Data controlplane.RetentionCleanupResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.Data.Before.IsZero() {
		t.Fatalf("cleanup response=%s err=%v", rec.Body.String(), err)
	}
}
