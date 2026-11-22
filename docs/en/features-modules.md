---
title: Built-in module list
summary: What each built-in module and plugin does, where it lives, and what it depends on.
---

## How to read this

- The **module name** is the name in `disabled_modules` (the module's `Name()`).
- Abbreviated permissions inherit the row's prefix: `create` on the user row means `user:create`. For the routes behind each permission, see [Permission codes and endpoints](features-permissions.md).
- Admin paths are relative to `/admin`: `/system/user` opens at `/admin/system/user`.

> [!TIP]
> Looking for "which page does X"? Read the [Admin tour](overview.md). Looking for "what does this permission code cover"? Read [Permission codes and endpoints](features-permissions.md).

## Which module is where

Listed in admin-menu order. Find a name below in "Module details" for the fine print.

| Module | Where | In one sentence |
| --- | --- | --- |
| `dashboard` | Dashboard `/dashboard` | Stat cards and trends; each user can rearrange their own layout |
| `user` | System → Users | Account CRUD, import/export, password resets; also the login provider |
| `role` | System → Roles | Assign menus, button permissions and data scopes |
| `menu` | System → Menus | Three-level menu tree; platform level |
| `dept` | System → Departments | Org tree and posts; cross-level drag moves |
| `post` | Departments → Posts tab | Post CRUD and export |
| `dict` | System → Dictionaries | One place for dropdown options; platform level |
| `config` | Settings → Advanced → Parameters | Parameter KV pairs and upload limits; platform level |
| `language` | Settings → Advanced → Languages | Whole-list language save; platform level |
| `recycle` | System → Recycle bin | Restore or purge soft-deleted records |
| `plugin` | Development & security → Plugins | Install, license, toggle; some operations need a restart |
| `backup` | System → Backups | Manual/scheduled backup and download; super admin only |
| `openapi` | Development & security → Open platform | Apps, webhooks, deliveries, API docs |
| `sso` | Settings → Security & sign-in → SSO | This site as an OIDC Provider; also App portal `/portal` |
| `notice` | System → Announcements | Rich-text notices, scheduled publishing, read stats |
| `message` | Bell icon → Message center `/message` | Personal messages, unread counts, SSE push |
| `file` | System → Files | Folders, resumable uploads, public/signed links |
| `exportcenter` | Import & export → Export center `/jobs` | Async export jobs, download / cancel / retry |
| `log` | Monitoring → Operation logs / Sign-in logs | Query, prune, export |
| `sendlog` | Monitoring → Delivery logs | SMS/email/bot delivery results and failure reasons |
| `online` | Monitoring → Online users | Active sessions and force logout |
| `server-monitor` | Monitoring → Server monitor | CPU/memory/disk/Go runtime, with a frontend-errors tab |
| `client-error` | Server monitor → Frontend errors | JS error reports (anonymous, IP rate-limited) |
| `cache-monitor` | Monitoring → Cache monitor | Redis/Valkey memory, hit rate, key count |
| `api-monitor` | Monitoring → API monitor | QPS, latency and error rate over the last 30 seconds |
| `tenant` | Tenants → Tenants / Packages | Tenants and packages; admins use "My tenant" |
| `profile` | Profile `/profile` | Details, password, bindings, devices, access tokens |
| `about` | About `/about` | Version, build time, dependency licences |
| `region` | No menu | Province/city/district data and sync |
| `ai` | No menu | AI status, natural-language filtering, translation, writing |

## Platform data: who may change it

Some settings are **one row shared by every tenant** — those tables have no `tenant_id`. "May use this module" and "may overwrite this row" are therefore different questions; a permission code only answers the first.

The criterion is `core.IsPlatformCaller` (`core/router.go:51`): **a super admin, or a request whose tenant resolves to the default tenant**. The single source is `tenant.IsPlatformCallerID`, shipped to the frontend per session as `auth.User.Platform`. On top of that, `core.RequirePlatform()` (`core/router.go:63`) guards **55 write routes**: the permission point *and* this gate must both pass.

| These write routes | Count | Code |
| --- | --- | --- |
| Ticket shared config: canned replies / SLA and named policies / groups and members / automation rules / custom fields / plugin settings | 17 | `plugins/ticket/handler.go` |
| Code generator: install and save config | 4 | `plugins/codegen/plugin.go` |
| Parameters and settings sections | 8 | `modules/system/config/module.go` |
| Dictionary types and items (both `/sort` included) | 10 | `modules/system/dict/module.go` |
| Menus (drag sort and move included) | 6 | `modules/system/menu/module.go` |
| Whole-list language save | 1 | `modules/system/language/module.go` |
| Region `preview`/`import`/`sync`/`reset`/`rollback` | 5 | `modules/region/module.go` |
| LDAP / Maps / Media credential pages | 4 | — |

**What a tenant admin sees**: even holding `ticket:edit`, `dict:update`, `menu:update` or `settings:update`, the save returns 403 with `error.platform_only`. The message says outright that this row is shared by every tenant and only a platform administrator can change it — no re-checking your own roles. The UI greys out the write buttons and shows one line of explanation (`<PlatformNotice />`) instead of hiding the entry point.

- In a single-tenant deployment everyone is the default tenant, hence a platform caller; the gate changes nothing there. It only bites with multiple tenants.
- Backups (`superOnly()`) and tenants / packages (group-level `RequireSuper`) use stronger guards and are not among those 55; a "platform" cell in the table above does not imply all three guards.
- Opening these settings to tenant self-service first requires tenant-scoping the tables (three-engine migrations adding `tenant_id` plus reshaped unique keys). That is a design decision to make, not a TODO to queue.
- [Permission codes and endpoints](features-permissions.md) marks each route that carries the extra gate ("also platform-level").

## System Settings: 13 sections

| Group | Sections (`?tab=`) | Served by |
| --- | --- | --- |
| Site | Site information `site` | settings endpoints in `config` |
| Security & sign-in | Login security `security`, Login methods `auth`, LDAP `ldap`, SSO `sso` | settings for the first two; `/plugins/ldap/*` for LDAP; `/sso/*` for SSO |
| Storage & integrations | File storage `storage`, AI model `ai`, SMS `sms`, Notifications `notify`, Maps `maps` | settings for the first four; `/plugins/maps/*` for maps |
| Advanced | Languages `language`, Parameters `params` | `/languages` for languages; `/configs` for parameters |
| Runtime | Cache `cache` (read-only) | shown by the settings API only; driver and address come from `cache.*` in `config.yaml` at boot |

- **13 sections do not mean 13 `/settings/:section` values.** That endpoint accepts eight (site, security, cache, storage, ai, sms, auth, notify), and cache is read-only; languages, parameters, LDAP, SSO and maps have their own read/write APIs. Connectivity tests only apply to settings with supported probes.
- The LDAP, SSO and map menu rows stay in place but are hidden. Do not delete them or change their parent — permission codes remain assignable. The legacy `/ldap/config`, `/system/sso-apps` and `/system/map` URLs redirect to the matching section.
- Live cache state is under Monitoring → Cache monitor.

## Module details

### System

- `user`: account CRUD (batch included), enable/disable, password reset, unlock, role / department / post assignment, xlsx import/export (exports go to the export center). Also the login provider and the setup wizard's admin-creation entry — **do not disable it**, or sign-in goes away too.
- `dept`: tree via `parent_id` plus a materialized path; a cross-level drag (`PUT /depts/:id/move`) rewrites the whole subtree's path in one transaction. Reads return the full tree by default because every department dropdown depends on it; only the department page's `scene=manage` calls clip it for non-super-admins whose scope is narrower than "all" — ancestors up to the root are kept and marked `readonly:true`, and out-of-scope detail is 403.
- `dict`: factory types cannot be deleted or re-coded, and their factory items cannot be deleted or re-valued (409 `dict.builtin`); name / label / sort / style / status remain editable. Dropdowns and tags read `GET /dicts/:code/items`, which only requires a login.
- `notice`: rich-text announcements to all / roles / departments / users, draft → published → revoked, scheduled publish and expiry, pinned, a login popup for important ones, read stats; deletes go to the recycle bin. A non-super-admin whose scope is not "all" cannot publish to everyone or platform-wide, and specified departments / users must be within their scope (specified roles are unrestricted).
- `recycle`: soft-deleted users / notices / roles / dictionary types & items / posts, filterable by name, type and deletion date. Only a purge cascades to related rows; a restore keeps them.
- `file`: folders, tus resumable uploads, previews, public and signed links, move and rename, local or S3 storage, bounded by the tenant's storage quota.
- `backup`: immediate backup, scheduled backups with retention, download and delete; also requires super admin.

### Monitoring

- `log`: operation and sign-in logs can be queried, deleted, pruned and exported. Exports always go to the export center with an explicit column whitelist (no request params, responses or stack traces). `log.export_max_rows` is a **per-job** cap — exceeding it fails the whole job and asks for narrower filters.
- `sendlog`: channel, scene (captcha / notice / phone bind / account notice / security alert / test), recipient, title or template name, status, failure reason and duration. Recipients are masked (`138****1234`, `a***@x.com`); codes, bodies and keys are never stored. Writes are enqueued and a failure only reaches the log; in-app messages are not recorded.
- The three monitors share `sys_monitor_snapshot`: a snapshot every 30 seconds, kept 7 days. With the memory cache driver only process memory and uptime are available (key count and hit rate are 0); API stats are in-process and reset on restart.
- `online`: session list and force logout, backed by cache indexes rather than a table.
- Data-scope clipping actually applies to: users, departments (writes plus the department page's `scene=manage` read), notices (admin-side by creator, with extra publish-target limits), and operation / sign-in logs.

### Development, security and menu-less endpoints

- `plugin`: installed plugins with version, licence state and a runtime toggle. Disabling immediately closes that plugin's routes and menu entries; plugins with workers need a restart after being re-enabled. See the [plugin development guide](plugin-dev.md).
- `openapi`: open-platform apps, event catalog, webhooks and deliveries, online API docs; personal access tokens live in Profile.
- `sso`: this site as an OIDC Provider — downstream app registration, secret reset, signing-key rotation. App portal `/portal` lists the apps you can enter.
- `region`: provinces/cities/districts are readable with a login; `region:sync` is needed for sync status, and the five write endpoints also require a platform caller — what they replace is the snapshot every tenant's address picker reads. Data ships embedded in `core/region`; synced snapshots live in `sys_region_snapshot` (no `tenant_id`).
- `ai`: `GET /ai/status`, `POST /ai/filter` (natural-language filtering), `/ai/translate`, `/ai/write` (writing/polishing of caller-submitted text, with the entry inside the notice form). Without a configured model the entries are hidden; tenants can disable AI or hit a daily call cap.
- `identity`: five permission levels — `identity:list` view, `identity:edit` CRUD and toggle, `identity:test` test connection / user login, `identity:sync` directory sync, `identity:match` allow binding external accounts to local ones by username. Admin endpoints return 401 anonymously and 403 without permission; the public CAS entry `/plugins/identity/cas/*` has an in-process IP fallback limit (600/min).
- `identity` LDAP/AD directory sync is **manual only**: `POST /plugins/identity/sources/:id/sync` submits a job, `GET` on the same path reports status; it needs `identity:sync`, a valid licence, an enabled LDAP source and `directorySync.enabled`. Enumeration uses RFC 2696 `SearchPaged` (100 per page, 5,000 per run) and fails past 100 entries if the server ignores paging controls. `deactivateMissing` (off by default) only deactivates "bound accounts absent from this run" after a complete enumeration and full success; super admins and built-in role accounts are untouched, and reappearing accounts are not restored. A partial failure can leave some accounts written; real-directory acceptance is still pending.
- `assistant`: personal named-session CRUD requiring `assistant:use`, capped at 30 per tenant per user. `plg_assistant_conversation` belongs to platform-database migrations, not tenant business tables.
- `about`: version, build time, database engine, Go version and the open-source licences of backend dependencies.

### Plugins and licensing

- Base services and general-purpose plugins are free; business enhancements are paid. The initial free plan covers `cron`, `codegen`, `imports`, `totp`, `ldap`, `maps`, `crm` and `realname`; others are planned as pro. Formal purchase terms have not been published, so the catalogue is not a price or licence commitment.
- A free plan does not convert existing pro packages; the master repo compiles and mounts `totp` and `ldap`, while the manifest or source tree of the other free plugins does not prove they were compiled and mounted. Release packs contain no plugins; `nexadmin new` ships only `totp` by default (disable with `--without-totp`). Install the rest on demand or name them at derivation time with `--plugins <id>,<id>`.
- `plugin list` reads disk only (free shows `-` in the licence column); for runtime state use `GET /plugins`, startup logs and the actual endpoints.
- The LDAP plugin accepts a CA certificate (PEM, multiple allowed) in its config page, so internal self-signed LDAPS/StartTLS works; leaving it empty trusts system roots only.

## Easy-to-miss rules

- The factory menus include a "Help Docs" external link (`https://example.com/docs`, `docs:view`), disabled by default — a placeholder.
- `internal/modules/example` is a sample business module (module name `example`, `GET /example/hello`, `/example/ping`), not part of the framework.
- Audit and job operators: `core/auth`'s `ContextWithUser` / `UserFromContext` are **for attribution only**, not Claims, and pass no permission checks. `core/job` puts the submitter and `job:<type>` into context; the `audit` plugin resolves the operator via `WithOperator` → `ClaimsFromContext` → `UserFromContext` → system.
- **Standing invariant**: a table with no `tenant_id` must have a registered answer to "what guards it" — whole-group closure, a write-route entry, a stronger gate, no HTTP write path, or parent-row ownership. To open one of those consoles, first add the tenant column, then remove the registration; never "open it first and decide later".
