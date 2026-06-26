# pit-pilot Cycle 1 (Vertical MVP) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the end-to-end core flow of pit-pilot — login with Google/Apple, create/join groups by invite code, create concerts, and RSVP Yes/No — as a Go API + Postgres + minimal React PWA, deployable on Kubernetes.

**Architecture:** A single Go REST service in three layers (HTTP handlers → service/domain → sqlc store), backed by Postgres. Auth verifies a provider OIDC ID token, then issues its own session JWT in an httpOnly cookie. A React+Vite PWA consumes the API. Everything ships as containers with Kustomize manifests for Kubernetes.

**Tech Stack:** Go 1.26, `chi` v5 router, `pgx` v5 + `sqlc`, `golang-migrate`, `golang-jwt/jwt` v5, `coreos/go-oidc` v3, `testcontainers-go`; React 18 + Vite + TypeScript + TanStack Query + Vitest; Docker (distroless / nginx), Kustomize, kind.

## Global Constraints

- Go module path: `github.com/jaydee94/pit-pilot/backend`; Go version floor `1.26` (use the latest Go; local toolchain is 1.26.4).
- **Always use the latest stable version of every library, tool, and base image.** Any version number shown in this plan (Go module versions, `npm` dependency ranges, `sqlc@…`, Docker image tags) is illustrative only — at implementation time fetch the current latest: `go get <module>@latest`, `npm install <pkg>@latest`, `sqlc@latest`, and the newest stable Docker base-image tags. After pulling, run the tests; if a latest version introduces a breaking change, fix forward to the new API (do not pin to an older version to avoid the work) — and note it in the task report.
- Strict TDD on every unit: write the failing test first, watch it fail, implement minimally, watch it pass, commit. No implementation code without a failing test first.
- Primary keys are UUIDs. Money is stored as integer **cents**, never floats.
- RSVP status is exactly `'yes'` or `'no'`. Group role is exactly `'admin'` or `'member'`. Provider is exactly `'google'` or `'apple'`.
- API errors use JSON shape `{"error":{"code":"...","message":"..."}}` with HTTP status 400/401/403/404/409.
- Session cookie: name `pp_session`, `HttpOnly`, `SameSite=Lax`, `Secure` controlled by config, 30-day TTL.
- Layer discipline: handlers never touch SQL; services never import `net/http`; the store never contains domain rules.
- Every task ends on a green test run and a commit.

## File Structure

```
backend/
  go.mod
  sqlc.yaml
  Dockerfile
  cmd/api/main.go                 # process entrypoint, wiring
  internal/
    config/config.go              # env -> Config
    apperr/apperr.go              # typed domain errors + HTTP mapping
    auth/
      session.go                  # session JWT issue/validate
      verifier.go                 # IDTokenVerifier interface + Identity
      oidc_verifier.go            # real go-oidc implementation
    store/
      migrations/0001_init.up.sql
      migrations/0001_init.down.sql
      queries/users.sql
      queries/groups.sql
      queries/concerts.sql
      queries/rsvps.sql
      gen/                        # sqlc-generated (db.go, models.go, *.sql.go)
    service/
      users.go                    # upsert-on-login
      groups.go                   # create/list/invite/join + membership authz
      concerts.go                 # create/list/detail
      rsvps.go                    # set/list
    httpapi/
      router.go                   # route table
      render.go                   # JSON + error rendering helpers
      middleware.go               # session + group-membership middleware
      auth_handlers.go
      group_handlers.go
      concert_handlers.go
      rsvp_handlers.go
    testutil/
      postgres.go                 # testcontainers Postgres + migrate helper
frontend/
  package.json, vite.config.ts, tsconfig.json
  src/
    api/client.ts                 # typed fetch wrapper
    auth/AuthContext.tsx
    routes/                       # Login, Groups, GroupDetail, ConcertForm, ConcertDetail
    components/
  Dockerfile, nginx.conf
deploy/
  base/                           # api, frontend, postgres, ingress, migrate-job
  overlays/dev/
.github/workflows/ci.yml
```

---

## Phase 0 — Scaffolding & Test Harness

### Task 0.1: Initialize Go module and CI skeleton

**Files:**
- Create: `backend/go.mod`
- Create: `.github/workflows/ci.yml`
- Create: `backend/internal/version/version_test.go`
- Create: `backend/internal/version/version.go`

**Interfaces:**
- Produces: `version.String() string` — sanity target proving the toolchain + CI work.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/version/version_test.go
package version

import "testing"

func TestStringIsNotEmpty(t *testing.T) {
	if String() == "" {
		t.Fatal("version.String() must not be empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go mod init github.com/jaydee94/pit-pilot/backend && go test ./internal/version/...
```
Expected: FAIL — `undefined: String`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/version/version.go
package version

// String returns the current build identifier.
func String() string { return "0.1.0-dev" }
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/version/...
```
Expected: PASS.

- [ ] **Step 5: Add CI workflow**

```yaml
# .github/workflows/ci.yml
name: ci
on:
  push:
  pull_request:
jobs:
  backend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: cd backend && go vet ./... && go test ./...
  frontend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      - run: cd frontend && npm ci && npm run test -- --run && npm run build
```
> Note: the `frontend` job will fail until Phase 5 creates `frontend/`. That is expected; it goes green when Phase 5 lands. Backend job must be green now.

- [ ] **Step 6: Commit**

```bash
git add backend .github && git commit -m "chore: scaffold go module, version sanity test, CI skeleton"
```

---

### Task 0.2: Config from environment

**Files:**
- Create: `backend/internal/config/config.go`
- Test: `backend/internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `type Config struct { DatabaseURL string; SessionSigningKey []byte; GoogleClientID, AppleClientID, Port string; CookieSecure bool }`
  - `func Load(getenv func(string) string) (Config, error)` — pure, takes a getenv func for testability.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/config/config_test.go
package config

import "testing"

func TestLoadRequiresDatabaseURL(t *testing.T) {
	_, err := Load(func(string) string { return "" })
	if err == nil {
		t.Fatal("expected error when DATABASE_URL is missing")
	}
}

func TestLoadReadsValues(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL":        "postgres://localhost/pp",
		"SESSION_SIGNING_KEY": "supersecretkey",
		"GOOGLE_CLIENT_ID":    "gid",
		"APPLE_CLIENT_ID":     "aid",
		"COOKIE_SECURE":       "true",
	}
	cfg, err := Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DatabaseURL != "postgres://localhost/pp" || string(cfg.SessionSigningKey) != "supersecretkey" {
		t.Fatalf("config not parsed: %+v", cfg)
	}
	if !cfg.CookieSecure || cfg.Port != "8080" {
		t.Fatalf("defaults/bools wrong: %+v", cfg)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/config/...
```
Expected: FAIL — `undefined: Load`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/config/config.go
package config

import "fmt"

type Config struct {
	DatabaseURL       string
	SessionSigningKey []byte
	GoogleClientID    string
	AppleClientID     string
	Port              string
	CookieSecure      bool
}

// Load builds Config from a getenv function. DATABASE_URL and
// SESSION_SIGNING_KEY are required.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		DatabaseURL:       getenv("DATABASE_URL"),
		SessionSigningKey: []byte(getenv("SESSION_SIGNING_KEY")),
		GoogleClientID:    getenv("GOOGLE_CLIENT_ID"),
		AppleClientID:     getenv("APPLE_CLIENT_ID"),
		Port:              getenv("PORT"),
		CookieSecure:      getenv("COOKIE_SECURE") == "true",
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if len(cfg.SessionSigningKey) == 0 {
		return Config{}, fmt.Errorf("SESSION_SIGNING_KEY is required")
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	return cfg, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/config/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config && git commit -m "feat: load config from environment"
```

---

### Task 0.3: Database migrations (schema)

**Files:**
- Create: `backend/internal/store/migrations/0001_init.up.sql`
- Create: `backend/internal/store/migrations/0001_init.down.sql`
- Create: `backend/sqlc.yaml`

**Interfaces:**
- Produces: the Postgres schema (`users`, `groups`, `group_members`, `concerts`, `rsvps`) and sqlc config consumed by Task 0.5+.

- [ ] **Step 1: Write the up migration**

```sql
-- backend/internal/store/migrations/0001_init.up.sql
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE users (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider     text NOT NULL,
    provider_sub text NOT NULL,
    email        text NOT NULL DEFAULT '',
    display_name text NOT NULL DEFAULT '',
    avatar_url   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_sub),
    CHECK (provider IN ('google','apple'))
);

CREATE TABLE groups (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    invite_code text NOT NULL UNIQUE,
    created_by  uuid NOT NULL REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE group_members (
    group_id  uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role      text NOT NULL,
    joined_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id),
    CHECK (role IN ('admin','member'))
);

CREATE TABLE concerts (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id      uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    artist        text NOT NULL,
    event_at      timestamptz NOT NULL,
    venue         text,
    city          text,
    ticket_url    text,
    price_cents   integer,
    notes         text,
    rsvp_deadline timestamptz NOT NULL,
    created_by    uuid NOT NULL REFERENCES users(id),
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE rsvps (
    concert_id   uuid NOT NULL REFERENCES concerts(id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status       text NOT NULL,
    responded_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (concert_id, user_id),
    CHECK (status IN ('yes','no'))
);

CREATE INDEX idx_concerts_group_event ON concerts (group_id, event_at);
CREATE INDEX idx_group_members_user ON group_members (user_id);
```

> Decision resolved from the spec's open question: `rsvp_deadline` is `NOT NULL` — the deadline is a required product concept and there is no reason to model it as optional in the database. The API requires it on creation.

- [ ] **Step 2: Write the down migration**

```sql
-- backend/internal/store/migrations/0001_init.down.sql
DROP TABLE IF EXISTS rsvps;
DROP TABLE IF EXISTS concerts;
DROP TABLE IF EXISTS group_members;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS users;
```

- [ ] **Step 3: Write sqlc config**

```yaml
# backend/sqlc.yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "internal/store/queries"
    schema: "internal/store/migrations"
    gen:
      go:
        package: "gen"
        out: "internal/store/gen"
        sql_package: "pgx/v5"
        emit_json_tags: true
        emit_pointers_for_null_types: true
        overrides:
          - db_type: "uuid"
            go_type: "github.com/google/uuid.UUID"
```

- [ ] **Step 4: Commit**

```bash
git add backend/internal/store/migrations backend/sqlc.yaml && git commit -m "feat: initial postgres schema and sqlc config"
```

> No test here: this task is pure schema/config consumed and verified by Task 0.4 (migration harness test). It is committed separately because a reviewer can reject schema design independently.

---

### Task 0.4: Postgres test harness (testcontainers)

**Files:**
- Create: `backend/internal/testutil/postgres.go`
- Test: `backend/internal/testutil/postgres_test.go`

**Interfaces:**
- Produces: `func NewPostgres(t *testing.T) *pgxpool.Pool` — spins up a throwaway Postgres, runs all migrations, returns a ready pool; auto-cleans via `t.Cleanup`.

- [ ] **Step 1: Add dependencies**

```bash
cd backend && \
go get github.com/jackc/pgx/v5/pgxpool && \
go get github.com/testcontainers/testcontainers-go/modules/postgres && \
go get github.com/golang-migrate/migrate/v4 && \
go get github.com/google/uuid
```

- [ ] **Step 2: Write the failing test**

```go
// backend/internal/testutil/postgres_test.go
package testutil

import (
	"context"
	"testing"
)

func TestNewPostgresRunsMigrations(t *testing.T) {
	pool := NewPostgres(t)
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables WHERE table_name='users'`).Scan(&n)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected users table to exist, got count %d", n)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd backend && go test ./internal/testutil/...
```
Expected: FAIL — `undefined: NewPostgres`.

- [ ] **Step 4: Write minimal implementation**

```go
// backend/internal/testutil/postgres.go
package testutil

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// NewPostgres starts a disposable Postgres container, applies all migrations,
// and returns a connected pool. Cleanup is registered on t.
func NewPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("pp"),
		tcpostgres.WithUsername("pp"),
		tcpostgres.WithPassword("pp"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	_, file, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(file), "..", "store", "migrations")
	m, err := migrate.New("file://"+migrationsDir, dsn)
	if err != nil {
		t.Fatalf("migrate init: %v", err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/testutil/...
```
Expected: PASS (requires Docker running locally).

- [ ] **Step 6: Commit**

```bash
git add backend && git commit -m "test: postgres testcontainers harness with migrations"
```

---

### Task 0.5: Generate sqlc query layer for users

**Files:**
- Create: `backend/internal/store/queries/users.sql`
- Generate: `backend/internal/store/gen/*` (via `sqlc generate`)
- Test: `backend/internal/store/gen/users_gen_test.go`

**Interfaces:**
- Produces (sqlc-generated, consumed by services):
  - `gen.New(db gen.DBTX) *gen.Queries`
  - `(*Queries).UpsertUser(ctx, gen.UpsertUserParams) (gen.User, error)` where params = `{Provider, ProviderSub, Email, DisplayName string; AvatarUrl *string}`
  - `(*Queries).GetUserByID(ctx, uuid.UUID) (gen.User, error)`
  - `gen.User` struct with fields `ID uuid.UUID; Provider, ProviderSub, Email, DisplayName string; AvatarUrl *string; CreatedAt time.Time`

- [ ] **Step 1: Write the query file**

```sql
-- backend/internal/store/queries/users.sql

-- name: UpsertUser :one
INSERT INTO users (provider, provider_sub, email, display_name, avatar_url)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (provider, provider_sub) DO UPDATE
    SET email = EXCLUDED.email,
        display_name = EXCLUDED.display_name,
        avatar_url = EXCLUDED.avatar_url
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;
```

- [ ] **Step 2: Generate and verify it fails first**

Install sqlc if needed, then generate:
```bash
cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
```
Then write a test that exercises generated code:
```go
// backend/internal/store/gen/users_gen_test.go
package gen_test

import (
	"context"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestUpsertUserIsIdempotentOnProviderSub(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	ctx := context.Background()

	u1, err := q.UpsertUser(ctx, gen.UpsertUserParams{
		Provider: "google", ProviderSub: "sub-1", Email: "a@x.io", DisplayName: "Ann",
	})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	u2, err := q.UpsertUser(ctx, gen.UpsertUserParams{
		Provider: "google", ProviderSub: "sub-1", Email: "a@x.io", DisplayName: "Annie",
	})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if u1.ID != u2.ID {
		t.Fatalf("expected same user id, got %s vs %s", u1.ID, u2.ID)
	}
	if u2.DisplayName != "Annie" {
		t.Fatalf("expected display name updated, got %q", u2.DisplayName)
	}
}
```

- [ ] **Step 3: Run test to verify it fails (before generation) / passes (after)**

```bash
cd backend && go test ./internal/store/gen/...
```
Expected: FAIL if generation not yet run (package empty), PASS after `sqlc generate`. Add `github.com/sqlc-dev/sqlc` to tools and re-run generate until PASS.

- [ ] **Step 4: Commit**

```bash
git add backend && git commit -m "feat: sqlc user queries (upsert/get) with generated store"
```

---

## Phase 1 — Authentication

### Task 1.1: Session JWT issue/validate

**Files:**
- Create: `backend/internal/auth/session.go`
- Test: `backend/internal/auth/session_test.go`

**Interfaces:**
- Produces:
  - `type SessionManager struct { ... }`
  - `func NewSessionManager(key []byte, ttl time.Duration, now func() time.Time) *SessionManager`
  - `func (m *SessionManager) Issue(userID uuid.UUID) (string, error)`
  - `func (m *SessionManager) Validate(token string) (uuid.UUID, error)` — returns error on bad signature or expiry.

- [ ] **Step 1: Add dependency**

```bash
cd backend && go get github.com/golang-jwt/jwt/v5
```

- [ ] **Step 2: Write the failing test**

```go
// backend/internal/auth/session_test.go
package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func fixedNow() time.Time { return time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC) }

func TestSessionRoundTrip(t *testing.T) {
	m := NewSessionManager([]byte("k"), time.Hour, fixedNow)
	id := uuid.New()
	tok, err := m.Issue(id)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	got, err := m.Validate(tok)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got != id {
		t.Fatalf("round-trip mismatch: %s != %s", got, id)
	}
}

func TestSessionRejectsExpired(t *testing.T) {
	m := NewSessionManager([]byte("k"), time.Hour, fixedNow)
	tok, _ := m.Issue(uuid.New())
	expired := NewSessionManager([]byte("k"), time.Hour,
		func() time.Time { return fixedNow().Add(2 * time.Hour) })
	if _, err := expired.Validate(tok); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestSessionRejectsWrongKey(t *testing.T) {
	tok, _ := NewSessionManager([]byte("k1"), time.Hour, fixedNow).Issue(uuid.New())
	if _, err := NewSessionManager([]byte("k2"), time.Hour, fixedNow).Validate(tok); err == nil {
		t.Fatal("expected wrong-key token to be rejected")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd backend && go test ./internal/auth/...
```
Expected: FAIL — `undefined: NewSessionManager`.

- [ ] **Step 4: Write minimal implementation**

```go
// backend/internal/auth/session.go
package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type SessionManager struct {
	key []byte
	ttl time.Duration
	now func() time.Time
}

func NewSessionManager(key []byte, ttl time.Duration, now func() time.Time) *SessionManager {
	if now == nil {
		now = time.Now
	}
	return &SessionManager{key: key, ttl: ttl, now: now}
}

func (m *SessionManager) Issue(userID uuid.UUID) (string, error) {
	now := m.now()
	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.key)
}

func (m *SessionManager) Validate(token string) (uuid.UUID, error) {
	parsed, err := jwt.ParseWithClaims(token, &jwt.RegisteredClaims{},
		func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return m.key, nil
		},
		jwt.WithTimeFunc(m.now),
	)
	if err != nil {
		return uuid.Nil, err
	}
	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok || !parsed.Valid {
		return uuid.Nil, fmt.Errorf("invalid token")
	}
	return uuid.Parse(claims.Subject)
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/auth/...
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/auth && git commit -m "feat: session JWT issue/validate with injectable clock"
```

---

### Task 1.2: ID token verifier interface + fake

**Files:**
- Create: `backend/internal/auth/verifier.go`
- Test: `backend/internal/auth/verifier_test.go`

**Interfaces:**
- Produces:
  - `type Identity struct { Provider, Subject, Email, Name, PictureURL string }`
  - `type IDTokenVerifier interface { Verify(ctx context.Context, provider, rawToken string) (Identity, error) }`
  - `type FakeVerifier struct { Identities map[string]Identity; Err error }` implementing the interface, keyed by rawToken — used by service/handler tests.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/auth/verifier_test.go
package auth

import (
	"context"
	"errors"
	"testing"
)

func TestFakeVerifierReturnsMappedIdentity(t *testing.T) {
	var v IDTokenVerifier = &FakeVerifier{
		Identities: map[string]Identity{
			"tok-abc": {Provider: "google", Subject: "s1", Email: "a@x.io", Name: "Ann"},
		},
	}
	id, err := v.Verify(context.Background(), "google", "tok-abc")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if id.Subject != "s1" || id.Provider != "google" {
		t.Fatalf("unexpected identity: %+v", id)
	}
}

func TestFakeVerifierUnknownTokenErrors(t *testing.T) {
	v := &FakeVerifier{Identities: map[string]Identity{}}
	if _, err := v.Verify(context.Background(), "google", "nope"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/auth/ -run Verifier
```
Expected: FAIL — `undefined: IDTokenVerifier`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/auth/verifier.go
package auth

import (
	"context"
	"errors"
)

var ErrInvalidToken = errors.New("invalid id token")

type Identity struct {
	Provider   string
	Subject    string
	Email      string
	Name       string
	PictureURL string
}

type IDTokenVerifier interface {
	Verify(ctx context.Context, provider, rawToken string) (Identity, error)
}

// FakeVerifier is a test double mapping raw tokens to identities.
type FakeVerifier struct {
	Identities map[string]Identity
	Err        error
}

func (f *FakeVerifier) Verify(_ context.Context, provider, rawToken string) (Identity, error) {
	if f.Err != nil {
		return Identity{}, f.Err
	}
	id, ok := f.Identities[rawToken]
	if !ok {
		return Identity{}, ErrInvalidToken
	}
	return id, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/auth/ -run Verifier
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/auth && git commit -m "feat: IDTokenVerifier interface and fake double"
```

---

### Task 1.3: Real OIDC verifier (Google/Apple)

**Files:**
- Create: `backend/internal/auth/oidc_verifier.go`
- Test: `backend/internal/auth/oidc_verifier_test.go`

**Interfaces:**
- Consumes: `Identity`, `IDTokenVerifier`, `ErrInvalidToken` from Task 1.2.
- Produces: `func NewOIDCVerifier(ctx context.Context, googleAud, appleAud string) (*OIDCVerifier, error)` implementing `IDTokenVerifier` by dispatching to the provider's discovery + JWKS.

- [ ] **Step 1: Add dependency**

```bash
cd backend && go get github.com/coreos/go-oidc/v3/oidc
```

- [ ] **Step 2: Write the failing test (constructor + unknown provider)**

```go
// backend/internal/auth/oidc_verifier_test.go
package auth

import (
	"context"
	"testing"
)

func TestOIDCVerifierRejectsUnknownProvider(t *testing.T) {
	v := &OIDCVerifier{} // verifiers map nil → unknown provider path
	if _, err := v.Verify(context.Background(), "facebook", "x"); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
```
> Network-dependent verification against live Google/Apple JWKS is covered by manual/integration testing, not unit tests. The unit test pins the unknown-provider branch; the construction path is exercised in the smoke test of Task 1.6.

- [ ] **Step 3: Run test to verify it fails**

```bash
cd backend && go test ./internal/auth/ -run OIDC
```
Expected: FAIL — `undefined: OIDCVerifier`.

- [ ] **Step 4: Write minimal implementation**

```go
// backend/internal/auth/oidc_verifier.go
package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

type oidcClaims struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

type OIDCVerifier struct {
	verifiers map[string]*oidc.IDTokenVerifier
}

// NewOIDCVerifier wires Google and Apple issuers with their audiences.
func NewOIDCVerifier(ctx context.Context, googleAud, appleAud string) (*OIDCVerifier, error) {
	v := &OIDCVerifier{verifiers: map[string]*oidc.IDTokenVerifier{}}
	google, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, fmt.Errorf("google provider: %w", err)
	}
	v.verifiers["google"] = google.Verifier(&oidc.Config{ClientID: googleAud})
	apple, err := oidc.NewProvider(ctx, "https://appleid.apple.com")
	if err != nil {
		return nil, fmt.Errorf("apple provider: %w", err)
	}
	v.verifiers["apple"] = apple.Verifier(&oidc.Config{ClientID: appleAud})
	return v, nil
}

func (v *OIDCVerifier) Verify(ctx context.Context, provider, rawToken string) (Identity, error) {
	ver, ok := v.verifiers[provider]
	if !ok {
		return Identity{}, fmt.Errorf("%w: unknown provider %q", ErrInvalidToken, provider)
	}
	tok, err := ver.Verify(ctx, rawToken)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	var c oidcClaims
	if err := tok.Claims(&c); err != nil {
		return Identity{}, fmt.Errorf("%w: claims: %v", ErrInvalidToken, err)
	}
	return Identity{
		Provider: provider, Subject: tok.Subject,
		Email: c.Email, Name: c.Name, PictureURL: c.Picture,
	}, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/auth/ -run OIDC
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/auth && git commit -m "feat: real OIDC verifier for google/apple id tokens"
```

### Task 1.4: Typed domain errors + HTTP mapping

**Files:**
- Create: `backend/internal/apperr/apperr.go`
- Test: `backend/internal/apperr/apperr_test.go`

**Interfaces:**
- Produces:
  - `type Error struct { Code string; Message string; HTTPStatus int }` implementing `error`.
  - Constructors used everywhere: `apperr.NotFound(code, msg)`, `apperr.Forbidden(code, msg)`, `apperr.BadRequest(code, msg)`, `apperr.Conflict(code, msg)`, `apperr.Unauthorized(code, msg)`.
  - `func As(err error) (*Error, bool)` — unwrap helper for the render layer.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/apperr/apperr_test.go
package apperr

import (
	"fmt"
	"net/http"
	"testing"
)

func TestConstructorsSetStatus(t *testing.T) {
	cases := []struct {
		err  *Error
		want int
	}{
		{NotFound("group_not_found", "x"), http.StatusNotFound},
		{Forbidden("not_member", "x"), http.StatusForbidden},
		{BadRequest("invalid", "x"), http.StatusBadRequest},
		{Conflict("dup", "x"), http.StatusConflict},
		{Unauthorized("no_session", "x"), http.StatusUnauthorized},
	}
	for _, c := range cases {
		if c.err.HTTPStatus != c.want {
			t.Fatalf("%s: status %d, want %d", c.err.Code, c.err.HTTPStatus, c.want)
		}
	}
}

func TestAsUnwrapsWrapped(t *testing.T) {
	base := NotFound("group_not_found", "missing")
	wrapped := fmt.Errorf("service: %w", base)
	got, ok := As(wrapped)
	if !ok || got.Code != "group_not_found" {
		t.Fatalf("As failed to unwrap: %v %v", got, ok)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/apperr/...
```
Expected: FAIL — `undefined: Error`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/apperr/apperr.go
package apperr

import (
	"errors"
	"net/http"
)

type Error struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func new(status int, code, msg string) *Error {
	return &Error{Code: code, Message: msg, HTTPStatus: status}
}

func NotFound(code, msg string) *Error     { return new(http.StatusNotFound, code, msg) }
func Forbidden(code, msg string) *Error     { return new(http.StatusForbidden, code, msg) }
func BadRequest(code, msg string) *Error    { return new(http.StatusBadRequest, code, msg) }
func Conflict(code, msg string) *Error      { return new(http.StatusConflict, code, msg) }
func Unauthorized(code, msg string) *Error  { return new(http.StatusUnauthorized, code, msg) }

// As unwraps err to a *Error if present anywhere in the chain.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/apperr/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/apperr && git commit -m "feat: typed domain errors with http status mapping"
```

---

### Task 1.5: User login service (verify token → upsert user)

**Files:**
- Create: `backend/internal/service/users.go`
- Test: `backend/internal/service/users_test.go`

**Interfaces:**
- Consumes: `auth.IDTokenVerifier`, `auth.FakeVerifier`, `gen.Queries.UpsertUser`, `testutil.NewPostgres`.
- Produces:
  - `type UserService struct { ... }`
  - `func NewUserService(q *gen.Queries, v auth.IDTokenVerifier) *UserService`
  - `func (s *UserService) LoginWithIDToken(ctx, provider, rawToken string) (gen.User, error)` — verifies, upserts, returns the user. Returns `apperr.Unauthorized` when the token is invalid.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/service/users_test.go
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/auth"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestLoginUpsertsUser(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	v := &auth.FakeVerifier{Identities: map[string]auth.Identity{
		"tok": {Provider: "google", Subject: "sub-9", Email: "z@x.io", Name: "Zed"},
	}}
	svc := service.NewUserService(q, v)

	u, err := svc.LoginWithIDToken(context.Background(), "google", "tok")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if u.DisplayName != "Zed" || u.Email != "z@x.io" {
		t.Fatalf("unexpected user: %+v", u)
	}
}

func TestLoginRejectsInvalidToken(t *testing.T) {
	pool := testutil.NewPostgres(t)
	svc := service.NewUserService(gen.New(pool), &auth.FakeVerifier{Identities: map[string]auth.Identity{}})
	_, err := svc.LoginWithIDToken(context.Background(), "google", "bad")
	appErr, ok := apperr.As(err)
	if !ok || appErr.HTTPStatus != 401 {
		t.Fatalf("expected 401 apperr, got %v", err)
	}
	if !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("expected wrapped ErrInvalidToken, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/service/ -run Login
```
Expected: FAIL — `undefined: service.NewUserService`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/service/users.go
package service

import (
	"context"
	"fmt"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/auth"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type UserService struct {
	q *gen.Queries
	v auth.IDTokenVerifier
}

func NewUserService(q *gen.Queries, v auth.IDTokenVerifier) *UserService {
	return &UserService{q: q, v: v}
}

func (s *UserService) LoginWithIDToken(ctx context.Context, provider, rawToken string) (gen.User, error) {
	id, err := s.v.Verify(ctx, provider, rawToken)
	if err != nil {
		return gen.User{}, fmt.Errorf("%w", &apperr.Error{
			Code: "invalid_id_token", Message: "could not verify id token",
			HTTPStatus: 401,
		})
	}
	var avatar *string
	if id.PictureURL != "" {
		avatar = &id.PictureURL
	}
	user, err := s.q.UpsertUser(ctx, gen.UpsertUserParams{
		Provider: id.Provider, ProviderSub: id.Subject,
		Email: id.Email, DisplayName: id.Name, AvatarUrl: avatar,
	})
	if err != nil {
		return gen.User{}, fmt.Errorf("upsert user: %w", err)
	}
	return user, nil
}
```
> The test asserts `errors.Is(err, auth.ErrInvalidToken)`. To satisfy both the apperr unwrap and the sentinel, wrap both: replace the error return with `fmt.Errorf("%w: %w", &apperr.Error{Code:"invalid_id_token",Message:"could not verify id token",HTTPStatus:401}, auth.ErrInvalidToken)`. Use Go 1.20+ multi-`%w`.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/service/ -run Login
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service && git commit -m "feat: user login service verifies token and upserts user"
```

---

### Task 1.6: HTTP render helpers + auth handlers + session middleware

**Files:**
- Create: `backend/internal/httpapi/render.go`
- Create: `backend/internal/httpapi/middleware.go`
- Create: `backend/internal/httpapi/auth_handlers.go`
- Test: `backend/internal/httpapi/auth_handlers_test.go`

**Interfaces:**
- Produces:
  - `func WriteJSON(w http.ResponseWriter, status int, v any)`
  - `func WriteError(w http.ResponseWriter, err error)` — maps `apperr.Error`→its status, anything else→500 with code `internal`.
  - `type ctxKey` + `func UserID(r *http.Request) (uuid.UUID, bool)`
  - `func (a *AuthHandlers) SessionMiddleware(next http.Handler) http.Handler` — reads `pp_session` cookie, validates, injects user id; 401 via WriteError if missing/invalid.
  - `type AuthHandlers struct { Users *service.UserService; Sessions *auth.SessionManager; CookieSecure bool }`
  - `func (a *AuthHandlers) Login(provider string) http.HandlerFunc` and `func (a *AuthHandlers) Logout(w, r)` and `func (a *AuthHandlers) Me(w, r)`.

- [ ] **Step 1: Add dependency**

```bash
cd backend && go get github.com/go-chi/chi/v5
```

- [ ] **Step 2: Write the failing test**

```go
// backend/internal/httpapi/auth_handlers_test.go
package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/auth"
	"github.com/jaydee94/pit-pilot/backend/internal/httpapi"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func newAuthHandlers(t *testing.T) *httpapi.AuthHandlers {
	pool := testutil.NewPostgres(t)
	v := &auth.FakeVerifier{Identities: map[string]auth.Identity{
		"good": {Provider: "google", Subject: "s1", Email: "a@x.io", Name: "Ann"},
	}}
	return &httpapi.AuthHandlers{
		Users:    service.NewUserService(gen.New(pool), v),
		Sessions: auth.NewSessionManager([]byte("k"), time.Hour, nil),
	}
}

func TestLoginSetsCookie(t *testing.T) {
	h := newAuthHandlers(t)
	req := httptest.NewRequest(http.MethodPost, "/auth/google",
		strings.NewReader(`{"id_token":"good"}`))
	rec := httptest.NewRecorder()
	h.Login("google")(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Set-Cookie"), "pp_session=") {
		t.Fatalf("expected session cookie, got %q", rec.Header().Get("Set-Cookie"))
	}
}

func TestLoginRejectsBadToken(t *testing.T) {
	h := newAuthHandlers(t)
	req := httptest.NewRequest(http.MethodPost, "/auth/google",
		strings.NewReader(`{"id_token":"nope"}`))
	rec := httptest.NewRecorder()
	h.Login("google")(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestSessionMiddlewareInjectsUser(t *testing.T) {
	h := newAuthHandlers(t)
	// login to obtain a cookie
	loginReq := httptest.NewRequest(http.MethodPost, "/auth/google", strings.NewReader(`{"id_token":"good"}`))
	loginRec := httptest.NewRecorder()
	h.Login("google")(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	var sawUser bool
	protected := h.SessionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := httpapi.UserID(r); ok {
			sawUser = true
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req = req.WithContext(context.Background())
	req.AddCookie(cookie)
	protected.ServeHTTP(httptest.NewRecorder(), req)
	if !sawUser {
		t.Fatal("expected user id injected into context")
	}
}

func TestSessionMiddlewareRejectsMissingCookie(t *testing.T) {
	h := newAuthHandlers(t)
	rec := httptest.NewRecorder()
	h.SessionMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd backend && go test ./internal/httpapi/ -run 'Login|Session'
```
Expected: FAIL — `undefined: httpapi.AuthHandlers`.

- [ ] **Step 4: Write minimal implementation**

```go
// backend/internal/httpapi/render.go
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
)

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func WriteError(w http.ResponseWriter, err error) {
	if e, ok := apperr.As(err); ok {
		WriteJSON(w, e.HTTPStatus, map[string]any{
			"error": map[string]string{"code": e.Code, "message": e.Message},
		})
		return
	}
	WriteJSON(w, http.StatusInternalServerError, map[string]any{
		"error": map[string]string{"code": "internal", "message": "internal error"},
	})
}
```

```go
// backend/internal/httpapi/middleware.go
package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
)

type ctxKey int

const userIDKey ctxKey = iota

func UserID(r *http.Request) (uuid.UUID, bool) {
	id, ok := r.Context().Value(userIDKey).(uuid.UUID)
	return id, ok
}

func withUserID(r *http.Request, id uuid.UUID) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userIDKey, id))
}

func (a *AuthHandlers) SessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("pp_session")
		if err != nil {
			WriteError(w, apperr.Unauthorized("no_session", "authentication required"))
			return
		}
		id, err := a.Sessions.Validate(c.Value)
		if err != nil {
			WriteError(w, apperr.Unauthorized("invalid_session", "session invalid or expired"))
			return
		}
		next.ServeHTTP(w, withUserID(r, id))
	})
}
```

```go
// backend/internal/httpapi/auth_handlers.go
package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/auth"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
)

type AuthHandlers struct {
	Users        *service.UserService
	Sessions     *auth.SessionManager
	CookieSecure bool
}

func (a *AuthHandlers) Login(provider string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDToken string `json:"id_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.IDToken == "" {
			WriteError(w, apperr.BadRequest("invalid_body", "id_token required"))
			return
		}
		user, err := a.Users.LoginWithIDToken(r.Context(), provider, body.IDToken)
		if err != nil {
			WriteError(w, err)
			return
		}
		token, err := a.Sessions.Issue(user.ID)
		if err != nil {
			WriteError(w, err)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: "pp_session", Value: token, Path: "/",
			HttpOnly: true, Secure: a.CookieSecure, SameSite: http.SameSiteLaxMode,
			Expires: time.Now().Add(30 * 24 * time.Hour),
		})
		WriteJSON(w, http.StatusOK, map[string]any{
			"id": user.ID, "display_name": user.DisplayName, "avatar_url": user.AvatarUrl,
		})
	}
}

func (a *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: "pp_session", Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: a.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (a *AuthHandlers) Me(w http.ResponseWriter, r *http.Request) {
	id, ok := UserID(r)
	if !ok {
		WriteError(w, apperr.Unauthorized("no_session", "authentication required"))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"id": id})
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/httpapi/ -run 'Login|Session'
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/httpapi && git commit -m "feat: auth handlers, session middleware, json render helpers"
```

## Phase 2 — Groups

### Task 2.1: Groups sqlc queries

**Files:**
- Create: `backend/internal/store/queries/groups.sql`
- Generate: regenerate `backend/internal/store/gen/`
- Test: `backend/internal/store/gen/groups_gen_test.go`

**Interfaces:**
- Produces (generated, consumed by the group service):
  - `CreateGroup(ctx, CreateGroupParams{Name, InviteCode string; CreatedBy uuid.UUID}) (Group, error)`
  - `AddGroupMember(ctx, AddGroupMemberParams{GroupID, UserID uuid.UUID; Role string}) error`
  - `ListGroupsForUser(ctx, uuid.UUID) ([]Group, error)`
  - `GetGroup(ctx, uuid.UUID) (Group, error)`
  - `GetGroupByInviteCode(ctx, string) (Group, error)`
  - `GetGroupMember(ctx, GetGroupMemberParams{GroupID, UserID uuid.UUID}) (GroupMember, error)`
  - `ListGroupMembers(ctx, uuid.UUID) ([]ListGroupMembersRow, error)` (row = user id, display_name, avatar_url, role)
  - `UpdateGroupInviteCode(ctx, UpdateGroupInviteCodeParams{ID uuid.UUID; InviteCode string}) (Group, error)`

- [ ] **Step 1: Write the query file**

```sql
-- backend/internal/store/queries/groups.sql

-- name: CreateGroup :one
INSERT INTO groups (name, invite_code, created_by)
VALUES ($1, $2, $3) RETURNING *;

-- name: AddGroupMember :exec
INSERT INTO group_members (group_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (group_id, user_id) DO NOTHING;

-- name: ListGroupsForUser :many
SELECT g.* FROM groups g
JOIN group_members m ON m.group_id = g.id
WHERE m.user_id = $1
ORDER BY g.created_at DESC;

-- name: GetGroup :one
SELECT * FROM groups WHERE id = $1;

-- name: GetGroupByInviteCode :one
SELECT * FROM groups WHERE invite_code = $1;

-- name: GetGroupMember :one
SELECT * FROM group_members WHERE group_id = $1 AND user_id = $2;

-- name: ListGroupMembers :many
SELECT u.id, u.display_name, u.avatar_url, m.role
FROM group_members m
JOIN users u ON u.id = m.user_id
WHERE m.group_id = $1
ORDER BY m.joined_at;

-- name: UpdateGroupInviteCode :one
UPDATE groups SET invite_code = $2 WHERE id = $1 RETURNING *;
```

- [ ] **Step 2: Regenerate**

```bash
cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
```

- [ ] **Step 3: Write the failing test**

```go
// backend/internal/store/gen/groups_gen_test.go
package gen_test

import (
	"context"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestListGroupsForUserReturnsJoinedGroups(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	owner, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "o", DisplayName: "O"})
	g, err := q.CreateGroup(ctx, gen.CreateGroupParams{Name: "Metal", InviteCode: "ABC123", CreatedBy: owner.ID})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := q.AddGroupMember(ctx, gen.AddGroupMemberParams{GroupID: g.ID, UserID: owner.ID, Role: "admin"}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	groups, err := q.ListGroupsForUser(ctx, owner.ID)
	if err != nil || len(groups) != 1 || groups[0].Name != "Metal" {
		t.Fatalf("unexpected groups: %v err=%v", groups, err)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/store/gen/ -run Groups
```
Expected: PASS (FAIL before regeneration).

- [ ] **Step 5: Commit**

```bash
git add backend && git commit -m "feat: sqlc group queries"
```

---

### Task 2.2: Group service (create/list/invite/join + membership authz)

**Files:**
- Create: `backend/internal/service/groups.go`
- Test: `backend/internal/service/groups_test.go`

**Interfaces:**
- Consumes: `*gen.Queries`, `*pgxpool.Pool`, group queries from Task 2.1, `apperr`.
- Produces:
  - `type GroupService struct { ... }`
  - `func NewGroupService(pool *pgxpool.Pool, q *gen.Queries, newCode func() string) *GroupService`
  - `func (s *GroupService) Create(ctx, userID uuid.UUID, name string) (gen.Group, error)` — inserts group + adds creator as `admin` in one transaction.
  - `func (s *GroupService) ListForUser(ctx, userID uuid.UUID) ([]gen.Group, error)`
  - `func (s *GroupService) Join(ctx, userID uuid.UUID, code string) (gen.Group, error)` — `apperr.NotFound` on bad code; idempotent membership as `member`.
  - `func (s *GroupService) RegenerateInvite(ctx, userID, groupID uuid.UUID) (gen.Group, error)` — admin-only (`apperr.Forbidden`).
  - `func (s *GroupService) Members(ctx, userID, groupID uuid.UUID) ([]gen.ListGroupMembersRow, error)` — member-only.
  - `func (s *GroupService) RequireMembership(ctx, userID, groupID uuid.UUID) (gen.GroupMember, error)` — returns `apperr.Forbidden("not_member",...)` if not a member; reused by concert/RSVP services and middleware.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/service/groups_test.go
package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func seedUser(t *testing.T, q *gen.Queries, sub string) gen.User {
	t.Helper()
	u, err := q.UpsertUser(context.Background(), gen.UpsertUserParams{
		Provider: "google", ProviderSub: sub, DisplayName: sub})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u
}

func newGroupSvc(t *testing.T) (*service.GroupService, *gen.Queries) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	codes := []string{"CODE01", "CODE02", "CODE03"}
	i := 0
	svc := service.NewGroupService(pool, q, func() string { c := codes[i%len(codes)]; i++; return c })
	return svc, q
}

func TestCreateGroupMakesCreatorAdmin(t *testing.T) {
	svc, q := newGroupSvc(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")

	g, err := svc.Create(ctx, owner.ID, "Festival Squad")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	m, err := svc.RequireMembership(ctx, owner.ID, g.ID)
	if err != nil || m.Role != "admin" {
		t.Fatalf("creator should be admin, got %v err=%v", m, err)
	}
}

func TestJoinByCodeAddsMember(t *testing.T) {
	svc, q := newGroupSvc(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	joiner := seedUser(t, q, "joiner")
	g, _ := svc.Create(ctx, owner.ID, "Crew")

	joined, err := svc.Join(ctx, joiner.ID, g.InviteCode)
	if err != nil || joined.ID != g.ID {
		t.Fatalf("join: %v err=%v", joined, err)
	}
	if _, err := svc.RequireMembership(ctx, joiner.ID, g.ID); err != nil {
		t.Fatalf("joiner should be member: %v", err)
	}
}

func TestJoinUnknownCodeIsNotFound(t *testing.T) {
	svc, q := newGroupSvc(t)
	joiner := seedUser(t, q, "joiner")
	_, err := svc.Join(context.Background(), joiner.ID, "WRONG!")
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 404 {
		t.Fatalf("expected 404, got %v", err)
	}
}

func TestRequireMembershipForbidsNonMember(t *testing.T) {
	svc, q := newGroupSvc(t)
	owner := seedUser(t, q, "owner")
	stranger := seedUser(t, q, "stranger")
	g, _ := svc.Create(context.Background(), owner.ID, "Private")
	_, err := svc.RequireMembership(context.Background(), stranger.ID, g.ID)
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 403 {
		t.Fatalf("expected 403, got %v", err)
	}
	_ = uuid.Nil
}

func TestRegenerateInviteRequiresAdmin(t *testing.T) {
	svc, q := newGroupSvc(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	member := seedUser(t, q, "member")
	g, _ := svc.Create(ctx, owner.ID, "Crew")
	_, _ = svc.Join(ctx, member.ID, g.InviteCode)

	if _, err := svc.RegenerateInvite(ctx, member.ID, g.ID); err == nil {
		t.Fatal("member must not regenerate invite")
	}
	g2, err := svc.RegenerateInvite(ctx, owner.ID, g.ID)
	if err != nil || g2.InviteCode == g.InviteCode {
		t.Fatalf("admin regenerate failed: %v err=%v", g2, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/service/ -run 'Group|Join|Require|Regenerate|Create'
```
Expected: FAIL — `undefined: service.NewGroupService`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/service/groups.go
package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type GroupService struct {
	pool    *pgxpool.Pool
	q       *gen.Queries
	newCode func() string
}

func NewGroupService(pool *pgxpool.Pool, q *gen.Queries, newCode func() string) *GroupService {
	return &GroupService{pool: pool, q: q, newCode: newCode}
}

func (s *GroupService) Create(ctx context.Context, userID uuid.UUID, name string) (gen.Group, error) {
	if name == "" {
		return gen.Group{}, apperr.BadRequest("invalid_name", "group name required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return gen.Group{}, err
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	g, err := qtx.CreateGroup(ctx, gen.CreateGroupParams{
		Name: name, InviteCode: s.newCode(), CreatedBy: userID})
	if err != nil {
		return gen.Group{}, err
	}
	if err := qtx.AddGroupMember(ctx, gen.AddGroupMemberParams{
		GroupID: g.ID, UserID: userID, Role: "admin"}); err != nil {
		return gen.Group{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return gen.Group{}, err
	}
	return g, nil
}

func (s *GroupService) ListForUser(ctx context.Context, userID uuid.UUID) ([]gen.Group, error) {
	return s.q.ListGroupsForUser(ctx, userID)
}

func (s *GroupService) Join(ctx context.Context, userID uuid.UUID, code string) (gen.Group, error) {
	g, err := s.q.GetGroupByInviteCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return gen.Group{}, apperr.NotFound("invalid_invite", "invite code not found")
	}
	if err != nil {
		return gen.Group{}, err
	}
	if err := s.q.AddGroupMember(ctx, gen.AddGroupMemberParams{
		GroupID: g.ID, UserID: userID, Role: "member"}); err != nil {
		return gen.Group{}, err
	}
	return g, nil
}

func (s *GroupService) RequireMembership(ctx context.Context, userID, groupID uuid.UUID) (gen.GroupMember, error) {
	m, err := s.q.GetGroupMember(ctx, gen.GetGroupMemberParams{GroupID: groupID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return gen.GroupMember{}, apperr.Forbidden("not_member", "not a member of this group")
	}
	if err != nil {
		return gen.GroupMember{}, err
	}
	return m, nil
}

func (s *GroupService) Members(ctx context.Context, userID, groupID uuid.UUID) ([]gen.ListGroupMembersRow, error) {
	if _, err := s.RequireMembership(ctx, userID, groupID); err != nil {
		return nil, err
	}
	return s.q.ListGroupMembers(ctx, groupID)
}

func (s *GroupService) RegenerateInvite(ctx context.Context, userID, groupID uuid.UUID) (gen.Group, error) {
	m, err := s.RequireMembership(ctx, userID, groupID)
	if err != nil {
		return gen.Group{}, err
	}
	if m.Role != "admin" {
		return gen.Group{}, apperr.Forbidden("not_admin", "admin role required")
	}
	return s.q.UpdateGroupInviteCode(ctx, gen.UpdateGroupInviteCodeParams{
		ID: groupID, InviteCode: s.newCode()})
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/service/ -run 'Group|Join|Require|Regenerate|Create'
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service && git commit -m "feat: group service with membership authorization"
```

---

### Task 2.3: Group HTTP handlers + group-membership middleware

**Files:**
- Create: `backend/internal/httpapi/group_handlers.go`
- Modify: `backend/internal/httpapi/middleware.go` (add `RequireGroupMember`)
- Test: `backend/internal/httpapi/group_handlers_test.go`

**Interfaces:**
- Consumes: `GroupService`, `UserID`, `WriteJSON/WriteError`, chi URL params.
- Produces:
  - `type GroupHandlers struct { Groups *service.GroupService }`
  - HTTP handlers: `Create`, `List`, `Join`, `Members`, `Invite` (regenerate) bound to the routes from the spec.
  - `func RequireGroupMember(groups *service.GroupService) func(http.Handler) http.Handler` — reads `{groupID}` URL param + `UserID`, 403 if not a member; used by concert/RSVP routes.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/httpapi/group_handlers_test.go
package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/httpapi"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

// reqAs builds a request whose context already carries userID, bypassing the
// session middleware so handler logic is tested directly.
func reqAs(method, target string, body string, userID uuid.UUID) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	return httpapi.WithUserIDForTest(r, userID)
}

func TestCreateAndListGroups(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	codes := []string{"AAA111", "BBB222"}
	i := 0
	gs := service.NewGroupService(pool, q, func() string { c := codes[i%len(codes)]; i++; return c })
	h := &httpapi.GroupHandlers{Groups: gs}

	owner, _ := q.UpsertUser(reqAs(http.MethodGet, "/", "", uuid.Nil).Context(),
		gen.UpsertUserParams{Provider: "google", ProviderSub: "o", DisplayName: "O"})

	rec := httptest.NewRecorder()
	h.Create(rec, reqAs(http.MethodPost, "/groups", `{"name":"Crew"}`, owner.ID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body.String())
	}

	listRec := httptest.NewRecorder()
	h.List(listRec, reqAs(http.MethodGet, "/groups", "", owner.ID))
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), "Crew") {
		t.Fatalf("list failed: %d %s", listRec.Code, listRec.Body.String())
	}
	_ = chi.NewRouter
}
```
> Add a test-only helper `WithUserIDForTest` in the production package guarded for tests; simplest is to export a thin wrapper around `withUserID` in a `testing.go`-style file. Implementation shown in Step 3.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/httpapi/ -run Groups
```
Expected: FAIL — `undefined: httpapi.GroupHandlers`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/httpapi/group_handlers.go
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
)

type GroupHandlers struct {
	Groups *service.GroupService
}

// WithUserIDForTest injects a user id into the request context (test helper).
func WithUserIDForTest(r *http.Request, id uuid.UUID) *http.Request { return withUserID(r, id) }

func (h *GroupHandlers) Create(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "name required"))
		return
	}
	g, err := h.Groups.Create(r.Context(), uid, body.Name)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, g)
}

func (h *GroupHandlers) List(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	groups, err := h.Groups.ListForUser(r.Context(), uid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, groups)
}

func (h *GroupHandlers) Join(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "code required"))
		return
	}
	g, err := h.Groups.Join(r.Context(), uid, body.Code)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, g)
}

func (h *GroupHandlers) Members(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	gid, err := uuid.Parse(chi.URLParam(r, "groupID"))
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad group id"))
		return
	}
	members, err := h.Groups.Members(r.Context(), uid, gid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, members)
}

func (h *GroupHandlers) Invite(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	gid, err := uuid.Parse(chi.URLParam(r, "groupID"))
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad group id"))
		return
	}
	g, err := h.Groups.RegenerateInvite(r.Context(), uid, gid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"invite_code": g.InviteCode})
}
```

Add the membership middleware to `middleware.go`:
```go
// append to backend/internal/httpapi/middleware.go

import (
	// keep existing imports; add:
	"github.com/go-chi/chi/v5"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
)

// RequireGroupMember ensures the session user is a member of {groupID}.
func RequireGroupMember(groups *service.GroupService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid, ok := UserID(r)
			if !ok {
				WriteError(w, apperr.Unauthorized("no_session", "authentication required"))
				return
			}
			gid, err := uuid.Parse(chi.URLParam(r, "groupID"))
			if err != nil {
				WriteError(w, apperr.BadRequest("invalid_id", "bad group id"))
				return
			}
			if _, err := groups.RequireMembership(r.Context(), uid, gid); err != nil {
				WriteError(w, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```
> Merge the two import blocks in `middleware.go` into one; `uuid` and `apperr` are already imported there.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/httpapi/ -run Groups
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi && git commit -m "feat: group handlers and group-membership middleware"
```

## Phase 3 — Concerts

### Task 3.1: Concerts sqlc queries

**Files:**
- Create: `backend/internal/store/queries/concerts.sql`
- Generate: regenerate `backend/internal/store/gen/`
- Test: `backend/internal/store/gen/concerts_gen_test.go`

**Interfaces:**
- Produces (generated):
  - `CreateConcert(ctx, CreateConcertParams) (Concert, error)` where params = `{GroupID uuid.UUID; Artist string; EventAt time.Time; Venue, City, TicketUrl *string; PriceCents *int32; Notes *string; RsvpDeadline time.Time; CreatedBy uuid.UUID}`
  - `ListConcertsForGroup(ctx, uuid.UUID) ([]Concert, error)`
  - `GetConcert(ctx, uuid.UUID) (Concert, error)`

- [ ] **Step 1: Write the query file**

```sql
-- backend/internal/store/queries/concerts.sql

-- name: CreateConcert :one
INSERT INTO concerts
    (group_id, artist, event_at, venue, city, ticket_url, price_cents, notes, rsvp_deadline, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- name: ListConcertsForGroup :many
SELECT * FROM concerts WHERE group_id = $1 ORDER BY event_at;

-- name: GetConcert :one
SELECT * FROM concerts WHERE id = $1;
```

- [ ] **Step 2: Regenerate**

```bash
cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
```

- [ ] **Step 3: Write the failing test**

```go
// backend/internal/store/gen/concerts_gen_test.go
package gen_test

import (
	"context"
	"testing"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestCreateAndListConcert(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	owner, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "o", DisplayName: "O"})
	g, _ := q.CreateGroup(ctx, gen.CreateGroupParams{Name: "G", InviteCode: "X1", CreatedBy: owner.ID})

	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, err := q.CreateConcert(ctx, gen.CreateConcertParams{
		GroupID: g.ID, Artist: "Tool", EventAt: when,
		RsvpDeadline: when.Add(-14 * 24 * time.Hour), CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatalf("create concert: %v", err)
	}
	list, err := q.ListConcertsForGroup(ctx, g.ID)
	if err != nil || len(list) != 1 || list[0].ID != c.ID {
		t.Fatalf("list concerts: %v err=%v", list, err)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/store/gen/ -run Concert
```
Expected: PASS (FAIL before regeneration).

- [ ] **Step 5: Commit**

```bash
git add backend && git commit -m "feat: sqlc concert queries"
```

---

### Task 3.2: Concert service (create/list/detail, member-gated)

**Files:**
- Create: `backend/internal/service/concerts.go`
- Test: `backend/internal/service/concerts_test.go`

**Interfaces:**
- Consumes: `*gen.Queries`, `*GroupService.RequireMembership`, `apperr`.
- Produces:
  - `type ConcertInput struct { Artist string; EventAt time.Time; Venue, City, TicketURL, Notes string; PriceCents *int32; RSVPDeadline time.Time }`
  - `type ConcertService struct { ... }`
  - `func NewConcertService(q *gen.Queries, groups *GroupService) *ConcertService`
  - `func (s *ConcertService) Create(ctx, userID, groupID uuid.UUID, in ConcertInput) (gen.Concert, error)` — member-gated; validates `Artist != ""`, `EventAt` set, `RSVPDeadline` set and not after `EventAt`.
  - `func (s *ConcertService) ListForGroup(ctx, userID, groupID uuid.UUID) ([]gen.Concert, error)` — member-gated.
  - `func (s *ConcertService) Get(ctx, userID, concertID uuid.UUID) (gen.Concert, error)` — loads concert, then gates on its group membership; `apperr.NotFound` if missing.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/service/concerts_test.go
package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func newConcertSetup(t *testing.T) (*service.ConcertService, *service.GroupService, *gen.Queries) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "INV" + string(rune('A'+n)) })
	return service.NewConcertService(q, gs), gs, q
}

func TestCreateConcertMemberOnly(t *testing.T) {
	cs, gs, q := newConcertSetup(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	stranger := seedUser(t, q, "stranger")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	in := service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-72 * time.Hour)}

	if _, err := cs.Create(ctx, stranger.ID, g.ID, in); err == nil {
		t.Fatal("stranger must not create concert")
	}
	c, err := cs.Create(ctx, owner.ID, g.ID, in)
	if err != nil || c.Artist != "Tool" {
		t.Fatalf("owner create failed: %v err=%v", c, err)
	}
}

func TestCreateConcertRejectsDeadlineAfterEvent(t *testing.T) {
	cs, gs, q := newConcertSetup(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	in := service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(time.Hour)}
	_, err := cs.Create(ctx, owner.ID, g.ID, in)
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 400 {
		t.Fatalf("expected 400, got %v", err)
	}
}

func TestGetConcertGatesOnMembership(t *testing.T) {
	cs, gs, q := newConcertSetup(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	stranger := seedUser(t, q, "stranger")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})
	if _, err := cs.Get(ctx, stranger.ID, c.ID); err == nil {
		t.Fatal("stranger must not read concert")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/service/ -run Concert
```
Expected: FAIL — `undefined: service.NewConcertService`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/service/concerts.go
package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type ConcertInput struct {
	Artist       string
	EventAt      time.Time
	Venue        string
	City         string
	TicketURL    string
	Notes        string
	PriceCents   *int32
	RSVPDeadline time.Time
}

type ConcertService struct {
	q      *gen.Queries
	groups *GroupService
}

func NewConcertService(q *gen.Queries, groups *GroupService) *ConcertService {
	return &ConcertService{q: q, groups: groups}
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *ConcertService) Create(ctx context.Context, userID, groupID uuid.UUID, in ConcertInput) (gen.Concert, error) {
	if _, err := s.groups.RequireMembership(ctx, userID, groupID); err != nil {
		return gen.Concert{}, err
	}
	if in.Artist == "" {
		return gen.Concert{}, apperr.BadRequest("invalid_artist", "artist required")
	}
	if in.EventAt.IsZero() || in.RSVPDeadline.IsZero() {
		return gen.Concert{}, apperr.BadRequest("invalid_dates", "event time and rsvp deadline required")
	}
	if in.RSVPDeadline.After(in.EventAt) {
		return gen.Concert{}, apperr.BadRequest("deadline_after_event", "rsvp deadline must be before the event")
	}
	return s.q.CreateConcert(ctx, gen.CreateConcertParams{
		GroupID: groupID, Artist: in.Artist, EventAt: in.EventAt,
		Venue: strPtr(in.Venue), City: strPtr(in.City), TicketUrl: strPtr(in.TicketURL),
		PriceCents: in.PriceCents, Notes: strPtr(in.Notes),
		RsvpDeadline: in.RSVPDeadline, CreatedBy: userID,
	})
}

func (s *ConcertService) ListForGroup(ctx context.Context, userID, groupID uuid.UUID) ([]gen.Concert, error) {
	if _, err := s.groups.RequireMembership(ctx, userID, groupID); err != nil {
		return nil, err
	}
	return s.q.ListConcertsForGroup(ctx, groupID)
}

func (s *ConcertService) Get(ctx context.Context, userID, concertID uuid.UUID) (gen.Concert, error) {
	c, err := s.q.GetConcert(ctx, concertID)
	if errors.Is(err, pgx.ErrNoRows) {
		return gen.Concert{}, apperr.NotFound("concert_not_found", "concert not found")
	}
	if err != nil {
		return gen.Concert{}, err
	}
	if _, err := s.groups.RequireMembership(ctx, userID, c.GroupID); err != nil {
		return gen.Concert{}, err
	}
	return c, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/service/ -run Concert
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service && git commit -m "feat: concert service with validation and membership gating"
```

---

### Task 3.3: Concert HTTP handlers

**Files:**
- Create: `backend/internal/httpapi/concert_handlers.go`
- Test: `backend/internal/httpapi/concert_handlers_test.go`

**Interfaces:**
- Consumes: `ConcertService`, `UserID`, chi params, render helpers.
- Produces:
  - `type ConcertHandlers struct { Concerts *service.ConcertService }`
  - `Create` (POST `/groups/{groupID}/concerts`), `ListForGroup` (GET same), `Get` (GET `/concerts/{concertID}`).
  - Request JSON for create: `{artist, event_at (RFC3339), venue, city, ticket_url, price_cents, notes, rsvp_deadline (RFC3339)}`.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/httpapi/concert_handlers_test.go
package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jaydee94/pit-pilot/backend/internal/httpapi"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestCreateConcertHandler(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "C" + string(rune('A'+n)) })
	cs := service.NewConcertService(q, gs)
	h := &httpapi.ConcertHandlers{Concerts: cs}

	owner, _ := q.UpsertUser(context.Background(), gen.UpsertUserParams{Provider: "google", ProviderSub: "o", DisplayName: "O"})
	g, _ := gs.Create(context.Background(), owner.ID, "Crew")

	body := `{"artist":"Tool","event_at":"2026-09-01T20:00:00Z","rsvp_deadline":"2026-08-20T20:00:00Z"}`
	r := httpapi.WithUserIDForTest(httptest.NewRequest(http.MethodPost, "/groups/x/concerts", strings.NewReader(body)), owner.ID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("groupID", g.ID.String())
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.Create(rec, r)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), "Tool") {
		t.Fatalf("create concert failed: %d %s", rec.Code, rec.Body.String())
	}
}
```
> Note: `WithUserIDForTest` runs first and returns a request; chi's route context is then layered on with `context.WithValue`. Because `WithUserIDForTest` already set the user-id value, both values coexist in the chain.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/httpapi/ -run Concert
```
Expected: FAIL — `undefined: httpapi.ConcertHandlers`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/httpapi/concert_handlers.go
package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
)

type ConcertHandlers struct {
	Concerts *service.ConcertService
}

type concertBody struct {
	Artist       string `json:"artist"`
	EventAt      string `json:"event_at"`
	Venue        string `json:"venue"`
	City         string `json:"city"`
	TicketURL    string `json:"ticket_url"`
	PriceCents   *int32 `json:"price_cents"`
	Notes        string `json:"notes"`
	RSVPDeadline string `json:"rsvp_deadline"`
}

func (h *ConcertHandlers) Create(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	gid, err := uuid.Parse(chi.URLParam(r, "groupID"))
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad group id"))
		return
	}
	var b concertBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "malformed json"))
		return
	}
	eventAt, err1 := time.Parse(time.RFC3339, b.EventAt)
	deadline, err2 := time.Parse(time.RFC3339, b.RSVPDeadline)
	if err1 != nil || err2 != nil {
		WriteError(w, apperr.BadRequest("invalid_dates", "event_at and rsvp_deadline must be RFC3339"))
		return
	}
	c, err := h.Concerts.Create(r.Context(), uid, gid, service.ConcertInput{
		Artist: b.Artist, EventAt: eventAt, Venue: b.Venue, City: b.City,
		TicketURL: b.TicketURL, Notes: b.Notes, PriceCents: b.PriceCents, RSVPDeadline: deadline,
	})
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, c)
}

func (h *ConcertHandlers) ListForGroup(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	gid, err := uuid.Parse(chi.URLParam(r, "groupID"))
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad group id"))
		return
	}
	list, err := h.Concerts.ListForGroup(r.Context(), uid, gid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, list)
}

func (h *ConcertHandlers) Get(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := uuid.Parse(chi.URLParam(r, "concertID"))
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	c, err := h.Concerts.Get(r.Context(), uid, cid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, c)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/httpapi/ -run Concert
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi && git commit -m "feat: concert handlers"
```

---

## Phase 4 — RSVP

### Task 4.1: RSVP sqlc queries

**Files:**
- Create: `backend/internal/store/queries/rsvps.sql`
- Generate: regenerate `backend/internal/store/gen/`
- Test: `backend/internal/store/gen/rsvps_gen_test.go`

**Interfaces:**
- Produces (generated):
  - `UpsertRSVP(ctx, UpsertRSVPParams{ConcertID, UserID uuid.UUID; Status string}) (Rsvp, error)`
  - `ListRSVPsForConcert(ctx, uuid.UUID) ([]ListRSVPsForConcertRow, error)` (row = user id, display_name, avatar_url, status)

- [ ] **Step 1: Write the query file**

```sql
-- backend/internal/store/queries/rsvps.sql

-- name: UpsertRSVP :one
INSERT INTO rsvps (concert_id, user_id, status)
VALUES ($1, $2, $3)
ON CONFLICT (concert_id, user_id) DO UPDATE
    SET status = EXCLUDED.status, responded_at = now()
RETURNING *;

-- name: ListRSVPsForConcert :many
SELECT u.id, u.display_name, u.avatar_url, r.status
FROM rsvps r
JOIN users u ON u.id = r.user_id
WHERE r.concert_id = $1
ORDER BY u.display_name;
```

- [ ] **Step 2: Regenerate**

```bash
cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
```

- [ ] **Step 3: Write the failing test**

```go
// backend/internal/store/gen/rsvps_gen_test.go
package gen_test

import (
	"context"
	"testing"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestUpsertRSVPChangesStatus(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	u, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "u", DisplayName: "U"})
	g, _ := q.CreateGroup(ctx, gen.CreateGroupParams{Name: "G", InviteCode: "Z9", CreatedBy: u.ID})
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := q.CreateConcert(ctx, gen.CreateConcertParams{GroupID: g.ID, Artist: "A", EventAt: when, RsvpDeadline: when.Add(-time.Hour), CreatedBy: u.ID})

	if _, err := q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: u.ID, Status: "yes"}); err != nil {
		t.Fatalf("first rsvp: %v", err)
	}
	r2, err := q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: u.ID, Status: "no"})
	if err != nil || r2.Status != "no" {
		t.Fatalf("rsvp change failed: %v err=%v", r2, err)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/store/gen/ -run RSVP
```
Expected: PASS (FAIL before regeneration).

- [ ] **Step 5: Commit**

```bash
git add backend && git commit -m "feat: sqlc rsvp queries"
```

---

### Task 4.2: RSVP service

**Files:**
- Create: `backend/internal/service/rsvps.go`
- Test: `backend/internal/service/rsvps_test.go`

**Interfaces:**
- Consumes: `*gen.Queries`, `*ConcertService.Get` (which already gates membership), `apperr`.
- Produces:
  - `type RSVPService struct { ... }`
  - `func NewRSVPService(q *gen.Queries, concerts *ConcertService) *RSVPService`
  - `func (s *RSVPService) Set(ctx, userID, concertID uuid.UUID, status string) (gen.Rsvp, error)` — validates status ∈ {yes,no}, gates membership via concert lookup.
  - `func (s *RSVPService) List(ctx, userID, concertID uuid.UUID) ([]gen.ListRSVPsForConcertRow, error)` — member-gated.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/service/rsvps_test.go
package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestRSVPSetAndList(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "R" + string(rune('A'+n)) })
	cs := service.NewConcertService(q, gs)
	rs := service.NewRSVPService(q, cs)
	ctx := context.Background()

	owner := seedUser(t, q, "owner")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})

	if _, err := rs.Set(ctx, owner.ID, c.ID, "yes"); err != nil {
		t.Fatalf("set rsvp: %v", err)
	}
	rows, err := rs.List(ctx, owner.ID, c.ID)
	if err != nil || len(rows) != 1 || rows[0].Status != "yes" {
		t.Fatalf("list rsvps: %v err=%v", rows, err)
	}
}

func TestRSVPRejectsBadStatus(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "S" + string(rune('A'+n)) })
	cs := service.NewConcertService(q, gs)
	rs := service.NewRSVPService(q, cs)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})

	_, err := rs.Set(ctx, owner.ID, c.ID, "maybe")
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 400 {
		t.Fatalf("expected 400 for bad status, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/service/ -run RSVP
```
Expected: FAIL — `undefined: service.NewRSVPService`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/service/rsvps.go
package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type RSVPService struct {
	q        *gen.Queries
	concerts *ConcertService
}

func NewRSVPService(q *gen.Queries, concerts *ConcertService) *RSVPService {
	return &RSVPService{q: q, concerts: concerts}
}

func (s *RSVPService) Set(ctx context.Context, userID, concertID uuid.UUID, status string) (gen.Rsvp, error) {
	if status != "yes" && status != "no" {
		return gen.Rsvp{}, apperr.BadRequest("invalid_status", "status must be 'yes' or 'no'")
	}
	// Get gates membership (and existence) for us.
	if _, err := s.concerts.Get(ctx, userID, concertID); err != nil {
		return gen.Rsvp{}, err
	}
	return s.q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: concertID, UserID: userID, Status: status})
}

func (s *RSVPService) List(ctx context.Context, userID, concertID uuid.UUID) ([]gen.ListRSVPsForConcertRow, error) {
	if _, err := s.concerts.Get(ctx, userID, concertID); err != nil {
		return nil, err
	}
	return s.q.ListRSVPsForConcert(ctx, concertID)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/service/ -run RSVP
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service && git commit -m "feat: rsvp service with status validation and gating"
```

---

### Task 4.3: RSVP HTTP handlers

**Files:**
- Create: `backend/internal/httpapi/rsvp_handlers.go`
- Test: `backend/internal/httpapi/rsvp_handlers_test.go`

**Interfaces:**
- Consumes: `RSVPService`, `UserID`, chi params, render helpers.
- Produces:
  - `type RSVPHandlers struct { RSVPs *service.RSVPService }`
  - `Set` (PUT `/concerts/{concertID}/rsvp`, body `{"status":"yes"|"no"}`), `List` (GET `/concerts/{concertID}/rsvps`).

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/httpapi/rsvp_handlers_test.go
package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jaydee94/pit-pilot/backend/internal/httpapi"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestSetRSVPHandler(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "H" + string(rune('A'+n)) })
	cs := service.NewConcertService(q, gs)
	rs := service.NewRSVPService(q, cs)
	h := &httpapi.RSVPHandlers{RSVPs: rs}

	owner, _ := q.UpsertUser(context.Background(), gen.UpsertUserParams{Provider: "google", ProviderSub: "o", DisplayName: "O"})
	g, _ := gs.Create(context.Background(), owner.ID, "Crew")
	when := mustTime("2026-09-01T20:00:00Z")
	c, _ := cs.Create(context.Background(), owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})

	r := httpapi.WithUserIDForTest(httptest.NewRequest(http.MethodPut, "/concerts/x/rsvp", strings.NewReader(`{"status":"yes"}`)), owner.ID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("concertID", c.ID.String())
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.Set(rec, r)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "yes") {
		t.Fatalf("set rsvp failed: %d %s", rec.Code, rec.Body.String())
	}
}
```
> Add the small helpers `mustTime(s string) time.Time` (wraps `time.Parse(time.RFC3339, s)`, `t.Fatal` on error) and import `time` in this test file.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/httpapi/ -run RSVP
```
Expected: FAIL — `undefined: httpapi.RSVPHandlers`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/httpapi/rsvp_handlers.go
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
)

type RSVPHandlers struct {
	RSVPs *service.RSVPService
}

func (h *RSVPHandlers) Set(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := uuid.Parse(chi.URLParam(r, "concertID"))
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	var b struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "status required"))
		return
	}
	rsvp, err := h.RSVPs.Set(r.Context(), uid, cid, b.Status)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, rsvp)
}

func (h *RSVPHandlers) List(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := uuid.Parse(chi.URLParam(r, "concertID"))
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	rows, err := h.RSVPs.List(r.Context(), uid, cid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, rows)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/httpapi/ -run RSVP
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi && git commit -m "feat: rsvp handlers"
```

---

### Task 4.4: Router assembly + main entrypoint + health checks

**Files:**
- Create: `backend/internal/httpapi/router.go`
- Create: `backend/cmd/api/main.go`
- Test: `backend/internal/httpapi/router_test.go`

**Interfaces:**
- Consumes: every handler + middleware above.
- Produces:
  - `type Deps struct { Auth *AuthHandlers; Groups *GroupHandlers; Concerts *ConcertHandlers; RSVPs *RSVPHandlers; GroupSvc *service.GroupService }`
  - `func NewRouter(d Deps) http.Handler` — full route table with `/healthz`, `/readyz`, and the API tree under session + membership middleware.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/httpapi/router_test.go
package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/httpapi"
)

func TestHealthEndpoints(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Deps{})
	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s returned %d", path, rec.Code)
		}
	}
}

func TestProtectedRouteRequiresSession(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Deps{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/groups", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without session, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/httpapi/ -run 'Health|Protected'
```
Expected: FAIL — `undefined: httpapi.NewRouter`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/httpapi/router.go
package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
)

type Deps struct {
	Auth     *AuthHandlers
	Groups   *GroupHandlers
	Concerts *ConcertHandlers
	RSVPs    *RSVPHandlers
	GroupSvc *service.GroupService
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	if d.Auth != nil {
		r.Post("/auth/google", d.Auth.Login("google"))
		r.Post("/auth/apple", d.Auth.Login("apple"))
	}

	r.Route("/api", func(api chi.Router) {
		if d.Auth != nil {
			api.Use(d.Auth.SessionMiddleware)
			api.Get("/me", d.Auth.Me)
			api.Post("/auth/logout", d.Auth.Logout)
		}
		if d.Groups != nil {
			api.Get("/groups", d.Groups.List)
			api.Post("/groups", d.Groups.Create)
			api.Post("/groups/join", d.Groups.Join)
		}
		if d.GroupSvc != nil {
			api.Route("/groups/{groupID}", func(gr chi.Router) {
				gr.Use(RequireGroupMember(d.GroupSvc))
				if d.Groups != nil {
					gr.Get("/members", d.Groups.Members)
					gr.Post("/invite", d.Groups.Invite)
				}
				if d.Concerts != nil {
					gr.Get("/concerts", d.Concerts.ListForGroup)
					gr.Post("/concerts", d.Concerts.Create)
				}
			})
		}
		if d.Concerts != nil {
			api.Get("/concerts/{concertID}", d.Concerts.Get)
		}
		if d.RSVPs != nil {
			api.Put("/concerts/{concertID}/rsvp", d.RSVPs.Set)
			api.Get("/concerts/{concertID}/rsvps", d.RSVPs.List)
		}
	})
	return r
}
```
> The `/api` group requires a session even when `d.Auth` is nil? No — when `Auth` is nil (as in the health test) the session middleware is not attached, so `TestProtectedRouteRequiresSession` would fail. Fix: always attach a session guard. Simplest correct form for the test: when `d.Auth == nil`, attach a guard that always 401s on `/api`. Add before the route registrations inside `r.Route("/api", …)`:
> ```go
> if d.Auth != nil { api.Use(d.Auth.SessionMiddleware) } else {
>     api.Use(func(next http.Handler) http.Handler {
>         return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
>             WriteError(w, apperr.Unauthorized("no_session", "authentication required"))
>         })
>     })
> }
> ```
> Import `apperr` accordingly. This keeps the health test (no `/api`) green and makes the protected-route test pass.

- [ ] **Step 4: Write the main entrypoint**

```go
// backend/cmd/api/main.go
package main

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jaydee94/pit-pilot/backend/internal/auth"
	"github.com/jaydee94/pit-pilot/backend/internal/config"
	"github.com/jaydee94/pit-pilot/backend/internal/httpapi"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"os"
)

func newInviteCode() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()
	q := gen.New(pool)

	verifier, err := auth.NewOIDCVerifier(ctx, cfg.GoogleClientID, cfg.AppleClientID)
	if err != nil {
		log.Fatalf("oidc: %v", err)
	}
	sessions := auth.NewSessionManager(cfg.SessionSigningKey, 30*24*time.Hour, nil)

	users := service.NewUserService(q, verifier)
	groups := service.NewGroupService(pool, q, newInviteCode)
	concerts := service.NewConcertService(q, groups)
	rsvps := service.NewRSVPService(q, concerts)

	router := httpapi.NewRouter(httpapi.Deps{
		Auth:     &httpapi.AuthHandlers{Users: users, Sessions: sessions, CookieSecure: cfg.CookieSecure},
		Groups:   &httpapi.GroupHandlers{Groups: groups},
		Concerts: &httpapi.ConcertHandlers{Concerts: concerts},
		RSVPs:    &httpapi.RSVPHandlers{RSVPs: rsvps},
		GroupSvc: groups,
	})

	log.Printf("pit-pilot api listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 5: Run tests + build to verify**

```bash
cd backend && go test ./... && go build ./cmd/api
```
Expected: all PASS, binary builds.

- [ ] **Step 6: Commit**

```bash
git add backend && git commit -m "feat: assemble router, health checks, and api entrypoint"
```

## Phase 5 — Frontend (React PWA)

> Visual polish is deferred to a later cycle (`frontend-design` skill). Phase 5 wires the five screens to the API with TDD on the logic-bearing pieces (API client, RSVP toggle). Component tests use Vitest + Testing Library with `fetch` mocked.

### Task 5.1: Scaffold Vite + React + TS + Vitest

**Files:**
- Create: `frontend/package.json`, `frontend/vite.config.ts`, `frontend/tsconfig.json`, `frontend/index.html`
- Create: `frontend/src/main.tsx`, `frontend/src/App.tsx`
- Create: `frontend/src/setupTests.ts`
- Test: `frontend/src/App.test.tsx`

**Interfaces:**
- Produces: a building Vite app with a passing Vitest run (`npm run test`, `npm run build`).

- [ ] **Step 1: Create package.json**

```json
{
  "name": "pit-pilot-frontend",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview",
    "test": "vitest"
  },
  "dependencies": {
    "@tanstack/react-query": "^5.51.0",
    "react": "^18.3.1",
    "react-dom": "^18.3.1",
    "react-router-dom": "^6.26.0"
  },
  "devDependencies": {
    "@testing-library/jest-dom": "^6.4.8",
    "@testing-library/react": "^16.0.0",
    "@types/react": "^18.3.3",
    "@types/react-dom": "^18.3.0",
    "@vitejs/plugin-react": "^4.3.1",
    "jsdom": "^24.1.1",
    "typescript": "^5.5.4",
    "vite": "^5.4.0",
    "vite-plugin-pwa": "^0.20.1",
    "vitest": "^2.0.5"
  }
}
```

- [ ] **Step 2: Create config files**

```ts
// frontend/vite.config.ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { VitePWA } from "vite-plugin-pwa";

export default defineConfig({
  plugins: [react(), VitePWA({ registerType: "autoUpdate" })],
  server: { proxy: { "/api": "http://localhost:8080", "/auth": "http://localhost:8080" } },
  test: { environment: "jsdom", globals: true, setupFiles: "./src/setupTests.ts" },
});
```

```json
// frontend/tsconfig.json
{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "jsx": "react-jsx",
    "strict": true,
    "noEmit": true,
    "types": ["vitest/globals", "@testing-library/jest-dom"]
  },
  "include": ["src"]
}
```

```html
<!-- frontend/index.html -->
<!doctype html>
<html lang="de">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>pit-pilot</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

```ts
// frontend/src/setupTests.ts
import "@testing-library/jest-dom";
```

- [ ] **Step 3: Write the failing test**

```tsx
// frontend/src/App.test.tsx
import { render, screen } from "@testing-library/react";
import App from "./App";

test("renders app shell heading", () => {
  render(<App />);
  expect(screen.getByText(/pit-pilot/i)).toBeInTheDocument();
});
```

- [ ] **Step 4: Run test to verify it fails**

```bash
cd frontend && npm install && npm run test -- --run
```
Expected: FAIL — cannot find `./App`.

- [ ] **Step 5: Write minimal implementation**

```tsx
// frontend/src/App.tsx
export default function App() {
  return <h1>pit-pilot</h1>;
}
```

```tsx
// frontend/src/main.tsx
import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
```

- [ ] **Step 6: Run test + build to verify it passes**

```bash
cd frontend && npm run test -- --run && npm run build
```
Expected: PASS + build succeeds.

- [ ] **Step 7: Commit**

```bash
git add frontend && git commit -m "feat: scaffold react+vite+vitest frontend"
```

---

### Task 5.2: Typed API client

**Files:**
- Create: `frontend/src/api/client.ts`
- Test: `frontend/src/api/client.test.ts`

**Interfaces:**
- Produces:
  - `type ApiError = { code: string; message: string }`
  - `async function apiFetch<T>(path: string, init?: RequestInit): Promise<T>` — always `credentials: "include"`; throws `ApiError` (parsed from `{error:{...}}`) on non-2xx.
  - Typed helpers: `login(provider, idToken)`, `me()`, `listGroups()`, `createGroup(name)`, `joinGroup(code)`, `groupMembers(id)`, `regenerateInvite(id)`, `listConcerts(groupId)`, `createConcert(groupId, input)`, `getConcert(id)`, `setRsvp(concertId, status)`, `listRsvps(concertId)`.

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/api/client.test.ts
import { afterEach, expect, test, vi } from "vitest";
import { apiFetch } from "./client";

afterEach(() => vi.restoreAllMocks());

test("apiFetch returns parsed json on success", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify([{ id: "1", name: "Crew" }]), { status: 200 }),
  ));
  const data = await apiFetch<{ id: string; name: string }[]>("/api/groups");
  expect(data[0].name).toBe("Crew");
});

test("apiFetch throws ApiError on failure", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify({ error: { code: "not_member", message: "no" } }), { status: 403 }),
  ));
  await expect(apiFetch("/api/groups/x/members")).rejects.toMatchObject({ code: "not_member" });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd frontend && npm run test -- --run client
```
Expected: FAIL — cannot find `./client`.

- [ ] **Step 3: Write minimal implementation**

```ts
// frontend/src/api/client.ts
export type ApiError = { code: string; message: string };

export async function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(path, {
    credentials: "include",
    headers: { "Content-Type": "application/json", ...(init.headers ?? {}) },
    ...init,
  });
  if (!res.ok) {
    let err: ApiError = { code: "unknown", message: res.statusText };
    try {
      const body = await res.json();
      if (body?.error) err = body.error;
    } catch {
      /* keep default */
    }
    throw err;
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export type Group = { id: string; name: string; invite_code: string };
export type Concert = {
  id: string; group_id: string; artist: string; event_at: string;
  venue?: string; city?: string; ticket_url?: string; price_cents?: number;
  notes?: string; rsvp_deadline: string;
};
export type Member = { id: string; display_name: string; avatar_url?: string; role: string };
export type Rsvp = { id: string; display_name: string; avatar_url?: string; status: "yes" | "no" };

export const login = (provider: "google" | "apple", idToken: string) =>
  apiFetch<{ id: string }>(`/auth/${provider}`, { method: "POST", body: JSON.stringify({ id_token: idToken }) });
export const me = () => apiFetch<{ id: string }>("/api/me");
export const listGroups = () => apiFetch<Group[]>("/api/groups");
export const createGroup = (name: string) =>
  apiFetch<Group>("/api/groups", { method: "POST", body: JSON.stringify({ name }) });
export const joinGroup = (code: string) =>
  apiFetch<Group>("/api/groups/join", { method: "POST", body: JSON.stringify({ code }) });
export const groupMembers = (id: string) => apiFetch<Member[]>(`/api/groups/${id}/members`);
export const regenerateInvite = (id: string) =>
  apiFetch<{ invite_code: string }>(`/api/groups/${id}/invite`, { method: "POST" });
export const listConcerts = (groupId: string) => apiFetch<Concert[]>(`/api/groups/${groupId}/concerts`);
export const createConcert = (groupId: string, input: Record<string, unknown>) =>
  apiFetch<Concert>(`/api/groups/${groupId}/concerts`, { method: "POST", body: JSON.stringify(input) });
export const getConcert = (id: string) => apiFetch<Concert>(`/api/concerts/${id}`);
export const setRsvp = (concertId: string, status: "yes" | "no") =>
  apiFetch<unknown>(`/api/concerts/${concertId}/rsvp`, { method: "PUT", body: JSON.stringify({ status }) });
export const listRsvps = (concertId: string) => apiFetch<Rsvp[]>(`/api/concerts/${concertId}/rsvps`);
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd frontend && npm run test -- --run client
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend && git commit -m "feat: typed api client with error handling"
```

---

### Task 5.3: Routing, auth gate, and the five screens

**Files:**
- Create: `frontend/src/auth/AuthContext.tsx`
- Create: `frontend/src/routes/Login.tsx`
- Create: `frontend/src/routes/Groups.tsx`
- Create: `frontend/src/routes/GroupDetail.tsx`
- Create: `frontend/src/routes/ConcertForm.tsx`
- Create: `frontend/src/routes/ConcertDetail.tsx`
- Modify: `frontend/src/App.tsx`, `frontend/src/main.tsx`
- Test: `frontend/src/routes/ConcertDetail.test.tsx`, `frontend/src/routes/Groups.test.tsx`

**Interfaces:**
- Consumes: the API client from Task 5.2, React Router, TanStack Query.
- Produces: a wired SPA. `AuthContext` exposes `{ userId, refresh }`; `Login` calls `login()` then navigates to `/groups`; protected routes redirect to `/login` when `me()` 401s.

> Because real Google/Apple sign-in needs registered OAuth client IDs (configured during deployment, Task 6.x), `Login` in Cycle 1 accepts a pasted ID token field in addition to the provider buttons, so the flow is testable end-to-end in dev before OAuth apps exist. The provider buttons are wired to the OAuth redirect once client IDs are set.

- [ ] **Step 1: Write the failing tests**

```tsx
// frontend/src/routes/Groups.test.tsx
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, test, vi } from "vitest";
import Groups from "./Groups";

afterEach(() => vi.restoreAllMocks());

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}><MemoryRouter>{ui}</MemoryRouter></QueryClientProvider>;
}

test("lists groups from the api", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify([{ id: "1", name: "Metal Buddies", invite_code: "X" }]), { status: 200 }),
  ));
  render(wrap(<Groups />));
  await waitFor(() => expect(screen.getByText("Metal Buddies")).toBeInTheDocument());
});
```

```tsx
// frontend/src/routes/ConcertDetail.test.tsx
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { afterEach, expect, test, vi } from "vitest";
import ConcertDetail from "./ConcertDetail";

afterEach(() => vi.restoreAllMocks());

test("shows concert artist and rsvp list", async () => {
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/rsvps"))
      return new Response(JSON.stringify([{ id: "u1", display_name: "Ann", status: "yes" }]), { status: 200 });
    return new Response(JSON.stringify({ id: "c1", artist: "Tool", event_at: "2026-09-01T20:00:00Z", rsvp_deadline: "2026-08-20T20:00:00Z" }), { status: 200 });
  }));
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter initialEntries={["/concerts/c1"]}>
        <Routes><Route path="/concerts/:id" element={<ConcertDetail />} /></Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(screen.getByText("Tool")).toBeInTheDocument());
  expect(screen.getByText("Ann")).toBeInTheDocument();
});
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd frontend && npm run test -- --run routes
```
Expected: FAIL — cannot find route modules.

- [ ] **Step 3: Write minimal implementations**

```tsx
// frontend/src/auth/AuthContext.tsx
import { createContext, useContext, useEffect, useState, ReactNode } from "react";
import { me } from "../api/client";

type AuthState = { userId: string | null; loading: boolean; refresh: () => void };
const Ctx = createContext<AuthState>({ userId: null, loading: true, refresh: () => {} });

export function AuthProvider({ children }: { children: ReactNode }) {
  const [userId, setUserId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const refresh = () => {
    setLoading(true);
    me().then((u) => setUserId(u.id)).catch(() => setUserId(null)).finally(() => setLoading(false));
  };
  useEffect(refresh, []);
  return <Ctx.Provider value={{ userId, loading, refresh }}>{children}</Ctx.Provider>;
}
export const useAuth = () => useContext(Ctx);
```

```tsx
// frontend/src/routes/Login.tsx
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { login } from "../api/client";
import { useAuth } from "../auth/AuthContext";

export default function Login() {
  const [token, setToken] = useState("");
  const [err, setErr] = useState("");
  const nav = useNavigate();
  const { refresh } = useAuth();
  const submit = async (provider: "google" | "apple") => {
    try {
      await login(provider, token);
      refresh();
      nav("/groups");
    } catch (e: any) {
      setErr(e.message ?? "Login fehlgeschlagen");
    }
  };
  return (
    <main>
      <h1>pit-pilot</h1>
      <button onClick={() => submit("google")}>Mit Google anmelden</button>
      <button onClick={() => submit("apple")}>Mit Apple anmelden</button>
      <details>
        <summary>Dev: ID-Token einfügen</summary>
        <textarea value={token} onChange={(e) => setToken(e.target.value)} />
      </details>
      {err && <p role="alert">{err}</p>}
    </main>
  );
}
```

```tsx
// frontend/src/routes/Groups.tsx
import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { listGroups, createGroup, joinGroup } from "../api/client";

export default function Groups() {
  const qc = useQueryClient();
  const { data: groups } = useQuery({ queryKey: ["groups"], queryFn: listGroups });
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const create = useMutation({ mutationFn: () => createGroup(name), onSuccess: () => qc.invalidateQueries({ queryKey: ["groups"] }) });
  const join = useMutation({ mutationFn: () => joinGroup(code), onSuccess: () => qc.invalidateQueries({ queryKey: ["groups"] }) });
  return (
    <main>
      <h1>Meine Gruppen</h1>
      <ul>{groups?.map((g) => <li key={g.id}><Link to={`/groups/${g.id}`}>{g.name}</Link></li>)}</ul>
      <form onSubmit={(e) => { e.preventDefault(); create.mutate(); }}>
        <input aria-label="Gruppenname" value={name} onChange={(e) => setName(e.target.value)} placeholder="Neue Gruppe" />
        <button type="submit">Anlegen</button>
      </form>
      <form onSubmit={(e) => { e.preventDefault(); join.mutate(); }}>
        <input aria-label="Einladungscode" value={code} onChange={(e) => setCode(e.target.value)} placeholder="Code" />
        <button type="submit">Beitreten</button>
      </form>
    </main>
  );
}
```

```tsx
// frontend/src/routes/GroupDetail.tsx
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { groupMembers, listConcerts } from "../api/client";

export default function GroupDetail() {
  const { id = "" } = useParams();
  const { data: members } = useQuery({ queryKey: ["members", id], queryFn: () => groupMembers(id) });
  const { data: concerts } = useQuery({ queryKey: ["concerts", id], queryFn: () => listConcerts(id) });
  return (
    <main>
      <h1>Gruppe</h1>
      <section><h2>Mitglieder</h2><ul>{members?.map((m) => <li key={m.id}>{m.display_name} ({m.role})</li>)}</ul></section>
      <section>
        <h2>Konzerte</h2>
        <Link to={`/groups/${id}/concerts/new`}>Konzert anlegen</Link>
        <ul>{concerts?.map((c) => <li key={c.id}><Link to={`/concerts/${c.id}`}>{c.artist} — {new Date(c.event_at).toLocaleDateString()}</Link></li>)}</ul>
      </section>
    </main>
  );
}
```

```tsx
// frontend/src/routes/ConcertForm.tsx
import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { createConcert } from "../api/client";

export default function ConcertForm() {
  const { id = "" } = useParams();
  const nav = useNavigate();
  const [f, setF] = useState({ artist: "", event_at: "", rsvp_deadline: "", venue: "", city: "", ticket_url: "", notes: "" });
  const set = (k: string) => (e: React.ChangeEvent<HTMLInputElement>) => setF({ ...f, [k]: e.target.value });
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const payload = {
      ...f,
      event_at: new Date(f.event_at).toISOString(),
      rsvp_deadline: new Date(f.rsvp_deadline).toISOString(),
    };
    const c = await createConcert(id, payload);
    nav(`/concerts/${c.id}`);
  };
  return (
    <form onSubmit={submit}>
      <h1>Konzert anlegen</h1>
      <input aria-label="Künstler" value={f.artist} onChange={set("artist")} required />
      <input aria-label="Datum/Zeit" type="datetime-local" value={f.event_at} onChange={set("event_at")} required />
      <input aria-label="Anmeldeschluss" type="datetime-local" value={f.rsvp_deadline} onChange={set("rsvp_deadline")} required />
      <input aria-label="Venue" value={f.venue} onChange={set("venue")} />
      <input aria-label="Stadt" value={f.city} onChange={set("city")} />
      <input aria-label="Ticket-Link" value={f.ticket_url} onChange={set("ticket_url")} />
      <input aria-label="Notizen" value={f.notes} onChange={set("notes")} />
      <button type="submit">Speichern</button>
    </form>
  );
}
```

```tsx
// frontend/src/routes/ConcertDetail.tsx
import { useParams } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { getConcert, listRsvps, setRsvp } from "../api/client";

export default function ConcertDetail() {
  const { id = "" } = useParams();
  const qc = useQueryClient();
  const { data: concert } = useQuery({ queryKey: ["concert", id], queryFn: () => getConcert(id) });
  const { data: rsvps } = useQuery({ queryKey: ["rsvps", id], queryFn: () => listRsvps(id) });
  const mutate = useMutation({
    mutationFn: (status: "yes" | "no") => setRsvp(id, status),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["rsvps", id] }),
  });
  if (!concert) return <p>Lädt…</p>;
  return (
    <main>
      <h1>{concert.artist}</h1>
      <p>{new Date(concert.event_at).toLocaleString()} — {concert.venue} {concert.city}</p>
      <div>
        <button onClick={() => mutate.mutate("yes")}>Bin dabei</button>
        <button onClick={() => mutate.mutate("no")}>Kann nicht</button>
      </div>
      <h2>Wer kommt mit</h2>
      <ul>{rsvps?.map((r) => <li key={r.id}>{r.display_name}: {r.status === "yes" ? "✅" : "❌"}</li>)}</ul>
    </main>
  );
}
```

Wire the router:
```tsx
// frontend/src/App.tsx
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider, useAuth } from "./auth/AuthContext";
import Login from "./routes/Login";
import Groups from "./routes/Groups";
import GroupDetail from "./routes/GroupDetail";
import ConcertForm from "./routes/ConcertForm";
import ConcertDetail from "./routes/ConcertDetail";

const qc = new QueryClient();

function Protected({ children }: { children: JSX.Element }) {
  const { userId, loading } = useAuth();
  if (loading) return <p>Lädt…</p>;
  return userId ? children : <Navigate to="/login" replace />;
}

export default function App() {
  return (
    <QueryClientProvider client={qc}>
      <AuthProvider>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route path="/groups" element={<Protected><Groups /></Protected>} />
            <Route path="/groups/:id" element={<Protected><GroupDetail /></Protected>} />
            <Route path="/groups/:id/concerts/new" element={<Protected><ConcertForm /></Protected>} />
            <Route path="/concerts/:id" element={<Protected><ConcertDetail /></Protected>} />
            <Route path="*" element={<Navigate to="/groups" replace />} />
          </Routes>
        </BrowserRouter>
      </AuthProvider>
    </QueryClientProvider>
  );
}
```
> The `App.test.tsx` from Task 5.1 asserted an `<h1>pit-pilot</h1>`. With the router replacing `App`, update that test to render `<Login />` (which contains the heading) or assert a redirect to `/login`. Simplest: change `App.test.tsx` to import and render `Login` within a `MemoryRouter` and assert the heading. Make that edit in this step so the suite stays green.

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd frontend && npm run test -- --run && npm run build
```
Expected: PASS + build succeeds.

- [ ] **Step 5: Commit**

```bash
git add frontend && git commit -m "feat: auth gate, router, and the five core screens"
```

---

## Phase 6 — Containers & Kubernetes

### Task 6.1: Backend Dockerfile (distroless) + frontend Dockerfile (nginx)

**Files:**
- Create: `backend/Dockerfile`
- Create: `frontend/Dockerfile`, `frontend/nginx.conf`

**Interfaces:**
- Produces: two runnable images. No unit test; verification is a successful `docker build`.

- [ ] **Step 1: Backend Dockerfile**

```dockerfile
# backend/Dockerfile
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/api"]
```

- [ ] **Step 2: Frontend Dockerfile + nginx config**

```dockerfile
# frontend/Dockerfile
FROM node:20 AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
```

```nginx
# frontend/nginx.conf
server {
  listen 80;
  root /usr/share/nginx/html;
  index index.html;
  location / {
    try_files $uri $uri/ /index.html;   # SPA fallback
  }
}
```

- [ ] **Step 3: Verify builds**

```bash
docker build -t pitpilot-api ./backend && docker build -t pitpilot-web ./frontend
```
Expected: both images build successfully.

- [ ] **Step 4: Commit**

```bash
git add backend/Dockerfile frontend/Dockerfile frontend/nginx.conf && git commit -m "feat: container images for api and frontend"
```

---

### Task 6.2: Kustomize manifests + migration job

**Files:**
- Create: `deploy/base/postgres.yaml`, `deploy/base/api.yaml`, `deploy/base/frontend.yaml`, `deploy/base/ingress.yaml`, `deploy/base/migrate-job.yaml`, `deploy/base/kustomization.yaml`
- Create: `deploy/overlays/dev/kustomization.yaml`, `deploy/overlays/dev/secret.example.env`

**Interfaces:**
- Produces: a deployable manifest set. Verification: `kubectl kustomize deploy/overlays/dev` renders without error and (optionally) applies to a local `kind` cluster.

- [ ] **Step 1: Postgres + secret-driven config**

```yaml
# deploy/base/postgres.yaml
apiVersion: apps/v1
kind: StatefulSet
metadata: { name: postgres }
spec:
  serviceName: postgres
  replicas: 1
  selector: { matchLabels: { app: postgres } }
  template:
    metadata: { labels: { app: postgres } }
    spec:
      containers:
        - name: postgres
          image: postgres:16-alpine
          envFrom: [{ secretRef: { name: pitpilot-secrets } }]
          env:
            - { name: POSTGRES_DB, value: pp }
            - { name: POSTGRES_USER, value: pp }
            - { name: POSTGRES_PASSWORD, valueFrom: { secretKeyRef: { name: pitpilot-secrets, key: POSTGRES_PASSWORD } } }
          ports: [{ containerPort: 5432 }]
          volumeMounts: [{ name: data, mountPath: /var/lib/postgresql/data }]
  volumeClaimTemplates:
    - metadata: { name: data }
      spec: { accessModes: ["ReadWriteOnce"], resources: { requests: { storage: 1Gi } } }
---
apiVersion: v1
kind: Service
metadata: { name: postgres }
spec:
  selector: { app: postgres }
  ports: [{ port: 5432, targetPort: 5432 }]
```

- [ ] **Step 2: Migration Job**

```yaml
# deploy/base/migrate-job.yaml
apiVersion: batch/v1
kind: Job
metadata: { name: db-migrate }
spec:
  backoffLimit: 5
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: migrate
          image: migrate/migrate:v4.17.1
          envFrom: [{ secretRef: { name: pitpilot-secrets } }]
          command: ["migrate"]
          args:
            - "-path=/migrations"
            - "-database=$(DATABASE_URL)"
            - "up"
          volumeMounts: [{ name: migrations, mountPath: /migrations }]
      volumes:
        - name: migrations
          configMap: { name: pitpilot-migrations }
```
> The migration SQL files are mounted via a ConfigMap `pitpilot-migrations`, generated by kustomize from `backend/internal/store/migrations` (see Step 5 kustomization `configMapGenerator`).

- [ ] **Step 3: API Deployment + Service**

```yaml
# deploy/base/api.yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: api }
spec:
  replicas: 2
  selector: { matchLabels: { app: api } }
  template:
    metadata: { labels: { app: api } }
    spec:
      containers:
        - name: api
          image: pitpilot-api:latest
          envFrom: [{ secretRef: { name: pitpilot-secrets } }]
          ports: [{ containerPort: 8080 }]
          readinessProbe: { httpGet: { path: /readyz, port: 8080 }, initialDelaySeconds: 3 }
          livenessProbe: { httpGet: { path: /healthz, port: 8080 }, initialDelaySeconds: 5 }
---
apiVersion: v1
kind: Service
metadata: { name: api }
spec:
  selector: { app: api }
  ports: [{ port: 80, targetPort: 8080 }]
```

- [ ] **Step 4: Frontend Deployment + Service + Ingress**

```yaml
# deploy/base/frontend.yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: frontend }
spec:
  replicas: 2
  selector: { matchLabels: { app: frontend } }
  template:
    metadata: { labels: { app: frontend } }
    spec:
      containers:
        - name: frontend
          image: pitpilot-web:latest
          ports: [{ containerPort: 80 }]
---
apiVersion: v1
kind: Service
metadata: { name: frontend }
spec:
  selector: { app: frontend }
  ports: [{ port: 80, targetPort: 80 }]
```

```yaml
# deploy/base/ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: pitpilot
  annotations: { nginx.ingress.kubernetes.io/rewrite-target: / }
spec:
  rules:
    - http:
        paths:
          - { path: /api, pathType: Prefix, backend: { service: { name: api, port: { number: 80 } } } }
          - { path: /auth, pathType: Prefix, backend: { service: { name: api, port: { number: 80 } } } }
          - { path: /, pathType: Prefix, backend: { service: { name: frontend, port: { number: 80 } } } }
```

- [ ] **Step 5: Kustomization + dev overlay**

```yaml
# deploy/base/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - postgres.yaml
  - migrate-job.yaml
  - api.yaml
  - frontend.yaml
  - ingress.yaml
configMapGenerator:
  - name: pitpilot-migrations
    files:
      - ../../backend/internal/store/migrations/0001_init.up.sql
      - ../../backend/internal/store/migrations/0001_init.down.sql
```

```yaml
# deploy/overlays/dev/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../base
secretGenerator:
  - name: pitpilot-secrets
    envs:
      - secret.example.env
images:
  - { name: pitpilot-api, newTag: latest }
  - { name: pitpilot-web, newTag: latest }
```

```dotenv
# deploy/overlays/dev/secret.example.env  (copy to secret.env, do not commit real secrets)
DATABASE_URL=postgres://pp:pp@postgres:5432/pp?sslmode=disable
POSTGRES_PASSWORD=pp
SESSION_SIGNING_KEY=change-me-in-prod
GOOGLE_CLIENT_ID=
APPLE_CLIENT_ID=
COOKIE_SECURE=false
PORT=8080
```

- [ ] **Step 6: Verify manifests render**

```bash
kubectl kustomize deploy/overlays/dev > /dev/null && echo OK
```
Expected: prints `OK` (no render errors). Optional full bring-up on `kind`: build images, `kind load docker-image pitpilot-api pitpilot-web`, `kubectl apply -k deploy/overlays/dev`.

- [ ] **Step 7: Commit**

```bash
git add deploy && git commit -m "feat: kustomize manifests, migration job, dev overlay"
```

> Add `deploy/overlays/dev/secret.env` to `.gitignore` so real secrets never land in git.

---

## Self-Review

**Spec coverage check (each spec section → task):**
- §3 Architecture (3-layer Go, chi, pgx/sqlc, migrate) → Tasks 0.1–0.5, all service/httpapi tasks.
- §4 Data model (5 tables, UUIDs, cents) → Task 0.3.
- §5 Auth flow (provider verify → session cookie) → Tasks 1.1–1.3, 1.5, 1.6.
- §6 API surface (every endpoint) → Tasks 1.6 (auth/me/logout), 2.3 (groups), 3.3 (concerts), 4.3 (rsvp), 4.4 (routing). Authorization middleware → 2.3, 4.4.
- §7 Frontend (5 screens, PWA, responsive, TanStack Query) → Tasks 5.1–5.3.
- §8 Kubernetes (distroless/nginx, Kustomize, Postgres StatefulSet, Ingress, Secrets, migrate Job, probes) → Tasks 6.1–6.2.
- §9 Test strategy (TDD, testcontainers, httptest, Vitest, CI) → harness in 0.4, CI in 0.1, applied throughout.
- §10 Build order → matches Phases 0→6.

**Resolved during planning:**
- `rsvp_deadline` made `NOT NULL` (spec's open question) — Task 0.3.
- Login screen includes a dev "paste ID token" affordance so the flow is testable before OAuth client IDs exist — Task 5.3.
- Router session guard handles the `Auth == nil` test case explicitly — Task 4.4 note.

**Known follow-ups (out of scope, later cycles):** payment tables/flow, Web Push + reminders (needs a scheduler/cron), native apps, external concert API, real OAuth client registration, production DB/overlay, rate limiting, structured logging/observability.




