package identity

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/auth"
	"nexadmin/core/job"
	"nexadmin/core/plugin"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

const directoryJobType = "identity.directory-sync"

// maxDirectoryEntries 是一次同步最多处理的目录条目数；用 RFC 2696 分页枚举，超过即整体失败（不做部分同步）。
const maxDirectoryEntries = 5000

// pagedSearcher 是支持分页枚举的会话（真实实现 *ldapConn）；测试替身可以不实现，退回单次 Search。
type pagedSearcher interface {
	SearchPaged(base string, scope int, filter string, attrs []string, max int) ([]ldapEntry, error)
}

// directoryDeactivator 由用户模块实现（modules/system/user/ext_login.go），避免插件 import 内置模块。
type directoryDeactivator interface {
	DeactivateMissingExternal(ctx context.Context, provider string, keep map[string]bool) (int, error)
}

// One latest-run pointer per source; the source row serializes submit and execution.
type directoryRun struct {
	SourceID idgen.ID `gorm:"primaryKey;column:source_id"`
	TenantID idgen.ID `gorm:"column:tenant_id"`
	JobID    idgen.ID `gorm:"column:job_id"`
	Scanned  int      `gorm:"column:scanned"`
	Created  int      `gorm:"column:created"`
	Updated  int      `gorm:"column:updated"`
	Complete bool     `gorm:"column:complete"`
	// Deactivated 是本次同步因“已不在目录里”而停用的账号数（仅 deactivateMissing 开启时非零）。
	Deactivated int `gorm:"column:deactivated"`
}

func (directoryRun) TableName() string { return "plg_identity_directory_run" }

type DirectorySyncStatus struct {
	JobID       string     `json:"jobId"`
	Status      string     `json:"status"`
	Scanned     int        `json:"scanned"`
	Created     int        `json:"created"`
	Updated     int        `json:"updated"`
	Deactivated int        `json:"deactivated"`
	Complete    bool       `json:"complete"`
	Error       string     `json:"error"`
	StartedAt   *time.Time `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
}

func directoryProvider(id idgen.ID) string { return "ldap-" + id.String() }

// userProvider 是非目录作用域（以登录名为主体）的绑定键：ldapu-<source id> / cas-<source id>。
// 用户模块（modules/system/user/ext_login.go）按同样的前缀校验并识别升级前的旧键 "ldap" / "cas"。
func userProvider(id idgen.ID, typ string) string {
	if typ == TypeCAS {
		return "cas-" + id.String()
	}
	return "ldapu-" + id.String()
}

var directoryUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func directorySubject(e ldapEntry, attr string) (string, error) {
	values := e.Attrs[strings.ToLower(attr)]
	if len(values) != 1 {
		return "", errors.New("identity: stable subject must be single valued")
	}
	switch attr {
	case "entryUUID":
		if directoryUUID.MatchString(values[0]) {
			return "uuid:" + strings.ToLower(values[0]), nil
		}
	case "objectGUID":
		if len(values[0]) == 16 {
			// Exact AD octets, deliberately no platform-dependent GUID byte swapping.
			return "guid:" + hex.EncodeToString([]byte(values[0])), nil
		}
	}
	return "", errors.New("identity: missing or invalid stable directory subject")
}

func directoryLicense() error {
	for _, p := range plugin.Default().List() {
		if p.ID == "identity" && p.Status == plugin.StatusActive {
			return nil
		}
	}
	return response.Forbidden("auth.login_denied").WithMsg("identity Pro license is not active")
}

func (s *Service) directoryDB() (*gorm.DB, error) {
	g, ok := s.st.(gormStore)
	if !ok || g.db == nil {
		return nil, errors.New("identity: persisted directory runner unavailable")
	}
	return g.db, nil
}

func directoryConfig(src *Source) (LDAPConfig, error) {
	c, err := src.ldapConfig()
	if err != nil || !src.Enabled || !c.scopedDirectory() {
		return c, badReq("sync requires an enabled LDAP source with directorySync.enabled")
	}
	return c, c.validateDirectory()
}

func lockDirectory(ctx context.Context, tx *gorm.DB, tid, id idgen.ID) (*Source, error) {
	var src Source
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND id = ?", tid, id).First(&src).Error
	return &src, err
}

func (h *handler) syncNow(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	start := time.Now()
	claims := auth.ClaimsFrom(c)
	if claims == nil || claims.UserID <= 0 {
		response.Fail(c, response.Forbidden("auth.login_denied"))
		return
	}
	jid, err := h.svc.submitDirectory(c.Request.Context(), id, claims.UserID)
	h.audit(c, "sync", start, map[string]any{"id": id.String()}, err)
	respond(c, gin.H{"jobId": jid.String()}, err)
}

func (h *handler) syncStatus(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	v, err := h.svc.directoryStatus(c.Request.Context(), id)
	respond(c, v, err)
}

func (s *Service) submitDirectory(ctx context.Context, id, owner idgen.ID) (idgen.ID, error) {
	if err := directoryLicense(); err != nil {
		return 0, err
	}
	m := job.Default()
	if m == nil || !m.Ready() || s.auth == nil {
		return 0, badReq("directory sync runner unavailable")
	}
	src, err := s.load(ctx, id)
	if err != nil {
		return 0, err
	}
	if _, err := directoryConfig(src); err != nil {
		return 0, err
	}
	db, err := s.directoryDB()
	if err != nil {
		return 0, err
	}
	var jid idgen.ID
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		src, err := lockDirectory(ctx, tx, src.TenantID, id)
		if err != nil {
			return err
		}
		if _, err := directoryConfig(src); err != nil {
			return err
		}
		var old directoryRun
		err = tx.Where("tenant_id = ? AND source_id = ?", src.TenantID, id).First(&old).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if old.JobID != 0 {
			j, err := m.Get(ctx, job.Actor{Admin: true}, old.JobID)
			if err != nil && !errors.Is(err, job.ErrNotFound) {
				return err
			}
			if j != nil && !job.IsFinal(j.Status) {
				jid = j.ID
				return nil
			}
		}
		j, err := m.Submit(ctx, job.Submit{Type: directoryJobType, Owner: owner, Params: map[string]string{"sourceId": id.String()}})
		if err != nil {
			return err
		}
		jid = j.ID
		run := directoryRun{SourceID: id, TenantID: src.TenantID, JobID: jid}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, UpdateAll: true}).Create(&run).Error
	})
	return jid, err
}

func (s *Service) directoryStatus(ctx context.Context, id idgen.ID) (*DirectorySyncStatus, error) {
	if err := directoryLicense(); err != nil {
		return nil, err
	}
	src, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	db, err := s.directoryDB()
	if err != nil {
		return nil, err
	}
	var run directoryRun
	err = db.WithContext(ctx).Where("tenant_id = ? AND source_id = ?", src.TenantID, id).First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &DirectorySyncStatus{Status: "idle"}, nil
	}
	if err != nil {
		return nil, err
	}
	m := job.Default()
	if m == nil {
		return nil, badReq("directory sync runner unavailable")
	}
	j, err := m.Get(ctx, job.Actor{Admin: true}, run.JobID)
	if errors.Is(err, job.ErrNotFound) {
		return &DirectorySyncStatus{JobID: run.JobID.String(), Status: "expired", Scanned: run.Scanned,
			Created: run.Created, Updated: run.Updated, Deactivated: run.Deactivated, Complete: run.Complete}, nil
	}
	if err != nil {
		return nil, err
	}
	return &DirectorySyncStatus{JobID: j.ID.String(), Status: j.Status, Scanned: run.Scanned,
		Created: run.Created, Updated: run.Updated, Deactivated: run.Deactivated, Complete: run.Complete,
		Error: j.Error, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt}, nil
}

func (s *Service) runDirectorySync(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := directoryLicense(); err != nil {
		return job.ResultFile{}, err
	}
	var params struct {
		SourceID idgen.ID `json:"sourceId"`
	}
	if err := j.Decode(&params); err != nil {
		return job.ResultFile{}, err
	}
	db, err := s.directoryDB()
	if err != nil {
		return job.ResultFile{}, err
	}
	var workErr error
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		src, err := lockDirectory(ctx, tx, j.TenantID, params.SourceID)
		if err != nil {
			return err
		}
		var run directoryRun
		if err := tx.Where("tenant_id = ? AND source_id = ? AND job_id = ?", j.TenantID, src.ID, j.ID).First(&run).Error; err != nil {
			return err
		}
		cfg, err := directoryConfig(src)
		if err != nil {
			return err
		}
		a := &ldapAuthenticator{svc: s, dial: s.dial, log: s.log}
		sess, err := a.connect(ctx, cfg)
		if err != nil {
			return err
		}
		defer sess.Close()
		var entries []ldapEntry
		if ps, ok := sess.(pagedSearcher); ok {
			entries, err = ps.SearchPaged(cfg.BaseDN, scopeSub, cfg.DirectorySync.Filter, cfg.attrs(), maxDirectoryEntries)
		} else {
			entries, err = sess.Search(cfg.BaseDN, scopeSub, cfg.DirectorySync.Filter, cfg.attrs(), maxLDAPEntries)
		}
		if err != nil {
			return err
		}
		if len(entries) > maxDirectoryEntries {
			return job.Errorf("Directory exceeds %d entries", maxDirectoryEntries)
		}
		// Validate every subject and account before the first user write.
		identities := make([]auth.ExternalIdentity, 0, len(entries))
		subjects, names := map[string]bool{}, map[string]bool{}
		for _, e := range entries {
			sub, err := directorySubject(e, cfg.DirectorySync.SubjectAttr)
			if err != nil {
				return err
			}
			name := strings.TrimSpace(e.first(cfg.AttrUsername))
			if e.DN == "" || !localUsernameRe.MatchString(name) || subjects[sub] || names[strings.ToLower(name)] {
				return job.Errorf("Directory contains invalid or duplicate accounts/subjects")
			}
			codes, err := mappedGroupRoles(e.Attrs[strings.ToLower(cfg.AttrGroups)], cfg.GroupRoleMappings, cfg.RequireMappedGroup)
			if err != nil {
				return err
			}
			subjects[sub], names[strings.ToLower(name)] = true, true
			identities = append(identities, auth.ExternalIdentity{ProviderKey: directoryProvider(src.ID), Subject: sub,
				Username: name, Nickname: clip(e.first(cfg.AttrNickname), 64), Email: clip(e.first(cfg.AttrEmail), 100),
				Phone: clip(e.first(cfg.AttrPhone), 32), DefaultRole: cfg.DefaultRole, MappedRoleCodes: codes})
		}
		run.Scanned = len(entries)
		run.Created, run.Updated, run.Deactivated, run.Complete = 0, 0, 0, false
		for i, id := range identities {
			if workErr = ctx.Err(); workErr != nil {
				break
			}
			if workErr = directoryLicense(); workErr != nil {
				break
			}
			var created bool
			created, workErr = s.auth.SyncExternal(ctx, id)
			if workErr != nil {
				break
			}
			if created {
				run.Created++
			} else {
				run.Updated++
			}
			report((i+1)*100/max(1, len(identities)), fmt.Sprintf("Created %d, updated %d", run.Created, run.Updated))
		}
		// 只有“完整枚举 + 全部写入成功”之后才可能停用消失的账号；防误停保护在用户模块里（返回数过少即中止）。
		if workErr == nil && cfg.DirectorySync.DeactivateMissing {
			keep := make(map[string]bool, len(identities))
			for _, id := range identities {
				keep[id.Subject] = true
			}
			run.Deactivated, workErr = s.deactivateMissing(ctx, directoryProvider(src.ID), keep)
		}
		run.Complete = workErr == nil
		return tx.Model(&directoryRun{}).Where("tenant_id = ? AND source_id = ? AND job_id = ?", j.TenantID, src.ID, j.ID).
			Updates(map[string]any{"scanned": run.Scanned, "created": run.Created, "updated": run.Updated,
				"deactivated": run.Deactivated, "complete": run.Complete}).Error
	})
	if err != nil {
		return job.ResultFile{}, err
	}
	return job.ResultFile{}, workErr
}

func (s *Service) deactivateMissing(ctx context.Context, provider string, keep map[string]bool) (int, error) {
	var p any
	if s.auth != nil {
		p = s.auth.Provider()
	}
	d, ok := p.(directoryDeactivator)
	if !ok {
		return 0, job.Errorf("The user module cannot deactivate missing directory accounts")
	}
	n, err := d.DeactivateMissingExternal(ctx, provider, keep)
	if err != nil {
		return n, job.Errorf("Deactivation skipped: %v", err)
	}
	return n, nil
}
