package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestAdminRecordExportEndpointsSupportQueryParameters(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "export key",
		ModelAllowlist:    []string{"model-a", "model-b"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := control.AuthorizeGatewayModel(context.Background(), created.Key, "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := control.RecordGatewayUsage(context.Background(), auth, controlplane.GatewayUsageInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-a", InputTokens: 1}); err != nil {
		t.Fatalf("RecordGatewayUsage a: %v", err)
	}
	if err := control.RecordGatewayUsage(context.Background(), auth, controlplane.GatewayUsageInput{Model: "model-b", Status: "error", ProviderID: "provider-b", ErrorType: "policy_error", InputTokens: 2}); err != nil {
		t.Fatalf("RecordGatewayUsage b: %v", err)
	}
	if err := control.RecordGatewayTrace(context.Background(), auth, controlplane.GatewayTraceInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-a", ResponseSummary: "ok"}); err != nil {
		t.Fatalf("RecordGatewayTrace a: %v", err)
	}
	if err := control.RecordGatewayTrace(context.Background(), auth, controlplane.GatewayTraceInput{Model: "model-b", Status: "error", ProviderID: "provider-b", ErrorType: "policy_error", ResponseSummary: "export blocked"}); err != nil {
		t.Fatalf("RecordGatewayTrace b: %v", err)
	}
	if err := control.RecordGatewayCall(context.Background(), auth, "model-b", "error", "Export query audit marker"); err != nil {
		t.Fatalf("RecordGatewayCall(): %v", err)
	}

	usageReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage/export?model=model-b&status=error&limit=10", nil)
	usageRec := httptest.NewRecorder()
	handler.ServeHTTP(usageRec, usageReq)
	usageRows := readCSVRows(t, usageRec)
	if len(usageRows) != 2 || usageRows[0][3] != "model" || usageRows[0][4] != "upstream_model" || usageRows[1][3] != "model-b" || usageRows[1][7] != "error" {
		t.Fatalf("usage export query not applied: %+v", usageRows)
	}
	if strings.Contains(usageRec.Body.String(), "model-a") {
		t.Fatalf("usage export leaked filtered record: %s", usageRec.Body.String())
	}

	traceReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces/export?status=error&q=blocked&limit=10", nil)
	traceRec := httptest.NewRecorder()
	handler.ServeHTTP(traceRec, traceReq)
	traceRows := readCSVRows(t, traceRec)
	if len(traceRows) != 2 || traceRows[0][3] != "model" || traceRows[0][11] != "upstream_model" || traceRows[0][18] != "policy_snapshot" || traceRows[0][19] != "status" || traceRows[1][3] != "model-b" || traceRows[1][19] != "error" {
		t.Fatalf("trace export query not applied: %+v", traceRows)
	}
	if strings.Contains(traceRec.Body.String(), "provider-a") {
		t.Fatalf("trace export leaked filtered record: %s", traceRec.Body.String())
	}

	auditReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs/export?action=invoke&resource_type=gateway_call&q=Export&limit=10", nil)
	auditRec := httptest.NewRecorder()
	handler.ServeHTTP(auditRec, auditReq)
	auditRows := readCSVRows(t, auditRec)
	if len(auditRows) != 2 || auditRows[0][2] != "action" || auditRows[1][2] != "invoke" || !strings.Contains(auditRows[1][5], "Export") {
		t.Fatalf("audit export query not applied: %+v", auditRows)
	}
}

func TestAdminAsyncExportJobLifecycle(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "async export key",
		ModelAllowlist:    []string{"model-a"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := control.AuthorizeGatewayModel(context.Background(), created.Key, "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := control.RecordGatewayCall(context.Background(), auth, "model-a", "forwarded", "AsyncExport marker"); err != nil {
		t.Fatalf("RecordGatewayCall(): %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/export-jobs?kind=audit_logs&action=invoke&q=AsyncExport&limit=10", nil)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var createResp struct {
		Data csvExportJob `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if createResp.Data.ID == "" || createResp.Data.Kind != "audit_logs" {
		t.Fatalf("unexpected created job: %+v", createResp.Data)
	}

	job := waitExportJob(t, handler, createResp.Data.ID)
	if job.Status != exportJobStatusSucceeded || job.RowCount != 1 || job.SizeBytes == 0 {
		t.Fatalf("job did not succeed with expected metadata: %+v", job)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export-jobs/"+job.ID+"/download", nil)
	downloadRec := httptest.NewRecorder()
	handler.ServeHTTP(downloadRec, downloadReq)
	rows := readCSVRows(t, downloadRec)
	if len(rows) != 2 || rows[1][2] != "invoke" || !strings.Contains(rows[1][5], "AsyncExport") {
		t.Fatalf("async export CSV mismatch: %+v", rows)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export-jobs?limit=5", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), job.ID) {
		t.Fatalf("list missing job status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	badReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/export-jobs?kind=unknown", nil)
	badRec := httptest.NewRecorder()
	handler.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("bad kind status = %d body=%s", badRec.Code, badRec.Body.String())
	}
}

func waitExportJob(t *testing.T, handler http.Handler, id string) csvExportJob {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last csvExportJob
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export-jobs/"+id, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("job status = %d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data csvExportJob `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode job: %v", err)
		}
		last = resp.Data
		if last.Status == exportJobStatusSucceeded || last.Status == exportJobStatusFailed {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("export job did not finish: %+v", last)
	return csvExportJob{}
}

func readCSVRows(t *testing.T, rec *httptest.ResponseRecorder) [][]string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("csv status = %d body=%s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/csv") {
		t.Fatalf("csv content-type = %q", contentType)
	}
	rows, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v body=%s", err, rec.Body.String())
	}
	return rows
}
