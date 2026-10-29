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

把名称、输出目录与 Go module 路径替换成自己的值。输出目录须不存在或为空，且不能在母版目录内部。默认不初始化 Git、不提交，也**不带任何插件**；不要为开始业务开发加 `--with-plugins` 搬入母版的开发插件与状态。

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

## 5. 获取并验证一个免费插件

基础服务免费，可选通用插件免费，业务增强插件收费。首批免费计划为 **cron / codegen / imports / totp / ldap / maps / crm**，其余插件按 pro 规划。免费计划不等于当前包已转换完成或已挂载：安装前核对市场目录、包内 `plugin.yaml` 和 Go 注册的 `Edition` 一致。若仍是 pro，应取得发行方发布的免费版本，不要改授权代码、使用 `--force` 或签测试授权冒充免费验收。LDAP / TOTP 不因清单里存在就已编译挂载。

以免费 cron 为例：从发行方提供的插件市场或独立插件源码仓库取得 **已发布的免费 cron 安装包**（`plugin.yaml + backend/ + web/`）。框架发布包及默认 `new` 工程不含 cron，不应假设包里必带示例。下面把下载文件命名为 `cron.zip` 并放在派生项目根目录：

```bash
# 先停止开发后端（所在终端 Ctrl+C）及其他服务进程
go run ./cmd/nexadmin plugin install ./cron.zip
go run ./cmd/nexadmin plugin list
sh scripts/build.sh
./bin/nexadmin-server start
```

逐项验证，不要只看安装命令退出成功：

1. `plugins/cron/plugin.yaml` 的 `edition` 是 `free`；`plugins/plugins.go` 有对应空导入，前端文件在 `web/src/plugins/cron/`。
2. `plugin list` 离线清单列出 cron；免费插件的 `LICENSE` 列为 `-`，不是 `active`。
3. 构建成功；启动日志没有迁移或插件启动错误；已初始化后运行 `./bin/nexadmin-server migrate --status` 检查迁移状态。
4. 用超管登录真实后端的 `/admin/system/plugin`，核对运行时 `GET /api/v1/plugins` 中 cron 为 `active`、`edition: free`；打开任务页，检查列表及安全测试任务的执行日志。
5. 业务使用权限按角色最小授权；插件管理权限不下放给普通租户。mock 页面、磁盘清单与运行时 `active` 都不能单独证明任务执行、真实数据库或外部服务验收完成。

后台「插件市场」也提供安装入口，但需要可写源码树、Go / Node 工具链和网络，并涉及重建与重启；只部署运行二进制的环境应在构建环境安装后重新部署。

## 6. 授权、停用与日常启停

免费插件无需 `.lic`。pro 插件取得正式授权后，由平台超管在插件管理导入，授权文件保存在 `data/licenses/<id>.lic`。源码安装仍须构建；授权导入只尝试激活已编译的插件。检查返回的 `needRestart`、状态和日志：热挂载失败、官网根路径或启动期认证扩展等情况可能需要重启，不承诺全部授权热激活。域名、有效期、版本要求也必须满足。

**正式购买条款尚未发布。** 本文不设定商业价格、退款、分成或知识产权条款；免费/pro 分类也不是法律许可证声明，购买前应取得正式条款与交付范围。

```bash
./bin/nexadmin-server status
./bin/nexadmin-server stop
./bin/nexadmin-server start
./bin/nexadmin-server restart
```

这些启停命令管理由 `start` 拉起的进程；前台 `go run` 用 Ctrl+C，systemd / 容器用其管理器。Windows 对应 `bin\nexadmin-server.exe`。

临时停用插件：在用户配置 `config/config.yaml` 的 `disabled_plugins` 完整列表中加入其 ID，然后重启；移除 ID 并重启恢复。`plugin remove <id>` 删除源码与注册导入，需重新构建并重启，保留数据库表与授权文件；不要将会清表的 `--purge` 当成普通停用。

## 7. 文档与上线检查

技术文档唯一真源是本仓库 `docs/`，包括 `docs/en/`；官网文档区渲染嵌入的 Markdown，修改后重新构建部署。产品介绍、新闻与营销页面用官网 CMS，不复制技术正文维护第二份。官网插件默认不随派生项目安装。

语言清单来自 `sys_language`；源码字典和 Markdown 更新需要重建，语言集合或 URL 路由前缀变化需要重启。文档按稳定语言 Code 定位：`zh-CN` 使用中文源，英文用 `en/`，其余用 Code 的小写主段，不按可修改的 URL 前缀移动文件。

上线前按[部署与运维指南](部署与运维指南.md)完成真实数据库迁移、备份恢复、权限与租户隔离、外部集成及部署方式验收。构建和单元测试不能替代这些检查。更多接口与权限见[功能清单](功能与权限清单.md)，插件结构见[插件开发指南](plugin-development.md)。
