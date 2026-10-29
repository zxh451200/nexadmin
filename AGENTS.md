# AGENTS.md — NexAdmin 框架母版仓库

> 本文件写给在本仓库（框架母版）里工作的开发者和 AI agent。
> **如果你的目标是做业务二次开发：不要在本仓库里写业务代码。** 先运行
> `go run ./cmd/nexadmin new myapp --out ../myapp --module example.com/myapp`
> 派生一个项目，然后在派生项目里按其根目录的 AGENTS.md 工作。派生项目的目录所有权、升级流程见 `docs/二次开发指南.md`。

## 1. 本仓库是什么

NexAdmin 框架母版：后端 Gin + GORM、前端 React + shadcn/ui，生产构建为单一二进制。目录分两层：

- **框架区**：`core/`（框架内核）、`modules/`（内置业务模块）、`pkg/`、`cmd/`（server 与 nexadmin CLI）、`web/src/framework/`、`migrations/framework/`、`docs/`、`deploy/`、`scripts/`。
- **插件**：`plugins/`（源码插件，随框架编译；规范见 `docs/plugin-development.md`）。

## 2. 在本仓库工作的规则

1. 改框架代码前先读 `docs/二次开发指南.md` 的第 3 节——很多"框架缺能力"的需求其实已有扩展点（事件、job、限流、数据范围、覆盖页面、替代模块）。
2. 新能力优先做成扩展点而不是硬编码：能通过 `core.Deps`、Option、注册接口暴露的，不要让调用方改核心文件。
3. **迁移只增不改**：`migrations/framework/` 已执行的 SQL 永不修改，结构变更写新编号文件（mysql/mariadb/postgres 三份同步）。
4. 内置表前缀 `sys_`，插件表 `plg_<id>_`，业务表 `biz_`；权限码 `模块:动作`。
5. 文案一律走 i18n：前端 `web/src/locales/{zh-CN,en-US}.json` 双份同步；后端错误 key 写 `config/locales/<lang>.yaml`。
6. 前端只用语义 token 与 framework 组件（DataTable/FormLayout/ResponsiveDialog 等），移动优先，逻辑方向类（ms-/me-/ps-/pe-）。
7. `plugins/website`（官网）与 `plugins/` 其他插件同样受以上约束；官网文档区渲染 `docs/*.md`（含 `docs/en/` 译文），改文档重新构建即更新。

## 3. 完成标准

每次改动后：

```
go build ./... && go vet ./... && go test ./...
cd web && npx tsc --noEmit && npx eslint src
```

涉及迁移时补跑 `nexadmin-server migrate --dry-run`；涉及官网时截图验证浅色/深色两套主题。

## 4. 常用命令

| 命令 | 用途 |
| --- | --- |
| `go run ./cmd/server` | 前台起后端（dev） |
| `go run ./cmd/nexadmin new/add module/upgrade/...` | 派生项目与脚手架 |
| `go run ./cmd/nexadmin plugin list/install/pack/lint` | 插件管理 |
| `sh scripts/build.sh` | 生产构建（嵌入前端） |
| `bin/nexadmin-server start/stop/status` | 后台启停 |

## 5. 文档索引

`docs/二次开发指南.md`（二开总纲）、`docs/quickstart.md`（15 分钟上手）、`docs/plugin-development.md`（插件开发）、`docs/配置参考.md`、`docs/部署与运维指南.md`、`docs/功能与权限清单.md`、`core/README.md`（框架内核能力地图）。
