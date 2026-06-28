# pit-pilot — Native Email/Password Auth + Dev Dummy-Login Design

**Date:** 2026-06-28
**Status:** Approved design, pending implementation plan
**Scope:** Add a native email + password authentication method (register + login) alongside the existing Google Sign-In, plus a dev-only dummy login for local testing. Built on the existing Cycle 1 session machinery.

---

## 1. Vision & Scope

pit-pilot today authenticates only via OIDC providers (Google live, Apple stubbed): the client sends an ID token, the backend verifies it and issues its own `pp_session` session-JWT cookie. This feature adds a **provider-independent** login so users (and local testers) don't need a Google account.

### In scope
- **Registration:** email + password + display name → creates an account, issues a session.
- **Login:** email + password → issues a session.
- **Dev dummy login:** a one-field "type a name → log in" affordance, **only** when `ALLOW_DEV_LOGIN=true`, for local testing of multi-user flows.

### Explicitly out of scope (later features)
- **Email verification** and **password reset** — both require an email-sending subsystem pit-pilot does not have yet. Until then: registering an account makes it immediately usable; a forgotten password has no self-service recovery.
- **Account linking** — a Google account and a password account with the same email are **separate accounts** (distinct `provider`). No linking in this step.

---

## 2. Key Decisions (settled in brainstorming)

- **Password hashing:** Argon2id (`golang.org/x/crypto/argon2`), OWASP params m=19456 KiB, t=2, p=1, 16-byte salt, 32-byte output, stored as the standard PHC string. No extra dependency beyond `golang.org/x/crypto`.
- **Data model:** reuse the `users` table — no new table. `provider='password'`, `provider_sub=<email>` (so the existing `UNIQUE(provider, provider_sub)` enforces per-email uniqueness for password accounts); `provider='dummy'`, `provider_sub=<name>` for dummy users (same name → same user). New nullable `password_hash` column.
- **Dummy login guard:** config flag `ALLOW_DEV_LOGIN` (default `false`); the compose stack sets it `true`, Kustomize leaves it `false`. When disabled, `POST /auth/dev-login` returns 404 and the frontend hides the dummy block.
- **Dummy form:** a free-text name field (not a single fixed user), so multiple stable test users can be created locally.
- **Coexistence:** the Google button stays; email/password and dummy are additive on the login screen.
- **Password policy:** minimum length 8.

---

## 3. Data Model & Migration

Migration `0004_password_auth`:

```sql
-- up
ALTER TABLE users ADD COLUMN password_hash text;
ALTER TABLE users DROP CONSTRAINT users_provider_check;
ALTER TABLE users ADD CONSTRAINT users_provider_check
  CHECK (provider IN ('google','apple','password','dummy'));
```
```sql
-- down
ALTER TABLE users DROP CONSTRAINT users_provider_check;
ALTER TABLE users ADD CONSTRAINT users_provider_check
  CHECK (provider IN ('google','apple'));
ALTER TABLE users DROP COLUMN password_hash;
```

- `password_hash` is nullable and set **only** for `provider='password'` accounts (NULL for google/apple/dummy).
- The implementation must confirm the existing inline CHECK constraint's generated name (`users_provider_check` is the expected Postgres default for a single-column table check); adjust the migration if it differs.
- After adding the migration: regenerate sqlc, run `make sync-migrations`, and add `0004_password_auth.{up,down}.sql` to `deploy/base/kustomization.yaml`'s `configMapGenerator`. The testcontainers harness applies it automatically.

New sqlc queries in `internal/store/queries/users.sql`:
- `CreatePasswordUser` — insert `provider='password'`, `provider_sub=<email>`, `email`, `display_name`, `password_hash`; RETURNING the row. No `ON CONFLICT` — a duplicate raises a unique violation that the service maps to 409.
- `GetPasswordUserByEmail` — `SELECT ... WHERE provider='password' AND provider_sub=<email>`.
- `UpsertDummyUser` — insert `provider='dummy'`, `provider_sub=<name>`, `display_name=<name>` with `ON CONFLICT (provider, provider_sub) DO UPDATE SET display_name=EXCLUDED.display_name` RETURNING the row.

---

## 4. Backend

### `password` package (`internal/password/`)
- `Hash(plain string) (string, error)` — 16-byte crypto-random salt; argon2id with the params above; returns the PHC string `$argon2id$v=19$m=19456,t=2,p=1$<b64salt>$<b64hash>`.
- `Verify(plain, encoded string) (bool, error)` — parse the PHC string (params + salt + hash), recompute with the parsed params, compare with `subtle.ConstantTimeCompare`. A malformed/foreign encoded string returns `(false, error)`.
- Self-contained and unit-tested; no `net/http`, no DB.

### `UserService` (adds to the existing service)
- `Register(ctx, email, password, displayName string) (gen.User, error)` — validate (email non-empty, password length ≥ 8 → `apperr.BadRequest`); `password.Hash`; `CreatePasswordUser`; a unique-violation maps to `apperr.Conflict("email_taken", …)`.
- `LoginWithPassword(ctx, email, password string) (gen.User, error)` — `GetPasswordUserByEmail`; if not found **or** `password.Verify` is false → `apperr.Unauthorized` with one identical message (no user enumeration).
- `DevLogin(ctx, name string) (gen.User, error)` — validate name non-empty; `UpsertDummyUser`.
- The existing `LoginWithIDToken` is unchanged. `UserService` keeps its verifier dependency; the password package is used directly.

### Handlers & routes (top-level, public — next to `/auth/google`)
| Route | Handler | Behavior |
|---|---|---|
| `POST /auth/register` | `AuthHandlers.Register` | body `{email, password, display_name}` → 201 + `pp_session` cookie + user; 409 if email taken; 400 on validation |
| `POST /auth/login` | `AuthHandlers.PasswordLogin` | body `{email, password}` → 200 + cookie + user; 401 on bad credentials |
| `POST /auth/dev-login` | `AuthHandlers.DevLogin` | body `{name}` → 200 + cookie + user **iff** `AllowDevLogin`; otherwise 404 |

- Extract the `pp_session` cookie-setting logic (currently inline in the OIDC `Login` closure) into a shared `issueSession(w http.ResponseWriter, userID uuid.UUID)` helper used by all four login paths (DRY). The cookie attributes are unchanged (httpOnly, `Secure: cfg.CookieSecure`, SameSite=Lax, 30-day expiry).
- Login responses keep the existing shape (`id`, `display_name`, `avatar_url`); `password_hash` is never serialized.

### Config
- New `AllowDevLogin bool` in `config.go`, read from `ALLOW_DEV_LOGIN` (default `false`). Wired into `AuthHandlers` from `main.go` (alongside `CookieSecure`, `GoogleClientID`).
- `GET /auth/config` returns `{ "google_client_id": <id>, "allow_dev_login": <bool> }` so the frontend can show/hide the dummy block. (`AuthHandlers.Config` gains the field.)

---

## 5. Frontend

New client helpers in `src/api/client.ts`:
- `register(email, password, displayName)` → `POST /auth/register`
- `passwordLogin(email, password)` → `POST /auth/login`
- `devLogin(name)` → `POST /auth/dev-login`
- `getAuthConfig()` return type extends to `{ google_client_id: string; allow_dev_login: boolean }`.

`Login.tsx` (the Google button is kept; methods coexist):
- An **email/password form** with an **Anmelden ↔ Registrieren** toggle:
  - *Anmelden*: email + password → `passwordLogin` → `refresh()` → navigate `/groups`.
  - *Registrieren*: display name + email + password → `register` → `refresh()` → navigate `/groups`. Client-side minimum check (password ≥ 8); server errors (409 "E-Mail vergeben", 401 "falsche Daten", 400) shown via the existing `role="alert"`.
- Below it, visually separated, the existing **"Mit Google anmelden"** button (only when a client ID is configured).
- At the bottom, **only when `allow_dev_login`**, a small dev block: a name field + "Dev-Login" button → `devLogin(name)` → navigate `/groups`.

The component stays focused; the GIS logic already lives in `src/auth/google.ts`. All copy is German.

---

## 6. Configuration & Deployment

- `ALLOW_DEV_LOGIN` defaults to `false`. The compose stack (`docker-compose.yml`) sets it `true` (dummy login available locally); the Kustomize overlays leave it unset/`false` (no dummy login in production). Documented in `.env.example`.
- No new runtime dependency beyond `golang.org/x/crypto`.

---

## 7. Test Strategy (test-first)

- **`password` package:** Hash→Verify roundtrip; wrong password → false; two hashes of the same password differ (salt); a malformed/foreign PHC string is rejected.
- **Service (testcontainers Postgres):** `Register` success + duplicate email → `Conflict`; `LoginWithPassword` success + wrong-password/unknown-email → `Unauthorized` (same message); `DevLogin` creates and is idempotent (same name → same user id).
- **Handlers:** `/auth/register` sets the cookie + 201; `/auth/login` 200/401; `/auth/dev-login` 200 when `AllowDevLogin=true` and 404 when `false`; `/auth/config` includes `allow_dev_login`.
- **Frontend:** the three client helpers (fetch mocked); `Login.tsx` — the toggle posts register vs login to the correct endpoints, errors are surfaced, and the dummy block renders only when `allow_dev_login` is true.

### Security details
- Identical error message on any login failure (no user enumeration).
- Passwords are never logged or returned; `password_hash` is not part of any API response.

---

## 8. Build Order

1. Migration `0004` (password_hash + CHECK) + sqlc queries (`CreatePasswordUser`, `GetPasswordUserByEmail`, `UpsertDummyUser`) + regenerate/sync.
2. `password` package (Argon2id Hash/Verify) — test-first.
3. `UserService.Register` / `LoginWithPassword` / `DevLogin` — test-first.
4. Config `AllowDevLogin` + the `issueSession` helper + `/auth/register`, `/auth/login`, `/auth/dev-login` handlers + routes + `/auth/config` extension + main wiring — test-first.
5. Frontend client helpers (`register`, `passwordLogin`, `devLogin`, extended `getAuthConfig`).
6. `Login.tsx` email/password form + toggle + dev dummy block + tests.
7. `ALLOW_DEV_LOGIN=true` in the compose stack + `.env.example` docs.

Each step is built test-first.
