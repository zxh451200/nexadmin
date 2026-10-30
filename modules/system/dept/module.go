package dept

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

func (module) Name() string { return "dept" }

// Register 注册 /api/v1/depts 路由，权限码 dept:list|create|update|delete。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &Handler{svc: NewService(d.DB)}
	g := r.Group("/depts")
	g.GET("/tree", core.Perm("dept:list"), h.Tree)
	g.PUT("/sort", core.Perm("dept:update"), h.Sort)     // 固定路径先于 /:id
	g.PUT("/:id/move", core.Perm("dept:update"), h.Move) // 跨父级移动（拖拽）
	g.GET("/:id", core.Perm("dept:list"), h.Get)
	g.POST("", core.Perm("dept:create"), h.Create)
	g.PUT("/:id", core.Perm("dept:update"), h.Update)
	g.DELETE("/:id", core.Perm("dept:delete"), h.Delete)
}

// seedID 与前端 mock 的部门 ID 一致（186000000000000 + 4 位序号）。
func seedID(n int) idgen.ID { return idgen.ID(1860000000000000000 + int64(n)) }

type seedDept struct {
	n, parent    int // parent=0 表示根
	name         string
	sort         int
	leader       string
	phone, email string
	status       string
}

var seedDepts = []seedDept{
	{1, 0, "总公司 Headquarters", 10, "Admin", "010-88886666", "hq@nexadmin.io", "1"},
	{2, 1, "研发中心", 10, "张伟", "010-88886601", "rd@nexadmin.io", "1"},
	{3, 1, "市场中心", 20, "Emily Chen", "010-88886602", "marketing@nexadmin.io", "1"},
	{4, 1, "运营中心", 30, "李娜", "010-88886603", "ops@nexadmin.io", "1"},
	{5, 1, "综合管理部", 40, "王芳", "010-88886604", "admin@nexadmin.io", "1"},
	{6, 2, "前端组 Frontend", 10, "刘洋", "010-88886611", "fe@nexadmin.io", "1"},
	{7, 2, "后端组 Backend", 20, "Kevin Zhao", "010-88886612", "be@nexadmin.io", "1"},
	{8, 2, "测试组 QA", 30, "陈静", "010-88886613", "qa@nexadmin.io", "1"},
	{9, 3, "品牌部", 10, "周杰", "010-88886621", "brand@nexadmin.io", "1"},
	{10, 3, "销售部 Sales", 20, "Linda Wu", "010-88886622", "sales@nexadmin.io", "0"},
	{11, 4, "客服部", 10, "赵敏", "010-88886631", "cs@nexadmin.io", "1"},
	{12, 5, "人力资源组 HR", 10, "孙丽", "010-88886641", "hr@nexadmin.io", "1"},
}

// Seed 幂等种入默认租户的出厂部门：按固定 ID 判断，已存在一律不动。
func (module) Seed(db *gorm.DB) error {
	db = db.WithContext(tn.Skip(context.Background()))
	now := time.Now().UTC()
	paths := map[int]string{}
	for _, s := range seedDepts { // 父节点在前，path 可直接拼接
		path := "/" + seedID(s.n).String() + "/"
		if s.parent != 0 {
			path = paths[s.parent] + seedID(s.n).String() + "/"
		}
		paths[s.n] = path
		var cnt int64
		if err := db.Model(&Dept{}).Where("id = ?", int64(seedID(s.n))).Count(&cnt).Error; err != nil {
			return err
		}
		if cnt > 0 {
			continue
		}
		leader := idgen.ID(1900000000000000000 + int64(s.n))
		d := &Dept{
			Name: s.name, Path: path, Sort: s.sort, LeaderID: &leader, LeaderName: s.leader,
			Phone: s.phone, Email: s.email, Status: s.status,
		}
		d.ID, d.TenantID = seedID(s.n), tn.DefaultID
		d.CreatedAt, d.UpdatedAt = now, now
		if s.parent != 0 {
			p := seedID(s.parent)
			d.ParentID = &p
		}
		if err := db.Create(d).Error; err != nil {
			return err
		}
	}
	return nil
}
