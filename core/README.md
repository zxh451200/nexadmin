# core/ 框架内核能力地图

`core/` 是框架内核：所有子包对模块和插件公开（`plugins/` 与业务模块只允许依赖 `core/` 与 `pkg/`）。
按能力查表：包名 → 职责 → 典型用法 → 代表文件。改动某个包前先看它"代表文件"的包注释。

## Web 与装配

| 包 | 职责 | 典型用法 | 代表文件 |
| --- | --- | --- | --- |
| `core`（根） | Module 接口、App 装配、Deps、RegisterBuiltin、LoadConfig、Perm 权限登记 | `func init() { core.RegisterBuiltin(m) }`；`func (m) Register(r *gin.RouterGroup, d *core.Deps)` | `app.go`、`operlog.go` |
| `router`（根，router.go） | 引擎装配：中间件链、模块挂载、路由冲突聚合报错、静态资源、NoRoute | 一般不直接用；`core.New(...)` 内部调用 | `router.go` |
| `config` | 配置分层加载（default.yaml → config.yaml → NEXADMIN_* env）与启动校验 | `core.LoadConfig("config/default.yaml", "config/config.yaml")` | `config/config.go` |
| `setup` | 初始化向导、非交互 `init --config`、Seed/StartupSeeder 钩子、IsInitialized | 模块实现 `setup.Seeder` 参与出厂数据 | `setup/`、`app.go` collect |
| `ctl` | 运维子命令：start/stop/restart/status、backup/restore-bundle、reinit | `nexadmin-server start`（见部署指南 §4） | `ctl/` |
| `static`（static.go） | 前端产物托管（embedweb 时 web/dist；hash 资源 immutable 缓存） | `core.WithStatic(web.Dist())` | `static.go` |

## 数据与租户

| 包 | 职责 | 典型用法 | 代表文件 |
| --- | --- | --- | --- |
| `database` | GORM 引擎创建（mysql/mariadb/postgres）、连接池、isolation（租户独立库路由） | 由框架调用；业务代码只拿 `d.DB` | `database/` |
| `tenant` | 租户解析链（header/域名/登录态）、`WithTenant` context、默认租户 | 查询已由 `TenantModel` 自动带租户；特殊场景 `tenant.With(ctx, id)` | `tenant/` |
| `datascope` | 数据范围裁剪（all/deptAndChild/dept/self/custom） | 参考 `modules/notice/scope.go` | `datascope/` |
| `region` | 省市区数据：随包内置完整三级树（go:embed），运行时可切换成同步下来的快照 | `GET /regions/tree`；同步接口见 `modules/region` | `region/` |
| `secret` | 静态加密（AES-256-GCM，主密钥 `data/secret.key` / `NEXADMIN_SECRET_KEY`） | 存第三方密钥等敏感值 | `secret/secret.go` |

## 认证与安全

| 包 | 职责 | 典型用法 | 代表文件 |
| --- | --- | --- | --- |
| `auth` | 登录/JWT 双 token、权限码登记与校验、Provider 接口、Claims | `core.Perm("article:list")`；`auth.RegisterPerm`；实现 `auth.Provider` 接入自定义登录 | `auth/` |
| `middleware` | 限流（固定窗口）、RequireLogin、租户中间件等 gin 中间件 | `middleware.RateLimitRule(d.Cache, d.Log, d.Config.RateLimit, "scope", middleware.ByUser())` | `middleware/` |
| `roleassign` | 角色分配三道校验（可分配/权限不越权/数据范围不更宽） | 参考 `modules/system/user/roleguard.go` | `roleassign/` |
| `mcp` | MCP 工具入口（JSON-RPC，只读直调、写走确认流程） | 只读工具自动发现；见功能清单 MCP 节 | `mcp/` |
| `cluster` | 多实例广播总线（redis Pub/Sub）与 node_id 租约 | 租户修改失效广播、job 去重 | `cluster/` |
| `ws` | WebSocket 入口（`/ws`，浏览器先用 `POST /api/v1/ws/ticket` 换一次性入场票，访问令牌不进握手 URL；主题归属由各模块登记的 TopicGuard 判定） | 前端实时通道 | `ws/` |

## 业务支撑

| 包 | 职责 | 典型用法 | 代表文件 |
| --- | --- | --- | --- |
| `event` | 进程内事件总线（user.created 等目录见功能清单） | `event.Subscribe(event.SubscriberFunc(func(e event.Event){...}))`，写在模块 Register 里 | `event/` |
| `job` | 异步任务（导出中心等）：注册 handler、提交、进度 | `job.Register("x.y", h)`；`job.Default().Submit(ctx, ...)` | `job/` |
| `notify` | 统一通知（站内/邮件/短信/机器人），nil 安全 | `d.Notify.Send(...)`（`d.Notify` 可能为 nil） | `notify/` |
| `storage` | 本地 / S3 存储、tus 断点续传 | 文件模块已封装；直接用较少 | `storage/` |
| `i18n` | 后端文案（config/locales/*.yaml，key 缺失原样返回） | `response.BadRequest("biz.key")` 的 key 写在这里 | `i18n/` |
| `response` | 统一响应体 `OK/Page/Fail` 与业务错误 | `response.OK(c, data)`、`response.BadRequest("biz.title_empty")` | `response/` |
| `seed` | 出厂数据：按业务键"不存在才插入"，升级不覆盖 | 模块实现 `setup.Seeder`，写菜单/字典/参数种子 | `seed/seed.go` |
| `ai` | OpenAI 兼容网关：filter/translate/write + 租户配额 | `POST /ai/*`（前端入口在公告表单） | `ai/` |

## 观测与工具

| 包 | 职责 | 典型用法 | 代表文件 |
| --- | --- | --- | --- |
| `metrics` | Prometheus 指标（`/metrics`，token/回环保护） | `metrics.enabled` / `metrics.token` 配置 | `metrics/` |
| `apidoc` | 运行时接口文档（OpenAPI 3.0.3 + 路由清单），自动跟踪模块路由 | `GET /openapi.json` | `apidoc/` |
| `migrate` | 迁移运行器（checksum、锁、baseline、mark-applied） | `nexadmin-server migrate` | `migrate/` |
| `openapi` | 开放平台：PAT、应用签名、Webhook、事件 | 见功能清单 §3 | `openapi/` |
| `plugin` | 插件管理器：安装/卸载/授权闸门/热挂载/市场下载 | `nexadmin plugin ...` | `plugin/` |
| `cache` | 缓存抽象（memory / redis）+ GetDel/Incr/Lease 原子原语 | `d.Cache` | `cache/` |
| `testkit` `testdata` | 测试辅助（构造 App/固件） | 各 `*_test.go` | `testkit/` |
| `health`（health.go） | `/healthz` 与健康详情（db/cache/storage/migrations/plugins/disk） | `GET /healthz`、`GET /monitor/health` | `health.go`、`health_probes.go` |

## 常见"我想…"速查

| 我想… | 去哪 |
| --- | --- |
| 给接口加权限 | `core.Perm("module:action")`（auth 包登记） |
| 写操作日志 | `d.OperLogMiddleware(module, action)` 挂到路由（core/operlog.go，无需关心注册时序） |
| 发通知 | `d.Notify`（站内/邮件/短信/机器人） |
| 加异步任务 | `job.Register` + `job.Default().Submit` |
| 订阅事件 | `event.Subscribe(...)` |
| 按部门裁剪数据 | `nexadmin/core/datascope` |
| 限制某接口频率 | `middleware.RateLimitRule(...)` |
| 加一个后台菜单/字典 | 模块实现 `setup.Seeder`（业务键幂等） |
| 定制租户解析/登录提供者 | `internal/boot/boot.go` 返回 Option（WithTenantResolver / WithAuth） |
