---
title: Plugins and licensing
summary: How plugins ship and install, what separates free from pro, what a licence file validates, and how to toggle a plugin at runtime.
---

Plugins ship as **source packages**, not shared libraries. Installing means dropping the source into `plugins/<id>` and rebuilding — there is no hot-plugging.

| Edition | Declared as | Licence file |
| --- | --- | --- |
| Free | `Edition: Free` in the plugin struct | not needed |
| Pro | `Edition: Pro` in the plugin struct | required at `data/licenses/<id>.lic` |

A licence is an ed25519-signed offline file. Its payload carries the plugin ID, customer name, issue and expiry dates, and optionally a bound domain.

## When validation happens

Each plugin is validated at startup; only valid ones get their routes and menu entries mounted.

- Missing, expired or tampered licence → not mounted, with the reason shown on the plugin page.
- After importing a licence, an already-compiled plugin is activated on the fly; when the response carries `needRestart`, restart as instructed.
- Domain binding, expiry and the `requires_nexadmin` version range must all hold.

> [!NOTE]
> Importing a licence only activates a plugin that is already compiled into the binary. If the source was never installed, no licence makes it appear.

## Commands

| Command | Purpose |
| --- | --- |
| `nexadmin plugin list` | List installed plugins and licence state |
| `nexadmin plugin install <zip>` | Unpack into the source tree and register in `plugins/plugins.go` |
| `nexadmin plugin remove <id>` | Drop source and registration (tables and licence stay) |
| `nexadmin plugin keygen` / `sign` / `revoke` | Issue and revoke licences (vendor side) |

Install or remove is not enough on its own: rebuild and restart so the change reaches the binary.

## Toggling at runtime

Every card on the plugin page has a switch, backed by `POST /plugins/<id>/enabled`. Disabling takes effect immediately:

- That plugin's routes return 403 with the reason.
- Its sidebar entries disappear, and `GET /plugins/active` stops listing it.
- The disabled list is stored per node at `data/licenses/disabled.json`, so a multi-node deployment has one copy each.

> [!WARNING]
> Disabling never touches the database — tables and rows remain. `--purge` is different: it drops the plugin's tables together with its migration records and cannot be undone. Stop the service and back up first.

## Plugin inventory

### Free

| ID | Name | Version |
| --- | --- | --- |
| `codegen` | Code generator | 1.4.0 |
| `crm` | Customer master data | 0.1.0 |
| `cron` | Scheduled jobs | 1.0.0 |
| `imports` | Import center | 0.1.0 |
| `kb` | Knowledge base | 0.1.0 |
| `ldap` | LDAP/AD sign-in | 0.1.0 |
| `maps` | Map services | 0.1.0 |
| `realname` | ID verification | 0.1.0 |
| `totp` | Two-factor authentication | 0.1.0 |

### Pro

| ID | Name | Version |
| --- | --- | --- |
| `aigateway` | AI gateway | 0.1.0 |
| `assistant` | AI assistant | 1.1.0 |
| `audit` | Data change audit | 1.0.0 |
| `devicehub` | Device access and monitoring | 0.1.0 |
| `formbuilder` | Form builder | 1.0.0 |
| `i18n-editor` | Inline translation editor | 1.0.0 |
| `identity` | Identity providers | 1.0.0 |
| `im` | Live chat | 0.6.0 |
| `media` | Media optimization | 0.1.0 |
| `pay` | Payment hub | 0.1.0 |
| `report` | Reporting | 1.1.0 |
| `saml` | SAML single sign-on | 0.9.0 |
| `scim` | SCIM user sync | 1.0.0 |
| `security` | Advanced security | 1.0.0 |
| `ticket` | Ticketing | 0.1.0 |
| `website` | Company website | 0.6.0 |
| `workflow` | Workflow | 0.2.0 |

What each plugin does and where it sits in the menu is in the [Built-in module list](features-modules.md); how a plugin package is laid out and packaged is in the [Plugin development guide](plugin-dev.md).

> [!IMPORTANT]
> Free and pro are runtime technical categories, not a legal licence statement. Pricing, trials and transfer terms are governed by the vendor's contract.
