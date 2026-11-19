---
title: Features & permissions
summary: Look up a capability by topic — modules, login, open platform and quotas, plugin licensing, permission codes.
---

This is a **reference for verification**, not a tutorial. Use it to answer "does this exist, who can use it, which endpoint serves it".

| To look up | Read |
| --- | --- |
| What each built-in module and plugin does, and where it lives | [Built-in module list](features-modules.md) |
| Sign-in methods, token lifetimes, lockout and captcha rules | [Login and authentication](features-auth.md) |
| Open platform, roles and data scopes, tenant quotas | [Open platform and access](features-access.md) |
| How plugins are delivered and licensed, and how to toggle them | [Plugins and licensing](features-plugins.md) |
| Which endpoints a permission code covers | [Permission codes and endpoints](features-permissions.md) |

> [!TIP]
> To get an environment running first, read the [Quick start](quickstart.md). To learn what each admin page does, read the [Admin tour](overview.md).

## How to read this

- It documents **actual system behaviour**, not design intent. When it disagrees with the code, the code wins — report the mismatch as a bug.
- Tenant isolation follows the model's embedded base: `TenantModel` (has `tenant_id`, queries are scoped automatically) means per-tenant; `PlatformModel` means platform-level, and its write endpoints additionally require a platform-level caller via `core.RequirePlatform()`.
- All API paths are under `/api/v1`; only `/healthz` and `/metrics` sit at the root.
- Configuration keys and defaults live in the [Configuration reference](config.md); deployment steps live in the [Deployment guide](deploy.md). Neither is repeated here.
- Permission codes are written `module:action` (for example `ticket:edit`). Data scopes (own / department / all) are covered in [Open platform and access](features-access.md).
- Chains that can only be verified against a real database (migrations, sign-in, payment callbacks) are described by their acceptance criteria here; manual replay recipes are kept out of published docs.

This is an interface reference, not an environment acceptance report. Real databases, Redis multi-instance deployments, LDAP/AD/CAS, browser flows and deployment paths still need separate validation.
