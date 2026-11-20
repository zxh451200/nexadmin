---
title: Quickstart
summary: Derive a project, build and initialize it, develop against real APIs, verify a free plugin, and manage service lifecycle.
---

# NexAdmin Quickstart

Start business development in a derived project, **not in the framework template**. Except for step 1, run commands from the derived project root. Prerequisites: Go 1.27.3+, Node 22 with npm, and a reachable empty MySQL / MariaDB / PostgreSQL database. Initial dependency installation needs network access.

By the end you will have a locally running admin console you can log into, an initialized real database, and one verified free plugin.

![First run in four steps: derive, build, initialize, sign in](/website-assets/docs/quickstart-flow.en.svg)

## 1. Derive your project

From the framework template root (Linux / macOS / Git Bash):

```bash
go run ./cmd/nexadmin new myapp --out ../myapp --module example.com/myapp
cd ../myapp
```

Replace the name, output directory and Go module path. The destination must be absent or empty and outside the template directory. By default this neither initializes Git nor commits, and includes **no plugins other than `totp`** (two-factor, part of the security baseline); pass `--without-totp` if you do not need MFA.

Three ways to pick plugins:

| Flag | What is carried over |
| --- | --- |
| default | `totp` only |
| `--plugins cron,codegen` | named plugins with their `web/src/plugins/<id>` trees, re-added to `plugins/plugins.go`; an id the template lacks fails the command |
| `--plugins basic` | every free plugin, usable without a licence |
| `--plugins standard` | basic plus the report / audit / security pro plugins, compiled in so a licence activates them |
| `--with-plugins` | every plugin in the template — only for a straight copy |

Read the generated `AGENTS.md` and `.nexadmin.manifest.json`. Put backend business code in `internal/`, pages in `web/src/pages/`, migrations in `migrations/business/`, and config overrides in `config/config.yaml`. See the [Customization Guide](dev-guide.md) for upgrade boundaries.

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

## 5. Install and use plugins

![Plugin lifecycle: free plugins work right after start, pro plugins need a licence, plugins touching the root router need a restart](/website-assets/docs/plugin-lifecycle.en.svg)

### Free plugins

Scheduled jobs, the code generator, the import center and other general-purpose capabilities ship with the framework and need no licence file. Install the scheduled-jobs plugin once to see the whole flow.

> [!NOTE]
> The master and official release packages compile `totp` (two-factor) by default, and `nexadmin new` carries it over; other free plugins are installed on demand. `ldap` and `totp` do not conflict — one verifies the password, the other asks for a code after sign-in. Do **not** enable `ldap` and `identity` directory sources together: both register external authenticators, only one takes effect, and "configured but silent" is the hardest failure to diagnose.

#### Install from the CLI

```bash
# Prerequisite: cron.zip is in the project root (from a release package or the repo)

./bin/nexadmin-server stop                        # 1. stop the service
go run ./cmd/nexadmin plugin install ./cron.zip   # 2. unpack and register the source
sh scripts/build.sh                               # 3. rebuild, about 30 seconds
./bin/nexadmin-server start                       # 4. start
```

#### Install from the plugin marketplace

If development mode (`go run ./cmd/server`) is already running, open `http://localhost:8080/admin/system/plugin` → Marketplace:

1. Find the "Scheduled Jobs" card (marked Free)
2. Click Install and let the progress bar finish
3. Refresh; the status reads Active

#### Verify the install

- The plugin page shows cron as `active`
- `/admin/plugins/cron/jobs` lets you create a task that runs in 5 minutes
- `logs/nexadmin.out` contains the scheduler startup line

### Pro plugins

Workflow, data audit, the payment hub and similar capabilities need a separate licence.

1. **Get a licence**: request a `.lic` file from the vendor
2. **Install the source**: follow the steps above to bring the package in
3. **Import the licence**: open the plugin page, select the plugin, paste the licence contents
4. **Restart**: when the UI reports `needRestart`, run `./bin/nexadmin-server restart`

> [!WARNING]
> A licence only activates a plugin that is **already compiled into the binary**. Three more things invalidate it: serving a domain other than the licensed one, expiry (the plugin is disabled automatically), and a `requires_nexadmin` version mismatch. Plugins that report `needRestart=true` extend root routes or startup-time authentication and cannot be enabled without a restart.

#### Licence FAQ

| Question | Answer |
|---|---|
| Why does it ask for a restart? | The plugin extends root routes or startup-time authentication, which can only load when the service starts. Other plugins activate as soon as the licence is imported. |
| How do I tell free from paid? | The marketplace marks free plugins as Free; pro plugins show a price or "TBD". |
| Can a licence be transferred? | Each licence binds a customer name and a domain. Reissuing is required to change the domain. |
| Is there a trial? | There is no automatic trial. Ask the vendor for a temporary licence to evaluate pro features. |

## 6. Licensing, disabling and service lifecycle

Free plugins need no `.lic`. For pro plugins, a platform super admin imports the license in Plugin Management; the file is stored at `data/licenses/<id>.lic`. Source installation still needs a build, and license import only activates compiled plugins: when it returns `needRestart=true`, restart as instructed. Not every plugin can be hot-activated. Domain binding, expiry and the `requires_nexadmin` range must all match.

Free / pro is a runtime classification, not a legal licence grant; pricing, refunds and IP terms are not defined in this guide.

```bash
./bin/nexadmin-server status
./bin/nexadmin-server stop
./bin/nexadmin-server start
./bin/nexadmin-server restart
```

These commands manage processes launched by `start`. Stop foreground `go run` with Ctrl+C, and manage systemd / containers through their supervisors. On Windows use `bin\nexadmin-server.exe`.

To temporarily disable a plugin, add its ID to the complete `disabled_plugins` list in user configuration `config/config.yaml`, then restart. Remove the ID and restart to restore it. `plugin remove <id>` deletes source and the registration import, requiring a rebuild and restart; database tables and license files remain. Do not use the table-dropping `--purge` option for ordinary disabling; `--purge` also clears plugin tables and migration records, so stop the service and back up the database first.

## 7. Documentation and release checks

Technical documentation has one source of truth: this repository's `docs/`, including `docs/en/`. The website documentation section renders embedded Markdown; edits require rebuilding and redeploying. Product descriptions, news and marketing pages belong in the website CMS, not a second copy of technical documentation. The website plugin is not installed by default in derived projects.

Languages come from `sys_language`. Source dictionaries and Markdown require rebuilds; changes to the language set or URL route prefixes require a restart. Documents follow stable language Code: `zh-CN` uses Chinese source files, English uses `en/`, and other languages use the lowercase primary subtag, not an editable URL prefix.

Before production, follow the [Deployment Guide](deploy.md) to validate real database migrations, backup and restore, permissions and tenant isolation, external integrations and the chosen deployment method. Builds and unit tests do not replace these checks. See [Features & Permissions](features.md) for APIs and permissions, and the [Plugin Development Guide](plugin-dev.md) for package structure.
