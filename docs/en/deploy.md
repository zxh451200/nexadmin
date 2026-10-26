# NexAdmin Deployment & Operations Guide

For deployment and operations staff. Content comes from `scripts/`, `deploy/`, `core/ctl`, `core/setup`, `core/migrate` and `cmd/server` code and scripts. The 2026-10-23 pass only statically read code and corrected the documentation — no application commands, tests, builds, SQL or real acceptance were run; statements like "verified on this machine" below are historical records and do not mean the current code has been re-verified. A passing build does not mean real database, Redis multi-instance, browser or identity-source flows were accepted — steps involving those still need separate acceptance.

Convention: unless stated otherwise, commands run from the project root (the directory containing `config/` and `migrations/`). The program locates `config/default.yaml`, `config/config.yaml`, `data/`, `logs/` and `migrations/` relative to the current working directory.

## 1. Runtime shapes

- Single binary: `scripts/build.sh` uses `-tags embedweb` to embed the frontend build `web/dist` into `nexadmin-server`; one port serves `/api/v1`, `/healthz`, `/metrics` and the frontend pages (frontend routes fall back to `index.html`; hashed `/assets/` files are cached immutable; `sw.js`, `manifest.webmanifest` and `offline.html` are not cached).
- Building without `embedweb` (`go run ./cmd/server`, `go build ./cmd/server`): no frontend hosting; unmatched paths return a JSON 404. This is dev mode, with vite running separately.
- Dev mode: `go run ./cmd/server` starts the backend (`app.env=dev` logs to the console, CORS allows any origin); `cd web && npm run dev` starts the frontend on port 5173 with `/api` proxied to `http://127.0.0.1:8080`. `web/.env.development` sets `VITE_MOCK=true`; frontend code loads mock data only when `VITE_MOCK === 'true'` (determined centrally in `web/src/framework/env.ts`); an unset variable means off, so dev mode relies on `.env.development` explicitly setting `true` and production builds rely on `.env.production`'s `false` (a missing entry never turns mocks on). To hit the real backend, override per Vite convention with an environment variable: `VITE_MOCK=false npm run dev` (untested).
- Files needed at runtime: `nexadmin-server`, `config/default.yaml`, `migrations/` (`framework/` and `business/`). `config/config.yaml`, `data/` and `logs/` are created by the wizard or at runtime. Verified: an empty directory containing only `config/default.yaml` can start the uninitialized service; after initialization, a missing `migrations/framework` errors out (see §13).
- Foreground: `./nexadmin-server` (Ctrl+C or SIGTERM for graceful exit, waiting up to 10 seconds). Background: `start | stop | restart | status` (§4). For production, prefer systemd or container orchestration.

## 2. Build & packaging

### 2.1 scripts/build.sh (Linux / macOS)

```
sh scripts/build.sh
```

Script content (checked line by line): reads `framework_version` from `.nexadmin.manifest.json` (exits if missing) → `cd web && npm ci && npm run build` → `go build -trimpath -tags embedweb -ldflags "-s -w -X main.version=<version>" -o bin/nexadmin-server ./cmd/server`. No make required.

Prerequisites: Go (`go.mod` declares `go 1.25.0`), Node and npm (the Dockerfile uses `node:22-alpine`).

Verification: `npm run build` (`tsc --noEmit && vite build`) and the same-flag `go build -tags embedweb` ran locally; the binary's `version` prints `0.1.0`, and after startup `/` returned `text/html`. `npm ci` needs network and was not run locally.

### 2.2 scripts/build.bat (Windows)

The same three steps, with the version read from the manifest via PowerShell; artifact `bin\nexadmin-server.exe`. Not run on Windows.

### 2.3 deploy/Dockerfile

Build context is the project root:

```
docker build -f deploy/Dockerfile -t nexadmin:latest .
```

Multi-stage: `node:22-alpine` builds the frontend → `golang:1.25-alpine` builds with `CGO_ENABLED=0 go build -tags embedweb` → `alpine:3.20` runs. Runtime image points (checked line by line):

- Non-root user `nexadmin` (uid/gid 10001); working directory `/app`.
- Binary `/app/nexadmin-server`; `/app/migrations` ships with the image; default config at `/app/defaults/default.yaml`.
- Entrypoint `/app/entrypoint.sh`: on every start it first copies `/app/defaults/default.yaml` over `/app/config/default.yaml` (upgrading the image upgrades the defaults), then `exec /app/nexadmin-server "$@"`, so `docker run <image> init --config ...` accepts subcommands directly.
- The runtime image (alpine 3.20) installs, besides `ca-certificates` and `tzdata`, `mariadb-client` (providing `mysqldump` / `mysql`, common to MySQL and MariaDB) and `postgresql16-client` (providing `pg_dump` / `psql`, matching compose's `postgres:16`; Alpine 3.20 has no unversioned `postgresql-client` package name). Page backups, `reinit`'s backup and `restore` depend on them (§7).
- Volumes: `/app/config`, `/app/data`, `/app/logs`; exposes 8080; `HEALTHCHECK` uses `wget` against `http://127.0.0.1:8080/healthz` (unchanged).
- Backup directory: the image creates and grants `/app/data/backup` (writable by `nexadmin`, inside the data volume) and sets `ENV NEXADMIN_BACKUP_DIR=/app/data/backup`. Note the environment variable takes precedence over `config.yaml` — to change the directory, change this variable (or override in compose), not just `config.yaml`'s `backup.dir`. The backup page, `reinit` and `restore` all use the same directory.

Verification: package names come from the Alpine 3.20 package index (`mariadb-client` contains `/usr/bin/mysqldump`, `/usr/bin/mysql`; `postgresql16-client` exists); the image itself was not built or verified live.

### 2.4 deploy/docker-compose.example.yml

```
# Write .env in the project root: POSTGRES_PASSWORD=... (required)
docker compose -f deploy/docker-compose.example.yml up -d --build
docker compose -f deploy/docker-compose.example.yml --profile cache up -d   # also start valkey
# First deployment uses non-interactive init (the container does not expose the remote wizard)
docker compose -f deploy/docker-compose.example.yml run --rm app init --config /app/config/init.yaml
```

File content (checked line by line): services `app` (`context: ..`, `dockerfile: deploy/Dockerfile`, port `8080:8080`), `postgres` (`postgres:16` with a `pg_isready` health check, `app` depends on it being healthy), `valkey` (`valkey/valkey:8`, only started with `--profile cache`, password `VALKEY_PASSWORD`, default `change-me`). The `app` environment: `NEXADMIN_APP_ENV=production`, `NEXADMIN_DATABASE_TYPE=postgres`, `..._HOST=postgres`, `..._PORT=5432`, `..._USER=nexadmin`, `..._PASSWORD=${POSTGRES_PASSWORD}`, `..._NAME=nexadmin`, plus `NEXADMIN_BACKUP_DIR=/app/data/backup` (already the image default, spelled out for visibility); the file also has a commented-out `NEXADMIN_SERVER_TRUSTED_PROXIES` example which must be configured when a reverse proxy sits in front (§11.1). When using valkey, add `NEXADMIN_CACHE_DRIVER=redis`, `NEXADMIN_CACHE_ADDR=valkey:6379` and `NEXADMIN_CACHE_PASSWORD` yourself (the file's comments only mention the first two; the password entry comes from the config key `cache.password` and valkey's `--requirepass`). Four named volumes `nexadmin_config`, `nexadmin_data`, `nexadmin_backup`, `nexadmin_logs` map to `/app/config`, `/app/data`, `/app/data/backup` (backups get their own volume, mounted as a subdirectory inside the data volume for easy copying off), `/app/logs`.

`init --config /app/config/init.yaml` requires that file to already exist in the `nexadmin_config` volume (format in §3.2). The sample `postgres` / `valkey` passwords and image versions must be replaced and pinned in production.

Verification: the local Docker has neither the `compose` subcommand nor a daemon — `docker compose config` and the image build could not run; only manual review was done.

### 2.5 deploy/nexadmin.service (systemd)

Install steps (from the file header comments):

```
useradd -r -s /usr/sbin/nologin nexadmin
mkdir -p /opt/nexadmin/{bin,config,data,logs,backup}      # place bin/nexadmin-server, config/default.yaml, migrations/
chown -R nexadmin:nexadmin /opt/nexadmin/{config,data,logs,backup}
cp deploy/nexadmin.service /etc/systemd/system/ && systemctl daemon-reload && systemctl enable --now nexadmin
```

Environment file `/etc/nexadmin/nexadmin.env` (mode 0600, owned by root), e.g. `NEXADMIN_APP_ENV=production`, `NEXADMIN_DATABASE_PASSWORD=...`. Unit essentials: `User/Group=nexadmin`, `WorkingDirectory=/opt/nexadmin`, `ExecStart=/opt/nexadmin/bin/nexadmin-server`, `Restart=on-failure`, `RestartSec=5`, `LimitNOFILE=65536`; hardening: `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `PrivateDevices`, `ProtectKernel*`, `ProtectControlGroups`, `RestrictSUIDSGID`, `RestrictRealtime`, `LockPersonality`; only `config`, `data`, `logs` and `backup` are writable (`ReadWritePaths`, hardening options all kept).

Note (not verified live): the `backup` directory must exist beforehand, otherwise that `ReadWritePaths` entry fails unit startup (systemd namespace error) — hence the `mkdir` above includes it. The default `backup.dir=./backup` resolves under `WorkingDirectory` to `/opt/nexadmin/backup`, exactly this writable directory; changing `backup.dir` requires a matching `ReadWritePaths` change. Database clients must be installed on the host yourself (e.g. `apt install mariadb-client postgresql-client`) — the unit does not. `migrations/` must sit at `/opt/nexadmin/migrations` (under the working directory).

Verification: the local `systemd-analyze verify` was too old to recognize `ProtectKernelTunables` etc., so verification was incomplete; the unit has never started under systemd.

## 3. Initialization

### 3.1 Wizard (browser)

While uninitialized, the service starts normally but only exposes the wizard and status endpoints:

- Business APIs uniformly return HTTP 503, `{"code":503,"message":"System not initialized","data":null,"reason":"NOT_INITIALIZED"}` (verified locally). `/healthz`, `/metrics`, `GET /api/v1/system/status` (returning `{"initialized":false}`), `GET /api/v1/i18n/languages` and `/api/v1/setup/*` are available.
- The console (for background starts, `logs/nexadmin.out`) prints the wizard URL and a one-time initialization token. `start`'s output carries it too.
- Only the local machine may access the wizard by default. "Local" requires the TCP peer to be a loopback address with no `X-Forwarded-For`, `X-Real-IP`, `Forwarded` or `X-Forwarded-Host` forwarding headers; requests forwarded by same-machine nginx count as proxied. Remote or proxied initialization requires `setup.allow_remote: true` plus the one-time console token entered on the page, or `X-Setup-Token` from an API client. Direct local access without forwarding headers needs no token.
- The wizard has 6 steps: environment check → database → cache → admin & site → optional config → confirm & execute. Database and cache support connection tests; S3 and AI provide inputs without dedicated probes.
- Executing `POST /api/v1/setup/execute` runs in order: `writeConfig` (writes `config/config.yaml`, mode 0600) → `migrate` → `seed` → `createAdmin` → `writeMark` (writes `data/.initialized`). Any failure rolls back the written `config.yaml` and parameters can be retried; only one initialization runs at a time. The page receives real per-stage progress via POST SSE, while plain API clients keep JSON compatibility. Reverse proxies must pass SSE through immediately.
- The remote-init token lives only in component memory and is cleared on success — never in browser storage.
- Password recovery depends on the account's bound email / phone being **unique** among enabled accounts: when one email (or phone) matches multiple enabled accounts, the anti-enumeration policy treats them all as "not found" and none can use code-based recovery — an admin must reset the password instead. Note that entry points (admin user creation, Excel import, profile binding) currently only validate format without strong uniqueness enforcement — uniqueness must be guaranteed administratively.
- Admin validation: username `^[A-Za-z][A-Za-z0-9_]{2,31}$`; the password has **no strength rules** — non-empty and at most 72 bytes (bcrypt reads only the first 72 bytes, so longer input is rejected; backend `passwordAcceptable`); site name non-empty and ≤60 characters; timezone must be a valid IANA name; language must be in `languages`. The site name is written to the settings page "Site" section (`sys_setting`'s `site.siteName`, written in the `seed` step, re-runnable) — the login page, top bar and PWA manifest display it; it is also written to `config.yaml`'s `app.name`, but that entry is only for logs / default naming and the pages don't read it.
- During initialization each module's `Seed` writes, besides factory data like menus, dictionaries, params and roles, demo data: demo users (created without a password field — by code reading they cannot log in with a password, untested), departments, posts, notices, messages, logs, demo tenants and packages, factory roles (`operator`, `auditor`, `editor`, `guest`, `secadmin`). These rows are "insert if absent, never touch if present", and `Seed` runs only during initialization, not on every start (each start only tops up `StartupSeeder` items like menus). Clean up demo data as needed before go-live (the exact list is in each module's `Seed`, not verified table by table).
- After initialization the wizard routes are not registered at all in an initialized process — `/api/v1/setup/*` becomes unavailable; the service switches to full routes in place without restart.
- The "is initialized" check (`setup.IsInitialized`): `data/.initialized` exists → initialized; `config/config.yaml` missing → uninitialized; otherwise connect to the database and look for an admin — connection or read failures count as initialized (only a reachable database with zero tables counts as uninitialized, which is the case right after `reinit`).

### 3.2 Non-interactive initialization (containers / automation)

```
./nexadmin-server init --config init.yaml
```

The file is JSON or YAML with the same body as the wizard's `POST /api/v1/setup/execute`; field names come from `core/setup/request.go`:

```yaml
db:
  type: postgresql        # mysql | mariadb | postgresql
  host: 127.0.0.1
  port: 5432
  username: nexadmin
  password: "<database password>"
  database: nexadmin      # letters, digits, underscore only, max 64
  autoCreate: false       # true: create the database if absent
cache:
  type: memory            # memory | valkey (redis accepted)
  # addr: valkey:6379     # required for valkey / redis
  # password: ""
admin:
  username: admin
  password: "<non-empty, max 72 bytes; no strength rules>"
  siteName: NexAdmin
  language: zh-CN
  timezone: Asia/Shanghai
optional:                 # the whole section may be omitted
  storage: local          # local | s3
  # s3Endpoint / s3Bucket / s3AccessKey / s3SecretKey / s3Region / s3Prefix / s3PathStyle
  # aiEnabled / aiBaseUrl / aiApiKey / aiModel
```

Behavior (validation and failure paths tested locally without a database):

- Prints `writeConfig` / `migrate` / `seed` / `createAdmin` / `writeMark` status step by step, never printing passwords; any failure exits non-zero with `init failed at <step>: <reason>` and rolls back the written `config/config.yaml`.
- Empty or over-72-byte admin password: `init failed at writeConfig: admin password must be 1-72 bytes` (no strength rules; fragments equal to the password are masked as `***` in error text).
- Unreachable database: `init failed at migrate: database: connect postgres 127.0.0.1:5432: ...`.
- Already initialized (or cannot confirm uninitialized): refused with `System already initialized (or cannot confirm), refusing duplicate initialization; use reinit for factory reset`.
- Missing `--config`: `init requires --config <file>`.
- The full successful flow (real database table creation, seeding, admin creation) is not verified live.

## 4. One-click start/stop

### 4.1 Scripts

`deploy/` contains `ctl.sh` (Linux), `ctl.command` (macOS), `ctl.bat` (Windows) and matching `reinit.*`. They are thin shells: they open a terminal if none exists (Linux tries `x-terminal-emulator`, `gnome-terminal`, `xterm` in order), find `nexadmin-server` in the script directory or its parent, fall back to `go run ./cmd/server` when `go.mod` and `go` are present, and pause after running.

- The `ctl.*` menu has four items: `1 start  2 stop  3 restart  4 status`, mapping to `start` / `stop` / `restart` / `status`.
- `reinit.*` runs `nexadmin-server reinit` directly and accepts extra arguments, e.g. `deploy/reinit.sh --dry-run`.
- `init`, `migrate`, `restore` and `version` have no script entries — use the binary directly.

### 4.2 nexadmin-server subcommands

Usage below is checked against `cmd/server/main.go`'s parsing and calls (usage itself not run this round):

| Command | Purpose |
| --- | --- |
| (none) | Run in the foreground |
| `version` (or `--version`) | Print the version; `dev` without ldflags, `build.sh` builds the manifest's `framework_version` (currently `0.1.0`) |
| `start` | Start in background: logs append to `logs/nexadmin.out` (mode 0600), writes `data/run/nexadmin.pid` (JSON: pid, port, shutdown token, start time, mode 0600), waits up to 20s for readiness. Already running → `already running (port N)`. When uninitialized, prints the wizard URL and init token |
| `stop` | Graceful exit via `POST /api/v1/system/shutdown` on loopback (header `X-Shutdown-Token`) — no signals, identical on Windows and Linux; waits up to 20s. If no pid file but the service runs: `service is running but was not started by start (no pid file); stop it with Ctrl+C in its terminal` |
| `restart` | `stop` then `start` |
| `status` | Prints state / pid / listen address / initialized; when stopped prints `state: stopped` |
| `init --config <file>` | Non-interactive initialization (§3.2) |
| `migrate [--tenant platform-or-tenantID] [--status] [--dry-run] [--mark-applied <scope>/<file>] [--unmark <scope>/<file>] [--yes]` | Migrations (§5); omitting tenant defaults to platform plus all configured tenants |
| `backup --output <new dir>` | Full SQL bundle of the platform and all configured tenants (§7.3) |
| `restore-bundle --dir <dir> [--tenant <ID>] (--dry-run or --yes)` | Validated sequential restore; service must be stopped; exactly one of the two execution flags (§7.3) |
| `restore <backup file> [--yes] [--dry-run] [--sha256 <value>] [--platform-only]` | Single platform-database restore; isolation mode requires explicit platform-only (§7) |
| `reinit [--dry-run] [--platform-only]` | Factory reset; isolation mode requires explicit platform-only, shared resources still cleaned (§8) |

The full `start` → `status` → `restart` → `stop` → `status` cycle was tested locally without a database, plus `migrate --dry-run` (uninitialized prints `system not initialized, nothing to migrate (the wizard / init creates tables)`), `reinit --dry-run` (without `config/config.yaml` prints `no config/config.yaml — never initialized, no factory reset needed`), and `restore --dry-run` (unreachable database prints `cannot reach the database, aborted`).

## 5. Database migrations

### 5.1 Rules

The migration runner (`core/migrate`) executes three classes under one rule set; applied files are recorded in the version table `sys_migration`:

| scope | Directory | Notes |
| --- | --- | --- |
| `framework` | `migrations/framework/<engine>/` | Framework migrations, overwritten on upgrade; must exist |
| `business` | `migrations/business/<engine>/` | Business migrations; a missing directory means none |
| `plugin:<id>` | The plugin's own migration directory | Only licensed and mounted plugins |

- `<engine>` is `mysql`, `mariadb` or `postgres`; MariaDB falls back to the `mysql` directory when it has no dedicated one. The framework directory currently has 24 files per engine; the business directory gets `mysql`, `mariadb`, `postgres` from `nexadmin add module`.
- Within a scope, files run in name order, skipping files already recorded; scope order is framework → business → plugins.
- SQL is split on semicolons; `--` comment lines are ignored — do not put semicolons inside string literals in migration files.
- PostgreSQL wraps each file in a transaction with full rollback on failure; MySQL / MariaDB DDL cannot roll back — statements run one by one, and failure reports `executed N/M statements, statement N+1 failed, file not recorded, repair manually first`.
- Multiple instances starting simultaneously rely on database-level locks (MySQL / MariaDB `GET_LOCK`, PostgreSQL `pg_try_advisory_lock`); after acquiring the lock the version table is re-read and work completed by other instances is skipped; the lock wait caps at 2 minutes with `migrate: migration lock wait timed out (2m0s), another instance may still be migrating`.
- `INSERT` / `UPDATE` / `DELETE` inside migrations only WARN and do not block (factory data belongs to module seeds at startup, not migrations).

### 5.2 auto_migrate and the migrate command

- `app.auto_migrate=true` (default): every startup executes pending migrations while holding the lock, then idempotently tops up factory data (menus etc.).
- `app.auto_migrate=false` (production separating upgrades from startup): startup only checks, refusing to start with pending migrations: `N pending migrations (framework/xxx.sql, ...), app.auto_migrate=false, run nexadmin-server migrate first`. After an upgrade, run `nexadmin-server migrate` before starting.

The above describes platform migrations. With `database.isolation.enabled`, tenant-database auto-migration for an initialized service requires `app.auto_migrate && database.isolation.auto_migrate` (the latter default `false`); with either off, tenant pending migrations are still checked and pending items refuse startup. The wizard / `init` explicitly runs platform migrations; tenant parts only run or are checked per isolation.auto_migrate; manual `migrate` is not restricted by the automatic switches. The platform runs framework and platform-target migrations; tenants run business and tenant-target migrations — additional migration sources without an explicit target are refused; each database records and locks separately — not an atomic cross-database migration — and existing data is never moved.

`migrate --tenant platform` processes only the platform; `migrate --tenant <canonical tenant ID>` only that configured tenant; combinable with `--status` and `--dry-run`; omitted, it processes the platform then all configured tenants. With separate databases, `--mark-applied` / `--unmark` must specify `--tenant` to avoid mis-marking several databases. `migrate --dry-run` still needs a database connection and migration state reads — it is not an offline check, unlike `restore-bundle --dry-run` below which runs no database commands.

| Command | Purpose |
| --- | --- |
| `nexadmin-server migrate` | Run all pending; if none, prints `migrations are up to date` |
| `nexadmin-server migrate --status` | Lists per scope `applied` / `manually marked` / `pending` / `CHECKSUM mismatch` (each line with the first 12 checksum chars), summarizing `applied N, pending N, checksum mismatches N`; non-zero exit on mismatches |
| `nexadmin-server migrate --dry-run` | Lists files and statement counts only, touching nothing: `N pending migrations in total (dry-run, database untouched)` |
| `nexadmin-server migrate --mark-applied <scope>/<file> [--yes]` | After a mid-migration failure (MySQL DDL cannot roll back) that you repaired manually, mark the file applied without running any of its SQL; interactive confirm, default N, `--yes` skips. `<scope>` is `framework`, `business` or `plugin:<id>` |
| `nexadmin-server migrate --unmark <scope>/<file> [--yes]` | Revoke a manual mark; the file becomes pending again; only manual marks can be revoked — real executions refuse |

`--mark-applied` refuses: files missing on disk, records already in the version table (`already recorded, refusing duplicate mark`), and records with checksum mismatches. Marking logs the operator (system user@host) and time. `--mark-applied` and `--unmark` cannot be combined. When uninitialized, all `migrate` variants print "nothing to migrate".

Migration commands (`migrate`, `--status`, etc.) need a real database; locally only the uninitialized outputs were tested, the rest is code-checked and not verified live.

### 5.3 Checksum rules

- Each migration file's fingerprint is the sha256 of its content (with `\r\n` normalized to `\n` to avoid git autocrlf false positives), written to the version table on first execution.
- If an executed file's content changes later (violating "append-only"), both startup and `migrate` refuse: `migration file modified after execution (violates append-only), refusing to start: framework/xxx.sql, .... Restore these files and put the needed changes into a new migration file`. Fix: restore the file; add the change as a new numbered SQL.
- Never "fix" a checksum by editing the version table; when you truly need to skip a file, use `--mark-applied`.

### 5.4 Baseline for legacy databases

When a scope has no records at all in the version table (e.g. a database whose tables predate the framework), unrecorded files are treated as a "baseline": for `CREATE TABLE` statements whose target table already exists, that table and following index statements are skipped; if every statement in a file is skipped it is recorded without executing, logged as `migration recorded (table exists, baseline)`, shown by `--dry-run` as `will record <scope>/<file> (table exists, not executed)`. Statements before the first `CREATE TABLE` cannot be judged and run normally. After baselining, the scope returns to strict mode. Plugin records from the old `sys_plugin_migration` table are imported directly.

## 6. Data that needs separate backups

Database backup (§7) covers only the database. The following are not included and must be backed up yourself, kept in correspondence with database backups:

| Path | Content | Notes |
| --- | --- | --- |
| `data/secret.key` (or env `NEXADMIN_SECRET_KEY`) | Static-encryption master key | Lost, encrypted values in the database (third-party keys, SMTP passwords etc.) cannot be recovered |
| `data/jwt.secret` (or `auth.jwt_secret`) | JWT signing key | Lost, it regenerates and all users must log in again |
| `data/sso-keys/` | SSO (OIDC Provider) signing key directory | The published JWKS comes from here; behavior when lost: unverified |
| `data/uploads/` | Locally stored uploads | With `storage.driver=s3`, files live in object storage, not here |
| `data/licenses/` | Plugin license files | |
| `config/config.yaml` | Site configuration (contains the database password) | Mode should be 0600 |

## 7. Backup & restore

### 7.1 Backup (page)

"System → Data Backup" (module `backup`, permissions `backup:list` / `backup:edit` / `backup:download`, plus super admin). Immediate backups are asynchronous — the page polls the list; scheduling is also available.

- Depends on local CLI tools: MySQL / MariaDB use `mysqldump` (or `mariadb-dump`), PostgreSQL uses `pg_dump`. Missing tools report `mysqldump / mariadb-dump not found on this machine; install the database client and add it to PATH` (`pg_dump` for pg); scheduled sources leave a failure record in the list.
- File name `nexadmin-<engine>-v<framework version>-<yyyymmdd-hhmmss>.sql.gz` with a `.sha256` sidecar; directory mode 0700, files 0600; written to `.tmp` first then renamed — failures leave no partial file. The database password reaches the subprocess via `MYSQL_PWD` / `PGPASSWORD`, never in the command line, logs or error text.
- Directory from `backup.dir`; retention `backup.keep_days` (30), `backup.keep_count` (10) and schedule `backup.schedule` (`HH:MM`, per `app.timezone`) are initial values on first start, then governed by page settings (stored in `sys_backup_setting`, shared across instances).
- Multi-instance: only one backup runs at a time (an unexpired `running` record counts as busy); scheduling uses a "today already claimed by an instance" marker in the database — when several instances hit the schedule, only one wins.

### 7.2 Restore (command line only)

Restore has no web entry:

The single-file examples below apply to non-isolation mode; in isolation mode `restore` (including `--dry-run`) and `reinit` must explicitly add `--platform-only`, touching only the platform database. For full configured-database sets use the bundle commands in §7.3.

```
nexadmin-server stop
nexadmin-server restore backup/nexadmin-mysql-v0.1.0-20261020-120000.sql.gz --dry-run   # review first
nexadmin-server restore backup/nexadmin-mysql-v0.1.0-20261020-120000.sql.gz
nexadmin-server start
```

Execution order and safeguards (`core/ctl/restore.go`):

1. Only `.sql.gz` files produced by this system are accepted; a running service is refused (except `--dry-run`): `service is running, restore refused; run stop first (restore overwrites live data)`.
2. sha256 validation: the expected value comes from the `<backup>.sha256` sidecar or `--sha256 <value>`; with neither, the restore refuses — no "blind restore"; failures report without touching data.
3. Requires local import tools (`mysql` / `mariadb` or `psql`) and dump tools (`mysqldump` / `pg_dump`, for the pre-restore backup); either missing aborts.
4. Prints the manifest: databases to be overwritten (engine, host, port, name, current table count, rows, size); tables present in the backup are dropped and recreated, tables absent from the backup are kept.
5. Interactive `confirm restore? [y/N]` — only `y` continues; with `app.env=production` the database name must also be typed; `--yes` skips (automation).
6. A "pre-restore backup" runs first (`nexadmin-pre-restore-<engine>-v<version>-<time>.sql.gz` into `backup.dir`), aborting on failure; then the import. Import failure reports `the database may be partially restored; pre-restore backup at <path>, use restore to roll back`.

The restore flow depends on a real database and client tools and was not verified live (this machine lacks them).

### 7.3 Platform & tenant bundle (statically reviewed only, never executed)

`backup --output <new dir>` exports the platform and all tenant databases per configuration; the parent directory must exist, the output directory must not, and paths cannot contain `..` or symlinks. Output contains each database's `.sql.gz`, `.sha256` and `manifest.json` (targets, sizes, sha256; no connection credentials or password env names). Backups are sequential independent dumps — **not an atomic cross-database snapshot**; for a consistent point in time, operations should pause relevant writes. Failures keep a partial directory which must not be treated as a fully recoverable bundle.

Command shapes below are for documentation only and were not executed this round (`123` should be a configured canonical tenant ID):

```text
nexadmin-server backup --output /absolute/existing-parent/new-bundle
nexadmin-server stop
nexadmin-server restore-bundle --dir /absolute/existing-parent/new-bundle --dry-run
nexadmin-server restore-bundle --dir /absolute/existing-parent/new-bundle --tenant 123 --dry-run
nexadmin-server restore-bundle --dir /absolute/existing-parent/new-bundle --tenant 123 --yes
```

Restore requires exactly one of `--dry-run` / `--yes` — there is no implicit interactive mode. Default restores everything; `--tenant <ID>` imports only that tenant (`platform` not accepted) but still validates the complete bundle and all configured targets, and still needs the password environment variables and dump / import clients for every database. Member paths, sizes, sha256, gzip, engine and configured targets are verified — the bundle is not a tool that auto-remaps to different targets.

Even `--dry-run` requires the service stopped and no PID file; it validates local files and tools, connects to no database, runs no database commands and creates no files. Before an actual `--yes` import, `backup.dir` must exist and be symlink-free; a `pre-restore-bundle-*` pre-restore bundle is generated and verified for **all configured databases** (even for single-tenant restore) — failure prevents the import. Then inputs are re-verified and selected databases imported sequentially; SQL objects absent from the dumps are not deleted automatically. Failure may leave a partial restore with no auto-rollback — completed targets and the pre-restore backup directory are reported; the service stays stopped for human recovery, acceptance, then a decision to start. SQL bundles exclude secrets, configuration, licenses and uploads — back those up separately per §6.

## 8. Factory reset (reinit)

Isolation mode must use `reinit --platform-only` (preview needs it too). It resets only the platform database and **still cleans shared cache, uploaded files and the initialization state**; the current flow keeps `config/config.yaml`, keys and similar files — cleanup details below. It is not a "one tenant only" operation and does not reset tenant business databases.

```
nexadmin-server reinit --dry-run     # or deploy/reinit.sh --dry-run
nexadmin-server reinit
```

Requires `config/config.yaml` to exist, otherwise: `no config/config.yaml — never initialized, no factory reset needed`. Flow (`core/ctl/reinit*.go`):

- First a read-only manifest: whether the service is running (only `start`-started ones auto-stop), the database's actual engine / host / name / table count / estimated rows / size, the count of cache keys prefixed `nex:*` (memory cache is skipped), and `data/uploads` file count and size; any unreadable item aborts.
- By default a backup is taken with local `mysqldump` / `pg_dump`, identical in format to the backup page: into `backup.dir` (the same directory as the backup page and `restore`, no longer hard-coded `./backup`), named `nexadmin-reinit-<engine>-v<framework version>-<yyyymmdd-hhmmss>.sql.gz`, gzip-compressed with a `.sha256` sidecar, directory 0700, files 0600. So it can be fed directly to `restore` (whether `restore` recognizes it depends on the `.sql.gz` suffix, `SafeBackupName` naming and sha256 — unit-tested with a fake `mysqldump`); missing tools note "not backed up" in the manifest and you decide whether to continue. `--dry-run` lists only, without backing up or executing.
- Confirmation defaults to N; only `y` executes; `app.env=production` also requires the database name.
- Execution: stop the service → drop all tables → clean cache `nex:*` → delete `data/uploads` → delete `data/.initialized` → auto-start, hinting that the wizard's database settings reuse the old values. Completed steps are not rolled back on later failure — fix and re-run.
- Untouched: `config/config.yaml`, `internal/`, `migrations/business/`, logs, files in remote object storage (manual cleanup), `data/secret.key`, `data/jwt.secret`.

The full reinit flow depends on a real database; locally only `--dry-run`'s refusal without `config/config.yaml` was tested — the rest is unverified. Backup naming, gzip + sha256 format and `restore` recognition have unit tests (with a fake `mysqldump` on PATH); real `mysqldump` / `pg_dump` export and import are unverified.

## 9. Monitoring

### 9.1 /healthz

`GET /healthz` (root path, no login, for load balancers and container probes). Initialized: one `Ping` to the database — success returns HTTP 200 `{"code":0,"message":"ok","data":{"status":"ok"}}`, failure returns 503 (unified error body). Uninitialized: only reports process liveness, always 200. It only probes the database — not cache, storage or migrations; those are in the health details below.

### 9.2 /metrics

`GET /metrics` (root path, Prometheus text format, `Cache-Control: no-store`), controlled by `metrics.enabled` (default `true`) and `metrics.token`:

- Empty `token`: only loopback addresses may access, and any request with `X-Forwarded-For` or `X-Real-IP` is refused (403). So a local Prometheus can scrape directly; requests forwarded by a same-machine reverse proxy are refused.
- Non-empty `token`: every source must carry `Authorization: Bearer <token>`, compared constant-time; missing or wrong returns 401 with `WWW-Authenticate`.
- `enabled=false`: the route is not registered.

Metrics (names from code; the first 11 were observed locally while uninitialized): `nexadmin_http_requests_total{method,route,status}`, `nexadmin_http_request_duration_seconds` (histogram), `nexadmin_http_requests_in_flight`, `nexadmin_cache_requests_total`, `nexadmin_cache_errors_total`, `go_goroutines`, `go_memstats_alloc_bytes`, `go_memstats_heap_inuse_bytes`, `go_memstats_sys_bytes`, `go_gc_cycles_total`, `go_gc_pause_seconds_total`; after initialization also `nexadmin_db_connections_{open,in_use,idle,max_open}`, `nexadmin_db_wait_total`, `nexadmin_db_wait_seconds_total`, `nexadmin_migrations_pending`, `nexadmin_migrations_check_ok`, `nexadmin_job_queue_length`, `nexadmin_plugin_licensed` (the latter depend on their modules; post-init output not verified live).

### 9.3 Health details

"System → Server Monitor" (module `server-monitor`, permission `serverMonitor:list`):

- `GET /api/v1/monitor/health`: concurrently checks `db`, `cache`, `storage`, `migrations`, `plugins`, `disk` — each with a 3-second timeout, overall state = worst (`ok` < `warn` < `fail`). Rules: database slower than 500ms, cache slower than 200ms, storage slower than 1500ms → `warn`; pending migrations → `warn`; plugin licenses expired / invalid / expiring within 30 days → `warn`; the partition with the highest disk usage ≥85% → `warn`, ≥95% → `fail`.
- `GET /api/v1/monitor/server`: CPU, memory, disk, network, Go runtime (Linux reads `/proc`; other systems only runtime).
- Also cache monitor (`cacheMonitor:list`, `cache-monitor`) and API monitor (`apiMonitor:list`, `api-monitor`: per-endpoint QPS / latency / error rate over the last 30 seconds, in-process, reset on restart).

### 9.4 Frontend error reporting

The frontend `POST`s runtime errors to `/api/v1/client-errors` (reported with or without login, IP rate-limited by `ratelimit.client_error`, default 30/min; body capped at 8KB with server-side field truncation); admins view them under the server monitor page's "frontend errors" tab via `GET /api/v1/monitor/client-errors` (permission `serverMonitor:list`). Records are kept 14 days, cleaned hourly.

### 9.5 Logs

`app.env=dev`: console output; `production`: JSON to stdout and `logs/nexadmin.log` — no built-in rotation, use logrotate etc. yourself (`copytruncate` effect: unverified). Processes started via `start` additionally mirror console output to `logs/nexadmin.out`. One log line per request (method, path, status, latency, ip).

## 10. Multi-instance notes

Each item below comes from code or code comments:

- `database.node_id` must be unique per instance (snowflake node 0–1023; the export center also uses it to distinguish nodes). With a redis cache, startup uses a TTL lease (30 seconds, renewed every 10) to detect duplicates: a claim by another live instance refuses startup; restarting a crashed instance with the same node_id may be refused for up to 30 seconds. `memory` cache (single instance) skips detection.
- `cache.driver=redis` is required: token revocation, captchas, login-failure lockout, rate-limit counters and online sessions all live in cache; `memory` is per-process and invisible across instances (e.g. a token logged out on instance A stays valid on B; a captcha generated on A fails validation on B).
- `auth.jwt_secret` must match across instances: left empty, each instance reads its own `data/jwt.secret` — either share that file or configure the same value explicitly. Same for `data/secret.key` (or `NEXADMIN_SECRET_KEY`) so encrypted values decrypt everywhere.
- Migrations serialize via database locks — multi-instance simultaneous starts are safe; backups and export jobs use optimistic claiming and never run twice.
- Already atomicized (only one winner under concurrency; noted in code comments):
  - One-time credential "fetch and void": SSO code exchange, SMS captcha and forgot-password captcha successful validation, third-party `state`, bind tokens (`bindToken`, consumed after password verification and before writing the binding). The login graphic captcha (`core/auth/captcha.go`, valid for exactly one attempt regardless of correctness) uses the same pattern. All go through `cache.GetDel`: redis via a Lua script performing GET+DEL server-side in one step, memory under the same lock. Custom external caches lacking the optional `cache.AtomicGetDel` interface fall back to Get then Del (non-atomic) with a warning.
  - Quota "count then insert": user (create, import, third-party auto-create) and role creation first `SELECT ... FOR UPDATE` the tenant row within the same transaction, then count — concurrent creations for one tenant serialize and cannot exceed `maxUsers` / `maxRoles`. Quota 0 (unlimited), the default tenant and platform-level operations skip the lock and the extra query.
  - Failure counters: SMS captcha wrong entries (`modules/internal/smscode`), forgot-password wrong entries (`modules/system/user`), bind-token attempts (`modules/internal/oauthflow`), login-failure counts (`core/auth/captcha.go`, by username and IP dimensions) and SMS daily / hourly-per-IP caps all use atomic `cache.Incr` (helper `core/cache/incr.go`, TTL set on first creation, redis via Lua). Captcha-style counters increment before comparing, so concurrent guessing gets at most the first 5 comparisons, and hitting the cap `Del`s the code; counter keys carry a captcha salt — resending restarts from 0. Semantics unchanged (5 attempts void, 60s cooldown, daily caps). The only difference: the login-failure 15-minute window is now fixed from the first failure instead of sliding with each failure. Caches without `cache.Counter` fall back to in-process locked Get+Set (with a warning) — imprecise counting across instances.
  - Storage quota is atomically pre-allocated at tus session creation by declared length (`modules/file/usage.go`), checked as actual usage plus in-flight reservations, released on completion / cancellation, with leases cleaning expired reservations. Gated uploads require the cache to support `cache.Lease` and are rejected otherwise; multi-instance must share Redis/Valkey — memory suits single instance only. Real shared-cache concurrency acceptance is still pending.
- Remaining concurrency windows and limits (noted in code comments):
  - Online sessions now use "one key per session + a sorted-set index" — no more read-modify-write of the whole index, so concurrent logins lose no sessions and force-logout only affects the target. Residual windows: the same session registered by two instances simultaneously — last writer wins (losing only display fields: last-active time / token jti, corrected at next registration); while cleaning expired index entries, a concurrent re-registration may have an entry mistakenly deleted, briefly missing from the list until that session's next registration; caches without sorted-set support fall back to in-process storage, invisible across instances (built-in memory / redis both support it).
  - In-app message SSE and tenant-change invalidation events cross instances via `core/cluster`'s broadcast bus (redis Pub/Sub with auto-reconnect and full resync; memory cache keeps it local). This path has only been verified on fakes and miniredis, not on real multi-instance + redis.
  - Tenant info (expiry, quotas) still has a 30-second in-process cache fallback, but modifications broadcast invalidation so other instances usually take effect immediately — worst case 30 seconds if a broadcast is lost.
  - Rate limits fail open on cache errors (with alerts); token validation is the opposite — cache unavailability rejects.
- Unverified: overall behavior in real multi-instance topologies; the `cache.GetDel` / `cache.Incr` Lua scripts and quota `FOR UPDATE` row locks were verified only against memory cache / sqlmock for statement shape and order (this machine has no database or redis).

## 11. Security checklist

| Check | Practice & basis |
| --- | --- |
| Environment | `app.env=production`: Gin release mode, CORS whitelist-only, `reinit` / `restore` additionally require the database name. `dev` emits CORS headers for any origin — never for public environments |
| JWT secret | Empty auto-generates `data/jwt.secret` (0600); explicit config must be ≥32 chars or startup fails with `auth: auth.jwt_secret must be at least 32 characters`. Must match across instances (§10). Pass via env `NEXADMIN_AUTH_JWT_SECRET`, never commit it |
| secret.key | Static-encryption master key: `data/secret.key` (0600) or `NEXADMIN_SECRET_KEY` (32 bytes, 64 hex chars or base64). Keep with database backups — lost, encrypted values are unrecoverable |
| metrics token | When exposing `/metrics` externally or scraping through a proxy, set `metrics.token`; empty only allows loopback peers without forwarding headers |
| CORS | `server.cors_origins` lists only needed origins (exact match, no `*`); leave empty for same-origin frontend+backend |
| Setup wizard | Keep `setup.allow_remote=false`; initialize locally before opening ports. Containers / automation use `init --config` |
| Database account | Password via env `NEXADMIN_DATABASE_PASSWORD`; `config/config.yaml` contains the password — keep 0600, never commit |
| Third-party secrets | `auth.providers.*.client_secret`, `storage.s3.secret_key` etc. use `${ENV}` references — never in plaintext in the repo |
| SMS | Never `sms.provider: log` in production (codes go to logs; production enables a startup warning) |
| TLS | The service listens plain HTTP only (`ListenAndServe`, no TLS option) — HTTPS must terminate at a reverse proxy / load balancer in front |
| Open platform | PATs store only sha256 hashes; app secrets and webhook secrets are stored `core/secret`-encrypted (hence the `secret.key` dependency); apps can pin IP whitelists; token / signature validation failures are IP rate-limited (`ratelimit.openapi_sign`) |
| Before release | Run `nexadmin release check` in the framework repo — clear development keys, demo licenses, `config/config.yaml` and installed plugins (see `release check` FAIL items) |

### 11.1 Reverse proxy must-read

The service has two kinds of "address", used differently:

- **Real client IP** (`c.ClientIP()`): decided by `server.trusted_proxies` (an IP / CIDR list), default empty = trust no proxy, using only the TCP peer address — forged `X-Forwarded-For` / `X-Real-IP` are ineffective. When configured, forwarding headers are trusted only when the peer is in the list, taking the **rightmost address not in the list** (proxies append in order; the rightmost is the real source before the last hop; client-forged leftmost entries have no effect). Used by: all per-IP rate limits (login, captcha, SMS, tokens, frontend error reporting, open-platform failure counts, SSO token), login / operation logs, online sessions, open-app IP whitelists, forgot-password rate limiting. Hot-mounted plugin sub-engines use the same configuration.
- **TCP peer address** (`RemoteAddr`): used for "is this local" security decisions, never reading forwarding headers and unaffected by `trusted_proxies`: the wizard's local check, the `/metrics` loopback check, and the local shutdown endpoint (used by `stop`).

**Reverse-proxy deployments must configure `server.trusted_proxies`** (e.g. same-machine nginx writes `["127.0.0.1"]`, container networks write their subnet, env `NEXADMIN_SERVER_TRUSTED_PROXIES=127.0.0.1,10.0.0.0/8`), and the proxy must set `X-Forwarded-For` (nginx: `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;`) or `X-Real-IP`. Consequences when unconfigured (not verified live, from code and unit tests):

- All per-IP rate limits count against the proxy address — every user shares one bucket, and the default `ratelimit.login` (20/min) becomes site-wide shared.
- IPs recorded in login logs and online sessions are the proxy's; open-app IP whitelists also compare against the proxy address.
- After configuring, list entries must be exact: too wide (e.g. including public ranges) lets anyone in that range forge source IPs; the proxy must set or append forwarding headers rather than passing client-provided ones through.
- Setup wizard: requests forwarded by same-machine nginx have loopback peers but carry forwarding headers — the wizard **no longer treats them as local** and refuses by default; proxied initialization needs `setup.allow_remote=true` with `X-Setup-Token`, or use `init --config` directly.
- `/metrics` without a token only accepts loopback **peers** without `X-Forwarded-For` / `X-Real-IP`; proxied scraping requires `metrics.token`.

Proxy configuration essentials:

- Uploads are 5MB chunks (frontend `CHUNK_SIZE = 5MB`, tus `PATCH`); body limits like nginx `client_max_body_size` must not be smaller than the chunk, and disabling request buffering on upload paths is recommended. Application-layer size / type limits come from the "System Parameters" upload limits (effective within seconds after saving, up to 5 seconds later on multi-instance); unset items fall back to `config.yaml`'s `upload.max_size_*` (see Configuration Reference).
- In-app message SSE (`GET /api/v1/messages/stream`) already sends `X-Accel-Buffering: no`; proxy read timeouts should be long enough — after disconnects the frontend reconnects automatically and degrades to polling (per the design doc, not separately verified).
- Forward `Host`: tenant domain binding and plugin-license domain checks both read the request Host.

## 12. Upgrades

For the full framework and derived-project upgrade flow see the Secondary Development Guide §1. Operations essentials:

```
nexadmin upgrade --from <new-version source dir or zip> --dry-run    # list changes only; dirty workspaces warn in dry-run
nexadmin upgrade --from <new-version source dir or zip>              # real upgrade (dirty workspaces are refused unless --allow-dirty)
go mod tidy && (cd web && npm install)                               # after merging go.mod / package.json
sh scripts/build.sh                                                  # rebuild
```

(`nexadmin` means `go run ./cmd/nexadmin`, or a self-compiled `cmd/nexadmin`. `upgrade --from ... --dry-run --allow-dirty` was tested locally on a demo project created with `nexadmin new`, printing `summary: added 0, modified 0, deleted 0, unchanged 958, WARN 0`.)

Server-side actions after upgrade:

- `app.auto_migrate=true`: restarting automatically runs new framework migrations and tops up new menus (log: `N new menus added — assign them in Role Management` — new menus are visible only to the super admin until assigned).
- `app.auto_migrate=false`: run `nexadmin-server migrate --dry-run` to review, then `nexadmin-server migrate`, then start.
- Containers: rebuild the image; the entrypoint overwrites the volume's `config/default.yaml` with the image's copy and leaves `config/config.yaml` untouched.
- The revocation list (`config/license-revoked.json`) and additional public keys (`config/license-keys.pub`) are distributed by the vendor with upgrade packages and overwritten as framework files; the revocation list has seq rollback protection (a verified list with a seq lower than the trusted version is ignored with a warning; in-process only — after restart the disk file wins). When the package lacks these two files they are kept, not moved into `.nexadmin-trash/`, marked `K` in the `--dry-run` listing.
- Factory data follows "insert by business key only when absent": when a new release changes factory data, existing databases are not automatically updated — admins must change it in the admin UI.

## 13. Troubleshooting

Only error texts confirmable from code or local runs are listed. "Verified" means reproduced locally; the rest comes from reading code.

| Symptom / error | Cause | Fix |
| --- | --- | --- |
| `config: read config/default.yaml: open config/default.yaml: no such file or directory` (verified) | Not started from the project root, or missing `config/default.yaml` | Run from the project root; for systemd check `WorkingDirectory` |
| `config: server.port 99999 out of range`, `config: app.env must be dev or production, got "prod"` (verified) | Config validation failure (env vars trigger it too) | Fix the config. Other validation copy is in the Configuration Reference §1 |
| `listen tcp :8080: bind: address already in use` (verified; Go standard library text, not this project's copy) | Port occupied | Change `server.port` or stop the occupant; run `nexadmin-server status` first to see if it's your own instance |
| `database: connect mysql 127.0.0.1:3306: dial tcp ...: connection refused` (verified); PostgreSQL: `database: connect postgres host:port: ...` | Database unreachable: address / port / credentials / firewall / database down | Check `database.*`; `restore` and `reinit` report `cannot reach the database, aborted` |
| `database: unsupported type "x" (want mysql, mariadb or postgres)` / `database: invalid database name "x" (letters, digits, underscore only)` | Invalid engine or database name | Database names allow letters, digits, underscore only |
| After losing `data/`, startup errors on the database instead of entering the wizard | See §3.1: without `data/.initialized` but with `config/config.yaml` present, it connects to look for an admin — unreachable counts as "initialized", then full-route assembly fails on the database (better to misjudge as initialized than uninitialized) | Restore database connectivity; if an admin really exists, just restore the marker file |
| `migration file modified after execution (violates append-only), refusing to start: framework/xxx.sql, .... Restore these files...` | An executed migration's content changed (including hand-editing framework files or overwriting with a different version) | Restore the file; write new changes into a new numbered SQL; check `migrate --status` for `CHECKSUM mismatch` files |
| `N pending migrations (...), app.auto_migrate=false, run nexadmin-server migrate first` | `auto_migrate=false` with new migrations after upgrade | Run `nexadmin-server migrate` |
| `migrate: cannot find framework migration directory <path> (engine <engine>; run from the project root or set NEXADMIN_MIGRATIONS_DIR)` / `migrate: cannot find framework migration directory migrations/framework (...)` | Missing `migrations/` directory, or no subdirectory for the engine | Place `migrations/` under the working directory (or an ancestor / beside the executable), or set `NEXADMIN_MIGRATIONS_DIR`; don't forget it in systemd / container deployments |
| `migrate: <scope>/<file>: executed N/M statements, statement N+1 failed, file not recorded, repair manually first: ...` (MySQL / MariaDB) | DDL cannot roll back; the file half-ran | Repair the database manually then mark with `migrate --mark-applied <scope>/<file>`; PostgreSQL reports `statement N/M failed, file rolled back entirely` — fix and re-run |
| `migrate: migration lock wait timed out (2m0s), another instance may still be migrating` | Another instance held the lock over 2 minutes | Check whether other instances are stuck; the MySQL lock is `nexadmin_migrate_<dbname>`, auto-released when the session ends |
| `cache: redis/valkey at <addr> is unavailable: ...` / `config: cache.addr is required when cache.driver is redis` | Cache unreachable / address missing | Check `cache.*` |
| `auth: auth.jwt_secret must be at least 32 characters` | Explicit JWT secret too short | At least 32 characters, or leave empty to auto-generate |
| `secret: key from <source> must be 32 bytes encoded as hex (64 chars) or base64` | `NEXADMIN_SECRET_KEY` malformed | 32 bytes as hex (64 chars) or base64 |
| `core: duplicate module name "xxx"` | Business module or plugin named like a built-in | Rename the business module's `Name()` (and its permission-code and table prefixes) |
| Business APIs return HTTP 503 with `reason: NOT_INITIALIZED` (verified) | System uninitialized | Use the wizard or `init --config` |
| After login APIs return HTTP 403 with `reason: TENANT_EXPIRED` | Tenant expired / suspended (beyond grace) | Renew or enable in Tenant Management; tenant info has a 30-second cache fallback, and changes broadcast invalidation across instances (§10), effective within 30 seconds at most |
| HTTP 429 with `Retry-After` | Rate limit hit | See Configuration Reference §6; behind a proxy see §11.1 |
| `service is running but was not started by start (no pid file); stop it with Ctrl+C in its terminal` | A foreground process has no pid file | Ctrl+C in its terminal |
| `shutdown refused (HTTP 403): the pid file's token does not match the running process` | `data/run/nexadmin.pid` stale or altered | Verify the process, end it manually, delete the pid file |
| `process exited (<reason>), see logs/nexadmin.out` / `timed out waiting for service start` | A `start`-launched background process failed or wasn't ready within 20 seconds | Read `logs/nexadmin.out` |
| `service is running, restore refused; run stop first (restore overwrites live data)` | `restore` without stopping first | `stop` first |
| `cannot find <backup>.sha256 — integrity cannot be verified; provide --sha256 <value> manually, aborted` | Backup missing its sidecar checksum | Restore the `.sha256` or use `--sha256` |
| `mysqldump / mariadb-dump not found on this machine; install the database client and add it to PATH` | Client tools needed by backup / reinit missing from PATH | Install clients; container images don't include them by default |
| Startup log `file module disabled: storage init failed` | File module storage init failed (e.g. S3 config, unset `${ENV}`) | Read the error on the same line; the file module's routes were not registered |

## 14. Verification notes

| Item | State |
| --- | --- |
| Subcommand list and usage | Verified locally (`nexadmin-server <unknown>` prints usage; `version`, `status`, `start`, `restart`, `stop`, `init`, `migrate --dry-run/--status`, `reinit --dry-run`, `restore --dry-run` all run) |
| Uninitialized mode: `/healthz`, `/metrics`, `/api/v1/system/status`, business 503, `setup/env` | Verified locally |
| Frontend build + `-tags embedweb` build + static serving | Verified locally (`npm ci` not run) |
| `nexadmin new`, `add module`, `upgrade --dry-run`, `release check`, `release pack` (refuses with FAIL), `plugin list`, `i18n check` | Verified locally |
| Env override rules (int / bool / duration / comma list / nested keys) | Verified locally |
| Successful init, real migration runs, backup, restore, full reinit, multi-instance behavior | Not verified live (no database / redis / client tools) |
| Dockerfile build, compose, systemd unit | Manually reviewed only, not run |
| `deploy/` `.sh` / `.command` / `.bat` scripts, `scripts/build.bat` | Content read only, never run (scripts without a terminal try to open one); their `nexadmin-server` subcommands were verified |
| `docker compose` commands | The local Docker lacks the `compose` subcommand — the commands themselves could not be checked |
