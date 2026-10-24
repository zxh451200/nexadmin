# NexAdmin 插件开发指南

本文教你从零写一个 NexAdmin 插件，并打包提交到插件市场。全文以官方示例插件「定时任务 cron」为例，它的完整源码在发行包 `nexadmin-plugins/cron/`，边读边对照最快。

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
edition: pro                 # free | pro
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
        ID: "cron", Name: "定时任务", Version: "1.0.0", Edition: plugin.Pro,
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

### 4.5 测试

后端至少带一个单测，覆盖你最容易出错的纯逻辑（cron 示例测了表达式解析、下次执行时间、SSRF 拦截）。需要真实数据库的部分如果没法在单测里跑，请在提交说明里写明。

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

1. 在框架仓库里按上面的结构建一个目录，或直接复制 `nexadmin-plugins/cron/` 改名。
2. 前端：把 `web/` 放到 `web/src/plugins/<id>/`，`npm run dev`，打开页面用 mock 数据调试。
3. 后端：把 `backend/` 放到 `plugins/<id>/`，并把包根的 `plugin.yaml` 复制到 `plugins/<id>/plugin.yaml`。目录名必须与清单 `id` 完全一致（如 `i18n-editor` 不能写成 `i18neditor`，Go 的 package 名可以不同）；管理器按 ID 查找清单和 `migrations/`。在 `plugins/plugins.go` 的标记行之间加一行 `_ "nexadmin/plugins/<id>"`（用安装命令会自动加）。
4. 用开发密钥签一份授权（pro 插件必需），放进 `data/licenses/<id>.lic`：

```bash
go run ./cmd/nexadmin plugin keygen            # 首次：生成开发密钥对
go run ./cmd/nexadmin plugin sign --key .dev-keys/private.pem \
  --plugin <id> --customer 测试 --expires 2027-12-31 --out data/licenses/<id>.lic
```

   注意：开发私钥只在你本机有效。框架内置的公钥是官方的，用你自己生成的私钥签的授权，需要把 `core/plugin/pubkey.go` 里的公钥换成你自己的才能在本地通过校验。**不要把私钥提交到仓库。**
5. 查看状态：`go run ./cmd/nexadmin plugin list`，状态应为 `active`。
6. 后台「系统管理 → 插件管理」可以看到插件和授权状态。

## 7. 打包与安装验证

```bash
zip -r cron-1.0.0.zip cron/        # 目录名 = 插件 id；包内不要带 node_modules、.git、授权文件、密钥
```

在一个干净的框架副本里验证安装：

```bash
go run ./cmd/nexadmin plugin install cron-1.0.0.zip
go build ./... && go vet ./... && go test ./plugins/<id>/...
cd web && npx tsc --noEmit && npx eslint src
```

覆盖安装使用 `go run ./cmd/nexadmin plugin install --force <目录或zip>`；`--force` 必须在路径之前（安装子命令使用标准 `flag.Parse`，不解析路径之后的选项）。CLI 没有 `plugin pack` 子命令；源码包按上面的 zip 结构制作，`release pack` 打的是框架包。`plugin list` 读取磁盘清单与离线授权，不证明 Go 空导入、迁移和运行时挂载成功；还需核对 `plugins/plugins.go`、启动日志与运行时 `GET /api/v1/plugins`。

安装命令会拒绝路径穿越的压缩包，并限制解压后的文件数量和总大小。卸载用 `plugin remove <id>`（默认保留数据表）。

## 8. 提交与审核

提交时附一页说明：功能介绍、截图、支持的数据库、已验证与未验证的部分、定价建议。审核按下面的清单逐项检查，不通过会退回并写明原因。

**必须满足**
- 清单字段完整，`id`、权限码、表名、接口路径前缀都符合约定。
- 三种数据库的迁移都有，迁移只增不改。
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

1. 作者提交插件包 → 平台审核通过后上架到插件市场。
2. 买家购买 → 平台用官方私钥签发授权文件（绑定插件、客户名、到期时间，可选绑定域名）。
3. 买家在「插件管理」导入授权文件，立即生效，无需重新编译。
4. 授权到期后插件自动下线（路由返回 403，状态 `expired`），续费后导入新授权即可恢复。授权被发行方吊销时状态为 `revoked`（见第 11 节）。

作者不需要、也不应该自己实现授权。授权校验在框架统一进行，插件代码里不要出现任何授权相关逻辑。

> 授权校验防的是误用盗版和随手分享，不是对抗性破解——源码交付下，有能力的买家可以改掉校验代码。真正的保护来自合同、升级服务和技术支持。作者应当把价值放在持续更新和服务上。

## 10. 分成与定价（待平台方确定）

以下条款**尚未确定**，由平台方在上线插件市场前补充，此处只列需要明确的项目：

- 作者与平台的分成比例，以及结算周期、结算方式。
- 定价模式：一次性买断、按年订阅，还是按租户或按实例计价。
- 退款与争议处理规则。
- 作者的更新与维护义务（例如框架大版本升级后多久内适配），逾期未适配的处理方式。
- 作者下架插件后，已购买的买家能否继续使用和获取更新。
- 插件的知识产权归属与许可证条款。

## 11. 常见问题

**能不能只做前端或只做后端？** 可以。纯前端插件没有 `backend/`，纯后端插件没有 `web/`，清单里的 `menus` 按需填写。

**插件要改内置功能怎么办？** 不允许改。需要框架提供扩展点的，请向框架提需求，不要通过修改 core 或 framework 的方式绕过去。

**框架升级后插件会坏吗？** 插件只依赖 `core/` 与 `framework/` 的公开接口。框架遵守「所有权契约」：这些接口发生不兼容变化会在升级说明里提前标注，并在 `requires_nexadmin` 的版本范围里体现。

**怎么发布新版本？** 修改 `plugin.yaml` 的 `version`，追加新的迁移文件，重新打包提交。已发布的迁移文件不能改。

**多实例部署要注意什么？** 插件没有现成的分布式锁可用，后台任务要自己防重复执行，cron 示例用数据库乐观抢占（`UPDATE ... WHERE last_slot < ?` 看影响行数），可以参照。框架层面的多实例要求与已知并发窗口以《部署与运维指南》第 10 节为准。

**已知限制（框架当前版本）**
- 域名校验已实现：授权里的 `domains` 会与当前站点域名比对（精确或 `*.example.com` 通配；站点域名取自 `site.domain` / `site.domains` / `sso.issuer`，都没配时用第一次请求的 `Host`，`license.allow_dev_hosts` 可放行本机地址）。不匹配时插件状态为 `invalid`，路由返回 403，原因显示在插件管理页。授权里没写 `domains` 表示不绑定域名。
- 授权到期会自动下线：后台定时检查（`CheckEvery`，默认每小时）加每次进入插件路由前的检查，到期的插件被标为 `expired`，路由返回 403，不需要重启；定时检查只影响插件管理页的状态刷新，请求路径上是实时判定的。
- `requires_nexadmin` 已校验：插件挂载时（以及 `nexadmin plugin install`）会按它检查框架版本，不满足则不挂载（状态 `invalid`，授权再有效也没用），`plugin install` 可用 `--force` 跳过。该项为空，或框架版本未知（开发环境没有版本号）时放行；写错而无法解析的范围视为不满足。
- 授权不联网回查，只有离线吊销：发行方用 `plugin revoke`（或授权后台导出）生成签名的吊销清单 `config/license-revoked.json`，随升级包分发；框架验签后把序列号命中的授权标为 `revoked`（路由返回 403，原因显示在插件管理页），文件变化后无需重启。这只是辅助手段，不是强保护：源码交付下买家可以删除清单或改掉校验代码；没有序列号的旧授权不能被单独吊销。仍应缩短授权有效期、按期续签；发行方私钥泄露或需要整体作废时，从公钥集合里去掉对应公钥（`config/license-keys.pub` 或内置列表），由该钥签发的授权会全部失效。
- 支持多把公钥：校验时用内置公钥加 `config/license-keys.pub`（每行一把 base64，`#` 起为注释）里追加的公钥，授权文件里的 `kid` 指明用哪一把（旧授权文件没有 `kid`，依次尝试全部公钥）。发行方轮换密钥时先追加新公钥，等旧授权都过期后再去掉旧公钥，不必改源码。内置的是开发公钥，正式发行前必须替换（`nexadmin release check` 会拦截）。
- 插件的迁移、菜单写入和热挂载在真实数据库上还未完整验证，请在提交说明中写明你的验证范围。



