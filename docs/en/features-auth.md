---
title: Login and authentication
summary: Sign-in methods, captcha and lockout rules, token lifetimes.
---

### Login methods

The login page shows methods per configuration; unconfigured ones are absent without errors (`GET /api/v1/auth/methods` returns `methods`, `providers`, `registerProviders`, `forgotEnabled`). All three methods issue the same JWT dual-token pair on success.

| Method | Enabled when | Notes |
| --- | --- | --- |
| Password | `auth.methods` contains `password` (default) | Optional captcha, account lockout on repeated failures; rules in the next two sections |
| Phone | `auth.methods` contains `phone` and `sms.provider` is non-empty (otherwise hidden) | The phone must already be bound; codes last 5 minutes, one SMS per phone per 60 seconds, 10 per phone per day, 10 per IP per hour, 5 wrong entries void the code |
| Third-party login | `auth.providers.<name>` fully configured, or enabled and completed in Settings → Sign-in methods (read per request, effective on save) | OAuth2 authorization code flow, see below |

Supported providers: WeCom, DingTalk, Feishu, Google (OIDC, code + PKCE), GitHub, and generic OIDC (Keycloak, Casdoor, etc.). `state` is one-time and callback URLs are exact-whitelisted; the first login matches existing accounts by `match_by`, an unmatched account is told to "bind an existing account", and `auto_create: true` creates one automatically; only verified emails count; binding and unbinding live in Profile.

The callback page is `/admin/login/oauth/<provider>`, and `redirect_uris` must be its full URL — that is exactly what Settings → Sign-in methods → Redirect URIs asks for, prefilled with the current site's address when a provider is enabled. Google / GitHub need outbound internet; configure `auth.proxy` on restricted networks. Config keys are in the Configuration Reference §3.

#### Captcha

**Switch**: `captchaEnabled` under Settings → Security & sign-in → Sign-in security, on by default and effective immediately on save. When off, sign-in, self-registration and anonymous SMS code requests show no captcha at all; sign-in stays bound by IP rate limits and account lockout.

**Three forms**, assigned by the server per risk — users cannot downgrade:

| Form | Rules |
| --- | --- |
| Slider puzzle | **Default.** A notch is cut in a 384×150 backdrop (its X is retried into the legal range; flush with the form, scaled on narrow screens); dragging the piece into it issues a one-time token. At most 3 verify attempts per challenge, and a miss issues a new one |
| Click characters | **Escalates here after 6 failures.** A 384×150 master image with distractor characters; click the 2–3 characters from the strip in order. Dot positions stay server-side and each click is checked within its tolerance; at most 5 attempts |
| Digit image | **Manual switch**, labelled "use an image code instead" in the UI. 5 digits, 224×72 glyph rendering with random scaling / slant / wave distortion, valid 60 seconds, single use; `GET /auth/captcha/:key/audio` reads the same code aloud (read-only, not consumed), in zh or en per UI language |

- All three paths (sign-in, SMS, register) share one cache key and one `VerifyCaptcha` gate: IP-bound and single use. Issued tokens carry a form marker — slider `s|`, click `c|`, digit text unmarked.
- **The hard tier accepts only click tokens server-side** (`VerifyCaptchaHard`); slider tokens and digit text are rejected. A script that calls `/auth/slider` directly to mint a token is blocked by the server, not merely by the frontend switching form. The trade-off: the digit code is unusable during escalation either, and the counter self-heals after a fixed 15-minute window.
- The slider also inspects the trail: at least 6 samples, 150ms–15s duration, at least 40px of travel, landing within ±6px, plus speed dispersion and a teleport cap across samples. The form cannot be switched away, and the only exit in the UI is "use an image code instead".
- There are 21 backdrop assets, each render overlaid with a random tone shift (applied to backdrop and cut-out alike), so a pre-marked image library cannot locate the notch; the notch X is not derived from the client-visible key.
- The keyboard path was removed by product decision; the accessibility fallback is the digit code (typeable, with audio). All three forms have a refresh button in the top-right corner.
- Operational metrics are exposed on `/metrics`: `nexadmin_auth_captcha_issued_total` / `verify_pass_total` / `verify_fail_total` (split by image / slider / click) and `escalation_total` (by slider / click tier).

#### Lockout and rate limits

- An account locks once consecutive password failures hit the limit: fallback 5 failures / 15 minutes, with the factory lockout duration at 30 minutes, adjustable in the `security` section of Settings or via the `login.max_retry` and `login.lock_minutes` system parameters.
- Only **password** failures count, and they are counted per username — a clumsy human never gets locked. A slider drag miss only feeds the IP-side escalation counter. Slider-grinding automation still escalates, and escalation is immediate: once a drag miss pushes the IP counter past the hard threshold, the verify-failure response itself carries `CAPTCHA_REQUIRED_HARD` and the frontend switches to the click form on the spot, without waiting for the next wrong password.
- Lockout is persisted to `sys_login_lock` (per tenant + username); the cache is just a fast path, so a restart or another instance still rejects the correct password of a locked account. Expired rows are pruned at most once an hour, triggered when the next lock is created.
- A non-existent user still costs one bcrypt comparison, and the response is always "wrong username or password" while the real reason only reaches the log; lockout treats existing and non-existent usernames alike.
- Forgot password (`POST /auth/forgot/send-code`, `/auth/forgot/reset`): recovery uses the available email / SMS channels and account bindings; codes last 10 minutes, with a 60-second cooldown, at most 5 attempts, and per-IP and per-account hourly limits. A non-unique email or phone across enabled accounts cannot use recovery and account existence is not disclosed; an admin must reset the password.

#### Runtime configuration of sign-in methods

- The password / phone toggles, provider toggles and credentials, redirect URIs, WeCom AgentId, matching rules and outbound proxy are stored in `sys_setting.auth` and read per request — a save takes effect on the next login-page load, without editing `config.yaml` or restarting.
- `config.yaml` becomes the fallback and seed: when the section was never saved everything follows the file; a saved provider only overrides the fields it filled (an empty secret keeps the file or env value), and turning it off disables it even if the file configures it.
- The settings GET hydrates providers configured in the file as enabled (secret left empty), so admins can see them and won't assume nothing is configured.
- Saving validates with the same merge rules: an enabled provider still missing `client_id` / `client_secret` / `redirect_uris` (plus `agent_id` for WeCom, `issuer` for OIDC) is rejected (`config.auth.provider_incomplete`), and so is leaving no sign-in method at all (`config.auth.no_login_method`) — no more "saved successfully, nothing on the login page".
- Provider adapters are cached by a config fingerprint, so changed credentials, endpoints or proxy rebuild them on the next authorization instead of redeeming codes with a stale `client_id`.

### Registration

The register page is reachable once `auth.allow_register` or the `user.register_enabled` system parameter is on, and shows the same row of "quick sign-up" buttons under the register button, listing every configured provider — `registerProviders` from `/auth/methods` (equal to `providers` while self-registration is open, empty otherwise). Two registration paths coexist: username and password go through `POST /auth/register`, while a provider button goes through `GET /auth/oauth/<provider>/start?intent=register`.

- **The register intent lives in the server-side state** (`oauthflow.PurposeRegister`) and cannot be forged by the client. `start` checks the same runtime switch as `/auth/register` (404 when closed) and rate-limits by IP to 10 per hour (the `oauth_register` bucket, shared across providers and counted separately from password registration).
- On the way back, `resolveLogin` still honours an already-bound identity first and then `match_by` against existing accounts; when neither matches it **creates the account**, and it is **not gated by the provider's `auto_create`** — that switch only governs auto-creation on the login side.
- Creation obeys the same constraints as self-registration: the tenant plan's user quota (`ensureUserQuota`), a passwordless account (no password sign-in; use the provider afterwards or set a password in Profile), roles from `auth.register_roles` (same as password registration, **not** the provider's `default_role`), and the same `user.created` event on success.
- If an administrator turns self-registration off mid-authorization, the register intent in the state lapses and the flow falls back to login semantics (no account is created), so a user who already authorized is not left stranded.

### Tokens & sessions

- Access token (default 30 minutes) and refresh token (default 168 hours), HS256; refreshing revokes the old refresh immediately; logout revokes the current access and its paired refresh; revocation records live in cache, and cache unavailability rejects requests.
- Optional `auth.single_session: true`: only the latest login's tokens are accepted for the same tenant+user; old access/refresh tokens are rejected, and refresh uses an atomic compare-and-swap so old sessions cannot overwrite new logins. Off by default; multi-instance requires shared Redis and identical configuration.
- Users of expired / suspended tenants cannot log in, cannot refresh, and their active requests are rejected too (§6).
- Online sessions are passively registered: logged-in requests record "user + IP + UA" into cache; force logout revokes the token of that session's last-seen entry.
