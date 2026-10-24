package i18neditor

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/language"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core"
	"nexadmin/core/cluster"
	"nexadmin/core/i18n"
	"nexadmin/core/plugin"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func init() {
	plugin.Register(plugin.Plugin{ID: "i18n-editor", Name: "多语言在线编辑", Version: "1.0.0", Edition: plugin.Pro, Module: &module{}})
}

type module struct {
	once sync.Once
	svc  *service
}

func (*module) Name() string { return "i18n-editor" }

func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	m.once.Do(func() { m.svc = &service{db: d.DB, bus: d.Bus} })
	g := r.Group("/plugins/i18n-editor")
	g.GET("/published", m.svc.published)
	g.GET("/entries", core.Perm("i18n-editor:list"), m.svc.list)
	g.PUT("/entries", core.Perm("i18n-editor:edit"), m.svc.save)
	g.POST("/publish", core.Perm("i18n-editor:publish"), m.svc.publish)
	g.POST("/rollback", core.Perm("i18n-editor:publish"), m.svc.rollback)
	g.GET("/versions", core.Perm("i18n-editor:list"), m.svc.versions)
}

func (m *module) Start(ctx context.Context, d *core.Deps) error {
	if m.svc == nil {
		m.svc = &service{db: d.DB, bus: d.Bus}
	}
	if err := m.svc.restorePublished(ctx); err != nil {
		return err
	}
	if d.Bus != nil {
		d.Bus.On(cluster.KindI18nInvalidate, func(raw json.RawMessage) {
			var ev i18nEvent
			if json.Unmarshal(raw, &ev) == nil {
				m.svc.refresh(ctx, ev.TenantID, ev.Lang)
			}
		}, func() {
			if err := m.svc.restorePublished(ctx); err != nil && ctx.Err() == nil {
				log.Printf("i18n-editor: restore published: %v", err)
			}
		})
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := m.svc.restorePublished(ctx); err != nil && ctx.Err() == nil {
					log.Printf("i18n-editor: periodic restore: %v", err)
				}
			}
		}
	}()
	return nil
}

type i18nEvent struct {
	TenantID idgen.ID `json:"tenant_id"`
	Lang     string   `json:"lang"`
}

type entry struct {
	ID        idgen.ID  `json:"id" gorm:"primaryKey;autoIncrement:false"`
	TenantID  idgen.ID  `json:"tenantId" gorm:"index;not null"`
	Lang      string    `json:"lang" gorm:"size:32;not null"`
	Key       string    `json:"key" gorm:"size:255;not null"`
	Value     string    `json:"value" gorm:"size:2000;not null"`
	Status    string    `json:"status" gorm:"size:16;not null"` // draft/published
	Version   int       `json:"version" gorm:"not null"`
	UpdatedAt time.Time `json:"updatedAt"`
	CreatedAt time.Time `json:"createdAt"`
}

func (entry) TableName() string { return "plg_i18n_editor_entry" }

type version struct {
	ID        idgen.ID  `json:"id" gorm:"primaryKey;autoIncrement:false"`
	TenantID  idgen.ID  `json:"tenantId" gorm:"index;not null"`
	Lang      string    `json:"lang" gorm:"size:32;not null"`
	Version   int       `json:"version" gorm:"not null"`
	Entries   string    `json:"entries" gorm:"type:text;not null"`
	CreatedAt time.Time `json:"createdAt"`
}

func (version) TableName() string { return "plg_i18n_editor_version" }

type service struct {
	db        *gorm.DB
	bus       *cluster.Bus
	restoreMu sync.Mutex
}

func (s *service) restorePublished(ctx context.Context) error {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	var rows []version
	if err := s.db.WithContext(tenant.Skip(ctx)).Order("tenant_id,lang,version DESC").Find(&rows).Error; err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		key := strconv.FormatInt(int64(row.TenantID), 10) + ":" + row.Lang
		if seen[key] {
			continue
		}
		seen[key] = true
		if err := applyVersion(ctx, row); err != nil {
			// A corrupt historical language must not block recovery of other tenants.
			log.Printf("i18n-editor: restore tenant %v language %s: %v", row.TenantID, row.Lang, err)
		}
	}
	return nil
}

func (s *service) restoreLanguage(ctx context.Context, tid idgen.ID, lang string) error {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	var row version
	if err := s.db.WithContext(tenant.Skip(ctx)).Where("tenant_id = ? AND lang = ?", tid, lang).Order("version DESC").First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			i18n.SetRuntimeOverrides(tid, lang, nil)
			return nil
		}
		return err
	}
	return applyVersion(ctx, row)
}

func applyVersion(ctx context.Context, row version) error {
	values := map[string]string{}
	if err := json.Unmarshal([]byte(row.Entries), &values); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	i18n.SetRuntimeOverrides(row.TenantID, row.Lang, values)
	return nil
}

func (s *service) broadcast(tid idgen.ID, lang string) {
	if s.bus != nil {
		s.bus.Publish(cluster.KindI18nInvalidate, i18nEvent{TenantID: tid, Lang: lang})
	}
}

func (s *service) refresh(ctx context.Context, tid idgen.ID, lang string) {
	if err := s.restoreLanguage(ctx, tid, lang); err != nil && ctx.Err() == nil {
		log.Printf("i18n-editor: refresh tenant %v language %s: %v", tid, lang, err)
	}
}

// Take a database write lock before any reads. A no-op UPDATE also serializes
// SQLite writers, where SELECT FOR UPDATE is unavailable. The lock is shared
// by all plugin instances and held until this transaction ends.
func (s *service) mutate(ctx context.Context, tid idgen.ID, fn func(*gorm.DB) error) error {
	if tid <= 0 {
		return errors.New("missing tenant")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		lock := tx.WithContext(tenant.Skip(ctx)).Table("sys_tenant").Where("id = ?", tid)
		if err := lock.UpdateColumn("id", gorm.Expr("id")).Error; err != nil {
			return err
		}
		// Do not rely on RowsAffected: MySQL may report zero for a no-op.
		var row struct{ ID idgen.ID }
		if err := tx.WithContext(tenant.Skip(ctx)).Table("sys_tenant").
			Select("id").Where("id = ?", tid).Take(&row).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

func respond(c *gin.Context, status int, data any) {
	if status == 200 {
		c.JSON(status, gin.H{"code": 0, "message": "ok", "data": data})
		return
	}
	c.JSON(status, gin.H{"code": status, "message": data, "data": nil})
}

type entryInput struct {
	Lang  string `json:"lang"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

var namespacePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Read only published snapshots, scoped exclusively by authenticated tenant.
func (s *service) published(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Vary", "Authorization")
	tid := mustTenant(c)
	if tid <= 0 {
		respond(c, 403, "missing tenant")
		return
	}
	lang, ns := strings.TrimSpace(c.Query("lang")), c.DefaultQuery("namespace", "translation")
	if !validLanguage(lang) || !namespacePattern.MatchString(ns) {
		respond(c, 400, "invalid language or namespace")
		return
	}
	var row version
	err := s.db.WithContext(c.Request.Context()).Where("tenant_id = ? AND lang = ?", tid, lang).
		Order("version DESC").First(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		respond(c, 500, "internal error")
		return
	}
	values, entries := map[string]string{}, map[string]string{}
	if err == nil && json.Unmarshal([]byte(row.Entries), &values) != nil {
		respond(c, 500, "invalid published snapshot")
		return
	}
	for key, value := range values {
		namespace, name := "translation", key
		if before, after, ok := strings.Cut(key, ":"); ok {
			namespace, name = before, after
		}
		if namespace == ns {
			entries[name] = value
		}
	}
	respond(c, 200, gin.H{"tenantId": tid, "lang": lang, "namespace": ns, "version": row.Version, "entries": entries})
}

func (s *service) list(c *gin.Context) {
	lang := strings.TrimSpace(c.Query("lang"))
	if lang != "" && !validLanguage(lang) {
		respond(c, 400, "invalid language")
		return
	}
	q := s.db.WithContext(c.Request.Context()).Where("tenant_id = ?", mustTenant(c))
	if lang != "" {
		q = q.Where("lang = ?", lang)
	}
	rows := []entry{}
	if err := q.Order(clause.OrderByColumn{Column: clause.Column{Name: "lang"}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "key"}}).Find(&rows).Error; err != nil {
		respond(c, 500, "internal error")
		return
	}
	respond(c, 200, gin.H{"list": rows, "total": len(rows)})
}

func (s *service) save(c *gin.Context) {
	var in entryInput
	if c.ShouldBindJSON(&in) != nil || !validEntry(in) {
		respond(c, 400, "invalid entry or placeholders")
		return
	}
	tid := mustTenant(c)
	var row entry
	err := s.mutate(c.Request.Context(), tid, func(tx *gorm.DB) error {
		err := tx.Where(map[string]any{"tenant_id": tid, "lang": in.Lang, "key": in.Key}).First(&row).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		now := time.Now().UTC()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			id, err := idgen.Next()
			if err != nil {
				return err
			}
			row = entry{ID: id, TenantID: tid, Lang: in.Lang, Key: in.Key, CreatedAt: now}
		}
		if err := validateValue(tx, tid, in); err != nil {
			return err
		}
		row.Value, row.Status, row.UpdatedAt = in.Value, "draft", now
		return tx.Save(&row).Error
	})
	if err != nil {
		if errors.Is(err, errPlaceholders) {
			respond(c, 400, err.Error())
			return
		}
		respond(c, 500, "internal error")
		return
	}
	respond(c, 200, row)
}

func (s *service) publish(c *gin.Context) {
	lang := strings.TrimSpace(c.Query("lang"))
	if !validLanguage(lang) {
		respond(c, 400, "invalid language")
		return
	}
	tid := mustTenant(c)
	values := map[string]string{}
	var next int
	err := s.mutate(c.Request.Context(), tid, func(tx *gorm.DB) error {
		var rows []entry
		if err := tx.Where("tenant_id = ? AND lang = ?", tid, lang).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			in := entryInput{Lang: lang, Key: row.Key, Value: row.Value}
			if !validEntry(in) {
				return errPlaceholders
			}
			if err := validateValue(tx, tid, in); err != nil {
				return err
			}
			values[row.Key] = row.Value
		}
		var err error
		next, err = createSnapshot(tx, tid, lang, values)
		if err != nil {
			return err
		}
		return tx.Model(&entry{}).Where("tenant_id = ? AND lang = ?", tid, lang).
			Updates(map[string]any{"status": "published", "version": next, "updated_at": time.Now().UTC()}).Error
	})
	if err != nil {
		if errors.Is(err, errPlaceholders) {
			respond(c, 400, err.Error())
			return
		}
		respond(c, 500, "internal error")
		return
	}
	s.refresh(c.Request.Context(), tid, lang)
	s.broadcast(tid, lang)
	respond(c, 200, gin.H{"lang": lang, "version": next, "count": len(values)})
}

// Caller holds the tenant write lock. Only INSERTs touch snapshot history.
func createSnapshot(tx *gorm.DB, tid idgen.ID, lang string, values map[string]string) (int, error) {
	var latest version
	err := tx.Where("tenant_id = ? AND lang = ?", tid, lang).Order("version DESC").First(&latest).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	// Migrations use SQL INT, including PostgreSQL's signed 32-bit integer.
	if latest.Version < 0 || latest.Version >= 1<<31-1 {
		return 0, errors.New("version overflow")
	}
	next := latest.Version + 1
	raw, err := json.Marshal(values)
	if err != nil {
		return 0, err
	}
	id, err := idgen.Next()
	if err != nil {
		return 0, err
	}
	err = tx.Create(&version{ID: id, TenantID: tid, Lang: lang, Version: next, Entries: string(raw), CreatedAt: time.Now().UTC()}).Error
	return next, err
}

func (s *service) rollback(c *gin.Context) {
	lang := strings.TrimSpace(c.Query("lang"))
	v, err := strconv.Atoi(c.Query("version"))
	if !validLanguage(lang) || err != nil || v <= 0 {
		respond(c, 400, "invalid language or version")
		return
	}
	tid := mustTenant(c)
	values := map[string]string{}
	var next int
	err = s.mutate(c.Request.Context(), tid, func(tx *gorm.DB) error {
		var source version
		if err := tx.Where("tenant_id = ? AND lang = ? AND version = ?", tid, lang, v).First(&source).Error; err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(source.Entries), &values); err != nil {
			return err
		}
		// Replace the working set exactly, including removing keys absent in the
		// source. Otherwise a subsequent publish would resurrect newer keys.
		for key, value := range values {
			in := entryInput{Lang: lang, Key: key, Value: value}
			if !validEntry(in) {
				return errPlaceholders
			}
			if err := validateValue(tx, tid, in); err != nil {
				return err
			}
		}
		var err error
		next, err = createSnapshot(tx, tid, lang, values)
		if err != nil {
			return err
		}
		if err := tx.Where("tenant_id = ? AND lang = ?", tid, lang).Delete(&entry{}).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		for key, value := range values {
			id, err := idgen.Next()
			if err != nil {
				return err
			}
			item := entry{ID: id, TenantID: tid, Lang: lang, Key: key, Value: value,
				Status: "published", Version: next, CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respond(c, 404, "version not found")
			return
		}
		if errors.Is(err, errPlaceholders) {
			respond(c, 400, err.Error())
			return
		}
		respond(c, 500, "internal error")
		return
	}
	s.refresh(c.Request.Context(), tid, lang)
	s.broadcast(tid, lang)
	respond(c, 200, gin.H{"lang": lang, "version": next, "count": len(values)})
}

func (s *service) versions(c *gin.Context) {
	lang := strings.TrimSpace(c.Query("lang"))
	if !validLanguage(lang) {
		respond(c, 400, "invalid language")
		return
	}
	rows := []version{}
	if err := s.db.WithContext(c.Request.Context()).Where("tenant_id = ? AND lang = ?", mustTenant(c), lang).Order("version DESC").Find(&rows).Error; err != nil {
		respond(c, 500, "internal error")
		return
	}
	respond(c, 200, rows)
}

func validEntry(in entryInput) bool {
	return validLanguage(in.Lang) && utf8.ValidString(in.Key) && utf8.ValidString(in.Value) &&
		strings.TrimSpace(in.Key) != "" && utf8.RuneCountInString(in.Key) <= 255 &&
		utf8.RuneCountInString(in.Value) <= 2000 && !strings.ContainsRune(in.Key+in.Value, '\x00')
}

func validLanguage(lang string) bool {
	if len(lang) < 2 || len(lang) > 32 || strings.TrimSpace(lang) != lang {
		return false
	}
	if _, err := language.Parse(lang); err != nil {
		return false
	}
	if b := i18n.Default(); b != nil {
		for _, l := range b.Languages() {
			if l.Code == lang {
				return true
			}
		}
		return false
	}
	return true
}

var errPlaceholders = errors.New("invalid entry or mismatched placeholders")

// fmt placeholders retain order and full formatting to preserve argument types,
// indexes, width and precision. Named placeholders may move, but not disappear
// or be duplicated. %% consumes no argument.
var printfToken = regexp.MustCompile(`%%|%(?:\[[1-9][0-9]*\])?[-+# 0]*(?:[0-9]+|\*|\[[1-9][0-9]*\]\*)?(?:\.(?:[0-9]+|\*|\[[1-9][0-9]*\]\*))?(?:\[[1-9][0-9]*\])?[vTtbcdoOqxXUeEfFgGspw]`)
var namedToken = regexp.MustCompile(`\{\{[A-Za-z_0-9][A-Za-z0-9_.]*\}\}|\{[A-Za-z_0-9][A-Za-z0-9_.]*\}`)

func samePlaceholders(source, value string) bool {
	printf := func(text string) []string {
		tokens := printfToken.FindAllString(text, -1)
		out := make([]string, 0, len(tokens))
		for _, token := range tokens {
			if token != "%%" {
				out = append(out, token)
			}
		}
		return out
	}
	a, b := namedToken.FindAllString(source, -1), namedToken.FindAllString(value, -1)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b) && slices.Equal(printf(source), printf(value))
}

func validateValue(tx *gorm.DB, tid idgen.ID, in entryInput) error {
	if b := i18n.Default(); b != nil {
		// Bundle.T reads the core/config catalogue without runtime overrides.
		source := b.T(in.Lang, in.Key)
		if source != in.Key && !samePlaceholders(source, in.Value) {
			return errPlaceholders
		}
	}
	var peers []entry
	if err := tx.Where(map[string]any{"tenant_id": tid, "key": in.Key}).Find(&peers).Error; err != nil {
		return err
	}
	for _, peer := range peers {
		if !samePlaceholders(peer.Value, in.Value) {
			return errPlaceholders
		}
	}
	return nil
}

func mustTenant(c *gin.Context) idgen.ID {
	id, _ := tenant.From(c.Request.Context())
	return id
}
