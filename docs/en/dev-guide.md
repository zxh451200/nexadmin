# NexAdmin Secondary Development Guide

This guide is for teams and AI agents doing secondary development (BI, digital twins, and similar products) on NexAdmin. Goal: develop without touching the framework, so that `nexadmin upgrade` later applies painlessly.
This document is updated with each framework release — do not hand-edit it; keep your business docs elsewhere. The condensed rules for AI agents live in `AGENTS.md` at the project root (`CLAUDE.md` is a copy of it).

## 1. The full workflow

```
nexadmin new → develop (user area only) → git commit → nexadmin upgrade --dry-run → upgrade → build & test
```

1. Derive a project: `go run ./cmd/nexadmin new myapp --out ../myapp --module example.com/myapp`. The output is the framework source plus empty business directories (`internal/modules`, `web/src/pages`, `migrations/business`) and `AGENTS.md`/`CLAUDE.md`.
2. Develop: `go run ./cmd/nexadmin add module article --fields "title:string,status:dict(sys_status)"` generates a skeleton, then fill in business logic. Backend: `go run ./cmd/server`; frontend: `cd web && npm install && npm run dev`.
3. Upgrade: `git commit` first; get a release package (artifact of `nexadmin release pack`) or the new source directory/zip; `nexadmin upgrade --from <path> --dry-run` to review; drop `--dry-run` to upgrade for real; run `go mod tidy`, `cd web && npm install`; build and test; check `*.bak` files and `WARN` lines in the output.

## 2. Three classes of ownership

The project root `.nexadmin.manifest.json` is authoritative (`framework_paths` / `user_paths`). The table below is the convention:

| Class | Paths | On upgrade | What you may do |
| --- | --- | --- | --- |
| Framework area | `core/` `modules/` `pkg/` `cmd/` `web/src/framework/` `migrations/framework/` `docs/` `deploy/` `scripts/` `config/default.yaml` `config/license-keys.pub` `config/license-revoked.json` | Overwritten (the last two are distributed by the vendor with release packages; kept if absent from the package) | Do not modify. Hand-edited files are saved as `.bak` with a WARN on upgrade |
| Merge area | `go.mod` `go.sum` `web/package.json` | Merged: framework entries override same-named ones; your own additions are kept | Add your own dependencies (exact versions); do not change existing framework entries (they will be reverted) |
| Lock files | `web/package-lock.json` | Untouched | Run `go mod tidy` and `cd web && npm install` after upgrading to regenerate |
| User area | `internal/` `plugins/` `web/src/pages/` `web/src/plugins/` `web/src/locales/` `web/src/theme.css` `config/config.yaml` `config/locales/` `migrations/business/` `data/` | Untouched | All business code, configuration, translations, migrations |

The revocation list (`config/license-revoked.json`) and additional public keys (`config/license-keys.pub`) are distributed by the vendor with release packages and overwritten as framework files on upgrade; the revocation list has sequence-number rollback protection (a signed list whose seq is lower than the currently trusted version is ignored with a warning; this guard is in-process only — after a restart the on-disk file wins). When the upgrade package does not contain these two files they are kept and not moved into `.nexadmin-trash/`; the `--dry-run` listing marks them `K`.

Under `web/`, the following also belong to the framework area (per the manifest) and must not be modified: `src/App.tsx`, `src/main.tsx`, `src/index.css`, `src/lib/`, `src/vite-env.d.ts`, `public/`, `.env.production`, `.env.development`, `embed.go`, `embed_dev.go`, `vite.config.ts`, `tailwind.config.ts`, `postcss.config.js`, `tsconfig.json`, `eslint.config.js`, `index.html`. Meanwhile `.github/`, `AGENTS.md`, `CLAUDE.md`, `README.md`, `.gitignore`, `.dockerignore` and `.dev-keys/` belong to the user area.

## 3. Customization paths: how to do it, minimal examples, verified conclusions

Conclusion labels: verified = code read or path exercised; patched = implemented; unsupported = file a framework request.

Decision order: configuration → extension points → events/hooks → override pages / disable-and-replace modules → file a framework request. Never hand-edit the framework area.

### 3.1 Replace a built-in module on the backend (verified)

Disable the original module in `config/config.yaml` via `disabled_modules`, and provide a replacement module with a **different `Name()`** in `internal/modules/`, keeping the same routes and permission codes.

- Registering the same method+path from two modules makes Gin panic while assembling routes, so the original module must be disabled first.
- A built-in module and a business module sharing a name fails startup with `core: duplicate module name` (not a panic); a replacement module named the same as the disabled module is disabled together with it.
- Both behaviors are covered by `core/modules_test.go`.

```yaml
# config/config.yaml
disabled_modules: [post]
```
```go
// internal/modules/mypost/module.go (the built-in post module serves /posts with permission codes post:list etc.)
func init() { core.RegisterBuiltin(module{}) }
func (module) Name() string { return "mypost" } // must not be "post"
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	r.GET("/posts", core.Perm("post:list"), list(d.DB)) // reuse the permission codes; existing role grants keep working
}
```

Trade-off: once replaced, that module no longer upgrades with the framework. The disabled module's menu rows remain in the database (data); the page needs overriding too, see 3.2.

### 3.2 Override a built-in frontend page (verified, unit-tested)

Export the same `path` as a built-in page from `web/src/pages/<any-dir>/route.tsx`. Merge order: framework → plugins → user `pages`; for the same path the later one wins, regardless of file name order.

- Implemented in `web/src/framework/router/merge.ts` (called by `discover.ts`); test: `cd web && node --test src/framework/router/merge.test.ts`, plus `npx tsc --noEmit`.
- The whole page is overridden, including menu icon/title/order taken from your declaration.

```tsx
// web/src/pages/my-post/route.tsx (the built-in post page path is system/post)
export const routes: ModuleRoute[] = [
  { path: 'system/post', component: lazy(() => import('./MyPostPage')), perm: 'post:list' },
]
```

### 3.3 i18n (verified, deterministic order)

Frontend: put the same keys into `web/src/locales/zh-CN.json` and `en-US.json`. Backend copy: write `config/locales/<lang>.yaml`.

- Frontend merge order: `web/src/framework/**` → `web/src/plugins/**` → the rest (`web/src/locales`, `pages/**/locales`); for the same key the later one wins. The order used to depend on Vite glob file order which let plugins override user translations; it is now explicitly sorted in `i18n/init.ts`.
- Backend `core/i18n.New` loads the built-in dictionaries first, then overrides with same-name keys from `config/locales/*.yaml`.

```json
{ "menu": { "post": "Post Management" } }
```

### 3.4 Menus (verified)

- Menus for secondary-development modules are written by the seed generated with `add module`, topped up on restart.
- Renaming/hiding/reordering built-in menus: change them in the admin "Menu Management" — they are data. The factory seed rule is "insert by business key only when absent" (`core/seed/seed.go` `Menus`), so upgrades never overwrite your changes.
- Menus are hard-deleted; the next startup re-inserts them by business key (the menu table has no soft delete). To hide one, disable it or mark it hidden — do not delete.
- The sidebar only shows menus that have a matching frontend route; hidden/disabled/unpermitted nodes never appear (`buildMenuFromServer`).

### 3.5 Theme & branding (verified)

- Brand color: write only the variables you want to change into the `:root {}` of `web/src/theme.css` — it is imported last by `index.css` and overrides `framework/theme/tokens.css`. Dark-theme variables go into `.dark {}`.
- Site name and logo: set them in the admin "System Settings" (`siteName`, `logoUrl` stored in the database) — no code changes.

```css
:root { --primary: 160 84% 39%; } /* in tokens.css --primary is an H S% L% triple */
```

### 3.6 Configuration (verified)

Only write the keys you want to change into `config/config.yaml`; `config/default.yaml` is overwritten on upgrade. `core/config.Load` reads defaults first, then merges the user file, and `NEXADMIN_*` environment variables take the highest priority. Maps merge key by key; lists are replaced wholesale (`disabled_modules`, `languages` must be written in full).

```yaml
ratelimit:
  login: { limit: 5, window: 1m }   # change only the login rate limit; the rest inherits defaults
```

### 3.7 Backend extension points (one by one)

| Need | How, with a minimal example | Status |
| --- | --- | --- |
| Event subscription | `event.Subscribe(event.SubscriberFunc(func(e event.Event){ if e.Name == event.UserCreated { ... } }))`, written in your module's `Register`. Handlers run serially on a dispatch goroutine — be fast; the queue drops events when full | verified |
| Async jobs | `job.Register("report.export", handler)`; submit with `job.Default().Submit(ctx, job.Submit{Type: ..., Owner: uid})`; re-registering the same type overwrites the previous handler | verified |
| Operation logs | Attach `logmod.Default().Middleware("Article Management", "create")` to write routes, package `nexadmin/modules/log`. Business module `init` runs before built-in modules, so take `Default()` at request time — see `modules/system/backup/handler.go` `audit` | verified |
| Permission points | `core.Perm("article:list")` registers and enforces; prefix with your module name. New permission points are granted to the super admin by default; other roles get them via "Role Management" | verified |
| Rate limiting | `middleware.RateLimit(d.Cache, d.Log, "my-scope", middleware.ByUser(), 60, time.Minute)`, 429 on exceed; passes through on cache failure | verified |
| Unified responses & errors | `response.OK / Page / Fail`; errors: `response.BadRequest("biz.title_empty")`, the key's copy lives in `config/locales/<lang>.yaml`; a missing key is returned verbatim | verified |
| Data scope / role assignment checks | `nexadmin/core/datascope` (clips queries by the operator's data scope, `Resolve` fetches the scope) and `nexadmin/core/roleassign` (three checks for role assignment) are public packages for business modules; usage: `modules/notice/scope.go`, `modules/system/user/roleguard.go` | verified |
| Custom tenant resolution / domain mapping / login providers | Return `core.WithTenantResolver(...)` etc. from `Options()` in the user area's `internal/boot/boot.go`; `cmd/server/main.go` (framework area) appends them — no need to touch main.go | verified |
| Custom login method | Implement `auth.Provider` in a module and `core` collects it automatically (the first one wins) | verified |

### 3.8 Do the frontend ESLint rules apply in derived projects (partial)

`web/eslint.config.js` ships with the project; `npx eslint src` applies to `src/pages/**`: importing `@/framework/ui/table|dialog|sheet|drawer` directly, hard-coded hex colors or scale classes like `bg-blue-500` all produce warnings. They are all `warn` and never fail the build; `web/src/plugins/**` is outside the "no direct primitive imports" rule. To make warnings a gate, run `npx eslint src --max-warnings 0` in CI, or require AI agents to clear them.

### 3.9 Add a card to the dashboard (verified, unit-tested)

Put a `cards.tsx` in any module directory exporting `cards: DashboardCard[]` — the dashboard discovers it automatically, **no need to edit the framework's dashboard page**. Discovery covers `web/src/framework/modules/*/cards.tsx`, `web/src/plugins/*/cards.tsx`, `web/src/pages/**/cards.tsx` (missing or empty directories are not an error). Derived projects put cards in `web/src/pages/<module>/cards.tsx`.

```tsx
// web/src/pages/my-order/cards.tsx
import { lazy } from 'react'
import type { DashboardCard } from '@/framework/modules/dashboard/types'

export const cards: DashboardCard[] = [
  {
    id: 'my-order-today', // globally unique; suggested shape "module-meaning"
    title: 'myOrder.card.today', // i18n key, also shown when editing the layout
    perm: 'myOrder:list', // without a permission code the card is visible to all logged-in users
    size: 'sm',
    order: 300,
    component: lazy(() => import('./TodayOrdersCard')), // the card fetches its own data (react-query)
  },
]
```

Rules:
- Fields: `id` (max 64, only `[a-z0-9-_.]`, must start with a letter or digit; invalid cards are ignored), `title` (i18n key), `perm?`, `size` (`sm`/`md`/`lg`/`full`: from desktop md these occupy 1/4, 1/2, 3/4, full row; mobile is always full row), `order?` (default order, smaller first; unset sorts after set values), `component` (use `React.lazy`), `defaultHidden?` (new cards start hidden).
- Overriding: for the same `id`, precedence is framework < plugins < user pages (same as routes); declare a built-in card's id (e.g. `dashboard-stats`) inside `pages` to replace it. Implemented in `web/src/framework/modules/dashboard/merge-cards.ts`.
- Permissions: filtered by current permissions (`checkPerm`, same as route `perm`); cards without permission neither display nor appear in "Edit Layout".
- Personal layout: users drag / move up-down / hide cards in "Edit Layout"; layouts are saved per user to the backend (`GET/PUT/DELETE /dashboard/layout`, table `sys_dashboard_layout`, own rows only). Merge rules in `mergeLayout` (`dashboard/layout.ts`): saved order kept for ids that still exist; gone ids dropped; new cards (from upgrades or newly installed plugins) appended at the end ordered by `order`; `defaultHidden` new cards start hidden. The server does not validate that ids exist.
- Unit tests: `cd web && node --test src/framework/modules/dashboard/*.test.ts`.
- This release does not provide free placement/resizing; `add module` does not generate `cards.tsx` — write it by hand when needed.

## 4. Common conflict scenarios and fixes

| Scenario | Symptom | Fix |
| --- | --- | --- |
| You edited a framework file | Upgrade prints `WARN: hand-edited`, old content in `<file>.bak` | Diff `.bak` against the new file, move your change into the user area (the extension points in §3, an override page, or a replacement module), then delete `.bak`; do not copy old content back into framework files |
| `go.mod` / `package.json` conflicts | Dependency versions differ or stray entries appear after upgrade | Framework entries win; re-add your own additions; run `go mod tidy`, `npm install`; use exact versions for your dependencies and avoid duplicating framework entries |
| Menus & permissions | A new menu is only visible to the super admin; a deleted built-in menu reappears | Assign new menus in "Role Management"; hide/disable built-in menus instead of deleting (the seed re-inserts by business key) |
| Migration checksum error | Startup refuses, saying an executed migration's content changed | Restore that file and put the structural change into the next numbered new SQL. After fixing the database manually, mark it with `nexadmin-server migrate --mark-applied <scope>/<file>` (check state with `migrate --status` first) |
| Plugin and business module name clash | Startup reports `core: duplicate module name "xxx"` | Rename the business module's `Name()` (and its permission-code prefix and table prefix); plugin tables are prefixed `plg_<id>_`, business tables use `biz_` |
| Business table clashes with framework | Table name collides with `sys_` / `plg_` | Always prefix business tables with `biz_` |
| Build fails after deleting the example module | A dangling import remains in `internal/modules/modules.go` | Delete the directory and remove that `_ ".../internal/modules/example"` line |

## 5. How to use AI agents

1. Hand the agent the project root `AGENTS.md` (Claude Code reads `CLAUDE.md`, same content). Before developing, have it read `.nexadmin.manifest.json` and §3 of this guide.
2. When dispatching sub-agents, put the fixed prefix from `AGENTS.md` §9 verbatim at the start of each task prompt (sub-agents lack the main session's context; without it they forget).
3. Require agents to stop and report when "the framework lacks a capability" instead of hand-editing the framework area.
4. At the end of each task have it run: `go build ./... && go vet ./...`, and for the frontend `cd web && npx tsc --noEmit`.
5. Before and after upgrades, have the agent confirm with `git diff --stat` that only the expected directories changed.

The fixed prefix (identical to `AGENTS.md` §9):

```
You are working in a NexAdmin derived project. Directory ownership: framework area (core/ modules/ pkg/ cmd/ web/src/framework/ migrations/framework/ docs/ deploy/ config/default.yaml) must never be modified;
go.mod, go.sum, web/package.json may only gain your own dependencies (exact versions), never change existing entries; all other code goes only into internal/, web/src/pages/, web/src/locales/, web/src/theme.css, config/config.yaml, migrations/business/.
When the framework lacks a capability, stop and report the gap — do not hand-edit the framework area. Backend modules run core.RegisterBuiltin in their package init(), never touch main.go; business tables are prefixed biz_, migrations are append-only; only use the nexadmin commands listed in AGENTS.md.
Frontend: build on the framework page shells (AppLayout/DataTable/FormLayout/ResponsiveDialog), mobile first; use semantic color tokens only; use logical classes (ms-/me-/ps-/pe-/text-start) for direction; copy goes into both web/src/locales zh-CN and en-US; write a *.mock.ts first so pages run before wiring APIs.
Definition of done: go build ./..., go vet ./..., cd web && npx tsc --noEmit pass; the report lists added/changed files.
```
