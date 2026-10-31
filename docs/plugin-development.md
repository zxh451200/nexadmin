# NexAdmin 插件开发指南

本文教你从零写一个 NexAdmin 插件，并打包提交到插件市场。以首批免费计划中的「定时任务 cron」说明结构：从发行方插件市场或独立插件源码仓库获取其已发布的免费安装包，再对照阅读。框架源码交付包及默认 `nexadmin new` 不附带 cron，也不附带付费 cron 示例。

基础服务免费，可选通用插件免费，业务增强收费。首批免费计划：`cron`、`codegen`、`imports`、`totp`、`ldap`、`maps`、`crm`；其余按 pro 规划。计划不代表所有现有包已经切换 edition，更不代表 LDAP / TOTP 已编译挂载；须核对包清单、Go 注册与运行时状态。免费安装验证见[快速开始](quickstart.md)。

> 适合谁：会写 Go（Gin + GORM）和 React 的开发者。你不需要读懂整个框架，只需要读本文和 cron 示例。

## 1. 先弄清三件事

**插件是源码，不是二进制。** Go 不能在运行时热加载代码，所以插件以源码包交付：安装 = 把文件放进约定目录 + 重新编译。买家拿到的是可读可改的源码，这也是 NexAdmin 的交付方式。

**插件分免费和专业（pro）两种。** `edition: free` 不校验授权；`edition: pro` 需要买家导入授权文件才会生效。授权由平台签发，作者不需要自己做授权校验。

**插件是自包含的。** 一个插件 = 一个后端 Go 包 + 一个前端目录 + 一份清单（`plugin.yaml`）。安装和卸载只会动这几处，不会改框架代码。

## 2. 包结构

```
cron/                         # 目录名 = 插件 id
├── plugin.yaml               # 清单（必须）
├── backend/                  # 安装到 plugins/cron/
│   ├── plugin.go             # init() 里注册插件
│   ├── *.go                  # 你的业务代码
│   └── migrations/
│       ├── mysql/001_init.sql
│       ├── mariadb/001_init.sql
│       └── postgres/001_init.sql
└── web/                      # 安装到 web/src/plugins/cron/
    ├── route.tsx             # 前端路由
    ├── api.ts
    ├── cron.mock.ts          # 前端 mock（必须）
    ├── CronPage.tsx
    └── locales/{zh-CN,en-US}.json
```

zip 包的根目录或唯一的一级子目录里要能找到 `plugin.yaml`。

## 3. 清单 plugin.yaml

```yaml
id: cron                     # 小写字母/数字/连字符，全局唯一，最长 48
name: 定时任务
version: 1.0.0
description: 定时任务（5 段 cron 调度、立即执行、失败重试、执行日志）
author: NexAdmin
edition: free                # 免费 cron 示例；收费插件使用 pro
requires_nexadmin: ">=0.1.0"

menus:                       # 安装/挂载时幂等写入 sys_menu
  - parent: /system          # 父节点：目录写它的 path，按钮写菜单的 perm
    type: menu               # dir | menu | button
    title: plugin_cron.menu  # i18n key，文案放在 web/locales
    path: /system/cron
    component: plugins/cron/CronPage
    perm: cron:list
    icon: Timer              # lucide 图标名
    sort: 100
  - parent: cron:list
    type: button
    title: plugin_cron.btn.edit
    perm: cron:edit
    sort: 10
```

规则：
- 所有权限码必须以 `<id>:` 开头（安装时强制检查）。
- 菜单文案不要写死中文，一律用 i18n key。
- 菜单是否对某个角色可见，由买家在「角色管理」里分配，不是你决定的。

## 4. 后端

### 4.1 入口

```go
package cron

import (
    "context"
    "github.com/gin-gonic/gin"
    "nexadmin/core"
    "nexadmin/core/plugin"
)

func init() {
    plugin.Register(plugin.Plugin{
        ID: "cron", Name: "定时任务", Version: "1.0.0", Edition: plugin.Free,
        Module: &module{},
    })
}

type module struct{}

func (*module) Name() string { return "cron" }

// Register 注册路由。r 已经是 /api/v1 且默认要求登录。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
    g := r.Group("/plugins/cron")
    g.GET("/jobs", core.Perm("cron:list"), listJobs(d))
    g.POST("/jobs", core.Perm("cron:edit"), createJob(d))
}
```

`ID`、`Version`、`Edition` 要和 `plugin.yaml` 保持一致。

### 4.2 后台任务（可选）

需要常驻 goroutine（调度器、消费者）就实现 `plugin.Starter`：

```go
func (m *module) Start(ctx context.Context, d *core.Deps) error {
    go scheduler.Run(ctx) // 必须非阻塞；ctx 在进程退出时取消
    return nil
}
```

`Start` 是同步调用的，必须尽快返回。返回的错误只会记日志，不影响路由挂载。

### 4.3 约定（违反会被审核打回）

- 接口路径：`/api/v1/plugins/<id>/...`。
- 数据表前缀：`plg_<id>_`，例如 `plg_cron_job`。
- 权限码前缀：`<id>:`，例如 `cron:list`。每个接口都要加 `core.Perm(...)`。
- 只能依赖 `core/` 和 `pkg/`。**不要改 core、不要改 framework、不要 import 内置模块或别的插件。**
- 可用的框架能力都在 `core.Deps` 里：`DB`、`Cache`、`Auth`、`Log`、`Config`、统一通知入口 `Notify`（站内 / 邮件 / 短信 / 机器人，nil 安全）、跨实例事件广播 `Bus`（nil 安全）、停止时的清理登记 `OnClose`、运行时接口文档 `APIDoc`，以及不要求登录的 `Public` 和仅超管的 `Admin` 路由组。不要直接查别人的表；确实要读内置表（如 cron 的 `clean_logs` 清理日志）要用表名常量，并处理表不存在的情况，不能 panic。
- 业务主键用雪花 ID（`pkg/idgen`），接口里序列化成字符串。
- 租户隔离：按租户的数据表带 `tenant_id`，查询必须带当前租户条件。
- 外部请求（HTTP 回调等）必须防 SSRF：禁止内网和回环地址，设置超时。cron 的 `ssrf.go` 可以直接参考。

### 4.4 数据库迁移

放在 `backend/migrations/<引擎>/`，**mysql、mariadb、postgres 三份都要有**，文件名 `001_init.sql`、`002_xxx.sql` 递增，**只增不改**（已发布的迁移不能再改内容，改动写新文件）。运行时会按文件名顺序执行并记录已执行的版本，不会重复执行。

注意三种数据库的差异：自增/主键类型、`TEXT` 默认值、布尔类型、`ON UPDATE` 语法。cron 的三份 SQL 是现成的对照。

**迁移只做结构变更，不写 `INSERT`/`UPDATE`/`DELETE`**：runner 会在 `migrate --dry-run` 里逐条警告。出厂数据（菜单、字典、模板）一律由 Seeder 负责；存量库的菜单结构收敛也在 Seeder 里做（见 `modules/system/menu/reconcile.go`），不靠迁移改数据。

### 4.5 测试

后端至少带一个单测，覆盖你最容易出错的纯逻辑（cron 示例测了表达式解析、下次执行时间、SSRF 拦截）。需要真实数据库的部分如果没法在单测里跑，请在提交说明里写明。

### 4.6 给导入中心提供数据源（可选）

插件里有值得批量导入的主数据时，实现 `pkg/importspec` 的 `Provider` 即可挂进「导入中心」，**不需要依赖导入中心插件本身**：

```go
func (m *module) ImportSources(db *gorm.DB) []importspec.Source {
    svc := NewService(db, zapLog{zap.NewNop()}) // 用插件自己的日志适配
    return []importspec.Source{{
        Name: "crm_customers", Title: "客户导入", TitleKey: "imports.source.crm_customers.title",
        Columns: []importspec.Column{
            {Key: "name", Label: "客户名称", LabelKey: "imports.source.crm_customers.name", Required: true, Example: "张三"},
        },
        Handler: func(ctx context.Context, row int, vals map[string]string) error { /* 逐行落库 */ },
    }}
}
```

上面两个 key 要在后端语言包里补上，否则来源名和模板表头只显示代码里的中文兜底。位置是 `core/i18n/locales/<lang>.yaml` 的 `imports.source.<来源名>`（`imports` 块已存在，只在 `source:` 下加新来源），`zh-CN.yaml` 与 `en-US.yaml` 各一份、键路径一致：

```yaml
imports:
  source:
    crm_customers:
      title: 客户导入   # Source.Name 对应的来源名（下拉与台账的来源列）
      name: 客户名称    # 每列一个键，键名 = Column.Key
      phone: 手机号
      remark: 备注
```

- `imports.source.<Source.Name>.title` 对应 `TitleKey`，其余每个 `Column.Key` 对应同名 `LabelKey`；列键不用加 `title.` 之类前缀。
- 两处声明要一致：`Source{Name: "crm_customers"}` ↔ `imports.source.crm_customers`，`Column{Key: "name"}` ↔ `imports.source.crm_customers.name`。写错不会报错，只会退回 `Title`/`Label` 兜底文案，排查时先看 YAML 的层级。
- `TitleKey`/`LabelKey` 留空即始终用兜底文案（等同于只支持中文），只在来源确实只有一种语言时才省略。
- 行内业务错误的文案（如 `dict_data.sort_invalid`）也放在同一个 `imports.source.<来源名>` 块下，由 `Handler` 自己 `i18n.T(ctx, key)` 翻译后返回。

约定：

- 来源在导入中心挂载时自动收集（插件模块在内核里被包了一层，收集前会 `core.UnwrapModule` 拆包）；重名会记 warning 并跳过，不会 panic。框架内置模块同样可以实现这个接口（不会反向依赖插件），出厂已带两个：`crm_customers`（`plugins/crm/import.go`）与 `dict_data`（`modules/system/dict/importsource.go`）。
- `Label` 是兜底表头，`TitleKey`/`LabelKey` 有值时按请求语言渲染来源名与模板表头；解析上传文件时**所有已配置语言的表头都认**，所以用户切了语言再上传自己下载的模板也不会丢列。新增语言记得同步补 key。
- `Handler` 只处理一行，返回 error 即该行失败并写进错误报告，其它行继续。报告里是给人看的文本，别把 `crm.name_required` 这种 i18n key 原样返回——自己 `i18n.T(ctx, key)` 翻译后再返回。
- 大文件走后台协程，跑在 `context.Background()` 上（租户与语言从台账行还原），落库要用传入的 `ctx`，不要缓存请求级状态。
- 按业务键做「命中就更新、未命中就新建」，让同一份文件重复导入不翻倍——参考 `plugins/crm/import.go`。

## 5. 前端

前端目录的结构和内置模块完全一致，安装后位于 `web/src/plugins/<id>/`，路由、mock、i18n 文案会被自动发现，**不需要改框架任何文件**。

### 5.1 路由

```tsx
// web/route.tsx
import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/cron',
    component: lazy(() => import('./CronPage')),
    perm: 'cron:list',
    menu: { title: 'plugin_cron.menu', icon: 'Timer', order: 100, group: 'system' },
  },
]
```

真正显示在侧边栏的菜单来自服务端的 `sys_menu`（即 `plugin.yaml` 的 `menus`），所以 `path` 和 `plugin.yaml` 里的 `path` 要一致。

如果页面只是某个配置项，可以把它做成「系统设置」的一个分区，而不是独立侧栏入口：路由声明 `settings`（`component` 指向不带 `PageContainer` / `PageHeader` 的分区组件），`plugin.yaml` 的菜单行保持原父节点并设 `hidden: true`，旧地址 `<Navigate>` 到 `/system/settings?tab=<key>`。不要为了「归到设置下」把菜单挂到 `settings:list` 父节点：角色分配会自下而上补齐祖先，等于顺手授予 `settings:list`。权限点仍由该菜单行提供，角色授权不受影响。参考 `plugins/ldap/route.tsx` 与 `plugins/maps/route.tsx`：

```tsx
{
  path: 'system/map',
  component: () => <Navigate to="/system/settings?tab=maps" replace />,
  perm: 'maps:config',
  settings: { key: 'maps', group: 'integration', title: 'plugin_maps.menu', order: 100, component: lazy(() => import('./MapSettingsPage').then((m) => ({ default: m.MapSection }))) },
}
```

### 5.2 约定

- **i18n**：命名空间固定为 `plugin_<id>`，在 `locales/zh-CN.json` 和 `en-US.json` 里同步写，顶层 key 就是 `plugin_cron`。动态拼接的 key（如 `` t(`plugin_cron.type.${v}`) ``）要保证每个取值都有文案。
- **组件**：只用 `@/framework/components` 和 `@/framework/ui` 里的组件，例如 `DataTable`、`FilterBar`、`ResponsiveDialog`、`FormLayout`、`PageHeader`。不要直接用 `ui/table`、`ui/dialog`、`ui/sheet`、`ui/drawer`（这条 ESLint 规则目前只覆盖 `src/pages/**` 与内置模块页面，不覆盖 `src/plugins/**`，插件里不会有警告，审核时人工检查）。
- **颜色**：只用语义色 token（`bg-card`、`text-muted-foreground`、`border-border`……），不要写 `#fff`、`bg-blue-500` 这类硬编码颜色，否则暗色主题会坏（ESLint 对 `src/**` 会给警告）。
- **布局**：移动优先，用逻辑类（`ms-`、`me-`、`ps-`、`pe-`、`start-`、`end-`）而不是 `ml-`、`pl-`，保证 RTL 语言也正常。手机上触控目标不小于 40px。
- **表单控件**：能用 Select 的地方不要用 Input（枚举、字典、外键、布尔、时间都有现成控件）。
- **按钮**：图标按钮必须带 `aria-label` 或 `title`，会自动生成悬浮提示。
- **权限**：按钮是否显示用权限码判断，和内置页面写法一致。
- **工作台卡片（可选）**：在 `web/cards.tsx` 导出 `cards: DashboardCard[]`（类型从 `@/framework/modules/dashboard/types` 引入），安装后自动出现在工作台，可被用户隐藏和排序。`id` 用 `<插件id>-<含义>`（只允许 `[a-z0-9-_.]`，最长 64），`title` 是 `plugin_<id>` 命名空间下的 i18n key，`perm` 填插件自己的权限码，组件用 `React.lazy` 并自己拉数据；对应的后端接口和 mock 也要有。示例见 `cron` 插件的 `cron-overview`。

### 5.3 mock（必须）

`web/<id>.mock.ts` 用 `defineMock` / `createCrudMock` 提供全部接口，让页面在没有后端时就能完整演示。框架的开发流程是前端先行，没有 mock 的插件不会通过审核。

## 6. 本地开发流程

1. 先按[快速开始](quickstart.md)派生开发项目，在项目外准备上面的插件安装包结构，或以取得的免费 cron 包为参考；自己的插件须更换 ID、权限码与表前缀，不在框架母版里写业务代码。示例里的 `nexadmin/...` 导入路径应使用派生项目的实际 Go module 路径。
2. 前端：把 `web/` 放到 `web/src/plugins/<id>/`，`npm run dev`，打开页面用 mock 数据调试。
3. 后端：把 `backend/` 放到 `plugins/<id>/`，并把包根的 `plugin.yaml` 复制到 `plugins/<id>/plugin.yaml`。目录名必须与清单 `id` 完全一致（如 `i18n-editor` 不能写成 `i18neditor`，Go 的 package 名可以不同）；管理器按 ID 查找清单和 `migrations/`。在 `plugins/plugins.go` 的标记行之间加一行 `_ "nexadmin/plugins/<id>"`（用安装命令会自动加）。
4. 用开发密钥签一份授权（pro 插件必需），放进 `data/licenses/<id>.lic`：

```bash
go run ./cmd/nexadmin plugin keygen            # 首次：生成开发密钥对
mkdir -p data/licenses                       # sign 不会创建输出父目录
go run ./cmd/nexadmin plugin sign --key .dev-keys/private.pem \
  --plugin <id> --customer 测试 --expires 2027-12-31 --out data/licenses/<id>.lic
```

   将 `keygen` 输出的 base64 公钥追加为本地 `config/license-keys.pub` 的独立一行（保留已有公钥，支持 `#` 注释），重启开发进程使其重新载入信任集合。**不修改 `core/plugin/pubkey.go`，不提交私钥，不把开发信任配置或测试授权随正式包分发。** 附加公钥文件受框架升级管理，升级后核对本地开发配置。自签授权只用于隔离的开发环境，不代替购买授权。
5. 查看磁盘状态：`go run ./cmd/nexadmin plugin list`。free 插件的 `LICENSE` 列为 `-`；pro 开发授权验签成功才显示 `active`。此命令不证明插件已编译或挂载。
6. 重新构建并启动后，由平台超管在「系统管理 → 插件管理」核对运行时状态、日志与实际接口；普通租户只分配业务使用权限。

## 7. 打包与安装验证

```bash
zip -r cron-1.0.0.zip cron/        # 目录名 = 插件 id；包内不要带 node_modules、.git、授权文件、密钥
```

在一个干净的派生项目里验证安装（使用实际包文件名与插件 ID 替换下例）：

```bash
go run ./cmd/nexadmin plugin install cron-1.0.0.zip
go build ./... && go vet ./... && go test ./plugins/<id>/...
cd web && npx tsc --noEmit && npx eslint src
```

覆盖安装使用 `go run ./cmd/nexadmin plugin install --force <目录或zip>`；`--force` 必须在路径之前（安装子命令使用标准 `flag.Parse`，不解析路径之后的选项）。把已安装插件重新打成发行包：`go run ./cmd/nexadmin plugin pack [--out <路径.zip>] <id>`（产出 `plugin.yaml + backend/ + web/` 的 zip，与 install 输入格式互逆；`--out` 同样要在位置参数之前）。`release pack` 打的是框架包。`plugin list` 读取磁盘清单与离线授权，不证明 Go 空导入、迁移和运行时挂载成功；还需核对 `plugins/plugins.go`、启动日志与运行时 `GET /api/v1/plugins`。

安装命令会拒绝路径穿越的压缩包，并限制解压后的文件数量和总大小。卸载与停用：

- `plugin remove <id>`：删除 `plugins/<id>/`、`web/src/plugins/<id>/` 并从 `plugins.go` 摘掉导入；**数据表与授权文件保留**，重装后数据原样回来。重新编译并重启生效。
- `plugin remove --purge <id>`：在 remove 之外先连数据库清理——DROP `plg_<id>_%` 前缀的表（平台插件清平台库，`migration_target: tenant` 插件清各租户业务库），并按清单的菜单码删除 `sys_menu` 记录；清单缺失时表仍按前缀清、菜单需手动处理。授权文件保留。数据库清不掉会中止且不删文件，可重试。
- 临时停用（不删代码不动数据）：`config/config.yaml` 里 `disabled_plugins: [<id>]`，重启后该插件完全不挂载，插件管理页显示"已停用"，`plugin list` 显示 `disabled`；free 插件的软停用就走这里（pro 插件也可用，不必撤授权）。从配置移除并重启即恢复。

### 官网插件市场（website 插件）

`config/plugin-market.yaml` 同时驱动后台「插件市场」与官网：

- `/plugins`：搜索、分类、免费/专业筛选；`/plugins/<id>`：详情、安装方式与授权说明。
- `/plugins/<id>/download`：服务端按目录的 `url` 直链或 GitHub `repo`/`ref` 拉源码，用 `subdir` 定位插件，校验包内 `plugin.yaml` 的 `id`/`edition`/`version` 后，重打成 `plugin.yaml + backend/ + web/` 的安装包。产物磁盘缓存 6 小时；该路由单独限流（每 IP 每分钟 30 次），响应带 `X-Checksum-SHA256`。
- `/plugins/<id>/download.sha256`：`sha256sum` 格式的校验文件，与下载产物一一对应，可用 `sha256sum -c` 校验。打包时 zip 时间戳固定为 1980-01-01，同一份源码重复打包字节一致，跨实例校验和可对比。
- `/plugins/index.json`：机器可读目录（含下载地址、安装命令、源码地址），供脚本与外部工具消费。
- 详情页的「在后台一键安装」跳转 `/admin/system/plugin?tab=market&plugin=<id>`，登录后直接落在后台市场并筛选到该插件。
- 目录的 `edition` 必须与插件包一致，否则下载被拒绝；`subdir` 只接受仓库内的相对路径。

官网与后台都直接读这份 YAML，改完即生效；`plugin market sync` 只用于导出 JSON（默认标准输出，`--docs` 写到指定目录），不再驱动官网展示。文档站的插件市场页只保留说明与入口，浏览/下载/安装统一在官网 `/plugins`。

**展示、签名与统计**

- 目录可选字段 `screenshots` / `changelog_url` / `versions`：详情页展示截图与版本历史；旧版本要能下载必须在 `versions` 填 `ref`（GitHub tag），官网用 `?version=` 取对应 tag。
- 包签名：官网进程设置 `NEXADMIN_MARKET_SIGN_KEY=<PKCS8 PEM 私钥>` 后，下载页提供 `.sig` 与公钥 `/plugins/signing-key.pub`；`nexadmin plugin verify --sig <文件> --key <公钥文件> <zip>` 校验。签名是**市场实例对最终包的 Ed25519 签名**（对 SHA-256 摘要），不是发布者签名；客户站点可以用自己的密钥。

**下载完整性校验（安装前、解压之前）**

市场安装紧接着就把下载内容交给 `go build` / `npm build`，等于在构建机上执行来路不明的源码，所以 `market.Download` 在**解压之前**必须确认字节与目录声明一致（实现见 `core/plugin/market/verify.go`）。校验来源按优先级：

| 校验来源 | 生效条件 | 取不到校验值时 |
| --- | --- | --- |
| 目录条目 `checksum`（包 zip 的 SHA-256，64 位十六进制；`versions[].checksum` 覆盖对应旧版本） | 填了就必须匹配 | ——（值本身不合法直接拒绝） |
| 目录条目 `checksum_url`（`sha256sum` 格式校验文件直链，http(s)） | 填了就必须匹配 | 拉不到或解析不出 SHA-256 一律拒绝安装 |
| 官网侧文件（`url` 指向 `<站点>/plugins/<id>/download` 时自动取 `<同路径>.sha256`） | 取到就必须匹配，无需人工填字段 | 侧文件 404 视作“无校验值”，退回默认策略 |
| 三者都没有（GitHub `repo` zipball、无侧文件的普通直链） | —— | 默认策略：放行并写显式告警（`NEXADMIN_MARKET_REQUIRE_CHECKSUM=1` 改为拒绝） |

- 默认选“告警放行”的理由：`repo` 且 `ref` 留空的条目跟随默认分支，字节每次推送都变，本质上无法预先固定校验值；直接拒绝会让现网的一键安装全部失败。放行不等于安全——它只是把“这个包没被完整性校验过”写进告警日志，而不是假装校验通过。对完整性有要求部署把 `NEXADMIN_MARKET_REQUIRE_CHECKSUM=1` 打开，或给条目补 `checksum`/`checksum_url`、把下载改走官网端点。
- 校验失败即拒绝安装：**不解压、不写 `plugins/`、不动 `plugins.go`**，下载的临时 zip 当场删除；没有 `--force` 之类的绕过口子（`--force` 只放宽清单与已安装检查）。错误文案走 i18n（`plugin.market.checksum_*`，`core/i18n/locales/{zh-CN,en-US}.yaml` 同步维护）。
- SHA-256 只回答“下载到的字节是不是我打算核对的那个包”，防的是传输与缓存被换、目录与包不一致。校验值与包来自同一下载通道时它**不能证明发布者身份**；来源认证仍是 Ed25519 签名（`.sig` + 固定公钥），两者互补，不要拿 SHA-256 当签名用。
- 产出侧与消费侧分开：官网 `/plugins/<id>/download` 自己拉上游源码重打包（`market.Package`）时不校验——它正是产出 `.sha256` 的一侧，没有期望值可对；校验只在客户端 `Download` 上做。
- `nexadmin plugin install <本地 zip>` 没有目录条目可比对，包的可信度由提供文件的人负责；需要校验先 `nexadmin plugin verify`。

### 官网多语言（website 插件）

**平台语言清单以 `sys_language` 为唯一真源**，官网通过 `core.Languages()` 使用它；`config.yaml` 的 `languages` 仅在表为空时播种，不维护另一份 `locales/languages.json`。

1. 由平台超管在「系统设置 → 高级 → 语言」维护 `code`（如 `ja-JP`）、`name`、`short`、`dir`、`htmlLang`、`ogLocale`、`prefix`、`dateFormat` 与默认语言。
2. 官网界面字典放在 `plugins/website/locales/<code>.json`，与现有字典保持键一致。字典是源码嵌入资源，修改后必须重新构建并部署二进制，单纯重启旧二进制不会更新。
3. 语言集合或 URL 前缀变化会改变启动时注册的路由，保存结果的 `needRestart` 会提示重启；重启后再验证导航、`hreflang`、`og:locale:alternate` 与 sitemap。

**技术文档只在 `docs/` 维护，营销内容用官网 CMS。** 技术文档 Markdown 嵌入二进制，修改后也要重新构建与部署；不要复制到 CMS 或旧 VitePress 站点维护第二份正文。文档路径绑定稳定的语言 `Code`，不绑定可编辑的 URL `prefix`：`zh-CN` 使用 `docs/` 中文源文件，英文使用 `docs/en/<slug>.md`，其余语言使用 Code 的小写主段（如 `ja-JP` → `docs/ja/<slug>.md`）。译文带 `title` / `summary` frontmatter；没有译文时回退中文源文，不把缺译误标为已翻译。调整默认语言或 URL 前缀不需要移动文档。新闻、产品介绍与营销页面在 CMS 中编辑、翻译、按语言发布，不需要为内容更新重新构建。

约定：

- 默认语言（`default: true`，且 `prefix` 必须为空）渲染在无前缀 URL；其余语言渲染在 `/<prefix>`。`?lang=<code>` 会切换并记住 cookie `nex_site_lang`，语言切换链接保留当前筛选参数。
- 没有语言 cookie 时，默认语言首页会按 `Accept-Language` 给偏好其他语言的访客显示一条可关闭的切换提示；**不自动 302**，避免搜索引擎把同一内容重复收录，响应带 `Vary: Accept-Language`。
- 后台内容（站点设置、导航、特性、页面、新闻）的译文存在各表的 `i18n_json`：`{"en-US":{"title":"…"}}`；未填字段在对应语言页面回退原文。后端只接收已配置语言与白名单字段，富文本翻译同样会 `Sanitize`。
- 编辑界面是「整表换语言」：弹窗顶部一个语言下拉决定你在编辑哪一种语言，表单只有一份，**语言数量不影响表单高度**。译文态下每个字段下方给出原文对照，富文本另有可折叠只读原文；`slug`、封面这类不随语言变化的字段置灰并标「共用」。组件在 `web/src/framework/components/i18n/`，二开可直接复用。
- 可翻译字段的白名单、长度与控件只写在 Go 里（`plugins/website/schema.go` 的 `entityViews`），前端经 `GET /plugins/website/i18n/schema/:entity` 取用；两边漂移由 `TestEntityViewsMatchWhitelist` 拦。给内容加可翻译字段只改这一处，界面不用动代码。
- 译文文档的解析、清洗、长度裁剪、完成度与按语言发布这套**算法**在 `pkg/i18n`（`Normalize` / `Translatable` / `NormalizePublish` / `Published`）。它不认识具体内容，也不认识语言清单从哪来——调用方实现一个 `Locales`（两方法：`Resolve` / `All`）即可复用。二开项目要做自己的多语言内容时用 `pkg/i18n` + `web/src/framework/components/i18n`，不要另写一套 JSON 约定；界面文案的词条体系（`core/i18n` + i18n-editor 插件）与它是两回事，不要合并。
- 新闻与页面支持**按语言发布**（`published_json`，只存非默认语言）：缺译的语言发不出去，服务端会把它降为未发布（不信客户端的完成度声明）；未发布语言的直接 URL 返回 404，且不出现在该语言的列表、`hreflang` 与 sitemap 中。该列为空表示从未按语言设置过，所有语言沿用整行 `published`，升级不改变现有可见性。导航/特性/设置没有可下线的 URL，不提供该开关。
- 批量机翻走插件自有路由 `POST /plugins/website/i18n/translate/<entity>`（按实体各挂自己的编辑权限，不把插件权限写进 `modules/ai`），复用 `core/ai` 网关，只填仍为空的字段。
- 市场目录条目用 `i18n` 映射本地化名称/简介（见上文 YAML 注释）；分类名、`待定/免费` 等占位价、日期格式由语言字典处理。
- 升级上来的老库由 `ensureSeedI18n` 只补 `i18n_json` 为空且原文仍是出厂文案的行，不覆盖已有译文。
- 统计：公开下载按次计数；评分为匿名 1-5 星，同一 IP 对同一插件只保留一条、可改分。后台一键安装不计入公开下载量。
- 私有/自建 GitHub：`NEXADMIN_MARKET_GITHUB_API` 覆盖 API 根地址（GitHub Enterprise）。
- 完整性校验：默认「有校验值就强制校验、取不到则告警放行」；`NEXADMIN_MARKET_REQUIRE_CHECKSUM=1` 改为拒绝任何取不到校验值的安装（见上文「下载完整性校验」）。

## 8. 提交与审核

提交时附一页说明：功能介绍、截图、支持的数据库、已验证与未验证的部分、定价建议。审核按下面的清单逐项检查，不通过会退回并写明原因。

**必须满足**
- 清单字段完整，`id`、权限码、表名、接口路径前缀都符合约定。
- 三种数据库的迁移都有，迁移只增不改。
- 迁移里没有 `INSERT`/`UPDATE`/`DELETE`（runner 会警告）；出厂数据与存量库的菜单结构收敛走 Seeder，不写在迁移里。
- 没有修改 `core/`、`web/src/framework/` 或任何内置模块，没有 import 内置模块或其他插件。
- 每个接口都有权限校验；按租户隔离的数据查询带租户条件。
- 没有硬编码密钥、没有向外部服务上传用户数据；对外请求有 SSRF 防护和超时。
- 前端：有 mock、中英文文案齐全、没有硬编码颜色、手机上可用、`tsc` 和 `eslint` 无 error。
- 后端：`go build`、`go vet`、`go test` 通过。
- 依赖：只用开源且许可证允许商用的依赖，使用精确版本；不引入商业授权的组件。
- 不含任何自带的授权校验、联网回传或后门代码。

**建议**
- 提供升级说明：新版本如何迁移，是否有不兼容改动。
- 写清已知限制，不要夸大功能。

## 9. 授权与销售流程

**免费插件**（`edition: free`，如 cron / codegen / imports / totp / ldap / maps / crm）：不校验授权。客户装好源码、构建、重启即可使用，不需要任何 `.lic`。

**专业插件**（`edition: pro`）：

1. 作者提交插件包 → 平台审核通过后上架到插件市场。
2. 买家购买 → 平台用官方私钥签发授权文件（绑定插件、客户名、到期时间，可选绑定域名）。
3. 客户安装源码并构建后，由平台超管在「插件管理」导入授权文件。管理器尝试激活已编译的插件：返回 `needRestart=true` 时按提示重启；`needRestart=false` 表示已生效。
4. 授权到期后插件自动下线（路由返回 403，状态 `expired`），续费后导入新授权即可恢复；被发行方吊销时状态为 `revoked`（见第 11 节）。

**`needRestart` 是什么？** 它表示该插件需要重启才能完整启用，常见于两类情况：插件在站点根路径注册页面（如官网），或参与启动期认证/身份扩展。框架不会为了“立即生效”去改动运行中的主路由，因此如实返回需要重启，而不是假装已激活。

作者不需要、也不应该自己实现授权。授权校验在框架统一进行，插件代码里不要出现任何授权相关逻辑。

> 授权校验防的是误用盗版和随手分享，不是对抗性破解——源码交付下，有能力的买家可以改掉校验代码。真正的保护来自合同、升级服务和技术支持。作者应当把价值放在持续更新和服务上。

## 10. 分成与定价（草案，非正式报价）

> **以下价格与比例均为结构示例，用于确定条款格式，不构成报价或承诺。** 正式价格、退款、分成与知识产权条款由发行方在上架前发布；发布前以购买页正式版本为准，本节的数字不得直接用于对外报价。

**定价模式**：按实例、按年订阅，含该年度内的框架兼容更新与技术支持；多租户、源码买断、OEM 分发单独议价。

**档位示例**

| 档位 | 适用特征 | 示例价（元/实例/年） |
| --- | --- | --- |
| 工具档 | 单一辅助能力，无外部服务依赖 | 486 |
| 集成档 | 对接外部系统或含运营后台 | 1,094 |
| 套件档 | 多角色协作、含完整业务流程 | 1,742 |

逐插件的示例目录价见发行方购买页草案；档位价与目录价用途不同，前者用于定价讨论，后者用于展示。

**需要发行方确认的条款项**

- 作者与平台的分成比例，以及结算周期、结算方式（示例位：`【待定：__%】`）。
- 退款与争议处理规则（示例位：`【待定：__ 天】`）。
- 作者的更新与维护义务，例如框架大版本升级后多久内适配，以及逾期未适配的处理方式。
- 作者下架插件后，已购买的买家能否继续使用和获取更新。
- 插件的知识产权归属与许可证条款。

**版本与价格变更**：已签发的授权在有效期内价格不变；续期按续期时公布的价格执行。上调价格不追溯已付款周期。

## 11. 常见问题

**能不能只做前端或只做后端？** 可以。纯前端插件没有 `backend/`，纯后端插件没有 `web/`，清单里的 `menus` 按需填写。

**插件要改内置功能怎么办？** 不允许改。需要框架提供扩展点的，请向框架提需求，不要通过修改 core 或 framework 的方式绕过去。

**框架升级后插件会坏吗？** 插件只依赖 `core/` 与 `framework/` 的公开接口。框架遵守「所有权契约」：这些接口发生不兼容变化会在升级说明里提前标注，并在 `requires_nexadmin` 的版本范围里体现。

**怎么发布新版本？** 修改 `plugin.yaml` 的 `version`，追加新的迁移文件，重新打包提交（`plugin pack`）。已发布的迁移文件不能改。上架由发行方维护 `config/plugin-market.yaml`（官网与后台直接读取，立即生效）；客户在后台「插件市场」一键安装，pro 插件由运行期授权闸门控制可用性。

**多实例部署要注意什么？** 插件没有现成的分布式锁可用，后台任务要自己防重复执行，cron 示例用数据库乐观抢占（`UPDATE ... WHERE last_slot < ?` 看影响行数），可以参照。框架层面的多实例要求与已知并发窗口以《部署与运维指南》第 10 节为准。

**已知限制（框架当前版本）**
- 域名校验已实现：授权里的 `domains` 会与当前站点域名比对（精确或 `*.example.com` 通配；站点域名取自 `site.domain` / `site.domains` / `sso.issuer`，都没配时用第一次请求的 `Host`，`license.allow_dev_hosts` 可放行本机地址）。不匹配时插件状态为 `invalid`，路由返回 403，原因显示在插件管理页。授权里没写 `domains` 表示不绑定域名。
- 授权到期会自动下线：后台定时检查（`CheckEvery`，默认每小时）加每次进入插件路由前的检查，到期的插件被标为 `expired`，路由返回 403，不需要重启；定时检查只影响插件管理页的状态刷新，请求路径上是实时判定的。
- `requires_nexadmin` 已校验：插件挂载时（以及 `nexadmin plugin install`）会按它检查框架版本，不满足则不挂载（状态 `invalid`，授权再有效也没用），`plugin install` 可用 `--force` 跳过。该项为空，或框架版本未知（开发环境没有版本号）时放行；写错而无法解析的范围视为不满足。
- 授权不联网回查，只有离线吊销：发行方用 `plugin revoke`（或授权后台导出）生成签名的吊销清单 `config/license-revoked.json`，随升级包分发；框架验签后把序列号命中的授权标为 `revoked`（路由返回 403，原因显示在插件管理页），文件变化后无需重启。这只是辅助手段，不是强保护：源码交付下买家可以删除清单或改掉校验代码；没有序列号的旧授权不能被单独吊销。仍应缩短授权有效期、按期续签；发行方私钥泄露或需要整体作废时，从公钥集合里去掉对应公钥（`config/license-keys.pub` 或内置列表），由该钥签发的授权会全部失效。
- 支持多把公钥：校验时用内置公钥加 `config/license-keys.pub`（每行一把 base64，`#` 起为注释）里追加的公钥，授权文件里的 `kid` 指明用哪一把（旧授权文件没有 `kid`，依次尝试全部公钥）。发行方轮换密钥时先追加新公钥，等旧授权都过期后再去掉旧公钥，不必改源码。内置的是开发公钥，正式发行前必须替换（`nexadmin release check` 会拦截）。
- 插件路由只有在声明「仅 API」热挂载契约后才可能在导入授权时即时挂载；当前官方插件未逐个声明，导入授权后请以返回的 `needRestart` 为准，必要时重启。声明需实现 `plugin.RuntimeRoutes` 的 `RequiresRootRoutes() bool` 并返回 `false`，且所有路由位于 `/api/v1/plugins/<id>/` 下。
- 插件的迁移、菜单写入和热挂载在真实数据库上还未完整验证，请在提交说明中写明你的验证范围。



