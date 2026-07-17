package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Service) ListDepartments(ctx context.Context) ([]Department, error) {
	return s.repo.ListDepartments(ctx)
}

func (s *Service) CreateDepartment(ctx context.Context, actor string, req DepartmentRequest) (Department, error) {
	now := time.Now().UTC()
	department, err := departmentFromRequest(req, now)
	if err != nil {
		return Department{}, err
	}
	if err := s.validateDepartmentParent(ctx, "", department.ParentID); err != nil {
		return Department{}, err
	}
	if err := s.ensureUniqueDepartmentCode(ctx, "", department.Code); err != nil {
		return Department{}, err
	}
	department.ID = "dept_" + randomID(10)
	if err := s.repo.SaveDepartment(ctx, department); err != nil {
		return Department{}, err
	}
	if err := s.audit(ctx, actor, "create", "department", department.ID, fmt.Sprintf("Created department %s", department.Code)); err != nil {
		return Department{}, err
	}
	return department, nil
}

func (s *Service) UpdateDepartment(ctx context.Context, actor string, id string, req DepartmentRequest) (Department, error) {
	existing, err := s.departmentByID(ctx, id)
	if err != nil {
		return Department{}, err
	}
	department, err := departmentFromRequest(req, existing.CreatedAt)
	if err != nil {
		return Department{}, err
	}
	department.ID = existing.ID
	department.CreatedAt = existing.CreatedAt
	department.UpdatedAt = time.Now().UTC()
	if err := s.validateDepartmentParent(ctx, department.ID, department.ParentID); err != nil {
		return Department{}, err
	}
	if err := s.ensureUniqueDepartmentCode(ctx, department.ID, department.Code); err != nil {
		return Department{}, err
	}
	if err := s.repo.SaveDepartment(ctx, department); err != nil {
		return Department{}, err
	}
	if err := s.audit(ctx, actor, "update", "department", department.ID, fmt.Sprintf("Updated department %s", department.Code)); err != nil {
		return Department{}, err
	}
	return department, nil
}

func departmentFromRequest(req DepartmentRequest, createdAt time.Time) (Department, error) {
	now := time.Now().UTC()
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return Department{}, errors.New("name is required")
	}
	code := strings.ToUpper(strings.TrimSpace(req.Code))
	if code == "" {
		return Department{}, errors.New("code is required")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = DepartmentStatusActive
	}
	if !oneOf(status, DepartmentStatusActive, DepartmentStatusArchived) {
		return Department{}, errors.New("status must be active or archived")
	}
	if req.MonthlyBudgetMicros < 0 {
		return Department{}, errors.New("monthly_budget_micros must be greater than or equal to 0")
	}
	if createdAt.IsZero() {
		createdAt = now
	}
	costCenter := strings.ToUpper(strings.TrimSpace(req.CostCenter))
	if costCenter == "" {
		costCenter = code
	}
	return Department{
		Name:                name,
		Code:                code,
		ParentID:            strings.TrimSpace(req.ParentID),
		CostCenter:          costCenter,
		MonthlyBudgetMicros: req.MonthlyBudgetMicros,
		Status:              status,
		CreatedAt:           createdAt,
		UpdatedAt:           now,
	}, nil
}

func (s *Service) departmentByID(ctx context.Context, id string) (Department, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Department{}, errors.New("department id is required")
	}
	departments, err := s.repo.ListDepartments(ctx)
	if err != nil {
		return Department{}, err
	}
	for _, department := range departments {
		if department.ID == id {
			return department, nil
		}
	}
	return Department{}, fmt.Errorf("department %s not found", id)
}

func (s *Service) validateDepartmentParent(ctx context.Context, currentID string, parentID string) error {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return nil
	}
	if currentID != "" && parentID == currentID {
		return errors.New("department cannot use itself as parent")
	}
	departments, err := s.repo.ListDepartments(ctx)
	if err != nil {
		return err
	}
	departmentByID := make(map[string]Department, len(departments))
	for _, department := range departments {
		departmentByID[department.ID] = department
	}
	if _, ok := departmentByID[parentID]; !ok {
		return fmt.Errorf("parent department %s not found", parentID)
	}
	for cursor := parentID; cursor != ""; {
		parent := departmentByID[cursor]
		if currentID != "" && parent.ParentID == currentID {
			return errors.New("department parent would create a cycle")
		}
		cursor = parent.ParentID
	}
	return nil
}

func (s *Service) ensureUniqueDepartmentCode(ctx context.Context, currentID string, code string) error {
	departments, err := s.repo.ListDepartments(ctx)
	if err != nil {
		return err
	}
	for _, department := range departments {
		if department.Code == code && department.ID != currentID {
			return fmt.Errorf("department code %s already exists", code)
		}
	}
	return nil
}
