package saml

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/pkg/idgen"
)

// RefreshFailure 是一次元数据刷新失败，由后台任务汇总后写日志。
type RefreshFailure struct {
	ID   idgen.ID
	Name string
	Err  error
}

// RefreshMetadata 按保存的 URL 重新拉取元数据；只有 Entity ID 与已选中的一致时才替换。
// 返回是否真的改动了元数据内容（仅刷新时间不算）。
func (s *Service) RefreshMetadata(ctx context.Context, m *Source) (bool, error) {
	if strings.TrimSpace(m.IdPMetadataURL) == "" {
		return false, errors.New("saml: 该身份源没有配置元数据地址")
	}
	raw, err := fetchMetadata(ctx, m.IdPMetadataURL, m.MetadataCACert)
	if err != nil {
		return false, err
	}
	entityID, changed, err := planRefresh(m, string(raw))
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	var fresh Source
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", int64(m.ID)).Take(&fresh).Error; err != nil {
			return err
		}
		entityID, changed, err = planRefresh(&fresh, string(raw))
		if err != nil {
			return err
		}
		if err := tx.Model(&Source{}).Where("id = ?", int64(m.ID)).Updates(map[string]any{
			"idp_metadata":          string(raw),
			"idp_entity_id":         entityID,
			"metadata_refreshed_at": now,
		}).Error; err != nil {
			return err
		}
		fresh.IdPMetadata, fresh.IDPEntityID, fresh.MetadataRefreshedAt = string(raw), entityID, &now
		if changed {
			return s.saveVersionDB(ctx, tx, &fresh, "metadata refresh")
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	*m = fresh
	detail := "unchanged"
	if changed {
		detail = "changed"
	}
	s.recordEvent(ctx, m, "metadata_refresh", "success", "", "", detail)
	return changed, nil
}

// planRefresh 是刷新的判定逻辑：保留已选 Entity ID，远端内容必须仍可用，返回是否真的变化。
func planRefresh(m *Source, raw string) (entityID string, changed bool, err error) {
	info, err := parseIdP(raw, m.IDPEntityID)
	if err != nil {
		return "", false, err
	}
	if info.EntityID == "" {
		return "", false, fmt.Errorf("saml: 远程元数据包含 %d 个 IdP，请先选择 Entity ID", len(info.Candidates))
	}
	if info.SSOURL == "" || info.CertCount == 0 {
		return "", false, errors.New("saml: 远程元数据缺少 SSO 端点或签名证书，拒绝替换")
	}
	if info.CertExpired {
		return "", false, errors.New("saml: 远程元数据的 IdP 签名证书已过期，拒绝替换")
	}
	if info.MetadataExpired {
		return "", false, errors.New("saml: 远程元数据已超过 validUntil，拒绝替换")
	}
	current, err := parseIdP(m.IdPMetadata, m.IDPEntityID)
	if err != nil {
		return "", false, err
	}
	if endpointDowngrade(current.SSOURL, info.SSOURL) || endpointDowngrade(current.SLOURL, info.SLOURL) {
		return "", false, errors.New("saml: 远程元数据把 HTTPS 端点降级为 HTTP，拒绝替换")
	}
	if err := verifyMetadataSignature(raw, m.MetadataSignCert); err != nil {
		return "", false, err
	}
	return info.EntityID, strings.TrimSpace(raw) != strings.TrimSpace(m.IdPMetadata), nil
}

func endpointDowngrade(current, next string) bool {
	if current == "" || next == "" {
		return false
	}
	oldURL, oldErr := url.Parse(current)
	newURL, newErr := url.Parse(next)
	return oldErr == nil && newErr == nil && oldURL.Scheme == "https" && newURL.Scheme == "http"
}

// SweepMetadata 刷新所有配置了元数据地址的身份源（含未启用的，便于先同步再启用）。
// 单个源失败不中断整轮，失败明细返回给调用方写日志。
func (s *Service) SweepMetadata(ctx context.Context) (checked, updated int, failures []RefreshFailure, err error) {
	list, err := s.List(ctx)
	if err != nil {
		return 0, 0, nil, err
	}
	for i := range list {
		if strings.TrimSpace(list[i].IdPMetadataURL) == "" {
			continue
		}
		checked++
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		changed, rerr := s.RefreshMetadata(cctx, &list[i])
		cancel()
		if rerr != nil {
			failures = append(failures, RefreshFailure{ID: list[i].ID, Name: list[i].Name, Err: rerr})
			continue
		}
		if changed {
			updated++
		}
	}
	return checked, updated, failures, nil
}
