---
title: Open platform and access
summary: Open platform access, roles and data scopes, tenant quotas, and demo-mode rules.
---

## Open platform
Module `openapi`, menu "System → Open Platform", permissions `openapi:list` (read) / `openapi:edit` (write).

- Personal access tokens (PAT): self-created in Profile by logged-in users; plaintext looks like `nxp_<32 random chars>` and is shown once, only the sha256 is stored; call with `Authorization: Bearer nxp_...` acting as the token owner; tokens cannot call the token management endpoints themselves; creation is rate-limited per user (`ratelimit.pat_create`).
- **App-signed calls**: open-platform apps (service accounts) bind role sets and optionally an IP whitelist. Requests carry `X-App-Key`, `X-Timestamp`, `X-Nonce` and `X-Signature`.

  ```text
  hex(HMAC-SHA256(appSecret,
      METHOD\npath\nnormalizedQuery\nsha256hex(body)\ntimestamp\nnonce))
  ```

  - `path` is the full URL path (including `/api/v1`, from `EscapedPath`); `timestamp` is Unix seconds within ±5 minutes.
  - `nonce` is one-time (`nonce already used` on replay, via cache); the signed body is capped at 10MB.
  - Query keys are sorted, values of the same key sorted too, and both are `url.QueryEscape`d before joining with `&`.

  App secrets are resettable and stored as `core/secret` ciphertext; validation failures are IP rate-limited (`ratelimit.openapi_sign`).
- Event catalog: `GET /openapi/events`. Currently 11 events: `user.created` `user.updated` `user.deleted`, `dept.created` `dept.updated` `dept.deleted`, `role.created` `role.updated` `role.deleted`, `notice.published`, `job.finished` (published when a `core/job` task reaches a terminal state, e.g. an export finishing or failing). Payloads contain only ids, necessary fields and tenant id — no password hashes or secrets. The in-process event bus has a 1024-slot queue; when full, events are dropped with a warning, never blocking business requests.
- Webhooks: configure a URL (SSRF-protected) and subscribed events; deliveries carry `X-Nex-Event`, `X-Nex-Delivery`, `X-Nex-Timestamp`, `X-Nex-Signature` (`hex(HMAC-SHA256(secret, timestamp + "." + body))`), a 5-second per-attempt timeout, up to 5 attempts (waiting 1 min, 5 min, 30 min, 2 h between), delivery records kept 30 days, with manual retry and test; webhook secrets look like `whsec_...` and are resettable, stored encrypted.
- API docs: `GET /openapi.json` (OpenAPI 3.0.3, raw JSON, no unified response wrapper) and `GET /openapi/routes` (runtime route list), permission `openapi:list`; disabled modules are absent from docs.
- MCP entry: `POST /mcp` (permission `mcp:list`), supporting JSON-RPC `initialize`, `ping`, `tools/list`, `tools/call` and notifications; protocol version `2025-03-26`. GET tools are invoked directly; explicitly registered write tools go through server-side confirmation (currently the message module registers `message_mark_read`) — arbitrary write routes or URLs are never exposed. Actual invocations re-run the original route's login, permission and tenant checks; app signatures cannot be reused as user Bearer credentials. `GET /mcp/tools` remains the catalog.
- MCP sessions: initialization returns `Mcp-Session-Id`, carried on subsequent calls; `DELETE /mcp` deletes the session. Metadata and up to 4 concurrent request slots per session live in the configured cache — multi-instance requires shared Redis / Valkey; the fixed validity is the shorter of 15 minutes and the init credential's remaining lifetime, never extended by activity or credential refresh, and bound to the specific identity and credential. Each instance tracks at most 1024 sessions and 64 concurrent requests; requests cap at 30 seconds, bounded by credential and session expiry. Cache failures reject session requests; re-initialize after credential changes. Protocol and write-confirmation boundaries are in the Features & Permissions document.
- MCP write confirmation: first a `confirmationId` and browser confirmation URL are returned; the user views it via the browser page `/admin/mcp/confirmations/:id` and approves with `POST /mcp/confirmations/:id/approve` using the session JWT plus local password, valid 5 minutes. PATs / app signatures cannot approve in the browser; after approval the same tool is called with the same `confirmationId`, re-verified and atomically consumed — a client-sent `confirmed` cannot bypass it. JSON or final single-SSE response wrapping is supported; batch requests, GET long-polling, event replay, progress streams and upstream tool SSE passthrough are not. Real Redis multi-instance and browser flows still require acceptance.

## Roles & data scopes
### RBAC

User → role → menu/button permissions; codes are named `module:action`; the super admin (`*`) short-circuits; permissions are read fresh from the login provider on every request — token snapshots are never trusted. Matching supports exact permissions, `*` and module prefixes such as `module:*` (for example, owning `crm:*` covers `crm:list`, `crm:edit`, etc.). New menus / permission points are visible only to the super admin by default; other roles are granted via "Role Management". Factory roles: `admin` (super admin, built-in, cannot be deleted / disabled / re-coded, cannot be assigned), `operator`, `auditor`, `editor` (disabled by default), `guest`, `secadmin` (demonstrates "cannot be assigned"). The latter five are demo roles with empty menu grants by default.

Both frontend and backend support wildcard matching, but frontend `checkPerm` is only a UI aid for menus and buttons, not a security boundary. Endpoints must be declared with `core.Perm` and enforced authoritatively by backend `auth.HasPerm`; hiding controls in the frontend is never access control.

### Data scopes

The role's `data_scope` field:

| Value | Meaning |
| --- | --- |
| `all` | All |
| `deptAndChild` | This department and below (relies on `sys_dept.path` materialized paths; falls back to this department when unavailable) |
| `dept` | This department |
| `self` | Self only |
| `custom` | Custom department set (`sys_role_dept`) |

With multiple roles the scopes union: any `all` role means all; preset scopes take the widest; custom departments merge with expanded preset departments into one set. The super admin is always "all". Operators always see themselves; users under "this department / department and below" without a department are treated as self-only. Scopes apply to reads and writes of users, departments, notices, operation logs and login logs (department read clipping applies only when the department page passes `scene=manage`; non-super-admin publishers with scopes narrower than "all" are also restricted when choosing publish targets).

### The three checks for assignable roles

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

## Tenant quotas & expiry enforcement
Tenants are maintained in "Tenant Management" with state (`active` / `suspended` / `expired`), expiry (empty = never), package, domains, and package-overriding user / role / storage (MB) quotas; a quota of 0 means unlimited. Tenant-level quotas take precedence over the package. The factory seeds 12 demo packages and several demo tenants.

Expiry & suspension:

- State: `suspended` / `expired` are rejected outright; when an expiry date has passed, requests within the grace window (`StateGrace`) pass, beyond it they are rejected. Grace days come from the system param `tenant.grace_days` (default 0); the reminder window from `tenant.expire_warn_days` (default 15, 0 = no reminder).
- Enforcement points: login, token refresh, and third-party / SSO token issuance all validate; logged-in non-super-admin requests (including PATs and app-signed calls) are validated in the per-request tenant middleware, with logout allowed so the frontend can clean up; rejections are HTTP 403 with `reason: TENANT_EXPIRED`. Platform super admins and the default (unmanaged) tenant are unaffected.
- Security revocation: setting a tenant to suspended / expired, deleting it, or restoring it from an unusable state advances the tenant security version in shared cache and removes its online sessions and WebSockets. Renewal / enablement never revives old JWTs, refresh tokens or WS tickets; the 30-second tenant-info cache is not a revocation delay. SSO blocks new token issuance and this site's userinfo / organization reads; SSO access tokens already held by downstream apps need their introspection / denylist or natural expiry.
- Reminder: `GET /auth/me` carries `tenantExpireSoon` (expiry and remaining days) within `expire_warn_days` of expiry (including grace).
- Renewal / state: `PUT /tenants/:id/renew`, `PUT /tenants/:id/status` (permission `tenant:update`); tenant info has a 30-second in-process cache fallback, and modifications broadcast invalidation via `core/cluster`. That delay affects cached tenant details / quotas only; credential revocation uses shared security versions and does not depend on this broadcast (multi-instance details in the Deployment Guide §10).

Quota enforcement (exceeding returns HTTP 403 with a `reason`):

| Quota | Enforced at | reason |
| --- | --- | --- |
| Users | Creating users, importing users, third-party auto-creation (tenant row locked with `SELECT ... FOR UPDATE` in the same transaction before counting — concurrent creation cannot exceed) | `TENANT_QUOTA_USERS` ("user count reached the package limit (N)") |
| Roles | Creating roles (same row-lock pattern) | `TENANT_QUOTA_ROLES` ("role count reached the package limit (N)") |
| Storage | tus session creation atomically pre-allocates by declared length, checking actual usage plus in-flight reservations; completion / cancellation release them, expired reservations are cleaned by lease (`modules/file/usage.go`) | `TENANT_QUOTA_STORAGE`; <br>gated uploads require the cache to implement `cache.Lease` and are rejected otherwise; multi-instance requires shared cache — real Redis concurrency is not yet accepted |
| AI switch | Tenants can individually disable AI (`sys_tenant.ai_enabled`); the AI gateway checks on `/ai/*`; platform super admins and the default tenant are exempt | `TENANT_AI_DISABLED` |
| AI daily calls | Package `ai_daily_quota` (0 = unlimited), overridable per tenant (empty = package, 0 = unlimited); `filter`, `translate` and `write` share one count; platform super admins and the default tenant are exempt | `TENANT_QUOTA_AI` |

Packages can also limit the menu scope: a user's effective permissions are intersected with the permissions of the package's menus (an empty package menu means no limit). Platform-level operations that skip tenant isolation (super admin) bypass quotas.

### Tenant deletion protection

Before deletion, one transaction checks tenant business tables registered by enabled modules / plugins through `nexadmin/core/tenant.Provider`. Remaining functional data or soft-deleted rows block deletion, as does a configured isolated tenant database. The registry neither scans schemas at runtime nor cascades data; append-only audit / usage / history follows platform retention. After approval, the tenant security version advances and sessions / connections close before the tenant row is deleted, so a missing row cannot make old tokens look "unmanaged" and allow them through. See "Tenant data and deletion protection" in the Plugin Development Guide for module integration.

### Tenant self-service, branding and provisioning

- My tenant: `GET/PUT /tenant-self` acts only on the caller's tenant and cannot read or modify another tenant. It shows the plan, expiry, effective quotas and usage plus a 30-day trend, and lets tenant administrators maintain the company name, contact, phone, email and logo; tenant name / domain / plan / quotas / expiry stay platform-managed. Platform super admins and the default tenant have no tenant record, so the endpoint returns 404 and the UI shows "This account does not belong to a tenant". HTTP 403, 500 and network failures show a retryable load error instead, never masquerading as non-membership or displaying stale cached status.
- Branding: when `GET /settings/public` resolves a tenant by domain or request header, `sys_tenant.name` / `sys_tenant.logo_url` override the site name and logo, taking effect on the sign-in page and sidebar for that domain; empty values fall back to platform settings.
- Provisioning: creating a tenant with "create administrator" seeds a root department ("Headquarters"), a default post and a "Staff" role (dashboard / profile / about, data scope self, not a built-in role) in the same transaction, so a new tenant starts with an org skeleton.
- Usage snapshots: `sys_tenant_usage_daily` is unique on `(tenant_id, day)`; each visit to My tenant upserts that day's row, the trend shows at most 30 days, and the first day has a single point.
- Per-tenant rate limiting: `ratelimit.tenant_api` (default `limit: 0`, i.e. off) counts every `/api/v1` request per tenant so one tenant's traffic spike cannot starve the others; requests without a resolved tenant are not counted.

## Public demo mode
Setting `demo.enabled: true` (off by default; keys in the Configuration Reference §9) turns on demo-site write protection:

- Every non-read request to `/api/v1` (anything but GET/HEAD/OPTIONS) returns 403 with "Demo mode: modifying data is disabled".
- Built-in allow-list: sign in, refresh token, sign out and the WebSocket entry ticket. Other write endpoints must be added through `demo.allow` or a plugin's `Deps.DemoAllow` (a trailing `*` is a prefix).
- Self-service endpoints such as password change are deliberately **not** allowed, so the public demo account's password cannot be changed.
- Public pages at the site root, including the website contact form, do not pass through this gate.
- The frontend shows a "Demo mode" badge on the sign-in page and in the top bar from `demo` in `GET /system/status`. When `demo.account` / `demo.password` are set, the credentials are served only by that endpoint and only in demo mode, with a one-click fill on the sign-in page.
- Deployment steps (demo account, scheduled reset) are in the Deployment Guide.
