// Package seed 是出厂数据的公共入口：内置菜单、插件 plugin.yaml 的菜单、`nexadmin add module` 生成的菜单种子
// 都走这里。规则（设计文档 §5）：按稳定业务键匹配，不存在才插入，已存在一律不动。
package seed

import (
	"context"
	"sync"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// MenuItem 是一条出厂菜单。Code 是业务键（目录 "dir:"+路径，菜单/按钮用权限码）；
// Parent 写父节点的 Code，目录也可以直接写它的路径（如 "/system"），留空表示顶级。
// ID 为 0 时由框架生成雪花 ID；内置菜单带固定出厂 ID。
type MenuItem struct {
	ID        idgen.ID
	Parent    string
	Code      string
	Type      string
	Title     string
	Icon      string
	Path      string
	Component string
	Perm      string
	Sort      int
	KeepAlive bool
	Hidden    bool
	External  bool
	Status    string // 空 = "1"（启用）
	CreatedAt time.Time
}

// menuRow 对应 sys_menu（平台级）。core 不能依赖 modules，所以这里单独声明。
type menuRow struct {
	database.PlatformModel
	ParentID  idgen.ID
	Code      string
	Type      string
	Title     string
	Icon      string
	Path      string
	Component string
	Perm      string
	Sort      int
	KeepAlive bool
	Hidden    bool
	External  bool
	Status    string
}

func (menuRow) TableName() string { return "sys_menu" }

// Menus 幂等补齐菜单，返回新增条数。已齐全时只有一次查询、没有写入、不开事务。
// 父节点按业务键解析；父节点不存在（被客户删了）的条目连同它的子孙一起跳过，记入 Report.Skipped。
func Menus(db *gorm.DB, items []MenuItem) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}
	codes := make([]string, 0, len(items)*2)
	for _, it := range items {
		codes = append(codes, it.Code)
		if it.Parent != "" {
			codes = append(codes, it.Parent, "dir:"+it.Parent)
		}
	}
	var rows []menuRow
	if err := db.Select("id", "code").Where("code IN ?", codes).Find(&rows).Error; err != nil {
		return 0, err
	}
	idByCode := make(map[string]idgen.ID, len(rows))
	for _, r := range rows {
		idByCode[r.Code] = r.ID
	}
	var missing []MenuItem
	for _, it := range items {
		if _, ok := idByCode[it.Code]; !ok {
			missing = append(missing, it)
		}
	}
	if len(missing) == 0 {
		return 0, nil
	}
	rep := ReportFrom(db.Statement.Context)
	added := 0
	err := db.Transaction(func(tx *gorm.DB) error {
		added = 0 // 事务重试 / 回滚后重新计数
		now := time.Now().UTC()
		for _, it := range missing { // items 里父在前子在后
			var parent idgen.ID
			if it.Parent != "" {
				p, ok := idByCode[it.Parent]
				if !ok {
					p, ok = idByCode["dir:"+it.Parent]
				}
				if !ok {
					rep.skip(it.Code)
					continue
				}
				parent = p
			}
			status := it.Status
			if status == "" {
				status = "1"
			}
			m := menuRow{
				ParentID: parent, Code: it.Code, Type: it.Type, Title: it.Title, Icon: it.Icon, Path: it.Path,
				Component: it.Component, Perm: it.Perm, Sort: it.Sort, KeepAlive: it.KeepAlive, Hidden: it.Hidden,
				External: it.External, Status: status,
			}
			m.ID, m.CreatedAt, m.UpdatedAt = it.ID, now, now
			if !it.CreatedAt.IsZero() {
				m.CreatedAt, m.UpdatedAt = it.CreatedAt, it.CreatedAt
			}
			if err := tx.Create(&m).Error; err != nil {
				return err
			}
			idByCode[m.Code] = m.ID
			added++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	rep.addMenus(added)
	return added, nil
}

// Report 汇总一次启动里各种子实际写入了什么，用来打印日志。通过 context 传给种子函数。
type Report struct {
	mu      sync.Mutex
	menus   int
	skipped []string
}

type reportKey struct{}

// WithReport 返回携带 Report 的 context。
func WithReport(ctx context.Context, r *Report) context.Context {
	return context.WithValue(ctx, reportKey{}, r)
}

// ReportFrom 取出 Report；没有时返回 nil（Report 的方法对 nil 安全）。
func ReportFrom(ctx context.Context) *Report {
	if ctx == nil {
		return nil
	}
	r, _ := ctx.Value(reportKey{}).(*Report)
	return r
}

func (r *Report) addMenus(n int) {
	if r == nil || n == 0 {
		return
	}
	r.mu.Lock()
	r.menus += n
	r.mu.Unlock()
}

func (r *Report) skip(code string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.skipped = append(r.skipped, code)
	r.mu.Unlock()
}

// Menus 返回新增的菜单条数。
func (r *Report) Menus() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.menus
}

// Skipped 返回因父节点不存在而跳过的菜单业务键。
func (r *Report) Skipped() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.skipped...)
}
