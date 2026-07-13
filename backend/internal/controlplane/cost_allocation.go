package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	CostAllocationByAPIKey     = "api_key"
	CostAllocationByModel      = "model"
	CostAllocationByUser       = "user"
	CostAllocationByDepartment = "department"
	CostAllocationByGroup      = "group"
)

var ErrInvalidCostAllocationDimension = errors.New("invalid cost allocation dimension")

func (s *Service) CostAllocationReportQuery(ctx context.Context, dimension string, query UsageQuery) (CostAllocationReport, error) {
	dimension, err := normalizeCostAllocationDimension(dimension)
	if err != nil {
		return CostAllocationReport{}, err
	}
	rollups, err := s.repo.SummarizeCostAllocation(ctx, dimension, query)
	if err != nil {
		return CostAllocationReport{}, err
	}
	aggregate, err := s.repo.SummarizeUsageRecords(ctx, query)
	if err != nil {
		return CostAllocationReport{}, err
	}
	keys, err := s.repo.ListAPIKeys(ctx)
	if err != nil {
		return CostAllocationReport{}, err
	}
	keyByID := make(map[string]APIKeyRecord, len(keys))
	for _, key := range keys {
		keyByID[key.ID] = key
	}
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return CostAllocationReport{}, err
	}
	userNames := map[string]string{}
	for _, user := range users {
		userNames[user.ID] = firstNonEmpty(user.DisplayName, user.Email, user.ID)
	}
	departments, err := s.repo.ListDepartments(ctx)
	if err != nil {
		return CostAllocationReport{}, err
	}
	departmentNames := map[string]string{}
	for _, department := range departments {
		departmentNames[department.ID] = department.Name
	}
	groups, err := s.repo.ListOrganizationGroups(ctx)
	if err != nil {
		return CostAllocationReport{}, err
	}
	groupNames := map[string]string{}
	for _, group := range groups {
		groupNames[group.ID] = group.Name
	}

	rows := make([]CostAllocationRow, 0, len(rollups))
	for _, rollup := range rollups {
		rows = append(rows, costAllocationRow(dimension, rollup, aggregate.TotalCostCents, keyByID, userNames, departmentNames, groupNames))
	}
	return CostAllocationReport{
		Dimension:      dimension,
		TotalRequests:  aggregate.TotalRequests,
		ErrorRequests:  aggregate.ErrorRequests,
		TotalTokens:    aggregate.TotalTokens,
		TotalCostCents: aggregate.TotalCostCents,
		AvgLatencyMS:   aggregate.AvgLatencyMS,
		Rows:           rows,
	}, nil
}

func normalizeCostAllocationDimension(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return CostAllocationByAPIKey, nil
	}
	if !oneOf(value, CostAllocationByAPIKey, CostAllocationByModel, CostAllocationByUser, CostAllocationByDepartment, CostAllocationByGroup) {
		return "", ErrInvalidCostAllocationDimension
	}
	return value, nil
}

func costAllocationRow(dimension string, rollup CostAllocationRollup, totalCostCents int, apiKeys map[string]APIKeyRecord, userNames, departmentNames, groupNames map[string]string) CostAllocationRow {
	row := CostAllocationRow{
		Dimension:      dimension,
		APIKeyID:       rollup.APIKeyID,
		APIFingerprint: rollup.APIFingerprint,
		Model:          rollup.Model,
		Requests:       rollup.Requests,
		ErrorRequests:  rollup.ErrorRequests,
		TotalTokens:    rollup.TotalTokens,
		TotalCostCents: rollup.TotalCostCents,
		AvgLatencyMS:   rollup.AvgLatencyMS,
	}
	row.ResourceID = firstNonEmpty(rollup.ResourceID, "unassigned")
	if dimension == CostAllocationByUser {
		row.ResourceName = firstNonEmpty(userNames[rollup.ResourceID], rollup.ResourceID, "Unassigned user")
	} else if dimension == CostAllocationByDepartment {
		row.ResourceName = firstNonEmpty(departmentNames[rollup.ResourceID], rollup.ResourceID, "Unassigned department")
	} else if dimension == CostAllocationByGroup {
		row.ResourceName = firstNonEmpty(groupNames[rollup.ResourceID], rollup.ResourceID, "Unassigned group")
	}
	if key, ok := apiKeys[row.APIKeyID]; ok {
		row.APIKeyName = key.Name
		if row.APIFingerprint == "" {
			row.APIFingerprint = key.Fingerprint
		}
	}
	if totalCostCents > 0 {
		row.CostSharePercent = percent(row.TotalCostCents, totalCostCents)
	}
	if row.ResourceName == "" {
		row.ResourceID, row.ResourceName = costAllocationResource(dimension, row)
	}
	return row
}

func costAllocationResource(dimension string, row CostAllocationRow) (string, string) {
	if dimension == CostAllocationByAPIKey {
		return firstNonEmpty(row.APIKeyID, "unassigned"), firstNonEmpty(row.APIKeyName, row.APIFingerprint, row.APIKeyID, "Unassigned workspace key")
	}
	return firstNonEmpty(row.Model, "unknown_model"), firstNonEmpty(row.Model, "Unknown model")
}

func percent(part int, total int) float64 {
	return float64(part) * 100 / float64(total)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (r *MemoryRepository) SummarizeCostAllocation(_ context.Context, dimension string, query UsageQuery) ([]CostAllocationRollup, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := map[string]*CostAllocationRollup{}
	for _, record := range r.usageRecords {
		if !memoryUsageRecordMatches(record, query) {
			continue
		}
		key := record.APIKeyID
		resourceID := ""
		if dimension == CostAllocationByModel {
			key = record.Model
		} else if dimension == CostAllocationByUser || dimension == CostAllocationByDepartment || dimension == CostAllocationByGroup {
			if apiKey, ok := r.apiKeys[record.APIKeyID]; ok {
				resourceID = apiKey.OwnerUserID
				if dimension == CostAllocationByDepartment {
					if user, ok := r.workspaceUsers[apiKey.OwnerUserID]; ok {
						resourceID = user.DepartmentID
					}
				} else if dimension == CostAllocationByGroup {
					resourceID = ""
					for groupID, group := range r.organizationGroups {
						if contains(group.MemberIDs, apiKey.OwnerUserID) {
							resourceID = groupID
							break
						}
					}
				}
			}
			key = firstNonEmpty(resourceID, "unassigned")
		}
		rollup := values[key]
		if rollup == nil {
			rollup = &CostAllocationRollup{
				APIKeyID:       record.APIKeyID,
				APIFingerprint: record.APIFingerprint,
				Model:          record.Model,
				ResourceID:     resourceID,
			}
			if dimension == CostAllocationByModel {
				rollup.APIKeyID = ""
				rollup.APIFingerprint = ""
			} else if dimension == CostAllocationByUser || dimension == CostAllocationByDepartment || dimension == CostAllocationByGroup {
				rollup.APIKeyID = ""
				rollup.APIFingerprint = ""
				rollup.Model = ""
			}
			values[key] = rollup
		}
		rollup.Requests++
		if record.Status == "upstream_error" || record.Status == "error" || record.ErrorType != "" {
			rollup.ErrorRequests++
		}
		rollup.TotalTokens += record.InputTokens + record.OutputTokens
		rollup.TotalCostCents += record.CostCents
		rollup.LatencyTotal += record.LatencyMS
	}
	out := make([]CostAllocationRollup, 0, len(values))
	for _, rollup := range values {
		if rollup.Requests > 0 {
			rollup.AvgLatencyMS = rollup.LatencyTotal / int64(rollup.Requests)
		}
		out = append(out, *rollup)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalCostCents == out[j].TotalCostCents {
			return out[i].Requests > out[j].Requests
		}
		return out[i].TotalCostCents > out[j].TotalCostCents
	})
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 100, 500)
	if offset >= len(out) {
		return []CostAllocationRollup{}, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (r *PostgresRepository) SummarizeCostAllocation(ctx context.Context, dimension string, query UsageQuery) ([]CostAllocationRollup, error) {
	selectFields, joins, groupBy, err := costAllocationSQLGrouping(dimension)
	if err != nil {
		return nil, err
	}
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 100, 500)
	clauses := []string{}
	args := []any{}
	appendUsageRecordFilters(&clauses, &args, query)
	sqlText := fmt.Sprintf(`
SELECT %s,
       COUNT(*),
       COALESCE(SUM(CASE WHEN status IN ('upstream_error', 'error') OR error_type <> '' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(input_tokens + output_tokens), 0),
       COALESCE(SUM(cost_cents), 0),
       COALESCE(SUM(latency_ms), 0)
FROM usage_records ur %s`, selectFields, joins)
	if len(clauses) > 0 {
		sqlText += " WHERE " + strings.Join(clauses, " AND ")
	}
	sqlText += " GROUP BY " + groupBy
	args = append(args, limit, offset)
	sqlText += fmt.Sprintf(" ORDER BY COALESCE(SUM(cost_cents), 0) DESC, COUNT(*) DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CostAllocationRollup{}
	for rows.Next() {
		var rollup CostAllocationRollup
		var requests, errorsCount, tokens, costCents, latencyTotal int64
		if err := rows.Scan(&rollup.APIKeyID, &rollup.APIFingerprint, &rollup.Model, &rollup.ResourceID, &requests, &errorsCount, &tokens, &costCents, &latencyTotal); err != nil {
			return nil, err
		}
		rollup.Requests = int(requests)
		rollup.ErrorRequests = int(errorsCount)
		rollup.TotalTokens = int(tokens)
		rollup.TotalCostCents = int(costCents)
		rollup.LatencyTotal = latencyTotal
		if requests > 0 {
			rollup.AvgLatencyMS = latencyTotal / requests
		}
		out = append(out, rollup)
	}
	return out, rows.Err()
}

func costAllocationSQLGrouping(dimension string) (string, string, string, error) {
	switch dimension {
	case CostAllocationByAPIKey:
		return "ur.api_key_id, MAX(ur.api_fingerprint) AS api_fingerprint, '' AS model, '' AS resource_id", "", "ur.api_key_id", nil
	case CostAllocationByModel:
		return "'' AS api_key_id, '' AS api_fingerprint, ur.model, '' AS resource_id", "", "ur.model", nil
	case CostAllocationByUser:
		return "'' AS api_key_id, '' AS api_fingerprint, '' AS model, COALESCE(k.owner_user_id, '') AS resource_id", "LEFT JOIN (SELECT id, owner_user_id FROM api_keys) k ON k.id = ur.api_key_id", "COALESCE(k.owner_user_id, '')", nil
	case CostAllocationByDepartment:
		return "'' AS api_key_id, '' AS api_fingerprint, '' AS model, COALESCE(u.department_id, '') AS resource_id", "LEFT JOIN (SELECT id, owner_user_id FROM api_keys) k ON k.id = ur.api_key_id LEFT JOIN (SELECT id, department_id FROM workspace_users) u ON u.id = k.owner_user_id", "COALESCE(u.department_id, '')", nil
	case CostAllocationByGroup:
		return "'' AS api_key_id, '' AS api_fingerprint, '' AS model, COALESCE(gm.group_id, '') AS resource_id", "LEFT JOIN (SELECT id, owner_user_id FROM api_keys) k ON k.id = ur.api_key_id LEFT JOIN organization_group_members gm ON gm.user_id = k.owner_user_id", "COALESCE(gm.group_id, '')", nil
	default:
		return "", "", "", ErrInvalidCostAllocationDimension
	}
}
