# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

pit-pilot helps groups who go to concerts together plan and manage upcoming concerts. Monorepo:

- `backend/` — Go API (module `github.com/jaydee94/pit-pilot/backend`, Go 1.26)
- `frontend/` — React 19 + Vite + TypeScript PWA
- `deploy/` — Kubernetes manifests (Kustomize)
- `docker-compose.yml` + `Makefile` — one-command local stack
- `docs/superpowers/specs/` and `docs/superpowers/plans/` — the design spec and implementation plan for each feature **cycle** (1=core, 2=payments, 3=notifications). Read the relevant spec before changing a subsystem; it explains intent the code can't.

## Commands

Local full stack (Docker required):
- `make up` — build + start at http://localhost:8080 (postgres → auto-migrate → API → nginx-served PWA, single-origin; nginx proxies `/api` + `/auth` to the API)
- `make down` — stop (keeps the db volume); `make nuke` — remove containers + volumes + local images; `make logs` / `make ps` / `make help`

Backend (`cd backend`):
- `go test ./...` — **requires a running Docker daemon** (integration tests spin up Postgres via testcontainers-go). `go test ./internal/service/ -run TestName -v` runs one test.
- `go vet ./...` · `go build ./cmd/api`
- Regenerate the sqlc store after editing any `internal/store/queries/*.sql` or migration: `go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate`

Frontend (`cd frontend`):
- `npm run test -- --run` (one-shot) · `npm run test -- --run <pattern>` (single file) · `npm run build` (`tsc -b && vite build` — **type-checks test files too**, so tests must compile) · `npm run dev`

Deploy: `kubectl kustomize deploy/overlays/dev` renders the manifests.

## Architecture

**Backend is three layers**, wired together in `cmd/api/main.go`:
`httpapi` (HTTP handlers; parse + `WriteJSON`/`WriteError`, no SQL) → `service` (domain logic + authorization, no `net/http`) → `store/gen` (sqlc-generated, type-safe pgx queries). Typed domain errors live in `apperr` and map to HTTP status via `httpapi.WriteError`. The router (`httpapi/router.go`) takes a `Deps` struct; everything under `/api` is session-gated.

**Auth:** clients send a Google/Apple OIDC ID token to `/auth/{provider}`; the backend verifies it (`auth/oidc_verifier.go`) and issues its **own** session JWT in an httpOnly `pp_session` cookie (`auth/session.go`). `SessionMiddleware` validates it and injects the user id. **Authorization is enforced in the service layer**, not the router: `GroupService.RequireMembership` is the primitive, and `ConcertService.Get` derives the group from the concert and gates membership — concert/RSVP/payment handlers rely on this rather than URL middleware.

**sqlc / migrations:** SQL queries in `backend/internal/store/queries/*.sql` generate Go into `backend/internal/store/gen`. `sqlc.yaml` overrides make `uuid`→`google/uuid.UUID`, `timestamptz`→`time.Time` (nullable timestamptz → `*time.Time`), and nullable columns → pointers. **After adding a migration** (`internal/store/migrations/000N_*.sql`): regenerate sqlc, run `make sync-migrations` (copies them into `deploy/base/migrations/`), and add the new files to the `configMapGenerator` in `deploy/base/kustomization.yaml` — the K8s migrate Job mounts migrations from that ConfigMap. The testcontainers harness (`testutil.NewPostgres`) applies all migrations automatically.

**Notifications (transactional outbox + worker):** domain events write rows into the `notifications` outbox table **in the same transaction** as the domain change, via a `notify.Enqueuer` that services receive through `SetEnqueuer` (a deliberately optional dependency — when nil, enqueue is skipped, so pre-existing service tests stay unchanged). An in-process `worker` (started from `main.go` only when VAPID is configured) scans for time-based reminders under a Postgres advisory lock, claims pending rows with `FOR UPDATE SKIP LOCKED`, and sends Web Push (`push` package, webpush-go) to each of a user's `push_subscriptions`, deleting dead ones. Reminders carry a `dedup_key` (unique) for idempotency; event-driven rows leave it NULL.

**Frontend:** `src/api/client.ts` is a thin typed `fetch` wrapper (`credentials: include`, throws `apperr`-shaped errors); screens use TanStack Query and invalidate queries on mutations. The service worker (`src/sw.ts`, vite-plugin-pwa `injectManifest`) handles `push` + `notificationclick`. JSON field names in the TS types must match the Go struct JSON tags exactly (the backend serializes `gen` rows directly).

## Conventions

- Money is integer **cents** (`*_cents` columns/fields), never floats. Primary keys are UUIDs.
- DB enums are `CHECK`-constrained (e.g. rsvp status `yes`/`no`; payment item status `open`/`reported`/`confirmed`; notification type/status). Match these exact strings in Go.
- Multi-statement domain operations that must be atomic use `pool.Begin` + `q.WithTx(tx)` + `Commit` (see `GroupService.Create`).
- This project is built **test-first** (every cycle's plan mandates strict TDD); follow that when extending it.
