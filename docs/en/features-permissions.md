---
title: Permission codes and endpoints
summary: The full mapping between permission codes and backend endpoints.
---

Generated from source, grouped by module; endpoints sharing a permission code are on one line.

- Paths omit the `/api/v1` prefix (`/healthz` and `/metrics` are at the root).
- **Login only** = a valid login is required but no permission code is checked; **public** = no login (some protected by credentials, one-time state, signatures or rate limits).
- **Also platform-level** = the endpoints on that line carry `core.RequirePlatform()` on top of the permission code and only accept a platform-level caller (super admin, or a request resolving to the default tenant — `core.IsPlatformCaller`, `core/router.go:51`), so a non-platform caller gets 403 + `error.platform_only` even with the permission code in place; the two checks run in registration order, and read endpoints never carry this gate.
- Where one permission code mixes gated and ungated endpoints (`ticket:edit`, `region:sync`) they are split across two lines rather than sharing one.

| Module | Permission | Endpoints |
| --- | --- | --- |
| about | `about:view` | `GET /about`<br>`GET /about/licenses` |
| ai | Login only | `GET /ai/status`<br>`POST /ai/filter`<br>`POST /ai/translate`<br>`POST /ai/write` |
| api-monitor | `apiMonitor:list` | `GET /monitor/api`<br>`GET /monitor/api/history` |
| cache-monitor | `cacheMonitor:list` | `GET /monitor/cache`<br>`GET /monitor/cache/history` |
| client-error | `serverMonitor:list` | `GET /monitor/client-errors` |
| client-error | Public | `POST /client-errors` |
| core | Public | `GET /auth/captcha`<br>`GET /auth/captcha/:key/audio`<br>`GET /auth/methods`<br>`GET /i18n/languages`<br>`GET /system/status`<br>`GET /healthz`<br>`GET /metrics` (loopback only without a token)<br>`POST /auth/login`<br>`POST /auth/refresh` |
| core | Login only | `GET /auth/me`<br>`POST /auth/logout` |
| core | `mcp:list` | `GET /mcp/tools` (returns only invokable route descriptions; original-route auth is never bypassed) |
| core | `mcp:list`, tool calls additionally check the original route's permission | `POST /mcp` (JSON-RPC; GET tools and explicitly registered write-confirmation tools)<br>`DELETE /mcp` (delete session) |
| core | Loopback only + `X-Shutdown-Token` (registered only by processes started via `start`; otherwise 404) | `POST /system/shutdown` |
| core setup wizard | Exists only when uninitialized; loopback, or with `X-Setup-Token` | `GET /setup/env`<br>`POST /setup/execute`<br>`POST /setup/test-cache`<br>`POST /setup/test-db`<br>`POST /setup/test-s3`<br>`POST /setup/test-ai` |
| dashboard | Login only | `DELETE /dashboard/layout`<br>`GET /dashboard/layout`<br>`GET /dashboard/stats`<br>`PUT /dashboard/layout` |
| exportcenter | Login only (users touch their own tasks; `job:all` touches all) | `DELETE /jobs/:id`<br>`GET /jobs`<br>`GET /jobs/:id`<br>`GET /jobs/:id/download` (each download writes a Module=Export Center, type=download operation log; the task DTO carries `expiresAt`, shown as "kept until")<br>`POST /jobs/:id/cancel`<br>`POST /jobs/:id/retry` |
| file | `file:delete` | `DELETE /file-groups/:id`<br>`DELETE /files`<br>`DELETE /files/:id` |
| file | `file:upload` | `DELETE /files/upload/:id`<br>`HEAD /files/upload/:id`<br>`PATCH /files/upload/:id`<br>`POST /file-groups`<br>`POST /files`<br>`POST /files/upload`<br>`PUT /file-groups/:id`<br>`PUT /files/:id`<br>`PUT /files/move` |
| file | `file:list` | `GET /file-groups`<br>`GET /files` |
| file | Public (public links rely on random strings; private files on expiring signatures) | `GET /files/:id/content`<br>`GET /files/public/:token/:name` |
| language | `language:list` | `GET /languages` |
| language | `language:update` (also platform-level) | `PUT /languages` (whole-list save, returns `languages` and `needRestart`) |
| log | `loginLog:delete` | `DELETE /login-logs` |
| log | `operLog:delete` | `DELETE /oper-logs`<br>`POST /oper-logs/clean` |
| log | `loginLog:export` | `POST /login-logs/export-async` (submits to the export center) |
| log | `operLog:export` | `POST /oper-logs/export-async` (submits to the export center) |
| log | `loginLog:list` | `GET /login-logs` |
| log | `operLog:list` | `GET /oper-logs` |
| message | Login only | `DELETE /messages/:id`<br>`GET /messages`<br>`GET /messages/stream`<br>`GET /messages/unread-count`<br>`PUT /messages/:id/read`<br>`PUT /messages/read-all` |
| notice | `notice:delete` | `DELETE /notices`<br>`DELETE /notices/:id` |
| notice | `notice:list` | `GET /notices`<br>`GET /notices/:id`<br>`GET /notices/:id/read-stats` |
| notice | Login only | `GET /notices/mine`<br>`GET /notices/popup`<br>`POST /notices/:id/read` |
| notice | `notice:create` | `POST /notices` |
| notice | `notice:publish` | `POST /notices/:id/publish`<br>`POST /notices/:id/revoke` |
| notice | `notice:update` | `PUT /notices/:id` |
| online | `online:forceLogout` | `DELETE /online-users/:id` |
| online | `online:list` | `GET /online-users` |
| openapi | `openapi:edit` | `DELETE /openapi/apps/:id`<br>`DELETE /openapi/webhooks/:id`<br>`POST /openapi/apps`<br>`POST /openapi/apps/:id/reset-secret`<br>`POST /openapi/deliveries/:id/retry`<br>`POST /openapi/webhooks`<br>`POST /openapi/webhooks/:id/reset-secret`<br>`POST /openapi/webhooks/:id/test`<br>`PUT /openapi/apps/:id`<br>`PUT /openapi/apps/:id/enabled`<br>`PUT /openapi/webhooks/:id`<br>`PUT /openapi/webhooks/:id/enabled` |
| openapi | Login only (tokens cannot call these three) | `DELETE /profile/tokens/:id`<br>`GET /profile/tokens`<br>`POST /profile/tokens` |
| openapi | `openapi:list` | `GET /openapi.json`<br>`GET /openapi/apps`<br>`GET /openapi/deliveries`<br>`GET /openapi/events`<br>`GET /openapi/routes`<br>`GET /openapi/webhooks` |
| plugin | `plugin:list` | `GET /plugins` |
| plugin | `plugin:license` (dynamically registered per installed plugin) | `POST /plugins/<id>/license` |
| plugin | `plugin:edit` (super admin only; the route stays reserved after disabling, so a plugin disabled at startup can still be re-enabled) | `POST /plugins/<id>/enabled` |
| plugin | Login only (forwarded to not-yet-mounted plugins; the license gate and the plugin's own `Perm` decide) | `ANY /plugins/<id>/*rest` |
| profile | Login only | `DELETE /profile/bindings/:key`<br>`DELETE /profile/bindings/phone`<br>`DELETE /profile/sessions/:id`<br>`GET /profile`<br>`GET /profile/bindings`<br>`GET /profile/sessions`<br>`POST /profile/bindings/:key`<br>`POST /profile/bindings/phone`<br>`PUT /profile`<br>`PUT /profile/password` |
| profile | Public (IP rate-limited, rule `sms_code`) | `POST /auth/sms-code` |
| recycle | `recycle:delete` | `DELETE /recycle/:id` |
| recycle | `recycle:list` | `GET /recycle` |
| recycle | `recycle:restore` | `POST /recycle/:id/restore` |
| region | Login only | `GET /regions`<br>`GET /regions/tree` |
| region | `region:sync` | `GET /regions/status`<br>`GET /regions/sync/status`<br>`GET /regions/versions`<br>`GET /regions/versions/:version` |
| region | `region:sync` (also platform-level) | `POST /regions/preview`<br>`POST /regions/import`<br>`POST /regions/sync`<br>`POST /regions/reset`<br>`POST /regions/versions/:version/rollback` |
| sendlog | `sendlog:delete` | `DELETE /sendlogs`<br>`POST /sendlogs/clean` |
| sendlog | `sendlog:list` | `GET /sendlogs`<br>`GET /sendlogs/:id` |
| server-monitor | `serverMonitor:list` | `GET /monitor/health`<br>`GET /monitor/server`<br>`GET /monitor/server/history` |
| sso | `sso:edit` | `DELETE /sso/apps/:id`<br>`POST /sso/apps`<br>`POST /sso/apps/:id/reset-secret`<br>`POST /sso/keys/rotate`<br>`PUT /sso/apps/:id`<br>`PUT /sso/apps/:id/enabled` |
| sso | Public (protocol endpoints, protected by client credentials or Bearer; `/oauth/token` failures are IP rate-limited) | `GET /oauth/.well-known/openid-configuration`<br>`GET /oauth/jwks`<br>`GET /oauth/logout`<br>`GET /oauth/org/depts`<br>`GET /oauth/org/users`<br>`GET /oauth/userinfo`<br>`POST /oauth/token`<br>`POST /oauth/userinfo` |
| sso | Login only | `GET /oauth/authorize/info`<br>`GET /sso/portal`<br>`POST /oauth/authorize` |
| sso | `sso:list` | `GET /sso/apps`<br>`GET /sso/keys` |
| backup (also super admin) | `backup:edit` | `DELETE /backups/:id`<br>`POST /backups`<br>`PUT /backups/config` |
| backup (also super admin) | `backup:list` | `GET /backups`<br>`GET /backups/config`<br>`GET /backups/status` |
| backup (also super admin) | `backup:download` | `GET /backups/:id/download` |
| config | `config:delete` (also platform-level) | `DELETE /configs`<br>`DELETE /configs/:id` |
| config | Login only | `GET /config/upload` |
| config | `config:list` | `GET /configs`<br>`GET /configs/:id`<br>`GET /configs/upload`<br>`GET /configs/upload-types` |
| config | `settings:list` | `GET /settings` (reads are not platform-gated, but a non-platform caller receives masked credential fields: `restrictSecrets` plus the context-based masking of `url` inside `robots` — see §4) |
| config | Public | `GET /settings/public` |
| config | `config:create` (also platform-level) | `POST /configs` |
| config | `config:update` (also platform-level) | `POST /configs/refresh-cache`<br>`PUT /configs/:id`<br>`PUT /configs/upload` |
| config | `settings:update` (also platform-level) | `POST /settings/:section/test`<br>`PUT /settings/:section` |
| dept | `dept:delete` | `DELETE /depts/:id` |
| dept | `dept:list` | `GET /depts/:id`<br>`GET /depts/tree` |
| dept | `dept:create` | `POST /depts` |
| dept | `dept:update` | `PUT /depts/:id`<br>`PUT /depts/sort` |
| dict | `dict:delete` (also platform-level) | `DELETE /dict/data`<br>`DELETE /dict/data/:id`<br>`DELETE /dict/types`<br>`DELETE /dict/types/:id` |
| dict | `dict:list` | `GET /dict/data`<br>`GET /dict/data/:id`<br>`GET /dict/types`<br>`GET /dict/types/:id` |
| dict | Login only | `GET /dicts/:code/items` (serves the one shared set to everyone; not platform-gated) |
| dict | `dict:create` (also platform-level) | `POST /dict/data`<br>`POST /dict/types` |
| dict | `dict:update` (also platform-level) | `PUT /dict/data/:id`<br>`PUT /dict/data/sort`<br>`PUT /dict/types/:id`<br>`PUT /dict/types/sort` |
| menu | `menu:delete` (also platform-level) | `DELETE /menus/:id`<br>`DELETE /menus` (`?ids=` batch, same permission code) |
| menu | `menu:list` | `GET /menus/:id` |
| menu | Login only | `GET /menus/mine` |
| menu | `menu:list` or `role:list` or `role:assign` | `GET /menus/tree` |
| menu | `menu:create` (also platform-level) | `POST /menus` |
| menu | `menu:update` (also platform-level) | `PUT /menus/:id`<br>`PUT /menus/sort`<br>`PUT /menus/:id/move` |
| post | `post:delete` | `DELETE /posts/:id` |
| post | `post:list` | `GET /posts`<br>`GET /posts/:id`<br>`POST /posts/export-async` (submits to the export center) |
| post | Login only | `GET /posts/options` |
| post | `post:create` | `POST /posts` |
| post | `post:update` | `PUT /posts/:id`<br>`PUT /posts/sort` |
| role | `role:delete` | `DELETE /roles/:id` |
| role | `role:list` | `GET /roles`<br>`GET /roles/:id`<br>`GET /roles/:id/menus`<br>`POST /roles/export-async` (submits to the export center) |
| role | Login only | `GET /roles/options` |
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
| user | Public (third-party login: one-time state + callback whitelist; forgot password: IP / account rate limits) | `GET /auth/oauth/:provider/start`<br>`GET /auth/providers`<br>`POST /auth/forgot/reset`<br>`POST /auth/forgot/send-code`<br>`POST /auth/oauth/:provider/callback`<br>`POST /auth/oauth/bind` |
| user | `user:list` | `GET /users`<br>`GET /users/:id` |
| user | `user:export` | `POST /users/export-async` (submits to the export center) |
| user | `user:import` | `GET /users/import-template`<br>`POST /users/import` |
| user | `user:create` | `POST /users` |
| user | `user:resetPwd` | `POST /users/:id/reset-password` |
| user | `user:unlock` | `POST /users/:id/unlock` |
| user | `user:update` | `PUT /users/:id`<br>`PUT /users/:id/status` |
| workflow (plugin, prefix `/plugins/workflow`) | `workflow:list` | `GET /plugins/workflow/defs`<br>`GET /plugins/workflow/defs/:id`<br>`GET /plugins/workflow/defs/:id/xml`<br>`GET /plugins/workflow/templates`<br>`GET /plugins/workflow/instances` (`todo=1` / `done=1` / `mine=started`)<br>`GET /plugins/workflow/instances/:id` |
| workflow (plugin) | `workflow:edit` | `POST /plugins/workflow/defs`<br>`PUT /plugins/workflow/defs/:id`<br>`DELETE /plugins/workflow/defs/:id` |
| workflow (plugin) | `workflow:start` | `POST /plugins/workflow/instances`<br>`POST /plugins/workflow/instances/:id/withdraw` |
| workflow (plugin) | `workflow:approve` | `POST /plugins/workflow/instances/:id/approve`<br>`POST /plugins/workflow/instances/:id/reject`<br>`POST /plugins/workflow/instances/:id/countersign`<br>`POST /plugins/workflow/instances/:id/rollback`<br>`POST /plugins/workflow/instances/:id/escalate` |
| example (sample business module) | Login only | `GET /example/hello` |
| example (sample business module) | `example:ping` | `GET /example/ping` |
| audit (plugin, prefix `/plugins/audit`) | `audit:clean` | `DELETE /plugins/audit/logs` |
| audit (plugin) | `audit:config` | `GET /plugins/audit/config`<br>`PUT /plugins/audit/config` |
| audit (plugin) | `audit:list` | `GET /plugins/audit/log-tables`<br>`GET /plugins/audit/logs`<br>`GET /plugins/audit/logs/:id` |
| codegen (plugin, prefix `/plugins/codegen`) | `codegen:list` | `GET /plugins/codegen/tables`<br>`GET /plugins/codegen/tables/:name/columns`<br>`GET /plugins/codegen/configs`<br>`POST /plugins/codegen/schema`<br>`POST /plugins/codegen/preview` (`schema` / `preview` are stateless rendering and carry no platform gate) |
| codegen (plugin) | `codegen:download` | `POST /plugins/codegen/download` (stateless rendering as well) |
| codegen (plugin) | `codegen:install` (also platform-level) | `POST /plugins/codegen/install` (renders files into the project root, which belongs to no tenant; by default it also adds the module's blank import inside the marker block of `internal/modules/modules.go`, which can be turned off; a non-platform caller gets 403 even holding `codegen:install`) |
| codegen (plugin) | `codegen:edit` (also platform-level) | `POST /plugins/codegen/configs`<br>`DELETE /plugins/codegen/configs/:id`<br>`DELETE /plugins/codegen/configs` (stored in `plg_codegen_config`, which has no `tenant_id`) |
| cron (plugin, prefix `/plugins/cron`) | `cron:edit` | `DELETE /plugins/cron/jobs/:id`<br>`POST /plugins/cron/jobs`<br>`PUT /plugins/cron/jobs/:id`<br>`PUT /plugins/cron/jobs/:id/enabled` |
| cron (plugin) | `cron:clean` | `DELETE /plugins/cron/logs` |
| cron (plugin) | `cron:list` | `GET /plugins/cron/handlers`<br>`GET /plugins/cron/jobs`<br>`GET /plugins/cron/stats` |
| cron (plugin) | `cron:log` | `GET /plugins/cron/logs`<br>`GET /plugins/cron/logs/:id`<br>`POST /plugins/cron/logs/export-async` (submits to the export center, xlsx; reuses `cron:log`, no new permission code) |
| cron (plugin) | `cron:run` | `POST /plugins/cron/jobs/:id/run` |
| scim (plugin) | `scim:list` | `GET /plugins/scim/tokens` |
| scim (plugin) | Dedicated SCIM Bearer token (no admin JWT or `scim:list` required) | `GET /plugins/scim/v2/Users`<br>`GET /plugins/scim/v2/Users/:id`<br>`GET /plugins/scim/v2/Groups`<br>`GET /plugins/scim/v2/Groups/:id` |
| scim (plugin) | `scim:token` | `POST /plugins/scim/tokens`<br>`DELETE /plugins/scim/tokens/:id` |
| scim (plugin) | Public (Bearer token) | `POST /plugins/scim/v2/Users`<br>`PUT /plugins/scim/v2/Users/:id`<br>`PATCH /plugins/scim/v2/Users/:id`<br>`DELETE /plugins/scim/v2/Users/:id`<br>`POST /plugins/scim/v2/Groups`<br>`PUT /plugins/scim/v2/Groups/:id`<br>`PATCH /plugins/scim/v2/Groups/:id`<br>`DELETE /plugins/scim/v2/Groups/:id` |
| i18n-editor (plugin) | `i18n-editor:list` | `GET /plugins/i18n-editor/entries`<br>`GET /plugins/i18n-editor/versions` |
| i18n-editor (plugin) | `i18n-editor:edit` | `PUT /plugins/i18n-editor/entries` |
| i18n-editor (plugin) | `i18n-editor:publish` | `POST /plugins/i18n-editor/publish`<br>`POST /plugins/i18n-editor/rollback` |
| im (plugin, path prefix `/plugins/im`) | `im:list` | `GET /plugins/im/conversations`<br>`GET /plugins/im/badge`<br>`GET /plugins/im/conversations/:no/messages`<br>`POST /plugins/im/conversations-export-async`<br>`GET /plugins/im/stats`<br>`POST /plugins/im/presence/heartbeat`<br>`GET /plugins/im/agents`<br>`GET /plugins/im/settings`<br>`POST /plugins/im/stream-ticket`<br>`GET /plugins/im/stream/:no` |
| im (plugin) | `im:reply` | `POST /plugins/im/conversations/:no/messages`<br>`POST /plugins/im/conversations/:no/messages/:seq/recall`<br>`POST /plugins/im/conversations/:no/claim`<br>`POST /plugins/im/conversations/:no/release`<br>`POST /plugins/im/conversations/:no/typing`<br>`GET /plugins/im/quick-replies`<br>`POST /plugins/im/conversations/:no/attachments` |
| im (plugin) | `im:edit` | `POST /plugins/im/conversations/:no/close`<br>`PUT /plugins/im/conversations/:no/tags`<br>`PUT /plugins/im/conversations/:no/note`<br>`POST /plugins/im/quick-replies`<br>`PUT /plugins/im/quick-replies/:id`<br>`DELETE /plugins/im/quick-replies/:id`<br>`POST /plugins/im/conversations/:no/transfer`<br>`POST /plugins/im/conversations/:no/merge`<br>`PUT /plugins/im/settings` |
| im (plugin) | Super admin (`middleware.RequireSuper`, operation-logged) | `DELETE /plugins/im/conversations/:no`<br>`POST /plugins/im/conversations-delete` |
| im (plugin) | Public (IP rate-limited; the unguessable conversation number acts as the visitor's capability token) | `GET /plugins/im/visitor/status`<br>`POST /plugins/im/visitor/conversations`<br>`GET /plugins/im/visitor/conversations/:no/messages`<br>`POST /plugins/im/visitor/conversations/:no/messages`<br>`POST /plugins/im/visitor/conversations/:no/read`<br>`POST /plugins/im/visitor/conversations/:no/typing`<br>`POST /plugins/im/visitor/conversations/:no/rating`<br>`POST /plugins/im/visitor/conversations/:no/attachments`<br>`GET /plugins/im/visitor/attachments/:token`<br>`GET /plugins/im/visitor/stream/:no` |
| kb (plugin, prefix `/plugins/kb`; documents and chunks are platform-level tables, the whole group gated by `core.RequirePlatformFeature()`) | `kb:list` | `GET /plugins/kb/documents`<br>`GET /plugins/kb/settings`<br>`GET /plugins/kb/status`<br>`POST /plugins/kb/ask` |
| kb (plugin) | `kb:edit` | `POST /plugins/kb/documents` (multipart upload, sync split and vectorize)<br>`POST /plugins/kb/documents/:id/reembed`<br>`PUT /plugins/kb/settings` |
| kb (plugin) | `kb:delete` (operation-logged) | `DELETE /plugins/kb/documents/:id` (cascades chunks) |
| ticket (plugin, prefix `/plugins/ticket`) | `ticket:list` | `GET /plugins/ticket/tickets`<br>`GET /plugins/ticket/tickets/stats`<br>`POST /plugins/ticket/tickets-export-async`<br>`GET /plugins/ticket/tickets/:no/replies`<br>`GET /plugins/ticket/tickets/:no/links`<br>`GET /plugins/ticket/canned`<br>`GET /plugins/ticket/sla`<br>`GET /plugins/ticket/groups`<br>`GET /plugins/ticket/rules`<br>`GET /plugins/ticket/biz-types`<br>`GET /plugins/ticket/tags`<br>`GET /plugins/ticket/fields`<br>`GET /plugins/ticket/sla/policies`<br>`GET /plugins/ticket/settings`<br>`GET /plugins/ticket/tickets/:no/attachments/:token` (the shared-config reads here are **not** platform-gated: a tenant agent sees the one set of definitions every tenant shares, and cannot change it) |
| ticket (plugin) | `ticket:reply` | `POST /plugins/ticket/tickets/:no/replies`<br>`POST /plugins/ticket/tickets/:no/attachments`<br>`DELETE /plugins/ticket/tickets/:no/attachments/:token` |
| ticket (plugin) | `ticket:edit` (also platform-level: these 17 write entry points touch the shared configuration rows, which have no `tenant_id`) | `POST /plugins/ticket/canned`<br>`PUT /plugins/ticket/canned/:id`<br>`DELETE /plugins/ticket/canned/:id`<br>`PUT /plugins/ticket/sla`<br>`POST /plugins/ticket/sla/policy`<br>`DELETE /plugins/ticket/sla/policy/:code`<br>`POST /plugins/ticket/groups`<br>`PUT /plugins/ticket/groups/:id`<br>`DELETE /plugins/ticket/groups/:id`<br>`PUT /plugins/ticket/groups/:id/members`<br>`POST /plugins/ticket/rules`<br>`PUT /plugins/ticket/rules/:id`<br>`DELETE /plugins/ticket/rules/:id`<br>`POST /plugins/ticket/fields`<br>`PUT /plugins/ticket/fields/:id`<br>`DELETE /plugins/ticket/fields/:id`<br>`PUT /plugins/ticket/settings` |
| ticket (plugin) | `ticket:edit` (writes tenant-owned tables, **not** platform-gated; agents keep working tickets normally) | `POST /plugins/ticket/tickets/bulk`<br>`POST /plugins/ticket/tickets/:no/status`<br>`POST /plugins/ticket/tickets/:no/assign`<br>`POST /plugins/ticket/tickets/:no/group`<br>`PUT /plugins/ticket/tickets/:no/tags`<br>`POST /plugins/ticket/tickets/:no/custom-values`<br>`POST /plugins/ticket/tickets/:no/merge`<br>`POST /plugins/ticket/tickets/:no/links`<br>`DELETE /plugins/ticket/tickets/:no/links/:other` |
| ticket (plugin) | `ticket:create` | `POST /plugins/ticket/tickets` |
| ticket (plugin) | Public (IP rate-limited; the unguessable ticket number acts as the visitor's capability token) | `POST /plugins/ticket/visitor/tickets`<br>`GET /plugins/ticket/visitor/tickets/:no`<br>`POST /plugins/ticket/visitor/tickets/:no/replies`<br>`POST /plugins/ticket/visitor/tickets/:no/rating`<br>`POST /plugins/ticket/visitor/tickets/:no/attachments`<br>`GET /plugins/ticket/visitor/tickets/:no/attachments/:token`<br>`POST /plugins/ticket/inbound/email` |
| pay (plugin, prefix `/plugins/pay`) | `pay:list` | `GET /plugins/pay/orders`<br>`GET /plugins/pay/orders/stats`<br>`GET /plugins/pay/orders/by-biz`<br>`POST /plugins/pay/orders/export-async`<br>`GET /plugins/pay/orders/:no`<br>`GET /plugins/pay/refunds`<br>`GET /plugins/pay/refunds/:no`<br>`GET /plugins/pay/authorizations`<br>`GET /plugins/pay/authorizations/:no`<br>`GET /plugins/pay/channels`<br>`GET /plugins/pay/providers`<br>`GET /plugins/pay/capabilities`<br>`GET /plugins/pay/callbacks`<br>`GET /plugins/pay/outbox`<br>`GET /plugins/pay/outbox/counts`<br>`GET /plugins/pay/outbox/:eventId` |
| pay (plugin) | `pay:edit` | `POST /plugins/pay/orders`<br>`POST /plugins/pay/orders/:no/close`<br>`POST /plugins/pay/orders/:no/sync`<br>`POST /plugins/pay/refunds/:no/sync` |
| pay (plugin) | `pay:refund` | `POST /plugins/pay/refunds` |
| pay (plugin) | `pay:auth` | `POST /plugins/pay/authorizations`<br>`POST /plugins/pay/authorizations/:no/capture`<br>`POST /plugins/pay/authorizations/:no/void` |
| pay (plugin) | `pay:channel` | `POST /plugins/pay/channels`<br>`DELETE /plugins/pay/channels/:id`<br>`DELETE /plugins/pay/channels` (batch, comma-separated `ids`)<br>`POST /plugins/pay/channels/:code/probe` |
| pay (plugin) | `pay:settle` | `GET/POST /plugins/pay/reconcile/tasks`<br>`GET /plugins/pay/reconcile/tasks/:id/diffs`<br>`POST /plugins/pay/reconcile/diffs/:id/resolve`<br>`GET/POST /plugins/pay/commission-rules`<br>`GET /plugins/pay/settlements`<br>`POST /plugins/pay/settlements/generate`<br>`POST /plugins/pay/settlements/:id/confirm`<br>`GET/POST /plugins/pay/payouts`<br>`POST /plugins/pay/payouts/:id/mark-paid`<br>`POST /plugins/pay/payouts/:id/pay`<br>`GET /plugins/pay/payouts/:id/items` |
| pay (plugin) | `pay:outbox` | `POST /plugins/pay/outbox/:eventId/retry`<br>`POST /plugins/pay/outbox/:eventId/discard` |
| pay (plugin) | Public (channel callbacks / mock cashier, signature verification applies) | `POST /plugins/pay/notify/:channelCode`<br>`GET /plugins/pay/mock/cashier`<br>`POST /plugins/pay/mock/cashier/confirm` |
| crm (plugin, prefix `/plugins/crm`) | `crm:list` | `GET /plugins/crm/customers`<br>`GET /plugins/crm/customers/:id`<br>`GET /plugins/crm/customers/resolve-external`<br>`GET /plugins/crm/opportunities`<br>`GET /plugins/crm/opportunities/:id` |
| crm (plugin) | `crm:edit` | `POST /plugins/crm/customers`<br>`PUT /plugins/crm/customers/:id`<br>`DELETE /plugins/crm/customers/:id`<br>`POST /plugins/crm/customers/bind-external` |
| crm (plugin) | `crm:opp` | `POST /plugins/crm/opportunities`<br>`PUT /plugins/crm/opportunities/:id`<br>`DELETE /plugins/crm/opportunities/:id` |
| aigateway (plugin, prefix `/plugins/aigateway`) | `aigateway:list` | `GET /plugins/aigateway/models`<br>`GET /plugins/aigateway/models/:id`<br>`GET /plugins/aigateway/usage`<br>`GET /plugins/aigateway/usage/summary` |
| aigateway (plugin) | `aigateway:edit` | `POST /plugins/aigateway/models`<br>`POST /plugins/aigateway/models/:id`<br>`DELETE /plugins/aigateway/models/:id`<br>`POST /plugins/aigateway/models/:id/ping` |
| ldap (plugin, prefix `/plugins/ldap`) | `ldap:list` | `GET /plugins/ldap/config` |
| ldap (plugin) | `ldap:edit` (also platform-level) | `PUT /plugins/ldap/config`<br>`POST /plugins/ldap/test` (`plg_ldap_config` has no `tenant_id`: one deployment holds one directory credential, so a tenant that configures it points everyone's sign-in at its own directory) |
| maps (plugin, prefix `/plugins/maps`) | `maps:config` | `GET /plugins/maps/config`<br>`GET /plugins/maps/regeo`<br>`GET /plugins/maps/geocode`<br>`GET /plugins/maps/poi` (the last three are login-only, they do not require `maps:config`) |
| maps (plugin) | `maps:config` (also platform-level) | `PUT /plugins/maps/config` (`plg_maps_setting` has no `tenant_id`) |
| media (plugin, prefix `/plugins/media`) | `media:optimize` | `GET /plugins/media/stats`<br>`GET /plugins/media/images`<br>`GET /plugins/media/video/config`<br>`GET /plugins/media/video/assets`<br>`POST /plugins/media/video/upload`<br>`POST /plugins/media/video/assets`<br>`DELETE /plugins/media/video/assets/:id`<br>`DELETE /plugins/media/video/assets` |
| media (plugin) | `media:optimize` (also platform-level) | `PUT /plugins/media/video/config` (this one route only: it is the streaming credential, stored in `plg_media_setting` with no `tenant_id`; video assets themselves are tenant-owned and stay writable) |

Permission points registered but not enforced at the route layer (menus / buttons): `dashboard:view`, `message:list`, `profile:view`, `docs:view`; enforced inside handlers: `notice:platform`, `job:all`.
