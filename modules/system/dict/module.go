package dict

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
	tn "nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "dict" }

// Register 注册字典路由，权限码 dict:list|create|update|delete（类型与数据共用）。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &Handler{svc: NewService(d.DB)}
	r.GET("/dicts/:code/items", h.Items) // 登录即可，前端所有下拉/标签都走它

	t := r.Group("/dict/types")
	t.PUT("/sort", core.Perm("dict:update"), h.SortTypes) // 固定路径先于 /:id
	t.GET("", core.Perm("dict:list"), h.ListTypes)
	t.GET("/:id", core.Perm("dict:list"), h.GetType)
	t.POST("", core.Perm("dict:create"), h.CreateType)
	t.PUT("/:id", core.Perm("dict:update"), h.UpdateType)
	t.DELETE("/:id", core.Perm("dict:delete"), h.DeleteType)
	t.DELETE("", core.Perm("dict:delete"), h.DeleteTypes)

	x := r.Group("/dict/data")
	x.PUT("/sort", core.Perm("dict:update"), h.SortData)
	x.GET("", core.Perm("dict:list"), h.ListData)
	x.GET("/:id", core.Perm("dict:list"), h.GetData)
	x.POST("", core.Perm("dict:create"), h.CreateData)
	x.PUT("/:id", core.Perm("dict:update"), h.UpdateData)
	x.DELETE("/:id", core.Perm("dict:delete"), h.DeleteOneData)
	x.DELETE("", core.Perm("dict:delete"), h.DeleteManyData)
}

// 种子 ID 与前端 mock 一致：类型 7100000000000010000+i，数据 7200000000010000000+n（n 从 1 起按全局顺序）。
func typeSeedID(i int) idgen.ID { return idgen.ID(7100000000000010000 + int64(i)) }
func dataSeedID(n int) idgen.ID { return idgen.ID(7200000000010000000 + int64(n)) }

// Seed 幂等种入出厂字典：类型按 code、数据按 类型+value 判断，已存在一律不动。
func (module) Seed(db *gorm.DB) error {
	db = db.WithContext(tn.Skip(context.Background()))
	now := time.Now().UTC()
	n := 0
	for i, st := range seedTypes {
		var t Type
		err := db.Where("code = ?", st.Code).Limit(1).Find(&t).Error
		if err != nil {
			return err
		}
		if t.ID == 0 {
			status := "1"
			if st.Disabled {
				status = "0"
			}
			t = Type{Name: st.Name, Code: st.Code, Sort: (i + 1) * 10, Status: status, Remark: st.Remark}
			t.ID, t.CreatedAt, t.UpdatedAt = typeSeedID(i), now, now
			if err := db.Create(&t).Error; err != nil {
				return err
			}
		}
		for j, it := range st.Items {
			n++
			var cnt int64
			if err := db.Model(&Data{}).Where("type_id = ? AND dict_value = ?", int64(t.ID), it.Value).Count(&cnt).Error; err != nil {
				return err
			}
			if cnt > 0 {
				continue
			}
			d := Data{TypeID: t.ID, Label: it.Label, LabelEn: it.LabelEn, Value: it.Value, Tag: it.Tag,
				Sort: (j + 1) * 10, Status: "1", Remark: it.LabelEn}
			d.ID, d.CreatedAt, d.UpdatedAt = dataSeedID(n), now, now
			// 固定 ID 按全局序号推算：往字典里新增一项会把后面所有项的序号挤开，
			// 新项的 ID 可能已被老项占用。撞了就交给雪花生成，别让启动种子直接失败。
			var taken int64
			if err := db.Model(&Data{}).Where("id = ?", int64(d.ID)).Count(&taken).Error; err != nil {
				return err
			}
			if taken > 0 {
				d.ID = 0
			}
			if err := db.Create(&d).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// SeedOnStartup 实现 setup.StartupSeeder：两次查询比对出厂字典（类型 code、数据 类型+value），齐全就返回；
// 缺项才走完整的幂等 Seed。
func (m module) SeedOnStartup(db *gorm.DB) error {
	db = db.WithContext(tn.Skip(db.Statement.Context))
	var types []Type
	if err := db.Select("id", "code").Find(&types).Error; err != nil {
		return err
	}
	idByCode := make(map[string]idgen.ID, len(types))
	for _, t := range types {
		idByCode[t.Code] = t.ID
	}
	var data []Data
	if err := db.Select("id", "type_id", "dict_value").Find(&data).Error; err != nil {
		return err
	}
	have := make(map[idgen.ID]map[string]bool, len(types))
	for _, d := range data {
		if have[d.TypeID] == nil {
			have[d.TypeID] = map[string]bool{}
		}
		have[d.TypeID][d.Value] = true
	}
	for _, st := range seedTypes {
		id, ok := idByCode[st.Code]
		if !ok {
			return m.Seed(db)
		}
		for _, it := range st.Items {
			if !have[id][it.Value] {
				return m.Seed(db)
			}
		}
	}
	return nil
}
