package controlplane

import (
	"context"
	"testing"
	"time"
)

func TestCleanupRetainedDataDeletesExpiredRecordsButPreservesActiveAlertsAndAuditEvidence(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "secret")
	old := time.Now().UTC().Add(-60 * 24 * time.Hour)
	recent := time.Now().UTC().Add(-time.Hour)
	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
	repo.usageRecords["old_usage"] = UsageRecord{ID: "old_usage", CreatedAt: old}
	repo.usageRecords["recent_usage"] = UsageRecord{ID: "recent_usage", CreatedAt: recent}
	repo.gatewayTraces["old_trace"] = GatewayTrace{ID: "old_trace", CreatedAt: old}
	repo.gatewayTraces["recent_trace"] = GatewayTrace{ID: "recent_trace", CreatedAt: recent}
	repo.alertEvents["old_resolved"] = AlertEvent{ID: "old_resolved", Status: AlertStatusResolved, LastSeenAt: old}
	repo.alertEvents["old_active"] = AlertEvent{ID: "old_active", Status: AlertStatusActive, LastSeenAt: old}
	repo.auditLogs["old_audit"] = AuditLog{ID: "old_audit", CreatedAt: old}

	result, err := svc.CleanupRetainedData(ctx, "admin", cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if result.UsageRecords != 1 || result.GatewayTraces != 1 || result.AlertEvents != 1 || result.AuditLogs != 1 {
		t.Fatalf("cleanup result=%+v", result)
	}
	if _, ok := repo.usageRecords["recent_usage"]; !ok {
		t.Fatal("recent usage was deleted")
	}
	if _, ok := repo.alertEvents["old_active"]; !ok {
		t.Fatal("active alert was deleted")
	}
	logs, err := svc.ListAuditLogs(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, log := range logs {
		found = found || log.Action == "retention_cleanup" && log.ResourceID == "data_retention"
	}
	if !found {
		t.Fatalf("cleanup audit evidence missing: %+v", logs)
	}
}
