package domain

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
	"uuid"
)

// Authorize 检查当前员工能否执行声明了 permission 的操作。超级管理员直接放行；
// 普通权限码每次查库，收回角色立即生效。
func (s *Service) Authorize(ctx context.Context, p *Principal, permission string) error {
	switch {
	case permission == PermissionAuthenticated:
		return nil
	case p.SuperAdmin:
		return nil
	case !s.isKnownCode(permission):
		// 保留值以外、admin.yaml 里也没有的权限码（包括已从 spec 删掉的）一律拒绝。
		return ErrPermissionDenied
	}
	ok, err := s.roles.HasPermission(ctx, p.StaffID, permission)
	if err != nil {
		return fmt.Errorf("authorize: %w", err)
	}
	if !ok {
		return ErrPermissionDenied
	}
	return nil
}

// Catalog 返回 admin.yaml 里全部可授予的权限码，按 tag、权限码排序。
func (s *Service) Catalog() []PermissionInfo {
	out := slices.Clone(s.codes)
	for i := range out {
		out[i].Operations = slices.Clone(out[i].Operations)
	}
	return out
}

func (s *Service) isKnownCode(code string) bool {
	return slices.ContainsFunc(s.codes, func(p PermissionInfo) bool { return p.Code == code })
}

// Permissions 返回当前员工的有效权限码：超级管理员是全部权限码；其余员工是角色权限码的
// 并集，spec 里已经删掉的权限码不返回。
func (s *Service) Permissions(ctx context.Context, p *Principal) ([]string, error) {
	if p.SuperAdmin {
		out := make([]string, 0, len(s.codes))
		for _, c := range s.codes {
			out = append(out, c.Code)
		}
		slices.Sort(out)
		return out, nil
	}
	perms, err := s.roles.Permissions(ctx, p.StaffID)
	if err != nil {
		return nil, fmt.Errorf("permissions: %w", err)
	}
	return slices.DeleteFunc(perms, func(c string) bool { return !s.isKnownCode(c) }), nil
}

func (s *Service) ListRoles(ctx context.Context) ([]*Role, error) {
	return s.roles.ListRoles(ctx)
}

func (s *Service) GetRole(ctx context.Context, id string) (*Role, error) {
	return s.roles.GetRole(ctx, id)
}

func validRoleName(name string) bool {
	return name != "" && utf8.RuneCountInString(name) <= 100
}

func (s *Service) CreateRole(ctx context.Context, name, description string) (*Role, error) {
	r := &Role{ID: uuid.NewV7().String(), Name: strings.TrimSpace(name), Description: strings.TrimSpace(description), Permissions: []string{}}
	if !validRoleName(r.Name) {
		return nil, ErrInvalidInput
	}
	if err := s.roles.CreateRole(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Service) UpdateRole(ctx context.Context, id, name, description string) (*Role, error) {
	r := &Role{ID: id, Name: strings.TrimSpace(name), Description: strings.TrimSpace(description)}
	if !validRoleName(r.Name) {
		return nil, ErrInvalidInput
	}
	if err := s.roles.UpdateRole(ctx, r); err != nil {
		return nil, err
	}
	return s.roles.GetRole(ctx, id)
}

func (s *Service) DeleteRole(ctx context.Context, id string) error {
	return s.uow.Run(ctx, func(ctx context.Context) error { return s.roles.DeleteRole(ctx, id) })
}

// SetRolePermissions 用 permissions 替换角色的权限码，只接受 admin.yaml 里出现过的权限码。
func (s *Service) SetRolePermissions(ctx context.Context, id string, permissions []string) (*Role, error) {
	for _, p := range permissions {
		if !s.isKnownCode(p) {
			return nil, ErrUnknownPermission
		}
	}
	err := s.uow.Run(ctx, func(ctx context.Context) error {
		if _, err := s.roles.GetRole(ctx, id); err != nil {
			return err
		}
		return s.roles.SetRolePermissions(ctx, id, permissions)
	})
	if err != nil {
		return nil, err
	}
	return s.roles.GetRole(ctx, id)
}

// BindRoles 用 roleIDs 替换员工的角色；有不存在的角色时整体不生效。
func (s *Service) BindRoles(ctx context.Context, staffID string, roleIDs []string) error {
	unique := slices.Compact(slices.Sorted(slices.Values(roleIDs)))
	return s.uow.Run(ctx, func(ctx context.Context) error {
		if _, err := s.accounts.GetAccount(ctx, staffID); err != nil {
			return err
		}
		n, err := s.roles.SetAccountRoles(ctx, staffID, unique)
		if err != nil {
			return err
		}
		if n != len(unique) {
			return ErrRoleNotFound
		}
		return nil
	})
}
