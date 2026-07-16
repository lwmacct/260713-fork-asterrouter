package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func (r *MemoryRepository) QueryAlertEvents(_ context.Context, query AlertQuery) ([]AlertEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AlertEvent, 0, len(r.alertEvents))
	for _, event := range r.alertEvents {
		if memoryAlertEventMatches(event, query) {
			out = append(out, cloneAlertEvent(event))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeenAt.After(out[j].LastSeenAt) })
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 50, 500)
	if offset >= len(out) {
		return []AlertEvent{}, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (r *MemoryRepository) SummarizeAlertEvents(_ context.Context, query AlertQuery) (AlertSummary, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var summary AlertSummary
	for _, event := range r.alertEvents {
		if !memoryAlertEventMatches(event, query) {
			continue
		}
		summary.Total++
		switch event.Status {
		case AlertStatusActive:
			summary.Active++
		case AlertStatusAcknowledged:
			summary.Acknowledged++
		case AlertStatusResolved:
			summary.Resolved++
		}
		switch event.Severity {
		case AlertSeverityWarning:
			summary.Warning++
		case AlertSeverityCritical:
			summary.Critical++
		}
	}
	return summary, nil
}

func (r *MemoryRepository) FindAlertEvent(_ context.Context, id string) (AlertEvent, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	event, ok := r.alertEvents[strings.TrimSpace(id)]
	if !ok {
		return AlertEvent{}, false, nil
	}
	return cloneAlertEvent(event), true, nil
}

func (r *MemoryRepository) FindAlertByDedupeKey(_ context.Context, dedupeKey string) (AlertEvent, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	dedupeKey = strings.TrimSpace(dedupeKey)
	for _, event := range r.alertEvents {
		if event.DedupeKey == dedupeKey {
			return cloneAlertEvent(event), true, nil
		}
	}
	return AlertEvent{}, false, nil
}

func (r *MemoryRepository) SaveAlertEvent(_ context.Context, event AlertEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.alertEvents[event.ID] = cloneAlertEvent(event)
	return nil
}

func (r *PostgresRepository) QueryAlertEvents(ctx context.Context, query AlertQuery) ([]AlertEvent, error) {
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 50, 500)
	clauses := []string{}
	args := []any{}
	appendAlertEventFilters(&clauses, &args, query)
	sqlText := `
SELECT id, type, severity, status, title, summary, resource_type, resource_id, profile_scope, platform_tenant_id, platform_tenant_name, gateway_principal_id, gateway_principal_name, external_auth_integration_id, external_subject_reference, dedupe_key,
       metadata_json, first_seen_at, last_seen_at, acknowledged_at, acknowledged_by, resolved_at, resolved_by
FROM alert_events`
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit, offset)
	sqlText += fmt.Sprintf(" ORDER BY last_seen_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AlertEvent, 0)
	for rows.Next() {
		event, err := scanAlertEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SummarizeAlertEvents(ctx context.Context, query AlertQuery) (AlertSummary, error) {
	clauses := []string{}
	args := []any{}
	appendAlertEventFilters(&clauses, &args, query)
	sqlText := `
SELECT COUNT(*),
       COUNT(*) FILTER (WHERE status = 'active'),
       COUNT(*) FILTER (WHERE status = 'acknowledged'),
       COUNT(*) FILTER (WHERE status = 'resolved'),
       COUNT(*) FILTER (WHERE severity = 'warning'),
       COUNT(*) FILTER (WHERE severity = 'critical')
FROM alert_events`
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	var summary AlertSummary
	if err := r.db.QueryRowContext(ctx, sqlText, args...).Scan(
		&summary.Total,
		&summary.Active,
		&summary.Acknowledged,
		&summary.Resolved,
		&summary.Warning,
		&summary.Critical,
	); err != nil {
		return AlertSummary{}, err
	}
	return summary, nil
}

func (r *PostgresRepository) FindAlertEvent(ctx context.Context, id string) (AlertEvent, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return AlertEvent{}, false, nil
	}
	row := r.db.QueryRowContext(ctx, `
SELECT id, type, severity, status, title, summary, resource_type, resource_id, profile_scope, platform_tenant_id, platform_tenant_name, gateway_principal_id, gateway_principal_name, external_auth_integration_id, external_subject_reference, dedupe_key,
       metadata_json, first_seen_at, last_seen_at, acknowledged_at, acknowledged_by, resolved_at, resolved_by
FROM alert_events
WHERE id = $1
`, id)
	event, err := scanAlertEvent(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return AlertEvent{}, false, nil
		}
		return AlertEvent{}, false, err
	}
	return event, true, nil
}

func (r *PostgresRepository) FindAlertByDedupeKey(ctx context.Context, dedupeKey string) (AlertEvent, bool, error) {
	dedupeKey = strings.TrimSpace(dedupeKey)
	if dedupeKey == "" {
		return AlertEvent{}, false, nil
	}
	row := r.db.QueryRowContext(ctx, `
SELECT id, type, severity, status, title, summary, resource_type, resource_id, profile_scope, platform_tenant_id, platform_tenant_name, gateway_principal_id, gateway_principal_name, external_auth_integration_id, external_subject_reference, dedupe_key,
       metadata_json, first_seen_at, last_seen_at, acknowledged_at, acknowledged_by, resolved_at, resolved_by
FROM alert_events
WHERE dedupe_key = $1
`, dedupeKey)
	event, err := scanAlertEvent(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return AlertEvent{}, false, nil
		}
		return AlertEvent{}, false, err
	}
	return event, true, nil
}

func (r *PostgresRepository) SaveAlertEvent(ctx context.Context, event AlertEvent) error {
	metadata := marshalAlertMetadata(event.Metadata)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO alert_events(
  id, type, severity, status, title, summary, resource_type, resource_id, profile_scope, platform_tenant_id, platform_tenant_name, gateway_principal_id, gateway_principal_name, external_auth_integration_id, external_subject_reference, dedupe_key,
  metadata_json, first_seen_at, last_seen_at, acknowledged_at, acknowledged_by, resolved_at, resolved_by
)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17::jsonb,$18,$19,$20,$21,$22,$23)
ON CONFLICT (dedupe_key) DO UPDATE SET
  type = EXCLUDED.type,
  severity = EXCLUDED.severity,
  status = EXCLUDED.status,
  title = EXCLUDED.title,
  summary = EXCLUDED.summary,
  resource_type = EXCLUDED.resource_type,
  resource_id = EXCLUDED.resource_id,
  profile_scope = EXCLUDED.profile_scope,
  platform_tenant_id = EXCLUDED.platform_tenant_id,
  platform_tenant_name = EXCLUDED.platform_tenant_name,
  gateway_principal_id = EXCLUDED.gateway_principal_id,
  gateway_principal_name = EXCLUDED.gateway_principal_name,
	 external_auth_integration_id = EXCLUDED.external_auth_integration_id,
	 external_subject_reference = EXCLUDED.external_subject_reference,
  dedupe_key = EXCLUDED.dedupe_key,
  metadata_json = EXCLUDED.metadata_json,
  first_seen_at = EXCLUDED.first_seen_at,
  last_seen_at = EXCLUDED.last_seen_at,
  acknowledged_at = EXCLUDED.acknowledged_at,
  acknowledged_by = EXCLUDED.acknowledged_by,
  resolved_at = EXCLUDED.resolved_at,
  resolved_by = EXCLUDED.resolved_by
`, event.ID, event.Type, event.Severity, event.Status, event.Title, event.Summary, event.ResourceType, event.ResourceID, event.ProfileScope, event.PlatformTenantID, event.PlatformTenantName, event.GatewayPrincipalID, event.GatewayPrincipalName, event.ExternalAuthIntegrationID, event.ExternalSubjectReference, event.DedupeKey, metadata, event.FirstSeenAt, event.LastSeenAt, event.AcknowledgedAt, event.AcknowledgedBy, event.ResolvedAt, event.ResolvedBy)
	return err
}

type alertEventScanner interface {
	Scan(dest ...any) error
}

func scanAlertEvent(scanner alertEventScanner) (AlertEvent, error) {
	var event AlertEvent
	var metadataRaw []byte
	var acknowledgedAt sql.NullTime
	var resolvedAt sql.NullTime
	if err := scanner.Scan(
		&event.ID,
		&event.Type,
		&event.Severity,
		&event.Status,
		&event.Title,
		&event.Summary,
		&event.ResourceType,
		&event.ResourceID,
		&event.ProfileScope,
		&event.PlatformTenantID,
		&event.PlatformTenantName,
		&event.GatewayPrincipalID,
		&event.GatewayPrincipalName,
		&event.ExternalAuthIntegrationID,
		&event.ExternalSubjectReference,
		&event.DedupeKey,
		&metadataRaw,
		&event.FirstSeenAt,
		&event.LastSeenAt,
		&acknowledgedAt,
		&event.AcknowledgedBy,
		&resolvedAt,
		&event.ResolvedBy,
	); err != nil {
		return AlertEvent{}, err
	}
	event.Metadata = parseAlertMetadata(metadataRaw)
	if acknowledgedAt.Valid {
		event.AcknowledgedAt = &acknowledgedAt.Time
	}
	if resolvedAt.Valid {
		event.ResolvedAt = &resolvedAt.Time
	}
	return event, nil
}

func appendAlertEventFilters(clauses *[]string, args *[]any, query AlertQuery) {
	appendExactFilter(clauses, args, "type", query.Type)
	appendExactFilter(clauses, args, "severity", query.Severity)
	appendExactFilter(clauses, args, "status", query.Status)
	appendExactFilter(clauses, args, "resource_type", query.ResourceType)
	appendAnyExactFilter(clauses, args, "resource_id", query.ResourceIDs)
	appendExactFilter(clauses, args, "profile_scope", query.ProfileScope)
	appendExactFilter(clauses, args, "platform_tenant_id", query.PlatformTenantID)
	appendExactFilter(clauses, args, "gateway_principal_id", query.GatewayPrincipalID)
	appendExactFilter(clauses, args, "external_auth_integration_id", query.ExternalAuthIntegrationID)
	appendTimeFilter(clauses, args, "last_seen_at", ">=", query.CreatedFrom)
	appendTimeFilter(clauses, args, "last_seen_at", "<=", query.CreatedTo)
	appendSearchFilter(clauses, args, query.Search, []string{"type", "severity", "status", "title", "summary", "resource_type", "resource_id", "dedupe_key", "profile_scope", "platform_tenant_id", "platform_tenant_name", "gateway_principal_id", "gateway_principal_name", "external_auth_integration_id", "external_subject_reference"})
}

func memoryAlertEventMatches(event AlertEvent, query AlertQuery) bool {
	if query.Type != "" && event.Type != query.Type {
		return false
	}
	if query.Severity != "" && event.Severity != query.Severity {
		return false
	}
	if query.Status != "" && event.Status != query.Status {
		return false
	}
	if query.ResourceType != "" && event.ResourceType != query.ResourceType {
		return false
	}
	if len(query.ResourceIDs) > 0 && !contains(query.ResourceIDs, event.ResourceID) {
		return false
	}
	if query.ProfileScope != "" && event.ProfileScope != query.ProfileScope {
		return false
	}
	if query.PlatformTenantID != "" && event.PlatformTenantID != query.PlatformTenantID {
		return false
	}
	if query.GatewayPrincipalID != "" && event.GatewayPrincipalID != query.GatewayPrincipalID {
		return false
	}
	if query.ExternalAuthIntegrationID != "" && event.ExternalAuthIntegrationID != query.ExternalAuthIntegrationID {
		return false
	}
	if !query.CreatedFrom.IsZero() && event.LastSeenAt.Before(query.CreatedFrom) {
		return false
	}
	if !query.CreatedTo.IsZero() && event.LastSeenAt.After(query.CreatedTo) {
		return false
	}
	keyword := strings.ToLower(strings.TrimSpace(query.Search))
	if keyword == "" {
		return true
	}
	values := []string{event.Type, event.Severity, event.Status, event.Title, event.Summary, event.ResourceType, event.ResourceID, event.DedupeKey, event.ProfileScope, event.PlatformTenantID, event.PlatformTenantName, event.GatewayPrincipalID, event.GatewayPrincipalName, event.ExternalAuthIntegrationID, event.ExternalSubjectReference}
	for key, value := range event.Metadata {
		values = append(values, key, value)
	}
	return anyContains(keyword, values...)
}

func cloneAlertEvent(event AlertEvent) AlertEvent {
	if event.Metadata == nil {
		event.Metadata = map[string]string{}
		return event
	}
	metadata := make(map[string]string, len(event.Metadata))
	for key, value := range event.Metadata {
		metadata[key] = value
	}
	event.Metadata = metadata
	return event
}

func marshalAlertMetadata(metadata map[string]string) string {
	if metadata == nil {
		return "{}"
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func parseAlertMetadata(raw []byte) map[string]string {
	if len(raw) == 0 {
		return map[string]string{}
	}
	var metadata map[string]string
	if err := json.Unmarshal(raw, &metadata); err != nil || metadata == nil {
		return map[string]string{}
	}
	return metadata
}
