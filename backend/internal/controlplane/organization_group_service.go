package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Service) ListOrganizationGroups(ctx context.Context) ([]OrganizationGroup, error) {
	return s.repo.ListOrganizationGroups(ctx)
}

func (s *Service) SaveOrganizationGroup(ctx context.Context, actor, id string, req OrganizationGroupRequest) (OrganizationGroup, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return OrganizationGroup{}, errors.New("organization group name is required")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = WorkspaceUserStatusActive
	}
	if !oneOf(status, WorkspaceUserStatusActive, WorkspaceUserStatusDisabled) {
		return OrganizationGroup{}, errors.New("organization group status must be active or disabled")
	}
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return OrganizationGroup{}, err
	}
	userIDs := map[string]struct{}{}
	for _, user := range users {
		userIDs[user.ID] = struct{}{}
	}
	members := make([]string, 0, len(req.MemberIDs))
	seen := map[string]struct{}{}
	for _, userID := range req.MemberIDs {
		userID = strings.TrimSpace(userID)
		if _, exists := userIDs[userID]; !exists {
			return OrganizationGroup{}, fmt.Errorf("workspace user %s does not exist", userID)
		}
		if _, duplicate := seen[userID]; duplicate {
			continue
		}
		seen[userID] = struct{}{}
		members = append(members, userID)
	}
	groups, err := s.repo.ListOrganizationGroups(ctx)
	if err != nil {
		return OrganizationGroup{}, err
	}
	now := time.Now().UTC()
	createdAt := now
	if id == "" {
		id = "orggrp_" + randomID(10)
	} else {
		found := false
		for _, existing := range groups {
			if existing.ID == id {
				createdAt, found = existing.CreatedAt, true
				break
			}
		}
		if !found {
			return OrganizationGroup{}, errors.New("organization group not found")
		}
	}
	for _, existing := range groups {
		if existing.ID != id && strings.EqualFold(existing.Name, name) {
			return OrganizationGroup{}, errors.New("organization group name already exists")
		}
	}
	group := OrganizationGroup{ID: id, Name: name, Description: strings.TrimSpace(req.Description), Status: status, MemberIDs: members, CreatedAt: createdAt, UpdatedAt: now}
	if err := s.repo.SaveOrganizationGroup(ctx, group); err != nil {
		return OrganizationGroup{}, err
	}
	if err := s.audit(ctx, actor, "save", "organization_group", group.ID, fmt.Sprintf("Saved organization group %s with %d members", group.Name, len(group.MemberIDs))); err != nil {
		return OrganizationGroup{}, err
	}
	return group, nil
}

func (s *Service) DeleteOrganizationGroup(ctx context.Context, actor, id string) error {
	groups, err := s.repo.ListOrganizationGroups(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, group := range groups {
		found = found || group.ID == id
	}
	if !found {
		return errors.New("organization group not found")
	}
	if err := s.repo.DeleteOrganizationGroup(ctx, id); err != nil {
		return err
	}
	return s.audit(ctx, actor, "delete", "organization_group", id, "Deleted organization group")
}
