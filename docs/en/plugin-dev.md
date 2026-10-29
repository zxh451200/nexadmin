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
    ├── cron.mock.ts          # frontend mock (required)
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

### 4.2 Background tasks (optional)

If you need a resident goroutine (a scheduler, a consumer), implement `plugin.Starter`:

```go
func (m *module) Start(ctx context.Context, d *core.Deps) error {
    go scheduler.Run(ctx) // must not block; ctx is cancelled at process exit
    return nil
}
```

`Start` is called synchronously and must return quickly. A returned error is only logged; it does not affect route mounting.

### 4.3 Conventions (violations are rejected in review)

- API paths: `/api/v1/plugins/<id>/...`.
- Table prefix: `plg_<id>_`, e.g. `plg_cron_job`.
- Permission code prefix: `<id>:`, e.g. `cron:list`. Every endpoint needs `core.Perm(...)`.
- You may only depend on `core/` and `pkg/`. **Do not modify core, do not modify framework, do not import built-in modules or other plugins.**
- Framework capabilities available in `core.Deps`: `DB`, `Cache`, `Auth`, `Log`, `Config`, the unified notify entry `Notify` (in-app / email / SMS / bot, nil-safe), cross-instance event broadcast `Bus` (nil-safe), shutdown cleanup registration `OnClose`, runtime API docs `APIDoc`, plus the `Public` route group (no login) and the super-admin-only `Admin` group. Do not query other modules' tables directly; if you truly must read a built-in table (like cron's `clean_logs`), use table-name constants and handle a missing table without panicking.
- Business primary keys use snowflake IDs (`pkg/idgen`), serialized as strings in APIs.
- Tenant isolation: tenant-scoped tables carry `tenant_id`, and queries must include the current tenant condition.
- Outbound requests (HTTP callbacks etc.) must guard against SSRF: internal and loopback addresses are forbidden, and timeouts are required. cron's `ssrf.go` is a ready reference.

### 4.4 Database migrations

Place them in `backend/migrations/<engine>/` — **mysql, mariadb and postgres must all be present**, named `001_init.sql`, `002_xxx.sql` and so on, **append-only** (a released migration never changes; changes go into new files). The runtime executes files in name order, records applied versions, and never re-executes.

**Migrations change structure only — no `INSERT`/`UPDATE`/`DELETE`.** The runner warns about every DML statement in `migrate --dry-run`. Seed data (menus, dictionaries, templates) belongs in a Seeder, and reconciling the menu shape of an existing install lives there too (see `modules/system/menu/reconcile.go`).

Watch the engine differences: auto-increment/primary-key types, `TEXT` defaults, boolean types, `ON UPDATE` syntax. cron's three SQL files are a ready comparison.

### 4.5 Tests

The backend needs at least one unit test covering your most error-prone pure logic (the cron example tests expression parsing, next-run calculation and SSRF blocking). If parts that need a real database cannot run in unit tests, say so in the submission notes.

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

### 5.2 Conventions

- **i18n**: the namespace is fixed to `plugin_<id>`, written in both `locales/zh-CN.json` and `en-US.json` with the top-level key `plugin_cron`. Dynamically composed keys (e.g. `` t(`plugin_cron.type.${v}`) ``) must have copy for every value.
- **Components**: use only `@/framework/components` and `@/framework/ui`, e.g. `DataTable`, `FilterBar`, `ResponsiveDialog`, `FormLayout`, `PageHeader`. Do not use `ui/table`, `ui/dialog`, `ui/sheet` or `ui/drawer` directly (the ESLint rule currently covers `src/pages/**` and built-in module pages, not `src/plugins/**`; plugins get no warning, so review checks this manually).
- **Colors**: semantic tokens only (`bg-card`, `text-muted-foreground`, `border-border`…). Never `#fff` or `bg-blue-500` — hard-coded colors break the dark theme (ESLint warns across `src/**`).
- **Layout**: mobile first; use logical classes (`ms-`, `me-`, `ps-`, `pe-`, `start-`, `end-`) instead of `ml-`, `pl-` so RTL languages also work. Touch targets are at least 40px on mobile.
- **Form controls**: prefer Select over Input wherever applicable (enums, dictionaries, foreign keys, booleans and time all have ready controls).
- **Buttons**: icon buttons must carry `aria-label` or `title`, which auto-generates hover hints.
- **Permissions**: gate button visibility with permission codes, same as built-in pages.
- **Dashboard cards (optional)**: export `cards: DashboardCard[]` from `web/cards.tsx` (types from `@/framework/modules/dashboard/types`); they appear on the dashboard after install and can be hidden and reordered by users. `id` uses `<plugin-id>-<meaning>` (only `[a-z0-9-_.]`, max 64), `title` is an i18n key under the `plugin_<id>` namespace, `perm` is the plugin's own permission code, and the component uses `React.lazy` and fetches its own data; the matching backend endpoint and mock are required too. See the cron plugin's `cron-overview`.

### 5.3 Mocks (required)

`web/<id>.mock.ts` uses `defineMock` / `createCrudMock` to serve every endpoint so the page demos fully without a backend. The framework's workflow is frontend-first; plugins without mocks do not pass review.

## 6. Local development workflow

1. Derive a development project using the [Quickstart](quickstart.md). Prepare the package structure outside that project, or use the free cron package you obtained as a reference. Give your own plugin a new ID, permission prefix and table prefix; do not write business code in the framework template. Replace example `nexadmin/...` imports with the derived project's actual Go module path.
2. Frontend: put `web/` into `web/src/plugins/<id>/`, run `npm run dev`, and debug the page against mock data.
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
go run ./cmd/nexadmin plugin install cron-1.0.0.zip
go build ./... && go vet ./... && go test ./plugins/<id>/...
cd web && npx tsc --noEmit && npx eslint src
```

Re-installing over an existing plugin uses `go run ./cmd/nexadmin plugin install --force <dir-or-zip>`; `--force` must come before the path (the install subcommand uses standard `flag.Parse` and does not parse options after positional args). To repackage an installed plugin as a release bundle: `go run ./cmd/nexadmin plugin pack [--out <path.zip>] <id>` (produces a zip of `plugin.yaml + backend/ + web/`, the inverse of install; `--out` also precedes positional args). `release pack` packages the framework itself. `plugin list` reads the on-disk manifest and offline licenses — it does not prove that the Go blank import, migrations and runtime mounting succeeded; also check `plugins/plugins.go`, the startup log and the runtime `GET /api/v1/plugins`.

The install command rejects zip-slip archives and caps the extracted file count and total size. Uninstall and disabling:

- `plugin remove <id>`: deletes `plugins/<id>/`, `web/src/plugins/<id>/` and removes the import from `plugins.go`; **data tables and the license file are kept** — after reinstall the data comes back as it was. Recompile and restart to take effect.
- `plugin remove --purge <id>`: on top of remove, connects to the database and cleans up — drops tables with the `plg_<id>_%` prefix (platform plugins clean the platform database; `migration_target: tenant` plugins clean each tenant business database) and deletes `sys_menu` rows by the manifest's menu codes; if the manifest is missing, tables are still cleaned by prefix and menus must be handled manually. The license file is kept. If the database cannot be cleaned the command aborts without deleting files, and can be retried.
- Temporarily disabling (no code deleted, no data touched): set `disabled_plugins: [<id>]` in `config/config.yaml`; after restart the plugin is not mounted at all, the plugin management page shows "disabled" and `plugin list` shows `disabled`. Free plugins use this for soft-disabling (pro plugins may too, no need to revoke the license). Removing it from the config and restarting restores the plugin.

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

### Website multi-language (website plugin)

**`sys_language` is the sole source of truth for platform languages.** The website consumes it through `core.Languages()`; `languages` in `config.yaml` only seeds an empty table. Do not maintain a separate `locales/languages.json` list.

1. A platform super admin maintains `code` (such as `ja-JP`), `name`, `short`, `dir`, `htmlLang`, `ogLocale`, `prefix`, `dateFormat` and the default language in System Settings → Advanced → Languages.
2. Website UI dictionaries live in `plugins/website/locales/<code>.json`, with matching keys across languages. These are embedded source assets: rebuild and deploy the binary after changes. Restarting an old binary cannot update its dictionaries.
3. Changes to the language set or URL prefixes affect routes registered at startup; the save response reports `needRestart`. Restart, then verify navigation, `hreflang`, `og:locale:alternate` and the sitemap.

**Maintain technical documentation only in `docs/`; use the website CMS for marketing content.** Markdown is embedded in the binary and requires a rebuild and deployment after edits. Do not duplicate technical documentation in the CMS or the retired VitePress site. Document paths use stable language `Code`, not the editable URL `prefix`: `zh-CN` reads the Chinese source files in `docs/`, English reads `docs/en/<slug>.md`, and other languages use the lowercase primary subtag of Code (e.g. `ja-JP` → `docs/ja/<slug>.md`). Translations carry `title` / `summary` frontmatter; missing translations fall back to the Chinese source, not a claim of completed translation. Changing the default language or URL prefix does not move files. News, product descriptions and marketing pages are edited, translated and published per language in the CMS without rebuilding for content changes.

Conventions:

- The default language (`default: true`, and its `prefix` must be empty) renders on prefix-less URLs; other languages render under `/<prefix>`. `?lang=<code>` switches and remembers cookie `nex_site_lang`; language-switch links keep current filter parameters.
- Without a language cookie, the default-language homepage shows a dismissible switch hint (based on `Accept-Language`) to visitors preferring other languages; **it never auto-302s**, avoiding duplicate search indexing, and responses carry `Vary: Accept-Language`.
- Admin content (site settings, navs, features, pages, posts) stores translations in each table's `i18n_json`: `{"en-US":{"title":"…"}}`; empty fields fall back to the source text on that language's pages. The backend only accepts configured languages and whitelisted fields, and rich-text translations are sanitized too.
- The editor uses **whole-form locale switching**: one dropdown at the top of the dialog decides which language you are editing, there is a single form instance, and **the number of languages never changes the form height**. In translation mode each field shows its source text underneath, rich text gets a collapsible read-only source panel, and locale-independent fields (slug, cover) are disabled and tagged "shared". The components live in `web/src/framework/components/i18n/` and are reusable from derived projects.
- The translatable field whitelist, length limits and widgets exist only in Go (`entityViews` in `plugins/website/schema.go`); the frontend reads them from `GET /plugins/website/i18n/schema/:entity`. `TestEntityViewsMatchWhitelist` catches drift between the two. Adding a translatable field means changing that one place — no UI code.
- The **algorithms** — parsing the translation document, sanitising, length clamping, completeness and per-language publishing — live in `pkg/i18n` (`Normalize` / `Translatable` / `NormalizePublish` / `Published`). They know nothing about specific content or where the language list comes from: implement `Locales` (two methods, `Resolve` / `All`) and you can reuse them. Derived projects adding their own translatable content should use `pkg/i18n` plus `web/src/framework/components/i18n` rather than inventing a second JSON convention. The UI-string catalogue (`core/i18n` + the i18n-editor plugin) is a different thing — do not merge the two.
- Posts and pages support **per-language publishing** (`published_json`, non-default languages only): a language with missing translations cannot be published — the server downgrades it rather than trusting the client's completeness claim — and its direct URL returns 404 while staying out of that language's listings, `hreflang` and the sitemap. An empty column means publishing has never been configured per language, so every language follows the row's `published` flag and an upgrade changes no existing visibility. Navs, features and settings have no URL to take offline and therefore expose no such switch.
- Batch machine translation goes through a plugin-owned route, `POST /plugins/website/i18n/translate/<entity>` (each entity keeps its own edit permission instead of teaching `modules/ai` about plugin codes), reuses the `core/ai` gateway, and only fills fields that are still blank.
- Market catalog entries use `i18n` maps to localize name/description (see the YAML comments above); category names, placeholder prices like `TBD/Free`, and date formats come from the language dictionary.
- Upgraded legacy databases: `ensureSeedI18n` only backfills rows whose `i18n_json` is empty and whose source text still matches the factory copy; existing translations are never overwritten.
- Stats: public downloads are counted per download; ratings are anonymous 1–5 stars, one row per IP per plugin, re-ratable. Admin one-click installs do not count as public downloads.
- Private/self-hosted GitHub: `NEXADMIN_MARKET_GITHUB_API` overrides the API root (GitHub Enterprise).

## 8. Submission and review

Attach a one-pager: feature introduction, screenshots, supported databases, verified and unverified parts, pricing suggestion. Review checks the list below item by item; failures are returned with reasons.

**Must pass**
- Manifest fields complete; `id`, permission codes, table names and API path prefixes follow the conventions.
- Migrations present for all three databases, append-only.
- No modification of `core/`, `web/src/framework/` or any built-in module; no imports of built-in modules or other plugins.
- Every endpoint has permission checks; tenant-scoped queries carry tenant conditions.
- No hard-coded secrets, no uploading user data to external services; outbound requests have SSRF protection and timeouts.
- Frontend: mock present, zh/en copy complete, no hard-coded colors, usable on mobile, `tsc` and `eslint` clean.
- Backend: `go build`, `go vet`, `go test` pass.
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

**What does `needRestart` mean?** It means the plugin needs a restart to become fully available. This is typical when a plugin registers pages at the site root (for example the website plugin) or participates in startup-time authentication/identity extensions. The framework will not mutate the live router just to claim "immediate activation", so it reports the restart honestly.

Authors do not need to — and should not — implement licensing themselves. Validation is centralized in the framework; plugin code must not contain any license logic.

> License validation guards against casual piracy and file sharing, not determined cracking — with source delivery, a capable buyer can remove the check. Real protection comes from contracts, upgrade services and support. Authors should put value into continuous updates and service.

## 10. Revenue share and pricing (draft, not an offer)

> **All prices and percentages below are structural examples used to fix the terms format. They are not an offer or a commitment.** The vendor publishes formal pricing, refund, revenue-share and IP terms before listing; until then the published purchase page prevails, and these numbers must not be quoted to customers.

**Pricing model**: per instance, annual subscription, including framework-compatible updates and technical support for that year. Multi-tenant, source buy-out and OEM distribution are quoted separately.

**Tier examples**

| Tier | Typical characteristics | Example price (CNY / instance / year) |
| --- | --- | --- |
| Tool | A single assistive capability, no external service dependency | 486 |
| Integration | Connects to external systems or includes an admin console | 1,094 |
| Suite | Multi-role collaboration with a complete business flow | 1,742 |

Per-plugin example list prices live in the vendor's purchase-page draft. Tier prices serve pricing discussions; list prices serve display, so the two are not interchangeable.

**Terms the vendor must confirm**

- Author/platform revenue share, settlement cycle and method (placeholder: `[TBD: __%]`).
- Refund and dispute rules (placeholder: `[TBD: __ days]`).
- Author update obligations, e.g. adaptation deadlines after framework major versions and the consequences of missing them.
- Whether buyers keep using and receiving updates after an author delists a plugin.
- IP ownership and license terms of plugins.

**Version and price changes**: issued licenses keep their price for the paid term; renewals use the price published at renewal time. Increases do not apply retroactively.

## 11. FAQ

**Can a plugin be frontend-only or backend-only?** Yes. A frontend-only plugin has no `backend/`, a backend-only plugin has no `web/`, and `menus` in the manifest is filled in as needed.

**My plugin needs to change built-in features.** Not allowed. If the framework needs an extension point, file a framework request — do not work around it by modifying core or framework.

**Will framework upgrades break plugins?** Plugins depend only on the public interfaces of `core/` and `framework/`. The framework honors the ownership contract: incompatible changes to those interfaces are flagged in upgrade notes ahead of time and reflected in the `requires_nexadmin` version range.

**How do I release a new version?** Bump `version` in `plugin.yaml`, append new migration files, repackage and resubmit (`plugin pack`). Released migration files never change. Listing is maintained by the vendor in `config/plugin-market.yaml` (read directly by the website and admin; effective immediately); customers install with one click in the admin "Plugin Marketplace", and pro plugins are gated by the runtime license check.

**Anything special for multi-instance deployments?** There is no distributed lock available for plugins — background tasks must prevent duplicate execution themselves; the cron example uses optimistic database claiming (`UPDATE ... WHERE last_slot < ?` and checks affected rows) as a reference. Multi-instance requirements and known concurrency windows are governed by §10 of the Deployment & Operations Guide.

**Known limitations (current framework version)**
- Domain validation is implemented: `domains` in the license is compared against the current site domain (exact or `*.example.com` wildcard; the site domain comes from `site.domain` / `site.domains` / `sso.issuer`, falling back to the first request's `Host` when none is set; `license.allow_dev_hosts` allows loopback addresses). On mismatch the plugin state is `invalid`, routes return 403, and the reason shows on the plugin management page. No `domains` in the license means no domain binding.
- Expired licenses go offline automatically: a periodic admin check (`CheckEvery`, default hourly) plus a check before each plugin request marks expired plugins `expired` with routes returning 403 — no restart needed; the periodic check only refreshes the plugin management page, the request path is checked in real time.
- `requires_nexadmin` is validated: when mounting (and in `nexadmin plugin install`), the framework version is checked against it; unsatisfied means no mounting (state `invalid`, even a valid license won't help), and `plugin install --force` skips it. Empty values, or an unknown framework version (dev builds without a version), pass through; an unparsable range counts as unsatisfied.
- Licenses are not checked online — only offline revocation exists: the vendor signs a revocation list `config/license-revoked.json` via `plugin revoke` (or export from the license admin) and distributes it with upgrade packages; after signature verification the framework marks licenses whose serial numbers appear in the list as `revoked` (routes return 403, reason shown on the plugin management page); file changes need no restart. This is an auxiliary measure, not strong protection: with source delivery, buyers can delete the list or remove the check. Old licenses without serial numbers cannot be individually revoked. Keep license validity windows short and renew on time; if the vendor's private key leaks or wholesale revocation is needed, remove the corresponding public key from the key set (`config/license-keys.pub` or the built-in list) — all licenses signed by that key become invalid.
- Multiple public keys are supported: validation uses the built-in key plus appended keys from `config/license-keys.pub` (one base64 per line, `#` starts a comment); the license file's `kid` selects which key (old license files without `kid` try all keys in turn). To rotate keys, append the new key first and remove the old one after old licenses expire — no source changes needed. The built-in key is the development key and must be replaced before official release (`nexadmin release check` blocks on it).
- A plugin's routes can mount immediately on license import only after it declares the API-only hot-mount contract. The official plugins do not each declare it yet, so rely on the returned `needRestart` and restart when required. The declaration implements `plugin.RuntimeRoutes` with `RequiresRootRoutes() bool` returning `false`, and all routes must live under `/api/v1/plugins/<id>/`.
- Plugin migrations, menu writes and hot-mounting are not fully verified against real databases yet — state your verification scope in the submission notes.
