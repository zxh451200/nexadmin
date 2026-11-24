package post

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/job"
	tn "nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "post" }

func (module) TenantTables() []tn.Table {
	return []tn.Table{{Name: "sys_post", Column: "tenant_id"}}
}

// Register 注册 /api/v1/posts 路由，权限码 post:list|create|update|delete。
// /posts/options 只要登录即可（用户表单下拉用，不暴露岗位详情）。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &Handler{svc: NewService(d.DB)}
	g := r.Group("/posts")
	g.GET("/options", h.Options) // 固定路径先于 /:id
	g.PUT("/sort", core.Perm("post:update"), d.OperLogMiddleware("post", "sort"), h.Sort)
	g.DELETE("", core.Perm("post:delete"), d.OperLogMiddleware("post", "delete"), h.DeleteBatch) // 批量删除：?ids=1,2,3，权限码与单条删除同一个
	g.GET("", core.Perm("post:list"), h.List)
	job.Register(jobTypePostExport, h.svc.ExportJob) // 导出统一走导出中心（异步任务）
	g.POST("/export-async", core.Perm("post:list"), d.OperLogMiddleware("post", "export"), h.ExportAsync)
	g.GET("/:id", core.Perm("post:list"), h.Get)
	g.POST("", core.Perm("post:create"), d.OperLogMiddleware("post", "create"), h.Create)
	g.PUT("/:id", core.Perm("post:update"), d.OperLogMiddleware("post", "update"), h.Update)
	g.DELETE("/:id", core.Perm("post:delete"), d.OperLogMiddleware("post", "delete"), h.Delete)
}

// 种子 ID 与前端 mock 一致：岗位 187000000000000+序号，部门 186000000000000+序号。
func postID(n int) idgen.ID { return idgen.ID(1870000000000000000 + int64(n)) }

type seedPost struct {
	n      int
	name   string
	code   string
	dept   int // 0 表示不关联部门
	status string
	remark string
}

var seedPosts = []seedPost{
	{1, "董事长", "chairman", 1, "1", "公司最高负责人"},
	{2, "技术总监", "cto", 2, "1", "统筹研发中心技术路线"},
	{3, "前端工程师", "frontend", 6, "1", "React / TypeScript"},
	{4, "后端工程师", "backend", 7, "1", "Go / PostgreSQL"},
	{5, "测试工程师", "qa", 8, "1", ""},
	{6, "市场经理", "marketing_manager", 3, "1", "负责品牌与市场活动"},
	{7, "客服专员", "support", 11, "1", ""},
	{8, "人事专员", "hr", 12, "0", "暂停招聘，岗位冻结"},
}

// legacyPostNames 早期出厂岗位名带英文后缀（技术总监 CTO…），与部门同一类问题：
// 岗位名是数据，中文界面里不该两套语言混排。按「默认租户 + 岗位编码 + 旧名」就地改名，
// 用户改过名或不认识旧名的行不匹配，不会被覆盖。
var legacyPostNames = map[string]string{
	"cto":     "技术总监 CTO",
	"backend": "后端工程师 Backend Engineer",
	"support": "客服专员 Support",
}

func seedPostName(code string) string {
	for _, s := range seedPosts {
		if s.code == code {
			return s.name
		}
	}
	return ""
}

// SeedOnStartup 实现 setup.StartupSeeder：每次启动先把早期双语名就地改名，再补缺失的出厂岗位。
func (module) SeedOnStartup(db *gorm.DB) error {
	db = db.WithContext(tn.Skip(context.Background()))
	for code, old := range legacyPostNames {
		name := seedPostName(code)
		if name == "" {
			continue
		}
		if err := db.Model(&Post{}).Where("tenant_id = ? AND code = ? AND name = ?", int64(tn.DefaultID), code, old).Update("name", name).Error; err != nil {
			return err
		}
	}
	return module{}.Seed(db)
}

// Seed 幂等种入默认租户的出厂岗位：按岗位编码判断，已存在一律不动。
func (module) Seed(db *gorm.DB) error {
	db = db.WithContext(tn.Skip(context.Background()))
	now := time.Now().UTC()
	for _, s := range seedPosts {
		var cnt int64
		if err := db.Model(&Post{}).Where("tenant_id = ? AND code = ?", int64(tn.DefaultID), s.code).Count(&cnt).Error; err != nil {
			return err
		}
		if cnt > 0 {
			continue
		}
		p := &Post{Name: s.name, Code: s.code, Sort: s.n * 10, Status: s.status, Remark: s.remark}
		p.ID, p.TenantID = postID(s.n), tn.DefaultID
		p.CreatedAt, p.UpdatedAt = now, now
		if s.dept != 0 {
			did := idgen.ID(1860000000000000000 + int64(s.dept))
			p.DeptID = &did
		}
		if err := db.Create(p).Error; err != nil {
			return err
		}
	}
	return nil
}
