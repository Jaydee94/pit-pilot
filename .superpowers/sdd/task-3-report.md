# Task 3 Report: UserService password + dummy methods

## Methods implemented (backend/internal/service/users.go)

### Register(ctx, email, pw, displayName) (gen.User, error)
- Trims email; rejects empty email (BadRequest "invalid_email") and password shorter than 8 chars (BadRequest "weak_password").
- Calls `password.Hash` to produce argon2id PHC string.
- Calls `gen.CreatePasswordUser`; detects unique-violation via `isUniqueViolation` and returns `apperr.Conflict("email_taken", ...)`.

### LoginWithPassword(ctx, email, pw) (gen.User, error)
- Calls `gen.GetPasswordUserByEmail`; maps `pgx.ErrNoRows` to `apperr.Unauthorized("invalid_credentials", "invalid email or password")`.
- Guards nil `PasswordHash` (same error, same message).
- Calls `password.Verify`; any error or `ok==false` returns the same identical Unauthorized error.
- **No-enumeration property**: all three failure paths (unknown email, nil hash, wrong password) produce the exact same `apperr.Error{Code:"invalid_credentials", Message:"invalid email or password"}`, preventing user-enumeration attacks.

### DevLogin(ctx, name) (gen.User, error)
- Trims name; rejects empty with BadRequest.
- Delegates to `gen.UpsertDummyUser` — idempotent by design.

### isUniqueViolation(err) bool
- Uses `errors.As` to unwrap to `*pgconn.PgError` and checks SQLSTATE code `"23505"`.

## TDD Red → Green

**RED output (before implementation):**
```
internal/service/users_test.go:50:16: svc.Register undefined
internal/service/users_test.go:68:18: svc.LoginWithPassword undefined
internal/service/users_test.go:84:16: svc.DevLogin undefined
FAIL github.com/jaydee94/pit-pilot/backend/internal/service [build failed]
```

**GREEN output (after implementation):**
```
--- PASS: TestRegisterAndLoginWithPassword (4.60s)
--- PASS: TestDevLoginIsIdempotent (1.15s)
PASS
ok  github.com/jaydee94/pit-pilot/backend/internal/service 6.148s
go vet: (clean)
```

## Self-review: no-enumeration identical-message property

`LoginWithPassword` has three distinct failure modes:
1. `pgx.ErrNoRows` — email not in DB
2. `user.PasswordHash == nil` — user exists but has no password (e.g. OAuth-only account)
3. `password.Verify` returns false or error

All three return `apperr.Unauthorized("invalid_credentials", "invalid email or password")` with no additional context. An attacker cannot distinguish between a non-existent account, an account with a different auth provider, or a wrong password. This is the intended behaviour.

## Commit
`a102c15 feat: UserService register/login-with-password/dev-login`

---

## Fix Report (post-review hardening)

### Fix 1 — nil PasswordHash on successful service return

Two sites patched in `backend/internal/service/users.go`:

- `Register`: `user.PasswordHash = nil` inserted immediately before `return user, nil` (after `CreatePasswordUser` succeeds).
- `LoginWithPassword`: `user.PasswordHash = nil` inserted immediately before `return user, nil` (after `password.Verify` returns true; the nil-out is deliberately *after* `Verify` so the hash is still available for the check).

### Fix 2 — typed error assertions in users_test.go

`apperr.As` signature used:
```go
func As(err error) (*apperr.Error, bool)   // field: HTTPStatus int
```

Assertions added to `TestRegisterAndLoginWithPassword`:

| Case | Previous assertion | New assertion |
|---|---|---|
| Duplicate email | `err == nil` (plain non-nil) | `apperr.As(err)` + `HTTPStatus != 409` |
| Short password | `err == nil` (plain non-nil) | `apperr.As(err)` + `HTTPStatus != 400` |
| Wrong password | `err == nil` (plain non-nil) | `apperr.As(err)` + `HTTPStatus != 401` |
| Unknown email | `err == nil` (plain non-nil) | `apperr.As(err)` + `HTTPStatus != 401` |

Two additional nil-hash guards added:
- After `Register` succeeds: assert `u.PasswordHash == nil`.
- After `LoginWithPassword` succeeds: assert `got.PasswordHash == nil`.

### Fix 3 — displayName trim in Register

`displayName = strings.TrimSpace(displayName)` added at the top of `Register`, alongside the existing email trim. No new validation added.

### Test + vet output

```
--- PASS: TestRegisterAndLoginWithPassword (1.56s)
--- PASS: TestDevLoginIsIdempotent (1.14s)
PASS
ok  github.com/jaydee94/pit-pilot/backend/internal/service  3.131s
go vet: (clean)
```

### Commit
`fix: nil password_hash on service return; assert error codes in tests`

---

## Pre-merge Fix Report (security hardening)

### Fix 1 — Timing-based user enumeration (Important / security)

Added a package-level decoy hash in `backend/internal/service/users.go`:

```go
var decoyHash, _ = password.Hash("pit-pilot-login-timing-decoy")
```

In `LoginWithPassword`, both fast-return paths now run `_, _ = password.Verify(pw, decoyHash)` before returning `Unauthorized`:
- `pgx.ErrNoRows` path (unknown email)
- `user.PasswordHash == nil` path (OAuth-only account)

The genuine wrong-password path was unchanged (already runs `password.Verify`). All three failure paths now consume approximately the same Argon2 CPU time, closing the ~20 ms timing side-channel.

No new test: unknown-email and wrong-password paths still both return 401 with identical message/code — the existing assertions confirm behavioral equivalence.

### Fix 2 — Email case-folding / silent lockout (Important)

`Register`: changed `email = strings.TrimSpace(email)` to `email = strings.ToLower(strings.TrimSpace(email))` so `provider_sub` and `email` columns are stored lowercase.

`LoginWithPassword`: same normalization applied before `GetPasswordUserByEmail`, so lookup always matches stored lowercase value.

New test assertion added to `TestRegisterAndLoginWithPassword`:
- Register with `"Mixed@Example.com"`, then login with `"mixed@example.com"` — succeeds and returns the same user ID.

### Fix 3 — Display name required (Minor)

After `displayName = strings.TrimSpace(displayName)` in `Register`, added:

```go
if displayName == "" {
    return gen.User{}, apperr.BadRequest("invalid_display_name", "display name required")
}
```

New test assertion: `Register(ctx, "x2@example.com", "longenough8", "   ")` returns HTTPStatus 400.

### Test + vet + build output

```
--- PASS: TestRegisterAndLoginWithPassword (4.78s)
--- PASS: TestDevLoginIsIdempotent (1.15s)
PASS
ok  github.com/jaydee94/pit-pilot/backend/internal/service  6.383s

go vet: (clean)
go build ./cmd/api: (clean)
Full module: all 12 packages PASS
```

### Commit
`fix: equalize login timing, case-fold email, require display name`
