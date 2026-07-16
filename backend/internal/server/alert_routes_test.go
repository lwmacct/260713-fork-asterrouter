package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestAdminAlertEndpoints(t *testing.T) {
	ctx := context.Background()
	handler, control := newTestRuntime(t, RuntimeConfig{})
	created, err := control.CreateAPIKey(ctx, "tester", controlplane.APIKeyCreateRequest{
		Name:              "HTTP alert key",
		ModelAllowlist:    []string{"gpt-alert"},
		QPSLimit:          0,
		MonthlyTokenLimit: 100,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := control.AuthorizeGatewayModel(ctx, created.Key, "gpt-alert")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := control.RecordGatewayUsage(ctx, auth, controlplane.GatewayUsageInput{Model: "gpt-alert", Status: "forwarded", InputTokens: 100}); err != nil {
		t.Fatalf("RecordGatewayUsage(): %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/alerts?type=api_key_quota&status=active", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Data []controlplane.AlertEvent `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Data) != 1 || listResp.Data[0].Severity != controlplane.AlertSeverityCritical {
		t.Fatalf("alert list mismatch: %+v", listResp.Data)
	}
	alertID := listResp.Data[0].ID

	summaryReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/alerts/summary?type=api_key_quota&status=active", nil)
	summaryRec := httptest.NewRecorder()
	handler.ServeHTTP(summaryRec, summaryReq)
	if summaryRec.Code != http.StatusOK {
		t.Fatalf("summary status = %d body=%s", summaryRec.Code, summaryRec.Body.String())
	}
	var summaryResp struct {
		Data controlplane.AlertSummary `json:"data"`
	}
	if err := json.Unmarshal(summaryRec.Body.Bytes(), &summaryResp); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summaryResp.Data.Total != 1 || summaryResp.Data.Critical != 1 {
		t.Fatalf("summary mismatch: %+v", summaryResp.Data)
	}

	ackReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/alerts/"+alertID+"/acknowledge", nil)
	ackRec := httptest.NewRecorder()
	handler.ServeHTTP(ackRec, ackReq)
	if ackRec.Code != http.StatusOK {
		t.Fatalf("ack status = %d body=%s", ackRec.Code, ackRec.Body.String())
	}
	var ackResp struct {
		Data controlplane.AlertEvent `json:"data"`
	}
	if err := json.Unmarshal(ackRec.Body.Bytes(), &ackResp); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if ackResp.Data.Status != controlplane.AlertStatusAcknowledged {
		t.Fatalf("ack mismatch: %+v", ackResp.Data)
	}

	resolveReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/alerts/"+alertID+"/resolve", nil)
	resolveRec := httptest.NewRecorder()
	handler.ServeHTTP(resolveRec, resolveReq)
	if resolveRec.Code != http.StatusOK {
		t.Fatalf("resolve status = %d body=%s", resolveRec.Code, resolveRec.Body.String())
	}
	var resolveResp struct {
		Data controlplane.AlertEvent `json:"data"`
	}
	if err := json.Unmarshal(resolveRec.Body.Bytes(), &resolveResp); err != nil {
		t.Fatalf("decode resolve: %v", err)
	}
	if resolveResp.Data.Status != controlplane.AlertStatusResolved {
		t.Fatalf("resolve mismatch: %+v", resolveResp.Data)
	}
}
