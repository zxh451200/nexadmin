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

## 5. 安装和使用插件

### 免费版插件（立即生效）

**什么是免费插件？** 定时任务 (cron)、代码生成器 (codegen)、数据导入 (imports) 等通用能力都免费提供，随框架一起开源。它们不需要授权文件，也不需要额外费用。

**安装步骤（以 cron 定时任务为例）：**

#### 方式一：通过命令行（适合开发者）

```bash
# 前提：先下载免费插件包（可从我们的 GitHub 仓库获取）
# 假设你已把 cron.zip 放到项目根目录

# 1. 停止正在运行的后端服务
./bin/nexadmin-server stop  # 或按 Ctrl+C

# 2. 安装插件（自动写入源码树）
go run ./cmd/nexadmin plugin install ./cron.zip

# 3. 重新编译（约 30 秒）
sh scripts/build.sh

# 4. 重启服务
./bin/nexadmin-server start
```

#### 方式二：通过 Web 界面（推荐生产环境）

如果你已经启动了开发模式（`go run ./cmd/server`），可以直接访问 `http://localhost:8080/admin/system/plugin` → 「插件市场」标签页：

1. 找到「定时任务」卡片（会标注「免费」）
2. 点击「安装」按钮
3. 等待进度条显示「安装成功」（自动完成编译和重启）
4. 刷新页面，你会看到 cron 的状态为「已启用」

**✅ 安装后如何验证？**

- **后台查看**：访问 `/admin/system/plugin`，确认 cron 状态为 `active`
- **功能测试**：访问 `/admin/plugins/cron/jobs`，新建一个 5 分钟执行的定时任务
- **日志确认**：查看 `logs/nexadmin.out`，应该能看到调度器启动的日志

---

### 专业版插件（需授权）

**什么是专业版插件？** workflown 工作流引擎、数据审计 (audit)、支付集成 (pay) 等高级功能需要单独购买授权。

**安装流程：**

1. **购买授权**：联系发行方获取 license 文件（`.lic`）
2. **安装插件**：先按上面的「安装插件」步骤安装源码
3. **导入授权**：在「插件管理」→ 选择对应的 pro 插件 → 粘贴 license 内容
4. **检查状态**：如果提示「需要重启」，执行 `./bin/nexadmin-server restart`

**⚠️ 重要说明**

- 免费版插件**不需要授权文件**，无需处理 license
- Pro 版插件导入授权后可能提示 `needRestart=true`，这表示该插件涉及根路由或认证扩展，必须重启才能完全启用。其他 pro 插件导入授权后会立即生效。
- **域名绑定**：如果你的插件授权绑定了特定域名（如 `example.com`），实际访问时必须使用这个域名，否则授权会失效。
- **有效期**：所有 Pro 授权都有到期时间（如 `2027-12-31`），到期后会自动禁用。续期时需要重新导入新的 license。

---

**📦 关于授权的 FAQ**

| 问题 | 回答 |
|---|---|
| Q: 为什么我的插件提示需要重启？ | A: 该插件使用了框架的根路由或身份认证扩展，这些能力必须在服务器启动时加载，因此需要重启。其他插件导入授权后立即生效。 |
| Q: 如何知道哪些是免费的，哪些要收费？ | A: 在「插件市场」里，免费插件会标注「免费」，Pro 插件会标注「待定」或具体价格。 |
| Q: 授权可以转让吗？ | A: 每个授权绑定一个客户名称和域名，原则上不可转让。如需更换域名，请联系发行方重发授权。 |
| Q: 试用版怎么搞？ | A: 目前暂不提供自动试用机制。如果需要评估 Pro 功能，请联系发行方获取临时授权。 |

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
