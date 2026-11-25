---
title: Plugin Development Guide
summary: Plugin package layout, manifest, backend and frontend conventions, plus packaging and install checks.
---

# NexAdmin Plugin Development Guide

This guide explains how to build and package a NexAdmin plugin. It uses Scheduled Jobs (cron), part of the initial free plan, to illustrate package structure. Obtain its released free install package from the vendor's marketplace or standalone plugin source repository. Framework source packages and default `nexadmin new` projects do not include cron or a paid cron example.

Base services and optional general-purpose plugins are free; business enhancements are paid. The initial free plan covers `cron`, `codegen`, `imports`, `totp`, `ldap`, `maps` and `crm`; other plugins are planned as pro. This does not mean every existing package has changed edition or that LDAP / TOTP are compiled and mounted. Check manifests, Go registration and runtime state. See the [Quickstart](quickstart.md) for free-plugin verification.

> Who this is for: developers who can write Go (Gin + GORM) and React. You do not need to understand the whole framework — only this document and the cron example.

## 1. Three things to understand first

**A plugin is source code, not a binary.** Go cannot hot-load code at runtime, so plugins are delivered as source packages: install = place files into the agreed directories + recompile. Buyers receive readable, modifiable source — which is also how NexAdmin itself is delivered.

**Plugins come in free and professional (pro) editions.** `edition: free` skips license validation; `edition: pro` only works after the buyer imports a license file. Licenses are issued by the platform — authors do not implement license checks themselves.

**A plugin is self-contained.** One plugin = one backend Go package + one frontend directory + one manifest (`plugin.yaml`). Installing and uninstalling only touch these places and never modify framework code.

## 2. Package layout

```
cron/                         # directory name = plugin id
├── plugin.yaml               # manifest (required)
├── backend/                  # installed to plugins/cron/
│   ├── plugin.go             # registers the plugin in init()
│   ├── *.go                  # your business code
│   └── migrations/
│       ├── mysql/001_init.sql
│       ├── mariadb/001_init.sql
│       └── postgres/001_init.sql
└── web/                      # installed to web/src/plugins/cron/
    ├── route.tsx             # frontend routes
    ├── api.ts
    ├── cron.mock.ts          # frontend mock (optional; backend-free demo only)
    ├── CronPage.tsx
    └── locales/{zh-CN,en-US}.json
```

`plugin.yaml` must be findable in the zip root or in its single top-level subdirectory.

## 3. The manifest: plugin.yaml

```yaml
id: cron                     # lowercase letters/digits/hyphens, globally unique, max 48
name: Scheduled Jobs
version: 1.0.0
description: Scheduled jobs (5-field cron scheduling, run now, failure retry, run logs)
author: NexAdmin
edition: free                # free cron example; paid plugins use pro
requires_nexadmin: ">=0.1.0"
migration_target: platform     # platform (default) | tenant; declare table ownership when isolation is enabled

menus:                       # written idempotently into sys_menu on install/mount
  - parent: /system          # parent node: a directory uses its path; a button uses the menu's perm
    type: menu               # dir | menu | button
    title: plugin_cron.menu  # i18n key; copy lives in web/locales
    path: /system/cron
    component: plugins/cron/CronPage
    perm: cron:list
    icon: Timer              # lucide icon name
    sort: 100
  - parent: cron:list
    type: button
    title: plugin_cron.btn.edit
    perm: cron:edit
    sort: 10
```

Rules:
- Every permission code must start with `<id>:` (enforced at install time).
- Never hard-code Chinese copy for menus — always use i18n keys.
- Whether a menu is visible to a role is decided by the buyer in "Role Management", not by you.
- Migration targets default to `platform`; a `tenant_id` column does not automatically move a table into a tenant business database. Declare `tenant` only when every table is physically split per tenant and queries do not join control-plane data. With `database.isolation` enabled, platform-plugin tables belong in `platform_tables` and `tenant`-plugin tables in `tables`; the manifest and config must agree. Workflow currently uses platform storage, so all five `plg_workflow_*` tables belong in `platform_tables`.

## 4. Backend

### 4.1 Entry point

```go
package cron

import (
    "context"
    "github.com/gin-gonic/gin"
    "nexadmin/core"
    "nexadmin/core/plugin"
)

func init() {
    plugin.Register(plugin.Plugin{
        ID: "cron", Name: "Scheduled Jobs", Version: "1.0.0", Edition: plugin.Free,
        Module: &module{},
    })
}

type module struct{}

func (*module) Name() string { return "cron" }

// Register registers routes. r is already /api/v1 and requires login by default.
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
    g := r.Group("/plugins/cron")
    g.GET("/jobs", core.Perm("cron:list"), listJobs(d))
    g.POST("/jobs", core.Perm("cron:edit"), createJob(d))
}
```

`ID`, `Version` and `Edition` must match `plugin.yaml`.

#### Tenant data and deletion protection

Tenant deletion uses an explicit registry rather than runtime schema scanning. A module implements `nexadmin/core/tenant.Provider`, and enabled modules and plugins are collected automatically at startup:

```go
func (*module) TenantTables() []tenant.Table {
    return []tenant.Table{{Name: "plg_cron_job", Column: "tenant_id"}}
}
```

Declare real tenant-scoped business tables only; soft-deleted rows also block deletion. Register pending jobs, retry deliveries, callbacks and foreign-key references that remain functionally relevant after deletion. Platform-shared configuration and append-only audit/usage/history tables may be omitted and handled by platform retention policies. Disabled modules and plugins are not collected. The registry only decides whether deletion is allowed; it never cascades or replaces declarations with scattered hard-coded table lists.

### 4.2 Background tasks (optional)

Implement `plugin.Starter` if you need a resident goroutine (a scheduler, a consumer), or need to register process-local events, cross-instance events or asynchronous jobs. A plugin without a `Starter` cannot register jobs; implement `Starter` even if you only need job registration and no background goroutine.

```go
func (m *module) Start(ctx context.Context, d *core.Deps) error {
    go scheduler.Run(ctx) // must not block; ctx is cancelled when this run ends or the process exits
    return nil
}
```

The framework waits synchronously for `Start` to return, so initialization must finish quickly. Before `Start` finishes, routes are already registered but blocked by the readiness gate with HTTP 503 and `Retry-After: 1`; `/plugins/active` and plugin menus are unavailable. If `Start` returns an error or panics, this run's readiness closes, `needRestart=true` is set, routes remain registered but are rejected by the gate, and a restart is required after fixing the cause. Do not start a permanent loop that blocks `Start`; launch resident work in a goroutine and handle the supplied ctx correctly.

Before each `Starter.Start`, the plugin manager injects `d.PluginRuntime`, a `*core/lifecycle.Runtime`, for that run. Register runtime extensions through it inside `Start`:

- `d.PluginRuntime.Subscribe(...)`: process-local events.
- `d.PluginRuntime.SubscribeCluster(d.Bus, ...)`: cross-instance events.
- `d.PluginRuntime.RegisterJob(type, handler)`: asynchronous jobs.

Do not use the global `event.Subscribe` / `job.Register` APIs; `plugin lint` rejects both. When the run ctx ends, these registrations are revoked automatically and jobs already started through this runtime are cancelled.

### 4.3 Conventions (violations are rejected in review)

- API paths: `/api/v1/plugins/<id>/...`.
- Table prefix: `plg_<id>_`, e.g. `plg_cron_job`.
- Permission code prefix: `<id>:`, e.g. `cron:list`. Every endpoint needs `core.Perm(...)`.
- You may only depend on `core/` and `pkg/`. **Do not modify core, do not modify framework, do not import built-in modules or other plugins.**
- Framework capabilities available in `core.Deps`: `DB`, `Cache`, `Auth`, `Log`, `Config`, the unified notify entry `Notify` (in-app / email / SMS / bot, nil-safe), cross-instance event broadcast `Bus` (nil-safe), current-run extensions `PluginRuntime`, shutdown cleanup registration `OnClose`, runtime API docs `APIDoc`, plus the `Public` route group (no login) and the super-admin-only `Admin` group. Do not query other modules' tables directly; if you truly must read a built-in table (like cron's `clean_logs`), use table-name constants and handle a missing table without panicking.
- Business primary keys use snowflake IDs (`pkg/idgen`), serialized as strings in APIs.
- Tenant isolation: tenant-scoped tables carry `tenant_id`, and queries must include the current tenant condition.
- Outbound requests (HTTP callbacks etc.) must guard against SSRF: internal and loopback addresses are forbidden, and timeouts are required. cron's `ssrf.go` is a ready reference.

### 4.4 Database migrations

Place them in `backend/migrations/<engine>/` — **mysql, mariadb and postgres must all be present**, named `001_init.sql`, `002_xxx.sql` and so on, **append-only** (a released migration never changes; changes go into new files). The runtime executes files in name order, records applied versions, and never re-executes.

The migration runner tokenizes SQL by database engine. It recognizes single/double quotes, MySQL/MariaDB backticks, escaped quotes, PostgreSQL dollar quotes and executable comments; semicolons inside quotes, default expressions or `DO` blocks are not split. `DELIMITER` and stored-program syntax such as `CREATE PROCEDURE`/`FUNCTION`/`TRIGGER`/`EVENT` are rejected — rewrite them as ordinary SQL or move them into application code.

**Migrations change structure only — no `INSERT`/`UPDATE`/`DELETE`.** The runner warns about every DML statement in `migrate --dry-run`. Seed data (menus, dictionaries, templates) belongs in a Seeder, and reconciling the menu shape of an existing install lives there too (see `modules/system/menu/reconcile.go`).

Watch the engine differences: auto-increment/primary-key types, `TEXT` defaults, boolean types, `ON UPDATE` syntax. cron's three SQL files are a ready comparison.

### 4.5 Tests

The repository no longer keeps Go/frontend automated tests (removed in commit 22ff72c on 2026-11-08; the Python helper tests under `scripts/ci` still run in CI). A plugin must at least pass `go build ./...` and `go vet ./...`; describe the scope and result of migration, lifecycle and menu-write verification against a real database. Automated tests may supplement this but never replace real-API integration.

### 4.6 Providing import sources (optional)

If your plugin owns master data worth bulk-importing, implement `Provider` from `pkg/importspec` and it shows up in the Import Center — **you do not depend on the imports plugin itself**:

```go
func (m *module) ImportSources(db *gorm.DB) []importspec.Source {
    svc := NewService(db, zapLog{zap.NewNop()}) // your plugin's logger adapter
    return []importspec.Source{{
        Name: "crm_customers", Title: "客户导入", TitleKey: "imports.source.crm_customers.title",
        Columns: []importspec.Column{
            {Key: "name", Label: "客户名称", LabelKey: "imports.source.crm_customers.name", Required: true, Example: "张三"},
        },
        Handler: func(ctx context.Context, row int, vals map[string]string) error { /* persist one row */ },
    }}
}
```

Add those two keys to the backend locale packs, otherwise the source name and template headers fall back to the Chinese strings in code. They live under `imports.source.<source name>` in `core/i18n/locales/<lang>.yaml` (the `imports` block already exists — just add the source under `source:`), with identical key paths in `zh-CN.yaml` and `en-US.yaml`:

```yaml
imports:
  source:
    crm_customers:
      title: Customer import   # source name for Source.Name (dropdown and job list)
      name: Customer name      # one key per column, named after Column.Key
      phone: Phone
      remark: Remark
```

- `imports.source.<Source.Name>.title` backs `TitleKey`; every `Column.Key` backs the `LabelKey` of the same name. Column keys take no extra prefix such as `title.`.
- Keep the two declarations in sync: `Source{Name: "crm_customers"}` ↔ `imports.source.crm_customers`, and `Column{Key: "name"}` ↔ `imports.source.crm_customers.name`. A mismatch does not error — it silently falls back to `Title`/`Label`, so check the YAML nesting first when debugging.
- Leaving `TitleKey`/`LabelKey` empty always uses the fallback text (effectively Chinese-only). Omit them only when the source truly has a single language.
- Per-row business error texts (e.g. `dict_data.sort_invalid`) live in the same `imports.source.<source name>` block; the `Handler` translates them with `i18n.T(ctx, key)` before returning.

Rules:

- Sources are collected when the Import Center mounts. Plugin modules reach the core wrapped, and the collector unwraps them first (`core.UnwrapModule`). A duplicate name logs a warning and is skipped; it never panics. Built-in modules may implement the same interface (no reverse dependency on the plugin); two ship out of the box: `crm_customers` (`plugins/crm/import.go`) and `dict_data` (`modules/system/dict/importsource.go`).
- `Label` is the fallback header. When `TitleKey`/`LabelKey` are set, the source name and template headers render in the request language, and parsing accepts the headers of **every configured language** — so a template downloaded in one language still imports after switching. Add the keys when you add a language.
- `Handler` handles exactly one row; returning an error marks that row failed (other rows continue). The report is read by humans, so translate before returning — `i18n.T(ctx, key)`, never a bare key such as `crm.name_required`.
- Large files run on a background goroutine with `context.Background()` (tenant and language restored from the job row). Use the `ctx` you are given and never cache request-scoped state.
- Match by a business key and update-then-insert, so importing the same file twice does not duplicate rows — see `plugins/crm/import.go`.

## 5. Frontend

The frontend directory matches built-in modules exactly; after install it lives at `web/src/plugins/<id>/`, and its routes, mocks and i18n copy are discovered automatically — **no framework file changes needed**.

### 5.1 Routes

```tsx
// web/route.tsx
import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/cron',
    component: lazy(() => import('./CronPage')),
    perm: 'cron:list',
    menu: { title: 'plugin_cron.menu', icon: 'Timer', order: 100, group: 'system' },
  },
]
```

The sidebar actually comes from the server-side `sys_menu` (i.e. the `menus` in `plugin.yaml`), so the `path` here must match the `path` in `plugin.yaml`.

Contributions merge by owner: `framework` < `plugin` < `pages`. Only `pages` may override another owner's route; a plugin overriding a framework route or a duplicate route within the same owner fails the build. A plugin route declares ownership through `plugin`; when replacing a route that still depends on plugin activation, explicitly set `requiresPlugin: '<id>'` — an override does not inherit the previous plugin dependency.

Plugin activation has three states: `undefined` means not loaded yet and temporarily filters plugin contributions; `null` means the backend is unavailable and retains them; an array filters by the enabled plugin IDs in it. Retaining contributions on `null` is a degraded display policy, not authorization. `/plugins/active` does not list a plugin before its Starter is ready.

A normal route also needs a matching frontend page at the same path. External-link menus have no frontend route, so ownership comes from their `<plugin-id>:<action>` permission namespace and follows the same active-state filter as plugin routes. The plugin ID must satisfy the framework ID rules; external links without a valid plugin namespace skip this filter. Backend menu queries apply runtime availability to registered plugins as well, preventing disabled entries from leading to HTTP 403.

If the page is just a configuration block, make it a System Settings section instead of a standalone sidebar entry: declare `settings` on the route (`component` points to a section component without `PageContainer` / `PageHeader`), keep the `plugin.yaml` menu at its original parent with `hidden: true`, and redirect the legacy path with `<Navigate>` to `/system/settings?tab=<key>`. Do not reparent the menu under `settings:list` just to "put it under settings": role assignment fills in ancestors, which silently grants `settings:list`. The menu row still supplies the permission code, so role grants are unaffected. Within the same owner, duplicate settings sections with the same `key` keep the first discovered item and log a warning. See `plugins/ldap/route.tsx` and `plugins/maps/route.tsx`:

```tsx
{
  path: 'system/map',
  component: () => <Navigate to="/system/settings?tab=maps" replace />,
  perm: 'maps:config',
  settings: { key: 'maps', group: 'integration', title: 'plugin_maps.menu', order: 100, component: lazy(() => import('./MapSettingsPage').then((m) => ({ default: m.MapSection }))) },
}
```

### 5.2 Conventions

- **i18n**: the namespace is fixed to `plugin_<id>`, written in both `locales/zh-CN.json` and `en-US.json` with the top-level key `plugin_cron`. Dynamically composed keys (e.g. `` t(`plugin_cron.type.${v}`) ``) must have copy for every value.
- **Components**: use only `@/framework/components` and `@/framework/ui`, e.g. `DataTable`, `FilterBar`, `ResponsiveDialog`, `FormLayout`, `PageHeader`. Do not use `ui/table`, `ui/dialog`, `ui/sheet` or `ui/drawer` directly (the ESLint rule currently covers `src/pages/**` and built-in module pages, not `src/plugins/**`; plugins get no warning, so review checks this manually).
- **List states**: pass query `isLoading`, `isError`, `isFetching` and `refetch` to `DataTable` / `TreeTable` as `loading`, `isError`, `retrying` and `onRetry`. The component distinguishes initial-load failures from refresh failures that retain stale rows and offers an inline retry. Pass `hasFilters` once filters are applied to distinguish a truly empty list from zero matches; `hasActiveFilterValues` detects active server-side filter parameters. Pass `total={data.total ?? data.length}` when pagination is disabled to show the match count. Never submit a filtered subset as a full-list ordering: retain the sorting intent and disable it explicitly with `sortable.disabled` + `disabledReason` (or `reorderDisabledReason` and no sorting callback for tree tables), explaining why. Join list copy with `t('common.listSeparator')` instead of hard-coding Chinese punctuation.
- **Colors**: semantic tokens only (`bg-card`, `text-muted-foreground`, `border-border`…). Never `#fff` or `bg-blue-500` — hard-coded colors break the dark theme (ESLint warns across `src/**`).
- **Layout**: mobile first; use logical classes (`ms-`, `me-`, `ps-`, `pe-`, `start-`, `end-`) instead of `ml-`, `pl-` so RTL languages also work. Touch targets are at least 40px on mobile.
- **Form controls**: prefer Select over Input wherever applicable (enums, dictionaries, foreign keys, booleans and time all have ready controls).
- **Buttons**: icon buttons must carry `aria-label` or `title`, which auto-generates hover hints.
- **Permissions**: gate button visibility with permission codes, same as built-in pages; wildcards such as `module:*` are supported. The frontend is only a UI aid; declare endpoints with `core.Perm` and rely on authoritative backend enforcement.
- **Dashboard cards (optional)**: export `cards: DashboardCard[]` from `web/cards.tsx` (types from `@/framework/modules/dashboard/types`); they appear on the dashboard after install and can be hidden and reordered by users. `id` uses `<plugin-id>-<meaning>` (only `[a-z0-9-_.]`, max 64), `title` is an i18n key under the `plugin_<id>` namespace, `perm` is the plugin's own permission code, and the component uses `React.lazy` and fetches its own data; the backend endpoint is required, while a mock is optional. Duplicate cards with the same id within one owner keep the first item and log a warning. See the cron plugin's `cron-overview`.

### 5.3 Mocks (optional)

`web/<id>.mock.ts` may use `defineMock` / `createCrudMock` to serve endpoints for a backend-free demo. When using mocks, `npm run dev` enables them; disable mocks and start the backend for real-API integration (`VITE_MOCK=false npm run dev`). State the APIs, databases and scenarios verified in the submission notes. Mocks are optional; real-API integration is not.

## 6. Local development workflow

1. Derive a development project using the [Quickstart](quickstart.md). Prepare the package structure outside that project, or use the free cron package you obtained as a reference; you can also generate a full-stack skeleton with `go run ./cmd/nexadmin plugin create <id> --out <dir> [--name <name>] [--edition free|pro]` (manifest, backend entry, frontend route/page/api/mock, bilingual locales) — an existing target is never overwritten. Give your own plugin a new ID, permission prefix and table prefix; do not write business code in the framework template. Replace example `nexadmin/...` imports with the derived project's actual Go module path.
2. Frontend: put `web/` into `web/src/plugins/<id>/`. Run `npm run dev` for mock-based development when mocks are provided; for real-API integration, start the backend and run `VITE_MOCK=false npm run dev`.
3. Backend: put `backend/` into `plugins/<id>/`, and copy the package-root `plugin.yaml` to `plugins/<id>/plugin.yaml`. The directory name must exactly match the manifest `id` (e.g. `i18n-editor`, not `i18neditor`; the Go package name may differ); the manager locates the manifest and `migrations/` by ID. Add `_ "nexadmin/plugins/<id>"` between the marker lines in `plugins/plugins.go` (the install command does this automatically).
4. Sign a development license (required for pro plugins) and put it in `data/licenses/<id>.lic`:

```bash
go run ./cmd/nexadmin plugin keygen            # first time: generate a dev key pair
mkdir -p data/licenses                       # sign does not create the parent directory
go run ./cmd/nexadmin plugin sign --key .dev-keys/private.pem \
  --plugin <id> --customer Test --expires 2027-12-31 --out data/licenses/<id>.lic
```

   Append the base64 public key printed by `keygen` as a separate line in local `config/license-keys.pub`, preserving existing keys (`#` comments are supported), then restart the development process to reload its trust set. **Do not modify `core/plugin/pubkey.go`, commit private keys, or ship development trust settings or test licenses in production packages.** The additional-key file is framework-managed during upgrades; recheck local development settings afterward. Self-signed licenses are for isolated development, not a substitute for purchased licenses.
5. Inspect disk state with `go run ./cmd/nexadmin plugin list`. Free plugins show `-` in the `LICENSE` column; pro development licenses show `active` only after verification. This does not prove compilation or mounting.
6. Rebuild and start the server, then have a platform super admin verify runtime status, logs and actual endpoints in System Management → Plugin Management. Ordinary tenants receive business-use permissions only.

## 7. Packaging and install verification

```bash
zip -r cron-1.0.0.zip cron/        # directory name = plugin id; the package must not contain node_modules, .git, license files or keys
```

Verify installation in a clean derived project (substitute the actual package filename and plugin ID below):

```bash
go run ./cmd/nexadmin plugin lint cron-1.0.0.zip  # package layout: plugin.yaml + backend/ + web/
go run ./cmd/nexadmin plugin install cron-1.0.0.zip
# Inside a framework or derived source project, lint by plugin ID:
go run ./cmd/nexadmin plugin lint --layout source cron
go build ./... && go vet ./...
cd web && npx tsc --noEmit && npx eslint src
```

`plugin lint` never executes plugin code and runs automatically during install, packaging and admin rebuilds. It checks valid SemVer; Go `plugin.Register` ID/Name/Version/Edition against the manifest (string constants and `plugin.Free/Pro` supported; dynamic registration refused); entry `init` functions limited to static registration, with global event subscriptions and job registration refused in favor of `d.PluginRuntime`; static `export const routes` paths/permissions against manifest menus (hidden auxiliary routes require `hidden: true`, no menu and plugin-owned permissions); optional mock files, which must be non-empty when present; identical zh/en locale key sets covering static `t(...)`/`i18nKey` and menu titles, with dynamic keys restricted to fixed literal namespaces; and identical mysql/mariadb/postgres migration filename sets. `common.*`, `dataTable.*` and `localeForm.*` come from the host locales. It does not type-check TypeScript — still build and perform real-API integration before publishing.

CLI `plugin install` and `plugin remove` also write persistent recovery records. A source-only install does not replace the executable, so recompiling later is not blocked by the old executable hash gate. See "Admin marketplace installs" below for recovery commands.

Re-installing over an existing plugin uses `go run ./cmd/nexadmin plugin install --force <dir-or-zip>`. The three relaxation switches are independent of each other and must all come before the path (the install subcommand uses standard `flag.Parse` and does not parse options after positional args):

- `--force`: only allows replacing an already-installed plugin. Before overwriting, the old sources are persistently backed up to `data/plugins/backups/` (database and licenses excluded).
- `--allow-incompatible`: skips the `requires_nexadmin` version check. CLI only; the admin marketplace never skips it.
- `--allow-modified`: allows overwriting even when the installed sources have local modifications against the install ledger's SHA-256 fingerprints (or the legacy ledger has no baseline); without it the install is refused, protecting local customizations.

To repackage an installed plugin as a release bundle: `go run ./cmd/nexadmin plugin pack [--out <path.zip>] <id>` (produces a zip of `plugin.yaml + backend/ + web/`, the inverse of install; `--out` also precedes positional args). `release pack` packages the framework itself. `plugin list` reads the on-disk manifest and offline licenses — it does not prove that the Go blank import, migrations and runtime mounting succeeded; also check `plugins/plugins.go`, the startup log and the runtime `GET /api/v1/plugins`.

The install command rejects zip-slip archives and caps the extracted file count and total size. Install, removal and rebuild are serialized across processes through `.nexadmin-plugin.lock` in the project root, with a two-minute wait limit. Only marketplace plugins recorded in the install ledger may be overwritten or removed through the marketplace; bundled project sources are protected from both operations. A legacy manually installed plugin absent from the ledger can only be removed explicitly with the CLI's `plugin remove --allow-unmanaged <id>`; the admin UI has no bypass. Uninstall and disabling:

- `plugin remove <id>`: deletes `plugins/<id>/`, `web/src/plugins/<id>/` and removes the import from `plugins.go`; **data tables and the license file are kept** — after reinstall the data comes back as it was. Recompile and restart to take effect.
- `plugin remove --purge <id>`: **stop the service and back up the database first**; on top of remove, connects to the database and cleans up — drops tables with the `plg_<id>_%` prefix (platform plugins clean the platform database; `migration_target: tenant` plugins clean each tenant business database) and deletes `sys_menu` rows by the manifest's menu codes; if the manifest is missing, tables are still cleaned by prefix and menus must be handled manually. The license file is kept. It is refused while the service is running; if the database cannot be cleaned the command aborts without deleting files, and can be retried after stopping the service and fixing the cause.
- Temporarily disabling (no code deleted, no data touched): set `disabled_plugins: [<id>]` in `config/config.yaml`; after restart the plugin is not mounted at all, the plugin management page shows "disabled" and `plugin list` shows `disabled`. Free plugins have no license gate; pro plugins may also be disabled without revoking a license, but license import is unavailable while disabled — enable the plugin first. Removing it from the config and restarting restores the plugin. The runtime switch (`POST /api/v1/plugins/<id>/enabled`, permission `plugin:edit`) writes the node-local `data/licenses/disabled.json` overlay. Both sources are unioned and **configuration takes precedence**: a configuration-disabled plugin cannot be enabled from the page (HTTP 400 with guidance to change the configuration). Multi-node deployments do not synchronize the operator overlay; switch each node separately, while cluster consistency remains configuration-driven. See the limitations below.

### Admin marketplace installs (batch, environment preflight, update protection)

Install/update/upload/remove/rebuild in the admin "Plugin Management → Marketplace" share one set of protections:

- **Environment preflight**: on load the page calls `GET /api/v1/plugins/market/environment`, checking the project source tree, `go`/`npm` availability, web dependencies (vite/typescript in `node_modules`) and managed-restart capability; when not ready it shows per-item guidance and disables the affected action buttons. A production single-binary deployment has no sources or toolchain — operate in a derived project on the build host and publish from there.
- **Batch updates**: "Update all" first downloads every package and passes each through the pre-install checks (this phase writes nothing to the project), then lands all sources inside one rollback boundary, and finally builds and restarts exactly once.
- **Failure recovery and persistent journals**: when a failure is detected during the source-change or build phase of install, update, batch update or rebuild, restore the plugin sources, dependency manifests, frontend artifacts, install ledger and executable from before this run. Each transaction has its own directory under `data/plugins/recovery/job-*/`, containing `manifest.json`, `events.jsonl`, source snapshots and `executable.backup`. Candidate and old executables use transaction-specific `exe.<job-id>.new` and `exe.<job-id>.old` paths so concurrent jobs cannot overwrite each other. Unresolved `prepared`, `activating` or `recovery_required` records block server startup and new source changes. **Automatic restart failure after transaction commit is outside rollback scope**: sources, build artifacts, ledger and executable may already have been replaced; the job reports an error, and you must restart manually and verify the version and plugin state.
- **Recovery commands**: stop the server and run `nexadmin-server recover-plugin-change --status` to inspect records. `--rollback --yes` auto-selects only when exactly one `prepared` / `activating` / `recovery_required` transaction is unresolved; multiple unresolved transactions are rejected. With none unresolved, it selects the newest `ready` record. `--accept-current --yes` first requires every unresolved transaction to be rolled back, then accepts the current sources and binary outside the newest `ready` / `restored` recovery point. `nexadmin-server recover-plugin-change --rollback --yes` and `nexadmin-server recover-plugin-change --accept-current --yes` share `.nexadmin-plugin.lock` with install, remove and rebuild so recovery writes cannot interleave with concurrent source changes. Records that replaced the executable first verify at startup that the running program path matches the transaction's canonical exe path, then verify its SHA-256; starting a copied binary or an old `.old` copy is refused. Source-only records that have not replaced the executable skip this gate. A `.initializing` marker distinguishes a crash before manifest creation from corruption, and pruning an old recovery point also removes its old executable.
- **Rollback boundary**: automatic and manual recovery do not promise rollback of databases, licenses, uploads or user data; applied data changes need separate backups or manual handling. `plugin remove --purge` deletes database objects and is not automatically reversible. An old executable copy alone contains no old sources, dependencies, frontend artifacts, ledger or runtime data and is not a complete recovery point.
- **Local-modification protection**: the install ledger (`data/plugins/installed.json`) records SHA-256 fingerprints of every plugin's sources; before updating, the admin compares them and asks for explicit confirmation when local modifications or an unknown baseline are detected. Pre-overwrite sources are persistently backed up under `data/plugins/backups/`.
- **Package identity check**: a catalog entry whose id/edition/version disagrees with the in-package manifest is refused.
- The temporary zip of an uploaded install is deleted once the job takes it over, whether it succeeds or fails.

### The website plugin marketplace (website plugin)

`config/plugin-market.yaml` drives both the admin "Plugin Marketplace" and the website:

- `/plugins`: search, category, free/pro filters; `/plugins/<id>`: details, install methods and license notes.
- `/plugins/<id>/download`: the server pulls source from the catalog's `url` or the GitHub `repo`/`ref`, locates the plugin via `subdir`, verifies the in-package `plugin.yaml` `id`/`edition`/`version`, and repackages into a `plugin.yaml + backend/ + web/` install bundle. Artifacts are cached on disk for 6 hours; the route has its own rate limit (30 req/min/IP) and responds with `X-Checksum-SHA256`.
- `/plugins/<id>/download.sha256`: a `sha256sum`-format checksum file matching the download artifact, verifiable with `sha256sum -c`. Zip timestamps are pinned to 1980-01-01 during packaging, so repacking the same source yields identical bytes and checksums are comparable across instances.
- `/plugins/index.json`: a machine-readable catalog (download URLs, install commands, source URLs) for scripts and external tools.
- The detail page's "One-click install in admin" links to `/admin/system/plugin?tab=market&plugin=<id>`; after login you land on the admin marketplace filtered to that plugin.
- The catalog's `edition` must match the plugin package, otherwise the download is refused; `subdir` only accepts relative paths inside the repository.

Both the website and the admin read this YAML directly — changes take effect immediately; `plugin market sync` only exports JSON (default stdout, `--docs` writes to a directory) and no longer drives the website display. The docs-site marketplace page keeps only an introduction and links; browsing/downloading/installing all live on the website `/plugins`.

**Display, signing and stats**

- Optional catalog fields `screenshots` / `changelog_url` / `versions`: the detail page shows screenshots and version history; for old versions to be downloadable, `versions` must carry a `ref` (GitHub tag), and the website fetches the matching tag via `?version=`.
- Package signing: after the website process sets `NEXADMIN_MARKET_SIGN_KEY=<PKCS8 PEM private key>`, the download page offers `.sig` and the public key at `/plugins/signing-key.pub`; verify with `nexadmin plugin verify --sig <file> --key <pubkey file> <zip>`. The signature is **the marketplace instance's Ed25519 signature over the final package** (over the SHA-256 digest), not the author's; customer sites can use their own keys.

**Download integrity verification (before install, before extraction)**

Market installs hand the downloaded bytes straight to `go build` / `npm build`, i.e. they execute unverified source on the build host. `market.Download` therefore confirms the bytes against the catalog declaration **before extracting** (implementation: `core/plugin/market/verify.go`). Verification sources, in priority order:

| Source | When it applies | When no checksum is available |
| --- | --- | --- |
| Catalog field `checksum` (SHA-256 of the package zip, 64 hex digits; `versions[].checksum` overrides it for a pinned old version) | declared → must match | —— (a malformed value is refused outright) |
| Catalog field `checksum_url` (direct link to a `sha256sum`-format file, http(s)) | declared → must match | failing to fetch or parse it refuses the install |
| Website sidecar (`url` pointing at `<site>/plugins/<id>/download` automatically fetches the same path + `.sha256`) | fetched → must match; no manual field needed | a 404 sidecar counts as "no checksum" and falls back to the default policy |
| None of the three (GitHub `repo` zipball, plain direct link without a sidecar) | —— | default policy: install with an explicit warning; `NEXADMIN_MARKET_REQUIRE_CHECKSUM=1` turns it into a refusal |

- The default is "warn and continue" because a `repo` entry with an empty `ref` follows the default branch, whose bytes change on every push, so it cannot carry a pinned checksum; refusing outright would break every one-click install in existing deployments. Continuing is not "safe" — it only records honestly in the warning log that the package was never integrity-checked, instead of pretending verification passed. Strict deployments set `NEXADMIN_MARKET_REQUIRE_CHECKSUM=1`, add `checksum`/`checksum_url` to the entry, or route downloads through the website endpoint.
- A verification failure refuses the install: **no extraction, nothing written under `plugins/`, `plugins.go` untouched**, and the temporary zip is deleted on the spot. There is no bypass switch (the CLI's `--force`/`--allow-incompatible`/`--allow-modified` never affect integrity verification). Messages are localized through i18n (`plugin.market.checksum_*`, kept in sync in `core/i18n/locales/{zh-CN,en-US}.yaml`).
- SHA-256 only answers "are these the bytes I meant to check" — it guards against transport/cache substitution and against a catalog that no longer matches the package. When the checksum comes from the same download channel as the package, it **does not authenticate the publisher**; that remains the job of the Ed25519 signature (`.sig` plus a pinned public key). The two are complementary; do not treat a checksum as a signature.
- Producer and consumer stay separate: the website's own `/plugins/<id>/download` fetch (`market.Package`) does not verify anything — it is the side that *produces* the `.sha256`, so there is nothing to compare against. Verification happens only in the client-side `Download`.
- `nexadmin plugin install <local zip>` has no catalog entry to compare with, so the operator supplying the file owns its trustworthiness; verify it first with `nexadmin plugin verify`.

### Display, localization and statistics

- Market catalog entries use `i18n` maps to localize name/description (see the YAML comments above); category names, placeholder prices like `TBD/Free` and date formats come from the website's language dictionary — no need to invent a second convention in the plugin.
- Public downloads are counted per download; ratings are anonymous 1–5 stars, one row per IP per plugin, re-ratable or clearable (`score=0`). Admin one-click installs do not count as public downloads.
- Private/self-hosted GitHub: `NEXADMIN_MARKET_GITHUB_API` overrides the API root (GitHub Enterprise).

The website plugin owns the language list, dictionaries, per-language publishing and the docs/CMS split — plugin authors do not handle these. See the "region / languages" section of the Configuration reference and the pre-production verification section of the Deployment guide for details.

## 8. Submission and review

Attach a one-pager: feature introduction, screenshots, supported databases, verified and unverified parts, pricing suggestion. Review checks the list below item by item; failures are returned with reasons.

**Must pass**
- Manifest fields complete; `id`, permission codes, table names and API path prefixes follow the conventions.
- Migrations present for all three databases, append-only.
- No modification of `core/`, `web/src/framework/` or any built-in module; no imports of built-in modules or other plugins.
- Every endpoint has permission checks; tenant-scoped queries carry tenant conditions.
- No hard-coded secrets, no uploading user data to external services; outbound requests have SSRF protection and timeouts.
- Frontend: mocks optional, zh/en copy complete, no hard-coded colors, usable on mobile, `tsc` and `eslint` clean; real-API integration scope and results documented.
- Backend: `go build` and `go vet` pass.
- Dependencies: open-source with commercially usable licenses only, exact versions; no commercially licensed components.
- No built-in license validation, phone-home or backdoor code.

**Recommended**
- Provide upgrade notes: how to migrate to the new version, any incompatible changes.
- State known limitations plainly; do not overstate features.

## 9. Licensing and sales workflow

**Free plugins** (`edition: free`, e.g. cron / codegen / imports / totp / ldap / maps / crm) do not validate a license. Customers install the source, rebuild and restart; no `.lic` is needed.

**Pro plugins** (`edition: pro`):

1. The author submits the plugin package → after platform review it is listed in the plugin marketplace.
2. The buyer purchases → the platform signs a license file with its private key (bound to the plugin, customer name, expiry, optionally domains).
3. After the customer installs and builds the source, a platform super admin imports the license in Plugin Management. The manager tries to activate compiled plugins: `needRestart=true` means restart as instructed; `needRestart=false` means it is already live.
4. When the license expires the plugin automatically goes offline (routes return 403, state `expired`); after renewal, importing the new license restores it. A license revoked by the vendor is in state `revoked` (see §11).

**What does `needRestart` mean?** It means the plugin needs a restart to become fully available. Three common cases are registering pages at the site root (for example the website plugin), participating in startup-time authentication/identity extensions, or a `Starter.Start` error/panic that closes readiness. The framework will not mutate the live router just to claim "immediate activation", so it reports the restart honestly.

Authors do not need to — and should not — implement licensing themselves. Validation is centralized in the framework; plugin code must not contain any license logic.

> License validation guards against casual piracy and file sharing, not determined cracking — with source delivery, a capable buyer can remove the check. Real protection comes from contracts, upgrade services and support. Authors should put value into continuous updates and service.

## 10. FAQ


**Can a plugin be frontend-only or backend-only?** A backend-only plugin may omit `web/`, with `menus` in the manifest filled in as needed. Frontend-only packages are currently not supported by the installer: a missing `backend/` directory is refused outright (`plugin lint` blocks it too); for frontend-page-only changes use the framework's page-override mechanism.

**My plugin needs to change built-in features.** Not allowed. If the framework needs an extension point, file a framework request — do not work around it by modifying core or framework.

**Will framework upgrades break plugins?** Plugins depend only on the public interfaces of `core/` and `framework/`. The framework honors the ownership contract: incompatible changes to those interfaces are flagged in upgrade notes ahead of time and reflected in the `requires_nexadmin` version range.

**How do I release a new version?** Bump `version` in `plugin.yaml`, append new migration files, repackage and resubmit (`plugin pack`). Released migration files never change. Listing is maintained by the vendor in `config/plugin-market.yaml` (read directly by the website and admin; effective immediately); customers install with one click in the admin "Plugin Marketplace", and pro plugins are gated by the runtime license check.

**Anything special for multi-instance deployments?** There is no distributed lock available for plugins — background tasks must prevent duplicate execution themselves; the cron example uses optimistic database claiming (`UPDATE ... WHERE last_slot < ?` and checks affected rows) as a reference. Multi-instance requirements and known concurrency windows are governed by §10 of the Deployment & Operations Guide.

## 11. Known limitations (current version)

This section describes boundaries that are "just how the framework is today", not a to-do list. Skim it before writing a plugin to save a round of rework.

### Domain binding

The license's `domains` are compared against the site domain: exact match, or a `*.example.com` wildcard. The site domain comes from `site.domain` / `site.domains` / `sso.issuer`, falling back to the first request's `Host` when none is set; loopback addresses can be allowed via `license.allow_dev_hosts`.

On mismatch the plugin state is `invalid`, routes return 403, and the reason shows on the plugin management page. No `domains` in the license means no domain binding.

### License expiry

Expired licenses go offline automatically — no restart needed. A periodic admin check (`CheckEvery`, default hourly) refreshes the state on the plugin management page; there is also a real-time check before every plugin route is entered, so a slightly stale page state does not affect enforcement.

### Framework version range

`requires_nexadmin` is validated when the plugin mounts, and also by `nexadmin plugin install`. If unsatisfied, the plugin is not mounted (state `invalid` — even a valid license won't help).

- CLI installs can skip it with `--allow-incompatible`; the admin marketplace has no such switch.
- An empty value, or an unknown framework version (dev builds without a version number), passes through.
- An unparsable range counts as unsatisfied.

### Revocation and public keys

Licenses are **not** checked online — only offline revocation exists: the vendor uses `plugin revoke` (or export from the license admin) to produce a signed revocation list `config/license-revoked.json`, distributed with upgrade packages. After signature verification the framework marks licenses whose serial numbers appear in the list as `revoked` (403, reason shown on the plugin management page); file changes need no restart.

> [!WARNING]
> Offline revocation is an auxiliary measure, not strong protection: with source delivery, buyers can delete the list or remove the check, and old licenses without serial numbers cannot be individually revoked. What really works is shorter validity windows and timely renewal. If the private key leaks or wholesale revocation is needed, remove the corresponding public key from the key set (`config/license-keys.pub` or the built-in list) — all licenses signed by that key become invalid.

Multiple public keys are supported: the built-in key plus keys appended in `config/license-keys.pub` (one base64 key per line, `#` starts a comment). The license file's `kid` selects which key; old license files without `kid` try all keys in turn. To rotate keys, append the new key first and remove the old one after old licenses expire — no source changes needed. The built-in key is the development key and must be replaced before official release (`nexadmin release check` blocks on it).

### Runtime toggling: what takes effect immediately

**Starter readiness**: before `Start` finishes, plugin routes remain registered but are blocked by the gate with 503 and `Retry-After: 1`; `/plugins/active` and menus are unavailable. An error or panic in `Start` closes this run's readiness and sets `needRestart=true`, so the plugin is not considered enabled. This is plugin startup state, distinct from process-level `/readyz`.

**Takes effect immediately** (no restart):

- The plugin's own routes — `gate` / `admit` decide from runtime state; after disabling, requests return 403 with a reason
- Availability in `GET /plugins/active`
- Sidebar menu visibility — `MenuVisible` uses the same criterion as `/plugins/active`, so no entry is left that leads to a 403
- Process-local/cross-instance event subscriptions and asynchronous job handlers registered through the current run's `PluginRuntime` — revoked when the local node is disabled, with started jobs cancelled

This revokes runtime registrations; it is not plugin-code hot unloading and does not mean background workers restart automatically.

**Requires a restart**:

- Starter `Start` returned an error or panicked and closed this run's readiness
- Established WebSocket / SSE connections — guards are appended, not removed
- Service singletons created with `sync.Once` inside the plugin
- Background workers that must be relaunched after disabling, unless the Module implements `plugin.RestartableStarter`

The `RestartableStarter` contract: when `Start` returns it must provide `Done()`; after ctx cancellation all tasks confirm their exit before the framework calls `Start` again with a fresh ctx on re-enable.

**No official plugin implements it yet**, so a plugin with a worker that is disabled and then re-enabled requires a restart: re-enabling sets `needRestart=true`, and routes and menus stay unavailable in the meantime — the framework does not pretend it is alive.

The revocation above applies only to the current node. The operator overlay in multi-node deployments still does not sync automatically: the disabled list is a node-local file (`data/licenses/disabled.json`), so either toggle node by node or use the `disabled_plugins` config distributed with your config. `nexadmin plugin list` reads only disk and config, not this overlay.

### Hot mounting

A plugin's routes can mount immediately on license import only after it declares the "API-only" hot-mount contract. No official plugin declares it yet, so after importing a license go by the returned `needRestart` and restart if required.

Declaration: implement `plugin.RuntimeRoutes`'s `RequiresRootRoutes() bool` returning `false`, with all routes under `/api/v1/plugins/<id>/`. Hot-mounted `Dispatch` uses the same runtime gate as static routes: enabled state, licensing and Starter readiness must all pass; before readiness it likewise returns 503 with `Retry-After: 1`.

### Real-database verification scope

Plugin migrations, menu writes and hot-mounting are not yet fully verified against a real database. State your verification scope explicitly in the submission notes; do not substitute unit-test conclusions for it.
