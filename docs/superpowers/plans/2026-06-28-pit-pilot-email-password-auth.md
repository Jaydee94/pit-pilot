# Native Email/Password Auth + Dev Dummy-Login Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add provider-independent email/password registration + login (Argon2id) alongside Google Sign-In, plus a dev-only dummy login gated by `ALLOW_DEV_LOGIN`.

**Architecture:** Reuse the `users` table (`provider='password'`, `provider_sub=<email>`; `provider='dummy'`, `provider_sub=<name>`) + a new nullable `password_hash`. A self-contained `password` package wraps Argon2id. `UserService` gains `Register`/`LoginWithPassword`/`DevLogin`; three public `POST /auth/*` handlers issue the same `pp_session` cookie via a shared helper. The frontend Login screen gains an email/password form (login/register toggle) and a dev-only dummy block.

**Tech Stack:** Go 1.26 (chi, pgx, sqlc), `golang.org/x/crypto/argon2`. React 19 + Vite + Vitest.

## Global Constraints

- Go module `github.com/jaydee94/pit-pilot/backend`, Go floor 1.26. Money/PKs conventions unchanged. New routes are PUBLIC (top-level, next to `/auth/google`), NOT under the session-gated `/api`.
- Password hashing: Argon2id, params m=19456 (KiB), t=2, p=1, 16-byte salt, 32-byte key, stored as the PHC string `$argon2id$v=19$m=19456,t=2,p=1$<rawb64 salt>$<rawb64 hash>` (base64 RawStdEncoding). Constant-time compare.
- Password policy: minimum length 8. Login failure returns ONE identical message for unknown-email and wrong-password (no user enumeration). `password_hash` is never serialized in any response.
- `ALLOW_DEV_LOGIN` defaults to `false`; `POST /auth/dev-login` returns 404 when disabled. Dev login uses a free-text name (`provider='dummy'`, same name → same user via upsert).
- DB enums are CHECK-constrained — the migration must extend `CHECK (provider IN (...))` to include `'password'` and `'dummy'`. Money is cents (unchanged). Strict TDD: failing test first.
- After adding the migration: regenerate sqlc (`go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate`), `make sync-migrations`, and list the new files in `deploy/base/kustomization.yaml` `configMapGenerator`. Backend tests need Docker (testcontainers). The frontend build (`tsc -b && vite build`) type-checks tests.

## Existing code this builds on

- `users` table (`internal/store/migrations/0001_init.up.sql`): `id, provider, provider_sub, email NOT NULL DEFAULT '', display_name, avatar_url, created_at`, `UNIQUE(provider, provider_sub)`, `CHECK (provider IN ('google','apple'))`. `sqlc.yaml` makes nullable `text` → `*string`, `uuid`→`uuid.UUID`.
- `internal/service/users.go`: `UserService{q *gen.Queries; v auth.IDTokenVerifier}`, `NewUserService(q, v)`, `LoginWithIDToken`. `apperr` has `BadRequest/Conflict/Unauthorized/NotFound/Forbidden(code, message string) *apperr.Error`.
- `internal/httpapi/auth_handlers.go`: `AuthHandlers{Users *service.UserService; Sessions *auth.SessionManager; CookieSecure bool; GoogleClientID string}`. `Login(provider)` decodes `{id_token}`, calls `Users.LoginWithIDToken`, `Sessions.Issue(user.ID)`, sets the `pp_session` cookie (Path `/`, HttpOnly, `Secure: a.CookieSecure`, SameSite=Lax, `Expires: now+30d`, `MaxAge: 30*24*60*60`), and `WriteJSON(w, 200, {id, display_name, avatar_url})`. `Config` returns `{google_client_id}`. Helpers: `WriteJSON`, `WriteError`, `UserID`.
- `internal/auth/session.go`: `(*SessionManager).Issue(userID uuid.UUID) (string, error)`.
- `internal/config/config.go`: `Load(getenv func(string) string)`; fields incl. `CookieSecure: getenv("COOKIE_SECURE")=="true"`.
- `internal/httpapi/router.go`: top-level `if d.Auth != nil { r.Post("/auth/google", ...); r.Post("/auth/apple", ...); r.Get("/auth/config", d.Auth.Config) }`.
- `cmd/api/main.go`: `&httpapi.AuthHandlers{Users: users, Sessions: sessions, CookieSecure: cfg.CookieSecure, GoogleClientID: cfg.GoogleClientID}`.
- Frontend `src/api/client.ts`: `apiFetch<T>(path, init?)`, `login(provider, idToken)`, `getAuthConfig()`. `src/routes/Login.tsx` (Google button + One Tap, fetches `/auth/config` on mount, `configured` state). `src/auth/AuthContext.tsx`: `useAuth().refresh` (memoized).

---

## Task 1: Migration 0004 + sqlc queries

**Files:**
- Create: `backend/internal/store/migrations/0004_password_auth.up.sql`, `0004_password_auth.down.sql`
- Modify: `backend/internal/store/queries/users.sql`
- Regenerate: `backend/internal/store/gen/*` (sqlc)
- Sync: `deploy/base/migrations/`, `deploy/base/kustomization.yaml`
- Test: `backend/internal/store/gen/users_password_gen_test.go`

**Interfaces:**
- Produces: `gen.User.PasswordHash *string`; `gen.CreatePasswordUserParams{Email, DisplayName string; PasswordHash *string}` + `(*Queries).CreatePasswordUser`; `(*Queries).GetPasswordUserByEmail(ctx, email string)`; `(*Queries).UpsertDummyUser(ctx, name string)` — all `:one` returning `gen.User`.

- [ ] **Step 1: Write the migration files**

`0004_password_auth.up.sql`:
```sql
ALTER TABLE users ADD COLUMN password_hash text;
ALTER TABLE users DROP CONSTRAINT users_provider_check;
ALTER TABLE users ADD CONSTRAINT users_provider_check
  CHECK (provider IN ('google','apple','password','dummy'));
```
`0004_password_auth.down.sql`:
```sql
ALTER TABLE users DROP CONSTRAINT users_provider_check;
ALTER TABLE users ADD CONSTRAINT users_provider_check
  CHECK (provider IN ('google','apple'));
ALTER TABLE users DROP COLUMN password_hash;
```
> The inline `CHECK (provider IN ('google','apple'))` on a single column is named `users_provider_check` by Postgres. Verify after the next step's migration runs (the testcontainers harness will fail loudly if the name is wrong); if it differs, correct both files.

- [ ] **Step 2: Add the sqlc queries** (append to `users.sql`)

```sql
-- name: CreatePasswordUser :one
INSERT INTO users (provider, provider_sub, email, display_name, password_hash)
VALUES ('password', sqlc.arg(email), sqlc.arg(email), sqlc.arg(display_name), sqlc.arg(password_hash))
RETURNING *;

-- name: GetPasswordUserByEmail :one
SELECT * FROM users
WHERE provider = 'password' AND provider_sub = $1;

-- name: UpsertDummyUser :one
INSERT INTO users (provider, provider_sub, email, display_name)
VALUES ('dummy', sqlc.arg(name), '', sqlc.arg(name))
ON CONFLICT (provider, provider_sub) DO UPDATE SET display_name = EXCLUDED.display_name
RETURNING *;
```

- [ ] **Step 3: Regenerate sqlc + sync**

```bash
cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
cd .. && make sync-migrations
```
Then add to `deploy/base/kustomization.yaml` under `configMapGenerator` `files:` (after the 0003 entries, matching the existing indentation):
```
      - migrations/0004_password_auth.up.sql
      - migrations/0004_password_auth.down.sql
```

- [ ] **Step 4: Write the failing test** (`users_password_gen_test.go`, package `gen_test`)

```go
package gen_test

import (
	"context"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestPasswordAndDummyUserQueries(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	ctx := context.Background()

	h := "argon2-hash"
	u, err := q.CreatePasswordUser(ctx, gen.CreatePasswordUserParams{
		Email: "a@example.com", DisplayName: "Alice", PasswordHash: &h,
	})
	if err != nil || u.Provider != "password" || u.ProviderSub != "a@example.com" || u.PasswordHash == nil || *u.PasswordHash != h {
		t.Fatalf("create password user: %+v err=%v", u, err)
	}

	got, err := q.GetPasswordUserByEmail(ctx, "a@example.com")
	if err != nil || got.ID != u.ID {
		t.Fatalf("get by email: %+v err=%v", got, err)
	}

	// duplicate email → unique violation
	if _, err := q.CreatePasswordUser(ctx, gen.CreatePasswordUserParams{Email: "a@example.com", DisplayName: "Dup"}); err == nil {
		t.Fatal("expected unique violation on duplicate email")
	}

	d1, err := q.UpsertDummyUser(ctx, "Tester")
	if err != nil || d1.Provider != "dummy" {
		t.Fatalf("dummy upsert: %+v err=%v", d1, err)
	}
	d2, err := q.UpsertDummyUser(ctx, "Tester")
	if err != nil || d2.ID != d1.ID {
		t.Fatalf("dummy upsert not idempotent: %v vs %v", d1.ID, d2.ID)
	}
}
```

- [ ] **Step 5: Run the test**

```bash
cd backend && go test ./internal/store/gen/ -run TestPasswordAndDummyUserQueries -v
```
Expected: PASS (migration applies, queries work, idempotent dummy, unique violation on dup). If it fails on the DROP CONSTRAINT name, fix the migration name and re-run.

- [ ] **Step 6: Commit**

```bash
git add backend deploy && git commit -m "feat: migration + sqlc queries for password and dummy users"
```

---

## Task 2: `password` package (Argon2id)

**Files:**
- Create: `backend/internal/password/password.go`
- Test: `backend/internal/password/password_test.go`

**Interfaces:**
- Produces: `password.Hash(plain string) (string, error)`; `password.Verify(plain, encoded string) (bool, error)`.

- [ ] **Step 1: Write the failing test**

```go
package password_test

import (
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/password"
)

func TestHashVerifyRoundtrip(t *testing.T) {
	h, err := password.Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := password.Verify("correct horse battery staple", h)
	if err != nil || !ok {
		t.Fatalf("verify should succeed: ok=%v err=%v", ok, err)
	}
	bad, err := password.Verify("wrong password", h)
	if err != nil || bad {
		t.Fatalf("verify should fail for wrong password: ok=%v err=%v", bad, err)
	}
}

func TestHashIsSalted(t *testing.T) {
	a, _ := password.Hash("samepw12")
	b, _ := password.Hash("samepw12")
	if a == b {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	if ok, err := password.Verify("x", "not-a-phc-string"); ok || err == nil {
		t.Fatalf("expected rejection, got ok=%v err=%v", ok, err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd backend && go test ./internal/password/
```
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write the implementation**

```go
// backend/internal/password/password.go
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	saltLen     = 16
	keyLen      = 32
	timeCost    = 2
	memoryCost  = 19456 // KiB
	parallelism = 1
)

// Hash returns a PHC-format argon2id string for the given plaintext.
func Hash(plain string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(plain), salt, timeCost, memoryCost, parallelism, keyLen)
	enc := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memoryCost, timeCost, parallelism, enc(salt), enc(key)), nil
}

// Verify reports whether plain matches the PHC-format argon2id encoded string.
func Verify(plain, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("invalid argon2id hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, err
	}
	if version != argon2.Version {
		return false, errors.New("incompatible argon2 version")
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(plain), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
```

- [ ] **Step 4: Run + tidy + verify pass**

```bash
cd backend && go mod tidy && go test ./internal/password/ -v
```
Expected: PASS (3 tests). `go mod tidy` pulls `golang.org/x/crypto` if not already present.

- [ ] **Step 5: Commit**

```bash
git add backend && git commit -m "feat: argon2id password hashing package"
```

---

## Task 3: UserService password + dummy methods

**Files:**
- Modify: `backend/internal/service/users.go`
- Test: `backend/internal/service/users_test.go` (add tests)

**Interfaces:**
- Consumes: `password.Hash/Verify`; `gen.CreatePasswordUser/GetPasswordUserByEmail/UpsertDummyUser`.
- Produces: `(*UserService).Register(ctx, email, pw, displayName string) (gen.User, error)`; `(*UserService).LoginWithPassword(ctx, email, pw string) (gen.User, error)`; `(*UserService).DevLogin(ctx, name string) (gen.User, error)`.

- [ ] **Step 1: Write the failing tests** (append to `users_test.go`; mirror how the existing tests build a `UserService` with `testutil.NewPostgres` + `gen.New` — pass a nil verifier since these methods don't use it)

```go
func TestRegisterAndLoginWithPassword(t *testing.T) {
	pool := testutil.NewPostgres(t)
	svc := service.NewUserService(gen.New(pool), nil)
	ctx := context.Background()

	u, err := svc.Register(ctx, "user@example.com", "hunter2hunter", "User")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if u.Provider != "password" || u.DisplayName != "User" {
		t.Fatalf("unexpected user: %+v", u)
	}

	// duplicate → Conflict
	if _, err := svc.Register(ctx, "user@example.com", "another8x", "Dup"); err == nil {
		t.Fatal("expected conflict on duplicate email")
	}

	// short password → BadRequest (no row created)
	if _, err := svc.Register(ctx, "short@example.com", "x", "Short"); err == nil {
		t.Fatal("expected error for short password")
	}

	got, err := svc.LoginWithPassword(ctx, "user@example.com", "hunter2hunter")
	if err != nil || got.ID != u.ID {
		t.Fatalf("login should succeed: %+v err=%v", got, err)
	}
	if _, err := svc.LoginWithPassword(ctx, "user@example.com", "wrongpass1"); err == nil {
		t.Fatal("login with wrong password should fail")
	}
	if _, err := svc.LoginWithPassword(ctx, "nobody@example.com", "whatever1"); err == nil {
		t.Fatal("login with unknown email should fail")
	}
}

func TestDevLoginIsIdempotent(t *testing.T) {
	pool := testutil.NewPostgres(t)
	svc := service.NewUserService(gen.New(pool), nil)
	ctx := context.Background()
	a, err := svc.DevLogin(ctx, "Alice")
	if err != nil || a.Provider != "dummy" {
		t.Fatalf("dev login: %+v err=%v", a, err)
	}
	b, err := svc.DevLogin(ctx, "Alice")
	if err != nil || b.ID != a.ID {
		t.Fatalf("dev login not idempotent: %v vs %v", a.ID, b.ID)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd backend && go test ./internal/service/ -run 'TestRegisterAndLoginWithPassword|TestDevLoginIsIdempotent'
```
Expected: FAIL — methods undefined.

- [ ] **Step 3: Implement** (add to `users.go`; add imports `errors`, `strings`, `github.com/jackc/pgx/v5`, `github.com/jackc/pgx/v5/pgconn`, the `password` package)

```go
func (s *UserService) Register(ctx context.Context, email, pw, displayName string) (gen.User, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return gen.User{}, apperr.BadRequest("invalid_email", "email required")
	}
	if len(pw) < 8 {
		return gen.User{}, apperr.BadRequest("weak_password", "password must be at least 8 characters")
	}
	hash, err := password.Hash(pw)
	if err != nil {
		return gen.User{}, fmt.Errorf("hash password: %w", err)
	}
	user, err := s.q.CreatePasswordUser(ctx, gen.CreatePasswordUserParams{
		Email: email, DisplayName: displayName, PasswordHash: &hash,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return gen.User{}, apperr.Conflict("email_taken", "email already registered")
		}
		return gen.User{}, fmt.Errorf("create password user: %w", err)
	}
	return user, nil
}

func (s *UserService) LoginWithPassword(ctx context.Context, email, pw string) (gen.User, error) {
	email = strings.TrimSpace(email)
	user, err := s.q.GetPasswordUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.User{}, apperr.Unauthorized("invalid_credentials", "invalid email or password")
		}
		return gen.User{}, fmt.Errorf("get password user: %w", err)
	}
	if user.PasswordHash == nil {
		return gen.User{}, apperr.Unauthorized("invalid_credentials", "invalid email or password")
	}
	ok, err := password.Verify(pw, *user.PasswordHash)
	if err != nil || !ok {
		return gen.User{}, apperr.Unauthorized("invalid_credentials", "invalid email or password")
	}
	return user, nil
}

func (s *UserService) DevLogin(ctx context.Context, name string) (gen.User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return gen.User{}, apperr.BadRequest("invalid_name", "name required")
	}
	return s.q.UpsertDummyUser(ctx, name)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
```

- [ ] **Step 4: Run + verify pass**

```bash
cd backend && go test ./internal/service/ -run 'TestRegisterAndLoginWithPassword|TestDevLoginIsIdempotent' -v && go vet ./...
```
Expected: PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
git add backend && git commit -m "feat: UserService register/login-with-password/dev-login"
```

---

## Task 4: Config flag + auth handlers + routes

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/httpapi/auth_handlers.go`
- Modify: `backend/internal/httpapi/router.go`
- Modify: `backend/cmd/api/main.go`
- Test: `backend/internal/httpapi/auth_handlers_test.go` (add tests)

**Interfaces:**
- Consumes: `Users.Register/LoginWithPassword/DevLogin`; `Sessions.Issue`.
- Produces: `Config.AllowDevLogin bool`; `AuthHandlers.AllowDevLogin bool`; handlers `Register`, `PasswordLogin`, `DevLogin`; `Config` returns `{google_client_id, allow_dev_login}`; routes `POST /auth/register`, `POST /auth/login`, `POST /auth/dev-login`. Shared `issueSession(w, user)` + `userResponse(user)` helpers.

- [ ] **Step 1: Write the failing tests** (append to `auth_handlers_test.go`)

```go
func TestConfigIncludesAllowDevLogin(t *testing.T) {
	h := &httpapi.AuthHandlers{GoogleClientID: "gid", AllowDevLogin: true}
	rec := httptest.NewRecorder()
	h.Config(rec, httptest.NewRequest(http.MethodGet, "/auth/config", nil))
	if !strings.Contains(rec.Body.String(), `"allow_dev_login":true`) {
		t.Fatalf("config missing allow_dev_login: %s", rec.Body.String())
	}
}

func TestDevLoginDisabledReturns404(t *testing.T) {
	h := &httpapi.AuthHandlers{AllowDevLogin: false}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/dev-login", strings.NewReader(`{"name":"X"}`))
	h.DevLogin(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when dev login disabled, got %d", rec.Code)
	}
}
```
> These two need no DB. The full register/login happy paths (cookie + 201/200) are covered through the service-layer DB tests in Task 3; the handler tests here assert the two HTTP-only behaviors that don't require a DB (config field + the dev-login gate). Ensure `net/http`, `net/http/httptest`, `strings`, `httpapi` are imported (already used by existing tests).

- [ ] **Step 2: Run to verify it fails**

```bash
cd backend && go test ./internal/httpapi/ -run 'TestConfigIncludesAllowDevLogin|TestDevLoginDisabledReturns404'
```
Expected: FAIL — `AllowDevLogin`/`DevLogin` undefined.

- [ ] **Step 3: Config** — in `config.go` add the field + load line:
```go
	AllowDevLogin     bool
```
```go
		AllowDevLogin:     getenv("ALLOW_DEV_LOGIN") == "true",
```

- [ ] **Step 4: Handlers** — in `auth_handlers.go`:

Add the field to the struct:
```go
	AllowDevLogin bool
```
Add shared helpers and refactor the existing cookie code in `Login` to use `issueSession`:
```go
func (a *AuthHandlers) issueSession(w http.ResponseWriter, user gen.User) error {
	token, err := a.Sessions.Issue(user.ID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: "pp_session", Value: token, Path: "/",
		HttpOnly: true, Secure: a.CookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: time.Now().Add(30 * 24 * time.Hour), MaxAge: 30 * 24 * 60 * 60,
	})
	return nil
}

func userResponse(u gen.User) map[string]any {
	return map[string]any{"id": u.ID, "display_name": u.DisplayName, "avatar_url": u.AvatarUrl}
}
```
Refactor the tail of `Login` (replace the inline `Sessions.Issue` + `http.SetCookie` + `WriteJSON`) with:
```go
	if err := a.issueSession(w, user); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, userResponse(user))
```
(`Login` already imports `gen` via `service`; add `"github.com/jaydee94/pit-pilot/backend/internal/store/gen"` to the imports.)

Update `Config`:
```go
func (a *AuthHandlers) Config(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{
		"google_client_id": a.GoogleClientID,
		"allow_dev_login":  a.AllowDevLogin,
	})
}
```
Add the three handlers:
```go
func (a *AuthHandlers) Register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "could not parse body"))
		return
	}
	user, err := a.Users.Register(r.Context(), body.Email, body.Password, body.DisplayName)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.issueSession(w, user); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, userResponse(user))
}

func (a *AuthHandlers) PasswordLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "could not parse body"))
		return
	}
	user, err := a.Users.LoginWithPassword(r.Context(), body.Email, body.Password)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.issueSession(w, user); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, userResponse(user))
}

func (a *AuthHandlers) DevLogin(w http.ResponseWriter, r *http.Request) {
	if !a.AllowDevLogin {
		WriteError(w, apperr.NotFound("not_found", "not found"))
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "could not parse body"))
		return
	}
	user, err := a.Users.DevLogin(r.Context(), body.Name)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.issueSession(w, user); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, userResponse(user))
}
```
> `apperr.NotFound` must map to HTTP 404 via `WriteError` (it does — `apperr.Error.HTTPStatus`). Confirm `apperr.NotFound(code, message string)` exists (it's listed among the constructors); if the signature differs, match it.

- [ ] **Step 5: Routes + wiring** — in `router.go`, inside the `if d.Auth != nil { ... }` block:
```go
		r.Post("/auth/register", d.Auth.Register)
		r.Post("/auth/login", d.Auth.PasswordLogin)
		r.Post("/auth/dev-login", d.Auth.DevLogin)
```
In `cmd/api/main.go`, add to the `AuthHandlers` literal:
```go
		AllowDevLogin: cfg.AllowDevLogin,
```

- [ ] **Step 6: Run tests + build + vet + full module**

```bash
cd backend && go test ./internal/httpapi/ -run 'TestConfigIncludesAllowDevLogin|TestDevLoginDisabledReturns404' -v && go build ./cmd/api && go vet ./... && rm -f api && go test ./...
```
Expected: PASS, builds, vet clean, full module green.

- [ ] **Step 7: Commit**

```bash
git add backend && git commit -m "feat: register/login/dev-login auth endpoints + ALLOW_DEV_LOGIN"
```

---

## Task 5: Frontend client helpers

**Files:**
- Modify: `frontend/src/api/client.ts`
- Test: `frontend/src/api/passwordauth.test.ts`

**Interfaces:**
- Produces: `register(email, password, displayName)`, `passwordLogin(email, password)`, `devLogin(name)`; `getAuthConfig()` return type `{ google_client_id: string; allow_dev_login: boolean }`.

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/api/passwordauth.test.ts
import { afterEach, expect, test, vi } from "vitest";
import { register, passwordLogin, devLogin } from "./client";

afterEach(() => vi.restoreAllMocks());

function captureFetch() {
  const calls: Array<{ url: string; init?: RequestInit }> = [];
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url: String(url), init });
    return new Response(JSON.stringify({ id: "u1", display_name: "U", avatar_url: null }), { status: 200 });
  }));
  return calls;
}

test("register posts email/password/display_name to /auth/register", async () => {
  const calls = captureFetch();
  await register("a@b.de", "secret12", "Alice");
  expect(calls[0].url).toContain("/auth/register");
  expect(calls[0].init?.method).toBe("POST");
  expect(JSON.parse(String(calls[0].init?.body))).toEqual({ email: "a@b.de", password: "secret12", display_name: "Alice" });
});

test("passwordLogin posts to /auth/login", async () => {
  const calls = captureFetch();
  await passwordLogin("a@b.de", "secret12");
  expect(calls[0].url).toContain("/auth/login");
  expect(JSON.parse(String(calls[0].init?.body))).toEqual({ email: "a@b.de", password: "secret12" });
});

test("devLogin posts the name to /auth/dev-login", async () => {
  const calls = captureFetch();
  await devLogin("Tester");
  expect(calls[0].url).toContain("/auth/dev-login");
  expect(JSON.parse(String(calls[0].init?.body))).toEqual({ name: "Tester" });
});
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd frontend && npm run test -- --run passwordauth
```
Expected: FAIL — helpers not exported.

- [ ] **Step 3: Implement** — append to `client.ts`; update `getAuthConfig`'s type:

```ts
export const register = (email: string, password: string, displayName: string) =>
  apiFetch<{ id: string; display_name: string; avatar_url: string | null }>("/auth/register", {
    method: "POST",
    body: JSON.stringify({ email, password, display_name: displayName }),
  });

export const passwordLogin = (email: string, password: string) =>
  apiFetch<{ id: string; display_name: string; avatar_url: string | null }>("/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  });

export const devLogin = (name: string) =>
  apiFetch<{ id: string; display_name: string; avatar_url: string | null }>("/auth/dev-login", {
    method: "POST",
    body: JSON.stringify({ name }),
  });
```
Change the existing `getAuthConfig` return type to include `allow_dev_login`:
```ts
export const getAuthConfig = () =>
  apiFetch<{ google_client_id: string; allow_dev_login: boolean }>("/auth/config");
```

- [ ] **Step 4: Run + build**

```bash
cd frontend && npm run test -- --run passwordauth && npm run build
```
Expected: PASS + build clean.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api && git commit -m "feat: register/passwordLogin/devLogin client helpers"
```

---

## Task 6: Login screen — email/password form + dummy block

**Files:**
- Modify: `frontend/src/routes/Login.tsx`
- Test: `frontend/src/routes/Login.test.tsx` (extend)

**Interfaces:**
- Consumes: `register`, `passwordLogin`, `devLogin`, `getAuthConfig` (now with `allow_dev_login`), `login`/`initGoogleSignIn` (unchanged), `useAuth().refresh`, `useNavigate`.

- [ ] **Step 1: Write the failing tests** (append to `Login.test.tsx`; reuse its existing `wrap()` + fetch-stub style)

```tsx
test("password login posts to /auth/login", async () => {
  const calls: string[] = [];
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url);
    if (u.endsWith("/auth/config")) return new Response(JSON.stringify({ google_client_id: "", allow_dev_login: false }), { status: 200 });
    calls.push(u);
    if (u.endsWith("/auth/login")) return new Response(JSON.stringify({ id: "u1", display_name: "U", avatar_url: null }), { status: 200 });
    return new Response(JSON.stringify({ error: { code: "x", message: "x" } }), { status: 401 });
  }));
  render(wrap());
  await screen.findByLabelText(/e-mail/i);
  fireEvent.change(screen.getByLabelText(/e-mail/i), { target: { value: "a@b.de" } });
  fireEvent.change(screen.getByLabelText(/passwort/i), { target: { value: "secret12" } });
  fireEvent.click(screen.getByRole("button", { name: /^anmelden$/i }));
  await waitFor(() => expect(calls.some((u) => u.endsWith("/auth/login"))).toBe(true));
});

test("dummy block only renders when allow_dev_login is true", async () => {
  vi.stubGlobal("fetch", vi.fn(async (url: string) =>
    String(url).endsWith("/auth/config")
      ? new Response(JSON.stringify({ google_client_id: "", allow_dev_login: true }), { status: 200 })
      : new Response(JSON.stringify({ error: { code: "x", message: "x" } }), { status: 401 })));
  render(wrap());
  await waitFor(() => expect(screen.getByRole("button", { name: /dev-login/i })).toBeInTheDocument());
});
```
> Add `fireEvent` to the existing `@testing-library/react` import in this file.

- [ ] **Step 2: Run to verify it fails**

```bash
cd frontend && npm run test -- --run Login
```
Expected: FAIL — no email/password form, no dev-login button.

- [ ] **Step 3: Rewrite `Login.tsx`** (keeps the Google GIS logic from the prior feature; adds the form + dummy block)

```tsx
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { login, register, passwordLogin, devLogin, getAuthConfig } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { initGoogleSignIn } from "../auth/google";

export default function Login() {
  const [clientId, setClientId] = useState<string | null>(null);
  const [allowDev, setAllowDev] = useState(false);
  const [configured, setConfigured] = useState<boolean | null>(null);
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [pw, setPw] = useState("");
  const [name, setName] = useState("");
  const [devName, setDevName] = useState("");
  const [err, setErr] = useState("");
  const btnRef = useRef<HTMLDivElement>(null);
  const nav = useNavigate();
  const { refresh } = useAuth();

  useEffect(() => {
    getAuthConfig()
      .then((c) => {
        setClientId(c.google_client_id || null);
        setAllowDev(Boolean(c.allow_dev_login));
        setConfigured(Boolean(c.google_client_id));
      })
      .catch(() => setConfigured(false));
  }, []);

  useEffect(() => {
    if (!clientId || !btnRef.current) return;
    initGoogleSignIn({
      clientId,
      buttonParent: btnRef.current,
      onCredential: async (idToken) => {
        try {
          await login("google", idToken);
          refresh();
          nav("/groups");
        } catch (e: unknown) {
          setErr(e instanceof Error ? e.message : "Login fehlgeschlagen");
        }
      },
    }).catch(() => setErr("Google-Login konnte nicht geladen werden"));
  }, [clientId, refresh, nav]);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr("");
    try {
      if (mode === "register") {
        if (pw.length < 8) {
          setErr("Passwort muss mindestens 8 Zeichen haben.");
          return;
        }
        await register(email, pw, name);
      } else {
        await passwordLogin(email, pw);
      }
      refresh();
      nav("/groups");
    } catch (e2: unknown) {
      setErr(e2 instanceof Error ? e2.message : "Anmeldung fehlgeschlagen");
    }
  };

  const onDev = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr("");
    try {
      await devLogin(devName);
      refresh();
      nav("/groups");
    } catch (e2: unknown) {
      setErr(e2 instanceof Error ? e2.message : "Dev-Login fehlgeschlagen");
    }
  };

  return (
    <main>
      <h1>pit-pilot</h1>

      <div>
        <button type="button" onClick={() => setMode("login")} aria-pressed={mode === "login"}>
          Anmelden
        </button>
        <button type="button" onClick={() => setMode("register")} aria-pressed={mode === "register"}>
          Registrieren
        </button>
      </div>

      <form onSubmit={onSubmit}>
        {mode === "register" && (
          <label>
            Anzeigename
            <input value={name} onChange={(e) => setName(e.target.value)} />
          </label>
        )}
        <label>
          E-Mail
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
        </label>
        <label>
          Passwort
          <input type="password" value={pw} onChange={(e) => setPw(e.target.value)} />
        </label>
        <button type="submit">{mode === "register" ? "Registrieren" : "Anmelden"}</button>
      </form>

      {configured && (
        <>
          <p>— oder —</p>
          <div ref={btnRef} />
        </>
      )}

      {allowDev && (
        <form onSubmit={onDev}>
          <label>
            Name
            <input value={devName} onChange={(e) => setDevName(e.target.value)} />
          </label>
          <button type="submit">Dev-Login</button>
        </form>
      )}

      {err && <p role="alert">{err}</p>}
    </main>
  );
}
```
> Note: the submit button and the register-toggle button both contain "Registrieren"/"Anmelden". The test selects the submit via `getByRole("button", { name: /^anmelden$/i })` in login mode — in login mode the toggle shows "Anmelden" (aria-pressed) AND the submit shows "Anmelden", so two buttons match. To disambiguate, give the submit button a distinct accessible name: change the submit text to `{mode === "register" ? "Konto erstellen" : "Einloggen"}` and update the test selector to `/einloggen/i`. Apply this: submit label `Einloggen`/`Konto erstellen`; the toggle stays `Anmelden`/`Registrieren`.

- [ ] **Step 4: Reconcile the existing Login tests**

The prior feature's Login tests assert the not-configured note and GIS init. Keep them working: the "nicht konfiguriert" branch was tied to `configured === false`. This rewrite dropped that note — instead, re-add it so the prior test passes, OR update that test. Simplest: keep a `{configured === false && <p>Google-Login ist nicht konfiguriert.</p>}` line above the Google block. Re-add it. Then run:
```bash
cd frontend && npm run test -- --run Login
```
Expected: PASS (old + new tests). Fix selectors per the Step 3 note until green.

- [ ] **Step 5: Full suite + build**

```bash
cd frontend && npm run test -- --run && npm run build
```
Expected: all PASS, build clean.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/routes/Login.tsx frontend/src/routes/Login.test.tsx && git commit -m "feat: email/password form + dev dummy-login on the login screen"
```

---

## Task 7: Enable dummy login in the compose stack + docs

**Files:**
- Modify: `docker-compose.yml`
- Modify: `.env.example`

- [ ] **Step 1: Add the env to the compose api service**

In `docker-compose.yml`, under the `api` service's `environment:`, add (after `COOKIE_SECURE`):
```yaml
      ALLOW_DEV_LOGIN: "true"
```

- [ ] **Step 2: Document it in `.env.example`**

Append:
```dotenv
# Dev-only dummy login (type a name -> logged in). The compose stack sets this
# to true; leave it false/unset in production. Never enable in a real deployment.
ALLOW_DEV_LOGIN=false
```

- [ ] **Step 3: Verify compose config**

```bash
cd /Users/jaydee/git/pit-pilot && docker compose config >/dev/null && echo COMPOSE_OK
```
Expected: `COMPOSE_OK`.

- [ ] **Step 4: Commit**

```bash
git add docker-compose.yml .env.example && git commit -m "chore: enable dummy login in the compose stack"
```

---

## Self-Review

**Spec coverage (each spec section → task):**
- §3 migration + queries → Task 1.
- §4 `password` package → Task 2; `UserService` methods → Task 3; config flag + handlers + routes + `/auth/config` extension + `issueSession` helper + wiring → Task 4.
- §5 client helpers → Task 5; Login form + toggle + dummy block → Task 6.
- §6 compose `ALLOW_DEV_LOGIN` + docs → Task 7.
- §7 tests → password (T2), service (T3), handlers + config (T4), client (T5), Login (T6).
- Security (§7): identical 401 message (T3 `LoginWithPassword`), `password_hash` never serialized (`userResponse` omits it, T4), dev-login 404 when disabled (T4).

**Type consistency:** `gen.User.PasswordHash *string` (nullable text) consumed as `&hash` in `Register` and `*user.PasswordHash` in `LoginWithPassword`. `CreatePasswordUserParams{Email, DisplayName, PasswordHash}`. Handlers return `userResponse(user)` (id/display_name/avatar_url only). `getAuthConfig` return type updated in T5 and consumed in T6.

**Resolved during planning:**
- Login-mode button-name collision (toggle vs submit) — submit relabeled `Einloggen`/`Konto erstellen` so `getByRole` is unambiguous (Task 6 Step 3 note).
- The prior feature's "nicht konfiguriert" note must be re-added in the rewrite so its test stays green (Task 6 Step 4).
- The CHECK constraint name (`users_provider_check`) must be confirmed against Postgres when the migration runs (Task 1 Step 1 note).
- Handler happy-paths needing a DB are covered at the service layer (Task 3); the handler tests (Task 4) assert only the DB-free behaviors (config field, dev-login 404 gate).
```
