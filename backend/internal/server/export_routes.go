package server

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

const (
	csvExportLimit      = 5000
	csvExportBatchSize  = 500
	asyncCSVExportLimit = 50000
)

func registerCSVExportJobRoutes(group *gin.RouterGroup, control *controlplane.Service, store CSVExportJobStore) {
	if control == nil || store == nil {
		return
	}
	group.GET("", func(c *gin.Context) {
		limit := intQuery(c, "limit", 50)
		jobs, err := store.list(c.Request.Context(), 100)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1804, err.Error())
			return
		}
		visible := make([]csvExportJob, 0, len(jobs))
		for _, job := range jobs {
			if exportJobVisible(c, job) {
				visible = append(visible, job)
			}
		}
		if limit > 0 && len(visible) > limit {
			visible = visible[:limit]
		}
		httpx.OK(c, visible)
	})
	group.POST("", func(c *gin.Context) {
		kind := exportJobKind(c)
		if kind == "audit_logs" && !principalAccess(c).Global {
			httpx.Error(c, http.StatusForbidden, 1451, "department-scoped access does not include global audit logs")
			return
		}
		filename, run, ok := exportJobRunner(c, control, kind)
		if !ok {
			httpx.Error(c, http.StatusBadRequest, 1801, "unsupported export kind")
			return
		}
		job, err := store.create(c.Request.Context(), actor(c), kind, filename, exportJobParameters(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1804, err.Error())
			return
		}
		_ = control.RecordExportEvent(c.Request.Context(), actor(c), "create", job.ID, fmt.Sprintf("Created %s export job with limit=%d", kind, asyncExportLimit(c)))
		go runCSVExportJob(store, job.ID, run)
		httpx.OK(c, job)
	})
	group.GET("/:id", func(c *gin.Context) {
		job, ok, err := store.get(c.Request.Context(), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1804, err.Error())
			return
		}
		if !ok {
			httpx.Error(c, http.StatusNotFound, 1802, "export job not found")
			return
		}
		if !exportJobVisible(c, job) {
			httpx.Error(c, http.StatusNotFound, 1802, "export job not found")
			return
		}
		httpx.OK(c, job)
	})
	group.GET("/:id/download", func(c *gin.Context) {
		job, body, ok, err := store.getDownload(c.Request.Context(), c.Param("id"))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1804, err.Error())
			return
		}
		if !ok {
			httpx.Error(c, http.StatusNotFound, 1803, "export job result is not available")
			return
		}
		if !exportJobVisible(c, job) {
			httpx.Error(c, http.StatusNotFound, 1803, "export job result is not available")
			return
		}
		_ = control.RecordExportEvent(c.Request.Context(), actor(c), "download", job.ID, fmt.Sprintf("Downloaded %s export job with %d rows", job.Kind, job.RowCount))
		c.Header("Content-Disposition", `attachment; filename="`+job.Filename+`"`)
		c.Data(http.StatusOK, job.ContentType, body)
	})
}

func exportJobVisible(c *gin.Context, job csvExportJob) bool {
	return principalAccess(c).Global || (job.Owner != "" && job.Owner == actor(c))
}

func runCSVExportJob(store CSVExportJobStore, id string, run func(context.Context) ([][]string, error)) {
	ctx := context.Background()
	if err := store.markRunning(ctx, id); err != nil {
		return
	}
	rows, err := run(ctx)
	if err != nil {
		_ = store.markFailed(ctx, id, err)
		return
	}
	body, err := csvRowsToBytes(rows)
	if err != nil {
		_ = store.markFailed(ctx, id, err)
		return
	}
	_ = store.markSucceeded(ctx, id, maxInt(0, len(rows)-1), body)
}

func exportJobKind(c *gin.Context) string {
	var req struct {
		Kind string `json:"kind"`
	}
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		_ = c.ShouldBindJSON(&req)
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = strings.TrimSpace(c.Query("kind"))
	}
	return kind
}

func exportJobParameters(c *gin.Context) map[string]string {
	keys := []string{"q", "model", "status", "api_key_id", "action", "resource_type", "from", "to", "limit", "offset"}
	parameters := map[string]string{}
	for _, key := range keys {
		value := strings.TrimSpace(c.Query(key))
		if value != "" {
			parameters[key] = value
		}
	}
	if _, ok := parameters["limit"]; !ok {
		parameters["limit"] = strconv.Itoa(asyncCSVExportLimit)
	}
	return parameters
}

func exportJobRunner(c *gin.Context, control *controlplane.Service, kind string) (string, func(context.Context) ([][]string, error), bool) {
	totalLimit, baseOffset := asyncExportWindow(c)
	switch kind {
	case "usage":
		query, scopeErr := scopeUsageQuery(c.Request.Context(), control, principalAccess(c), usageQuery(c))
		return "usage-records.csv", func(ctx context.Context) ([][]string, error) {
			if scopeErr != nil {
				return nil, scopeErr
			}
			records, err := collectUsageRecordsForExportQuery(ctx, control, query, totalLimit, baseOffset)
			if err != nil {
				return nil, err
			}
			return usageCSVRows(records), nil
		}, true
	case "gateway_traces":
		query, scopeErr := scopeGatewayTraceQuery(c.Request.Context(), control, principalAccess(c), gatewayTraceQuery(c))
		return "gateway-traces.csv", func(ctx context.Context) ([][]string, error) {
			if scopeErr != nil {
				return nil, scopeErr
			}
			traces, err := collectGatewayTracesForExportQuery(ctx, control, query, totalLimit, baseOffset)
			if err != nil {
				return nil, err
			}
			return gatewayTraceCSVRows(traces), nil
		}, true
	case "audit_logs":
		query := auditLogQuery(c)
		return "audit-logs.csv", func(ctx context.Context) ([][]string, error) {
			logs, err := collectAuditLogsForExportQuery(ctx, control, query, totalLimit, baseOffset)
			if err != nil {
				return nil, err
			}
			return auditLogCSVRows(logs), nil
		}, true
	default:
		return "", nil, false
	}
}

func collectUsageRecordsForExport(c *gin.Context, control *controlplane.Service) ([]controlplane.UsageRecord, error) {
	totalLimit, baseOffset := exportWindow(c)
	return collectUsageRecordsForExportQuery(c.Request.Context(), control, usageQuery(c), totalLimit, baseOffset)
}

func collectUsageRecordsForExportQuery(ctx context.Context, control *controlplane.Service, query controlplane.UsageQuery, totalLimit int, baseOffset int) ([]controlplane.UsageRecord, error) {
	records := make([]controlplane.UsageRecord, 0, minInt(totalLimit, csvExportBatchSize))
	for len(records) < totalLimit {
		batchSize := minInt(csvExportBatchSize, totalLimit-len(records))
		query.Limit = batchSize
		query.Offset = baseOffset + len(records)
		report, err := control.UsageReportQuery(ctx, query)
		if err != nil {
			return nil, err
		}
		if len(report.Recent) == 0 {
			break
		}
		records = append(records, report.Recent...)
		if len(report.Recent) < batchSize {
			break
		}
	}
	return records, nil
}

func collectGatewayTracesForExport(c *gin.Context, control *controlplane.Service) ([]controlplane.GatewayTrace, error) {
	totalLimit, baseOffset := exportWindow(c)
	return collectGatewayTracesForExportQuery(c.Request.Context(), control, gatewayTraceQuery(c), totalLimit, baseOffset)
}

func collectGatewayTracesForExportQuery(ctx context.Context, control *controlplane.Service, query controlplane.GatewayTraceQuery, totalLimit int, baseOffset int) ([]controlplane.GatewayTrace, error) {
	traces := make([]controlplane.GatewayTrace, 0, minInt(totalLimit, csvExportBatchSize))
	for len(traces) < totalLimit {
		batchSize := minInt(csvExportBatchSize, totalLimit-len(traces))
		query.Limit = batchSize
		query.Offset = baseOffset + len(traces)
		batch, err := control.ListGatewayTracesQuery(ctx, query)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		traces = append(traces, batch...)
		if len(batch) < batchSize {
			break
		}
	}
	return traces, nil
}

func collectAuditLogsForExport(c *gin.Context, control *controlplane.Service) ([]controlplane.AuditLog, error) {
	totalLimit, baseOffset := exportWindow(c)
	return collectAuditLogsForExportQuery(c.Request.Context(), control, auditLogQuery(c), totalLimit, baseOffset)
}

func collectAuditLogsForExportQuery(ctx context.Context, control *controlplane.Service, query controlplane.AuditLogQuery, totalLimit int, baseOffset int) ([]controlplane.AuditLog, error) {
	logs := make([]controlplane.AuditLog, 0, minInt(totalLimit, csvExportBatchSize))
	for len(logs) < totalLimit {
		batchSize := minInt(csvExportBatchSize, totalLimit-len(logs))
		query.Limit = batchSize
		query.Offset = baseOffset + len(logs)
		batch, err := control.ListAuditLogsQuery(ctx, query)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		logs = append(logs, batch...)
		if len(batch) < batchSize {
			break
		}
	}
	return logs, nil
}

func usageCSVRows(records []controlplane.UsageRecord) [][]string {
	rows := [][]string{{"time", "api_key_id", "api_fingerprint", "model", "upstream_model", "provider_id", "provider_account_id", "status", "error_type", "input_tokens", "output_tokens", "cost_cents", "latency_ms"}}
	for _, record := range records {
		rows = append(rows, []string{
			record.CreatedAt.Format(time.RFC3339),
			record.APIKeyID,
			record.APIFingerprint,
			record.Model,
			record.UpstreamModel,
			record.ProviderID,
			record.ProviderAccountID,
			record.Status,
			record.ErrorType,
			strconv.Itoa(record.InputTokens),
			strconv.Itoa(record.OutputTokens),
			strconv.Itoa(record.CostCents),
			strconv.FormatInt(record.LatencyMS, 10),
		})
	}
	return rows
}

func costAllocationCSVRows(report controlplane.CostAllocationReport) [][]string {
	rows := [][]string{{"dimension", "resource_id", "resource_name", "api_key_id", "api_key_name", "api_fingerprint", "model", "requests", "error_requests", "total_tokens", "total_cost_cents", "avg_latency_ms", "budget_cents", "budget_used_percent", "cost_share_percent"}}
	for _, row := range report.Rows {
		rows = append(rows, []string{
			row.Dimension,
			row.ResourceID,
			row.ResourceName,
			row.APIKeyID,
			row.APIKeyName,
			row.APIFingerprint,
			row.Model,
			strconv.Itoa(row.Requests),
			strconv.Itoa(row.ErrorRequests),
			strconv.Itoa(row.TotalTokens),
			strconv.Itoa(row.TotalCostCents),
			strconv.FormatInt(row.AvgLatencyMS, 10),
			strconv.Itoa(row.BudgetCents),
			strconv.FormatFloat(row.BudgetUsedPercent, 'f', 2, 64),
			strconv.FormatFloat(row.CostSharePercent, 'f', 2, 64),
		})
	}
	return rows
}

func writeCostAllocationError(c *gin.Context, err error) {
	if errors.Is(err, controlplane.ErrInvalidCostAllocationDimension) {
		httpx.Error(c, http.StatusBadRequest, 1111, err.Error())
		return
	}
	httpx.Error(c, http.StatusInternalServerError, 1110, err.Error())
}

func gatewayTraceCSVRows(traces []controlplane.GatewayTrace) [][]string {
	rows := [][]string{{"time", "api_key_id", "api_fingerprint", "model", "stream", "message_count", "provider_id", "provider_account_id", "gateway_model_id", "route_id", "route_group", "upstream_model", "route_source", "route_reason", "policy_id", "policy_name", "policy_source", "policy_version", "policy_snapshot", "status", "http_status", "error_type", "input_tokens", "output_tokens", "latency_ms", "request_summary", "response_summary", "route_attempts"}}
	for _, trace := range traces {
		rows = append(rows, []string{
			trace.CreatedAt.Format(time.RFC3339),
			trace.APIKeyID,
			trace.APIFingerprint,
			trace.Model,
			strconv.FormatBool(trace.Stream),
			strconv.Itoa(trace.MessageCount),
			trace.ProviderID,
			trace.ProviderAccountID,
			trace.GatewayModelID,
			trace.RouteID,
			trace.RouteGroup,
			trace.UpstreamModel,
			trace.RouteSource,
			trace.RouteReason,
			trace.PolicyID,
			trace.PolicyName,
			trace.PolicySource,
			strconv.Itoa(trace.PolicyVersion),
			trace.PolicySnapshot,
			trace.Status,
			strconv.Itoa(trace.HTTPStatus),
			trace.ErrorType,
			strconv.Itoa(trace.InputTokens),
			strconv.Itoa(trace.OutputTokens),
			strconv.FormatInt(trace.LatencyMS, 10),
			trace.RequestSummary,
			trace.ResponseSummary,
			trace.RouteAttempts,
		})
	}
	return rows
}

func auditLogCSVRows(logs []controlplane.AuditLog) [][]string {
	rows := [][]string{{"time", "actor", "action", "resource_type", "resource_id", "summary"}}
	for _, log := range logs {
		rows = append(rows, []string{
			log.CreatedAt.Format(time.RFC3339),
			log.Actor,
			log.Action,
			log.ResourceType,
			log.ResourceID,
			log.Summary,
		})
	}
	return rows
}

func writeCSV(c *gin.Context, filename string, rows [][]string) {
	body, err := csvRowsToBytes(rows)
	if err != nil {
		httpx.Error(c, http.StatusInternalServerError, 1704, err.Error())
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", body)
}

func csvRowsToBytes(rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.WriteAll(rows); err != nil {
		return nil, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func exportWindow(c *gin.Context) (int, int) {
	limit := intQuery(c, "limit", csvExportLimit)
	if limit <= 0 || limit > csvExportLimit {
		limit = csvExportLimit
	}
	return limit, exportOffset(c)
}

func asyncExportWindow(c *gin.Context) (int, int) {
	return asyncExportLimit(c), exportOffset(c)
}

func asyncExportLimit(c *gin.Context) int {
	limit := intQuery(c, "limit", asyncCSVExportLimit)
	if limit <= 0 || limit > asyncCSVExportLimit {
		return asyncCSVExportLimit
	}
	return limit
}

func exportOffset(c *gin.Context) int {
	offset := intQuery(c, "offset", 0)
	if offset < 0 {
		return 0
	}
	return offset
}

func usageQuery(c *gin.Context) controlplane.UsageQuery {
	return controlplane.UsageQuery{
		Limit:       intQuery(c, "limit", 50),
		Offset:      intQuery(c, "offset", 0),
		Search:      strings.TrimSpace(c.Query("q")),
		APIKeyID:    strings.TrimSpace(c.Query("api_key_id")),
		CustomerID:  strings.TrimSpace(c.Query("customer_id")),
		Model:       strings.TrimSpace(c.Query("model")),
		ProviderID:  strings.TrimSpace(c.Query("provider_id")),
		AccountID:   strings.TrimSpace(c.Query("provider_account_id")),
		Status:      strings.TrimSpace(c.Query("status")),
		CreatedFrom: timeQuery(c, "from"),
		CreatedTo:   timeQuery(c, "to"),
	}
}

func gatewayTraceQuery(c *gin.Context) controlplane.GatewayTraceQuery {
	return controlplane.GatewayTraceQuery{
		Limit:       intQuery(c, "limit", 50),
		Offset:      intQuery(c, "offset", 0),
		Search:      strings.TrimSpace(c.Query("q")),
		APIKeyID:    strings.TrimSpace(c.Query("api_key_id")),
		Model:       strings.TrimSpace(c.Query("model")),
		Status:      strings.TrimSpace(c.Query("status")),
		CreatedFrom: timeQuery(c, "from"),
		CreatedTo:   timeQuery(c, "to"),
	}
}

func auditLogQuery(c *gin.Context) controlplane.AuditLogQuery {
	return controlplane.AuditLogQuery{
		Limit:        intQuery(c, "limit", 50),
		Offset:       intQuery(c, "offset", 0),
		Search:       strings.TrimSpace(c.Query("q")),
		Action:       strings.TrimSpace(c.Query("action")),
		ResourceType: strings.TrimSpace(c.Query("resource_type")),
		CreatedFrom:  timeQuery(c, "from"),
		CreatedTo:    timeQuery(c, "to"),
	}
}

func intQuery(c *gin.Context, key string, fallback int) int {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func timeQuery(c *gin.Context, key string) time.Time {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
