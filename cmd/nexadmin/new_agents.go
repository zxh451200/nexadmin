package main

import "strings"

// agentsMD 是新项目的研发侧 AI 规则文件（AGENTS.md / CLAUDE.md），设计文档 §4 §5 §6 §8 §14。
// 源文本里用 ^ 代替反引号。保持 ≤120 行、只留高频决策规则；规格细节去 docs/二次开发指南.md。
var agentsMD = strings.ReplaceAll(`# AI 开发规则（AGENTS.md）

本项目 = NexAdmin 框架源码 + 你的业务代码，同一仓库。^nexadmin upgrade^ 会覆盖框架区，所以动手前先分清文件归谁。
详细做法与例子见 ^docs/二次开发指南.md^。

## 1. 目录所有权（三分，以 ^.nexadmin.manifest.json^ 为准）
- 框架区（升级覆盖，**禁止改**）：^core/^ ^modules/^ ^pkg/^ ^cmd/^（含 ^cmd/server/main.go^）^web/src/framework/^ ^migrations/framework/^ ^docs/^ ^deploy/^ ^scripts/^ ^config/default.yaml^；另有 ^web/^ 下的 ^src/App.tsx^ ^src/main.tsx^ ^src/index.css^ ^src/lib/^ ^public/^ ^embed.go^ ^embed_dev.go^ ^.env.production^ ^.env.development^ ^vite.config.ts^ ^tailwind.config.ts^ ^postcss.config.js^ ^tsconfig.json^ ^eslint.config.js^ ^index.html^ 也不要改。
- 合并区（只能**加**自己的依赖，不改框架已有条目）：^go.mod^ ^go.sum^ ^web/package.json^ ^web/package-lock.json^。
- 用户区（你的地盘，升级不碰）：^internal/^ ^plugins/^ ^web/src/pages/^ ^web/src/plugins/^ ^web/src/locales/^ ^web/src/theme.css^ ^config/config.yaml^ ^config/locales/^ ^migrations/business/^ ^data/^。
- 新文件一律放用户区；拿不准归属就读清单，别猜。

## 2. 想改框架行为：按顺序往下找，上一级能解决就不要进下一级
1. 配置：^config/config.yaml^ 只写要改的项（map 深合并，列表整体替换；^NEXADMIN_*^ 环境变量最优先）。品牌色改 ^web/src/theme.css^，站点名/logo 在后台「系统设置」。
2. 扩展点：模块可选接口（^auth.Provider^、^setup.Seeder^、^setup.StartupSeeder^、^migrate.SourceProvider^）、中间件（^core/middleware^ 的 ^RateLimit^）、^config/locales/<lang>.yaml^ 覆盖后端文案 key。
3. 事件/任务：^event.Subscribe(event.SubscriberFunc(...))^ 订阅 ^user.created^ 等；^job.Register("类型", handler)^ 注册异步任务。
4. 覆盖/替换：前端在 ^web/src/pages/^ 的 ^route.tsx^ 声明与内置页相同的 path，整页覆盖（pages > plugins > 框架）；后端在 ^disabled_modules^ 停用内置模块，并在 ^internal/modules/^ 提供**不同 Name()** 的替代模块（同名会被一起停用）。
5. 向框架提需求：以上都不行（如换租户解析 ^WithTenantResolver^ 需要改 main.go、数据范围包在 ^modules/internal^ 不可导入）就停下，写清缺口交给人，**绝不手改框架区**。

## 3. 后端
- 一个目录一个模块：^internal/modules/<名>/^，^init(){ core.RegisterBuiltin(module{}) }^，实现 ^Name()^（全局唯一，与内置模块/插件 id 重名启动即报错）+ ^Register(r *gin.RouterGroup, d *core.Deps)^；^internal/modules/modules.go^ 标记行之间的空导入由 ^add module^ 维护。**不要在 main.go 注册模块，main.go 不改。**
- 路由用原生 Gin；^r^ 是 /api/v1 且要登录，匿名用 ^d.Public^，超管用 ^d.Admin^。同 method+path 重复注册会 panic。
- 权限 ^core.Perm("<模块>:<动作>")^，前缀用你的模块名，不与内置权限码重名。
- 模型嵌入 ^database.TenantModel^；雪花主键，JSON 里 ID 是字符串，前端不做数值运算。
- 响应统一 ^response.OK/Page/Fail^，错误用 ^response.BadRequest("key")^ 等；文案 key 放 ^config/locales/<lang>.yaml^。
- 审计：写操作路由挂 ^logmod.Default().Middleware("模块名","create")^（包 ^nexadmin/modules/log^）；**请求时再取 ^Default()^**，别在 Register 里缓存（业务模块的 init 早于内置模块）。
- 限流：^middleware.RateLimit(d.Cache, d.Log, "scope", middleware.ByUser(), 60, time.Minute)^。
- 数据范围与角色分配校验：^nexadmin/core/datascope^（按操作者数据范围裁剪查询、校验目标是否在范围内）、^nexadmin/core/roleassign^（分配角色的三道校验）可直接导入使用，业务表带部门归属时用它，不要自己复制逻辑。
- 向框架入口传 Option（自定义租户解析、域名映射、登录提供者）：改 ^internal/boot/boot.go^ 的 ^Options()^，不要改 ^cmd/server/main.go^（框架区）。
- 参考 ^internal/modules/example/^（示例，可删：删目录并删 ^modules.go^ 里那一行空导入）。

## 4. 前端
- 页面放 ^web/src/pages/<名>/^，^route.tsx^ 导出 ^routes: ModuleRoute[]^（path 无前导斜杠、^perm^、^menu^）；自动发现，不用改路由壳。
- 往工作台加卡片：在 ^web/src/pages/<名>/cards.tsx^ 导出 ^cards: DashboardCard[]^（id 形如 ^模块名-含义^、^title^ 为 i18n key、^perm^、^size^、^component^ 用 ^React.lazy^），自动发现；**不要改 framework 的工作台页面**。
- 页面只用 ^web/src/framework/^ 的骨架（AppLayout、DataTable、FormLayout、ResponsiveDialog），不直接用 ^ui/table^ ^ui/dialog^ 等原语（ESLint 对 ^src/pages/**^ 生效，但只是警告，别当它不存在）。
- 移动优先：默认样式即手机样式，^md:^ 以上再加桌面样式；触控区域 ≥44px。
- 颜色只用语义 token（^bg-background^ ^text-foreground^ ^bg-primary^ ^border^ ^chart-1..5^），不写死色值或 ^bg-blue-500^；换品牌色改 ^theme.css^ 里的变量。
- 方向用逻辑类（^ms-^ ^me-^ ^ps-^ ^pe-^ ^text-start^），不用 ^ml-^ ^mr-^ ^text-left^。
- i18n：文案进 ^web/src/locales/{zh-CN,en-US}.json^（两份同步；合并顺序 框架 → 插件 → 用户，同名 key 用户覆盖）。
- 先 mock：^*.mock.ts^（放在页面目录或 ^src/mock/^，自动加载）让页面先跑通，再接真实接口。
- 菜单：业务模块的菜单由 ^add module^ 生成的种子写入；内置菜单改名/隐藏去后台「菜单管理」（数据，升级不覆盖）。**别删内置菜单**（重启会补回），隐藏或停用即可。

## 5. 冲突预防清单
- 不改：第 1 节框架区全部文件，尤其 ^cmd/server/main.go^、^config/default.yaml^、^web/src/framework/^。
- 业务表用 ^biz_^ 前缀（或任意非 ^sys_^ / ^plg_^ 的前缀）；权限码前缀 = 你的模块名。
- 迁移只增不改：^migrations/business/{mysql,mariadb,postgres}/NNN_名.sql^ 三引擎各一份；已执行的文件改一个字节都会因 checksum 拒绝启动，要改结构就加下一个编号；迁移里不写 INSERT/UPDATE/DELETE。
- 不编造命令、不编造框架 API：不确定就读源码或 ^docs/^。
- 依赖用精确版本（不写 caret、波浪线、latest 范围）；只在 ^go.mod^ / ^web/package.json^ **新增**自己的条目，不改框架已有条目的版本。
- 改共享文件（^modules.go^、^go.mod^）只用精确编辑，不整文件重写。

## 6. 命令（只有这些存在；前缀都是 ^go run ./cmd/nexadmin^，没列的一律不存在）
- ^plugin list|install [--force] <目录或zip>|remove <id>|keygen|sign|revoke^ 管理源码插件（^sign^ / ^revoke^ 是发行方用的授权签发与吊销，二开者一般不需要）。
- ^add module <名称> [--fields "title:string,status:dict(sys_status),owner_id:ref(user)"] [--sortable]^：生成后端 ^internal/modules/<名>/^、迁移 ^migrations/business/*/NNN_<名>.sql^、前端 ^web/src/pages/<名>/^，幂等追加空导入；重启自动执行迁移并补菜单（新菜单超管默认可见，其他角色到「角色管理」分配）。字段类型 string text int float bool date datetime dict(编码) ref(资源) image file。生成后补业务逻辑，别重写骨架；已存在的目录拒绝覆盖。
- ^i18n add <locale> [--name 显示名] [--dir ltr|rtl]^ 加语言；^i18n check [--all]^ 查缺失翻译（只提示）。
- ^new <名称> [--out 目录] [--module go模块路径] [--with-plugins] [--git]^ 派生新项目；^upgrade^ 见下节。
- ^release check [--strict]^、^release pack^：框架发版用，二开者一般不需要。
- 开发：后端 ^go run ./cmd/server^；前端 ^cd web && npm install && npm run dev^；生产迁移 ^nexadmin-server migrate [--status|--dry-run]^。
- 提交前：^go build ./... && go vet ./... && go test ./...^；前端 ^cd web && npx tsc --noEmit && npm run build^。

## 7. 升级流程
1. ^git commit^ 当前改动（工作区不干净会被拒绝）。
2. 拿到新版：release 包（^release pack^ 产物）或新版源码目录/zip。
3. ^upgrade --from <路径> --dry-run^ 看清单；确认后去掉 ^--dry-run^ 正式升级。
4. ^go mod tidy^；^cd web && npm install^。
5. 构建与测试（见上）；生产环境先 ^nexadmin-server migrate^。
6. 检查所有 ^*.bak^ 与输出里的 ^WARN^。

## 8. 手改过框架文件 / 出现冲突
被手改的框架文件升级时存成 ^<文件>.bak^ 并 WARN：对比 ^.bak^ 与新文件，把改动**迁移到用户区**（第 2 节的扩展点/覆盖页面/替代模块），确认后删 ^.bak^；不要把旧内容拷回框架文件。
^go.mod^ / ^package.json^ 冲突：保留框架的条目，再补回自己新增的条目，然后 ^go mod tidy^ / ^npm install^。

## 9. 派发子 agent 必带的前缀（子 agent 没有本会话上下文，原样复制）
^^^
你在 NexAdmin 二开项目里工作。目录所有权：框架区（core/ modules/ pkg/ cmd/ web/src/framework/ migrations/framework/ docs/ deploy/ config/default.yaml）绝不修改；
go.mod、go.sum、web/package.json 只能新增自己的依赖（精确版本），不改已有条目；其余代码只写在 internal/、web/src/pages/、web/src/locales/、web/src/theme.css、config/config.yaml、migrations/business/。
需要框架没有的能力时停下并汇报缺口，不要手改框架区。后端模块在自己包的 init() 里 core.RegisterBuiltin，不改 main.go；业务表前缀 biz_，迁移只增不改；只使用 AGENTS.md 里列出的 nexadmin 命令。
前端：用 framework 页面骨架（AppLayout/DataTable/FormLayout/ResponsiveDialog），移动优先；颜色只用语义 token；方向用逻辑类（ms-/me-/ps-/pe-/text-start）；文案进 web/src/locales 的 zh-CN 与 en-US 两份；先写 *.mock.ts 让页面跑通再接接口。
完成标准：go build ./...、go vet ./...、cd web && npx tsc --noEmit 通过；汇报时列出新增/修改的文件。
^^^
`, "^", "`")
