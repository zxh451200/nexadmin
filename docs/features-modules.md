---
title: 内置模块清单
summary: 内置模块与插件各能做什么、入口在哪、依赖什么。
---

“模块名”是 `config` 里 `disabled_modules` 要写的名字（模块的 `Name()`）。动作缩写沿用本行模块前缀，例如 user 行的 `create` 指 `user:create`。下表后台路径相对 `/admin`，如 `/system/settings` 的完整入口为 `/admin/system/settings`。

**平台全局管理只交给平台级调用方**，不向普通租户角色下放设置、语言、全局菜单/字典、插件安装/授权、备份、租户与套餐等管理权限。这里的“平台级”不只是数据归属，它已经焊进路由，判据是 `core.IsPlatformCaller`（`core/router.go:51`）：**超管，或请求落在默认租户**（单源在 `tenant.IsPlatformCallerID`，随会话以 `auth.User.Platform` 下发给前端）。`core.RequirePlatform()`（`core/router.go:63`）现在挂在 **55 条写路由**上，每条除原有权限点外再过这道闸：工单的快捷回复 / SLA 与命名策略 / 分组与成员 / 自动化规则 / 自定义字段 / 插件设置（`plugins/ticket/handler.go:605-653`，17 条）、代码生成器的 `POST /plugins/codegen/install` 与保存配置（`plugins/codegen/plugin.go:37-41`，4 条）、参数与设置分区（`modules/system/config/module.go:72-86`，8 条）、参数字典的类型与字典项**含两处排序**（`modules/system/dict/module.go:27-44`，10 条）、菜单**含拖拽排序与移动**（`modules/system/menu/module.go:39-45`，6 条）、语言清单整表保存（`modules/system/language/module.go:26`，1 条）、省市区 `preview|import|sync|reset|rollback`（`modules/region/module.go:100-106`，5 条）、ldap / maps / media 三个凭据页（4 条）。逐条对照见附录里带“另要求平台级”的那几行。

**租户管理员要预期的是：即使持有 `ticket:edit`、`dict:update`、`menu:update`、`settings:update` 这些权限点，保存仍然回 403**，错误 key 是 `error.platform_only`（文案在 `core/i18n/locales/{zh-CN,en-US}.yaml`，整句直接说明「该项是平台级共享配置、所有租户共用同一行，只有平台管理员能修改」，不是让人回头翻自己的角色）；写权限已收口的前端把写按钮置灰并显示一行 `<PlatformNotice />` 说明，而不是让入口蒸发。原因一句话：**这些表没有 `tenant_id`，租户的一次保存改写的就是所有租户共用的那一行**——权限点回答“能不能用这个模块”，回答不了“能不能改别人也在用的那一行”。所以“下放给租户自助”不是待办，而是一个需要先做租户化改造的决定（三引擎迁移补 `tenant_id` + 唯一键改形，改造前这一档只归平台；见本节末尾的总不变式与 `scripts/contract_check`）。单租户部署里人人都落在默认租户=平台调用方，这条闸不改任何行为，只在多租户下生效。另有两张表用更强闸：`sys_backup` / `sys_backup_setting` 走 `superOnly()`、`sys_tenant` / `sys_tenant_pkg` 走分组级 `RequireSuper`，表中“平台级”一格不等于这两条闸的都有 `RequirePlatform`。

系统设置页面共 **13 个分区**，按当前导航分组：

| 分组 | 分区（`?tab=`） | 接口归属 |
| --- | --- | --- |
| 站点 | 站点信息 `site` | `config` 模块的 settings 接口 |
| 安全与登录 | 登录安全 `security`、登录方式 `auth`、LDAP 配置 `ldap`、单点登录 `sso` | 前两个用 settings；LDAP 用 `/plugins/ldap/*`；单点登录用 `/sso/*` |
| 存储与集成 | 文件存储 `storage`、AI 模型 `ai`、短信 `sms`、通知渠道 `notify`、地图服务 `maps` | 前四个用 settings；地图用 `/plugins/maps/*` |
| 高级 | 语言 `language`、参数 `params` | 语言用 `/languages`；参数用 `/configs`。缓存没有设置分区：驱动与地址在启动时由 config.yaml 的 `cache.*` 决定（后端 SaveSection 拒绝 cache 分区），运行状态与统计看「系统监控 → 缓存监控」 |

十二个 UI 分区不等于八个 `/settings/:section`：后者只接受八个设置分区；语言、参数、LDAP、单点登录、地图由各自接口读写，连通性测试仅适用于支持探测的设置。LDAP / 单点登录 / 地图的菜单行保留在原位置并隐藏（不删行、不改父节点，权限点仍可单独分配给角色）；旧地址 `/ldap/config`、`/system/sso-apps`、`/system/map` 分别重定向到对应分区。

| 模块名 | 菜单（路径） | 页面能力 | 权限码 | 租户隔离 |
| --- | --- | --- | --- | --- |
| `dashboard` | 工作台（`/dashboard`） | 统计卡片与趋势等聚合数据；页面由「卡片」组成，模块/插件/业务页放 `cards.tsx` 即可自动出现，用户可在「编辑布局」里拖动排序、上移下移、显示/隐藏并按用户保存（`GET/PUT/DELETE /dashboard/layout`，不做自由摆放缩放）；统计各指标用原生 SQL 并显式带 `tenant_id`，对应模块被停用（表不存在）时该指标退化为 0 / 空 | 登录即可（菜单登记 `dashboard:view`，接口不强制；布局接口只能读写自己的，开放平台应用主体返回 403，限流规则 `ratelimit.dashboard_layout`） | 是（布局表 `sys_dashboard_layout` 按租户 + 用户隔离） |
| `user` | 系统管理 → 用户管理（`/system/user`） | 列表 / 详情 / 新增 / 编辑 / 删除（含批量）、启停、重置密码、解锁、分配角色 / 部门 / 岗位、xlsx 导入（含模板）、导出（一律提交导出中心异步任务）。同时是登录提供者（账号密码、锁定、手机号、第三方登录、忘记密码）和初始化向导的建超管 / 判定入口 | `user:list` `create` `update` `delete` `export` `import` `resetPwd` `unlock` | 是 |
| `role` | 系统管理 → 角色管理（`/system/role`） | CRUD、分配菜单 / 按钮权限、数据范围、“可分配”开关、排序、角色选项、导出（走导出中心，`POST /roles/export-async`） | `role:list` `create` `update` `delete` `assign` | 是 |
| `menu` | 系统管理 → 菜单管理（`/system/menu`） | 目录 / 菜单 / 按钮三级、图标 / 排序 / 路由 / 组件 / 缓存 / 隐藏 / 外链、拖动排序；侧边栏菜单按角色过滤（`GET /menus/mine`，登录即可） | `menu:list` `create` `update` `delete`；菜单树接口 `GET /menus/tree` 接受 `menu:list`、`role:list`、`role:assign` 任一 | 否（平台级；六条写路由含拖拽排序与移动另要求平台级调用方） |
| `dept` | 系统管理 → 部门管理（`/system/dept`） | 树形（`parent_id` + 物化路径 `path`）、CRUD、同级排序、跨层级拖动移动（`PUT /depts/:id/move`，整棵子树 path 同事务重写）；写操作受数据范围限制；读取默认返回完整树（各处部门下拉依赖），部门管理页带 `scene=manage` 调用 `GET /depts/tree`、`GET /depts/:id` 时，非超管且范围非“全部”的操作者按数据范围裁剪（保留到根的祖先节点以维持树结构，祖先标记 `readonly:true`，前端置灰且无操作按钮；范围外的详情得到 403） | `dept:list` `create` `update` `delete` | 是 |
| `post` | 系统管理 → 部门管理 → 岗位（`/system/dept?tab=post`，旧地址 `/system/post` 自动跳转） | CRUD、排序、岗位选项、导出（走导出中心，`POST /posts/export-async`） | `post:list` `create` `update` `delete` | 是 |
| `dict` | 系统管理 → 字典管理（`/system/dict`） | 字典类型与字典数据 CRUD、排序（这几张表没有 `tenant_id`，**全部写入口含两处 `/sort` 只认平台级调用方**，租户管理员不是自助方）；`GET /dicts/:code/items` 登录即可，前端下拉 / 标签都用它；内置字典保护：出厂字典类型（`seedTypes` 清单，返回字段 `builtin` 为派生标记、不入库）不可删除、不可改编码，其下出厂数据项不可删除、不可改值（409 `dict.builtin`），名称 / 标签 / 排序 / 样式 / 状态仍可改（内置保护只解释 409，不放开平台级闸：非平台调用方连这些可改字段也改不动） | `dict:list` `create` `update` `delete`；写入口在权限点之外另要求平台级调用方 | 否（平台级，`sys_dict_type`、`sys_dict_data`） |
| `config` | 系统管理 → 系统设置 → 高级 → 参数（`/system/settings?tab=params`，旧地址 `/system/config` 自动跳转） | 参数 KV CRUD 与刷新缓存、上传限制读写（页面）；设置页十二个 UI 分区见上表（八个 settings API 分区，语言、参数、LDAP、单点登录、地图使用独立接口）；支持相应分区的读写与连通性测试；`GET /settings/public` 匿名，登录页取站点名与 Logo | `config:list` `create` `update` `delete`；`settings:list` `update`；八个写入口（参数增删改、`PUT /configs/upload`、`POST /configs/refresh-cache`、`PUT /settings/:section`、`POST /settings/:section/test`）在权限点之外另要求平台级调用方 | 否（平台级，`sys_config`、`sys_setting`） |
| `language` | 系统管理 → 系统设置 → 高级 → 语言（`/system/settings?tab=language`，旧地址 `/system/language` 自动跳转） | 平台语言清单整表读写（`sys_language` 唯一真源）；整表保存是写入口，只认平台级调用方，租户管理员持 `language:update` 也回 403；前缀或语言集合变化需要重启后端才对公开站生效 | `language:list` `update`；`language:update` 另要求平台级调用方 | 否（平台级） |
| `recycle` | 系统管理 → 回收站（`/system/recycle`） | 展示 user / notice / role / dictType / dictData / post 的软删除记录，按名称、类型与删除日期范围筛选，可还原与彻底删除（字典这类平台级表按“执行删除的租户”记账；彻底删除才级联清关联行，还原保留关联） | `recycle:list` `restore` `delete` | 是 |
| `plugin` | 开发与安全 → 插件管理（`/system/plugin`） | 已装插件、版本、授权状态，市场安装与导入授权文件（检查 `needRestart`、状态和日志，部分能力需要重启）；每张卡片一个运行期启用开关（停用立刻关掉该插件的接口与菜单入口，带 worker 的插件重新启用后要重启才恢复，详见《plugin-development.md》的启停边界） | `plugin:list` `plugin:license` `plugin:edit` `plugin:install` | 无表 |
| `backup` | 系统管理 → 数据备份（`/system/backup`） | 立即备份、定时备份与保留策略、下载、删除；另要求超管 | `backup:list` `edit` `download` | 否（平台级） |
| `openapi` | 开发与安全 → 开放平台（`/system/openapi`） | 开放应用、事件目录、Webhook 与投递记录、接口文档；个人访问令牌在个人中心 | `openapi:list` `edit` | 是 |
| `sso` | 系统设置 → 安全与登录 → 单点登录（`/system/settings?tab=sso`，旧地址 `/system/sso-apps` 跳转）；顶级 → 应用中心（`/portal`） | 本站作为 OIDC Provider：下游应用注册、密钥重置、签名密钥轮换；应用中心展示可进入的应用 | `sso:list` `edit`；应用中心登录即可（无独立权限码） | 是 |
| `notice` | 系统管理 → 通知公告（`/content/notice`） | 富文本公告、按全员 / 角色 / 部门 / 用户发布、草稿 → 已发布 → 已撤回、定时发布与过期、置顶、重要公告登录弹窗、已读统计；删除走回收站；受数据范围裁剪；非超管且范围非“全部”的操作者创建 / 修改时发布对象受限：不能选“全员”和平台公告，指定部门与指定用户必须在其数据范围内，指定角色不限制（拒绝文案见 i18n `notice.audience_*`） | `notice:list` `create` `update` `delete` `publish`；`notice:platform`（在 handler 里判断，超管或持有者可发平台公告） | 是 |
| `message` | 顶栏铃铛 → 消息中心（`/message`；个人收件箱，侧栏不重复列，权限行保留） | 消息列表、未读数、已读 / 全部已读、清空消息（删除自己的全部消息）、我的公告的清空（按时间点隐藏，公告本身不动）、删除、SSE 推送；用户只能操作自己的消息 | 登录即可（菜单登记 `message:list`） | 是 |
| `file` | 系统管理 → 文件管理（`/content/file`） | 分组、tus 断点续传上传、预览 / 下载（公开链接与签名链接）、移动、改名、删除；本地或 S3 存储；受租户存储额度限制 | `file:list` `upload` `delete` | 是 |
| `exportcenter` | 导入导出 → 导出中心（`/jobs`） | 异步任务列表、取消、重试、删除、下载结果（每次下载记一条 Module=导出中心、type=download 的操作日志）；任务完成 / 失败给任务所有者发站内信（始终）+ 邮件（设置页 SMTP 配置好才发，走 notify 的 email 渠道）；`core/job` 落终态后发布 `job.finished` 事件（已进开放平台事件目录，Webhook 可订阅）；任务 DTO 带 `expiresAt`（终态 = finishedAt + `job.keep_days`，前端显示「保留至」）；普通用户只能看自己的任务，持有 `job:all` 的管理员看租户内全部 | 登录即可；按钮权限 `job:all`（在 handler 里判断） | 是 |
| `log` | 系统监控 → 操作日志（`/monitor/oper-log`）、登录日志（`/monitor/login-log`） | 查询、删除、操作日志清理、导出（`POST /oper-logs/export-async`、`POST /login-logs/export-async`，一律提交进导出中心；提交动作记一条 type=export 的操作日志；导出列仍为显式白名单，不含请求参数 / 响应 / 异常堆栈；行数上限 `log.export_max_rows` 是任务内上限，超过整个任务失败并提示缩小筛选范围）；均受数据范围裁剪 | `operLog:list` `delete` `export`；`loginLog:list` `delete` `export`（导出权限由服务端路由强制） | 是 |
| `sendlog` | 系统监控 → 发送日志（`/monitor/send-log`） | 短信 / 邮件 / 机器人的投递结果：渠道、场景（验证码 / 通知 / 绑定手机 / 账号通知 / 安全告警 / 测试）、收件人、标题或模板名、状态（成功 / 失败 / 跳过）、失败原因、耗时；分页 + 渠道 / 状态 / 时间范围 / 收件人模糊筛选、详情、批量删除、按天数清理（至少 1 天）。写入走 `core/notify` 的 `Recorder` 接口，由本模块注册时注入（`Notify.SetRecorder`，验证码短信走 `smscode.SetRecorder`），异步入队，失败只记日志、不影响发送；收件人脱敏存储（`138****1234`、`a***@x.com`，机器人存渠道名），不存验证码、正文和密钥，错误信息脱敏并截断到 500 字；站内消息不记 | `sendlog:list` `delete` | 是（`sys_send_log`，读写均显式带 `tenant_id`） |
| `online` | 系统监控 → 在线用户（`/monitor/online`） | 在线会话列表、强制下线；无表，基于缓存里的会话索引 | `online:list` `forceLogout` | 是 |
| `server-monitor` | 系统监控 → 服务监控（`/monitor/server`） | CPU / 内存 / 磁盘 / 网络 / Go 运行时；健康详情；“前端错误”标签；历史趋势（每 30 秒落一次快照，保留 7 天） | `serverMonitor:list` | 是（`sys_monitor_snapshot`，与缓存 / 接口监控共用） |
| `client-error` | 服务监控页的“前端错误”标签 | 接收前端运行时错误（匿名可报，按 IP 限流）；列表查询 | 上报公开；列表 `serverMonitor:list` | 是 |
| `cache-monitor` | 系统监控 → 缓存监控（`/monitor/cache`） | redis / valkey 读 INFO 与 SCAN；memory 驱动只能返回进程内存和运行时长，key 数与命中率为 0；历史趋势同上 | `cacheMonitor:list` | 是（共用 `sys_monitor_snapshot`） |
| `api-monitor` | 系统监控 → 接口监控（`/monitor/api`） | 最近 30 秒各接口 QPS、平均耗时、错误率（状态码 ≥400 计错误），进程内统计，重启清零；历史趋势同上 | `apiMonitor:list` | 是（共用 `sys_monitor_snapshot`） |
| `tenant` | 租户管理 → 租户管理（`/tenant/list`）、租户套餐（`/tenant/package`） | 租户 CRUD、续期、启停、用量；套餐 CRUD（菜单范围、用户 / 角色 / 存储额度、AI 每日调用次数）；域名绑定；租户可单独关闭 AI 并覆盖每日调用次数；我的租户（`/tenant/self`）供租户管理员查看本租户套餐 / 额度 / 用量趋势并维护企业资料与 logo（`tenantSelf:view` `tenantSelf:update`） | `tenant:list` `create` `update` `delete`；`tenantPkg:list` `create` `update` `delete`；`tenantSelf:view` `update` | 否（平台级，`sys_tenant`、`sys_tenant_pkg`）；我的租户只作用于调用者所在租户 |
| `profile` | 个人中心（`/profile`，菜单默认隐藏） | 资料、改密、第三方 / 手机号绑定与解绑、登录设备（会话）管理；个人访问令牌（`/profile/tokens`） | 登录即可（登记 `profile:view`） | 复用用户表 |
| `about` | 关于（`/about`） | 版本、构建时间、数据库引擎、Go 版本、后端依赖的开源许可证清单 | `about:view` | 无表 |
| `region` | 无菜单 | 省市区接口：`GET /regions/tree`、`GET /regions`（登录即可）；`GET /regions/status`、`GET /regions/sync/status`、`GET /regions/versions[/:version]`（`region:sync`）；`POST /regions/preview`、`POST /regions/import`、`POST /regions/sync`、`POST /regions/reset`、`POST /regions/versions/:version/rollback`（`region:sync` **且**只认平台级调用方——换掉的是所有租户的地址选择器读的那一份）。数据随包内置在 `core/region`（go:embed），同步的快照存在 `sys_region_snapshot`；`GET /regions/tree` 支持 `level=4` 下发随包街道/乡镇层（约 4.1 万条，见 core/region/streets.go）——街道名由前端折进详细地址，业务表仍只存 6 位行政区划代码 | 登录即可；同步需 `region:sync`，五个写入口另要求平台级调用方 | `sys_region_snapshot`（无 `tenant_id`） |
| `ai` | 无菜单 | `GET /ai/status`、`POST /ai/filter`（自然语言筛选）、`POST /ai/translate`（翻译）、`POST /ai/write`（写作 / 润色辅助，处理调用方自己提交的文字，前端入口在公告表单里）；未配置模型则入口隐藏。租户可被关闭 AI（`TENANT_AI_DISABLED`）或受每日调用次数限制（`TENANT_QUOTA_AI`），见第 6 节 | 登录即可；筛选另要求对应页面的列表权限 | 无表 |

其他：

- 出厂菜单里还有一条“帮助文档”外链（`https://example.com/docs`，权限 `docs:view`），默认状态为停用，是占位。
- `internal/modules/example`（模块名 `example`，`GET /example/hello`、`GET /example/ping`，权限 `example:ping`）是示例业务模块，不属于框架。
- 基础服务免费，可选通用插件免费，业务增强收费。首批免费计划为 `cron`、`codegen`、`imports`、`totp`、`ldap`、`maps`、`crm`、`realname`，其余插件按 pro 规划；正式购买条款尚未发布，不以目录展示推定价格或法律许可。具体交付以包清单、Go 注册与运行时状态一致为准，计划免费不等于现有 pro 包已转换。LDAP / TOTP 的清单与源码存在不代表已编译挂载。发布包不带插件；`nexadmin new` 默认只带 `totp`（可用 `--without-totp` 关闭），其余插件按需安装，或派生时用 `--plugins <id>,<id>` 点名带上，不能假定框架内附 cron 示例。`plugin list` 只读磁盘，free 的授权列是 `-`，运行时另查 `GET /plugins`、启动日志和实际接口。 LDAP 插件支持在配置页填写 CA 证书（PEM，可多张）：内网自签证书的 LDAPS/StartTLS 可直接使用，留空则只信任系统根证书。
- `identity` 权限码五级：`identity:list` 查看身份源、`identity:edit` 增删改与启停、`identity:test` 测试连接 / 测试用户登录、`identity:sync` 目录同步（含同步状态查询）、`identity:match` 允许 `matchExisting`（外部账号按用户名绑定已有本地账号）；管理接口匿名 401、缺权限 403。CAS 登录入口 `/plugins/identity/cas/*` 是公开接口，按 IP 有进程内兜底限流（每分钟 600 次），另叠加框架限流规则。
- `identity` 已有手动 LDAP/AD 目录同步：`POST /plugins/identity/sources/:id/sync` 提交后台任务，`GET` 同路径查状态，要求 `identity:sync`、有效授权及启用的 LDAP 源与 `directorySync.enabled`。调用者通过真实的 `auth.ClaimsFrom(c)` 获取，缺少 Claims 或用户 ID 非正数时拒绝提交。枚举走 RFC 2696 `SearchPaged`，每页上限 `ldapPageSize=100`，单次同步累计上限 `maxDirectoryEntries=5000`；服务端忽略分页控制时，在同一次检索中接收结果，超过 100 条即失败；普通 `Search` 的客户端硬上限为 50 条，不是该分页兼容路径的上限。超限、检索异常、subject / 账号非法或重复一律整体失败，全部校验通过后逐个新增或更新，中途失败可能已有部分账号写入。`deactivateMissing`（默认关）只在完整枚举且全部写入成功后停用“由该目录绑定过、本次不在目录里”的账号，目录返回数不到已绑定数一半时中止，超管与内置角色账号不动；已停用账号重新出现在目录里不会自动恢复。只支持手动触发，没有自动定时同步，真实目录验收仍待完成。
- `assistant` 路由要求 `assistant:use`，提供个人命名会话 CRUD，每租户每用户最多 30 个，查询显式带 `tenant_id` 与 `user_id`。`plg_assistant_conversation` 属平台库迁移，不能当作租户业务表；ID 是 32 位小写十六进制字符串。真实库建表、持久化和租户/用户隔离仍需单独验收。
- 停用 `user` 模块会同时失去登录提供者和初始化向导的建超管 / 判定入口（`core` 从模块里收集这些角色），不要停用；停用其他模块的影响由模块之间“表不存在退化为空”的约定兜底，未逐个实测。
- 数据范围裁剪实际用在：用户列表与读写、部门（写操作；读取仅 `scene=manage`）、公告（管理端按创建者裁剪，发布对象另有限制）、操作日志与登录日志（`core/datascope`、`modules/system/user/scope.go`）。
- 审计与后台任务的操作人：`core/auth` 的 `ContextWithUser` / `UserFromContext` 只用于归属记录（不是 Claims，过不了任何权限校验）；`core/job` 执行处理器时以提交者和 `job:<type>` 放入 context，`audit` 插件的操作人解析顺序为 `WithOperator` → `ClaimsFromContext` → `UserFromContext` → 系统。
