---
title: 内置模块清单
summary: 内置模块与插件各能做什么、入口在哪、依赖什么。
---

## 怎么读这篇

- **模块名**就是 `config` 里 `disabled_modules` 写的名字（模块的 `Name()`）。
- 权限码缩写沿用本行模块前缀：user 行的 `create` 指 `user:create`。逐条权限与接口见[权限码与接口对照](features-permissions.md)。
- 后台路径相对 `/admin`：`/system/user` 的完整入口是 `/admin/system/user`。

> [!TIP]
> 想找「哪个页面管什么」看[后台功能总览](后台功能总览.md)；想查「某个权限码覆盖了哪些接口」看[权限码与接口对照](features-permissions.md)。

## 哪个模块在哪

按后台菜单顺序列出。想知道细节就去下面「模块细节」按名字找。

| 模块 | 入口 | 一句话作用 |
| --- | --- | --- |
| `dashboard` | 工作台 `/dashboard` | 统计卡片与趋势；每个人可自己调整布局 |
| `user` | 系统管理 → 用户管理 | 账号增删改、导入导出、重置密码；也是登录提供者 |
| `role` | 系统管理 → 角色管理 | 分配菜单、按钮权限与数据范围 |
| `menu` | 系统管理 → 菜单管理 | 三级菜单树；平台级 |
| `dept` | 系统管理 → 部门管理 | 组织树与岗位；支持跨级拖动 |
| `post` | 部门管理 → 岗位 | 岗位增删改与导出 |
| `dict` | 系统管理 → 字典管理 | 下拉选项的统一定义；平台级 |
| `config` | 系统设置 → 高级 → 参数 | 参数 KV 与上传限制；平台级 |
| `language` | 系统设置 → 高级 → 语言 | 语言清单整表保存；平台级 |
| `recycle` | 系统管理 → 回收站 | 还原或彻底删除软删记录 |
| `plugin` | 开发与安全 → 插件管理 | 安装、授权、启停；部分操作需重启 |
| `backup` | 系统管理 → 数据备份 | 手动/定时备份与下载；仅超管 |
| `openapi` | 开发与安全 → 开放平台 | 应用、Webhook、投递记录、接口文档 |
| `sso` | 系统设置 → 安全与登录 → 单点登录 | 本站作为 OIDC Provider；入口还有应用中心 `/portal` |
| `notice` | 系统管理 → 通知公告 | 富文本公告、定时发布、已读统计 |
| `message` | 顶栏铃铛 → 消息中心 `/message` | 个人消息、未读数、SSE 推送 |
| `file` | 系统管理 → 文件管理 | 分组、断点续传、公开/签名下载 |
| `exportcenter` | 导入导出 → 导出中心 `/jobs` | 异步导出任务的下载、取消与重试 |
| `log` | 系统监控 → 操作日志 / 登录日志 | 查询、清理、导出 |
| `sendlog` | 系统监控 → 发送日志 | 短信/邮件/机器人投递结果与失败原因 |
| `online` | 系统监控 → 在线用户 | 在线会话与强制下线 |
| `server-monitor` | 系统监控 → 服务监控 | CPU/内存/磁盘/Go 运行时，带「前端错误」页签 |
| `client-error` | 服务监控 → 前端错误页签 | 前端 JS 报错上报（匿名，有 IP 限流） |
| `cache-monitor` | 系统监控 → 缓存监控 | Redis/Valkey 内存、命中率、key 数 |
| `api-monitor` | 系统监控 → 接口监控 | 最近 30 秒的 QPS、耗时、错误率 |
| `tenant` | 租户管理 → 租户管理 / 租户套餐 | 租户与套餐；租户管理员看「我的租户」 |
| `profile` | 个人中心 `/profile` | 资料、改密、绑定、登录设备、访问令牌 |
| `about` | 关于 `/about` | 版本、构建时间、依赖许可证 |
| `region` | 无菜单 | 省市区数据与同步 |
| `ai` | 无菜单 | AI 状态、自然语言筛选、翻译、写作辅助 |

## 平台级数据：谁能改

有些配置是**所有租户共用一行**——表里根本没有 `tenant_id`。所以「能用这个模块」和「能改这一行」是两件事：权限点只回答前者。

判据是 `core.IsPlatformCaller`（`core/router.go:51`）：**超管，或请求落在默认租户**，单源是 `tenant.IsPlatformCallerID`，随会话以 `auth.User.Platform` 下发给前端。在这之外，`core.RequirePlatform()`（`core/router.go:63`）给 **55 条写路由**又加一道闸——权限点和这道闸都要过。

| 这些写路由 | 条数 | 代码位置 |
| --- | --- | --- |
| 工单共享配置：快捷回复 / SLA 与命名策略 / 分组与成员 / 自动化规则 / 自定义字段 / 插件设置 | 17 | `plugins/ticket/handler.go` |
| 代码生成器：安装与保存配置 | 4 | `plugins/codegen/plugin.go` |
| 参数与设置分区 | 8 | `modules/system/config/module.go` |
| 字典类型与字典项（含两处排序） | 10 | `modules/system/dict/module.go` |
| 菜单（含拖拽排序与移动） | 6 | `modules/system/menu/module.go` |
| 语言清单整表保存 | 1 | `modules/system/language/module.go` |
| 区划 `preview`/`import`/`sync`/`reset`/`rollback` | 5 | `modules/region/module.go` |
| LDAP / 地图 / 媒体三个凭据页 | 4 | — |

**租户管理员会看到什么**：即使持有 `ticket:edit`、`dict:update`、`menu:update`、`settings:update`，保存仍回 403，错误 key 是 `error.platform_only`。文案直接说明「这一行所有租户共用，只有平台管理员能改」，不会让人回头翻自己的角色。前端把写按钮置灰并显示一行 `<PlatformNotice />` 说明，不藏入口。

- 单租户部署里人人都落在默认租户 = 平台调用方，这条闸不改变任何行为，只在多租户下生效。
- 备份（`superOnly()`）与租户 / 套餐（分组级 `RequireSuper`）用的是更强的闸，不在这 55 条里；「平台级」一格不等于三者都挂了 `RequirePlatform`。
- 想把这些配置开放给租户自助，得先做租户化改造（三引擎迁移补 `tenant_id` + 唯一键改形）。这是需要先做的设计决定，不是排期待办。
- 哪条路由额外挂了闸，[权限码与接口对照](features-permissions.md)里逐行标了「另要求平台级」。

## 系统设置的 13 个分区

| 分组 | 分区（`?tab=`） | 谁在提供接口 |
| --- | --- | --- |
| 站点 | 站点信息 `site` | `config` 模块的设置接口 |
| 安全与登录 | 登录安全 `security`、登录方式 `auth`、LDAP `ldap`、单点登录 `sso` | 前两个走 settings；LDAP 走 `/plugins/ldap/*`；单点登录走 `/sso/*` |
| 存储与集成 | 文件存储 `storage`、AI 模型 `ai`、短信 `sms`、通知 `notify`、地图 `maps` | 前四个走 settings；地图走 `/plugins/maps/*` |
| 高级 | 语言 `language`、参数 `params` | 语言走 `/languages`；参数走 `/configs` |
| 运行时 | 缓存 `cache`（只读） | settings 接口只做展示；驱动和地址在启动时由 `config.yaml` 的 `cache.*` 决定 |

- **13 个分区不等于 13 个 `/settings/:section`**：这个接口只认 8 个（site、security、cache、storage、ai、sms、auth、notify），其中 cache 只读；语言、参数、LDAP、单点登录、地图各有自己的读写接口。连通性测试也只对支持探测的分区可用。
- LDAP / 单点登录 / 地图的菜单位置保留但隐藏（别删行、别改父节点，权限点仍可单独分配）。旧地址 `/ldap/config`、`/system/sso-apps`、`/system/map` 会跳到对应分区。
- 缓存的运行状态看「系统监控 → 缓存监控」。

## 模块细节

### 系统管理

- `user`：账号 CRUD（含批量）、启停、重置密码、解锁、角色 / 部门 / 岗位分配、xlsx 导入导出（导出进导出中心）。同时是登录提供者和初始化向导的建超管入口，**不要停用**——停了连登录都没有。
- `dept`：树用 `parent_id` + 物化路径；跨级拖动 `PUT /depts/:id/move` 在同一事务里重写整棵子树的路径。读接口默认返回完整树（各处的部门下拉都依赖它）；只有部门管理页用 `scene=manage` 调用时，非超管且范围非「全部」的人才会看到裁剪后的树，保留到根的祖先并标 `readonly:true`，范围外详情回 403。
- `dict`：出厂字典类型不可删除、不可改编码，其下出厂数据项不可删除、不可改值（409 `dict.builtin`）；名称、标签、排序、样式、状态仍可改。下拉和标签的数据源是 `GET /dicts/:code/items`，登录即可。
- `notice`：富文本公告，按全员 / 角色 / 部门 / 用户发布，草稿 → 已发布 → 已撤回，定时发布与过期、置顶、重要公告登录弹窗、已读统计；删除进回收站。非超管且数据范围非「全部」时，不能选「全员」和平台公告，指定的部门 / 用户必须在其范围内（指定角色不限）。
- `recycle`：覆盖 user / notice / role / dictType / dictData / post 的软删记录，按名称、类型、删除日期筛选；彻底删除才级联清关联行，还原保留。
- `file`：分组、tus 断点续传、预览、公开链接与签名链接、移动改名删除；本地或 S3，受租户存储额度限制。
- `backup`：立即备份、定时备份与保留策略、下载与删除；另要求超管。

### 系统监控

- `log`：操作日志与登录日志都可查询、删除、清理和导出；导出一律提交进导出中心，列是白名单（不含请求参数、响应、堆栈）。`log.export_max_rows` 是**任务内**上限，超过整个任务失败并提示缩小筛选。
- `sendlog`：记录渠道、场景（验证码 / 通知 / 绑定手机 / 账号通知 / 安全告警 / 测试）、收件人、标题或模板名、状态、失败原因、耗时。收件人脱敏存储（`138****1234`、`a***@x.com`），不存验证码、正文和密钥；异步入队，失败只记日志不影响发送；站内消息不记。
- 三种监控共用 `sys_monitor_snapshot`：每 30 秒一次快照、保留 7 天。缓存监控用 memory 驱动时只能报进程内存和运行时长（key 数与命中率为 0）；接口监控是进程内统计，重启清零。
- `online`：在线会话列表与强制下线，基于缓存里的会话索引，没有表。
- 数据范围实际生效在：用户列表与读写、部门（写操作 + 管理部门页的读）、公告（管理端按创建者裁剪，发布对象另有限制）、操作日志与登录日志。

### 开发与安全、无菜单接口

- `plugin`：已装插件的版本、授权状态与运行期开关。停用立刻关掉该插件的路由和菜单入口；带 worker 的插件重新启用后需重启才恢复，详见[插件开发指南](plugin-development.md)。
- `openapi`：开放应用、事件目录、Webhook 与投递记录、在线接口文档；个人访问令牌在个人中心。
- `sso`：本站作为 OIDC Provider，下游应用注册、密钥重置、签名密钥轮换；应用中心 `/portal` 列出可进入的应用。
- `region`：省市区接口登录即可读；`region:sync` 才能查同步状态，五个写入口还要平台级调用方——换掉的是所有租户地址选择器读的那一份。数据随包内置在 `core/region`，同步快照存 `sys_region_snapshot`（无 `tenant_id`）。
- `ai`：`GET /ai/status`、`POST /ai/filter`（自然语言筛选）、`/ai/translate`、`/ai/write`（写作润色，处理调用方提交的文字，入口在公告表单里）。没配模型就隐藏入口；租户可关掉 AI 或受每日调用次数限制。
- `identity`：五级权限 —— `identity:list` 查看、`identity:edit` 增删改与启停、`identity:test` 测试连接与用户登录、`identity:sync` 目录同步、`identity:match` 允许按用户名绑定已有账号。管理接口匿名 401、缺权限 403；CAS 入口 `/plugins/identity/cas/*` 公开，有进程内 IP 兜底限流（600/分）。
- `identity` 的 LDAP/AD 目录同步是**手动触发**：`POST /plugins/identity/sources/:id/sync` 提交后台任务，同路径 `GET` 查状态；要求 `identity:sync`、有效授权、启用的 LDAP 源和 `directorySync.enabled`。枚举走 RFC 2696 `SearchPaged`（每页 100、单次累计 5000 条），服务端忽略分页控制时超过 100 条即失败。`deactivateMissing` 默认关，只在完整枚举且全部写入成功后停用「这次目录里没出现的已绑定账号」；超管和内置角色账号不动，重新出现也不会自动恢复。中途失败可能已有部分账号写入，真实目录验收还没做。
- `assistant`：个人命名会话 CRUD，需 `assistant:use`，每租户每用户最多 30 个；表 `plg_assistant_conversation` 属于平台库迁移，不是租户业务表。
- `about`：版本、构建时间、数据库引擎、Go 版本、后端依赖的开源许可证清单。

### 插件与收费

- 基础服务免费，可选通用插件免费，业务增强收费。首批免费计划：`cron`、`codegen`、`imports`、`totp`、`ldap`、`maps`、`crm`、`realname`；其余按 pro 规划。正式购买条款尚未发布，目录展示不代表报价或许可承诺。
- 免费计划不等于现有 pro 包已转换；母版已编译挂载 `totp` 与 `ldap`，其余免费插件的清单和源码存在不代表已编译挂载。发布包不带插件；`nexadmin new` 默认只带 `totp`（`--without-totp` 可关），其余按需安装，或派生时 `--plugins <id>,<id>` 点名带上。
- `plugin list` 只读磁盘（free 的授权列是 `-`）；运行时状态以 `GET /plugins`、启动日志和实际接口为准。
- LDAP 插件的配置页可填 CA 证书（PEM，可多张），内网自签的 LDAPS/StartTLS 可直接使用；留空则只信任系统根证书。

## 几条容易踩的约定

- 出厂菜单里还有一条「帮助文档」外链（`https://example.com/docs`，`docs:view`），默认停用，是占位。
- `internal/modules/example` 是示例业务模块（模块名 `example`，`GET /example/hello`、`/example/ping`），不属于框架。
- 审计和后台任务的操作人：`core/auth` 的 `ContextWithUser` / `UserFromContext` **只用于归属记录**，不是 Claims、过不了权限校验；`core/job` 执行时把提交者和 `job:<type>` 放进 context；`audit` 插件按 `WithOperator` → `ClaimsFromContext` → `UserFromContext` → 系统的顺序取操作人。
- **总不变式**：一张表没有 `tenant_id`，就必须有一条「谁挡着它」的登记——整组收口、写路由台账、更强闸、无 HTTP 写入口，或按父行归属。想放开某台控制台，先给它补租户列，再删登记，不许「先放开再说」。
