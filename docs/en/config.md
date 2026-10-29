---
title: Configuration Reference
summary: Config loading rules plus every section's keys, defaults, and runtime switches.
---

# NexAdmin Configuration Reference

This document lists every configuration key of `config/default.yaml`. Content comes from `config/default.yaml`, `core/config/config.go`, `core/config/providers.go` and consumer code; the table draft was expanded from default.yaml by script and the description column manually checked against code.

## 1. Loading rules

- Read order: `config/default.yaml` (framework area, overwritten on upgrade) → `config/config.yaml` (user area, ignored if absent) → environment variables. Later layers override earlier ones.
- Merge style: maps merge key by key; lists are replaced wholesale (`disabled_modules`, `languages`, `auth.methods`, `server.cors_origins`, `server.trusted_proxies` must be written in full).
- Paths are relative to the process working directory: config, `data/`, `logs/`, `config/locales/` and `migrations/` are all found from the current directory, so the service must start from the project root (or an equivalent layout). The `data/` and `config/locales/` directory names are fixed with no config keys.
- Environment variable shape: prefix `NEXADMIN_`, key upper-cased, `.` → `_`. Examples: `server.port` → `NEXADMIN_SERVER_PORT`, `database.password` → `NEXADMIN_DATABASE_PASSWORD`, `ratelimit.login.limit` → `NEXADMIN_RATELIMIT_LOGIN_LIMIT`, `ratelimit.pat_create.window` → `NEXADMIN_RATELIMIT_PAT_CREATE_WINDOW`.
- Environment variable types: integers, booleans, durations (`45m`, `2h`), comma-separated lists (`NEXADMIN_DISABLED_MODULES=post,notice`, `NEXADMIN_SERVER_CORS_ORIGINS=https://a.com,https://b.com`, `NEXADMIN_AUTH_METHODS=password,phone`). `languages` (an object list) and `auth.providers` (a map) via env: unverified — prefer `config.yaml`.
- `${ENV}` references: string fields of `auth.providers.*` expand `${NAME}` from environment variables, yielding an empty string when unset (that provider counts as unconfigured); `storage.s3.*` string fields expand too, but unset variables fail with `config: environment variable NAME is not set`. Other keys do not expand `${}`.
- Startup validation (`core/config`'s `validate`): `app.env` must be `dev` / `production`; `server.port` 1–65535; `database.type` must be `mysql` / `mariadb` / `postgres`; `database.node_id` 0–1023; `cache.driver` must be `memory` / `redis` and redis requires `cache.addr`; `languages` cannot be empty; `auth.access_ttl` and `auth.refresh_ttl` must be positive; `storage.driver` must be `local` / `s3` and s3 requires endpoint, bucket, access_key, secret_key; `backup.schedule` is empty or `HH:MM`; `backup.keep_days` and `backup.keep_count` cannot be negative. Failures look like `config: server.port 99999 out of range`.

## 2. app / server / database / cache

| Key | Type | Default | Purpose | When to change |
| --- | --- | --- | --- | --- |
| `app.env` | string | `dev` | `dev` or `production`. production: Gin release mode, JSON logs to stdout and `logs/nexadmin.log`, CORS whitelist-only, `reinit` / `restore` additionally require typing the database name; dev: console logs, CORS allows any origin | Must be `production` in real environments |
| `app.name` | string | `NexAdmin` | For logs / default naming. The wizard also writes the site name here, but the login page, top bar and PWA manifest display the name from the settings page "Site" section's `siteName` (`sys_setting`, written by the wizard and `init --config`, edited in the settings page afterwards) | No need to change |
| `app.timezone` | string | `UTC` | IANA timezone name. Dashboard statistics, logs, backup schedule (`backup.schedule`) and the AI prompt's "today" read it; the wizard validates and writes the chosen timezone | Match the business timezone |
| `app.auto_migrate` | bool | `true` | Controls platform-database auto-migration at startup for initialized services; tenant databases additionally require `database.isolation.auto_migrate=true`. With auto-migration off, pending migrations are still checked and any pending item refuses startup until `nexadmin-server migrate` runs | Set `false` to separate upgrades from startup in production |
| `server.port` | int | `8080` | HTTP listen port (listens `:port`, all interfaces) | Port conflicts or container mapping |
| `server.cors_origins` | list | `[]` | CORS origin whitelist, exact match, no `*`; empty means same-origin only. dev ignores the whitelist and allows any origin | When frontend and backend deploy on different origins |
| `server.trusted_proxies` | list | `[]` | Trusted reverse-proxy IPs / CIDRs (env `NEXADMIN_SERVER_TRUSTED_PROXIES=127.0.0.1,10.0.0.0/8`). Empty = trust no proxy; the real client IP is only the TCP peer and forged `X-Forwarded-For` / `X-Real-IP` are ineffective; non-empty = these headers are trusted only when the peer is in the list, taking the rightmost address not in the list. Used by rate limiting, login / operation log IPs, online sessions, open-app IP whitelists; the wizard's local check, the `/metrics` loopback check and the local shutdown endpoint always use the peer address. Every entry must be a valid IP or CIDR or startup fails with `config: server.trusted_proxies has invalid IP/CIDR ...` | **Required behind a reverse proxy (nginx / LB)**, otherwise all users share the proxy's rate-limit buckets; too wide lets anyone forge source IPs |
| `database.type` | string | `mysql` | `mysql` / `mariadb` / `postgres` (the wizard's `postgresql` is written as `postgres`) | Match your database |
| `database.host` / `database.port` | string / int | `127.0.0.1` / `3306` | Database address | PostgreSQL usually 5432 |
| `database.user` / `database.password` | string | `root` / empty | Database credentials; the password never enters logs, and the wizard writes `config.yaml` with file mode 0600 | Pass passwords via env in production |
| `database.name` | string | `nexadmin` | Database name; the wizard can optionally auto-create it | Match your database |
| `database.node_id` | int | `0` | Snowflake ID node number 0–1023. Multi-instance instances must differ; with redis cache, startup uses a TTL lease to detect duplicates and refuses startup when another live instance holds it (no detection with `memory`), see Deployment Guide §10 | Multi-instance deployments |
| `cache.driver` | string | `memory` | `memory` (in-process) or `redis` (Valkey-compatible). Token revocation, captchas, login lockout, rate-limit counters and online sessions all live in cache | Multi-instance should use `redis` |
| `cache.addr` / `cache.password` | string | empty / empty | Redis address (e.g. `valkey:6379`) and password; `driver=redis` requires addr, and unreachable fails startup with `cache: redis/valkey at <addr> is unavailable` | When using redis |

### 2.1 database.isolation (optional, off by default)

Fields are defined in `core/config/config.go`; pool and table boundaries are validated by `core/database/isolation.go`. The current default example contains comments only; the following documents the configuration shape — not present in existing deployment configs.

| Key (all under `database.isolation`) | Default / fallback | Purpose |
| --- | --- | --- |
| `enabled` | `false` | Enables tenant business-database routing configured on the server; existing data is never split or migrated automatically |
| `auto_migrate` | `false` | For initialized services, tenant migrations run only when both this and `app.auto_migrate` are `true`; otherwise they are checked and pending items refuse startup. The wizard / `init` explicitly run platform migrations, with tenant parts run or checked only per this switch; manual `migrate` is not restricted by either automatic switch |
| `max_pools` / `max_open` / `max_idle` / `lifetime` | `16` / `10` / `0` / `30m` | The first two and lifetime fall back to defaults when 0; configured tenant count must not exceed max_pools, and max_idle must not exceed max_open |
| `tables` | empty | Required list of tenant business tables; must not start with `sys_` nor overlap platform tables |
| `platform_tables` | empty | Platform tables beyond the built-in control-plane ones; must match actual model and SQL table names |
| `tenants` | empty | Required connection map; keys are canonical tenant IDs (no leading zeros), values contain `type`, `host`, `port`, `user`, `name`, `password_env` |

Configuration shape example (for future reference only — not a validated configuration):

```yaml
database:
  isolation:
    enabled: false
    auto_migrate: false
    max_pools: 16
    max_open: 10
    max_idle: 0
    lifetime: 30m
    tables: [biz_order]             # replace with real business tables; cannot enable with empty
    platform_tables: []            # add plg_assistant_conversation when enabling assistant, see below
    tenants:
      "123":
        type: mysql                # must match the platform database's normalized engine
        host: db-tenant.example.com
        port: 3306
        user: tenant_app
        name: tenant_123
        password_env: TENANT_123_DB_PASSWORD
```

`password_env` names an environment variable that must exist and be non-empty at runtime; connection info never comes from the tenant table. Platform and tenant connection targets must differ. Built-in `sys_*` control-plane tables (auth, org, operations) stay in the platform database — carrying `tenant_id` does not mean moving into a separate database; only business tables in `tables` route to tenant databases. Unknown tables, native SQL / joins that cannot prove single-database boundaries, and cross-database operations are rejected; business and plugin migrations must declare platform / tenant targets explicitly. Backup/restore and `migrate --tenant` scope are in the Deployment Guide.

The assistant manifest declares `migration_target: platform`. When enabling that plugin with separate-database isolation, add `plg_assistant_conversation` to `platform_tables` — it must not go into tenant business `tables`. Go models, three-engine SQL and the PostgreSQL index share the table name `plg_assistant_conversation`; the Go entity primary key `id` is a `string` generated by `newID()` using `crypto/rand` (16 bytes, hex-encoded to 32 lowercase chars), SQL type `CHAR(32)`; entry validation `validConversationID` checks length and the `0–9`/`a–f` charset, not using `pkg/idgen` or snowflake IDs. All session queries carry explicit `tenant_id` and `user_id`. During development (framework 0.1.0 unreleased) the 001 migration was fixed directly — dev databases that already ran the old 001 must drop and recreate the table manually. Transient run/lease IDs are random too. sqlmock unit tests cover create, list, read and tenant/user isolation; this pass only reviewed code and fixed documentation — no tests or real SQL were run, and adding the configuration does not mean tables exist.

### 2.2 MCP cache dependency

MCP session metadata and four concurrency slots per session live in the configured cache, which must implement atomic `cache.Locker`; multi-instance requires shared Redis / Valkey — `memory` is process-local. Initialization returns `Mcp-Session-Id`; sessions have a fixed validity no longer than 15 minutes and the init credential's expiry — activity and credential refresh never extend it; sessions bind to the user, tenant and specific credentials and cannot replace login. Each instance tracks at most 1024 sessions and 64 concurrent requests — these are code constants, not YAML keys. Per-request cap 30 seconds, also bounded by credential and session expiry. Cache failures reject session requests; re-initialize after credential changes. Protocol and write-confirmation boundaries are in the Features & Permissions document.

## 3. languages / auth / sms

| Key | Type | Default | Purpose | When to change |
| --- | --- | --- | --- | --- |
| `languages` | list | `zh-CN` (default), `en-US` | Seeds only an empty `sys_language`; entries may contain `code, name, dir, default, short, htmlLang, ogLocale, prefix, dateFormat`. Once populated, `sys_language` governs switching, negotiation and the website; YAML does not overwrite it | Platform super admins maintain initialized systems through System Settings → Advanced → Languages. Language-set or prefix changes require restart; source dictionaries and Markdown require rebuilding |
| `auth.methods` | list | `[password]` | Login methods: `password`, `phone`. `phone` additionally requires `sms.provider`, otherwise hidden | Enabling phone login |
| `auth.providers` | map | `{}` | Third-party login keyed by `wecom` / `dingtalk` / `feishu` / `google` / `github` / `oidc`; missing `client_id` / `client_secret` / `redirect_uris` counts as unconfigured — hidden from the login page without errors. `wecom` also needs `agent_id`; `oidc` needs `issuer`, or all of `auth_url`, `token_url`, `jwks_url`. Fields in 3.1 | Enabling third-party login |
| `auth.proxy` | string | empty | HTTP(S) proxy for outbound requests (third-party login, SMS etc.); empty = direct | When the server cannot reach Google / GitHub |
| `auth.jwt_secret` | string | empty | JWT signing key, at least 32 characters. Empty auto-generates on first start and saves to `data/jwt.secret` (mode 0600) | Multi-instance must match everywhere (share the file or configure explicitly) |
| `auth.access_ttl` | duration | `30m` | Access token validity, must be positive | Per security requirements |
| `auth.refresh_ttl` | duration | `168h` | Refresh token validity, must be positive | Per security requirements |
| `auth.single_session` | bool | `false` | When on, only the user's latest login tokens are accepted; refresh uses atomic compare-and-swap so old sessions cannot reclaim a new login. Tokens issued before enabling require re-login. Multi-instance must share Redis with identical config, the cache must implement `cache.Lease`, and cache failures reject | When requiring one session per account |
| `auth.allow_register` | bool | `false` | Self-registration switch: the login page gains a register entry (`POST /auth/register`). Registration enforces a graphic captcha (same as login, IP-bound, one-time) and IP rate limiting (10/hour); the username rule matches the wizard admin (letter start, 3-32 letters/digits/underscore); registration enters the currently resolved tenant (the default tenant without domain binding) and is bounded by its package user quota; publishes `user.created` on success | Public deployments with self-registration |
| `auth.register_roles` | list | `[]` | Default role codes for registered users (resolved in the current tenant, e.g. `[guest]`); if configured and any role is missing, registration fails entirely (no silent downgrading). Empty = no roles; registered users can log in with an empty sidebar until an admin assigns roles | Default permissions for registrants |
| `sms.provider` | string | empty | SMS channel: empty (off), `log` (dev only), `webhook`, `aliyun`, `tencent` | Enabling phone login / phone binding |
| `sms.template` | string | `login` | Captcha template identifier: Aliyun takes the approved TemplateCode (variable `${code}`); webhook puts it verbatim into the request body's `template` field | When using Aliyun |
| `sms.webhook.url` / `sms.webhook.secret` | string | empty / empty | Webhook channel: POSTs JSON to your gateway with `X-Timestamp` / `X-Nonce` / `X-Signature: sha256=<hex>`, signature = HMAC-SHA256(secret, timestamp + "." + nonce + "." + body) | With `provider=webhook` |
| `sms.aliyun.access_key_id` / `access_key_secret` / `sign_name` / `endpoint` | string | empty | Aliyun SMS (Dysmsapi); empty `endpoint` defaults to `https://dysmsapi.aliyuncs.com`; use a RAM sub-account with SMS-only permissions | With `provider=aliyun` |

Tencent Cloud SMS: `sms.tencent.secret_id`, `secret_key`, `sdk_app_id`, `sign_name` are required credentials and support env references; `region` defaults to `ap-guangzhou`, `endpoint` to `https://sms.tencentcloudapi.com`. Captchas use `sms.template`; notifications use `sms.tencent.notify_template` (empty skips notification SMS). `sms.tencent.param_order` pins variable order per template ID, e.g. `"123456": [code]`; unset sorts by parameter name — multi-parameter templates must verify order. Actual sending requires Tencent-approved signatures, templates and SMS permissions.

### 3.1 auth.providers.<name> fields

| Field | Description |
| --- | --- |
| `client_id` / `client_secret` | App credentials; WeCom client_id = CorpID, client_secret = app Secret; DingTalk AppKey / AppSecret; Feishu App ID / App Secret. Use `${ENV}` references — never plaintext in the repo |
| `redirect_uris` | Callback whitelist, exact match, equal to the frontend callback page `/admin/login/oauth/<provider>` full URL |
| `match_by` | Basis for first-login auto-matching: `phone` / `email` / `employee_no`, empty = no auto-match. `email` requires a verified email; `employee_no` maps to the local username; unmatched accounts are told to "bind an existing account" |
| `auto_create` | Auto-create the account (passwordless) when unmatched, default `false` |
| `default_role` | Role code granted on `auto_create`, `guest` when empty; built-in roles are never auto-granted |
| `allowed_email_domains` | Only these email domains may log in (requiring verified email), unrestricted by default |
| `agent_id` | WeCom only, required |
| `issuer` / `name` / `scopes` | `oidc` only (`google` may override `issuer`): issuer must serve `/.well-known/openid-configuration`; `name` is the login-page display name |
| `auth_url` / `token_url` / `userinfo_url` / `jwks_url` | Usually unneeded; manual endpoints for `oidc` without discovery |

## 4. upload / storage / message / job / export / setup / ai / disabled_modules

| Key | Type | Default | Purpose | When to change |
| --- | --- | --- | --- | --- |
| `upload.max_size_image_mb` / `max_size_video_mb` / `max_size_file_mb` | int | `0` | Fallback upload size caps (MB). The file module's effective limit = the "System Parameters" page "Upload Limits" (`sys_config`'s `upload.max_size_*_mb`, `upload.allowed_types`) entries that are set; unset items (size 0 / empty, empty type list) fall back to here, and only when both are 0 is it unlimited. Note that setting 0 on the page cannot relax a cap set here. Page saves take effect on this instance immediately, up to 5 seconds later on multi-instance (short cache). "Allowed upload types" is page-only (no config.yaml key); factory params ship a default whitelist (images / mp4 / webm / pdf / doc(x) / xls(x) / zip) — clearing it means no type restriction | Deployment-level upload caps, or when the page isn't set yet |
| `storage.driver` | string | `local` | `local`: save to `data/uploads/files` (resumable temp files in `data/uploads/tmp`); `s3`: S3-compatible storage | When using object storage |
| `storage.s3.endpoint` | string | empty | e.g. `https://s3.amazonaws.com`, `http://minio:9000`, `https://oss-cn-hangzhou.aliyuncs.com` | Required with `driver=s3` |
| `storage.s3.bucket` / `access_key` / `secret_key` | string | empty | Bucket and credentials, required with `driver=s3`; supports `${ENV}` — never commit secrets | `driver=s3` |
| `storage.s3.region` | string | empty | Empty defaults to `us-east-1` | When the provider requires a region |
| `storage.s3.prefix` | string | empty | Optional key prefix, e.g. `nexadmin` | Sharing one bucket across systems |
| `storage.s3.path_style` | bool | `false` | `true`: `endpoint/bucket/key` (MinIO); `false`: `bucket.endpoint/key` (OSS / COS / AWS) | MinIO etc. |
| `storage.s3.insecure_skip_verify` | bool | `false` | Skip TLS verification — internal self-signed only | Internal self-signed |
| `storage.s3.presign_redirect` | bool | `false` | `false`: private files are proxied by the backend, hiding the bucket; `true`: 302 to a presigned URL | Offloading download traffic from the app server |
| `storage.s3.part_size_mb` | int | `8` | Part size, minimum 5 | Large-file tuning |
| `storage.s3.multipart_threshold_mb` | int | `16` | Above this size, multipart upload kicks in | Large-file tuning |
| `message.retention_days` | int | `90` | Read-message retention in days, cleaned daily; `<=0` disables cleanup | Adjusting retention |
| `job.workers` | int | `2` | Async task (export center etc.) worker concurrency; `<=0` uses the default | With many export jobs |
| `job.keep_days` | int | `7` | Task record and result file retention; files cleaned together at expiry | Adjusting retention |
| `job.max_per_user` | int | `3` | Per-user in-flight (queued + running) task cap | Anti-abuse tuning |
| `export.sync_limit` | int | `5000` | Matched rows up to this export synchronously; beyond, async export; `<=0` uses 5000 | Tuning sync export size |
| `log.export_max_rows` | int | `50000` | Row cap for operation / login log server exports (`GET /oper-logs/export`, `GET /login-logs/export`); exceeding returns 400 suggesting narrower filters or the export center; `<=0` uses 50000 | Large log volumes or tightening exports |
| `setup.allow_remote` | bool | `false` | Whether non-local machines may access the setup wizard. Local requires a TCP loopback peer without forwarding headers. When allowed, the console-printed `X-Setup-Token` is required; the wizard page can input the token and attach it to all setup requests — the token lives only in component memory | Usually `false`; only for remote or proxied initialization |
| `ai.base_url` / `ai.api_key` / `ai.model` | string | empty | OpenAI-compatible endpoint, key and model name. These are fallbacks: once the settings page "AI" section is saved it wins; if neither is configured the AI entries hide | Enabling AI capabilities |
| `disabled_modules` | list | `[]` | Disable modules by `Name()` (routes not assembled, absent from API docs) without deleting code; menus are data — already-written menu rows remain, hide them in "Menu Management". Names in the Features & Permissions document | When a built-in module is unneeded |
| `disabled_plugins` | list | `[]` | Disable plugins by id (e.g. `[website]`): nothing mounts at all (routes, migrations, menu seeds all skipped), the admin page shows "disabled" and `plugin list` shows `disabled`. Free plugins without license gates use this; pro plugins may too (no need to revoke licenses). To remove data as well use `nexadmin plugin remove --purge <id>` | Temporarily taking a plugin offline |

## 5. backup

| Key | Type | Default | Purpose | When to change |
| --- | --- | --- | --- | --- |
| `backup.dir` | string | `./backup` | Backup directory (dir mode 0700, files 0600) — used by the backup page, `reinit`'s backup (`nexadmin-reinit-*.sql.gz`) and `restore`'s pre-restore backups; requires local `mysqldump` (or `mariadb-dump`) / `pg_dump`. The official Dockerfile overrides with `NEXADMIN_BACKUP_DIR=/app/data/backup` (env wins over config.yaml); the systemd example's `ReadWritePaths` includes the default `/opt/nexadmin/backup` | Wherever there is space and write permission |
| `backup.keep_days` | int | `30` | Retention days, 0 disables day-based cleanup | First start only as the initial value; the page governs afterwards |
| `backup.keep_count` | int | `10` | Retention count, 0 disables count-based cleanup | Same |
| `backup.schedule` | string | empty | Daily time `HH:MM` (per `app.timezone`), empty disables scheduled backups | Enabling scheduled backups (same, then the page governs) |

## 6. ratelimit

Fixed-window rate limiting: the same key allows at most `limit` requests per `window`, exceeding returns HTTP 429 with `Retry-After`. `limit <= 0` disables that rule; `ratelimit.enabled=false` disables all. Counters live in `cache`: `redis` shares across instances, `memory` is process-local; cache errors pass through with throttled warnings (fail-open). Each rule's sub-keys are `limit` (int) and `window` (duration), e.g. `NEXADMIN_RATELIMIT_LOGIN_LIMIT`.

| Key | Default | Dimension & endpoints |
| --- | --- | --- |
| `ratelimit.enabled` | `true` | Master switch |
| `ratelimit.login` | 20 / `1m` | IP + route; `POST /auth/login`, third-party login callbacks and similar login endpoints |
| `ratelimit.captcha` | 30 / `1m` | IP + route; `GET /auth/captcha` |
| `ratelimit.sms_code` | 5 / `1m` | IP + route; `POST /auth/sms-code` |
| `ratelimit.token` | 10 / `1m` | SSO `/oauth/token` failures (per IP or IP + client), 429 on exceed |
| `ratelimit.setup` | 30 / `1m` | IP; setup wizard `/api/v1/setup/*` (in-process memory counting while uninitialized) |
| `ratelimit.upload` | 120 / `1m` | Logged-in users; file uploads (tus create & append) |
| `ratelimit.ai` | 30 / `1m` | Logged-in users; `/ai/*`. The AI gateway also defaults to 20/min per user internally |
| `ratelimit.pat_create` | 10 / `1h` | Logged-in users; creating personal access tokens |
| `ratelimit.openapi_sign` | 30 / `1m` | IP; open-platform token / app-signature validation failures |
| `ratelimit.client_error` | 30 / `1m` | IP; `POST /api/v1/client-errors` frontend error reports |
| `ratelimit.dashboard_layout` | 30 / `1m` | Logged-in users; `/api/v1/dashboard/layout` reads & writes |
| `ratelimit.log_export` | 10 / `1m` | Logged-in users; operation & login log exports (both endpoints share one counter) |

## 7. metrics

| Key | Type | Default | Purpose | When to change |
| --- | --- | --- | --- | --- |
| `metrics.enabled` | bool | `true` | Mounts `/metrics` at the root (Prometheus text format, outside `/api/v1`, no login); `false` skips registration | Disable when metrics are unneeded |
| `metrics.token` | string | empty | Empty: loopback-only access, and any request with `X-Forwarded-For` / `X-Real-IP` is refused (403); non-empty: every source must send `Authorization: Bearer <token>` (constant-time compare, missing or wrong returns 401) | Required for remote Prometheus scraping or exposure via a reverse proxy |

## 8. Keys read by code but absent from default.yaml

The following keys are read by their owning modules directly from `config/default.yaml`, `config/config.yaml` and environment variables (same `NEXADMIN_` rules), bypassing `core/config` validation:

| Key | Read by | Purpose |
| --- | --- | --- |
| `sso.issuer` | `modules/sso` | OIDC issuer. Default `<site>/api/v1/oauth`; configure explicitly in production. Env `NEXADMIN_SSO_ISSUER`. Its host also participates in plugin-license domain checks |
| `site.domain` / `site.domains` | `core/plugin` | Site domains, used only for plugin-license `domains` comparison. With none configured, the first request carrying a Host is used |
| `license.allow_dev_hosts` | `core/plugin` | When `true`, license domain checks treat `localhost`, `127.0.0.1`, `::1` and `*.localhost` as matches (local development) |
| `identity.allow_private_hosts` | `plugins/identity` | Default `false`: LDAP addresses and CAS `serverUrl` refuse loopback / private / link-local / CGNAT / cloud-metadata addresses (literal check on save, all A/AAAA resolved before connect and actual IPs re-checked at connect). Internal directory and CAS deployments are naturally on private networks — set explicitly to `true` to connect. Env `NEXADMIN_IDENTITY_ALLOW_PRIVATE_HOSTS`; hot-mounting grants but never withdraws it |

## 9. Environment-variable-only items

| Variable | Purpose |
| --- | --- |
| `NEXADMIN_SECRET_KEY` | Static-encryption master key (AES-256-GCM, 32 bytes, hex or base64). Takes precedence over `data/secret.key`; unset, the first start generates `data/secret.key` (hex, mode 0600). Losing the master key makes encrypted values unrecoverable — back it up with the database |
| `NEXADMIN_MIGRATIONS_DIR` | Points to the `migrations/framework` directory; business migrations sit beside it in `business`. Unset, it searches the current directory and ancestors, then the executable's directory and ancestors for `migrations/framework` |
| `NEXADMIN_SHUTDOWN_TOKEN` | Internal: `start` uses it to pass the shutdown token to the background process, which clears it after reading. Do not set manually |

## 10. Settings page vs configuration file

System Settings (full admin URL `/admin/system/settings`) has ten UI sections: `site`, `security`, `cache`, `storage`, `ai`, `sms`, `auth`, `notify`, `language` and `params`. The first eight use the settings API / `sys_setting`; languages use `/languages` / `sys_language`, and parameters use `/configs` / `sys_config`. Do not pass the latter two to `/settings/:section`. Reserve this platform-global configuration for platform super admins; see [Features & Permissions](features.md) for navigation groups and API boundaries. Runtime precedence and limitations:

- `ai`: the settings section wins; never saved, falls back to `ai.*`.
- Login security (consecutive failures, lock minutes): settings `security` section → system params `login.max_retry` / `login.lock_minutes` → default 5 failures / 15 minutes.
- `storage`: file storage follows `config/config.yaml`. Saving the settings page's `storage` section is rejected with 400 (unit test `TestSaveStorageSectionRejected` comments: "rather than saving successfully but having no effect").
- The remaining sections (`cache`, `sms`, `auth`, `notify`, `site`) versus their YAML counterparts at runtime: not verified item by item. For deployment decisions, trust the YAML.
