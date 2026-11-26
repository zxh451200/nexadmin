package saml

import (
	"context"
	"errors"
	"slices"
	"strings"

	"gorm.io/gorm"

	"nexadmin/pkg/idgen"
)

type RolePreview struct {
	Allowed                  bool     `json:"allowed"`
	UserScoped               bool     `json:"userScoped"`
	SyncRolesOnLogin         bool     `json:"syncRolesOnLogin"`
	RevokeRolesOnEmptyGroups bool     `json:"revokeRolesOnEmptyGroups"`
	MatchedGroups            []string `json:"matchedGroups"`
	DesiredRoles             []string `json:"desiredRoles"`
	PreviousRoles            []string `json:"previousRoles"`
	WouldAdd                 []string `json:"wouldAdd"`
	WouldRemove              []string `json:"wouldRemove"`
	UnavailableRoles         []string `json:"unavailableRoles"`
	Reason                   string   `json:"reason,omitempty"`
}

func (s *Service) PreviewRoles(ctx context.Context, m *Source, groups []string, userID string) (*RolePreview, error) {
	if len(groups) > 1000 {
		return nil, errors.New("saml: 预览组数量不能超过 1000")
	}
	clean := make([]string, 0, len(groups))
	for _, group := range groups {
		if len(group) > 2048 || strings.ContainsFunc(group, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return nil, errors.New("saml: 预览组值不合法")
		}
		clean = append(clean, group)
	}
	out := &RolePreview{
		Allowed: true, SyncRolesOnLogin: m.SyncRolesOnLogin,
		RevokeRolesOnEmptyGroups: m.RevokeRolesOnEmptyGroups,
		MatchedGroups:            []string{}, DesiredRoles: []string{}, PreviousRoles: []string{},
		WouldAdd: []string{}, WouldRemove: []string{}, UnavailableRoles: []string{},
	}
	seen := map[string]bool{}
	for _, mapping := range m.GroupRoleMappings {
		for _, group := range clean {
			if group == mapping.ExternalGroup && !seen[group] {
				seen[group] = true
				out.MatchedGroups = append(out.MatchedGroups, group)
			}
		}
	}
	desired, err := mappedRoleCodes(clean, m.GroupRoleMappings, m.RequireMappedGroup)
	if err != nil {
		out.Allowed, out.Reason = false, err.Error()
		return out, nil
	}
	out.DesiredRoles = desired

	if userID != "" {
		id, err := idgen.Parse(userID)
		if err != nil || id == 0 {
			return nil, errors.New("saml: 用户不存在或不在当前租户")
		}
		var user struct{ ID int64 }
		if err := s.db.WithContext(ctx).Table("sys_user").Select("id").
			Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", int64(m.TenantID), int64(id)).Take(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, errors.New("saml: 用户不存在或不在当前租户")
			}
			return nil, err
		}
		out.UserScoped = true
		if out.SyncRolesOnLogin {
			plan, err := planRoleChanges(s.db.WithContext(ctx), int64(m.TenantID), int64(id), int64(m.ID), desired, m.RevokeRolesOnEmptyGroups)
			if err != nil {
				return nil, err
			}
			out.PreviousRoles = plan.Previous
			out.UnavailableRoles = append(out.UnavailableRoles, plan.Unavailable...)
			for _, change := range plan.Add {
				if change.Effective {
					out.WouldAdd = append(out.WouldAdd, change.Code)
				}
			}
			for _, change := range plan.Remove {
				if change.Effective {
					out.WouldRemove = append(out.WouldRemove, change.Code)
				}
			}
		} else {
			var grants []roleGrant
			if err := s.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND source_id = ?",
				int64(m.TenantID), int64(id), int64(m.ID)).Find(&grants).Error; err != nil {
				return nil, err
			}
			codes, err := currentRoleCodes(s.db.WithContext(ctx), int64(m.TenantID), grants)
			if err != nil {
				return nil, err
			}
			for _, grant := range grants {
				code := grant.RoleCode
				if current := codes[grant.RoleID]; current != "" {
					code = current
				}
				out.PreviousRoles = append(out.PreviousRoles, code)
			}
		}
	}
	if len(desired) > 0 {
		var roles []struct{ Code string }
		if err := s.db.WithContext(ctx).Table("sys_role").Select("code").
			Where("tenant_id = ? AND code IN ? AND status = ? AND builtin = ? AND deleted_at IS NULL",
				int64(m.TenantID), desired, "1", false).Find(&roles).Error; err != nil {
			return nil, err
		}
		available := map[string]bool{}
		for _, role := range roles {
			available[role.Code] = true
		}
		for _, code := range desired {
			if !available[code] && !slices.Contains(out.UnavailableRoles, code) {
				out.UnavailableRoles = append(out.UnavailableRoles, code)
			}
		}
	}
	return out, nil
}
