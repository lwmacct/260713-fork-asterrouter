package controlplane

import (
	"context"
	"errors"
	"sort"
	"strings"
)

func (r *MemoryRepository) ListOrganizationGroups(context.Context) ([]OrganizationGroup, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]OrganizationGroup, 0, len(r.organizationGroups))
	for _, group := range r.organizationGroups {
		group.MemberIDs = append([]string(nil), group.MemberIDs...)
		out = append(out, group)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *MemoryRepository) SaveOrganizationGroup(_ context.Context, group OrganizationGroup) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, existing := range r.organizationGroups {
		if id != group.ID && strings.EqualFold(strings.TrimSpace(existing.Name), strings.TrimSpace(group.Name)) {
			return errors.New("organization group name already exists")
		}
		for _, existingUserID := range existing.MemberIDs {
			if id != group.ID && contains(group.MemberIDs, existingUserID) {
				return errors.New("workspace user already belongs to another organization group")
			}
		}
	}
	group.MemberIDs = append([]string(nil), group.MemberIDs...)
	r.organizationGroups[group.ID] = group
	return nil
}

func (r *MemoryRepository) DeleteOrganizationGroup(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.organizationGroups, id)
	return nil
}

func (r *PostgresRepository) ListOrganizationGroups(ctx context.Context) ([]OrganizationGroup, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT g.id,g.name,g.description,g.status,g.created_at,g.updated_at,COALESCE(m.user_id,'')
FROM organization_groups g
LEFT JOIN organization_group_members m ON m.group_id=g.id
ORDER BY lower(g.name),m.created_at,m.user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]*OrganizationGroup{}
	order := []string{}
	for rows.Next() {
		var group OrganizationGroup
		var userID string
		if err := rows.Scan(&group.ID, &group.Name, &group.Description, &group.Status, &group.CreatedAt, &group.UpdatedAt, &userID); err != nil {
			return nil, err
		}
		current := byID[group.ID]
		if current == nil {
			group.MemberIDs = []string{}
			byID[group.ID] = &group
			order = append(order, group.ID)
			current = &group
		}
		if userID != "" {
			current.MemberIDs = append(current.MemberIDs, userID)
		}
	}
	out := make([]OrganizationGroup, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveOrganizationGroup(ctx context.Context, group OrganizationGroup) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO organization_groups(id,name,description,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,status=EXCLUDED.status,updated_at=EXCLUDED.updated_at`, group.ID, group.Name, group.Description, group.Status, group.CreatedAt, group.UpdatedAt)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM organization_group_members WHERE group_id=$1`, group.ID); err != nil {
		return err
	}
	for _, userID := range group.MemberIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO organization_group_members(group_id,user_id,created_at) VALUES($1,$2,$3)`, group.ID, userID, group.UpdatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *PostgresRepository) DeleteOrganizationGroup(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM organization_groups WHERE id=$1`, id)
	return err
}
