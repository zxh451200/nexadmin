---
title: 权限码与接口对照
summary: 权限码与后端接口的完整对照表，供实现与核对。
---

由源码解析生成。路径省略 `/api/v1` 前缀（`/healthz`、`/metrics` 在根路径）。“登录即可”= 需要有效登录但不校验权限码；“公开”= 不需要登录（部分靠凭据、一次性 state、签名或限流保护）；**“另要求平台级”** = 该行接口在权限码之外还挂 `core.RequirePlatform()`，只认平台级调用方（超管或请求落在默认租户，判据 `core.IsPlatformCaller`，`core/router.go:51`），非平台调用方即使权限码齐备也回 403 + `error.platform_only`；两道闸按注册顺序先后生效，读接口一律不挂。同一权限码下挡与不挡混在一起的（`ticket:edit`、`region:sync`）拆成两行，不再共用一行。按模块分组，同一权限码的接口放在一行。

| 模块 | 权限要求 | 接口 |
| --- | --- | --- |
| about | `about:view` | `GET /about`<br>`GET /about/licenses` |
| ai | 登录即可 | `GET /ai/status`<br>`POST /ai/filter`<br>`POST /ai/translate`<br>`POST /ai/write` |
| api-monitor | `apiMonitor:list` | `GET /monitor/api`<br>`GET /monitor/api/history` |
| cache-monitor | `cacheMonitor:list` | `GET /monitor/cache`<br>`GET /monitor/cache/history` |
| client-error | `serverMonitor:list` | `GET /monitor/client-errors` |
| client-error | 公开 | `POST /client-errors` |
| core | 公开 | `GET /auth/captcha`<br>`GET /auth/captcha/:key/audio`<br>`GET /auth/methods`<br>`GET /i18n/languages`<br>`GET /system/status`<br>`GET /healthz`<br>`GET /metrics`（无 token 时仅回环）<br>`POST /auth/login`<br>`POST /auth/refresh` |
| core | 登录即可 | `GET /auth/me`<br>`POST /auth/logout` |
| core | `mcp:list` | `GET /mcp/tools`（工具目录只返回可调用路由描述，不绕过原路由鉴权） |
| core | `mcp:list`，工具调用另校验原接口权限 | `POST /mcp`（JSON-RPC；GET 工具与显式注册的写确认工具）<br>`DELETE /mcp`（删除会话） |
| core | 仅回环 + `X-Shutdown-Token`（只有 `start` 启动的进程注册，其余是 404） | `POST /system/shutdown` |
| core 初始化向导 | 仅未初始化时存在；本机，或带 `X-Setup-Token` | `GET /setup/env`<br>`POST /setup/execute`<br>`POST /setup/test-cache`<br>`POST /setup/test-db`<br>`POST /setup/test-s3`<br>`POST /setup/test-ai` |
| dashboard | 登录即可 | `DELETE /dashboard/layout`<br>`GET /dashboard/layout`<br>`GET /dashboard/stats`<br>`PUT /dashboard/layout` |
| exportcenter | 登录即可（普通用户只能操作自己的任务，`job:all` 可操作全部） | `DELETE /jobs/:id`<br>`GET /jobs`<br>`GET /jobs/:id`<br>`GET /jobs/:id/download`（每次下载记一条 Module=导出中心、type=download 的操作日志；任务 DTO 带 `expiresAt`，前端显示「保留至」）<br>`POST /jobs/download-batch`（批量下载：选中任务的可下载结果文件打包成一个 zip 流式返回；逐条校验所有者与可下载状态，越权/无结果文件的任务跳过，一条都没有报 404；同名结果文件按「任务ID-文件名」去重；整包记一条 type=download_batch 的操作日志）<br>`POST /jobs/:id/cancel`<br>`POST /jobs/:id/retry` |
| file | `file:delete` | `DELETE /file-groups/:id`<br>`DELETE /files`<br>`DELETE /files/:id` |
| file | `file:upload` | `DELETE /files/upload/:id`<br>`HEAD /files/upload/:id`<br>`PATCH /files/upload/:id`<br>`POST /file-groups`<br>`POST /files`<br>`POST /files/upload`<br>`PUT /file-groups/:id`<br>`PUT /files/:id`<br>`PUT /files/move` |
| file | `file:list` | `GET /file-groups`<br>`GET /files` |
| file | 公开（公开链接靠随机串，私有文件靠带过期的签名） | `GET /files/:id/content`<br>`GET /files/public/:token/:name` |
| language | `language:list` | `GET /languages` |
| language | `language:update`（另要求平台级） | `PUT /languages`（整表保存，返回 `languages` 和 `needRestart`） |
| log | `loginLog:delete` | `DELETE /login-logs` |
| log | `operLog:delete` | `DELETE /oper-logs`<br>`POST /oper-logs/clean` |
| log | `loginLog:export` | `POST /login-logs/export-async`（提交进导出中心） |
| log | `operLog:export` | `POST /oper-logs/export-async`（提交进导出中心） |
| log | `loginLog:list` | `GET /login-logs` |
| log | `operLog:list` | `GET /oper-logs` |
| message | 登录即可 | `DELETE /messages/:id`<br>`GET /messages`<br>`GET /messages/stream`<br>`GET /messages/unread-count`<br>`PUT /messages/:id/read`<br>`PUT /messages/read-all` |
| notice | `notice:delete` | `DELETE /notices`<br>`DELETE /notices/:id` |
| notice | `notice:list` | `GET /notices`<br>`GET /notices/:id`<br>`GET /notices/:id/read-stats` |
| notice | 登录即可 | `GET /notices/mine`<br>`GET /notices/popup`<br>`POST /notices/:id/read` |
| notice | `notice:create` | `POST /notices` |
| notice | `notice:publish` | `POST /notices/:id/publish`<br>`POST /notices/:id/revoke` |
| notice | `notice:update` | `PUT /notices/:id` |
| online | `online:forceLogout` | `DELETE /online-users/:id` |
| online | `online:list` | `GET /online-users` |
| openapi | `openapi:edit` | `DELETE /openapi/apps/:id`<br>`DELETE /openapi/webhooks/:id`<br>`POST /openapi/apps`<br>`POST /openapi/apps/:id/reset-secret`<br>`POST /openapi/deliveries/:id/retry`<br>`POST /openapi/webhooks`<br>`POST /openapi/webhooks/:id/reset-secret`<br>`POST /openapi/webhooks/:id/test`<br>`PUT /openapi/apps/:id`<br>`PUT /openapi/apps/:id/enabled`<br>`PUT /openapi/webhooks/:id`<br>`PUT /openapi/webhooks/:id/enabled` |
| openapi | 登录即可（令牌本身不能调用这三个接口） | `DELETE /profile/tokens/:id`<br>`GET /profile/tokens`<br>`POST /profile/tokens` |
| openapi | `openapi:list` | `GET /openapi.json`<br>`GET /openapi/apps`<br>`GET /openapi/deliveries`<br>`GET /openapi/events`<br>`GET /openapi/routes`<br>`GET /openapi/webhooks` |
| plugin | `plugin:list` | `GET /plugins` |
| plugin | `plugin:license`（按已装插件动态注册） | `POST /plugins/<id>/license` |
| plugin | `plugin:edit`（且仅平台超管；停用后仍预留该路由，所以启动时被停用的插件也能重新打开） | `POST /plugins/<id>/enabled` |
| plugin | 登录即可（转发给尚未挂载的插件，由授权 gate 和插件自己的 `Perm` 把关） | `ANY /plugins/<id>/*rest` |
| profile | 登录即可 | `DELETE /profile/bindings/:key`<br>`DELETE /profile/bindings/phone`<br>`DELETE /profile/sessions/:id`<br>`GET /profile`<br>`GET /profile/bindings`<br>`GET /profile/sessions`<br>`POST /profile/bindings/:key`<br>`POST /profile/bindings/phone`<br>`PUT /profile`<br>`PUT /profile/password` |
| profile | 公开（按 IP 限流，规则 `sms_code`） | `POST /auth/sms-code` |
| recycle | `recycle:delete` | `DELETE /recycle/:id` |
| recycle | `recycle:list` | `GET /recycle` |
| recycle | `recycle:restore` | `POST /recycle/:id/restore` |
| region | 登录即可 | `GET /regions`<br>`GET /regions/tree` |
| region | `region:sync` | `GET /regions/status`<br>`GET /regions/sync/status`<br>`GET /regions/versions`<br>`GET /regions/versions/:version` |
| region | `region:sync`（另要求平台级） | `POST /regions/preview`<br>`POST /regions/import`<br>`POST /regions/sync`<br>`POST /regions/reset`<br>`POST /regions/versions/:version/rollback` |
| sendlog | `sendlog:delete` | `DELETE /sendlogs`<br>`POST /sendlogs/clean` |
| sendlog | `sendlog:list` | `GET /sendlogs`<br>`GET /sendlogs/:id` |
| server-monitor | `serverMonitor:list` | `GET /monitor/health`<br>`GET /monitor/server`<br>`GET /monitor/server/history` |
| sso | `sso:edit` | `DELETE /sso/apps/:id`<br>`POST /sso/apps`<br>`POST /sso/apps/:id/reset-secret`<br>`POST /sso/keys/rotate`<br>`PUT /sso/apps/:id`<br>`PUT /sso/apps/:id/enabled` |
| sso | 公开（协议端点，靠 client 凭据或 Bearer 保护；`/oauth/token` 失败按 IP 限流） | `GET /oauth/.well-known/openid-configuration`<br>`GET /oauth/jwks`<br>`GET /oauth/logout`<br>`GET /oauth/org/depts`<br>`GET /oauth/org/users`<br>`GET /oauth/userinfo`<br>`POST /oauth/token`<br>`POST /oauth/userinfo` |
| sso | 登录即可 | `GET /oauth/authorize/info`<br>`GET /sso/portal`<br>`POST /oauth/authorize` |
| sso | `sso:list` | `GET /sso/apps`<br>`GET /sso/keys` |
| backup（另要求超管） | `backup:edit` | `DELETE /backups/:id`<br>`POST /backups`<br>`PUT /backups/config` |
| backup（另要求超管） | `backup:list` | `GET /backups`<br>`GET /backups/config`<br>`GET /backups/status` |
| backup（另要求超管） | `backup:download` | `GET /backups/:id/download` |
| config | `config:delete`（另要求平台级） | `DELETE /configs`<br>`DELETE /configs/:id` |
| config | 登录即可 | `GET /config/upload` |
| config | `config:list` | `GET /configs`<br>`GET /configs/:id`<br>`GET /configs/upload`<br>`GET /configs/upload-types` |
| config | `settings:list` | `GET /settings`（读不挂平台级闸，但非平台调用方拿到的凭据字段是掩码值：`restrictSecrets` + robots 的 `url` 按上下文码，见第 4 节） |
| config | 公开 | `GET /settings/public` |
| config | `config:create`（另要求平台级） | `POST /configs` |
| config | `config:update`（另要求平台级） | `POST /configs/refresh-cache`<br>`PUT /configs/:id`<br>`PUT /configs/upload` |
| config | `settings:update`（另要求平台级） | `POST /settings/:section/test`<br>`PUT /settings/:section` |
| dept | `dept:delete` | `DELETE /depts/:id` |
| dept | `dept:list` | `GET /depts/:id`<br>`GET /depts/tree` |
| dept | `dept:create` | `POST /depts` |
| dept | `dept:update` | `PUT /depts/:id`<br>`PUT /depts/sort` |
| dict | `dict:delete`（另要求平台级） | `DELETE /dict/data`<br>`DELETE /dict/data/:id`<br>`DELETE /dict/types`<br>`DELETE /dict/types/:id` |
| dict | `dict:list` | `GET /dict/data`<br>`GET /dict/data/:id`<br>`GET /dict/types`<br>`GET /dict/types/:id` |
| dict | 登录即可 | `GET /dicts/:code/items`（读的是所有租户共用的一份，不受平台级闸） |
| dict | `dict:create`（另要求平台级） | `POST /dict/data`<br>`POST /dict/types` |
| dict | `dict:update`（另要求平台级） | `PUT /dict/data/:id`<br>`PUT /dict/data/sort`<br>`PUT /dict/types/:id`<br>`PUT /dict/types/sort` |
| menu | `menu:delete`（另要求平台级） | `DELETE /menus/:id`<br>`DELETE /menus`（`?ids=` 批量，同一个权限码） |
| menu | `menu:list` | `GET /menus/:id` |
| menu | 登录即可 | `GET /menus/mine` |
| menu | `menu:list` 或 `role:list` 或 `role:assign` | `GET /menus/tree` |
| menu | `menu:create`（另要求平台级） | `POST /menus` |
| menu | `menu:update`（另要求平台级） | `PUT /menus/:id`<br>`PUT /menus/sort`<br>`PUT /menus/:id/move` |
| post | `post:delete` | `DELETE /posts/:id` |
| post | `post:list` | `GET /posts`<br>`GET /posts/:id`<br>`POST /posts/export-async`（提交进导出中心） |
| post | 登录即可 | `GET /posts/options` |
| post | `post:create` | `POST /posts` |
| post | `post:update` | `PUT /posts/:id`<br>`PUT /posts/sort` |
| role | `role:delete` | `DELETE /roles/:id` |
| role | `role:list` | `GET /roles`<br>`GET /roles/:id`<br>`GET /roles/:id/menus`<br>`POST /roles/export-async`（提交进导出中心） |
| role | 登录即可 | `GET /roles/options` |
| role | `role:create` | `POST /roles` |
| role | `role:update` | `PUT /roles/:id`<br>`PUT /roles/sort` |
| role | `role:assign` | `PUT /roles/:id/menus` |
| tenant | `tenantPkg:delete` | `DELETE /tenant-packages`<br>`DELETE /tenant-packages/:id` |
| tenant | `tenant:delete` | `DELETE /tenants`<br>`DELETE /tenants/:id` |
| tenant | `tenantPkg:list` | `GET /tenant-packages`<br>`GET /tenant-packages/:id` |
| tenant | `tenant:list` | `GET /tenant-packages/options`<br>`GET /tenants`<br>`GET /tenants/:id`<br>`GET /tenants/:id/usage` |
| tenant | `tenantPkg:create` | `POST /tenant-packages` |
| tenant | `tenant:create` | `POST /tenants` |
| tenant | `tenantPkg:update` | `PUT /tenant-packages/:id` |
| tenant | `tenant:update` | `PUT /tenants/:id`<br>`PUT /tenants/:id/renew`<br>`PUT /tenants/:id/status` |
| user | `user:delete` | `DELETE /users`<br>`DELETE /users/:id` |
| user | 公开（第三方登录：一次性 state + 回调白名单；忘记密码：按 IP / 账号限流） | `GET /auth/oauth/:provider/start`<br>`GET /auth/providers`<br>`POST /auth/forgot/reset`<br>`POST /auth/forgot/send-code`<br>`POST /auth/oauth/:provider/callback`<br>`POST /auth/oauth/bind` |
| user | `user:list` | `GET /users`<br>`GET /users/:id` |
| user | `user:export` | `POST /users/export-async`（提交进导出中心） |
| user | `user:import` | `GET /users/import-template`<br>`POST /users/import` |
| user | `user:create` | `POST /users` |
| user | `user:resetPwd` | `POST /users/:id/reset-password` |
| user | `user:unlock` | `POST /users/:id/unlock` |
| user | `user:update` | `PUT /users/:id`<br>`PUT /users/:id/status` |
| workflow（插件，路径前缀 `/plugins/workflow`） | `workflow:list` | `GET /plugins/workflow/defs`<br>`GET /plugins/workflow/defs/:id`<br>`GET /plugins/workflow/defs/:id/xml`<br>`GET /plugins/workflow/templates`<br>`GET /plugins/workflow/instances`（`todo=1` / `done=1` / `mine=started`）<br>`GET /plugins/workflow/instances/:id` |
| workflow（插件） | `workflow:edit` | `POST /plugins/workflow/defs`<br>`PUT /plugins/workflow/defs/:id`<br>`DELETE /plugins/workflow/defs/:id` |
| workflow（插件） | `workflow:start` | `POST /plugins/workflow/instances`<br>`POST /plugins/workflow/instances/:id/withdraw` |
| workflow（插件） | `workflow:approve` | `POST /plugins/workflow/instances/:id/approve`<br>`POST /plugins/workflow/instances/:id/reject`<br>`POST /plugins/workflow/instances/:id/countersign`<br>`POST /plugins/workflow/instances/:id/rollback`<br>`POST /plugins/workflow/instances/:id/escalate` |
| example（示例业务模块） | 登录即可 | `GET /example/hello` |
| example（示例业务模块） | `example:ping` | `GET /example/ping` |
| audit（插件，路径前缀 `/plugins/audit`） | `audit:clean` | `DELETE /plugins/audit/logs` |
| audit（插件） | `audit:config` | `GET /plugins/audit/config`<br>`PUT /plugins/audit/config` |
| audit（插件） | `audit:list` | `GET /plugins/audit/log-tables`<br>`GET /plugins/audit/logs`<br>`GET /plugins/audit/logs/:id` |
| codegen（插件，路径前缀 `/plugins/codegen`） | `codegen:list` | `GET /plugins/codegen/tables`<br>`GET /plugins/codegen/tables/:name/columns`<br>`GET /plugins/codegen/configs`<br>`POST /plugins/codegen/schema`<br>`POST /plugins/codegen/preview`（`schema` / `preview` 是无状态渲染，不挂平台级闸） |
| codegen（插件） | `codegen:download` | `POST /plugins/codegen/download`（同上，无状态渲染） |
| codegen（插件） | `codegen:install`（另要求平台级） | `POST /plugins/codegen/install`（渲染结果写进项目根目录，那份文件不属于任何租户；默认同时把模块空导入写进 `internal/modules/modules.go` 的标记区，可关；非平台调用方 403，即使持 `codegen:install`） |
| codegen（插件） | `codegen:edit`（另要求平台级） | `POST /plugins/codegen/configs`<br>`DELETE /plugins/codegen/configs/:id`<br>`DELETE /plugins/codegen/configs`（存到 `plg_codegen_config`，无 `tenant_id`） |
| cron（插件，路径前缀 `/plugins/cron`） | `cron:edit` | `DELETE /plugins/cron/jobs/:id`<br>`POST /plugins/cron/jobs`<br>`PUT /plugins/cron/jobs/:id`<br>`PUT /plugins/cron/jobs/:id/enabled` |
| cron（插件） | `cron:clean` | `DELETE /plugins/cron/logs` |
| cron（插件） | `cron:list` | `GET /plugins/cron/handlers`<br>`GET /plugins/cron/jobs`<br>`GET /plugins/cron/stats` |
| cron（插件） | `cron:log` | `GET /plugins/cron/logs`<br>`GET /plugins/cron/logs/:id`<br>`POST /plugins/cron/logs/export-async`（按当前筛选提交进导出中心，xlsx；复用 `cron:log` 不新增权限点） |
| cron（插件） | `cron:run` | `POST /plugins/cron/jobs/:id/run` |
| scim（插件） | `scim:list` | `GET /plugins/scim/tokens` |
| scim（插件） | 专用 SCIM Bearer token（不要求后台 JWT 或 `scim:list`） | `GET /plugins/scim/v2/Users`<br>`GET /plugins/scim/v2/Users/:id`<br>`GET /plugins/scim/v2/Groups`<br>`GET /plugins/scim/v2/Groups/:id` |
| scim（插件） | `scim:token` | `POST /plugins/scim/tokens`<br>`DELETE /plugins/scim/tokens/:id` |
| scim（插件） | 公开（Bearer token） | `POST /plugins/scim/v2/Users`<br>`PUT /plugins/scim/v2/Users/:id`<br>`PATCH /plugins/scim/v2/Users/:id`<br>`DELETE /plugins/scim/v2/Users/:id`<br>`POST /plugins/scim/v2/Groups`<br>`PUT /plugins/scim/v2/Groups/:id`<br>`PATCH /plugins/scim/v2/Groups/:id`<br>`DELETE /plugins/scim/v2/Groups/:id` |
| i18n-editor（插件） | `i18n-editor:list` | `GET /plugins/i18n-editor/entries`<br>`GET /plugins/i18n-editor/versions` |
| i18n-editor（插件） | `i18n-editor:edit` | `PUT /plugins/i18n-editor/entries` |
| i18n-editor（插件） | `i18n-editor:publish` | `POST /plugins/i18n-editor/publish`<br>`POST /plugins/i18n-editor/rollback` |
| im（插件，路径前缀 `/plugins/im`） | `im:list` | `GET /plugins/im/conversations`<br>`GET /plugins/im/badge`<br>`GET /plugins/im/conversations/:no/messages`<br>`POST /plugins/im/conversations-export-async`<br>`GET /plugins/im/stats`<br>`POST /plugins/im/presence/heartbeat`<br>`GET /plugins/im/agents`<br>`GET /plugins/im/settings`<br>`POST /plugins/im/stream-ticket`<br>`GET /plugins/im/stream/:no` |
| im（插件） | `im:reply` | `POST /plugins/im/conversations/:no/messages`<br>`POST /plugins/im/conversations/:no/messages/:seq/recall`<br>`POST /plugins/im/conversations/:no/claim`<br>`POST /plugins/im/conversations/:no/release`<br>`POST /plugins/im/conversations/:no/typing`<br>`GET /plugins/im/quick-replies`<br>`POST /plugins/im/conversations/:no/attachments` |
| im（插件） | `im:edit` | `POST /plugins/im/conversations/:no/close`<br>`PUT /plugins/im/conversations/:no/tags`<br>`PUT /plugins/im/conversations/:no/note`<br>`POST /plugins/im/quick-replies`<br>`PUT /plugins/im/quick-replies/:id`<br>`DELETE /plugins/im/quick-replies/:id`<br>`POST /plugins/im/conversations/:no/transfer`<br>`POST /plugins/im/conversations/:no/merge`<br>`PUT /plugins/im/settings` |
| im（插件） | 超管（`middleware.RequireSuper`，操作日志） | `DELETE /plugins/im/conversations/:no`<br>`POST /plugins/im/conversations-delete` |
| im（插件） | 公开（按 IP 限流；会话号不可枚举，充当访客能力令牌） | `GET /plugins/im/visitor/status`<br>`POST /plugins/im/visitor/conversations`<br>`GET /plugins/im/visitor/conversations/:no/messages`<br>`POST /plugins/im/visitor/conversations/:no/messages`<br>`POST /plugins/im/visitor/conversations/:no/read`<br>`POST /plugins/im/visitor/conversations/:no/typing`<br>`POST /plugins/im/visitor/conversations/:no/rating`<br>`POST /plugins/im/visitor/conversations/:no/handoff`<br>`POST /plugins/im/visitor/conversations/:no/attachments`<br>`GET /plugins/im/visitor/attachments/:token`<br>`GET /plugins/im/visitor/stream/:no` |
| kb（插件，路径前缀 `/plugins/kb`；文档与切块是平台级表，整组挂 `core.RequirePlatformFeature()`） | `kb:list` | `GET /plugins/kb/documents`<br>`GET /plugins/kb/settings`<br>`GET /plugins/kb/status`<br>`POST /plugins/kb/ask` |
| kb（插件） | `kb:edit` | `POST /plugins/kb/documents`（multipart 上传，同步切分并向量化）<br>`POST /plugins/kb/documents/:id/reembed`<br>`PUT /plugins/kb/settings` |
| kb（插件） | `kb:delete`（操作日志） | `DELETE /plugins/kb/documents/:id`（级联删切块） |
| ticket（插件，路径前缀 `/plugins/ticket`） | `ticket:list` | `GET /plugins/ticket/tickets`<br>`GET /plugins/ticket/tickets/stats`<br>`POST /plugins/ticket/tickets-export-async`<br>`GET /plugins/ticket/tickets/:no/replies`<br>`GET /plugins/ticket/tickets/:no/links`<br>`GET /plugins/ticket/canned`<br>`GET /plugins/ticket/sla`<br>`GET /plugins/ticket/groups`<br>`GET /plugins/ticket/rules`<br>`GET /plugins/ticket/biz-types`<br>`GET /plugins/ticket/tags`<br>`GET /plugins/ticket/fields`<br>`GET /plugins/ticket/sla/policies`<br>`GET /plugins/ticket/settings`<br>`GET /plugins/ticket/tickets/:no/attachments/:token`（这几条读的是共享配置行，**不**挂平台级闸：租户坐席看得见所有租户共用的那一份定义，改不动） |
| ticket（插件） | `ticket:reply` | `POST /plugins/ticket/tickets/:no/replies`<br>`POST /plugins/ticket/tickets/:no/attachments`<br>`DELETE /plugins/ticket/tickets/:no/attachments/:token` |
| ticket（插件） | `ticket:edit`（另要求平台级：这 17 个写入口动的是没有 `tenant_id` 的共享配置行） | `POST /plugins/ticket/canned`<br>`PUT /plugins/ticket/canned/:id`<br>`DELETE /plugins/ticket/canned/:id`<br>`PUT /plugins/ticket/sla`<br>`POST /plugins/ticket/sla/policy`<br>`DELETE /plugins/ticket/sla/policy/:code`<br>`POST /plugins/ticket/groups`<br>`PUT /plugins/ticket/groups/:id`<br>`DELETE /plugins/ticket/groups/:id`<br>`PUT /plugins/ticket/groups/:id/members`<br>`POST /plugins/ticket/rules`<br>`PUT /plugins/ticket/rules/:id`<br>`DELETE /plugins/ticket/rules/:id`<br>`POST /plugins/ticket/fields`<br>`PUT /plugins/ticket/fields/:id`<br>`DELETE /plugins/ticket/fields/:id`<br>`PUT /plugins/ticket/settings` |
| ticket（插件） | `ticket:edit`（写租户表，**不**挂平台级闸；持 `ticket:edit` 的坐席照常做单） | `POST /plugins/ticket/tickets/bulk`<br>`POST /plugins/ticket/tickets/:no/status`<br>`POST /plugins/ticket/tickets/:no/assign`<br>`POST /plugins/ticket/tickets/:no/group`<br>`PUT /plugins/ticket/tickets/:no/tags`<br>`POST /plugins/ticket/tickets/:no/custom-values`<br>`POST /plugins/ticket/tickets/:no/merge`<br>`POST /plugins/ticket/tickets/:no/links`<br>`DELETE /plugins/ticket/tickets/:no/links/:other` |
| ticket（插件） | `ticket:create` | `POST /plugins/ticket/tickets` |
| ticket（插件） | 公开（按 IP 限流；工单号不可枚举，充当访客能力令牌） | `POST /plugins/ticket/visitor/tickets`<br>`GET /plugins/ticket/visitor/tickets/:no`<br>`POST /plugins/ticket/visitor/tickets/:no/replies`<br>`POST /plugins/ticket/visitor/tickets/:no/rating`<br>`POST /plugins/ticket/visitor/tickets/:no/attachments`<br>`GET /plugins/ticket/visitor/tickets/:no/attachments/:token`<br>`POST /plugins/ticket/inbound/email` |
| pay（插件，路径前缀 `/plugins/pay`） | `pay:list` | `GET /plugins/pay/orders`<br>`GET /plugins/pay/orders/stats`<br>`GET /plugins/pay/orders/by-biz`<br>`POST /plugins/pay/orders/export-async`<br>`GET /plugins/pay/orders/:no`<br>`GET /plugins/pay/refunds`<br>`GET /plugins/pay/refunds/:no`<br>`GET /plugins/pay/authorizations`<br>`GET /plugins/pay/authorizations/:no`<br>`GET /plugins/pay/channels`<br>`GET /plugins/pay/providers`<br>`GET /plugins/pay/capabilities`<br>`GET /plugins/pay/callbacks`<br>`GET /plugins/pay/outbox`<br>`GET /plugins/pay/outbox/counts`<br>`GET /plugins/pay/outbox/:eventId` |
| pay（插件） | `pay:edit` | `POST /plugins/pay/orders`<br>`POST /plugins/pay/orders/:no/close`<br>`POST /plugins/pay/orders/:no/sync`<br>`POST /plugins/pay/refunds/:no/sync` |
| pay（插件） | `pay:refund` | `POST /plugins/pay/refunds` |
| pay（插件） | `pay:auth` | `POST /plugins/pay/authorizations`<br>`POST /plugins/pay/authorizations/:no/capture`<br>`POST /plugins/pay/authorizations/:no/void` |
| pay（插件） | `pay:channel` | `POST /plugins/pay/channels`<br>`DELETE /plugins/pay/channels/:id`<br>`DELETE /plugins/pay/channels`（批量，`ids` 逗号分隔）<br>`POST /plugins/pay/channels/:code/probe` |
| pay（插件） | `pay:settle` | `GET/POST /plugins/pay/reconcile/tasks`<br>`GET /plugins/pay/reconcile/tasks/:id/diffs`<br>`POST /plugins/pay/reconcile/diffs/:id/resolve`<br>`GET/POST /plugins/pay/commission-rules`<br>`GET /plugins/pay/settlements`<br>`POST /plugins/pay/settlements/generate`<br>`POST /plugins/pay/settlements/:id/confirm`<br>`GET/POST /plugins/pay/payouts`<br>`POST /plugins/pay/payouts/:id/mark-paid`<br>`POST /plugins/pay/payouts/:id/pay`<br>`GET /plugins/pay/payouts/:id/items` |
| pay（插件） | `pay:outbox` | `POST /plugins/pay/outbox/:eventId/retry`<br>`POST /plugins/pay/outbox/:eventId/discard` |
| pay（插件） | 公开（渠道回调 / mock 收银台，验签兜底） | `POST /plugins/pay/notify/:channelCode`<br>`GET /plugins/pay/mock/cashier`<br>`POST /plugins/pay/mock/cashier/confirm` |
| crm（插件，路径前缀 `/plugins/crm`） | `crm:list` | `GET /plugins/crm/customers`<br>`GET /plugins/crm/customers/:id`<br>`GET /plugins/crm/customers/resolve-external`<br>`GET /plugins/crm/opportunities`<br>`GET /plugins/crm/opportunities/:id` |
| crm（插件） | `crm:edit` | `POST /plugins/crm/customers`<br>`PUT /plugins/crm/customers/:id`<br>`DELETE /plugins/crm/customers/:id`<br>`POST /plugins/crm/customers/bind-external` |
| crm（插件） | `crm:opp` | `POST /plugins/crm/opportunities`<br>`PUT /plugins/crm/opportunities/:id`<br>`DELETE /plugins/crm/opportunities/:id` |
| aigateway（插件，路径前缀 `/plugins/aigateway`） | `aigateway:list` | `GET /plugins/aigateway/models`<br>`GET /plugins/aigateway/models/:id`<br>`GET /plugins/aigateway/usage`<br>`GET /plugins/aigateway/usage/summary` |
| aigateway（插件） | `aigateway:edit` | `POST /plugins/aigateway/models`<br>`POST /plugins/aigateway/models/:id`<br>`DELETE /plugins/aigateway/models/:id`<br>`POST /plugins/aigateway/models/:id/ping` |
| ldap（插件，路径前缀 `/plugins/ldap`） | `ldap:list` | `GET /plugins/ldap/config` |
| ldap（插件） | `ldap:edit`（另要求平台级） | `PUT /plugins/ldap/config`<br>`POST /plugins/ldap/test`（`plg_ldap_config` 无 `tenant_id`：一台部署只有一份目录凭据，租户配一次就把所有人的登录指向自己的目录） |
| maps（插件，路径前缀 `/plugins/maps`） | `maps:config` | `GET /plugins/maps/config`<br>`GET /plugins/maps/regeo`<br>`GET /plugins/maps/geocode`<br>`GET /plugins/maps/poi`（后三条登录即可，不要求 `maps:config`） |
| maps（插件） | `maps:config`（另要求平台级） | `PUT /plugins/maps/config`（`plg_maps_setting` 无 `tenant_id`） |
| media（插件，路径前缀 `/plugins/media`） | `media:optimize` | `GET /plugins/media/stats`<br>`GET /plugins/media/images`<br>`GET /plugins/media/video/config`<br>`GET /plugins/media/video/assets`<br>`POST /plugins/media/video/upload`<br>`POST /plugins/media/video/assets`<br>`DELETE /plugins/media/video/assets/:id`<br>`DELETE /plugins/media/video/assets` |
| media（插件） | `media:optimize`（另要求平台级） | `PUT /plugins/media/video/config`（只有这一条：它是流媒体凭据，落 `plg_media_setting`，无 `tenant_id`；视频资产本身按租户归属，照常可写） |

只登记、不在路由层强制的权限点（菜单 / 按钮用）：`dashboard:view`、`message:list`、`profile:view`、`docs:view`；在 handler 里判断的：`notice:platform`、`job:all`。
