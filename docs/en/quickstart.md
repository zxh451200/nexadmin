---
title: Quickstart
summary: Derive a project, build and initialize it, develop against real APIs, verify a free plugin, and manage service lifecycle.
---

# NexAdmin Quickstart

Start business development in a derived project, **not in the framework template**. Except for step 1, run commands from the derived project root. Prerequisites: Go 1.26+, Node 22 with npm, and a reachable empty MySQL / MariaDB / PostgreSQL database. Initial dependency installation needs network access.

## 1. Derive your project

From the framework template root (Linux / macOS / Git Bash):

```bash
go run ./cmd/nexadmin new myapp --out ../myapp --module example.com/myapp
cd ../myapp
```

Replace the name, output directory and Go module path. The destination must be absent or empty and outside the template directory. By default, this neither initializes Git nor commits, and **includes no plugins**. Do not add `--with-plugins` just to start business development with the template's development plugins and state.

Read the generated `AGENTS.md` and `.nexadmin.manifest.json`. Put backend business code in `internal/`, pages in `web/src/pages/`, migrations in `migrations/business/`, and configuration overrides in `config/config.yaml`. See the [Customization Guide](dev-guide.md) for upgrade boundaries.

## 2. Build a server that serves the setup wizard

```bash
sh scripts/build.sh
./bin/nexadmin-server start
```

The script runs `npm ci`, builds the frontend, then compiles with `-tags embedweb` into `bin/nexadmin-server`. In a native Windows terminal use these commands instead (not Bash syntax):

```bat
scripts\build.bat
bin\nexadmin-server.exe start
```

Start from the project root, not inside `bin/`: configuration, migrations, plugin manifests and data paths depend on it. `go run ./cmd/server` does not embed the admin SPA and cannot serve the wizard page on port 8080 by itself.

## 3. Initialize and sign in

Open `http://localhost:8080/admin/setup` (use the actual configured port shown at startup). The six steps are **Environment → Database → Cache → Administrator & Site → Optional Settings → Execute**.

- Enter and test real database credentials. Memory cache suits a single-instance trial; multiple instances require shared Redis / Valkey.
- Create your own super-admin account with a strong password. There is no universal default login password. S3, AI and other optional settings can be left empty; unconfigured integrations are not verified integrations.
- Execution writes configuration, migrations, seeds, the administrator and the initialization marker. Sign in at `/admin/login` with the account you created, then check the dashboard, users and roles.
- Initialization allows direct local access by default; proxy forwarding does not count as local. Remote setup requires explicit permission and the one-time console token, or use `init --config` as described in the [Deployment Guide](deploy.md). Do not expose an uninitialized service publicly for convenience.

Initialization creates demo data; review and remove it as appropriate before production. Reserve platform-global management (settings, languages, global menus, plugin installation and licensing, backups, tenants and packages) for platform super admins. Do not grant these administration permissions to ordinary tenant roles. Before release, also verify that direct API calls by non-super-admins are rejected; hidden menus alone are not protection.

## 4. Develop: mocks versus real APIs

Stop the background server from step 2 to avoid a port conflict:

```bash
./bin/nexadmin-server stop
go run ./cmd/server
```

In another terminal at the same project root:

```bash
VITE_MOCK=false npm --prefix web run dev
```

Open `http://localhost:5173/admin/`. Vite proxies `/api` to port 8080; this uses the real database and your setup account. In PowerShell use `$env:VITE_MOCK='false'; npm --prefix web run dev`.

**Default difference:** both the template and a default derived project have `VITE_MOCK=true` in `web/.env.development`. Plain `npm --prefix web run dev` uses mock APIs for frontend-first development, not proof of database, permission or plugin behavior. Only the explicit value `true` enables mocks. Production `.env.production` sets `false`; do not override it to `true` while building. Real-API development does not require editing the framework-owned `.env.development`.

Generate a business skeleton, then implement its logic:

```bash
go run ./cmd/nexadmin add module article --fields "title:string,status:dict(sys_status)"
```

Restart the development backend after adding modules and handle new migrations according to your migration policy. Development checks:

```bash
go build ./... && go vet ./... && go test ./...
(cd web && npx tsc --noEmit && npx eslint src)
```

## 5. Obtain and verify a free plugin

Base services are free, optional general-purpose plugins are free, and business enhancements are paid. The initial free plan covers **cron / codegen / imports / totp / ldap / maps / crm**; other plugins are planned as pro. This plan does not prove that a current package has completed conversion or is mounted: check that the catalog, packaged `plugin.yaml` and Go registration agree on `Edition`. If a package still says pro, obtain the vendor's released free version instead of changing license code, using `--force`, or signing a test license as a substitute for free-edition verification. LDAP / TOTP are not compiled and mounted merely because their manifests exist.

For cron, obtain a **released free cron install package** (`plugin.yaml + backend/ + web/`) from the vendor's marketplace or standalone plugin source repository. Framework release packages and default `new` projects do not include cron; never assume the example ships in the framework package. Save the download as `cron.zip` in the derived project root:

```bash
# Stop the development backend with Ctrl+C and stop any other server processes first
go run ./cmd/nexadmin plugin install ./cron.zip
go run ./cmd/nexadmin plugin list
sh scripts/build.sh
./bin/nexadmin-server start
```

Verify each layer, not just the install command's exit status:

1. `plugins/cron/plugin.yaml` declares `edition: free`; `plugins/plugins.go` contains its blank import; frontend files exist in `web/src/plugins/cron/`.
2. The offline `plugin list` includes cron. Free plugins show `-` in the `LICENSE` column, not `active`.
3. The build succeeds; startup logs show no migration or plugin-start errors. After initialization, inspect migration state with `./bin/nexadmin-server migrate --status`.
4. Sign in as super admin against the real server at `/admin/system/plugin`. Runtime `GET /api/v1/plugins` must show cron as `active` with `edition: free`. Open its task page and inspect the list and execution log of a safe test task.
5. Grant business-use permissions minimally by role; do not delegate plugin administration to ordinary tenants. Mock pages, disk manifests and even runtime `active` alone do not prove task execution, real-database or external-service acceptance.

The admin marketplace also offers installation, but requires a writable source tree, Go / Node, network access, rebuilding and restarting. A binary-only deployment should install plugins in the build environment and redeploy.

## 6. Licensing, disabling and service lifecycle

Free plugins need no `.lic`. For pro plugins, a platform super admin imports a formal license in Plugin Management; the file is stored at `data/licenses/<id>.lic`. Source installation still needs a build. License import only attempts to activate already compiled plugins. Inspect `needRestart`, status and logs: failed hot mounting, website root routes or startup-time authentication extensions may require a restart. Not every license can be hot-activated. Domain, expiry and framework-version requirements must also match.

**Formal purchase terms have not been published.** This guide sets no prices, refund rules, revenue shares or IP terms. Free/pro classification is not a legal source-license grant; obtain formal terms and the delivery scope before purchasing.

```bash
./bin/nexadmin-server status
./bin/nexadmin-server stop
./bin/nexadmin-server start
./bin/nexadmin-server restart
```

These commands manage processes launched by `start`. Stop foreground `go run` with Ctrl+C, and manage systemd / containers through their supervisors. On Windows use `bin\nexadmin-server.exe`.

To temporarily disable a plugin, add its ID to the complete `disabled_plugins` list in user configuration `config/config.yaml`, then restart. Remove the ID and restart to restore it. `plugin remove <id>` deletes source and the registration import, requiring a rebuild and restart; database tables and license files remain. Do not use the table-dropping `--purge` option for ordinary disabling.

## 7. Documentation and release checks

Technical documentation has one source of truth: this repository's `docs/`, including `docs/en/`. The website documentation section renders embedded Markdown; edits require rebuilding and redeploying. Product descriptions, news and marketing pages belong in the website CMS, not a second copy of technical documentation. The website plugin is not installed by default in derived projects.

Languages come from `sys_language`. Source dictionaries and Markdown require rebuilds; changes to the language set or URL route prefixes require a restart. Documents follow stable language Code: `zh-CN` uses Chinese source files, English uses `en/`, and other languages use the lowercase primary subtag, not an editable URL prefix.

Before production, follow the [Deployment Guide](deploy.md) to validate real database migrations, backup and restore, permissions and tenant isolation, external integrations and the chosen deployment method. Builds and unit tests do not replace these checks. See [Features & Permissions](features.md) for APIs and permissions, and the [Plugin Development Guide](plugin-dev.md) for package structure.
