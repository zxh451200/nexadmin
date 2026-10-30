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

Replace the name, output directory and Go module path. The destination must be absent or empty and outside the template directory. By default, this neither initializes Git nor commits and includes **no plugins other than `totp`** (two-factor, part of the security baseline); pass `--without-totp` if you do not need MFA. Do not add `--with-plugins` just to start business development with the template's development plugins and state.

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

## 5. Install and Use Plugins

### Free Plugins (Ready to Go)

**Default build scope**: the framework master and official release packages compile `totp` (two-factor) by default, and `nexadmin new` includes it; `ldap` and the other free plugins are installed on demand. They do not conflict: LDAP verifies the password, TOTP asks for a code after login succeeds. Do not enable both `ldap` and `identity` directory sources at the same time — both register external authenticators, only one takes effect, which looks like "configured but not working".

**What are free plugins?** General-purpose capabilities like Scheduled Jobs (cron), Code Generator (codegen), and Data Import (imports) are offered freely with the framework. They don't require license files or extra fees.

**Installation steps (using cron as an example):**

#### Method 1: Command Line (for developers)

```bash
# Prerequisite: Download the free plugin package from our GitHub repository
# Assuming you've placed cron.zip in your project root

# 1. Stop the running backend service
./bin/nexadmin-server stop  # or Ctrl+C

# 2. Install the plugin (automatically writes to source tree)
go run ./cmd/nexadmin plugin install ./cron.zip

# 3. Rebuild (about 30 seconds)
sh scripts/build.sh

# 4. Restart the service
./bin/nexadmin-server start
```

#### Method 2: Web Interface (requires a writable source tree and build toolchain)

If you've started development mode (`go run ./cmd/server`), you can directly visit `http://localhost:8080/admin/system/plugin` → Plugin Marketplace tab:

1. Find the "Scheduled Jobs" card (marked "Free")
2. Click the "Install" button
3. Wait for the progress bar to show "Installation complete" (auto-compiles and restarts)
4. Refresh the page; you'll see cron's status as "Active"

**✅ How to verify after installation?**

- **Check in admin**: Visit `/admin/system/plugin`, confirm cron status is `active`
- **Functional test**: Visit `/admin/plugins/cron/jobs`, create a task that runs every 5 minutes
- **Log confirmation**: Check `logs/nexadmin.out`; you should see scheduler startup logs

---

### Pro Plugins (License Required)

**What are pro plugins?** Advanced features like Workflow Engine, Data Audit (audit), Payment Integration (pay), etc., require separate licenses.

**Installation flow:**

1. **Purchase license**: Contact vendor to get `.lic` file
2. **Install plugin**: First follow the "install plugin" steps above to install source code
3. **Import license**: In Plugin Management → select the corresponding pro plugin → paste license content
4. **Check status**: If it prompts `needRestart=true`, execute `./bin/nexadmin-server restart`

**⚠️ Important Notes**

- Free plugins **don't require license files**; no need to handle license
- After importing pro plugin authorization, if it prompts `needRestart=true`, this means the plugin uses root routes or authentication extensions which must be loaded at server startup, so restart is required. Other pro plugins become active immediately after license import
- **Domain Binding**: If your plugin license binds a specific domain (like `example.com`), you must use this exact domain when accessing; otherwise the license becomes invalid.
- **Expiry**: All Pro licenses have an expiration date (e.g., `2027-12-31`). They will auto-disable after expiry. Renewal requires importing a new license.

---

**📦 License FAQ**

| Question | Answer |
|---|---|
| Q: Why does my plugin prompt a restart needed? | A: This plugin uses root routes or auth extensions which must be loaded at server startup, requiring restart. Other plugins activate immediately after license import. |
| Q: How do I know what's free vs paid? | A: In Plugin Marketplace, free plugins are labeled "Free", Pro plugins show "TBD" or a specific price. |
| Q: Can I transfer licenses? | A: Each license binds a customer name and domain. For domain changes, contact vendor to reissue. |
| Q: Is there a trial version? | A: Currently auto-trial isn't provided. For Pro evaluation, contact vendor for temporary license. |

## 6. Licensing, disabling and service lifecycle

Free plugins need no `.lic`. For pro plugins, a platform super admin imports the license in Plugin Management; the file is stored at `data/licenses/<id>.lic`. Source installation still needs a build, and license import only activates compiled plugins: when it returns `needRestart=true`, restart as instructed. Not every plugin can be hot-activated. Domain binding, expiry and the `requires_nexadmin` range must all match.

**Formal purchase terms have not been published.** This guide sets no prices, refund rules, revenue shares or IP terms; free / pro is a runtime classification, not a legal license grant.

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
