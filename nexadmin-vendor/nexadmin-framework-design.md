# NexAdmin 框架目录设计

> 面向独立部署的 Gin + React + shadcn Admin 后台 · 下载源码二次开发模式

## 实现状态（以代码为准，核对日期 2026-10-23）

各章正文已按现状修订；这里是汇总。证据与细节见 `nexadmin/docs/` 下的《功能与权限清单》《部署与运维指南》《配置参考》；插件源码同步、注册与交付阻塞见 [交付核对清单](nexadmin-vendor/交付核对清单.md)。

2026-10-23 较早阶段的历史检查：修复 identity 目录同步的调用者 ID 获取，并按主项目同步 identity、assistant 的独立包生产后端、前端及清单；当时全项目构建与前端 typecheck 通过，生产文件比较无差异，但未运行测试。随后同日收尾记录已记载 `go test ./...` 全部通过，identity 后端（含测试与三引擎迁移）、完整前端及清单逐文件一致、双向文件集合一致，详见交付核对清单。上述均为已有记录，本次仅核对代码并修正文档，未重新执行测试、构建或同步比较；SQL、浏览器、真实身份源及部署验收仍未完成。行政区划正式数据的来源、年份与再分发授权属于外部阻塞，保留现有数据，不以脚本存在或公开可访问替代授权确认。

- **已实现**：所有权分区与 `nexadmin new / add module / i18n add|check / upgrade --from [--to] / release check|pack`（含合并区、产品清单二层升级）；多租户（含额度与到期强制）；三引擎迁移与自研迁移运行器；缓存 memory / redis；认证（JWT 双 token、验证码、锁定、手机号、六种第三方登录、忘记密码）；SSO（OIDC Provider）；限流；开放平台（令牌、应用签名、事件、Webhook、接口文档）；文件（自研 tus、本地 / S3）；xlsx；导出中心；数据库备份与命令行恢复；通知渠道（站内 / 邮件 / 短信 / 机器人）及发送日志（`sendlog` 模块：投递与验证码短信均记一条，收件人脱敏，不存验证码与正文，异步写入，支持按天清理）；AI 网关（筛选、翻译、写作辅助；租户级开关与每日配额、敏感字段脱敏）；监控（服务 / 缓存 / 接口 / 健康 / 前端错误 / metrics）；PWA；插件体系（§19）；初始化向导、`init`、`reinit`、`ctl` 脚本。
- **部分实现或待数据**：`/healthz` 探数据库、缓存和存储（迁移等详情在 `/monitor/health`）；质量闸（已有 `ci.yml`、`derive.yml`、`security.yml`，完整漏洞扫描需在有网 CI 环境运行）；行政区划历史映射的数据文件目前为空，待可靠数据源确认后导入。初始化向导已补六步配置、远程令牌输入与 SSE 真实进度，仍需真实初始化验收。
- **未实现且不计划**（含设计原文有承诺、无代码也未排期的项，待产品确认保留或删除）：`assets/ownership-upgrade.svg`；Playwright E2E；以及“明确不做”清单里的各项。`govulncheck`、`gitleaks` 已接入 `security.yml`，但本地网络受限，不能宣称扫描已完成。
- **已实现但需真实环境验收**：租户容量限制（含套餐继承、租户覆盖、原子预占与释放）；通用 `core.Sort`；`core/testkit`；`nexadmin upgrade --to` 的本地升级包版本校验；SCIM 用户/组同步付费插件；`nexadmin i18n add --translate`；多语言在线编辑付费插件（含三数据库迁移、启动恢复、回滚、运行时覆盖和跨实例失效广播）。
- **计划中**（设计原文已标 M2 / 后续）：街道级区划正式数据。
- **已有机制、待真实验收**：按租户独立业务库路由、迁移 gating 和定向迁移、平台及全部配置租户的 bundle 备份恢复；认证、组织等控制面仍保留平台库，不代表存量数据已自动拆分。MCP 已有共享缓存会话、最终单条 SSE 封装与显式写工具确认。assistant 命名会话的主项目与独立包后端均为 1.1.0，模型及三引擎 SQL 使用 `plg_assistant_conversation`；实体 ID 为 `string`，由 `newID()` 使用 `crypto/rand` 生成 16 字节并编码为 32 位小写十六进制字符串，迁移主键为 `CHAR(32)`，入口由 `validConversationID` 校验格式。该会话主键不使用 `pkg/idgen` 或 Snowflake。独立包前端命名会话 API 已同步，已有生产文件比较记录无差异。SQL 未执行，持久化真实验收未做。

## 1. 背景与目标

- 技术栈：Gin（后端）+ React + shadcn/ui（前端）
- 能力要求：多租户、国际化（i18n）、天生移动端、SEO 友好
- 选型要求：开源社区 star 多的优先，全部依赖不触及商业授权，清单见 §13
- 人群要求：熟悉 Gin 的人简单容易上手，不过度设计
- 交付模式：客户把框架**下载到本地**，在框架上做二次开发
- 升级要求：框架发新版本后，客户可以低成本跟随升级

## 2. 核心决策：交付源码，不做依赖

私有化部署场景（信创、等保、审计）通常强制要求客户拿到**全部源码**，且不依赖框架方的 module 发布渠道。因此 NexAdmin 以**完整可运行的源码仓库**形式交付，客户 clone/fork 到本地直接开发。

下载源码模式只有一个真问题：**升级冲突**。所以目录设计的核心任务只有一件事——

> 把"框架的文件"和"用户的文件"在磁盘上物理分开，让升级可以做到"覆盖框架区、保留用户区"。

## 3. 设计原则

1. **一个业务模块 = 一个目录 = 一个 `Module` 接口**。熟悉 Gin 的人看 `module.go` 就能上手，不搞插件容器、DDD 分层。
2. **横切能力全部沉到中间件和 context**：多租户、i18n、鉴权、统一响应，业务 handler 零感知。
3. **约定优于配置**：路由前缀、目录名、文件名全部固定，CLI 负责生成和升级。
4. **新手只需记住五件事**：① 业务代码只放 `internal/modules`、`web/src/pages`、`migrations/business`；② 模块就是一个 `Module`（`Name` + `Register`），路由用原生 Gin；③ 模型嵌入 `TenantModel`；④ 权限用 `core.Perm("模块:动作")` 中间件；⑤ 不改 `core/` 和 `modules/`，要深改就复制到 `internal/`。其余（雪花 ID、多语言、主题、移动端骨架）都由框架和 `nexadmin add module` 生成的代码带着走。

## 4. 目录结构

框架与业务同仓，按目录划分所有权：

```
myapp/                            # 用户本地项目 = 框架源码 + 业务，同一个 git 仓库
├── go.mod  go.sum
├── cmd/server/main.go            # 入口（框架区）：装配 core + 内置子命令分发（start/stop/init/migrate/restore/reinit…），不需要改
├── cmd/nexadmin/                 # 命令行工具：plugin / upgrade / new / add module / i18n / release
├── core/                         # ── 框架区 · 升级时覆盖 ──
│   ├── app.go  boot.go  router.go  health.go  static.go   # health.go + health_probes.go = /healthz（探数据库、缓存、存储）；static.go 托管内嵌前端
│   ├── middleware/               #   cors / auth / tenant / i18n / logger / ratelimit
│   ├── tenant/  i18n/  auth/     #   多租户（含额度与到期强制）、国际化、鉴权
│   ├── cache/                    #   memory / redis 两个实现，配置切换
│   ├── ai/                       #   AI 网关（OpenAI 兼容协议），见 §16
│   ├── setup/                    #   初始化向导后端，见 §17
│   ├── ctl/                      #   start/stop/status、reinit、备份恢复的命令行实现，见 §17
│   ├── migrate/  seed/           #   迁移运行器（sys_migration、checksum、锁、基线）、出厂数据幂等种入
│   ├── region/                   #   省市区数据（内置，go:embed）+ 接口，见 §18
│   ├── notify/                   #   Notifier 接口：站内 / 邮件 / 短信 / 机器人渠道，见 §12
│   ├── job/  event/  openapi/    #   异步任务、进程内事件总线、开放平台鉴权（令牌 / 应用签名）
│   ├── apidoc/  metrics/  secret/   #   运行时接口文档、/metrics、静态加密（AES-GCM）
│   ├── datascope/  roleassign/   #   数据范围解析、角色可分配三道校验
│   ├── plugin/                   #   插件管理与授权校验，见 §19
│   ├── testkit/                  #   测试上下文、平台上下文与 memory cache 助手
│   └── response/  database/  storage/
├── pkg/                          # ── 框架区 · 能力件：无路由、无表 ──
│   └── sms/  mail/  robot/  excel/  htmlsafe/  oauth/  idgen/  apistat/  vrange/   # idgen = 雪花 ID；不得出现 GORM model 或 Register(r)
├── modules/                      # ── 框架区 · 可停用，升级会被覆盖 ──
│   ├── system/                   #   user role menu dept post dict config tenant backup
│   ├── notice/  message/  log/  online/  profile/  file/  dashboard/  recycle/  region/  about/  ai/
│   ├── openapi/  sso/  plugin/  exportcenter/  servermonitor/  cachemonitor/  apimonitor/  clienterror/
│   └── （框架模块由 core 自动注册，不需要改 main.go；完整清单见 §12）
├── internal/                     # ── 用户区 · 升级时原样保留 ──
│   ├── boot/                     #   二开定制入口：Options() 返回追加给 core.New 的 Option
│   └── modules/                  #   ★ 业务模块，一个目录一个模块
│       └── article/{module,model,service,handler,dto}.go
├── plugins/                      # ── 用户区 · 已安装的源码插件（§19）──
├── config/
│   ├── default.yaml              # ── 框架区 · 全部默认值，随升级覆盖 ──
│   ├── config.yaml               # ── 用户区 · 只写要改的项 ──
│   └── locales/                  # ── 用户区 · 业务翻译，覆盖框架同名 key ──
├── migrations/
│   ├── framework/                # ── 框架区：随框架覆盖 ──
│   └── business/                 # ── 用户区：自己的 SQL ──
├── web/src/
│   ├── framework/                # ── 框架区：布局/路由壳/请求封装/ui/主题 token ──
│   ├── theme.css                 # ── 用户区：品牌色等主题变量覆盖 ──
│   ├── pages/                    # ── 用户区：自己的页面 ──
│   └── locales/                  # ── 用户区：业务语言包 ──
├── docs/                         # ── 框架区：使用文档 ──
│   └── 部署与运维指南.md  配置参考.md  功能与权限清单.md  二次开发指南.md  plugin-development.md
│                                 #   （原设计的 engines.md 三引擎矩阵文档未提供，矩阵见 §7）
├── deploy/                       # ── 框架区：一键脚本 ctl / reinit（.bat / .command / .sh）+ Dockerfile / compose 样例 / systemd 单元 ──
├── scripts/                      # ── 框架区：build.sh / build.bat（derive_check、contract_check、gen_icons 仅框架仓库）──
├── data/  logs/                  # ── 用户区 · 运行时数据（初始化标记、上传、data/run/nexadmin.pid、密钥），不入 git ──
├── .nexadmin.manifest.json       # 记录路径归属 + 当前框架版本
└── README.md                     # 写明"只往用户区加代码"
```

### 启动代码（用户需要写的全部"框架级"代码）

```go
// cmd/server/main.go（框架区，节选；完整文件还有子命令分发，业务代码不需要改它）
func newApp(cfg *config.Config) *core.App {
    app := core.New(cfg, append([]core.Option{core.WithStatic(web.Dist())}, boot.Options()...)...)
    plugin.Mount(app, plugin.Options{FrameworkVersion: version})   // 评估插件授权，有效的挂载
    return app                                                      // 业务模块在各自包的 init() 里注册，main.go 只空导入汇总包
}
```

业务模块不在 `main.go` 里注册：每个模块在自己包的 `init()` 里调用 `core.RegisterBuiltin(module{})`，再由 `internal/modules/modules.go` 空导入（`nexadmin add module` 自动维护标记行之间的导入）。需要传给 `core.New` 的定制（租户解析、域名映射、登录提供者）写在用户区 `internal/boot/boot.go` 的 `Options()` 里。服务监听 `:8080`（`server.port`）。

不想要某个内置模块，在 `config/config.yaml` 写 `disabled_modules: [post, notice]`，不删代码、升级也不会把它"复活"。

```go
// internal/modules/article/module.go —— 框架的唯一扩展点
package article

func (Module) Name() string { return "article" }

func (m Module) Register(r *gin.RouterGroup, d *core.Deps) {
    h := NewHandler(NewService(d.DB))
    r.GET("/articles", core.Perm("article:list"), h.List)       // 最终路由 /api/v1/articles
    r.POST("/articles", core.Perm("article:create"), h.Create)  // core.Perm 是普通 Gin 中间件：鉴权 + 登记权限点
    r.PUT("/articles/:id", core.Perm("article:update"), h.Update)
}
```

（目录所有权分区与升级覆盖图未提供，以上文目录树和分区说明为准。）

## 5. 所有权契约与升级机制

`.nexadmin.manifest.json` 是所有权契约，`nexadmin upgrade` 照它办事：

```json
{
  "framework_version": "1.3.0",
  "framework_paths": ["core/", "modules/", "pkg/", "docs/", "deploy/", "cmd/", "web/src/framework/", "web/src/App.tsx", "web/src/main.tsx", "web/src/index.css", "web/src/lib/", "web/src/vite-env.d.ts", "web/index.html", "web/public/", "web/.env.production", "web/.env.development", "web/vite.config.ts", "web/tailwind.config.ts", "web/postcss.config.js", "web/tsconfig.json", "web/eslint.config.js", "web/embed.go", "web/embed_dev.go", "scripts/", "migrations/framework/", "config/default.yaml", "config/license-keys.pub", "config/license-revoked.json"],
  "merge_paths": ["go.mod", "go.sum", "web/package.json"],
  "user_paths": ["internal/", "plugins/", "web/src/plugins/", "web/src/pages/", "web/src/locales/", "web/src/theme.css", "config/config.yaml", "config/locales/", "migrations/business/", "data/", ".dev-keys/", ".github/", "AGENTS.md", "CLAUDE.md", "README.md", ".gitignore", ".dockerignore", "web/package-lock.json"]
}
```

三类所有权，缺一不可（只有"覆盖"和"永不触碰"两类时，框架新增依赖、`main.go` 装配新选项、PWA、错误上报入口这类改动永远到不了二开项目，升级后编译或运行必坏）：

- **覆盖（`framework_paths`）**：框架拥有，按新包整体覆盖；手改过的文件先存 `.bak` 并在清单里 WARN，包里已删除的文件移入 `.nexadmin-trash/`。含 `cmd/`、前端构建与入口文件（`web/index.html`、`web/public/`、`vite.config.ts`、`App.tsx`、`main.tsx`、`index.css`、`lib/` 等）和整个 `scripts/`（`scripts/derive_check`、`scripts/contract_check`、`scripts/gen_icons` 仅框架仓库使用，`new` 与打包都排除）。支持 `*` 通配（不跨目录，如 `web/tsconfig.*.json`）。
- **合并（`merge_paths`）**：双方都有改动，合并而不是覆盖，只支持 `go.mod`、`go.sum`、`web/package.json`。`go.mod`：module 行以当前工程为准，go 版本取较高者；`require` 新包有而当前没有的加入，两边都有取较高 semver，当前独有的（二开自己加的依赖）原样保留；`replace`/`exclude` 以当前工程为准，只补入新包里框架必需的模块型 `replace`；`// indirect` 不强求，升级后运行 `go mod tidy`。`go.sum`：两边行取并集，去重排序。`package.json`：`dependencies`/`devDependencies`/`scripts`/`engines` 等以新包条目覆盖同名条目，当前独有的依赖和脚本保留，`name`/`version`/`private` 等标识字段以当前工程为准；键序稳定、缩进 2 空格。合并结果与原文件相同则不写；`--dry-run` 逐项列出新增/升级/保留。升级后提示运行 `go mod tidy` 与 `(cd web && npm install)`。
- **永不触碰（`user_paths`）**：二开项目拥有，升级包里的同名文件一律忽略。除业务代码外还含 `.github/`、`AGENTS.md`、`CLAUDE.md`、`README.md`、`.gitignore`、`.dockerignore` 和 `web/package-lock.json`（锁文件不覆盖，合并 `package.json` 后由用户运行 `npm install` 重新生成）。

同时在多类中出现的路径，优先级为：永不触碰 > 合并 > 覆盖。

框架发版用 `nexadmin release pack [--out nexadmin-x.y.z.zip]` 生成干净的源码包（根目录 `nexadmin-<framework_version>/`，排除集合与 `nexadmin new` 完全一致，`release check` 有 FAIL 则拒绝打包），`nexadmin upgrade --from` 可直接使用该 zip；`--to <版本>` 可在执行前校验框架包版本。

升级行为：

```
$ nexadmin upgrade --from ../nexadmin-0.2.0.zip --to 0.2.0      # 本地源码包（目录或 zip，如 release pack 的产物）；先加 --dry-run 只看清单
框架升级 -> 0.2.0
  M  core/health.go
  A  docs/配置参考.md
  D  core/xxx.go  (框架已删除，移入 .nexadmin-trash/<时间>/)
摘要: 新增 1，修改 1，删除 1，未变 955，WARN 0
升级完成: framework_version 已更新为 0.2.0。迁移由服务启动时的 runner 执行；用 git diff 查看变更。
```

升级前会检查 git 工作区：有未提交改动拒绝（`--allow-dirty` 可跳过；`--dry-run` 只警告），不是 git 仓库则警告“无法用 git 回滚”。手改过的框架文件覆盖前存 `.bak` 并计入 WARN。不满意用 `git checkout .` 回滚。新增的框架迁移不是升级命令执行的，而是服务下次启动（`app.auto_migrate=true`）或 `nexadmin-server migrate` 时由迁移运行器执行。

**升级只需三步**：① `git commit` 当前改动 → ② `nexadmin upgrade --from <新版本源码包> [--to <版本>]` → ③ 看 `git diff`，跑一遍项目自己的构建。想先看影响，加 `--dry-run`，只列出会变的文件，不改任何东西（待执行的迁移用 `nexadmin-server migrate --dry-run` 查看）。

**升级易用性承诺**：
- 一个主版本内，`Module` 接口、`core.Perm`、Option、路由前缀、配置键、响应结构**不破坏**；要废弃的先标记 deprecated，至少保留一个次版本再删。
- 升级只改框架区，业务代码零改动；每次发版附一页"升级说明"，没有需要手动处理的事就写"无"。
- 破坏性变更只出现在大版本，并随版本提供迁移说明。

三条升级路线，按客户习惯自选：

| 路线     | 命令                                 | 说明                            |
| ------ | ---------------------------------- | ----------------------------- |
| CLI 覆盖 | `nexadmin upgrade --from <源码包目录或zip> [--to <版本>]` | 拿到源码包（目录或 zip），按 manifest 覆盖框架区；`--to` 校验框架包版本与操作者预期一致，不联网拉取 |
| git 合并 | `git merge v1.3.0`（upstream 为框架仓库） | 冲突只可能落在框架区，业务区零冲突             |
| 补丁     | 官方发布 v1.2→v1.3 diff，`git apply`    | 适合内网隔离环境                      |

合并策略：

- **配置**：框架默认值在 `config/default.yaml`（随升级覆盖），用户只在 `config/config.yaml` 写覆盖项。升级不碰用户文件，不需要任何合并逻辑。
- **migrations**：框架迁移在 `migrations/framework/`（随框架覆盖），客户迁移在 `migrations/business/`（永久保留）；引擎差异 DDL 按 `migrations/framework/{mysql,mariadb,postgres}/` 分目录，runner 按当前驱动选文件。两类迁移分别记录版本（版本表 `sys_migration` 的 scope：`framework` / `business` / `plugin:<id>`），互不干扰。设计规则是迁移里不写 INSERT/UPDATE/DELETE 与数据回填；运行器对违反的文件只给 WARN，不阻断。
- **出厂数据**（默认菜单、字典、参数、权限点）：由各模块启动时幂等种入，每行带稳定字符串业务键（如菜单用权限码 `user:list`、目录用 `dir:<路径>`，字典 `type+value`，参数 `key`），按业务键匹配，不用主键 id（雪花 ID 每个环境各自生成）。规则只有一条：**不存在才插入，已存在一律不动**——客户改过的行永远不会被升级覆盖。代价要说明：新版本修改了某条出厂数据时，存量库不会自动跟着变，需要管理员在后台手改。
- **手改框架区的文件**：`upgrade` 不静默覆盖，改过的文件存 `.bak` 并在变更清单中警告，让人工 review。允许改，但让代价可见。
- **工作区不干净时拒绝升级**：分不清"客户改了框架文件"和"只是没 commit"，所以要求先 commit 或 stash。执行期间的文件系统错误由 `upgrade_transaction.go` 尝试恢复；成功升级后的人工回退仍需保留 git 基线，数据库迁移不在文件恢复范围内。

### 二开产品的二层升级

二开团队（BI、数字孪生等）在框架上做成产品再卖给自己的客户，升级分两次：**框架 → 产品团队**（`nexadmin upgrade --from 框架包`），**产品 → 终端客户**（`nexadmin upgrade --from 产品包 --manifest .product.manifest.json`）。两次用同一套逻辑：覆盖清单声明的路径、用户区不动、手改文件存 `.bak`、框架删除的文件移入 `.nexadmin-trash/`。

`.product.manifest.json` 与框架清单同构，字段为：

```json
{ "product_name": "nexbi", "product_version": "2.1.0", "based_on_framework": ">=1.3.0 <2.0.0",
  "product_paths": ["modules/bi/", "web/src/bi/"], "user_paths": ["internal/", "plugins/", "config/config.yaml"] }
```

- `based_on_framework` 是产品兼容的框架版本范围；客户当前 `framework_version` 不在范围内时，`upgrade` 拒绝并说明，需先升级框架或换兼容的产品包。
- 升级后哈希记录写入 `.nexadmin.files.json`（属于用户区），两层共用一份，各自只管自己清单声明的路径。

二开团队规则：
1. 不改 `core/`、`framework/`、`modules/`（框架区）；要改行为走扩展点，扩展代码放用户区（`internal/`、`plugins/`、`web/src/plugins/`）或自己的 `product_paths`。
2. 产品包里的 `product_paths` 不得与框架的 `framework_paths` 重叠。
3. 以框架仓库为 git upstream 合并新版本，冲突只可能出现在被改过的框架区文件，这本身就是违规信号。
4. 每次发产品版先在目标框架版本上跑 `--dry-run` 与自己的构建，再更新 `based_on_framework`。

## 6. 军规（写进 README，违反任何一条升级都会疼）

1. 用户永远只往 `internal/modules`、`web/src/pages`、`migrations/business` 里加代码，配置只写 `config/config.yaml`。
2. `core/` 不允许改。要定制走 Option：`core.New(core.WithTenantResolver(...), core.WithAuth(...))`——不改也能定制，用户就没有改 core 的动机，这是升级无痛的前提。
3. 内置模块（`modules/`）也不改。要深改某个模块：把它整个复制到 `internal/modules/` 改名，在 `disabled_modules` 里停用原模块。代价是这个模块此后不再随框架升级，这个代价是可见且可接受的。
4. 业务表不得使用 `sys_` 前缀，该前缀归框架。
5. `Module` 接口和路由约定一个主版本内不破坏；破坏性变更放大版本号并附迁移说明。
6. 客户万一真改了框架区文件：CLI 存 `.bak` 并警告，不静默覆盖。

## 7. 横切能力落点

### 多租户

- 解析顺序：域名绑定（租户的 `domain` 字段，按请求 Host 精确匹配）> 请求头 `X-Tenant-ID` > JWT claim > 默认租户，结果注入 context，`tenant.From(ctx)` 读取。已登录的非超管用户，解析结果必须与令牌里的租户一致，否则 403。
- 数据隔离：默认共享数据库 + `tenant_id` 列，GORM scope 自动追加 `WHERE tenant_id = ?`。可选 `database.isolation` 已实现按显式业务表清单和服务器配置的租户连接池路由；认证、组织、运营等控制面表仍在平台库。未知表、不能证明单库边界的原生 SQL / join 及跨库操作拒绝，不自动搬迁存量数据。已初始化服务启动的租户自动迁移须 `app.auto_migrate` 与 `database.isolation.auto_migrate` 同时开启（后者默认 false），否则检查待迁移并拒绝未就绪启动；`migrate --tenant platform|<ID>` 可定向处理。SQL bundle 顺序备份恢复全部配置库，非原子快照、不自动回滚；隔离模式旧 `restore/reinit` 须明确 `--platform-only`，其中 reinit 仍清理共享资源。配置和操作边界见配置参考及部署指南，真实多库验收未完成。
- 超管端 `/api/v1/admin` 路由组跳过租户隔离。
- 缓存 key（`nex:t<租户ID>:...`）、文件存储路径（`<租户ID>/<日期>/<文件ID>.<扩展名>`）自动带租户前缀。业务代码完全无感。
- 租户额度（用户数 / 角色数 / 存储）与到期（含宽限期）由 core 强制执行，见 §12「租户额度与到期」。
- 平台级表（菜单/字典/参数/权限标识）是跨租户共享的事实，不进租户 scope。豁免必须在模型上显式登记（嵌入 `PlatformModel` 而非 `TenantModel`），框架 CI 只扫描框架自己的模型；客户模型由 `nexadmin add module` 默认生成嵌入 `TenantModel` 的骨架，不额外设闸。清单见 §12。

### 数据库引擎（三引擎矩阵）

支持矩阵写死，不浮动：

| 引擎         | 版本    | 说明                 |
| ---------- | ----- | ------------------ |
| MySQL      | 8.4   | 主目标                |
| MariaDB    | 10.11 | 信创常见替代，复用 MySQL 驱动 |
| PostgreSQL | 16    | PG 路线客户            |

三引擎等价是约束，不是口号：

- 框架迁移只用三引擎共有 SQL 子集：禁 `ENGINE=`、禁 `IF NOT EXISTS`、禁方言 `COMMENT`、禁原生 `ENUM`、禁依赖引擎语义的 `CHECK`。
- 确需引擎差异的 DDL 时，按 `migrations/framework/{mysql,mariadb,postgres}/` 分目录存，runner 按当前驱动选文件执行。
- 类型映射矩阵（单一事实源就是本节；原设计的 `docs/engines.md` 未提供）：

| 逻辑类型  | MySQL 8.4 / MariaDB 10.11 | PostgreSQL 16                       |
| ----- | ------------------------- | ----------------------------------- |
| 主键（雪花 ID） | BIGINT（应用侧生成）         | BIGINT（应用侧生成）                       |
| 短字符串  | VARCHAR(n)                | VARCHAR(n)                          |
| 长文本   | TEXT                      | TEXT                                |
| 布尔    | TINYINT(1)                | BOOLEAN                             |
| 时间    | DATETIME(3)，统一存 UTC       | TIMESTAMP(3)，统一存 UTC                |
| JSON  | JSON                      | JSONB                               |
| 枚举    | VARCHAR + 应用层校验           | VARCHAR + 应用层校验                     |

- 验收：框架仓库 CI 起三个服务容器（mysql:8.4 / mariadb:10.11 / postgres:16），迁移与启动冒烟各跑一遍；客户项目只允许使用声明的三个引擎之一。
- 主键规则：框架与客户业务表的通用基座及骨架生成器采用**雪花 ID**，`BIGINT` 类型，由应用侧 `pkg/idgen` 在 GORM `BeforeCreate` 钩子里生成，表上不设自增、不用 `IDENTITY`，三引擎迁移因此完全一致。插件自定义模型须以实际实现为准：assistant 的 `plg_assistant_conversation.id` 使用 `CHAR(32)` 随机字符串，不适用此通用主键规则。
  - 实现自带约 50 行，不引第三方依赖：41 位毫秒时间戳 + 10 位节点号 + 12 位序列号（纪元 2024-01-01 UTC）。
  - **节点号**在 `config` 里配置（`database.node_id`，0–1023，默认 0），多实例部署每个实例必须不同；启动时校验范围，redis 缓存下还会用带 TTL 的租约（`core/cluster`，30 秒、每 10 秒续约）检测重复，被别的存活实例占用就拒绝启动；`memory` 缓存（单实例）下不检测，也不会因为未配置而报错。
  - **时钟回拨**：回拨在容忍窗口（默认 5ms）内等待，超出则拒绝生成并报错，不产生重复 ID。
  - **前端精度**：雪花 ID 超过 JS 的安全整数范围（2^53），**后端 JSON 一律把 ID 序列化为字符串**，`core/response` 统一处理，业务 handler 不用关心；前端 ID 当字符串用，不做数值运算。
  - 雪花 ID 每个环境各自生成，不用于跨版本匹配；出厂数据用稳定业务键（见 §5）。
- 树形表（`sys_dept`）：`parent_id` + 物化路径 `path` 双写。"本部门及以下" = `path LIKE '/1/4/%'`，三引擎行为一致；框架迁移和热路径查询里不出现 `WITH RECURSIVE`。移动节点要重写子树 path（低频，需单测覆盖）。

### 缓存

`core/cache` 定义一个接口，提供 `memory` 和 `redis` 两个实现，由配置切换。单机部署默认 memory，零中间件；多实例部署配 Valkey（BSD-3，兼容 Redis 协议；不使用 Redis 7.4 及以上，授权原因见 §13）。在线用户、验证码、令牌黑名单都走这个接口。可选能力以独立接口提供，不往 `Cache` 里加方法（避免破坏外部实现者）：`cache.Counter`（原子计数，限流用）与 `cache.AtomicGetDel`（原子取出并删除；一次性凭据的消费走辅助函数 `cache.GetDel`，实现方不支持时回落 Get+Del 并记一次 warn）。启动时若配置要求的实现不可用，报错要指名原因，不 panic。

### 国际化

- 后端 `T(ctx, "user.not_found")` 取文案；locales 两层合并：框架 `core/i18n/locales` 打底，`config/locales` 同名 key 覆盖。
- 前端 react-i18next，同样两层语言包。
- Admin 用 `Accept-Language` + 用户设置协商语言，URL 不带语言前缀（那是公开站的做法）。
- **加一门语言 = 一行配置 + 翻译文件，组件代码零改动**：
  1. `config/config.yaml` 的 `languages` 加一行：`{code: fr-FR, name: Français, dir: ltr}`（`code` 统一 BCP-47，`dir` 为 ltr / rtl）。
  2. 放翻译文件：后端 `config/locales/fr-FR.yaml`，前端 `web/src/locales/fr-FR.json`。前端用 `import.meta.glob` 自动发现，不需要注册。
  - 一条命令完成：`nexadmin i18n add fr-FR` 生成配置行和以 en-US 为底的空壳翻译文件（带 `_untranslated` 标记，翻译后删除）；追加 `--translate` 时调用内置 AI 网关生成初稿，未配置 AI 或模型返回非法结果会失败且不写入半成品。
- **缺翻译不报错**：按 当前语言 → 默认语言 → key 本身 逐级回退，新语言可以只翻一部分就上线。`nexadmin i18n check` 只列出缺失 key，不作为发版闸。
- **语言清单只有一个来源**：语言切换器、`html[lang]`、`html[dir]`、`Intl` 格式化（日期/数字/货币）、后端语言协商都读 `languages` 配置（前端经 `/api/v1/i18n/languages` 获取），代码里不硬编码任何语言。
- **RTL**：样式一律用 Tailwind 逻辑类（`ms-` / `me-` / `ps-` / `pe-` / `text-start`），不用 `ml-` / `mr-` / `text-left` 等物理方向类，这样加阿拉伯语、希伯来语只需 `dir: rtl`。
- **菜单名、字典标签**：存 i18n key（如 `menu.system.user`）或直接存纯文本；是 key 就翻译，是纯文本就原样显示，客户自建菜单不需要为每门语言配翻译。

### 移动端

- 一套 React 代码，Tailwind 断点：`<md` 侧边栏收成抽屉、数据表格降级为卡片列表。不为移动端写第二套前端。

### SEO（需要点破的矛盾）

- 登录墙内的后台对爬虫没有任何价值，给 SPA 硬上 SSR 是纯负资产。
- 真正需要 SEO 的是公开页（官网/文档/分享页/H5）：**属另仓、不进框架包**，用 Next.js/Remix（SSR/SSG）独立开发和部署，与本框架的所有权契约无关。
- 如果产品是纯后台，建议直接划掉 SEO 需求。

## 8. 前端结构

```
web/
├── src/framework/                # 框架区：布局/路由壳/请求封装/权限指令/ui
├── src/pages/                    # 用户区：业务页面
├── src/locales/                  # 用户区：业务语言包
└── package.json                  # 升级时只增依赖不改值
```

升级 = 覆盖 `framework/` 目录 + `npm install` 合入新依赖，与后端同一套所有权契约。

### 移动端：靠"没法不适配"，不靠"记得适配"

做法是把响应式封进框架组件，页面只能用这些组件拼，写页面的人（包括 AI）不需要想断点：

1. **移动优先**：Tailwind 默认样式就是手机样式，`md:`（768px）以上才加桌面样式。
2. **框架提供页面骨架，页面只拼不造**（都在 `web/src/framework/`）：
   - `AppLayout`：`<md` 侧边栏收成抽屉 + 顶栏；`≥md` 固定侧边栏。
   - `DataTable`：`<md` 自动渲染成卡片列表，列定义里用 `mobile: 'title' | 'sub' | 'hidden'` 标注显示位置；搜索区在手机上折叠成筛选抽屉；`≥md` 是普通表格。
   - `FormLayout`：`<md` 单列，`≥md` 双列；输入框字号不小于 16px（防 iOS 自动放大）。
   - `ResponsiveDialog`：`≥md` 是弹窗，`<md` 是底部抽屉（shadcn Drawer）。
   - `Button`、输入框等：手机上触控区域不小于 44px。
   - 全局：viewport meta、`100dvh`、`env(safe-area-inset-*)` 刘海适配。
3. **提醒绕过**：ESLint `no-restricted-imports` 设为**警告**（不阻断构建）：`pages/` 下直接引用 shadcn 的 `table`、`dialog` 等原语时，提示改用框架组件。只检查改动的文件，几乎零成本。
4. **一条机器判据**：发版前那条 E2E 冒烟（§15）用 375px 视口再跑一遍，断言 `document.scrollWidth <= window.innerWidth`（页面没有横向溢出）。不新增测试，只多一个视口参数。
5. **写进仓库，不靠记忆**：本节规则同时写进客户项目的 AI 规则文件。多 agent 并行时，每个前端任务的 prompt 必须带上"用框架页面骨架、移动优先"这一句，因为子 agent 没有主会话的上下文，不写进 prompt 就会遗忘。

边界：复杂的宽表、权限树在手机上只能降级为卡片或带横向滚动的容器，保证能看、能操作，但不追求和桌面一样的信息密度。不做第二套前端、不做原生 App；PWA 已实现（`web/public/manifest.webmanifest`、手写的最小 `sw.js`、`offline.html`，只缓存应用壳，不拦截 `/api`、`/oauth`、`/sso`、`/healthz`、`/metrics`；没有使用 `vite-plugin-pwa`）。

### 主题：明暗两套 + 科技感

1. **切换机制**
   - 基于 shadcn/ui 的 CSS 变量，`<html class="dark">` 切换；三档：亮 / 暗 / 跟随系统。选择存 localStorage 并同步到用户设置。
   - `index.html` 里一段内联脚本在首屏渲染前设好 class，避免刷新时闪白；`meta theme-color` 随主题变化。
2. **颜色只能用语义 token**
   - 页面和组件只用 `bg-background`、`text-foreground`、`bg-primary`、`border`、`chart-1` 到 `chart-5` 这类 token，不写死色值，也不用 `bg-blue-500` 这类具体色阶。这样所有页面天然两套主题都对。
   - ESLint 对改动文件里的十六进制色值和色阶类给出**警告**，不阻断构建。
3. **科技感：克制，不满屏特效**
   - 色调：暗色以深蓝黑为底（不是纯黑），亮色用冷调浅灰蓝（不是纯白）；主色走电光蓝到青紫的冷色系。两个主题共用同一套色相，只调明度。
   - 质感：1px 低透明度细边框；暗色下卡片用轻微毛玻璃（`backdrop-blur`）；主按钮、聚焦态、卡片 hover 用主色柔和光晕。
   - 背景：登录页和工作台顶部用极淡的点阵或网格，不铺满整个后台。
   - 数字：统计数字用等宽数字（`tabular-nums`）和 JetBrains Mono，表格数字右对齐，有仪表盘的数据感。
   - 动效：过渡 150 到 200ms，遵守 `prefers-reduced-motion`。
   - 特效只用在登录页、工作台、主按钮、卡片 hover 四处，其余页面保持干净，保证低配机器和手机上流畅。
4. **可读性**：正文与背景的对比度不低于 WCAG AA（4.5:1），两个主题的 token 各验证一次。
5. **客户换品牌色不冲突升级**：框架 token 在 `web/src/framework/theme/tokens.css`（随升级覆盖），客户在用户区 `web/src/theme.css` 只写要改的变量（如 `--primary`），后加载覆盖。图表颜色取自 token，随品牌色联动。
6. **字体自托管**：Inter（西文）+ JetBrains Mono（数字与代码），不走 CDN，因为私有化环境可能没有外网；中文用系统字体栈。

## 9. 与"依赖模式"的关系

框架仓库可同时发布两种形态：**源码包**（本文模式，主推，私有化部署客户认这个）和 Go module 包（适合框架方自建 SaaS 托管）。`internal/modules` 的写法和 `Module` 接口在两种形态下完全一致，业务代码可互相复用。

## 10. CLI 工具

```
nexadmin new myapp              # 从当前版本源码派生一个新项目
nexadmin upgrade --from <源码包> [--to <版本>]  # 按 manifest 覆盖框架区、合并 go.mod / package.json、输出变更清单（工作区不干净则拒绝；--dry-run 只看清单）
nexadmin add module article     # 在 internal/modules 下生成业务模块骨架
nexadmin i18n add fr-FR         # 加一门语言：配置行 + 空壳翻译文件
nexadmin i18n add fr-FR --translate # 使用已配置的 AI 网关生成翻译初稿
nexadmin i18n check             # 列出各语言缺失的 key（仅提示）
nexadmin release check          # 发版前检查（FAIL 清零才能发版；--strict 让 WARN 也算失败）
nexadmin release pack           # 生成干净的源码包 zip
nexadmin plugin list            # 查看源码副本与离线授权，不证明运行时已挂载
nexadmin plugin install [--force] <目录或zip>  # 选项必须放在路径之前；安装后重新编译
nexadmin plugin remove <id>      # 卸载源码，保留数据表与授权文件
# plugin keygen / sign / revoke 的参数见 cmd/nexadmin/main.go，发行方使用
```

## 11. 派生验证 lane（M1，框架仓库 CI）

框架的核心承诺是"下载即可开发、升级无痛"，这条承诺必须由机器证伪，不能靠 README。框架仓库 CI 常驻一条最便宜的 lane：**从当前源码派生一个产品，编译、冒烟，再升级一次**。文档里其他一切承诺（所有权分区、manifest 覆盖、三引擎矩阵）都下游依赖这条 lane——它绿，交付机制成立；它红，其余免谈。

- 产物：框架仓库根的 `scripts/derive_check/run.sh` + `.github/workflows/derive.yml`。**这两项仅存在于框架仓库，不下发给客户**（`nexadmin new` 与源码包都排除它们）。
- 步骤：`nexadmin new demo-product` 派生 → 后端 `go build ./...` → 前端 `npm ci && npm run build` → 起服务探 `/healthz`（postgres:16 service 容器）
- 升级回环：在 demo-product 里加一个业务模块 → `nexadmin upgrade` 到上一个 tag → 再编译冒烟 → 断言 `internal/` 零 diff
- 依赖检查：`modules/*/` 之间互相 `import` 只警告（见 §12 原则 4）
- 定位：只验证框架自身的交付机制，**不施加于客户项目**，客户零负担
- 纪律：此 lane 红 = 框架不许发版。**只在发版前跑，不在每个 PR 跑**（含全量 `go build ./...` 和前端 build，这是整个体系里最重的一步）

## 12. 模板内置模块清单

选型依据：对标主流 admin（RuoYi-Vue-Plus / gin-vue-admin / eladmin / Ant Design Pro / saiadmin）的功能交集，砍掉私有化部署用不到的重型件。四条原则：

1. **覆盖 80% 后台场景**：组织（用户/角色/菜单/部门/岗位）+ 配置（字典/参数）+ 审计（双日志/在线用户）+ 多租户（租户/套餐）+ 门面（工作台图表）。
2. **每个模块都是标准 CRUD**：列表 + 表单 + 详情，不造特殊交互，客户看得懂也删得掉。
3. **可停用**：`modules/` 下每个模块自包含，在 `disabled_modules` 里写上名字即下线。
4. **框架内置模块之间尽量不直接 `import`**（为了能单独停用某个模块）：跨模块优先走 core 的注册式接口，服务未启用时返回明确提示，不 panic、不 500。这是框架内部的约束，框架 CI 对 `modules/*/` 间的 import 只**警告**；**客户的业务模块之间可以直接互相调用**，不受此限。

### M1 必带（删到只剩这些也能跑）

| 模块             | 页面能力                                | 关键表                          | 租户隔离     |
| -------------- | ----------------------------------- | ---------------------------- | -------- |
| 工作台 dashboard  | 卡片化：统计、登录 / 请求趋势、操作占比、待办、快捷入口、最近公告；用户可按个人编辑布局，模块 / 插件可自带卡片 | 布局表 sys_dashboard_layout，统计聚合各模块 API               | 是        |
| 用户 user        | CRUD、重置密码、解锁、状态、分配角色/部门/岗位、xlsx 导入（含模板）、同步导出与异步导出（走导出中心，基于 `pkg/excel`） | sys_user                     | 是        |
| 角色 role        | CRUD、菜单+按钮权限分配、数据范围、可分配角色           | sys_role, sys_role_menu      | 是        |
| 菜单 menu        | 多级（目录/菜单/按钮三级）、图标/排序/路由/组件/缓存/隐藏/外链 | sys_menu                     | 否（平台级）   |
| 部门 dept        | 树形（parent_id + path）、负责人、级联删除       | sys_dept                     | 是        |
| 岗位 post        | CRUD、与部门关联                          | sys_post                     | 是        |
| 字典 dict        | 字典类型+字典数据、前端下拉/标签渲染                 | sys_dict_type, sys_dict_data | 否（平台级）   |
| 参数 config      | 系统级 KV，运营改配置不发版                     | sys_config                   | 否（平台级）   |
| 通知公告 notice    | 富文本公告、按全员/角色/部门/用户发布、定时发布与过期、置顶、重要公告登录弹窗、已读未读统计、首页轮播 | sys_notice, sys_notice_target, sys_notice_read | 是（超管可发全租户公告） |
| 站内消息 message   | 消息中心、分类、已读未读、铃铛红点、全部已读、实时推送 | sys_message                  | 是        |
| 操作日志 log-oper  | 记录/查询/删除/清理，异常单独一栏（`operLog:export` 权限点与菜单按钮已登记，但没有服务端导出路由） | sys_oper_log                 | 是        |
| 登录日志 log-login | 成功/失败、登录异常（防枚举：真实原因只进日志）            | sys_login_log                | 是        |
| 在线用户 online    | 列表、强制下线                             | 无表，基于 `core/cache`            | 是        |
| 个人中心 profile   | 资料、改密、头像                            | 复用 sys_user                  | —        |
| 文件 file        | 上传（断点续传）、本地/S3 兼容存储、图片视频预览与裁剪、分组 | sys_file                     | 是        |
| 租户 tenant      | 租户 CRUD、套餐绑定、域名绑定、过期时间              | sys_tenant, sys_tenant_pkg   | 平台级（超管端） |

### 公告与消息

两者分工不同，不要混在一起：**公告**是发布一次、很多人看；**消息**是系统或业务事件发给某个人。

- **公告 notice**
  - 内容用富文本（§18），服务端 bluemonday 过滤；可带附件（`Uploader`）。
  - 发布对象用选择控件：全员 / 指定角色（Select）/ 指定部门（TreeSelect）/ 指定用户（远程搜索 Select），存在 `sys_notice_target`。
  - 状态：草稿 → 已发布 → 已撤回；支持定时发布和到期时间、置顶；"重要"公告用户登录后弹窗，点「我知道了」记为已读。
  - 已读统计：管理端能看已读 / 未读人数和未读名单。已读记在 `sys_notice_read`，不给每个人复制一份公告。
  - 超管端可发"全部租户"公告（平台公告），租户内管理员只能发本租户的。
- **消息 message**
  - 一条消息一个收件人，一行 `sys_message`（发给多人就批量插入）。字段：分类（字典）、标题、内容、可选跳转链接、`read_at`。
  - 消息中心有两个标签页：消息、公告。顶栏铃铛的红点 = 未读消息数 + 未读公告数；点开显示最近 5 条。
  - 实时推送用 SSE（`/api/v1/messages/stream`，推未读数变化），断线自动重连并退化为 30 秒轮询。单机直接推；多实例时先推本实例的连接，再经 `core/cluster` 的广播总线（`core/cache` 发布订阅，redis 下生效；`memory` 缓存只在本实例）通知其它实例，断线重连后全量校正，定时重算兜底。该链路只在假实现和 miniredis 上验证过，没有在真实多实例 + redis 上验证。
  - 用户只能读、改、删自己的消息，所有查询强制带当前用户条件。
  - 已读消息默认保留 90 天后自动清理（配置 `message.retention_days`），避免表无限增长。
- **业务模块怎么发消息**：不 import `modules/message`，而是用 core 提供的接口：

```go
d.Notify.Send(ctx, notify.Msg{To: userIDs, Category: "order", Title: "订单已发货", Link: "/orders/123"})
```

  消息模块被停用时，`Send` 是空操作并记一条日志，业务代码不用判断。
- **渠道**：站内（`modules/message`）、邮件（SMTP，`pkg/mail`）、短信（`pkg/sms`）、群机器人（钉钉 / 飞书 / 企业微信，`pkg/robot`）都已在 `core/notify` 实现，业务调用方式不变；邮件与机器人在设置页“通知渠道”配置，短信用 `sms.*` 配置，各渠道有独立限流。
- **规模边界**：M1 按"一次发给几千人以内"设计；十万级用户的广播留到 M2 再优化。

登录认证和 `/healthz`（探数据库、缓存、存储；迁移、插件授权、磁盘的健康详情在 `GET /api/v1/monitor/health`）不进 `modules/`，属 core 能力：账号密码+验证码、JWT 双 token、刷新轮换、登录失败防枚举。可选 `auth.single_session: true` 仅接受最近一次登录的令牌，默认关闭；登录与刷新使用缓存原子替换，多实例须共享 Redis 并配置一致。用户可在个人中心下线自己的其他设备，管理员可在“在线用户”强制下线。

### 登录方式

登录页按配置显示「账号登录」「手机号登录」两个标签，下方是「快捷登录」图标行。在 `auth.methods` 和 `auth.providers` 里配置，**没配置的方式不显示，不报错**。三种方式登录成功后发同一套 JWT 双 token，登录日志记录登录方式。

1. **账号登录**：用户名 + 密码；连续失败 N 次后要求图形验证码。
2. **手机号登录**：手机号 + 短信验证码。
   - 手机号必须已绑定到某个账号（个人中心或管理员绑定）；"未绑定"与"账号不存在"返回同样的提示，防枚举。
   - 验证码 6 位、5 分钟有效，哈希后存 `core/cache`；同一手机号 60 秒只能发一次、每天有上限，同一 IP 限流；连续输错 5 次作废。
   - 短信发送走 `pkg/sms`：`log`（开发用）、`webhook`（带 HMAC 签名）、`aliyun`（Dysmsapi RPC 签名）、`tencent`（SendSms、TC3-HMAC-SHA256 签名），均不依赖厂商 SDK。腾讯云按模板配置变量顺序，通知与验证码分别配置模板。未配置短信服务时，手机号登录入口自动隐藏；开发环境可用 `sms.provider: log` 把验证码打印到日志，生产环境启用它时启动即警告。
3. **快捷登录（第三方，OAuth2 授权码流程）**
   - 国内常用：企业微信、钉钉、飞书（扫码；在对应 App 内打开后台时自动走网页授权免登）。
   - 国际常用：**Google**（标准 OIDC，用通用 OIDC 适配器加预设地址即可）和 **GitHub**（普通 OAuth2，取用户资料和已验证邮箱）。
   - 另有通用 OIDC（可接 Keycloak、Casdoor 等）。每个都是 `pkg/oauth` 里的小适配器，只用标准库，没有引入 `golang.org/x/oauth2` 和 `go-oidc`，也不引各家 SDK。
   - 账号对应关系存 `sys_user_identity`（用户、provider、外部 ID，唯一）。首次登录按 `match_by` 配置（手机号 / 邮箱 / 工号）自动匹配已有账号；匹配不上就提示"绑定已有账号"（输一次账号密码）；`auto_create: true` 时自动建号并赋默认角色，默认关闭。个人中心可绑定、解绑。
   - 安全：`state` 用一次性随机值防 CSRF，OIDC 用 PKCE，回调地址只允许配置里的白名单（防开放重定向），密钥写在 `config.yaml` 并支持 `${ENV}` 环境变量引用，不明文存数据库。
   - 国际登录的额外规则：
     - 邮箱自动匹配**只认已验证的邮箱**（Google 的 `email_verified`、GitHub 的已验证主邮箱），未验证的一律不匹配，防止有人用别人的邮箱抢占账号。
     - 可配 `allowed_email_domains`（例如只允许公司域名的 Google 账号登录），默认不限制。
     - 这两家要求服务器能访问 `accounts.google.com`、`github.com` 等外网地址；内网或受限网络里要么不配置（入口自动隐藏），要么配置 `auth.proxy` 走代理。
     - 按钮样式遵守 Google、GitHub 官方的品牌规范（官方图标和文案），文案随界面语言切换。飞书目前用字节跳动 IconPark 的 new-lark 单色图标（Apache-2.0），不是带品牌色的官方标志，需要时从飞书开放平台品牌资源下载 SVG 替换。
   - 边界：M1 的 provider 配置是全局的；按租户各自配置（SaaS 下每个租户用自己的企业微信或 Google 应用）、LDAP 和 CAS 放 M2。个人微信、微博等国内个人社交登录不做。

### 图表（工作台门面）

- 图表库只用一套：**shadcn/ui charts（Recharts 封装）**，不引 ECharts——双图表库是维护负担。
- 框架提供三个壳组件：`StatCard`（统计卡）、`TrendChart`（趋势折线/柱状）、`ChartCard`（卡片容器）；业务图表在客户自己的 `pages/` 里拼。
- 数据走 `/api/v1/dashboard/stats` 聚合接口（登录即可访问，无表；各指标用原生 SQL 显式带 `tenant_id`，对应模块被停用时退化为 0），各模块按需暴露 `xxx:stats` 权限点的做法**未采用**。
- 确需地图/桑基/3D 等重图表，由业务层自行加 ECharts——可以加，别进 `framework/`。

### 相对上表新增的内置模块（均已实现，详见《功能与权限清单》）

- **设置 / 回收站 / 关于**：`config` 模块除参数 KV 外还有设置页（`site`、`security`、`cache`、`storage`、`ai`、`sms`、`auth`、`notify` 八个分区，存 `sys_setting`，平台级）；`recycle` 展示 user / notice 的软删除记录，可还原；`about` 显示版本与依赖许可证清单。
- **监控**：`server-monitor`（CPU / 内存 / 磁盘 / 网络 / Go 运行时 + 健康详情）、`cache-monitor`、`api-monitor`（最近 30 秒 QPS / 耗时 / 错误率）、`client-error`（前端错误上报，保留 14 天）；根路径另有 `/metrics`（Prometheus 文本，默认仅回环可访问，配 `metrics.token` 后需 Bearer）。
- **导出中心**：`exportcenter` + `core/job` 异步任务运行器（并发 worker、每用户上限、结果文件保留期，多实例靠乐观抢占不重复执行）；用户导出超过 `export.sync_limit` 行时改走异步。
- **数据库备份**：`backup` 模块（页面立即备份、定时、保留策略、下载，依赖本机 `mysqldump` / `pg_dump`），恢复只提供命令行 `nexadmin-server restore`（校验 sha256、恢复前自动备份）。
- **开放平台**：`openapi` 模块 + `core/openapi` 鉴权中间件。个人访问令牌（`nxp_` 前缀，库里存 sha256）、应用签名调用（`X-App-Key` / `X-Timestamp` / `X-Nonce` / `X-Signature`，HMAC-SHA256，±5 分钟，nonce 一次性，应用绑定角色并可配 IP 白名单）、事件目录（`user.*`、`dept.*`、`role.*`、`notice.published` 共 10 个）、Webhook（HMAC 签名、5 秒超时、最多 5 次重试、SSRF 防护）、投递记录；`GET /api/v1/openapi.json` 是运行时从路由清单生成的接口文档。
- **SSO**：见 §20。**插件管理**：见 §19。**AI**：见 §16。**省市区**：见 §18。
- **登录与限流**：登录失败达到上限锁定（默认 5 次 / 15 分钟，设置页可调），连续失败 3 次要求验证码；`ratelimit.*` 为一组固定窗口限流规则（登录、验证码、短信、令牌、向导、上传、AI、创建令牌、签名校验、前端错误上报），计数在 `core/cache`，缓存故障时放行。
- **真实客户端 IP**：限流、登录 / 操作日志、在线会话、开放应用 IP 白名单统一用 `c.ClientIP()`。其取值由 `server.trusted_proxies`（IP / CIDR 列表）决定：默认空 = 不信任任何代理，只用 TCP 对端地址，伪造的 `X-Forwarded-For` / `X-Real-IP` 无效；配置后仅对端在列表内才采信转发头，并取最右一个不在列表内的地址。反向代理部署必须配置，否则所有用户共用代理地址的限流额度。“对端是不是本机”这类安全判定（初始化向导、`/metrics` 回环、本机关停接口）永远直接读 `RemoteAddr`，不用 `ClientIP()`，否则转发头可伪造绕过。热挂载的插件子引擎沿用同一份配置。

### M2 可选模块（按需开，默认不带）

| 模块     | 说明                                                                |
| ------ | ----------------------------------------------------------------- |
| 代码生成   | 表 → model/service/handler/dto + 前端页面；`nexadmin add module` 的完全体，对标各家标配（现状：`add module` 出骨架，`codegen` 付费插件已能按表生成后端模块与前端页面，见 §19） |
| 表单构建器  | 拖拽生成表单 Schema（已作为付费插件提供，见 §19）                              |
| 定时任务   | cron + 执行日志 + 失败重试（已作为付费插件 `cron` 提供，见 §19，不在框架内置）                                                |
| 邮件 / 短信渠道 | 在 `Notifier` 里增加邮件、短信渠道（底层用 `pkg/mail`、`pkg/sms`）（已实现，见 `core/notify`）                |
| 服务监控   | CPU/内存/goroutine/GC（已实现为免费内置模块 `server-monitor`，另有健康详情与前端错误）                                               |
| 缓存监控   | Redis key 浏览、命中率统计（已实现为免费内置模块 `cache-monitor`；memory 驱动只有进程内存与运行时长，redis 读 INFO 与 SCAN）                                                |
| API 文档 | Swagger/OpenAPI，触发式开启（有外部消费者或前后端多人协作时）（已实现为运行时从路由清单生成的 OpenAPI：`GET /api/v1/openapi.json`，`openapi` 模块，权限 `openapi:list`；没有使用 swaggo/swag）                            |

### 明确不做（防范围蔓延，写进 README）

个人微信、微博等国内个人社交登录（企业类与国际常用的 Google、GitHub 见「登录方式」）、工作流引擎、支付、框架核心报表设计器/数据大屏、K8s 运维、短信/邮件发送（做成 `pkg/` 能力，不做模块）。理由：这些场景私有化客户各有定制，框架只做底座；需要时业务层自建或走插件。免费内置 `dashboard`、xlsx 与导出中心保持免费；付费 `report` 插件已有报表中心/数据大屏实现。其余“明确不做”项不因此自动转为付费能力。

### 权限模型

- **RBAC**：用户 → 角色 → 菜单/按钮权限；权限标识命名 `模块:动作`（`user:list`、`order:export`），超管 `*` 短路。
- **数据范围**（角色的一个字段，对标 RuoYi/eladmin 的数据权限）：全部 / 本部门 / 本部门及以下 / 仅本人 / 自定义部门。实现为一个查询 scope，多租户+部门场景刚需，成本低。
- **租户隔离分工**：菜单/字典/参数/权限标识平台级共享；用户/角色/部门/岗位/日志/文件/消息按租户隔离；租户与套餐在超管端管理。
- **数据范围的实际覆盖**：多角色取并集；裁剪用在用户、部门、公告、操作日志与登录日志（`core/datascope` 与用户模块自己的 SQL 版本，语义一致）。部门的读取默认不裁剪（用户表单、筛选、角色自定义范围的下拉要完整树），仅部门管理页带 `scene=manage`（参照 `roles/options` 的 `scene`）时裁剪，并保留到根的祖先节点、标 `readonly:true`。公告的发布对象对非超管且范围非“全部”的操作者也受限（不能发全员 / 平台公告，部门与用户必须在范围内，角色不限制）。
- **角色可分配三道校验**（`core/roleassign`，非超管分配角色时同时满足，超管豁免）：① 角色 `assignable=true` 且不是内置超管角色、权限不含 `*`；② 角色权限集合 ⊆ 操作者当前权限集合；③ 角色的数据范围不比操作者更宽。判定表见《功能与权限清单》§5。
- **租户额度与到期**（`core/tenant`）：租户或套餐上设用户数 / 角色数 / 存储（MB）额度（0 = 不限，租户自己的优先于套餐），新增用户、导入、第三方自动建号、新增角色、文件上传超额返回 403（reason `TENANT_QUOTA_USERS` / `TENANT_QUOTA_ROLES` / `TENANT_QUOTA_STORAGE`）；租户停用或过期（宽限天数 `tenant.grace_days`，到期提醒天数 `tenant.expire_warn_days`）后，登录、刷新令牌和已登录的非超管请求一律 403（reason `TENANT_EXPIRED`）；套餐还可限定菜单范围（用户有效权限与套餐菜单取交集）。

## 13. 技术选型

选型三条硬规则：

1. **许可证只收宽松型**：MIT / BSD / Apache-2.0 / ISC / PostgreSQL License。MPL-2.0 仅限"未修改、原样引用"（文件级 copyleft，不传染业务代码）。
2. **社区体量优先**：同类里选 star 多、近一年仍有发布的；star 少于同类头部一个量级的，需写明理由。
3. **不引入任何"开源核心 + 商业版"项目的商业功能**：只用社区版里本来就有的能力。

Star 数取自 GitHub，核验日期 2026-10-17；标 `—` 的是未在本次联网核验的项，许可证以各项目 LICENSE 文件为准，落地前由 CI 闸再扫一遍（见下文“许可证闸”，机制已接入，完整扫描依赖联网 CI，本轮未执行）。

> **实现现状（2026-10-20，以 `go.mod` / `web/package.json` 为准）**：下面两张表是选型意图。后端 `go.mod` 的直接依赖实际只有 gin、gorm（+ mysql / postgres 驱动、`go-sql-driver/mysql`）、golang-jwt、go-redis、viper、zap、`golang.org/x/crypto`、`golang.org/x/mod`、`yaml.v3`、`mapstructure`、go-sqlmock（测试）。下列项目**没有引入**，由自研或标准库代替：迁移（自研 `core/migrate`，非 golang-migrate / goose）、后端 i18n（自研 `core/i18n`，非 go-i18n）、Excel（自研 `pkg/excel`，标准库 zip + xml，非 excelize）、验证码（标准库 `image` 自绘，非 base64Captcha）、对象存储与断点续传（自研 tus 服务端 `modules/file/tus.go` + 自研 S3 客户端 `core/storage`，非 tusd / aws-sdk-go-v2）、富文本过滤（`pkg/htmlsafe`，基于 `golang.org/x/net/html`，非 bluemonday）、OAuth / OIDC（`pkg/oauth`，非 x/oauth2 / go-oidc）、短信（`pkg/sms` 的 log / webhook / aliyun，无腾讯云）、CLI（标准库 `flag`，非 cobra）、系统监控（读 `/proc`，非 gopsutil）、API 文档（运行时从路由清单生成，非 swag）、定时任务（付费插件 `cron` 自带表达式解析，非 robfig/cron）、测试（标准库 `testing`，非 testify）。前端表以外的差异：PWA 为手写 `sw.js`，没有 `vite-plugin-pwa`。

### 后端（Go）

| 用途 | 选型 | 许可证 | Star | 说明 |
| --- | --- | --- | --- | --- |
| Web 框架 | gin-gonic/gin | MIT | 89.4k ✓ | 主框架，目标人群熟悉 |
| ORM | go-gorm/gorm + 官方 mysql / postgres driver | MIT | 40.0k ✓ | MariaDB 复用 mysql driver |
| 配置 | spf13/viper | MIT | — | 读 default.yaml + config.yaml 两层 |
| 日志 | uber-go/zap | MIT | — | 结构化日志 |
| JWT | golang-jwt/jwt | MIT | — | 双 token |
| 参数校验 | go-playground/validator | MIT | — | gin 内置使用 |
| 迁移 | golang-migrate/migrate | MIT | — | 或 pressly/goose（MIT），二选一，定一个即可 |
| 缓存客户端 | redis/go-redis | BSD-2-Clause | — | 客户端库，同时兼容 Redis 协议的 Valkey |
| 后端 i18n | nicksnyder/go-i18n | MIT | — | 两层 locales 合并 |
| Excel | xuri/excelize | BSD-3-Clause | — | `pkg/excel` 的底层 |
| 验证码 | mojocn/base64Captcha | Apache-2.0 | — | 账号密码登录用 |
| 定时任务（M2） | robfig/cron | MIT | — | |
| 对象存储 + 断点续传服务端 | tus/tusd + aws/aws-sdk-go-v2（S3） | MIT / Apache-2.0 | — | tusd 实现 tus 断点续传协议；统一走 S3 协议，阿里云 OSS、腾讯云 COS 等都有 S3 兼容接口，不为各家单写适配（tusd 的依赖以落地时为准，由许可证闸核验） |
| 富文本 HTML 过滤 | microcosm-cc/bluemonday | BSD-3-Clause | — | 服务端白名单过滤，防 XSS |
| OAuth2 / OIDC | golang.org/x/oauth2（含 github 端点）、coreos/go-oidc | BSD-3-Clause / Apache-2.0 | — | 快捷登录适配器的底层；Google 走 OIDC，GitHub 用 x/oauth2 自带端点；企业微信、钉钉、飞书自写薄适配，不引各家 SDK |
| 短信 | 阿里云、腾讯云官方 Go SDK | Apache-2.0 | — | 在 `pkg/sms` 里按接口封装，用哪家配哪家（许可证以落地时核验为准） |
| CLI | spf13/cobra | Apache-2.0 | — | `nexadmin` 命令 |
| 系统监控（M2） | shirou/gopsutil | BSD-3-Clause | — | |
| API 文档（M2） | swaggo/swag | MIT | — | |
| 测试 | stretchr/testify | MIT | — | |

### 前端

| 用途 | 选型 | 许可证 | Star | 说明 |
| --- | --- | --- | --- | --- |
| UI 组件 | shadcn/ui（基于 Radix UI） | MIT | 125.3k ✓ | 组件源码复制进项目，无运行时依赖、无授权费用 |
| 样式 | Tailwind CSS | MIT | — | 移动端断点靠它 |
| 框架 / 构建 | React + Vite + TypeScript | MIT / MIT / Apache-2.0 | — | |
| 路由 | React Router | MIT | — | |
| 服务端状态 | TanStack Query | MIT | 50.4k ✓ | 请求缓存与重试 |
| 表格 | TanStack Table | MIT | — | headless，样式用 shadcn |
| 表单 + 校验 | react-hook-form + zod | MIT | — | |
| 图表 | Recharts（shadcn/ui charts） | MIT | 27.6k ✓ | 全站只用这一套 |
| 国际化 | react-i18next | MIT | — | |
| 全局状态 | zustand | MIT | — | 只放登录态、主题等少量全局状态 |
| 图标 | lucide-react | ISC | — | |
| 字体 | Inter + JetBrains Mono（自托管） | OFL-1.1 | — | OFL 允许商用和随包分发；中文用系统字体栈，不引 CDN |
| 富文本 | Tiptap 开源核心（基于 ProseMirror） | MIT | — | 只用 MIT 的开源包，不用 Tiptap Pro / Cloud 的付费扩展 |
| 上传 | Uppy 核心 + tus 插件（无界面版） | MIT | — | 界面自己用 shadcn 写 |
| 图片裁剪 | cropperjs | MIT | — | |
| 拖动排序 | dnd-kit（core + sortable） | MIT | — | 支持触屏和键盘 |
| 前端 HTML 过滤 | DOMPurify | Apache-2.0 / MPL-2.0 双许可，取 Apache-2.0 | — | |
| HTTP | axios | MIT | — | 封装在 `framework/` 请求层 |

### 基础设施（部署时的外部软件）

| 用途 | 选型 | 许可证 | Star | 说明 |
| --- | --- | --- | --- | --- |
| 数据库 | MySQL 8.4 / MariaDB 10.11 / PostgreSQL 16 | GPLv2 / GPLv2 / PostgreSQL License | — | 数据库作为**独立进程**由客户部署，不随源码包分发，不与业务代码链接，GPL 不传染 |
| 缓存（可选） | **Valkey** 或 Redis 7.2 及以前 | BSD-3-Clause | 27.4k ✓ (Valkey) | 见下文"明确避开" |
| 对象存储（可选） | 客户自有的 S3 兼容服务 / 阿里云 OSS / 腾讯云 COS | 服务商条款 | — | 框架只带客户端，不指定服务端产品 |

### 明确避开（已核验或确知的授权陷阱）

| 项目 | 原因 | 替代 |
| --- | --- | --- |
| Redis 7.4 及以上 | 许可证已不是宽松开源（RSALv2 / SSPLv1，Redis 8 增加 AGPLv3 选项）；GitHub 许可证识别为 NOASSERTION ✓ | Valkey（BSD-3，Linux 基金会维护），go-redis 直接兼容 |
| MinIO 服务端 | AGPLv3，且仓库已归档（archived）✓ | 客户自有 S3 兼容服务；框架只用 Apache-2.0 的客户端 |
| MongoDB、Elasticsearch / Kibana | SSPL / Elastic License / AGPL | 本框架不需要；确需搜索时由业务层评估 |
| Highcharts、AG Grid Enterprise、Handsontable、Syncfusion、Kendo UI | 商业授权 | Recharts + TanStack Table |
| Terraform、Vault 等 BUSL 项目 | 非开源许可 | 框架不涉及，业务层引入前先查 |
| CKEditor 5、TinyMCE（新版本） | GPL 或商业授权 | Tiptap 开源核心 |
| ffmpeg 随包分发 | LGPL / GPL，随包分发有合规负担 | 视频封面在浏览器端截帧，不做服务端转码 |
| 来源许可不明的行政区划第三方数据仓库 | 许可证不清，随包分发有风险 | 用国家统计局公开数据自行脚本生成（再分发条款需合规确认） |

### 许可证闸（框架仓库 CI，客户零负担）

- 已有 `security.yml` 和 `scripts/security/licenses.py`：Go 使用 `go-licenses`，npm 使用闸脚本核对锁文件依赖元数据；策略以脚本允许清单为准。PR 在依赖清单、锁文件、许可证策略或闸脚本变化时触发；手动与主分支入口保留完整检查。完整依赖许可证扫描需要联网工具和元数据，本轮未执行。
- 允许清单：MIT、BSD-2/3、Apache-2.0、ISC、PostgreSQL、MPL-2.0（仅原样引用）。
- 命中 AGPL、GPL（链接进二进制的）、SSPL、BUSL、RSAL、Elastic License、Commons Clause、FSL 即红，不许发版。
- 新增依赖必须在 PR 里写明"许可证 + star 数 + 为什么不用现有的"，防止选型悄悄膨胀。

## 14. 开发约定（本项目实施阶段）

1. **开发期不写文档**：不写 README、说明文件，也不回改本文档，全部精力放在代码上。全部完成后，**按最终代码现状一次性生成文档**，文档以代码为准。（2026-10-20 已按此生成 `nexadmin/README.md` 与 `nexadmin/docs/` 下的《部署与运维指南》《配置参考》《功能与权限清单》，并回改本文使之与实现对齐，见文首「实现状态」。）
2. **严格按本文档实现**：目录、命名、选型、规则以本文档为准。实施中发现文档与实际不符或有缺口，先停下来问，不自行偏离。
3. **多 agent 并行**：按目录所有权切分任务，每个 agent 只写自己负责的目录，互不改对方文件。切分示例：`core/`、`pkg/`、`modules/*`、`web/src/framework/`、`migrations/framework/`、CLI。共享契约（`Module` 接口、`core/response`、租户 context、`pkg/idgen` 接口）先由一个 agent 定下，其余再并行展开。
4. **批量写代码**：动手前一次想清楚，再整批写出，不写一句想一句。
5. **改文件一次读、一次改**：改某个文件前一次性读完，一次性想好所有改动，一次性改完，不反复读同一个文件、不分多次小改。
6. **只验证改动到的部分**：不做全量构建。各 agent 写完自己的目录后，只对自己改动的包做增量编译和测试（`go build` / `go test` 指定包路径，依赖 Go 构建缓存）；全部合并后只做**一次**整体编译。不在每个小改动后构建。
7. **防卡死、防死循环**：下面每一条都要写进每个 agent 的 prompt（子 agent 没有主会话的上下文，不写就会遗忘）。
   - **有预算**：每个 agent 只做一个目录里的一件事，有明确的完成标准，最多约 40 次工具调用；到预算还没完成就停下，汇报"已完成什么、卡在哪"，不硬撑。
   - **失败两次就停**：同一做法失败两次，不再第三次重试，换思路或上报。相同的命令加相同的参数，不重复调用超过两次。
   - **命令必须有超时且非交互**：`go test -timeout 60s`、`timeout 120 <命令>`；前端测试用一次性模式（`--run` 或 `CI=1`），不开 watch；开发服务器、`nexadmin-server` 这类常驻进程只能后台启动并限定存活时间，不许前台运行。
   - **不写无上限的等待**：只允许"最多检查 N 次"，或等后台任务的完成通知，不用 sleep 循环。
   - **不扩散**：子 agent 不再派生子 agent；同时并行的 agent 不超过 6 个。
   - **先理清依赖再并行**：共享契约先由一个 agent 定下，其余才并行，避免互相等待造成死锁。
   - **主 agent 兜底**：后台 agent 超时未返回就用停止工具终止，读取它已写入的文件，再决定重派或自己接手；每个 agent 结束时必须返回产出文件清单。

## 15. 质量闸（精简版）

原则：**只测"承诺"，不测代码行数。** 框架的承诺是升级不炸、租户不串、权限不漏、三引擎一致，闸只围着这四件事设。测试代码量不追求覆盖率，不为凑数写测试；总耗时有预算，超了就砍，不加机器。

闸只设在框架仓库 CI，客户项目零负担。

> **实现现状**：`ci.yml` 已接入 merge-base 增量包选择（包含测试反向依赖、资源归属及保守全量回退），前端按构建相关路径触发；三类公共契约 golden 位于 `core/testdata/contracts/`，PR 比较基线并要求 golden 变化时同步更新现有清单版本。正常验证不改写 golden。`security.yml` 已接入 govulncheck、gitleaks 和 Go/npm 许可证闸，机制接入不代表扫描通过；本轮未执行联网扫描。`derive.yml` 的真实数据库派生 lane 保持原状；三引擎矩阵、升级回环、正式发布门禁远端验收尚未完成。PR 总预算 ≤3 分钟、发版总预算 ≤20 分钟仍需真实 CI 耗时证据，不能以单 job 超时配置代替验收。Playwright 仍不计划；下文保留的原始 E2E 设想不构成本轮任务。旧静态接口报告标为历史结论。

### 每个 PR（只跑改动涉及的部分，预算 ≤ 3 分钟）

- CI 按路径过滤：只改前端就不跑后端，只改某个模块就只编译、测试该模块及依赖它的包（`go build` / `go test` 指定包，开启 Go 构建缓存；前端在 `web/` 或影响构建的配置有改动时跑 `tsc`）。**依赖清单、共享配置、删除包或依赖图不可用时保守回退全量。**
- 关键件单测，只覆盖会出事的地方：租户 scope、数据范围 scope、`idgen`、权限判断、统一响应结构。不设覆盖率门槛。
- 架构闸脚本（几十行），只检查框架自己的代码：模型未登记租户基座或豁免、业务表用了 `sys_` 前缀；框架内置模块互相 import 只警告。
- PR 在 Go/npm 依赖清单、锁文件、许可证策略或闸脚本变化时触发许可证闸（§13）；手动与主分支入口保留完整检查。机制接入不代表本轮已执行扫描。

### 发版前（预算 ≤ 20 分钟，一次性，不在每个 PR 跑）

- 全量构建（后端 `go build ./...`、前端 build），`govulncheck`、`gitleaks`。
- 三引擎迁移与启动冒烟（mysql:8.4 / mariadb:10.11 / postgres:16，真库，不 mock）。
- 派生验证 lane + 升级回环（§11）：`internal/` 零 diff。
- **1 条** E2E 冒烟（Playwright，Apache-2.0）：登录 → 一个典型 CRUD → 租户 A 看不到租户 B 的数据。

### 契约

- 只锁三样，用 golden 文件：`Module` 接口、路由前缀、统一响应结构与错误码。改了就必须同时改 golden 并升版本号。
- OpenAPI 对比、前后端类型生成**暂不做**，等框架稳定、接口多起来再加。

### 明确不做（防止闸比业务代码还多）

覆盖率硬门槛、前端组件单测、全量 E2E、夜间任务、移动端截图对比、ZAP 动态扫描、镜像扫描、flaky 重试框架。需要时按下面的规则再加。

### 新增一道闸的规则

- 必须说明它防的是哪条承诺、或哪次真实出过的事故，说不出就不加。
- 加一道，要同时评估耗时；PR 档超过 3 分钟、发版档超过 20 分钟，就必须砍掉一道价值最低的。
- `core/testkit`（构造租户上下文、平台上下文和 memory cache）：已实现；真实数据库、Redis 和登录 token 的集成测试仍由各模块自行装配。
- 所有 CI 任务设超时（单个任务不超过 20 分钟），测试默认带 `-timeout`，任何等待都有上限，防止 CI 卡死。

## 16. AI 内置（M1 打底，不是外挂）

目的：让 AI 成为系统的一部分，而不是事后加的聊天窗口。三条设计：

1. **统一 AI 网关 `core/ai`**
   - 一个 Provider 接口，只实现 **OpenAI 兼容协议**，一个实现就能接通义千问、DeepSeek、智谱，以及 Ollama / vLLM 本地模型。用标准 `net/http` 调用，不引第三方 SDK，没有额外许可证风险。
   - 配置：设置页“AI”分区（`sys_setting`，平台级）优先，没保存过时回落到 `config.yaml` 的 `ai.base_url / api_key / model`。**未配置则所有 AI 入口自动隐藏，不报错**；私有化客户默认不向外发数据，可指向内网模型。
   - 网关实际做的：每用户限流（默认每分钟 20 次，另受 `ratelimit.ai` 约束）与超时（默认 30 秒，上限 120 秒）、token 上限、输入长度上限、模型输出按白名单重新校验（筛选字段、译文占位符与标签）、上游报错细节不透给调用方、调用审计只记元信息（用户、接口、耗时、token），不记提示词与回复。**租户级开关与每日配额、敏感字段脱敏**已实现：套餐设每日次数，租户可覆盖（空 = 沿用套餐，0 = 不限）并有 `ai_enabled` 开关，计数在缓存、按站点时区隔天重置，超管与平台租户不受限，关闭返回 403 `TENANT_AI_DISABLED`、超额返回 429 `TENANT_QUOTA_AI`，策略查询或计数出错时放行；发往上游前对手机号、邮箱、身份证号、银行卡号脱敏（默认开，设置页可关，返回内容不还原）。模型与密钥配置仍是平台级的，不按租户。
2. **不改变 Gin 的写法，也为 AI 留好口子**
   - 路由仍是原生 `r.GET(...)`，权限用普通中间件 `core.Perm("article:list")`：它负责鉴权，同时把权限点登记到框架。权限点清单加上框架已知的路由，就是 AI 工具清单的原料。
   - 可选地给路由补 `core.Desc("文章列表")` 提供描述，OpenAPI 和 MCP 共用。框架 `POST /api/v1/mcp` 提供 JSON-RPC 协议，支持 initialize（2025-03-26）、ping、tools/list、tools/call 与通知，入口要求 `mcp:list`。GET 工具直接执行，显式注册的写工具走服务端浏览器确认（当前 `message_mark_read`）：会话 JWT + 本地密码批准，5 分钟有效，重调时原子消费；不接受任意 URL 或客户端 confirmed 绕过。实际调用重新经过原 Gin 路由登录、权限与租户校验。初始化返回 `Mcp-Session-Id`，`DELETE /api/v1/mcp` 删除；会话及每会话 4 个并发槽位存配置缓存，多实例须共享 Redis / Valkey。会话固定期限取 15 分钟与初始化凭据寿命的较短者，不续期并绑定具体凭据；每实例最多跟踪 1024 会话、64 并发请求，每请求最长 30 秒，缓存故障拒绝。目录 `/api/v1/mcp/tools` 保留；支持 JSON 或最终单条 SSE 封装，不支持批量、GET 长连接、重放、进度或上游工具 SSE 转发。真实多实例与浏览器确认链路仍待验收。
3. **AI 不开后门**
   - AI 调用系统能力时，以**当前登录用户**的身份走同一套鉴权、租户隔离、数据范围，权限不会因为是 AI 而放大。
   - 写操作必须由用户确认后才执行；AI 只产出结构化参数（如筛选条件），不能直接拼 SQL 或执行命令。
   - 用户数据与系统指令严格分开，防提示词注入。
   - **硬上限防死循环**：每次 AI 调用有超时（默认 30 秒）和 token 上限（默认 2048，最大 8192）。M1 的筛选与翻译都是单次调用，没有工具调用循环，所以“最大工具调用轮数 / 相同参数重复调用中止”未实现（也暂时用不到）；超限返回明确错误，不无限重试。

### M1 自带的 AI 能力（少而实）

| 能力 | 说明 |
| --- | --- |
| 自然语言筛选 | `DataTable` 搜索框支持一句话描述，转成结构化过滤条件，再走正常查询，受数据范围约束 |
| 翻译助手 | 公告、字典标签、语言包的机翻初稿，与 `nexadmin i18n add --translate` 同源（§7）（页面里有翻译助手按钮） |

公告写作辅助已实现（`POST /ai/write`，入口在公告表单里）。M2 再加：工作台数据解读、日志与错误解释、表单写作辅助。每一项都建立在上面的网关和元数据之上，不另起炉灶。

研发侧的 AI：`nexadmin new` 生成的项目自带 AI 规则文件（军规、目录所有权、移动端骨架规则），客户用 AI 开发时不会改坏框架区。

## 17. 部署、初始化与恢复出厂

### 运行形态

生产构建为**单一二进制**（前端构建产物 `go:embed` 进去，`scripts/build.sh` 带 `-tags embedweb`），旁边是 `config/`、`data/`、`logs/`、`migrations/`。内置子命令的逻辑只写一份（Go，跨平台，`core/ctl`、`core/setup`），脚本只是薄壳：

```
nexadmin-server                  # 前台运行（默认）
nexadmin-server version
nexadmin-server start | stop | restart | status
nexadmin-server init --config <文件>          # 非交互初始化（容器 / 自动化）
nexadmin-server migrate [--status | --dry-run | --mark-applied <scope>/<文件> | --unmark <scope>/<文件>] [--yes]
nexadmin-server restore <备份文件> [--yes] [--dry-run] [--sha256 <值>]   # 命令行恢复数据库，需先 stop
nexadmin-server reinit [--dry-run]            # 恢复出厂（交互确认）
```

- 后台运行写 `data/run/nexadmin.pid`（JSON：pid、端口、关停令牌），控制台输出追加到 `logs/nexadmin.out`；生产环境（`app.env=production`）另有 JSON 日志 `logs/nexadmin.log`。`stop` 通过本机回环地址上、带令牌保护的关停接口优雅退出（令牌在 pid 文件里），Windows 与 Linux 行为一致，不依赖信号。
- 正式生产环境建议交给 systemd 或容器编排；这组脚本面向单机、演示和测试环境。

### 首次部署：初始化向导

启动时检测"未初始化"（`core/setup.IsInitialized`）：`data/.initialized` 存在即视为已初始化；`config/config.yaml` 不存在即视为未初始化（全新安装）；否则连数据库看有没有超管，连不上、读不到都按已初始化处理（库可连通且一张表都没有时判未初始化，`reinit` 之后就是这种情况）。未初始化时系统进入"未初始化"状态，并向用户明确提示，**初始化完成后提示自动消失**：

- 打开任何页面，都显示整页提示卡片「系统尚未初始化」，带「开始初始化」按钮进入向导；除 `/setup/*`、`/api/v1/system/status` 和静态资源外，不显示任何业务内容。
- 业务 API 统一返回 503，错误码 `NOT_INITIALIZED`，信息「系统尚未初始化」，方便调用方和监控识别。
- 如果是远程访问且未允许远程初始化（默认），后端对 `/api/v1/setup/*` 返回 403「请在服务器本机打开 http://localhost:端口 完成初始化」；前端提示卡片本身不区分本机 / 远程，「开始初始化」按钮始终显示，进入向导后请求会收到上述 403。
- 服务器控制台启动时同样打印醒目提示、向导地址和一次性令牌（后台启动时 `start` 的输出里也带令牌）。
- 前端启动时请求公开的 `GET /api/v1/system/status`（只返回 `{initialized: true|false}`，不含版本等其他信息）：未初始化显示提示卡片；已初始化则这套提示和向导的代码都不再渲染。

向导步骤（页面共 6 步，`SetupWizard.tsx`）：

1. 环境检查：`data/` 与 `config/` 可写、端口、支持的数据库引擎（MySQL / MariaDB / PostgreSQL）。
2. 数据库：类型、主机、端口、账号、密码、库名；「测试连接」；可选「库不存在则自动创建」。
3. 缓存：memory 或 Valkey / Redis，支持连接测试。
4. 管理员与站点：超管账号（`^[A-Za-z][A-Za-z0-9_]{2,31}$`）与密码（非空且不超过 72 字节）、站点名称、默认语言、时区。
5. 可选配置：本地或 S3 存储与 AI 模型；密码与密钥不出现在确认摘要。
6. 确认汇总 → 执行，通过 POST SSE 实时接收阶段状态：写 `config/config.yaml`（权限 0600）→ 跑迁移 → 种入出厂数据 → 创建超管 → 写初始化标记。普通客户端保留 JSON 响应；失败显示具体步骤和原因，配置文件回滚，可改参数重试。完成后按页面提示进入登录或重启服务。

缓存、S3 与 AI 配置均可在页面输入；缓存提供 `POST /api/v1/setup/test-cache` 测试接口，S3/AI 当前只配置不探测。`init --config` 与 `POST /api/v1/setup/execute` 同样支持 `cache`、`optional` 字段。

初始化接口天生没有登录态，所以防护写死：
- 本机直连无需令牌；远程或经代理需显式配置 `setup.allow_remote: true` 并携带 `X-Setup-Token`。本机判定只看 TCP 回环地址且不接受转发头，保持既有防护。页面提供密码式令牌输入，所有 setup 请求使用该头；令牌仅保存在组件内存，初始化成功后清空，不写浏览器持久存储。
- 同一时刻只允许一个初始化在跑；向导接口按 IP 限流（`ratelimit.setup`）。
- **完成后向导永远进不去**：后端 `/setup/*` 一律返回 404；前端 `/setup` 页面直接跳转到登录页；向导的路由在已初始化的进程里根本不注册（不是注册后再拦截）。
- **宁可错判为已初始化，也不错判为未初始化**：只要 `data/.initialized` 存在就视为已初始化；没有标记但有 `config/config.yaml` 时，连库看有没有超管，数据库连不上、读不到状态时同样按已初始化处理，并在日志里报错，不开放向导。只有标记不存在、且（没有 `config.yaml`，或库可连通且没有超管 / 一张表都没有）时才开放。
- 想重新初始化，只能在服务器上运行 `reinit` 脚本（见下文恢复出厂）；网页上没有任何入口能重新打开向导。
- 密码和令牌不进日志。
- 容器或自动化部署用 `nexadmin-server init --config ...` 非交互完成，不开放向导。
- 注意：服务只看对端 TCP 地址，放在同机反向代理后面时代理转发来的请求对端是回环地址，会被当成本机。完成初始化之前不要把服务经代理暴露出去。

### 一键脚本（双击运行）

放在框架区 `deploy/`，Windows `.bat`、macOS `.command`、Linux `.sh`。每个都只做三件事：没有终端就自动开一个终端窗口；调用二进制对应子命令；结束时暂停，窗口不立刻关闭。
- `ctl`：菜单 1 启动 / 2 停止 / 3 重启 / 4 状态。
- `reinit`：恢复出厂，见下节。

### 恢复出厂（reinit）

双击后在终端先列出**将要做什么**，再让用户确认。示例：

```
========== NexAdmin 恢复出厂设置 ==========
将要执行：
  1. 停止服务（运行中，pid 1234）
  2. 清空数据库  mysql://127.0.0.1:3306/nexadmin
       共 47 张表，约 12.8 万行，184 MB；该库中所有表都会被删除
  3. 清理缓存    仅删除 key 前缀 nex:*（不会 FLUSHALL）
  4. 删除本地上传目录  ./data/uploads（1,204 个文件，2.1 GB）
  5. 删除初始化标记，重启后进入初始化向导
保留：config/config.yaml、internal/、migrations/business/、日志
备份：已用 mysqldump 备份到 ./backup/reinit-20261016-141825.sql
注意：远程对象存储（S3 / OSS）里的文件不会被删除，需要手动清理。
确认执行？[y/N]
```

规则：
- 默认 N；只有输入 `y` 才执行，其他一律取消。
- 显示的库名、主机、表数、数据量必须是**实际读到的**，不是配置里的猜测；读不到就中止，不执行。
- 配置 `app.env: production` 时，输入 `y` 之后还要再输入库名确认。
- 默认先备份（用本机的 `mysqldump` / `pg_dump`），格式与备份页一致：写到 `backup.dir`（与备份页、`restore` 同一目录），文件名 `nexadmin-reinit-<引擎>-v<版本>-<时间>.sql.gz`，gzip 压缩并带 `.sha256` 旁路文件，可以直接交给 `restore` 恢复；找不到工具时，清单里明确写"备份：未备份"，由用户决定是否继续。`--dry-run` 只列清单不执行。
- 缓存只删本系统前缀的 key；不动远程存储；不动 `internal/` 等用户代码。
- 完成后自动启动，打印向导地址和新的初始化令牌，并提示向导里的数据库配置按旧值填写（类型、主机、端口、账号、库名；页面不会自动预填）。

## 18. 文件上传、富文本与表单控件

### 上传（断点续传）

- 协议用 tus（开放的断点续传协议）。服务端是自研的 tus 实现（`modules/file/tus.go`，没有使用 `tusd`），挂在 `/api/v1/files/upload`（POST 创建、HEAD 查进度、PATCH 追加、DELETE 取消），前端用 Uppy 核心 + tus 插件（无界面版），分片默认 5MB，断网、刷新页面后可续传。
- 钩子：创建前校验登录态、租户、大小上限、类型白名单（tus 在上传开始前就知道总大小，超限直接拒绝，不浪费流量）；完成后写 `sys_file`（租户、大小、类型、哈希、访问级别）。
- 存储走 `core/storage`：本地目录（默认）或 S3 协议存储，不为各家云单写适配。
- 类型校验：服务端会读取真实文件头进行校验。创建上传时先按客户端声明的类型和文件名做预检，完成上传时再校验图片、视频、PDF 等真实头信息，并拦截 HTML、伪造扩展名和截断文件；类型白名单由服务端强制（来源与默认值见下节，`modules/file/tus.go` 的 `typeAllowed`）；下载时带 `X-Content-Type-Options: nosniff`。
- 访问分两级：`public`（路径含随机串，不需登录，用于富文本图片、头像）；`private`（默认，短期签名链接，带过期时间，解决 `<img>`、`<video>` 带不了 Authorization 头的问题）。本地存储支持 Range 请求，视频可拖动进度。
- 部署注意：反向代理（如 nginx）的 `client_max_body_size` 不能小于分片大小，上传路径关闭请求缓冲。

### 上传大小限制（后台可设，默认不限）

上传大小有三项，`0` 或留空表示不限制（默认）。**服务端强制的限制 = 后台“系统参数”页（`/configs/upload`）里设置的值，页面没设的项回落到 `config.yaml` 的 `upload.*`**：页面读写 `sys_config` 里的 `upload.max_size_image_mb`、`upload.max_size_video_mb`、`upload.max_size_file_mb` 和“允许上传的文件类型”（`upload.allowed_types`）；系统参数模块通过 `core.SetUploadLimitsSource` 登记来源（文件模块不 import 系统参数模块），文件模块在 tus 创建前钩子里取“页面值叠加 config.yaml”：大小 > 0 才覆盖，类型列表非空才覆盖，页面读不到（数据库出错）时整体回落 `config.yaml`。读取带 5 秒短缓存，页面保存 / 参数增删改 / 刷新缓存时本实例立即失效，多实例最多晚 5 秒。取舍：页面里填 `0` 表示“没设”而不是“强行放开”，所以不能放宽 `config.yaml` 里已设的上限；类型白名单出厂带一份默认值（图片 / mp4 / webm / pdf / doc(x) / xls(x) / zip），升级后这份白名单会开始被服务端强制，需要其他类型请在页面里增加或清空。前端选文件时的预检读 `GET /config/upload`，页面未限制大小时会显示“未限制上传大小，磁盘可能被占满”的提示；不嗅探文件头。按租户单独限额放 M2。

### 回显预览与裁剪

- `<Uploader>` 是框架组件，表单里直接用，上传完成立刻回显：图片显示缩略图（点击放大），视频显示 `<video controls>` 加封面，其他文件显示图标和文件名；可替换、可删除。
- 视频封面在浏览器端用 canvas 截首帧，不在服务端转码，不引入 ffmpeg。
- 裁剪只针对图片，选图后弹出裁剪框（cropperjs），按字段配置：

```tsx
<Uploader kind="image" crop={{ aspect: 16 / 9, width: 1280, height: 720 }} />  // 固定比例 + 固定输出尺寸
<Uploader kind="image" crop="free" />                                           // 自由裁剪
<Uploader kind="image" />                                                       // 不裁剪
```

- 裁剪和缩放在浏览器里完成，只上传结果，省带宽也省服务器。视频 M1 不做裁剪和转码。列表缩略图 M1 直接用原图，按需生成缩略图放 M2。

### 富文本

- 编辑器用 Tiptap 开源核心（MIT），工具栏用 shadcn 组件做，手机上工具栏可横向滑动。M1 功能：标题、加粗 / 斜体 / 下划线 / 删除线、列表、引用、代码块、链接、表格、对齐、文字颜色与高亮、撤销重做、插入图片和视频（走 `<Uploader>`，图片为 `public`）。
- 只使用 MIT 的开源包，不使用 Tiptap Pro / Cloud 的付费扩展。
- 安全：存的是 HTML，**前端用 DOMPurify 过滤一次，服务端用 `pkg/htmlsafe` 白名单再过滤一次**（基于 `golang.org/x/net/html` 的 tokenizer 逐 token 重新生成 HTML，没有使用 bluemonday；`img` / `video` 只允许自家文件地址或 https，去掉脚本和事件属性），展示时只渲染过滤后的内容。

### 拖动排序

- **适用**：所有带顺序的列表——菜单、部门、岗位、角色、字典类型与字典数据，以及 `Uploader` 多图 / 多文件的顺序。
- **前端**：`DataTable` 加 `sortable` 属性即出现拖动手柄列；菜单、部门这类树形列表用 `TreeTable` 的 `onReorder` 和 `onMove`。同级排序与跨父级移动均基于 dnd-kit，支持触屏（长按约 200ms 后拖动，不与页面滚动冲突，手柄触控区不小于 44px）和键盘（空格抬起、方向键移动、空格放下）。拖完立即显示新顺序，失败自动回滚并提示。
- **后端**：表里一个 `sort` 整数列，默认 `ORDER BY sort, id`。拖动后前端把**同一组兄弟的完整 id 顺序**一次提交，服务端在一个事务里整组重排（`sort = 序号 × 10`），不做"插到两条之间"的间隔算法，三引擎行为一致。一组兄弟通常在几十条以内。现有模块仍保留各自的 `PUT .../sort` 路由，并可复用 `core.Sort` 的完整性校验与事务更新。
- **接入方式**：各模块自带处理器，如 `PUT /posts/sort`（权限复用 `post:update`）；菜单额外提供 `PUT /menus/:id/move`，校验不能移动到自己或后代、目标不能是按钮，并将节点追加到目标父级末尾。排序提交的 id 集合必须与库里该组完全一致，否则返回 409「列表已变化，请刷新」，避免两人同时拖动互相覆盖。`nexadmin add module --sortable` 会为新模块生成排序接口和带拖动手柄的列表页。
- **限制**：分页的长列表不提供拖动（顺序不明确），`sortable` 与分页同时开启时框架给出警告；这类列表按字段排序即可。
- `nexadmin add module` 遇到标记为 sortable 的字段，自动生成排序接口和带拖动手柄的列表页。

### 省市区（内置）

- **数据**：省市区查询读取框架内嵌数据（`core/region`，`go:embed`），不建数据库表，没有迁移和种子。全国三级约 3,500 条是目标规模，不是当前正式数据完整性的验收结论；计划使用可靠公开来源按年生成，具体来源、年份、完整性与再分发授权尚待外部确认。每条有 `code`（6 位行政区划代码）、`name`、`pinyin`（供拼音搜索）。
- **存什么**：业务表只存**行政区划代码**（`VARCHAR(6)`，如 `province_code`、`city_code`、`district_code`），不存名称、不存自增 id。代码是国家标准，跨环境、跨版本稳定。
- **区划调整**：`core/region/region.go` 已嵌入 `regions_history.json`，实现历史名称回退和继任代码查询；历史数据文件当前为空。全国三级、街道及历史正式数据需要可靠来源、年份和再分发条件，正式数据发布链路未验收；本轮保留现有数据，不猜测补齐。
- **接口**：`GET /api/v1/regions/tree?level=3` 一次取回三级树（压缩后约几十 KB，浏览器缓存）；`GET /api/v1/regions?parent=<code>` 按需取下级（第四级街道乡镇放 M2，按需加载）。提交时服务端校验代码存在、上下级关系一致。
- **组件**：`RegionSelect`（级联选择，可输入名称或拼音搜索；桌面是级联下拉，手机是底部抽屉里的分栏选择），`level` 可设为 2 或 3；`RegionText` 把代码显示成"广东省 / 深圳市 / 南山区"。详细地址仍用 Input。
- **边界**：M1 只做中国大陆省市区三级；港澳台只到省级行政区（统计局公布的区划代码通常不含港澳台下级，以实际数据为准，需要时由业务层补充）。海外地区不做省市区，国家 / 地区用 `CountrySelect`：ISO 3166-1 国家代码，名称用浏览器自带的 `Intl.DisplayNames` 按当前语言显示，不增加翻译数据。

### 表单控件：能选就不要输入

框架表单规则：只要取值范围有限，或来自已有数据，就用选择控件，不用文本框。

| 字段类型 | 控件 |
| --- | --- |
| 字典项（状态、类型、性别等） | `DictSelect`（绑定字典） |
| 角色、岗位、用户 | 可搜索的 Select（远程搜索） |
| 部门、菜单上级 | `TreeSelect` |
| 布尔 | Switch |
| 日期、时间 | DatePicker / DateTimePicker |
| 语言、时区、币种 | Select |
| 省、市、区 | `RegionSelect`（级联，数据内置） |
| 国家 / 地区 | `CountrySelect`（ISO 3166-1，名称随语言切换） |
| 图标、颜色 | IconPicker / ColorPicker |
| 文件、图片、视频 | `Uploader` |
| 长文本 / 富文本 | Textarea / 富文本编辑器 |
| 其余（名称、编号等） | Input |

手机上 Select 自动变成底部抽屉的选择列表（§8）。`nexadmin add module` 生成表单时按字段类型自动选控件：枚举或字典 → `DictSelect`，外键 → 远程搜索 Select，布尔 → Switch，时间 → DatePicker，图片或视频 → `Uploader`。

---

## 19. 插件体系（基础免费，高级功能以插件售卖）

### 19.1 免费内置 vs 付费插件

- **免费内置（开源，永远不收费）**：§12 全部 M1 模块，加设置、回收站、服务/缓存/接口监控、关于、AI 基础能力（智能筛选、翻译）、全部登录方式、邮件/短信的 `pkg/` 能力。原则：**没有它后台就不完整的，都在内置里**。
- **付费插件（高级，按需购买）**：当前有九份源码交付目录：`cron`（调度、执行日志、失败重试、工作台卡片）、`audit`（字段级数据变更审计）、`codegen`（读业务表结构、预览并下载生成代码）、`security`（TOTP、恢复码、IP 黑白名单）、`identity`（租户 LDAP/AD 与 CAS）、`formbuilder`（设计、发布、登录填写、提交查询与导出）、`report`（结构化数据集、报表与大屏）、`scim`（用户/组同步）、`i18n-editor`（词条、草稿、发布、回滚）。源码包在 `nexadmin-plugins/<id>/`，工作副本前端在 `web/src/plugins/<id>/`、后端在 `plugins/<id>/`。九个 Go 包均已在 `plugins/plugins.go` 空导入，九个前端均有 `route.tsx`，并按插件 ID 放置清单与迁移。
- **同步与验收状态**：独立源码包与工作副本须按 `backend/`、`web/` 和清单分别核对；多语言后端、清单和迁移统一放在 `plugins/i18n-editor/`。代码存在、空导入、授权有效、运行时挂载和真实环境验收须分别记录；完整矩阵见 [交付核对清单](nexadmin-vendor/交付核对清单.md)。本轮未执行真实数据库、浏览器、AD/OpenLDAP/CAS 或 IdP 验收；`codegen` 真实表结构读取仍待验收。发布包和 `nexadmin new` 默认排除已装插件，`release check` 对残留插件报 FAIL。
- **新增 assistant 的当前边界**：原九插件列表不是当前插件总数。主项目及独立包后端均为 assistant 1.1.0，包含个人命名会话路由（`assistant:use`，每租户每用户最多 30 个，查询显式带 tenant_id / user_id），清单迁移目标为 platform。Go 模型与两处三引擎 SQL 统一使用平台表 `plg_assistant_conversation`；Go 实体 ID 为 `string`，SQL 主键为 `CHAR(32)`，PostgreSQL 索引也使用该表名。创建调用 `newID()` 并处理错误，以 `crypto/rand` 生成 16 字节，再用 `hex.EncodeToString` 编码为 32 位小写十六进制字符串；`validConversationID` 只接受长度 32 且字符属于 `0–9`、`a–f` 的 ID，不调用 `idgen.Next()` 或 `idgen.Parse()`。瞬态 run/lease 同样使用随机 ID。独立库须把该表加入 `platform_tables`，不能加入业务 `tables`。已有同步记录覆盖生产后端、完整前端、清单及三引擎迁移；独立包 `api.ts` 已包含命名会话 CRUD 及各会话请求的 `conversationId`。2026-10-23 已有全项目 Go 构建和前端 typecheck 通过记录，本次文档更正未重新执行。SQL 未执行，持久化及真实数据库验收均未完成。
- 免费插件同样用这套机制，只是 `edition: free`，不校验授权。

### 19.2 安装模型：源码插件

Go 不能运行时热加载代码，所以插件以**源码包**形式交付：安装 = 把文件放进约定目录 + 重新编译，仍是"客户拿到的都是可读可改的源码"。

- 插件包（目录或 zip）结构：

```
<id>/
├── plugin.yaml          # 清单（必须）
├── backend/             # Go 包，安装到 plugins/<id>/
│   ├── plugin.go        # init() 里调用 plugin.Register
│   └── migrations/{mysql,mariadb,postgres}/*.sql
└── web/                 # 前端模块，安装到 web/src/plugins/<id>/（结构同 framework/modules/<name>/）
```

- `plugin.yaml` 字段：`id`（小写字母数字连字符，全局唯一）、`name`、`version`、`description`、`author`、`edition`（`free` / `pro`）、`requires_nexadmin`（版本范围）、`menus`（安装时写入 `sys_menu` 的目录/菜单/按钮，含权限码）。
- 安装命令：`go run ./cmd/nexadmin plugin install <目录或zip>`，做四件事：校验清单与版本 → 拷贝文件 → 更新 `plugins/plugins.go` 的空导入（标记行之间的内容由命令维护）→ 提示重新编译并重启。`plugin remove <id>`、`plugin list` 同理；卸载默认保留数据表。
- 后台「插件管理」页：列出已装插件、版本、授权状态（有效 / 未授权 / 已过期 / 无效 / 已吊销，另有即将到期提示）、导入授权文件。授权文件导入后**立即生效，不需要重新编译**。

### 19.3 编写约定（插件作者必读）

1. 后端包放在 `backend/`，只依赖 `core/`、`pkg/` 暴露的接口，**不改 core、不改 framework、不 import 其他插件或内置模块**。
2. 入口：

```go
func init() {
    plugin.Register(plugin.Plugin{
        ID: "cron", Name: "定时任务", Version: "1.0.0", Edition: plugin.Pro,
        Module: &module{}, // 实现 core.Module：Name() / Register(r, d)
    })
}
```

3. 数据表前缀 `plg_<id>_`，权限码前缀 `<id>:`，接口路径 `/api/v1/plugins/<id>/...`，避免与内置或其他插件冲突。
4. 迁移 SQL 与内置一致：三种数据库各一份，文件名 `001_init.sql` 递增，只增不改。
5. 前端模块结构与内置模块一致（`route.tsx`、`api.ts`、`*.mock.ts`、页面、`locales/{zh-CN,en-US}.json`），i18n 命名空间用 `plugin_<id>`，只用 `framework/ui` 与 `framework/components` 的组件和语义色 token。
6. 必须带 mock（前端先行）和至少一个后端单测；不做授权校验——授权由框架在加载前统一拦截。

### 19.4 授权（离线签名）

- 授权文件 `data/licenses/<id>.lic`：JSON 载荷（`plugin`、`customer`、`issued_at`、`expires_at`、可选 `domains`）加 ed25519 签名。公钥内置在 `core/plugin/pubkey.go`，**私钥只在发行方手里**，不进仓库。
- 启动时逐个插件校验：有效才挂载路由、写入菜单；缺失、过期、被篡改一律不挂载（页面和菜单都不出现），「插件管理」页显示原因。到期前 30 天提示。
- 发行方工具：`plugin keygen`（生成密钥对）、`plugin sign`（给某客户签发授权）。
- 坦白的边界：源码交付下，懂行的客户可以改掉校验代码——授权校验防的是"不小心用盗版"和"随手分享"，不是对抗性破解；真正的保护靠合同、升级服务和技术支持。域名校验与到期下线已实现：载荷带 `domains` 时，只有当前服务域名（配置的站点域名 / `sso.issuer` 的 host，都没有则取第一次带 Host 的请求）精确或 `*.example.com` 通配匹配才有效，否则状态为 invalid；运行中每小时检查一次，到期的插件路由立即返回 403 并标 expired，到期前 30 天在 `GET /plugins` 里带 `expiresSoon`。吊销只做离线辅助：授权载荷可带可选的 `serial`，发行方用 `plugin revoke`（或 licensor 后台导出）生成带签名的吊销清单 `config/license-revoked.json`（沿用授权的公钥集合与 kid，`seq` 递增，验签失败或回退的清单一律忽略并告警），随升级包分发；命中序列号的授权状态为 `revoked`，路由返回 403，清单变化后无需重启。它不联网回查，源码交付下客户删掉清单或检查代码即可绕过，所以只是辅助手段；没有序列号的旧授权不能单独吊销，主要手段仍是短有效期（如一年）加订阅续期，需要提前终止的客户靠合同约束。

---

## 20. 生态与单点登录（SSO）

NexAdmin 既是底座，也是生态的统一身份中心：BI、数字孪生、OA、企业邮箱等产品各自独立，都通过它登录。

- **内置免费**：`modules/sso` 让 NexAdmin 成为标准 OIDC Provider（授权码 + PKCE、discovery、JWKS、userinfo、单点登出、refresh 轮换，RS256 签名）。下游应用用任何标准 OIDC 客户端接入，不需要定制 SDK。
- **应用注册表**：每个下游应用有 client_id/密钥（库中只存哈希）、严格匹配的回调地址白名单、可见角色。后台「应用管理」维护，「应用中心」按权限展示可进入的应用。
- **授权页是前端路由**：SPA 用 Bearer 而非 cookie，所以浏览器先到 `/sso/authorize`，由前端带令牌调接口换取 code 再跳回下游应用。
- **issuer**：默认 `<站点>/api/v1/oauth`，标准 discovery 地址为 `<issuer>/.well-known/openid-configuration`；生产环境请显式配置 `sso.issuer`。
- **边界**：IdP 只在令牌里给出用户、租户、角色、部门等声明；各应用内的功能权限由应用自己管，不做全局统一权限。企业邮箱集成成熟的开源邮件系统（SSO 登录 + 账号同步），不自研邮件服务器；OA 是基于框架二开的独立产品，不进框架。
- **付费身份源已实现**：`identity` 提供按租户 LDAP/AD 与 CAS 对接，已注册登录扩展点，仍待真实身份源验收。手动目录同步已接入后台任务与 `identity:sync`，要求有效授权、启用的 LDAP 源及 `directorySync.enabled`；`SearchPaged` 采用 RFC 2696 分页，每页最多 100 条，单次同步累计上限 `maxDirectoryEntries=5000`。服务端忽略分页控制时，在同一次检索中接收结果，超过 `ldapPageSize=100` 即失败；普通 `Search` 的客户端硬上限则为 50 条。超限或检索异常拒绝，全部 subject / 账号校验后逐个新增或更新，中途失败可能部分更新。`deactivateMissing` 默认关闭，开启后仅在完整枚举且全部写入成功后停用该目录绑定过但本次缺失的账号，并受返回数量及受保护账号检查约束；不支持自动定时同步。2026-10-23 后续收尾记录已记载全量 Go 测试通过，identity 后端（含测试与三引擎迁移）、完整前端及清单与独立包逐文件一致，取代此前仅生产文件一致的阶段结论；本次未重新执行验证，真实身份源验收仍待完成。其他登录审计与异常告警需求按实际实现另行核对。
- **已做（原“未做”中的一部分）**：密钥轮换的管理接口（`GET /api/v1/sso/keys`、`POST /api/v1/sso/keys/rotate`，权限 `sso:list` / `sso:edit`）；用户 / 部门向下游的同步，已提供拉取式通讯录接口 `GET /api/v1/oauth/org/users`、`/oauth/org/depts`（应用需开启 `allowOrgRead`，用 `client_credentials` 令牌、scope `org.read`），并有 `user.*` / `dept.*` 事件可通过 Webhook 推送（§12 开放平台）。
- **已实现**：SCIM 风格的同步接口已作为独立付费插件提供；OIDC discovery 同时提供标准根路径 `/.well-known/openid-configuration` 和兼容路径 `/api/v1/oauth/.well-known/openid-configuration`。
- **issuer 配置**：`sso.issuer` 不在 `config/default.yaml` 里声明，由 `modules/sso` 自行读取（环境变量 `NEXADMIN_SSO_ISSUER`）。

*附：目录所有权分区与升级覆盖图未提供，见 §4、§5 的文字说明。*

### 本轮离线交付与验收边界（2026-10-22）

日志导出已存在于 `modules/log/export.go` 和模块路由：服务端权限、数据范围、行数上限、按用户限流及导出审计均有机制，不重复立项。真实数据库与浏览器验收未在本轮执行。identity/codegen 交付验证在临时框架副本中替换交付后端，前端交付验证在临时前端副本替换六文件并使用现有本地工具执行类型与 lint 检查；类型检查不能代替页面验收。

升级恢复仅覆盖文件系统执行错误，不承诺数据库迁移回滚或断电事务。正式授权密钥、吊销与发行演练、真实 Redis/S3/IdP、初始化与 RegionText 页面行为均不在本轮范围。当前工作区缺少 Git 元数据，真实 PR 基线及 `git diff --check` 的本地验收受阻；固定图和版本规则单测不等同于远端工作流验收。
