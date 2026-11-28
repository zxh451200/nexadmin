---
title: Admin tour
summary: A walk through every admin page — what it is for and how you use it day to day.
---

# Admin tour

This document walks you through every page of the admin console: what each page is for and how you use it. After reading it you can drive the console yourself — no development background needed.

> Want to get an environment running first? Read the [Quick start](quickstart.md). Want the precise contract of every endpoint and permission point? Read [Features & permissions](features.md).

## Find a feature by task

| What you want to do | Where to go |
| --- | --- |
| Add a colleague's account and grant permissions | System → Users, Roles |
| Check whether the system is healthy | System monitoring |
| Send an announcement / manage files | System → Announcements, Files |
| Handle customer enquiries and issues | Business → Live chat, Tickets |
| Configure an approval flow | Business → Approval center |
| Take payments, refund, reconcile | Business → Payments |
| Edit website content | Website |
| Install plugins, do custom development | Development & security |

## Dashboard

The first screen after signing in. Stat cards (today's sign-ins, unread messages, total users, storage usage and so on), a 7-day trend chart, to-dos, shortcuts and recent announcements all live here.

- "Edit layout" in the top-right lets you drag and hide each card; the layout is saved per user.
- Statistics only cover the current tenant's data.

## Import & export

- **Export center**: every "async export" job (a user list exported to xlsx, for example). You can download results, cancel, retry and delete. Regular users see only their own jobs; administrators see the whole tenant's.
- **Import center**: the single entry point for Excel / CSV imports. Download a template, fill it in and upload; the system validates headers and required fields and reports failing rows separately without touching the valid data. Which business tables can be imported is registered by each module.

## System

- **Users**: every colleague's account. Create, edit, delete (batch supported), enable or disable, reset password, unlock, assign roles / departments / posts, plus Excel import and export. When someone forgets a password, an administrator resets it here. Disabling, deleting or resetting invalidates every existing login for that account; re-enabling does not restore old tokens.
- **Roles**: permissions are granted through roles. Create a role, tick the menus it sees and the buttons it can use, set its data scope (own only / own department / all, etc.), then assign the role to users.
- **Menus**: the admin sidebar tree. Add or remove menus, change icons and order, show or hide them; button-level permissions are registered here too. This edits the site-wide menu (platform level).
- **Departments**: the company org tree, multi-level with drag-and-drop ordering; the "Posts" tab maintains posts. The user list and approval flows both use departments.
- **Dictionaries**: fixed option sets such as dropdowns and tags (for example "Customer tier: A/B/C") defined in one place. Factory dictionaries are protected — rename only, no deletion.
- **Settings**: 13 sections — site info (name / logo / footer), sign-in security, sign-in methods, LDAP, SSO, file storage, AI models, SMS, notification channels, map service, languages, parameters. Sections with a "Test" button (SMS, email, AI and so on) can be tested before saving. Most sections are platform level: one shared configuration for every tenant.
- **Recycle bin**: deleted users, announcements, roles and the like land here first; restore or delete permanently.
- **Backups**: press "Back up now", or configure scheduled backups and retention; backup files are downloadable. Super admin only.
- **Announcements**: publish to everyone, or to specific roles / departments / users. Rich text, scheduled publishing, pinning, a login popup for important ones, and read statistics.
- **Files**: the tenant's own drive. Grouped, with resumable uploads for large files, previews, and public or signed download links; bounded by the tenant's storage quota.

## Development & security

- **Plugins**: an overview of installed plugins. Each card shows its version and licence state with its own enable / disable switch; install from the marketplace in one click, and import licence files for pro plugins here. Some plugins need a restart after being toggled.
- **Scheduled jobs**: cron-scheduled tasks (5 fields). Run immediately, retry on failure, and every run is logged.
- **Code generator**: pick a business table (or define a new one in the wizard) and generate full-stack, backend-only or frontend-only output. Field settings include labels, placeholders, defaults and string validation rules. Review the checks for the selected scope before downloading a zip or writing to the project. Direct writes show a file plan, preserve existing files by default, back up overwrites with rollback on failure, and protect applied or conflicting migrations. Decimal values stay strings and nullable fields preserve NULL.
- **SAML single sign-on**: connect a corporate IdP (Okta, Entra ID, Keycloak, ADFS, etc.) for SSO and single logout over HTTP-Redirect or HTTP-POST. Metadata import/refresh, signing and encryption policies, two-step certificate rollover, IdP/SP certificate expiry alerts, self-tests and safe operation auditing are supported. **External base URL** under Basic info pins the public SP http(s) origin; once set, ACS, SLO, SP metadata and sign-in return URLs no longer depend on Host or proxy headers. Creating, updating and toggling a source creates config versions, and automatic refresh creates one only when metadata content actually changes. Roll back from **Diagnostics & governance → Config versions**, where field-level differences are shown; rollback keeps the current SP certificate and private key and restores all other settings from the selected version. Live SSO/SLO round-trip tests open the IdP in a separate window and return a standalone result page without issuing a NexAdmin session; SLO tests require a NameID. Diagnostic events store redacted details and a correlation ID, never raw assertions or private keys. SAML sessions are persisted so administrators can revoke local access/refresh tokens per session, and IdP SLO updates the corresponding database sessions. Disabling or deleting a source revokes roles granted by that source. Group mappings preview effective role additions/revocations; SCIM external-ID matching and explicit empty-group revocation are available. Role reconciliation runs after MFA and only revokes assignments created by SAML, never claiming or deleting pre-existing manual assignments. First-time role initialization resumes after an interrupted MFA challenge, and missing assignments are repaired from actual role rows. Manual role replacement applies only additions/removals: unchanged assignments keep their provenance, while added or removed roles invalidate the corresponding records in the same transaction. Role renames do not break assignment identity.
- **Open platform**: expose this system's API to downstream apps — register apps with assignable-role boundaries, set credential call quotas, manage HMAC keys, configure webhook event subscriptions, review delivery records and call audits, and read the online API docs.
- **Translation editor**: edit UI copy on the web with drafts, publishing and per-version rollback; no code files involved.

## Monitoring

- **Operation log**: every write operation in the admin (who, when, which endpoint). Filterable, exportable, pruned by day.
- **Sign-in log**: every sign-in attempt (success / failure / IP / device). Start here when investigating a compromised account.
- **Online users**: the sessions currently active; force anyone offline.
- **Server monitoring**: CPU, memory, disk and the Go runtime; the "Frontend errors" tab collects JS errors from the browser. Sampled every 30 seconds, kept for 7 days.
- **Cache monitoring**: Redis / Valkey memory, hit rate and key count.
- **API monitoring**: QPS, latency and error rate per endpoint over the last 30 seconds.
- **Delivery log**: SMS / email / bot delivery results and failure reasons, with recipients stored masked.

## Tenants

- **Tenants**: create tenants (customer companies), renew, enable or disable, and watch usage. Suspending or deleting invalidates existing tenant logins and connections; restoring an expired / suspended tenant requires a fresh sign-in. Deletion is refused while registered business data or an isolated database remains.
- **Packages**: define how many users / storage / AI quota each tier gets and which menus it may use.
- **My tenant**: tenant admins see their own plan, quota and usage, and maintain the company profile.

## Business center

These pages come from plugins installed on demand — you see the ones you installed.

- **Identity verification**: ID number validation and name + ID two-factor checks, with a ledger of results.
- **Payments**: integrations with Alipay, WeChat Pay and other channels. Payment orders, refunds, reconciliation, payouts and settlement all live here, and every money movement is recorded.
- **Live chat**: the agent console behind the chat bubble on the website. Visitor messages are auto-assigned or queued; an agent claims and replies. Transfers, quick replies, internal notes, satisfaction ratings, conversation merging and export are supported. Mobile switches between queue and detail, synchronizes selection with the URL and browser history, and clears deep links when the current conversation is closed or deleted.
- **Tickets**: the full submit → assign → handle → close flow for customer issues. Priorities, SLA deadlines, internal notes, attachments and automation rules. Mobile keeps a return path between queue and detail, including invalid or failed detail deep links.
- **Approval center**: approvals for business processes (leave, expenses). Admins draw the flow on a BPMN canvas and instances follow it; or-sign / all-sign, timeout escalation, withdrawal and rollback.
- **Device monitoring**: connect industrial devices such as inverters and PLCs; live readings, historical curves, threshold alarms and remote commands.
- **Customer master data**: customer profiles, contacts, follow-up records and opportunity ledger; align members from external systems (hotels, shops) to the same customer.

## Website

Manage the corporate website (the public site visitors browse without signing in). The first item in this group, "Open website", opens the public site in a new tab:

- **News / Pages**: the site's news and static pages (About us and so on), both with multiple languages, scheduled publishing and draft preview.
- **Products / Product categories**: publish and unpublish products, galleries, price and MOQ; categories support multiple levels.
- **Messages**: enquiries submitted from the "Contact" page — unread badges, a three-state workflow (to follow up / followed up / ignored), batch actions and export.
- **Navigation**: the site's top navigation, supporting external links and internal pages.
- **Features**: the copy and icons of the "product features" cards on the home page.
- **Site settings**: site name, logo, home title, separate light/dark hero images (dark falls back to light; empty fields restore theme defaults), footer, SEO including a whole-file robots.txt override (clear it to restore defaults), notification recipients (empty falls back to the contact email), a website-chat switch (off removes both chat DOM and scripts), and privacy policy URL. "Default language" can follow the browser (first visit lands on the matching language) or pin one language to the root path (the previous default moves under a prefix; restart required).

## Profile

Avatar in the top-right → Profile: edit your own details and password, bind a phone number or third-party account, manage sign-in devices and personal access tokens (for calling the open API). Two-factor binding also lives here.
