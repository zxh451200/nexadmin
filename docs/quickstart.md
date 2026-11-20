---
title: 快速开始
summary: 从派生项目、构建与初始化，到真实后端开发、免费插件安装验证和日常启停。
---

# NexAdmin 快速开始

业务开发从派生项目开始，**不要在框架母版里写业务代码**。本文命令除第一步外均在派生项目根目录运行；需要 Go 1.26+、Node 22 与 npm，以及可连接的空 MySQL / MariaDB / PostgreSQL 数据库。首次安装依赖需要网络。

## 1. 派生自己的工程

在拿到的框架母版根目录执行（下例为 Linux / macOS / Git Bash）：

```bash
go run ./cmd/nexadmin new myapp --out ../myapp --module example.com/myapp
cd ../myapp
```

把名称、输出目录与 Go module 路径替换成自己的值。输出目录须不存在或为空，且不在母版目录内部。默认不初始化 Git、不提交；除 `totp`（两步验证，安全基线）外**不带其他插件**，不需要 MFA 可加 `--without-totp`。

挑插件有三种写法：

| 写法 | 带过去的东西 |
| --- | --- |
| 默认 | 只带 `totp` |
| `--plugins cron,codegen` | 点名的插件连同 `web/src/plugins/<id>` 一起下发、写回 `plugins/plugins.go`；点错（模板里没有）直接报错 |
| `--plugins basic` | 全部 free 插件，开箱即用不需要授权 |
| `--plugins standard` | basic + 报表 / 审计 / 安全三件 pro，随包编译，导入授权即生效 |
| `--with-plugins` | 母版全部插件，只在照搬母版时用 |

先读生成项目的 `AGENTS.md` 与 `.nexadmin.manifest.json`。业务后端放 `internal/`，页面放 `web/src/pages/`，迁移放 `migrations/business/`，配置只覆盖 `config/config.yaml`；升级边界见[二次开发指南](二次开发指南.md)。

## 2. 构建可打开初始化向导的服务

```bash
sh scripts/build.sh
./bin/nexadmin-server start
```

构建脚本先执行 `npm ci`、前端构建，再用 `-tags embedweb` 编译，产物为 `bin/nexadmin-server`。Windows 原生终端用以下命令替代（不是 Bash 语法）：

```bat
scripts\build.bat
bin\nexadmin-server.exe start
```

在项目根目录启动，而不是进入 `bin/`：配置、迁移、插件清单与数据路径依赖项目根目录。`go run ./cmd/server` 不嵌入后台 SPA，不能单独用它在 8080 打开向导页面。

## 3. 初始化并登录

打开 `http://localhost:8080/admin/setup`（端口以配置和启动输出为准）。向导依次为：**环境检查 → 数据库 → 缓存 → 管理员与站点 → 可选配置 → 确认执行**。

- 填入真实数据库连接并测试；单实例体验可选 memory 缓存，多实例需共享 Redis / Valkey。
- 设置自己的超管账号与强密码，没有通用默认登录密码。S3、AI 等可选配置可以先留空；没有配置外部服务不等于这些集成已验收。
- 执行时写入配置、迁移、种子、管理员与初始化标记。完成后到 `/admin/login` 用刚创建的账号登录，检查工作台、用户与角色页面。
- 默认只允许本机直连初始化；代理转发不算本机。远程向导需显式允许并提供控制台一次性令牌，或使用 `init --config`，见[部署与运维指南](部署与运维指南.md)。不要为方便初始化直接向公网开放服务。

初始化会写演示数据，上线前按需清理。平台全局管理（系统设置、语言、全局菜单、插件安装与授权、备份、租户与套餐等）只交给平台超管，不给普通租户角色分配这些管理权限；上线前还须验证非超管直接调用接口被拒绝，不能只检查菜单隐藏。

## 4. 开发：分清 mock 与真实后端

先停止上一节的后台进程，避免端口冲突：

```bash
./bin/nexadmin-server stop
go run ./cmd/server
```

另开一个终端，在同一项目根目录运行：

```bash
VITE_MOCK=false npm --prefix web run dev
```

打开 `http://localhost:5173/admin/`。Vite 将 `/api` 代理到 8080；这时使用真实数据库与刚创建的账号。PowerShell 可用 `$env:VITE_MOCK='false'; npm --prefix web run dev`。

**默认差异：**母版和默认派生项目的 `web/.env.development` 都是 `VITE_MOCK=true`，直接 `npm --prefix web run dev` 使用模拟接口，适合先做页面，不能证明数据库、权限或插件挂载有效。只有显式值 `true` 才开启 mock；生产 `.env.production` 为 `false`，构建时也不要用环境变量覆盖成 `true`。无需修改框架的 `.env.development` 来联调。

生成一个业务模块骨架后再补逻辑：

```bash
go run ./cmd/nexadmin add module article --fields "title:string,status:dict(sys_status)"
```

新增后端模块后重启开发后端，按迁移策略处理新迁移。开发完成的检查：

```bash
go build ./... && go vet ./... && go test ./...
(cd web && npx tsc --noEmit && npx eslint src)
```

## 5. 安装和使用插件

### 免费插件

定时任务、代码生成器、导入中心这类通用能力随框架开源，不需要授权文件。以定时任务为例走一遍。

> [!NOTE]
> 母版与官方发行包默认编译 `totp`（两步验证），`nexadmin new` 也会带上；其余免费插件按需安装。`ldap` 与 `totp` 不冲突——前者校验密码，后者在登录成功后要动态码。但**不要同时启用 `ldap` 和 `identity` 的目录登录源**：两者都注册外部认证器，只会有一个生效，配了却不生效最难查。

#### 命令行安装

```bash
# 前提：cron.zip 已经在项目根目录（从发行包或仓库获取）

./bin/nexadmin-server stop                # 1. 停服务
go run ./cmd/nexadmin plugin install ./cron.zip   # 2. 解包并写回源码树
sh scripts/build.sh                       # 3. 重新编译，约 30 秒
./bin/nexadmin-server start               # 4. 启动
```

#### 在插件市场安装

已经跑在开发模式（`go run ./cmd/server`）时，也可以直接打开 `http://localhost:8080/admin/system/plugin` → 「插件市场」：

1. 找到「定时任务」卡片（标着「免费」）
2. 点「安装」，等进度条走完
3. 刷新页面，状态变成「已启用」

#### 验证装好了

- 插件管理页里 cron 状态是 `active`
- 打开 `/admin/plugins/cron/jobs`，新建一个 5 分钟后执行的任务
- `logs/nexadmin.out` 里能看到调度器启动的日志

### 专业插件

工作流、数据审计、支付中台这类能力需要单独授权。

1. **拿授权**：向发行方获取 license 文件（`.lic`）
2. **装源码**：按上面的步骤把插件包装进来
3. **导授权**：插件管理页选中对应插件，粘贴 license 内容
4. **重启**：提示需要重启时，执行 `./bin/nexadmin-server restart`

> [!WARNING]
> 授权只激活**已经编译进二进制**的插件。另外三种情况会让授权失效：实际访问的域名与授权绑定的不一致、授权过期（到期自动禁用）、`requires_nexadmin` 版本范围不匹配。提示 `needRestart=true` 的插件涉及根路由或启动期认证扩展，必须重启才能完全启用。

#### 授权常见问题

| 问题 | 回答 |
|---|---|
| 为什么提示需要重启？ | 该插件使用了框架的根路由或身份认证扩展，这些能力必须在服务启动时加载。其他插件导入授权后立即生效。 |
| 怎么知道哪些免费、哪些收费？ | 插件市场里免费插件标「免费」，专业插件标价格或「待定」。 |
| 授权可以转让吗？ | 每个授权绑定一个客户名称和域名，原则上不可转让。换域名需联系发行方重发。 |
| 能试用吗？ | 目前没有自动试用机制。要评估专业功能，联系发行方拿临时授权。 |

## 6. 授权、停用与日常启停

免费插件不需要 `.lic`；专业插件由平台超管在「插件管理」导入授权，文件保存在 `data/licenses/<id>.lic`。源码安装仍需构建，授权导入只激活已编译的插件：返回 `needRestart=true` 时按提示重启，不承诺所有插件都能热激活。域名绑定、有效期与 `requires_nexadmin` 版本范围必须同时满足。

免费 / pro 只是运行期分类，不是法律许可声明；价格、退款与知识产权条款不在这份文档里定义。

```bash
./bin/nexadmin-server status
./bin/nexadmin-server stop
./bin/nexadmin-server start
./bin/nexadmin-server restart
```

这些启停命令管理由 `start` 拉起的进程；前台 `go run` 用 Ctrl+C，systemd / 容器用其管理器。Windows 对应 `bin\nexadmin-server.exe`。

临时停用插件：在用户配置 `config/config.yaml` 的 `disabled_plugins` 完整列表中加入其 ID，然后重启；移除 ID 并重启恢复。`plugin remove <id>` 删除源码与注册导入，需重新构建并重启，保留数据库表与授权文件；不要将会清表的 `--purge` 当成普通停用。`--purge` 会连同插件表与迁移记录一起清理，必须先停服并备份数据库。

## 7. 文档与上线检查

技术文档唯一真源是本仓库 `docs/`，包括 `docs/en/`；官网文档区渲染嵌入的 Markdown，修改后重新构建部署。产品介绍、新闻与营销页面用官网 CMS，不复制技术正文维护第二份。官网插件默认不随派生项目安装。

语言清单来自 `sys_language`；源码字典和 Markdown 更新需要重建，语言集合或 URL 路由前缀变化需要重启。文档按稳定语言 Code 定位：`zh-CN` 使用中文源，英文用 `en/`，其余用 Code 的小写主段，不按可修改的 URL 前缀移动文件。

上线前按[部署与运维指南](部署与运维指南.md)完成真实数据库迁移、备份恢复、权限与租户隔离、外部集成及部署方式验收。构建和单元测试不能替代这些检查。更多接口与权限见[功能与权限清单](features.md)，插件结构见[插件开发指南](plugin-development.md)。
