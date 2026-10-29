# NexAdmin

以源码交付的后台管理框架：后端 Gin + GORM，前端 React + shadcn/ui。生产构建为单一二进制（前端产物通过 `-tags embedweb` 嵌入）。框架区与用户区在目录上分开，便于二次开发后用 `nexadmin upgrade` 升级。

## 特性（均以当前代码为准）

- 多租户：共享库 + `tenant_id`；租户额度（用户数 / 角色数 / 存储）与到期强制
- 权限：RBAC、五档数据范围、角色可分配三道校验
- 数据库：MySQL / MariaDB / PostgreSQL（各有一份迁移 SQL；真实库验证见各文档「未验证」项）
- 缓存：memory 或 redis（Valkey 兼容）
- 登录：账号密码（连续失败要求验证码、锁定）、手机号短信、第三方 OAuth2（企业微信 / 钉钉 / 飞书 / Google / GitHub / 通用 OIDC）
- SSO：内置 OIDC Provider（`modules/sso`）
- 开放平台：个人访问令牌、应用签名调用、事件目录、Webhook、运行时 OpenAPI 文档
- 文件：tus 断点续传、本地或 S3 兼容存储；异步导出中心；数据库备份（页面）与命令行恢复
- 通知：站内、邮件、短信、群机器人；AI 网关（自然语言筛选、翻译）
- 监控：服务 / 缓存 / 接口 / 健康检查 / 前端错误上报，`/metrics`（Prometheus 文本）
- 国际化（zh-CN、en-US，可增语言）、PWA、移动端适配
- 源码插件体系（离线签名授权，支持离线吊销清单）；CLI：`plugin` / `new` / `add module` / `i18n` / `upgrade` / `release`（`check`、`pack`）

## 快速开始

完整流程：[中文 Quickstart](docs/quickstart.md) · [English Quickstart](docs/en/quickstart.md)。业务开发先派生项目，不要直接改框架母版。

前置：Go 1.26+、Node 22 与 npm、一个空的 MySQL / MariaDB / PostgreSQL 数据库。以下为 Linux / macOS 或 Git Bash 命令：

```bash
# 在框架母版根目录运行；myapp 目标目录须不存在或为空
go run ./cmd/nexadmin new myapp --out ../myapp --module example.com/myapp
cd ../myapp
sh scripts/build.sh
./bin/nexadmin-server start
```

打开 `http://localhost:8080/admin/setup`，按六步向导初始化并创建超管，随后在 `/admin/login` 登录。状态与停止：`./bin/nexadmin-server status`、`./bin/nexadmin-server stop`。Windows 原生命令见 Quickstart。

开发时另开后端 `go run ./cmd/server` 和前端 `npm --prefix web run dev`，后台入口 `http://localhost:5173/admin/`。母版及默认派生项目的 `.env.development` 启用 mock；真实联调用 `VITE_MOCK=false npm --prefix web run dev`。生产构建关闭 mock。`new` 默认不带任何插件，官网也须另装并按其要求启用。

## 文档索引

| 文档 | 内容 |
| --- | --- |
| [docs/quickstart.md](docs/quickstart.md) · [English](docs/en/quickstart.md) | 派生 → 构建 → 初始化 → 开发 → 插件验证与启停 |
| [docs/部署与运维指南.md](docs/部署与运维指南.md) | 构建、部署、初始化、迁移、备份恢复、监控、安全清单、故障排查 |
| [docs/配置参考.md](docs/配置参考.md) | `config/default.yaml` 全部配置键、环境变量写法 |
| [docs/功能与权限清单.md](docs/功能与权限清单.md) | 内置模块、权限码、登录方式、开放平台、角色与租户规则 |
| [docs/二次开发指南.md](docs/二次开发指南.md) | 所有权分区、定制路径、升级流程 |
| [docs/plugin-development.md](docs/plugin-development.md) | 插件开发 |
| `nexadmin-framework-design.md` | 设计文档，位于框架仓库上一级目录，不在本源码包内；文首有「实现状态」 |

## 目录说明

| 路径 | 所有权 | 说明 |
| --- | --- | --- |
| `cmd/server` `cmd/nexadmin` | 框架区 | 服务入口 / 命令行工具 |
| `core/` `modules/` `pkg/` | 框架区 | 框架核心 / 内置模块 / 能力件 |
| `web/src/framework/` | 框架区 | 前端布局、路由壳、请求封装、内置模块页面 |
| `migrations/framework/` `config/default.yaml` | 框架区 | 框架迁移 / 默认配置 |
| `deploy/` `scripts/` `docs/` | 框架区 | 部署样例与脚本 / 构建脚本 / 文档 |
| `internal/` `web/src/pages/` `migrations/business/` | 用户区 | 业务模块 / 业务页面 / 业务迁移 |
| `config/config.yaml` `config/locales/` | 用户区 | 只写要改的配置项 / 业务翻译 |
| `plugins/` `data/` `logs/` | 用户区 | 已安装插件 / 运行时数据 / 日志 |

所有权的权威来源是 `.nexadmin.manifest.json`。

## 获取、许可与支持

- 基础服务免费；首批可选免费插件计划为 cron、codegen、imports、totp、ldap、maps、crm，其余业务增强插件按 pro 规划。以实际发布包的清单与注册信息核对交付状态，框架包不默认附带插件。
- 正式购买条款尚未发布，本文不承诺价格、退款、分成或知识产权条款。源码许可请直接阅读随交付物提供的 [LICENSE](LICENSE)，不从免费/pro 分类推导插件的具体许可范围。
- 先查上述文档和启动日志；反馈问题时提供版本、复现步骤与脱敏日志。支持渠道以取得源码的发行渠道所公布信息为准，此处不提供未经确认的联系方式。
