---
title: Features & Permissions
summary: The complete list of built-in modules, sign-in methods, permission codes, and data scopes.
---

# NexAdmin Features & Permissions

This document lists built-in modules, permission codes and endpoints, login methods, the open platform, the plugin mechanism, roles and data scopes, and tenant quotas & expiry. Sources:

- Routes and permission codes: each module's `module.go` / plugin routes are authoritative; use the framework repo's `scripts/contract_check` to parse and verify. The appendix is a reference for common endpoints, not a fixed route total; additions and plugins are governed by their source, delivery manifests and runtime OpenAPI. Dynamic plugin routes are assembled by the manager from registered plugins.
- Menus: `modules/system/menu/seed.go` (factory menus; business key = permission code; menu names come from the frontend zh-CN dictionary).
- Tenant isolation: look at the model's embedded base — `TenantModel` (with `tenant_id`, queries automatically scoped) means "yes", `PlatformModel` means "no (platform-level)".
- All API paths are under `/api/v1`; only `/healthz` and `/metrics` sit at the root. Common "permission ↔ endpoint" mappings are in the appendix.

This is an interface and usage reference, not an environment acceptance report. Real databases, Redis multi-instance deployments, LDAP/AD/CAS, browser flows and deployment paths still need separate validation. Region-data provenance, year, completeness and redistribution permission remain unconfirmed; do not deliver it as an officially licensed dataset. Start with the [Quickstart](quickstart.md).

## 1. Built-in module list

"Module name" is the module's `Name()` used in `disabled_modules`. Abbreviated actions inherit that row's module prefix: `create` in the user row means `user:create`. Admin paths below are relative to `/admin`; for example, `/system/settings` opens at `/admin/system/settings`.

**Reserve platform-global management for platform super admins.** Do not delegate settings, languages, global menus/dictionaries, plugin installation/licensing, backups, tenants or packages to ordinary tenant roles. "Platform-level" describes data ownership, not an automatic super-admin gate: some current administration routes enforce permission codes only. Verify and close non-super-admin direct-API access before deployment; RBAC assignment or hidden menus do not prove isolation.

System Settings has **13 UI sections**, grouped as in the current navigation:

| Group | Sections (`?tab=`) | API owner |
| --- | --- | --- |
| Site | Site information `site` | settings endpoints in the `config` module |
| Security & sign-in | Login security `security`, Login methods `auth`, LDAP config `ldap`, Single sign-on `sso` | settings for the first two; `/plugins/ldap/*` for LDAP; `/sso/*` for SSO |
| Storage & integrations | File storage `storage`, AI model `ai`, SMS `sms`, Notification channels `notify`, Maps `maps` | settings for the first four; `/plugins/maps/*` for maps |
| Advanced | Cache `cache`, Languages `language`, Parameters `params` | settings for cache; `/languages` for languages; `/configs` for parameters |

Thirteen UI sections do not mean thirteen `/settings/:section` values: that endpoint accepts eight settings sections; language, parameter, LDAP, SSO and map management use their own APIs. Connectivity tests apply only to settings with supported probes. The LDAP, SSO and map menu rows stay at their original positions and are hidden — do not delete or reparent them (permission codes remain assignable to roles). The legacy `/ldap/config`, `/system/sso-apps` and `/system/map` URLs redirect to the matching section.

| Module | Menu (path) | Page capabilities | Permission codes | Tenant-scoped |
| --- | --- | --- | --- | --- |
| `dashboard` | Dashboard (`/dashboard`) | Aggregated stat cards and trends; the page is composed of "cards" — modules/plugins/business pages drop a `cards.tsx` and cards appear automatically; users reorder, move, show/hide them in "Edit Layout" and layouts are saved per user (`GET/PUT/DELETE /dashboard/layout`, no free placement/resizing); statistics use raw SQL with explicit `tenant_id`, degrading to 0/empty when the owning module is disabled (table absent) | Login only (menu registers `dashboard:view`; endpoints don't enforce; layout endpoints only touch one's own rows; open-platform app subjects get 403; rate rule `ratelimit.dashboard_layout`) | Yes (layout table `sys_dashboard_layout` scoped by tenant + user) |
| `user` | System → User Management (`/system/user`) | List / detail / create / edit / delete (incl. batch), enable-disable, reset password, unlock, assign roles / departments / posts, xlsx import (with template), sync export, async export (via export center). Also the login provider (password, lockout, phone, third-party login, forgot password) and the setup wizard's admin-creation / initialized check | `user:list` `create` `update` `delete` `export` `import` `resetPwd` `unlock` | Yes |
| `role` | System → Role Management (`/system/role`) | CRUD, assign menu/button permissions, data scope, "assignable" switch, sort, role options | `role:list` `create` `update` `delete` `assign` | Yes |
| `menu` | System → Menu Management (`/system/menu`) | Directory/menu/button three levels, icon/sort/route/component/cache/hidden/external link, drag sort; sidebar menus filtered by role (`GET /menus/mine`, login only) | `menu:list` `create` `update` `delete`; the menu-tree endpoint `GET /menus/tree` accepts `menu:list`, `role:list` or `role:assign` | No (platform) |
| `dept` | System → Department Management (`/system/dept`) | Tree (`parent_id` + materialized path `path`), CRUD, sibling sort, cross-level drag move (`PUT /depts/:id/move`, the whole subtree's `path` rewritten in the same transaction); writes limited by data scope; reads return the full tree by default (department dropdowns everywhere depend on it); the department page uses `scene=manage` against `GET /depts/tree`, `GET /depts/:id` — non-super-admin operators with a scope narrower than "all" get the tree clipped (ancestors up to the root kept to preserve structure, marked `readonly:true`, greyed out with no action buttons; out-of-scope detail is 403) | `dept:list` `create` `update` `delete` | Yes |
| `post` | System → Department Management → Posts (`/system/dept?tab=post`; legacy `/system/post` redirects) | CRUD, sort, post options | `post:list` `create` `update` `delete` | Yes |
| `dict` | System → Dictionary Management (`/system/dict`) | Dictionary type & data CRUD, sort; `GET /dicts/:code/items` is login-only, used by frontend dropdowns/tags; built-in dictionary protection: factory types (the `seedTypes` list; the returned `builtin` field is derived, not stored) cannot be deleted or re-coded, and their factory items cannot be deleted or re-valued (409 `dict.builtin`); name / label / sort / style / status remain editable | `dict:list` `create` `update` `delete` | No (platform) |
| `config` | System → System Settings → Advanced → Parameters (`/system/settings?tab=params`; legacy `/system/config` redirects) | Parameter KV CRUD, cache refresh and upload limits; eight settings API sections within the thirteen-section UI above; `GET /settings/public` is anonymous for login branding | `config:list` `create` `update` `delete`; `settings:list` `update` | No (platform; `sys_config`, `sys_setting`) |
| `language` | System → System Settings → Advanced → Languages (`/system/settings?tab=language`; legacy `/system/language` redirects) | Whole-list language management; `sys_language` is the sole source of truth; language-set or prefix changes require backend restart for public-site routes | `language:list` `update` | No (platform) |
| `recycle` | System → Recycle Bin (`/system/recycle`) | Soft-deleted user / notice records, restore and purge | `recycle:list` `restore` `delete` | Yes |
| `plugin` | System → Plugin Management (`/system/plugin`) | Installed plugins, versions, license state, market installation and license import (inspect `needRestart`, status and logs; some capabilities require restart) | `plugin:list` `plugin:license` `plugin:install` | No tables |
| `backup` | System → Data Backup (`/system/backup`) | Immediate backup, scheduled backups & retention, download, delete; also requires super admin | `backup:list` `edit` `download` | No (platform) |
| `openapi` | System → Open Platform (`/system/openapi`) | Open-platform apps, event catalog, webhooks & deliveries, API docs; personal access tokens live in Profile | `openapi:list` `edit` | Yes |
| `sso` | System Settings → Security & sign-in → Single sign-on (`/system/settings?tab=sso`; legacy `/system/sso-apps` redirects); top-level → App Portal (`/portal`) | This site as an OIDC Provider: downstream app registration, secret reset, signing-key rotation; the portal lists apps you can enter | `sso:list` `edit`; portal is login-only (no separate permission code) | Yes |
| `notice` | Content → Notices (`/content/notice`) | Rich-text announcements, publish to all / roles / departments / users, draft → published → revoked, scheduled publish & expiry, pinned, important-notice login popup, read stats; deletes go to the recycle bin; data-scope clipping; non-super-admin publishers with scopes narrower than "all" cannot pick "all users" or platform notices, and specified departments/users must be inside their scope (specified roles unrestricted; rejection copy in i18n `notice.audience_*`) | `notice:list` `create` `update` `delete` `publish`; `notice:platform` (checked in the handler — super admin or holders may publish platform notices) | Yes |
| `message` | Content → Message Center (`/message`) | Message list, unread count, read / read-all, clear messages (delete all of your own), clear my notices (hidden up to a timestamp; the notices themselves stay), delete, SSE push; users only touch their own messages | Login only (menu registers `message:list`) | Yes |
| `file` | Content → File Management (`/content/file`) | Groups, tus resumable upload, preview / download (public & signed links), move, rename, delete; local or S3 storage; limited by tenant storage quota | `file:list` `upload` `delete` | Yes |
| `exportcenter` | Import & Export → Export Center (`/jobs`) | Async task list, cancel, retry, delete, download results; regular users see only their own tasks; admins with `job:all` see all in the tenant | Login only; button permission `job:all` (checked in handler) | Yes |
| `log` | Monitoring → Operation Logs (`/monitor/oper-log`), Login Logs (`/monitor/login-log`) | Query, delete, operation-log cleanup, export (`GET /oper-logs/export`, `GET /login-logs/export` — xlsx streamed, columns are an explicit whitelist excluding request params / responses / stack traces, row cap `log.export_max_rows`, rate rule `ratelimit.log_export`, each export writes a type=export operation log); both clipped by data scope | `operLog:list` `delete` `export`; `loginLog:list` `delete` `export` (export permissions enforced by server routes) | Yes |
| `sendlog` | Monitoring → Send Logs (`/monitor/send-log`) | SMS / email / bot delivery results: channel, scene (captcha / notice / phone bind / account notice / security alert / test), recipient, title or template name, status (success / failure / skipped), failure reason, duration; paged + channel / status / time-range / recipient fuzzy filters, detail, batch delete, cleanup by days (min 1). Writes go through `core/notify`'s `Recorder` interface injected when this module registers (`Notify.SetRecorder`; captcha SMS uses `smscode.SetRecorder`), enqueued async, failures logged only; recipients are masked (`138****1234`, `a***@x.com`; bots store the channel name), codes/bodies/keys are not stored, errors masked and truncated to 500 chars; in-app messages are not recorded | `sendlog:list` `delete` | Yes (`sys_send_log`, reads & writes carry explicit `tenant_id`) |
| `online` | Monitoring → Online Users (`/monitor/online`) | Online session list, force logout; no table — based on session indexes in cache | `online:list` `forceLogout` | Yes |
| `server-monitor` | Monitoring → Server Monitor (`/monitor/server`) | CPU / memory / disk / network / Go runtime; health details; "frontend errors" tab; persisted history trend (a snapshot every 30s, kept 7 days) | `serverMonitor:list` | Yes (`sys_monitor_snapshot`, shared with cache / API monitor) |
| `client-error` | The "frontend errors" tab of server monitor | Receives frontend runtime errors (anonymous reporting allowed, IP rate-limited); list query | Reporting is public; list is `serverMonitor:list` | Yes |
| `cache-monitor` | Monitoring → Cache Monitor (`/monitor/cache`) | redis / valkey INFO & SCAN; the memory driver can only report process memory and uptime, with 0 keys and 0 hit rate; persisted history trend as above | `cacheMonitor:list` | Yes (shares `sys_monitor_snapshot`) |
| `api-monitor` | Monitoring → API Monitor (`/monitor/api`) | Per-endpoint QPS, average latency and error rate over the last 30 seconds (status ≥400 counts as error), in-process stats, reset on restart; persisted history trend as above | `apiMonitor:list` | Yes (shares `sys_monitor_snapshot`) |
| `tenant` | Tenants → Tenant Management (`/tenant/list`), Tenant Packages (`/tenant/package`) | Tenant CRUD, renewal, enable/disable, usage; package CRUD (menu scope, user / role / storage quotas, AI daily calls); domain binding; tenants can individually disable AI and override daily call counts; My tenant (`/tenant/self`) lets tenant administrators view their own plan / quotas / usage trend and maintain company details and logo (`tenantSelf:view` `tenantSelf:update`) | `tenant:list` `create` `update` `delete`; `tenantPkg:list` `create` `update` `delete`; `tenantSelf:view` `update` | No (platform; `sys_tenant`, `sys_tenant_pkg`); My tenant reads only the caller's tenant |
| `profile` | Profile (`/profile`, menu hidden by default) | Profile, change password, third-party / phone binding & unbinding, login devices (sessions) management; personal access tokens (`/profile/tokens`) | Login only (registers `profile:view`) | Reuses the user table |
| `about` | About (`/about`) | Version, build time, database engine, Go version, open-source license list of backend dependencies | `about:view` | No tables |
| `region` | No menu | Provinces/cities/districts: `GET /regions/tree`, `GET /regions` (login only); `GET /regions/status`, `POST /regions/sync`, `POST /regions/reset` (`region:sync`). A full snapshot ships embedded in `core/region` (go:embed); synced snapshots are stored in `sys_region_snapshot` | Login only; syncing requires `region:sync` | `sys_region_snapshot` |
| `ai` | No menu | `GET /ai/status`, `POST /ai/filter` (natural-language filtering), `POST /ai/translate` (translation), `POST /ai/write` (writing/polishing assistance, processing caller-submitted text; frontend entry inside the notice form); entries hidden when no model is configured. Tenants can disable AI (`TENANT_AI_DISABLED`) or be limited by daily call counts (`TENANT_QUOTA_AI`), see §6 | Login only; filtering additionally requires the page's list permission | No tables |

Other:

- The factory menus include a "Help Docs" external link (`https://example.com/docs`, permission `docs:view`), disabled by default — a placeholder.
- `internal/modules/example` (module name `example`, `GET /example/hello`, `GET /example/ping`, permission `example:ping`) is a sample business module, not part of the framework.
- Base services and optional general-purpose plugins are free; business enhancements are paid. The initial free plan covers `cron`, `codegen`, `imports`, `totp`, `ldap`, `maps` and `crm`; others are planned as pro. Formal purchase terms have not been published; catalog display is not a price or legal-license commitment. Delivery requires agreement between package manifests, Go registration and runtime state; a free plan does not convert existing pro packages. LDAP / TOTP source and manifests do not prove compilation or mounting. Release packs include no plugins; `nexadmin new` ships `totp` by default (disable with `--without-totp`) and other plugins are installed on demand — do not assume a bundled cron example. `plugin list` reads disk only (free shows `-` in its license column); also inspect runtime `GET /plugins`, startup logs and actual endpoints. The LDAP plugin accepts a CA certificate (PEM, multiple allowed) in its config page, so internal self-signed LDAPS/StartTLS works; leaving it empty trusts system roots only.
- `identity` permission codes in five levels: `identity:list` view sources, `identity:edit` create/update/delete & toggle, `identity:test` test connection / test user login, `identity:sync` directory sync (including sync status query), `identity:match` allowing `matchExisting` (binding external accounts to existing local accounts by username); admin endpoints return 401 anonymously and 403 without permission. The CAS login entry `/plugins/identity/cas/*` is public, with an in-process IP rate limit fallback (600/min) on top of framework rate rules.
- `identity` has manual LDAP/AD directory sync: `POST /plugins/identity/sources/:id/sync` submits a background job, `GET` on the same path queries status, requiring `identity:sync`, a valid license, an enabled LDAP source and `directorySync.enabled`. Callers are obtained via real `auth.ClaimsFrom(c)`; missing claims or non-positive user IDs refuse submission. Enumeration uses RFC 2696 `SearchPaged` with a page cap of `ldapPageSize=100` and a cumulative sync cap of `maxDirectoryEntries=5000`; if the server ignores paging controls, results are accepted within the same search and the run fails past 100 entries; plain `Search` has a hard client cap of 50 entries, which is not this paging-compat path's cap. Overruns, search errors, invalid subjects/accounts or duplicates fail the whole run; after all validations pass, accounts are added/updated one by one — partial failure may leave some accounts written. `deactivateMissing` (default off) only deactivates "accounts bound by this directory but absent this run" after a complete enumeration and full success, aborting when directory results fall below half the bound count; super-admin and built-in role accounts are untouched; deactivated accounts reappearing in the directory are not auto-restored. Manual trigger only — no automatic scheduled sync; real-directory acceptance is still pending.
- `assistant` routes require `assistant:use` and provide personal named-session CRUD, capped at 30 per tenant per user; queries explicitly include `tenant_id` and `user_id`. `plg_assistant_conversation` belongs to platform-database migrations, not tenant business tables; IDs are 32-character lowercase hexadecimal strings. Real table creation, persistence and tenant/user isolation still require separate acceptance.
- Disabling the `user` module also removes the login provider and the setup wizard's admin creation / initialized check (`core` collects these roles from modules) — do not disable it; other modules' disabling is covered by the "missing tables degrade to empty" convention, not individually tested.
- Data-scope clipping is actually applied to: user list & reads/writes, departments (writes; reads only with `scene=manage`), notices (admin-side clipped by creator, publish targets additionally restricted), operation and login logs (`core/datascope`, `modules/system/user/scope.go`).
- Operators of audits and background jobs: `core/auth`'s `ContextWithUser` / `UserFromContext` are for attribution only (not claims — they pass no permission checks); `core/job` puts the submitter and `job:<type>` into context when running handlers, and the `audit` plugin's operator resolution order is `WithOperator` → `ClaimsFromContext` → `UserFromContext` → system.

## 2. Login & authentication

### 2.1 Login methods

The login page shows methods per configuration; unconfigured ones are absent without errors (`GET /api/v1/auth/methods` returns `methods`, `providers`, `forgotEnabled`). All three methods issue the same JWT dual-token pair on success.

| Method | Enabled when | Rules (from code) |
| --- | --- | --- |
| Password | `auth.methods` contains `password` (default) | Factory parameter `login.captcha_enabled` is true, requiring a graphic captcha by default. When disabled, an adaptive policy requires it after ≥3 failures for the same username or IP (4 digits, 60 seconds, one-time). Repeated wrong passwords lock the account; fallback is 5 failures / 15 minutes, while the factory lock-duration parameter is 30 minutes, adjustable in the settings page `security` section or system params `login.max_retry` / `login.lock_minutes`; when the user doesn't exist a bcrypt comparison is still performed and the response uniformly says "wrong username or password" — the real reason only goes to logs; lockout treats existing and non-existing usernames identically |
| Phone | `auth.methods` contains `phone` and `sms.provider` is non-empty, otherwise hidden | The phone must already be bound to an account; codes are valid 5 minutes, one SMS per phone per 60 seconds, max 10 per phone per day, max 10 per IP per hour, 5 wrong entries void the code |
| Third-party quick login | `auth.providers.<name>` fully configured | OAuth2 authorization code flow: WeCom, DingTalk, Feishu, Google (OIDC, code + PKCE), GitHub, generic OIDC (Keycloak, Casdoor, etc.). `state` is one-time and callback URLs are exact-whitelisted; first login matches existing accounts by `match_by`, unmatched accounts are told to "bind an existing account", and `auto_create: true` auto-creates; only verified emails count; binding/unbinding is available in Profile |

Third-party config keys are in the Configuration Reference §3. The frontend callback page is `/admin/login/oauth/<provider>`; `redirect_uris` must be its full URL. Google / GitHub need outbound internet; configure `auth.proxy` on restricted networks.

### 2.2 Tokens & sessions

- Access token (default 30 minutes) and refresh token (default 168 hours), HS256; refreshing revokes the old refresh immediately; logout revokes the current access and its paired refresh; revocation records live in cache, and cache unavailability rejects requests.
- Optional `auth.single_session: true`: only the latest login's tokens are accepted for the same tenant+user; old access/refresh tokens are rejected, and refresh uses an atomic compare-and-swap so old sessions cannot overwrite new logins. Off by default; multi-instance requires shared Redis and identical configuration.
- Users of expired / suspended tenants cannot log in, cannot refresh, and their active requests are rejected too (§6).
- Forgot password (`POST /auth/forgot/send-code`, `/auth/forgot/reset`): uses available email / SMS channels and account bindings; codes last 10 minutes, with a 60-second cooldown, at most 5 attempts, and per-IP/per-account hourly limits. Non-unique email or phone bindings among enabled accounts cannot use recovery; account existence is not disclosed and an admin must reset the password.
- Online sessions are passively registered: logged-in requests record "user + IP + UA" into cache; force logout revokes the token of that session's last-seen entry.

## 3. Open platform

Module `openapi`, menu "System → Open Platform", permissions `openapi:list` (read) / `openapi:edit` (write).

- Personal access tokens (PAT): self-created in Profile by logged-in users; plaintext looks like `nxp_<32 random chars>` and is shown once, only the sha256 is stored; call with `Authorization: Bearer nxp_...` acting as the token owner; tokens cannot call the token management endpoints themselves; creation is rate-limited per user (`ratelimit.pat_create`).
- App-signed calls: open-platform apps (service accounts) bind role sets, optionally with IP whitelists; requests carry `X-App-Key`, `X-Timestamp`, `X-Nonce`, `X-Signature`. Signature = `hex(HMAC-SHA256(appSecret, METHOD\npath\nnormalizedQuery\nsha256hex(body)\ntimestamp\nnonce))` where `path` is the full URL path (including `/api/v1`, from `EscapedPath`), `timestamp` is Unix seconds with a ±5-minute window, `nonce` is one-time (replays rejected with `nonce already used`; replay detection depends on cache), the signed body is capped at 10MB; query keys are sorted, values of the same key sorted too, and both keys and values are `url.QueryEscape`d before joining with `&`. App secrets are resettable; the database stores `core/secret`-encrypted ciphertext. Validation failures are IP rate-limited (`ratelimit.openapi_sign`).
- Event catalog: `GET /openapi/events`. Currently 10 events: `user.created` `user.updated` `user.deleted`, `dept.created` `dept.updated` `dept.deleted`, `role.created` `role.updated` `role.deleted`, `notice.published`. Payloads contain only ids, necessary fields and tenant id — no password hashes or secrets. The in-process event bus has a 1024-slot queue; when full, events are dropped with a warning, never blocking business requests.
- Webhooks: configure a URL (SSRF-protected) and subscribed events; deliveries carry `X-Nex-Event`, `X-Nex-Delivery`, `X-Nex-Timestamp`, `X-Nex-Signature` (`hex(HMAC-SHA256(secret, timestamp + "." + body))`), a 5-second per-attempt timeout, up to 5 attempts (waiting 1 min, 5 min, 30 min, 2 h between), delivery records kept 30 days, with manual retry and test; webhook secrets look like `whsec_...` and are resettable, stored encrypted.
- API docs: `GET /openapi.json` (OpenAPI 3.0.3, raw JSON, no unified response wrapper) and `GET /openapi/routes` (runtime route list), permission `openapi:list`; disabled modules are absent from docs.
- MCP entry: `POST /mcp` (permission `mcp:list`), supporting JSON-RPC `initialize`, `ping`, `tools/list`, `tools/call` and notifications; protocol version `2025-03-26`. GET tools are invoked directly; explicitly registered write tools go through server-side confirmation (currently the message module registers `message_mark_read`) — arbitrary write routes or URLs are never exposed. Actual invocations re-run the original route's login, permission and tenant checks; app signatures cannot be reused as user Bearer credentials. `GET /mcp/tools` remains the catalog.
- MCP sessions: initialization returns `Mcp-Session-Id`, carried on subsequent calls; `DELETE /mcp` deletes the session. Metadata and up to 4 concurrent request slots per session live in the configured cache — multi-instance requires shared Redis / Valkey; the fixed validity is the shorter of 15 minutes and the init credential's remaining lifetime, never extended by activity or credential refresh, and bound to the specific identity and credential. Each instance tracks at most 1024 sessions and 64 concurrent requests; requests cap at 30 seconds, bounded by credential and session expiry. Cache failures reject session requests; re-initialize after credential changes. Protocol and write-confirmation boundaries are in the Features & Permissions document.
- MCP write confirmation: first a `confirmationId` and browser confirmation URL are returned; the user views it via the browser page `/admin/mcp/confirmations/:id` and approves with `POST /mcp/confirmations/:id/approve` using the session JWT plus local password, valid 5 minutes. PATs / app signatures cannot approve in the browser; after approval the same tool is called with the same `confirmationId`, re-verified and atomically consumed — a client-sent `confirmed` cannot bypass it. JSON or final single-SSE response wrapping is supported; batch requests, GET long-polling, event replay, progress streams and upstream tool SSE passthrough are not. Real Redis multi-instance and browser flows still require acceptance.

## 4. Source plugins and licensing (summary)

Plugins ship as source packages: install = copy files + recompile; `edition: free` skips license validation, `edition: pro` requires an offline-signed license file (`data/licenses/<id>.lic`, ed25519 signature, payload contains plugin, customer, issue / expiry dates, optional domains).

- Commands: `nexadmin plugin list | install | remove | keygen | sign | revoke` (see the `nexadmin` usage; `sign` / `revoke` are vendor-side issuing and revocation).
- Validation: checked one by one at startup; only valid ones mount routes and menus; missing / expired / tampered do not mount, with reasons shown on the plugin management page; import attempts to activate compiled plugins; inspect `needRestart` and logs. Failed hot mounts, website root routes and startup-time authentication extensions still require restart verification; universal hot activation is not guaranteed. A running re-check happens hourly; expired plugin routes return 403; 30 days before expiry `GET /plugins` carries `expiresSoon`; licenses with `domains` validate the current service domain (exact or `*.example.com`, sources in Configuration Reference §8); `requires_nexadmin` is validated against the framework version at load; the vendor may additionally distribute a signed revocation list `config/license-revoked.json` — licenses whose serial numbers appear are `revoked` (routes return 403, no restart needed after the file changes; an auxiliary measure — buyers can delete the list).
- Conventions and the development workflow are in `plugin-development.md`. That document's "known limitations" section matches code behavior (domain validation, version-range validation, expiry auto-offline are all implemented).
- `im` (live chat, pro) provides the agent console (System -> Live chat, `/im/conversations`) and, via the public `/plugins/im/visitor/*` endpoints, the floating chat widget on the public website (open conversation, send, incremental and paged pull, SSE, read receipts, typing indicator, satisfaction rating). When the plugin is not mounted that route group returns 404 and the widget hides itself. Closing a conversation writes a system message and pushes it over both SSE and WebSocket; agents can claim or transfer conversations, set tags, keep internal notes (agent-only), use quick replies, send images/files (content-sniffed with type/size allowlists, stored via config.storage local or S3) and export the filtered conversation list (xlsx, up to 5000 rows); a welcome message can be sent automatically when a visitor opens a conversation, banned words configured in settings are masked as *** in visitor messages; agents send presence heartbeats (90s TTL) and an optional auto-assign routes new conversations to the online agent with the fewest open chats (queued in order while nobody is online); a stats panel covers 7/30/90-day conversation counts, average first response, ratings, daily trend and per-agent workload; agents can recall their own messages within 2 minutes and merge a conversation into another open one (the source is closed with a merge note); reopening a closed conversation (the widget sends again) resets the previous rating/assignee on the same row and appends a "conversation reopened" note; visitors can rate a closed conversation from 1 to 5; conversations idle for 24 hours are closed automatically; closed conversations can be purged after a configurable retention period (checked hourly, removing messages, attachments and stored files), and deletion (single or batch, closed conversations only, cascaded and operation-logged) is restricted to super admins; the agent console exposes chat settings (business hours with timezone/workdays/time window plus an offline notice, and a proactive invite with delay and message), and pushes are broadcast across instances through `cluster.Bus`. The agent console prefers WebSocket and falls back to SSE (one-time ticket); the widget uses SSE plus polling. Visitor messages also write an in-app notification (category `im`, linking straight to the conversation) to every agent holding `im:list`; at most one per conversation per 5 minutes, and none while an agent is already viewing that conversation. See the appendix for the endpoint list.
- `ticket` (pro) is the after-sales agent console (`/ticket/tickets`): open -> processing <-> pending -> resolved -> closed; a public agent reply moves the ticket to "waiting on customer" and pauses the SLA clock, while a reporter reply resumes or reopens it. It includes priority, claim/transfer, internal notes (invisible to visitors), canned replies, bulk actions (claim/resolve/close/set priority), agent-created tickets, in-app notifications (new tickets to holders of `ticket:list`, assignment and reporter replies to the assignee, overdue tickets grouped per assignee), first-response/resolution time, satisfaction (1-5 rating after resolve/close, with average and distribution) and SLA-by-priority analytics. Password-free customer pages are available at `/ticket/new` (submit) and `/ticket/view` (track by ticket number, public replies only). Data enters from those pages, the console, or from a host frontend through the public `POST /plugins/ticket/visitor/tickets` endpoint (no login, IP rate-limited to 10/min; the unguessable ticket number acts as the visitor's capability token); server-side integrations can use `ticket.Create` and subscribe to `ticket.created/replied/resolved/closed`. The route group returns 404 when the plugin is not mounted. See the appendix for endpoints.

## 5. Roles & data scopes

### 5.1 RBAC

User → role → menu/button permissions; codes are named `module:action`; the super admin (`*`) short-circuits; permissions are read fresh from the login provider on every request — token snapshots are never trusted. New menus / permission points are visible only to the super admin by default; other roles are granted via "Role Management". Factory roles: `admin` (super admin, built-in, cannot be deleted / disabled / re-coded, cannot be assigned), `operator`, `auditor`, `editor` (disabled by default), `guest`, `secadmin` (demonstrates "cannot be assigned"). The latter five are demo roles with empty menu grants by default.

### 5.2 Data scopes

The role's `data_scope` field:

| Value | Meaning |
| --- | --- |
| `all` | All |
| `deptAndChild` | This department and below (relies on `sys_dept.path` materialized paths; falls back to this department when unavailable) |
| `dept` | This department |
| `self` | Self only |
| `custom` | Custom department set (`sys_role_dept`) |

With multiple roles the scopes union: any `all` role means all; preset scopes take the widest; custom departments merge with expanded preset departments into one set. The super admin is always "all". Operators always see themselves; users under "this department / department and below" without a department are treated as self-only. Scopes apply to reads and writes of users, departments, notices, operation logs and login logs (department read clipping applies only when the department page passes `scene=manage`; non-super-admin publishers with scopes narrower than "all" are also restricted when choosing publish targets).

### 5.3 The three checks for assignable roles

When non-super-admins assign roles to others (create / edit users, import, role options), three checks run independently in order, reporting the first failure (`core/roleassign`):

1. Assignable: `assignable=true`, not the built-in super-admin role, and permissions exclude `*`. Otherwise: "this role cannot be assigned by lower-level admins".
2. No permission escalation: the role's permission set must be a subset of the operator's current permission union. Otherwise: "the selected role's permissions exceed your own; it cannot be assigned".
3. No wider data scope: the role's scope cannot be wider than the operator's. Otherwise: "the selected role's data scope is wider than yours; it cannot be assigned". Decision table (row = role scope, column = operator scope; operators with `all` always allow):

| Role \ Operator | All | Dept & below | Dept | Self | Custom (set S) |
| --- | --- | --- | --- | --- | --- |
| All | Allow | Deny | Deny | Deny | Deny |
| Dept & below | Allow | Allow | Deny | Deny | Allow only if S is closed under subordinates (each department's whole subtree is in S) |
| Dept | Allow | Allow | Allow | Deny | Allow |
| Self | Allow | Allow | Allow | Allow | Allow |
| Custom (set R) | Allow | R ⊆ operator-visible departments | R ⊆ {this dept} | Allow only if R is empty | R ⊆ S |

Operator permissions and data scope are read live from the database. The public packages `nexadmin/core/roleassign` and `nexadmin/core/datascope` are available to business modules.

## 6. Tenant quotas & expiry enforcement

Tenants are maintained in "Tenant Management" with state (`active` / `suspended` / `expired`), expiry (empty = never), package, domains, and package-overriding user / role / storage (MB) quotas; a quota of 0 means unlimited. Tenant-level quotas take precedence over the package. The factory seeds 12 demo packages and several demo tenants.

Expiry & suspension:

- State: `suspended` / `expired` are rejected outright; when an expiry date has passed, requests within the grace window (`StateGrace`) pass, beyond it they are rejected. Grace days come from the system param `tenant.grace_days` (default 0); the reminder window from `tenant.expire_warn_days` (default 15, 0 = no reminder).
- Enforcement points: login, token refresh, and third-party token issuance all validate; logged-in non-super-admin requests (including PATs and app-signed calls) are validated in the per-request tenant middleware, with logout allowed so the frontend can clean up; rejections are HTTP 403 with `reason: TENANT_EXPIRED`. Platform super admins and the default (unmanaged) tenant are unaffected.
- Reminder: `GET /auth/me` carries `tenantExpireSoon` (expiry and remaining days) within `expire_warn_days` of expiry (including grace).
- Renewal / state: `PUT /tenants/:id/renew`, `PUT /tenants/:id/status` (permission `tenant:update`); tenant info has a 30-second in-process cache fallback, and modifications broadcast invalidation via `core/cluster`, so other instances usually take effect immediately — worst case 30 seconds if a broadcast is lost (multi-instance details in the Deployment Guide §10).

Quota enforcement (exceeding returns HTTP 403 with a `reason`):

| Quota | Enforced at | reason |
| --- | --- | --- |
| Users | Creating users, importing users, third-party auto-creation (tenant row locked with `SELECT ... FOR UPDATE` in the same transaction before counting — concurrent creation cannot exceed) | `TENANT_QUOTA_USERS` ("user count reached the package limit (N)") |
| Roles | Creating roles (same row-lock pattern) | `TENANT_QUOTA_ROLES` ("role count reached the package limit (N)") |
| Storage | tus session creation atomically pre-allocates by declared length, checking actual usage plus in-flight reservations; completion / cancellation release them, expired reservations are cleaned by lease (`modules/file/usage.go`) | `TENANT_QUOTA_STORAGE`; gated uploads require the cache to implement `cache.Lease` and are rejected otherwise; multi-instance requires shared cache — real Redis concurrency is not yet accepted |
| AI switch | Tenants can individually disable AI (`sys_tenant.ai_enabled`); the AI gateway checks on `/ai/*`; platform super admins and the default tenant are exempt | `TENANT_AI_DISABLED` |
| AI daily calls | Package `ai_daily_quota` (0 = unlimited), overridable per tenant (empty = package, 0 = unlimited); `filter`, `translate` and `write` share one count; platform super admins and the default tenant are exempt | `TENANT_QUOTA_AI` |

Packages can also limit the menu scope: a user's effective permissions are intersected with the permissions of the package's menus (an empty package menu means no limit). Platform-level operations that skip tenant isolation (super admin) bypass quotas.

### 6.1 Tenant self-service, branding and provisioning

- My tenant: `GET/PUT /tenant-self` acts only on the caller's tenant and cannot read or modify another tenant. It shows the plan, expiry, effective quotas and usage plus a 30-day trend, and lets tenant administrators maintain the company name, contact, phone, email and logo; tenant name / domain / plan / quotas / expiry stay platform-managed. Platform super admins and the default tenant have no tenant record, so the endpoint returns 404 and the UI shows "This account does not belong to a tenant".
- Branding: when `GET /settings/public` resolves a tenant by domain or request header, `sys_tenant.name` / `sys_tenant.logo_url` override the site name and logo, taking effect on the sign-in page and sidebar for that domain; empty values fall back to platform settings.
- Provisioning: creating a tenant with "create administrator" seeds a root department ("Headquarters"), a default post and a "Staff" role (dashboard / profile / about, data scope self, not a built-in role) in the same transaction, so a new tenant starts with an org skeleton.
- Usage snapshots: `sys_tenant_usage_daily` is unique on `(tenant_id, day)`; each visit to My tenant upserts that day's row, the trend shows at most 30 days, and the first day has a single point.
- Per-tenant rate limiting: `ratelimit.tenant_api` (default `limit: 0`, i.e. off) counts every `/api/v1` request per tenant so one tenant's traffic spike cannot starve the others; requests without a resolved tenant are not counted.

## Appendix: permission codes ↔ endpoints

Generated from source. Paths omit the `/api/v1` prefix (`/healthz` and `/metrics` are at the root). "Login only" = a valid login is required but no permission code is checked; "public" = no login (some protected by credentials, one-time state, signatures or rate limits). Grouped by module; endpoints sharing a permission code are on one line.

| Module | Permission | Endpoints |
| --- | --- | --- |
| about | `about:view` | `GET /about`<br>`GET /about/licenses` |
| ai | Login only | `GET /ai/status`<br>`POST /ai/filter`<br>`POST /ai/translate`<br>`POST /ai/write` |
| api-monitor | `apiMonitor:list` | `GET /monitor/api`<br>`GET /monitor/api/history` |
| cache-monitor | `cacheMonitor:list` | `GET /monitor/cache`<br>`GET /monitor/cache/history` |
| client-error | `serverMonitor:list` | `GET /monitor/client-errors` |
| client-error | Public | `POST /client-errors` |
| core | Public | `GET /auth/captcha`<br>`GET /auth/methods`<br>`GET /i18n/languages`<br>`GET /system/status`<br>`GET /healthz`<br>`GET /metrics` (loopback only without a token)<br>`POST /auth/login`<br>`POST /auth/refresh` |
| core | Login only | `GET /auth/me`<br>`POST /auth/logout` |
| core | `mcp:list` | `GET /mcp/tools` (returns only invokable route descriptions; original-route auth is never bypassed) |
| core | `mcp:list`, tool calls additionally check the original route's permission | `POST /mcp` (JSON-RPC; GET tools and explicitly registered write-confirmation tools)<br>`DELETE /mcp` (delete session) |
| core | Loopback only + `X-Shutdown-Token` (registered only by processes started via `start`; otherwise 404) | `POST /system/shutdown` |
| core setup wizard | Exists only when uninitialized; loopback, or with `X-Setup-Token` | `GET /setup/env`<br>`POST /setup/execute`<br>`POST /setup/test-cache`<br>`POST /setup/test-db`<br>`POST /setup/test-s3`<br>`POST /setup/test-ai` |
| dashboard | Login only | `DELETE /dashboard/layout`<br>`GET /dashboard/layout`<br>`GET /dashboard/stats`<br>`PUT /dashboard/layout` |
| exportcenter | Login only (users touch their own tasks; `job:all` touches all) | `DELETE /jobs/:id`<br>`GET /jobs`<br>`GET /jobs/:id`<br>`GET /jobs/:id/download`<br>`POST /jobs/:id/cancel`<br>`POST /jobs/:id/retry` |
| file | `file:delete` | `DELETE /file-groups/:id`<br>`DELETE /files`<br>`DELETE /files/:id` |
| file | `file:upload` | `DELETE /files/upload/:id`<br>`HEAD /files/upload/:id`<br>`PATCH /files/upload/:id`<br>`POST /file-groups`<br>`POST /files`<br>`POST /files/upload`<br>`PUT /file-groups/:id`<br>`PUT /files/:id`<br>`PUT /files/move` |
| file | `file:list` | `GET /file-groups`<br>`GET /files` |
| file | Public (public links rely on random strings; private files on expiring signatures) | `GET /files/:id/content`<br>`GET /files/public/:token/:name` |
| language | `language:list` | `GET /languages` |
| language | `language:update` | `PUT /languages` (whole-list save, returns `languages` and `needRestart`) |
| log | `loginLog:delete` | `DELETE /login-logs` |
| log | `operLog:delete` | `DELETE /oper-logs`<br>`POST /oper-logs/clean` |
| log | `loginLog:export` | `GET /login-logs/export` |
| log | `operLog:export` | `GET /oper-logs/export` |
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
| plugin | Login only (forwarded to not-yet-mounted plugins; the license gate and the plugin's own `Perm` decide) | `ANY /plugins/<id>/*rest` |
| profile | Login only | `DELETE /profile/bindings/:key`<br>`DELETE /profile/bindings/phone`<br>`DELETE /profile/sessions/:id`<br>`GET /profile`<br>`GET /profile/bindings`<br>`GET /profile/sessions`<br>`POST /profile/bindings/:key`<br>`POST /profile/bindings/phone`<br>`PUT /profile`<br>`PUT /profile/password` |
| profile | Public (IP rate-limited, rule `sms_code`) | `POST /auth/sms-code` |
| recycle | `recycle:delete` | `DELETE /recycle/:id` |
| recycle | `recycle:list` | `GET /recycle` |
| recycle | `recycle:restore` | `POST /recycle/:id/restore` |
| region | Login only | `GET /regions`<br>`GET /regions/tree` |
| region | `region:sync` | `GET /regions/status`<br>`POST /regions/sync`<br>`POST /regions/reset` |
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
| config | `config:delete` | `DELETE /configs`<br>`DELETE /configs/:id` |
| config | Login only | `GET /config/upload` |
| config | `config:list` | `GET /configs`<br>`GET /configs/:id`<br>`GET /configs/upload`<br>`GET /configs/upload-types` |
| config | `settings:list` | `GET /settings` |
| config | Public | `GET /settings/public` |
| config | `config:create` | `POST /configs` |
| config | `config:update` | `POST /configs/refresh-cache`<br>`PUT /configs/:id`<br>`PUT /configs/upload` |
| config | `settings:update` | `POST /settings/:section/test`<br>`PUT /settings/:section` |
| dept | `dept:delete` | `DELETE /depts/:id` |
| dept | `dept:list` | `GET /depts/:id`<br>`GET /depts/tree` |
| dept | `dept:create` | `POST /depts` |
| dept | `dept:update` | `PUT /depts/:id`<br>`PUT /depts/sort` |
| dict | `dict:delete` | `DELETE /dict/data`<br>`DELETE /dict/data/:id`<br>`DELETE /dict/types`<br>`DELETE /dict/types/:id` |
| dict | `dict:list` | `GET /dict/data`<br>`GET /dict/data/:id`<br>`GET /dict/types`<br>`GET /dict/types/:id` |
| dict | Login only | `GET /dicts/:code/items` |
| dict | `dict:create` | `POST /dict/data`<br>`POST /dict/types` |
| dict | `dict:update` | `PUT /dict/data/:id`<br>`PUT /dict/data/sort`<br>`PUT /dict/types/:id`<br>`PUT /dict/types/sort` |
| menu | `menu:delete` | `DELETE /menus/:id` |
| menu | `menu:list` | `GET /menus/:id` |
| menu | Login only | `GET /menus/mine` |
| menu | `menu:list` or `role:list` or `role:assign` | `GET /menus/tree` |
| menu | `menu:create` | `POST /menus` |
| menu | `menu:update` | `PUT /menus/:id`<br>`PUT /menus/sort`<br>`PUT /menus/:id/move` |
| post | `post:delete` | `DELETE /posts/:id` |
| post | `post:list` | `GET /posts`<br>`GET /posts/:id` |
| post | Login only | `GET /posts/options` |
| post | `post:create` | `POST /posts` |
| post | `post:update` | `PUT /posts/:id`<br>`PUT /posts/sort` |
| role | `role:delete` | `DELETE /roles/:id` |
| role | `role:list` | `GET /roles`<br>`GET /roles/:id`<br>`GET /roles/:id/menus` |
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
| user | `user:export` | `GET /users/export`<br>`GET /users/export-plan`<br>`POST /users/export-async` |
| user | `user:import` | `GET /users/import-template`<br>`POST /users/import` |
| user | `user:create` | `POST /users` |
| user | `user:resetPwd` | `POST /users/:id/reset-password` |
| user | `user:unlock` | `POST /users/:id/unlock` |
| user | `user:update` | `PUT /users/:id`<br>`PUT /users/:id/status` |
| example (sample business module) | Login only | `GET /example/hello` |
| example (sample business module) | `example:ping` | `GET /example/ping` |
| audit (plugin, prefix `/plugins/audit`) | `audit:clean` | `DELETE /plugins/audit/logs` |
| audit (plugin) | `audit:config` | `GET /plugins/audit/config`<br>`PUT /plugins/audit/config` |
| audit (plugin) | `audit:list` | `GET /plugins/audit/log-tables`<br>`GET /plugins/audit/logs`<br>`GET /plugins/audit/logs/:id` |
| codegen (plugin, prefix `/plugins/codegen`) | `codegen:list` | `GET /plugins/codegen/tables`<br>`GET /plugins/codegen/tables/:name/columns`<br>`POST /plugins/codegen/schema`<br>`POST /plugins/codegen/preview` |
| codegen (plugin) | `codegen:download` | `POST /plugins/codegen/download` |
| cron (plugin, prefix `/plugins/cron`) | `cron:edit` | `DELETE /plugins/cron/jobs/:id`<br>`POST /plugins/cron/jobs`<br>`PUT /plugins/cron/jobs/:id`<br>`PUT /plugins/cron/jobs/:id/enabled` |
| cron (plugin) | `cron:clean` | `DELETE /plugins/cron/logs` |
| cron (plugin) | `cron:list` | `GET /plugins/cron/handlers`<br>`GET /plugins/cron/jobs`<br>`GET /plugins/cron/stats` |
| cron (plugin) | `cron:log` | `GET /plugins/cron/logs`<br>`GET /plugins/cron/logs/:id` |
| cron (plugin) | `cron:run` | `POST /plugins/cron/jobs/:id/run` |
| scim (plugin) | `scim:list` | `GET /plugins/scim/tokens` |
| scim (plugin) | Dedicated SCIM Bearer token (no admin JWT or `scim:list` required) | `GET /plugins/scim/v2/Users`<br>`GET /plugins/scim/v2/Users/:id`<br>`GET /plugins/scim/v2/Groups`<br>`GET /plugins/scim/v2/Groups/:id` |
| scim (plugin) | `scim:token` | `POST /plugins/scim/tokens`<br>`DELETE /plugins/scim/tokens/:id` |
| scim (plugin) | Public (Bearer token) | `POST /plugins/scim/v2/Users`<br>`PUT /plugins/scim/v2/Users/:id`<br>`PATCH /plugins/scim/v2/Users/:id`<br>`DELETE /plugins/scim/v2/Users/:id`<br>`POST /plugins/scim/v2/Groups`<br>`PUT /plugins/scim/v2/Groups/:id`<br>`PATCH /plugins/scim/v2/Groups/:id`<br>`DELETE /plugins/scim/v2/Groups/:id` |
| i18n-editor (plugin) | `i18n-editor:list` | `GET /plugins/i18n-editor/entries`<br>`GET /plugins/i18n-editor/versions` |
| i18n-editor (plugin) | `i18n-editor:edit` | `PUT /plugins/i18n-editor/entries` |
| i18n-editor (plugin) | `i18n-editor:publish` | `POST /plugins/i18n-editor/publish`<br>`POST /plugins/i18n-editor/rollback` |
| im (plugin, path prefix `/plugins/im`) | `im:list` | `GET /plugins/im/conversations`<br>`GET /plugins/im/conversations/:no/messages`<br>`GET /plugins/im/conversations-export`<br>`GET /plugins/im/stats`<br>`POST /plugins/im/presence/heartbeat`<br>`GET /plugins/im/agents`<br>`GET /plugins/im/settings`<br>`POST /plugins/im/stream-ticket`<br>`GET /plugins/im/stream/:no` |
| im (plugin) | `im:reply` | `POST /plugins/im/conversations/:no/messages`<br>`POST /plugins/im/conversations/:no/messages/:seq/recall`<br>`POST /plugins/im/conversations/:no/claim`<br>`POST /plugins/im/conversations/:no/release`<br>`POST /plugins/im/conversations/:no/typing`<br>`GET /plugins/im/quick-replies`<br>`POST /plugins/im/conversations/:no/attachments` |
| im (plugin) | `im:edit` | `POST /plugins/im/conversations/:no/close`<br>`PUT /plugins/im/conversations/:no/tags`<br>`PUT /plugins/im/conversations/:no/note`<br>`POST /plugins/im/quick-replies`<br>`PUT /plugins/im/quick-replies/:id`<br>`DELETE /plugins/im/quick-replies/:id`<br>`POST /plugins/im/conversations/:no/transfer`<br>`POST /plugins/im/conversations/:no/merge`<br>`PUT /plugins/im/settings` |
| im (plugin) | Super admin (`middleware.RequireSuper`, operation-logged) | `DELETE /plugins/im/conversations/:no`<br>`POST /plugins/im/conversations-delete` |
| im (plugin) | Public (IP rate-limited; the unguessable conversation number acts as the visitor's capability token) | `GET /plugins/im/visitor/status`<br>`POST /plugins/im/visitor/conversations`<br>`GET /plugins/im/visitor/conversations/:no/messages`<br>`POST /plugins/im/visitor/conversations/:no/messages`<br>`POST /plugins/im/visitor/conversations/:no/read`<br>`POST /plugins/im/visitor/conversations/:no/typing`<br>`POST /plugins/im/visitor/conversations/:no/rating`<br>`POST /plugins/im/visitor/conversations/:no/attachments`<br>`GET /plugins/im/visitor/attachments/:token`<br>`GET /plugins/im/visitor/stream/:no` |
| ticket (plugin, prefix `/plugins/ticket`) | `ticket:list` | `GET /plugins/ticket/tickets`<br>`GET /plugins/ticket/tickets/stats`<br>`GET /plugins/ticket/tickets/:no/replies`<br>`GET /plugins/ticket/canned` |
| ticket (plugin) | `ticket:reply` | `POST /plugins/ticket/tickets/:no/replies` |
| ticket (plugin) | `ticket:edit` | `POST /plugins/ticket/tickets/bulk`<br>`POST /plugins/ticket/tickets/:no/status`<br>`POST /plugins/ticket/tickets/:no/assign`<br>`POST /plugins/ticket/canned`<br>`PUT /plugins/ticket/canned/:id`<br>`DELETE /plugins/ticket/canned/:id` |
| ticket (plugin) | `ticket:create` | `POST /plugins/ticket/tickets` |
| ticket (plugin) | Public (IP rate-limited; the unguessable ticket number acts as the visitor's capability token) | `POST /plugins/ticket/visitor/tickets`<br>`GET /plugins/ticket/visitor/tickets/:no`<br>`POST /plugins/ticket/visitor/tickets/:no/replies`<br>`POST /plugins/ticket/visitor/tickets/:no/rating` |
| pay (plugin, prefix `/plugins/pay`) | `pay:list` | `GET /plugins/pay/orders`<br>`GET /plugins/pay/orders/stats`<br>`GET /plugins/pay/orders/by-biz`<br>`GET /plugins/pay/orders/:no`<br>`GET /plugins/pay/refunds`<br>`GET /plugins/pay/refunds/:no`<br>`GET /plugins/pay/authorizations`<br>`GET /plugins/pay/authorizations/:no`<br>`GET /plugins/pay/channels`<br>`GET /plugins/pay/providers`<br>`GET /plugins/pay/callbacks` |
| pay (plugin) | `pay:edit` | `POST /plugins/pay/orders`<br>`POST /plugins/pay/orders/:no/close`<br>`POST /plugins/pay/orders/:no/sync` |
| pay (plugin) | `pay:refund` | `POST /plugins/pay/refunds` |
| pay (plugin) | `pay:auth` | `POST /plugins/pay/authorizations`<br>`POST /plugins/pay/authorizations/:no/capture`<br>`POST /plugins/pay/authorizations/:no/void` |
| pay (plugin) | `pay:channel` | `POST /plugins/pay/channels`<br>`DELETE /plugins/pay/channels/:id` |
| pay (plugin) | `pay:settle` | `GET/POST /plugins/pay/reconcile/tasks`<br>`GET /plugins/pay/reconcile/tasks/:id/diffs`<br>`POST /plugins/pay/reconcile/diffs/:id/resolve`<br>`GET/POST /plugins/pay/commission-rules`<br>`GET /plugins/pay/settlements`<br>`POST /plugins/pay/settlements/generate`<br>`POST /plugins/pay/settlements/:id/confirm`<br>`GET/POST /plugins/pay/payouts`<br>`POST /plugins/pay/payouts/:id/mark-paid` |
| pay (plugin) | Public (channel callbacks / mock cashier, signature verification applies) | `POST /plugins/pay/notify/:channelCode`<br>`GET /plugins/pay/mock/cashier`<br>`POST /plugins/pay/mock/cashier/confirm` |
| crm (plugin, prefix `/plugins/crm`) | `crm:list` | `GET /plugins/crm/customers`<br>`GET /plugins/crm/customers/:id`<br>`GET /plugins/crm/customers/resolve-external`<br>`GET /plugins/crm/opportunities`<br>`GET /plugins/crm/opportunities/:id` |
| crm (plugin) | `crm:edit` | `POST /plugins/crm/customers`<br>`PUT /plugins/crm/customers/:id`<br>`DELETE /plugins/crm/customers/:id`<br>`POST /plugins/crm/customers/bind-external` |
| crm (plugin) | `crm:opp` | `POST /plugins/crm/opportunities`<br>`PUT /plugins/crm/opportunities/:id`<br>`DELETE /plugins/crm/opportunities/:id` |
| aigateway (plugin, prefix `/plugins/aigateway`) | `aigateway:list` | `GET /plugins/aigateway/models`<br>`GET /plugins/aigateway/models/:id`<br>`GET /plugins/aigateway/usage`<br>`GET /plugins/aigateway/usage/summary` |
| aigateway (plugin) | `aigateway:edit` | `POST /plugins/aigateway/models`<br>`POST /plugins/aigateway/models/:id`<br>`DELETE /plugins/aigateway/models/:id`<br>`POST /plugins/aigateway/models/:id/ping` |

Permission points registered but not enforced at the route layer (menus / buttons): `dashboard:view`, `message:list`, `profile:view`, `docs:view`; enforced inside handlers: `notice:platform`, `job:all`.
