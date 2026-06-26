# pit-pilot Cycle 2 — Payment Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-concert ticket-payment tracking to pit-pilot — an opt-in responsible role, per-person amounts, and a two-stage reversible state machine (open → reported → confirmed) — on top of the Cycle 1 MVP.

**Architecture:** Extends the existing three-layer Go backend (chi handlers → service → sqlc store) with a `payment_collections` + `payment_items` aggregate, and adds a payment section to the existing React `ConcertDetail` screen. No changes to existing tables; payment authorization is derived in the service layer from `collection.responsible_user_id` / `item.user_id`, gated on top of Cycle 1's concert→group membership.

**Tech Stack:** Go 1.26, chi v5, pgx v5 + sqlc, golang-migrate, testcontainers-go; React 19 + Vite + TanStack Query + Vitest. (All already in the repo from Cycle 1.)

## Global Constraints

- Go module path: `github.com/jaydee94/pit-pilot/backend`; Go version floor `1.26`.
- **Always use the latest stable version of every library/tool** (`@latest` for sqlc, etc.) — version numbers are illustrative; fetch current latest at implementation time.
- Strict TDD: failing test first, watch it fail, implement minimally, watch it pass, commit. No implementation code without a failing test first. Docker is available — testcontainers DB tests must genuinely pass.
- Money is integer **cents** (`amount_cents`, `default_amount_cents`), never floats. UUID primary keys.
- Payment item status is exactly `'open'`, `'reported'`, or `'confirmed'`.
- One `payment_collections` row per concert (DB-enforced UNIQUE on `concert_id`).
- API errors use JSON `{"error":{"code":"...","message":"..."}}` with status 400/403/404/409.
- Layer discipline: handlers never touch SQL; services never import `net/http`; the store holds no domain rules. All payment routes live under `/api/concerts/{concertID}/payments` and are session- + group-membership-gated like Cycle 1.
- Visibility: the responsible user sees all items + a summary; a plain member sees only their own item, never others' data.
- Every task ends on a green test run and a commit. After the backend lands, migrations must be synced into `deploy/base/migrations/` (`make sync-migrations`) and listed in the kustomize `configMapGenerator`.

## Existing patterns to follow (from Cycle 1 — read these before starting)

- Migrations: `backend/internal/store/migrations/0001_init.{up,down}.sql`. sqlc config: `backend/sqlc.yaml` (overrides: `uuid`→google/uuid, `timestamptz`→`time.Time`; `emit_pointers_for_null_types` so nullable cols are pointers).
- Store: generated `backend/internal/store/gen/` — `gen.New(db)`, `(*Queries).WithTx(tx)`.
- Services: `service/groups.go` (transaction + `RequireMembership` authz pattern), `service/concerts.go` (`ConcertService.Get(ctx, userID, concertID)` gates membership + returns the concert; 404 if missing, 403 if non-member). Typed errors via `apperr` (`apperr.NotFound/Forbidden/BadRequest/Conflict`, `apperr.As`).
- Handlers: `httpapi/concert_handlers.go`, `httpapi/render.go` (`WriteJSON`, `WriteError`), `httpapi/middleware.go` (`UserID(r)`), test helper `WithUserIDForTest` in `httpapi/export_test.go`. Router: `httpapi/router.go` (`Deps` struct + `NewRouter`). Entry: `cmd/api/main.go`.
- Test harness: `testutil.NewPostgres(t)` (testcontainers Postgres + migrations).
- Frontend: `frontend/src/api/client.ts` (`apiFetch`, typed helpers), `frontend/src/routes/ConcertDetail.tsx`, TanStack Query with `invalidateQueries` on mutations.

## File Structure

```
backend/
  internal/store/migrations/0002_payments.up.sql        # new tables
  internal/store/migrations/0002_payments.down.sql
  internal/store/queries/payments.sql                   # sqlc queries
  internal/store/gen/                                    # regenerated
  internal/service/payments.go                          # PaymentService: activation, item mgmt, state machine, authz
  internal/httpapi/payment_handlers.go                  # PaymentHandlers + routes
  internal/httpapi/router.go                            # MODIFY: add payment routes + Deps field
  cmd/api/main.go                                        # MODIFY: wire PaymentService + handlers
frontend/
  src/api/client.ts                                     # MODIFY: payments helpers + types
  src/components/PaymentSection.tsx                     # new: the role/state-aware payment UI
  src/routes/ConcertDetail.tsx                          # MODIFY: render <PaymentSection/>
deploy/base/migrations/                                 # sync 0002_* here (final task)
```

---

## Phase 0 — Schema & Store

### Task 0.1: Migration for payment tables

**Files:**
- Create: `backend/internal/store/migrations/0002_payments.up.sql`
- Create: `backend/internal/store/migrations/0002_payments.down.sql`

**Interfaces:**
- Produces: `payment_collections` and `payment_items` tables consumed by Task 0.2's queries.

- [ ] **Step 1: Write the up migration**

```sql
-- backend/internal/store/migrations/0002_payments.up.sql
CREATE TABLE payment_collections (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    concert_id           uuid NOT NULL UNIQUE REFERENCES concerts(id) ON DELETE CASCADE,
    responsible_user_id  uuid NOT NULL REFERENCES users(id),
    default_amount_cents integer NOT NULL,
    payment_link         text,
    created_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE payment_items (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    collection_id uuid NOT NULL REFERENCES payment_collections(id) ON DELETE CASCADE,
    user_id       uuid NOT NULL REFERENCES users(id),
    amount_cents  integer NOT NULL,
    status        text NOT NULL DEFAULT 'open',
    reported_at   timestamptz,
    confirmed_at  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (collection_id, user_id),
    CHECK (status IN ('open','reported','confirmed'))
);

CREATE INDEX idx_payment_items_collection ON payment_items (collection_id);
```

> `pgcrypto` (for `gen_random_uuid()`) was already enabled by migration 0001 and persists.

- [ ] **Step 2: Write the down migration**

```sql
-- backend/internal/store/migrations/0002_payments.down.sql
DROP TABLE IF EXISTS payment_items;
DROP TABLE IF EXISTS payment_collections;
```

- [ ] **Step 3: Verify the migration applies (harness smoke check)**

The testcontainers harness runs ALL migrations. Confirm it still builds cleanly:
```bash
cd backend && go test ./internal/testutil/... -run TestNewPostgres
```
Expected: PASS (harness applies 0001 + 0002 without error).

- [ ] **Step 4: Commit**

```bash
git add backend/internal/store/migrations && git commit -m "feat: payment tables migration (collections + items)"
```

---

### Task 0.2: sqlc queries for payments

**Files:**
- Create: `backend/internal/store/queries/payments.sql`
- Generate: regenerate `backend/internal/store/gen/`
- Test: `backend/internal/store/gen/payments_gen_test.go`

**Interfaces:**
- Produces (generated, consumed by `PaymentService`):
  - `CreatePaymentCollection(ctx, CreatePaymentCollectionParams{ConcertID, ResponsibleUserID uuid.UUID; DefaultAmountCents int32; PaymentLink *string}) (PaymentCollection, error)`
  - `GetPaymentCollectionByConcert(ctx, uuid.UUID) (PaymentCollection, error)`
  - `UpdatePaymentCollectionLink(ctx, UpdatePaymentCollectionLinkParams{ID uuid.UUID; PaymentLink *string}) (PaymentCollection, error)`
  - `DeletePaymentCollection(ctx, uuid.UUID) error`
  - (`PaymentCollection` gains a `PaymentLink *string` field, json `payment_link`)
  - `CreatePaymentItem(ctx, CreatePaymentItemParams{CollectionID, UserID uuid.UUID; AmountCents int32}) (PaymentItem, error)`
  - `GetPaymentItem(ctx, uuid.UUID) (PaymentItem, error)`
  - `ListPaymentItems(ctx, uuid.UUID) ([]ListPaymentItemsRow, error)` (row: item id, user id, display_name, avatar_url, amount_cents, status, reported_at, confirmed_at)
  - `GetPaymentItemForUser(ctx, GetPaymentItemForUserParams{CollectionID, UserID uuid.UUID}) (ListPaymentItemsRow, error)`
  - `UpdatePaymentItemAmount(ctx, UpdatePaymentItemAmountParams{ID uuid.UUID; AmountCents int32}) (PaymentItem, error)`
  - `UpdatePaymentItemStatus(ctx, UpdatePaymentItemStatusParams{ID uuid.UUID; Status string; ReportedAt, ConfirmedAt *time.Time}) (PaymentItem, error)`
  - `DeletePaymentItem(ctx, uuid.UUID) error`
  - `ListYesRsvpUserIDs(ctx, uuid.UUID) ([]uuid.UUID, error)`

- [ ] **Step 1: Write the query file** (no leading file-path comment before the first `-- name:` — avoids the sqlc doc-comment bleed)

```sql
-- name: CreatePaymentCollection :one
INSERT INTO payment_collections (concert_id, responsible_user_id, default_amount_cents, payment_link)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetPaymentCollectionByConcert :one
SELECT * FROM payment_collections WHERE concert_id = $1;

-- name: UpdatePaymentCollectionLink :one
UPDATE payment_collections SET payment_link = $2 WHERE id = $1 RETURNING *;

-- name: DeletePaymentCollection :exec
DELETE FROM payment_collections WHERE id = $1;

-- name: CreatePaymentItem :one
INSERT INTO payment_items (collection_id, user_id, amount_cents)
VALUES ($1, $2, $3) RETURNING *;

-- name: GetPaymentItem :one
SELECT * FROM payment_items WHERE id = $1;

-- name: ListPaymentItems :many
SELECT i.id, i.user_id, u.display_name, u.avatar_url,
       i.amount_cents, i.status, i.reported_at, i.confirmed_at
FROM payment_items i
JOIN users u ON u.id = i.user_id
WHERE i.collection_id = $1
ORDER BY u.display_name;

-- name: GetPaymentItemForUser :one
SELECT i.id, i.user_id, u.display_name, u.avatar_url,
       i.amount_cents, i.status, i.reported_at, i.confirmed_at
FROM payment_items i
JOIN users u ON u.id = i.user_id
WHERE i.collection_id = $1 AND i.user_id = $2;

-- name: UpdatePaymentItemAmount :one
UPDATE payment_items SET amount_cents = $2 WHERE id = $1 RETURNING *;

-- name: UpdatePaymentItemStatus :one
UPDATE payment_items
SET status = $2, reported_at = $3, confirmed_at = $4
WHERE id = $1 RETURNING *;

-- name: DeletePaymentItem :exec
DELETE FROM payment_items WHERE id = $1;

-- name: ListYesRsvpUserIDs :many
SELECT user_id FROM rsvps WHERE concert_id = $1 AND status = 'yes';
```

- [ ] **Step 2: Regenerate**

```bash
cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
```

- [ ] **Step 3: Write the failing test**

```go
// backend/internal/store/gen/payments_gen_test.go
package gen_test

import (
	"context"
	"testing"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestPaymentCollectionAndItemLifecycle(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	owner, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "o", DisplayName: "O"})
	g, _ := q.CreateGroup(ctx, gen.CreateGroupParams{Name: "G", InviteCode: "P1", CreatedBy: owner.ID})
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := q.CreateConcert(ctx, gen.CreateConcertParams{GroupID: g.ID, Artist: "A", EventAt: when, RsvpDeadline: when.Add(-time.Hour), CreatedBy: owner.ID})

	col, err := q.CreatePaymentCollection(ctx, gen.CreatePaymentCollectionParams{
		ConcertID: c.ID, ResponsibleUserID: owner.ID, DefaultAmountCents: 4500})
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	item, err := q.CreatePaymentItem(ctx, gen.CreatePaymentItemParams{
		CollectionID: col.ID, UserID: owner.ID, AmountCents: 4500})
	if err != nil || item.Status != "open" {
		t.Fatalf("create item: %v err=%v", item, err)
	}
	now := when
	upd, err := q.UpdatePaymentItemStatus(ctx, gen.UpdatePaymentItemStatusParams{
		ID: item.ID, Status: "reported", ReportedAt: &now, ConfirmedAt: nil})
	if err != nil || upd.Status != "reported" || upd.ReportedAt == nil {
		t.Fatalf("update status: %v err=%v", upd, err)
	}
	rows, err := q.ListPaymentItems(ctx, col.ID)
	if err != nil || len(rows) != 1 || rows[0].DisplayName != "O" {
		t.Fatalf("list items: %v err=%v", rows, err)
	}
}
```

- [ ] **Step 4: Run test to verify it passes** (FAIL before generation, PASS after)

```bash
cd backend && go test ./internal/store/gen/ -run Payment
```
Expected: PASS. Then `cd backend && go test ./...` to confirm regeneration didn't break existing consumers.

- [ ] **Step 5: Commit**

```bash
git add backend && git commit -m "feat: sqlc payment queries"
```

## Phase 1 — PaymentService (domain + authorization)

All three Phase-1 tasks build `backend/internal/service/payments.go` (tests in `payments_test.go`, package `service_test`). Task 1.1 introduces the struct, view types, shared helpers, and the activation/view flow; 1.2 adds item management; 1.3 adds the state machine. Each is independently testable.

### Task 1.1: Activation, seeding, and the role-aware view

**Files:**
- Create: `backend/internal/service/payments.go`
- Test: `backend/internal/service/payments_test.go`

**Interfaces:**
- Consumes: `*pgxpool.Pool`, `*gen.Queries`, `*ConcertService` (its `Get` gates membership+existence), the payment queries from Task 0.2, `apperr`, `pgx`.
- Produces:
  - `type PaymentSummary struct { OutstandingCents, ConfirmedCents int64; OpenCount, ReportedCount, ConfirmedCount int }` (json: `outstanding_cents`, `confirmed_cents`, `open_count`, `reported_count`, `confirmed_count`)
  - `type PaymentView struct { ResponsibleUserID uuid.UUID; DefaultAmountCents int32; PaymentLink *string; IsResponsible bool; Items []gen.ListPaymentItemsRow; Summary *PaymentSummary }` (json: `responsible_user_id`, `default_amount_cents`, `payment_link`, `is_responsible`, `items`, `summary`)
  - `func NewPaymentService(pool *pgxpool.Pool, q *gen.Queries, concerts *ConcertService) *PaymentService`
  - `func (s *PaymentService) Activate(ctx, userID, concertID uuid.UUID, defaultCents int32, paymentLink *string) (gen.PaymentCollection, error)`
  - `func (s *PaymentService) Get(ctx, userID, concertID uuid.UUID) (PaymentView, error)`
  - Unexported helpers used by Tasks 1.2/1.3: `requireResponsible(ctx, userID, concertID) (gen.PaymentCollection, error)` and `loadItem(ctx, userID, concertID, itemID) (gen.PaymentCollection, gen.PaymentItem, error)`.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/service/payments_test.go
package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
	"github.com/jackc/pgxpool" // if unused remove; pool comes from testutil
)

// paySetup builds the payment service stack on a fresh DB plus a concert in a
// group, and returns the services, queries, the concert, and the owner.
func paySetup(t *testing.T) (*service.PaymentService, *service.GroupService, *gen.Queries, gen.Concert, gen.User) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "PAY" + string(rune('A'+n)) })
	cs := service.NewConcertService(q, gs)
	ps := service.NewPaymentService(pool, q, cs)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})
	_ = pgxpool.Pool{}
	return ps, gs, q, c, owner
}

func TestActivateSeedsFromYesRsvps(t *testing.T) {
	ps, gs, q, c, owner := paySetup(t)
	ctx := context.Background()
	// a second member who RSVPs yes
	friend := seedUser(t, q, "friend")
	g, _ := q.GetGroup(ctx, c.GroupID)
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: owner.ID, Status: "yes"})
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: friend.ID, Status: "yes"})

	col, err := ps.Activate(ctx, owner.ID, c.ID, 4500, nil)
	if err != nil || col.ResponsibleUserID != owner.ID {
		t.Fatalf("activate: %v err=%v", col, err)
	}
	view, err := ps.Get(ctx, owner.ID, c.ID)
	if err != nil || !view.IsResponsible || len(view.Items) != 2 {
		t.Fatalf("responsible view: %+v err=%v", view, err)
	}
	if view.Summary == nil || view.Summary.OutstandingCents != 9000 {
		t.Fatalf("summary wrong: %+v", view.Summary)
	}
}

func TestActivateTwiceConflicts(t *testing.T) {
	ps, _, _, c, owner := paySetup(t)
	ctx := context.Background()
	if _, err := ps.Activate(ctx, owner.ID, c.ID, 1000, nil); err != nil {
		t.Fatalf("first activate: %v", err)
	}
	_, err := ps.Activate(ctx, owner.ID, c.ID, 1000, nil)
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 409 {
		t.Fatalf("expected 409, got %v", err)
	}
}

func TestGetMemberSeesOnlyOwnItem(t *testing.T) {
	ps, gs, q, c, owner := paySetup(t)
	ctx := context.Background()
	friend := seedUser(t, q, "friend")
	g, _ := q.GetGroup(ctx, c.GroupID)
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: owner.ID, Status: "yes"})
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: friend.ID, Status: "yes"})
	_, _ = ps.Activate(ctx, owner.ID, c.ID, 4500, nil)

	view, err := ps.Get(ctx, friend.ID, c.ID)
	if err != nil || view.IsResponsible || len(view.Items) != 1 || view.Items[0].UserID != friend.ID {
		t.Fatalf("member view: %+v err=%v", view, err)
	}
	if view.Summary != nil {
		t.Fatalf("member must not see summary")
	}
}

func TestGetNotActiveIs404(t *testing.T) {
	ps, _, _, c, owner := paySetup(t)
	_, err := ps.Get(context.Background(), owner.ID, c.ID)
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 404 {
		t.Fatalf("expected 404, got %v", err)
	}
}
```
> Note: `seedUser(t, q, name)` already exists in `groups_test.go` (same `service_test` package). Remove the `pgxpool` import line if the toolchain flags it unused — it's only there to mirror the harness; `testutil.NewPostgres` returns the pool.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/service/ -run 'Activate|GetMember|GetNotActive'
```
Expected: FAIL — `undefined: service.NewPaymentService`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/service/payments.go
package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type PaymentSummary struct {
	OutstandingCents int64 `json:"outstanding_cents"`
	ConfirmedCents   int64 `json:"confirmed_cents"`
	OpenCount        int   `json:"open_count"`
	ReportedCount    int   `json:"reported_count"`
	ConfirmedCount   int   `json:"confirmed_count"`
}

type PaymentView struct {
	ResponsibleUserID  uuid.UUID                 `json:"responsible_user_id"`
	DefaultAmountCents int32                     `json:"default_amount_cents"`
	PaymentLink        *string                   `json:"payment_link"`
	IsResponsible      bool                      `json:"is_responsible"`
	Items              []gen.ListPaymentItemsRow `json:"items"`
	Summary            *PaymentSummary           `json:"summary"`
}

type PaymentService struct {
	pool     *pgxpool.Pool
	q        *gen.Queries
	concerts *ConcertService
}

func NewPaymentService(pool *pgxpool.Pool, q *gen.Queries, concerts *ConcertService) *PaymentService {
	return &PaymentService{pool: pool, q: q, concerts: concerts}
}

func summarize(items []gen.ListPaymentItemsRow) *PaymentSummary {
	s := &PaymentSummary{}
	for _, it := range items {
		switch it.Status {
		case "confirmed":
			s.ConfirmedCents += int64(it.AmountCents)
			s.ConfirmedCount++
		case "reported":
			s.OutstandingCents += int64(it.AmountCents)
			s.ReportedCount++
		default:
			s.OutstandingCents += int64(it.AmountCents)
			s.OpenCount++
		}
	}
	return s
}

func (s *PaymentService) Activate(ctx context.Context, userID, concertID uuid.UUID, defaultCents int32, paymentLink *string) (gen.PaymentCollection, error) {
	if defaultCents < 0 {
		return gen.PaymentCollection{}, apperr.BadRequest("invalid_amount", "amount must be non-negative")
	}
	if _, err := s.concerts.Get(ctx, userID, concertID); err != nil {
		return gen.PaymentCollection{}, err
	}
	_, err := s.q.GetPaymentCollectionByConcert(ctx, concertID)
	if err == nil {
		return gen.PaymentCollection{}, apperr.Conflict("already_active", "payment tracking is already active")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return gen.PaymentCollection{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return gen.PaymentCollection{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := s.q.WithTx(tx)

	col, err := qtx.CreatePaymentCollection(ctx, gen.CreatePaymentCollectionParams{
		ConcertID: concertID, ResponsibleUserID: userID, DefaultAmountCents: defaultCents, PaymentLink: paymentLink})
	if err != nil {
		return gen.PaymentCollection{}, err
	}
	yes, err := qtx.ListYesRsvpUserIDs(ctx, concertID)
	if err != nil {
		return gen.PaymentCollection{}, err
	}
	for _, uid := range yes {
		if _, err := qtx.CreatePaymentItem(ctx, gen.CreatePaymentItemParams{
			CollectionID: col.ID, UserID: uid, AmountCents: defaultCents}); err != nil {
			return gen.PaymentCollection{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return gen.PaymentCollection{}, err
	}
	return col, nil
}

func (s *PaymentService) Get(ctx context.Context, userID, concertID uuid.UUID) (PaymentView, error) {
	if _, err := s.concerts.Get(ctx, userID, concertID); err != nil {
		return PaymentView{}, err
	}
	col, err := s.q.GetPaymentCollectionByConcert(ctx, concertID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentView{}, apperr.NotFound("payment_not_active", "no payment tracking for this concert")
	}
	if err != nil {
		return PaymentView{}, err
	}
	view := PaymentView{
		ResponsibleUserID:  col.ResponsibleUserID,
		DefaultAmountCents: col.DefaultAmountCents,
		PaymentLink:        col.PaymentLink,
		IsResponsible:      col.ResponsibleUserID == userID,
		Items:              []gen.ListPaymentItemsRow{},
	}
	if view.IsResponsible {
		items, err := s.q.ListPaymentItems(ctx, col.ID)
		if err != nil {
			return PaymentView{}, err
		}
		view.Items = items
		view.Summary = summarize(items)
		return view, nil
	}
	own, err := s.q.GetPaymentItemForUser(ctx, gen.GetPaymentItemForUserParams{CollectionID: col.ID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return view, nil // member with no item
	}
	if err != nil {
		return PaymentView{}, err
	}
	view.Items = []gen.ListPaymentItemsRow{own}
	return view, nil
}

// requireResponsible gates membership, loads the active collection, and verifies
// the caller is its responsible user.
func (s *PaymentService) requireResponsible(ctx context.Context, userID, concertID uuid.UUID) (gen.PaymentCollection, error) {
	if _, err := s.concerts.Get(ctx, userID, concertID); err != nil {
		return gen.PaymentCollection{}, err
	}
	col, err := s.q.GetPaymentCollectionByConcert(ctx, concertID)
	if errors.Is(err, pgx.ErrNoRows) {
		return gen.PaymentCollection{}, apperr.NotFound("payment_not_active", "no payment tracking for this concert")
	}
	if err != nil {
		return gen.PaymentCollection{}, err
	}
	if col.ResponsibleUserID != userID {
		return gen.PaymentCollection{}, apperr.Forbidden("not_responsible", "only the ticket organizer can do this")
	}
	return col, nil
}

// loadItem gates membership, loads the active collection and the item, and
// verifies the item belongs to that collection.
func (s *PaymentService) loadItem(ctx context.Context, userID, concertID, itemID uuid.UUID) (gen.PaymentCollection, gen.PaymentItem, error) {
	if _, err := s.concerts.Get(ctx, userID, concertID); err != nil {
		return gen.PaymentCollection{}, gen.PaymentItem{}, err
	}
	col, err := s.q.GetPaymentCollectionByConcert(ctx, concertID)
	if errors.Is(err, pgx.ErrNoRows) {
		return gen.PaymentCollection{}, gen.PaymentItem{}, apperr.NotFound("payment_not_active", "no payment tracking for this concert")
	}
	if err != nil {
		return gen.PaymentCollection{}, gen.PaymentItem{}, err
	}
	item, err := s.q.GetPaymentItem(ctx, itemID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && item.CollectionID != col.ID) {
		return gen.PaymentCollection{}, gen.PaymentItem{}, apperr.NotFound("item_not_found", "payment item not found")
	}
	if err != nil {
		return gen.PaymentCollection{}, gen.PaymentItem{}, err
	}
	return col, item, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/service/ -run 'Activate|GetMember|GetNotActive'
```
Expected: PASS. Then `cd backend && go test ./...` (full module green).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/payments.go backend/internal/service/payments_test.go && git commit -m "feat: payment service activation, seeding, and role-aware view"
```

---

### Task 1.2: Item management (add / set amount / remove / deactivate)

**Files:**
- Modify: `backend/internal/service/payments.go`
- Test: `backend/internal/service/payments_test.go` (add tests)

**Interfaces:**
- Consumes: `requireResponsible`, `loadItem` from Task 1.1.
- Produces:
  - `func (s *PaymentService) AddItem(ctx, userID, concertID, targetUserID uuid.UUID, amountCents *int32) (gen.ListPaymentItemsRow, error)` — responsible-only; defaults to the collection's default; `409` if the target already has an item.
  - `func (s *PaymentService) SetAmount(ctx, userID, concertID, itemID uuid.UUID, amountCents int32) (gen.PaymentItem, error)` — responsible-only.
  - `func (s *PaymentService) RemoveItem(ctx, userID, concertID, itemID uuid.UUID) error` — responsible-only.
  - `func (s *PaymentService) Deactivate(ctx, userID, concertID uuid.UUID) error` — responsible-only; deletes the collection (cascades items).
  - `func (s *PaymentService) SetPaymentLink(ctx, userID, concertID uuid.UUID, link *string) (gen.PaymentCollection, error)` — responsible-only; sets or (with `nil`) clears the collection's pay-me link.

- [ ] **Step 1: Write the failing test**

```go
// append to backend/internal/service/payments_test.go
func TestItemManagementResponsibleOnly(t *testing.T) {
	ps, gs, q, c, owner := paySetup(t)
	ctx := context.Background()
	friend := seedUser(t, q, "friend")
	g, _ := q.GetGroup(ctx, c.GroupID)
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)
	_, _ = ps.Activate(ctx, owner.ID, c.ID, 4500, nil) // no yes-rsvps yet → empty list

	// responsible adds an item for friend at default
	row, err := ps.AddItem(ctx, owner.ID, c.ID, friend.ID, nil)
	if err != nil || row.AmountCents != 4500 || row.UserID != friend.ID {
		t.Fatalf("add item: %+v err=%v", row, err)
	}
	// adding the same user again conflicts
	if _, err := ps.AddItem(ctx, owner.ID, c.ID, friend.ID, nil); err == nil {
		t.Fatal("expected conflict adding duplicate item")
	}
	// a non-responsible member cannot add
	if _, err := ps.AddItem(ctx, friend.ID, c.ID, owner.ID, nil); err == nil {
		t.Fatal("non-responsible must not add items")
	}
	// set amount + remove
	upd, err := ps.SetAmount(ctx, owner.ID, c.ID, row.ID, 3000)
	if err != nil || upd.AmountCents != 3000 {
		t.Fatalf("set amount: %+v err=%v", upd, err)
	}
	if err := ps.RemoveItem(ctx, owner.ID, c.ID, row.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
}

func TestSetPaymentLink(t *testing.T) {
	ps, gs, q, c, owner := paySetup(t)
	ctx := context.Background()
	friend := seedUser(t, q, "friend")
	g, _ := q.GetGroup(ctx, c.GroupID)
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: friend.ID, Status: "yes"})
	_, _ = ps.Activate(ctx, owner.ID, c.ID, 4500, nil)

	link := "https://paypal.me/owner/45"
	if _, err := ps.SetPaymentLink(ctx, owner.ID, c.ID, &link); err != nil {
		t.Fatalf("set link: %v", err)
	}
	// the owing member sees the link on their view
	view, err := ps.Get(ctx, friend.ID, c.ID)
	if err != nil || view.PaymentLink == nil || *view.PaymentLink != link {
		t.Fatalf("member should see link: %+v err=%v", view.PaymentLink, err)
	}
	// a non-responsible member cannot set it
	if _, err := ps.SetPaymentLink(ctx, friend.ID, c.ID, &link); err == nil {
		t.Fatal("non-responsible must not set the link")
	}
}

func TestDeactivateResponsibleOnly(t *testing.T) {
	ps, gs, q, c, owner := paySetup(t)
	ctx := context.Background()
	friend := seedUser(t, q, "friend")
	g, _ := q.GetGroup(ctx, c.GroupID)
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)
	_, _ = ps.Activate(ctx, owner.ID, c.ID, 4500, nil)

	if err := ps.Deactivate(ctx, friend.ID, c.ID); err == nil {
		t.Fatal("non-responsible must not deactivate")
	}
	if err := ps.Deactivate(ctx, owner.ID, c.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := ps.Get(ctx, owner.ID, c.ID); err == nil {
		t.Fatal("expected 404 after deactivate")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/service/ -run 'ItemManagement|Deactivate|SetPaymentLink'
```
Expected: FAIL — `undefined: ... AddItem`.

- [ ] **Step 3: Write minimal implementation** (append to `payments.go`)

```go
func (s *PaymentService) AddItem(ctx context.Context, userID, concertID, targetUserID uuid.UUID, amountCents *int32) (gen.ListPaymentItemsRow, error) {
	col, err := s.requireResponsible(ctx, userID, concertID)
	if err != nil {
		return gen.ListPaymentItemsRow{}, err
	}
	amount := col.DefaultAmountCents
	if amountCents != nil {
		amount = *amountCents
	}
	if _, err := s.q.GetPaymentItemForUser(ctx, gen.GetPaymentItemForUserParams{CollectionID: col.ID, UserID: targetUserID}); err == nil {
		return gen.ListPaymentItemsRow{}, apperr.Conflict("item_exists", "this person already has a payment item")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return gen.ListPaymentItemsRow{}, err
	}
	if _, err := s.q.CreatePaymentItem(ctx, gen.CreatePaymentItemParams{
		CollectionID: col.ID, UserID: targetUserID, AmountCents: amount}); err != nil {
		return gen.ListPaymentItemsRow{}, err
	}
	return s.q.GetPaymentItemForUser(ctx, gen.GetPaymentItemForUserParams{CollectionID: col.ID, UserID: targetUserID})
}

func (s *PaymentService) SetAmount(ctx context.Context, userID, concertID, itemID uuid.UUID, amountCents int32) (gen.PaymentItem, error) {
	if _, _, err := s.loadItemAsResponsible(ctx, userID, concertID, itemID); err != nil {
		return gen.PaymentItem{}, err
	}
	if amountCents < 0 {
		return gen.PaymentItem{}, apperr.BadRequest("invalid_amount", "amount must be non-negative")
	}
	return s.q.UpdatePaymentItemAmount(ctx, gen.UpdatePaymentItemAmountParams{ID: itemID, AmountCents: amountCents})
}

func (s *PaymentService) RemoveItem(ctx context.Context, userID, concertID, itemID uuid.UUID) error {
	if _, _, err := s.loadItemAsResponsible(ctx, userID, concertID, itemID); err != nil {
		return err
	}
	return s.q.DeletePaymentItem(ctx, itemID)
}

func (s *PaymentService) Deactivate(ctx context.Context, userID, concertID uuid.UUID) error {
	col, err := s.requireResponsible(ctx, userID, concertID)
	if err != nil {
		return err
	}
	return s.q.DeletePaymentCollection(ctx, col.ID)
}

func (s *PaymentService) SetPaymentLink(ctx context.Context, userID, concertID uuid.UUID, link *string) (gen.PaymentCollection, error) {
	col, err := s.requireResponsible(ctx, userID, concertID)
	if err != nil {
		return gen.PaymentCollection{}, err
	}
	return s.q.UpdatePaymentCollectionLink(ctx, gen.UpdatePaymentCollectionLinkParams{ID: col.ID, PaymentLink: link})
}

// loadItemAsResponsible loads an item and verifies the caller is the collection's responsible user.
func (s *PaymentService) loadItemAsResponsible(ctx context.Context, userID, concertID, itemID uuid.UUID) (gen.PaymentCollection, gen.PaymentItem, error) {
	col, item, err := s.loadItem(ctx, userID, concertID, itemID)
	if err != nil {
		return gen.PaymentCollection{}, gen.PaymentItem{}, err
	}
	if col.ResponsibleUserID != userID {
		return gen.PaymentCollection{}, gen.PaymentItem{}, apperr.Forbidden("not_responsible", "only the ticket organizer can do this")
	}
	return col, item, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/service/ -run 'ItemManagement|Deactivate|SetPaymentLink'
```
Expected: PASS. Then `cd backend && go test ./...`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service && git commit -m "feat: payment item management (add/set-amount/remove/deactivate)"
```

---

### Task 1.3: State machine (report / un-report / confirm / un-confirm)

**Files:**
- Modify: `backend/internal/service/payments.go`
- Test: `backend/internal/service/payments_test.go` (add tests)

**Interfaces:**
- Consumes: `loadItem`, `loadItemAsResponsible` from Tasks 1.1/1.2.
- Produces (each returns the updated `gen.PaymentItem`):
  - `Report(ctx, userID, concertID, itemID uuid.UUID) (gen.PaymentItem, error)` — item owner only; `open`→`reported`.
  - `UnReport(...) (gen.PaymentItem, error)` — owner only; `reported`→`open`.
  - `Confirm(...) (gen.PaymentItem, error)` — responsible only; `open`/`reported`→`confirmed`.
  - `UnConfirm(...) (gen.PaymentItem, error)` — responsible only; `confirmed`→`reported` (if `reported_at` set) else `open`.
  - Wrong actor → `apperr.Forbidden`; wrong current status → `apperr.BadRequest("invalid_transition", ...)`.

- [ ] **Step 1: Write the failing test**

```go
// append to backend/internal/service/payments_test.go
func paySetupWithItem(t *testing.T) (*service.PaymentService, *gen.Queries, gen.Concert, gen.User, gen.User, gen.ListPaymentItemsRow) {
	ps, gs, q, c, owner := paySetup(t)
	ctx := context.Background()
	friend := seedUser(t, q, "friend")
	g, _ := q.GetGroup(ctx, c.GroupID)
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: friend.ID, Status: "yes"})
	_, _ = ps.Activate(ctx, owner.ID, c.ID, 4500, nil)
	view, _ := ps.Get(ctx, owner.ID, c.ID)
	return ps, q, c, owner, friend, view.Items[0] // friend's item
}

func TestReportConfirmRoundTrip(t *testing.T) {
	ps, _, c, owner, friend, item := paySetupWithItem(t)
	ctx := context.Background()

	// owner (not the item owner) cannot report friend's item
	if _, err := ps.Report(ctx, owner.ID, c.ID, item.ID); err == nil {
		t.Fatal("non-owner must not report")
	}
	// friend reports their own item
	r, err := ps.Report(ctx, friend.ID, c.ID, item.ID)
	if err != nil || r.Status != "reported" || r.ReportedAt == nil {
		t.Fatalf("report: %+v err=%v", r, err)
	}
	// friend cannot confirm (only responsible)
	if _, err := ps.Confirm(ctx, friend.ID, c.ID, item.ID); err == nil {
		t.Fatal("non-responsible must not confirm")
	}
	// owner (responsible) confirms
	cf, err := ps.Confirm(ctx, owner.ID, c.ID, item.ID)
	if err != nil || cf.Status != "confirmed" || cf.ConfirmedAt == nil {
		t.Fatalf("confirm: %+v err=%v", cf, err)
	}
	// owner un-confirms → back to reported (reported_at was set)
	uc, err := ps.UnConfirm(ctx, owner.ID, c.ID, item.ID)
	if err != nil || uc.Status != "reported" || uc.ConfirmedAt != nil {
		t.Fatalf("unconfirm: %+v err=%v", uc, err)
	}
	// friend un-reports → open
	ur, err := ps.UnReport(ctx, friend.ID, c.ID, item.ID)
	if err != nil || ur.Status != "open" || ur.ReportedAt != nil {
		t.Fatalf("unreport: %+v err=%v", ur, err)
	}
}

func TestInvalidTransitionIsBadRequest(t *testing.T) {
	ps, _, c, _, friend, item := paySetupWithItem(t)
	ctx := context.Background()
	// un-report on an open item is invalid
	_, err := ps.UnReport(ctx, friend.ID, c.ID, item.ID)
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 400 {
		t.Fatalf("expected 400 invalid transition, got %v", err)
	}
}

func TestDirectConfirmFromOpen(t *testing.T) {
	ps, _, c, owner, _, item := paySetupWithItem(t)
	ctx := context.Background()
	// responsible confirms an open item directly (cash in hand)
	cf, err := ps.Confirm(ctx, owner.ID, c.ID, item.ID)
	if err != nil || cf.Status != "confirmed" {
		t.Fatalf("direct confirm: %+v err=%v", cf, err)
	}
	// un-confirm → open (no reported_at)
	uc, err := ps.UnConfirm(ctx, owner.ID, c.ID, item.ID)
	if err != nil || uc.Status != "open" {
		t.Fatalf("unconfirm to open: %+v err=%v", uc, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/service/ -run 'ReportConfirm|InvalidTransition|DirectConfirm'
```
Expected: FAIL — `undefined: ... Report`.

- [ ] **Step 3: Write minimal implementation** (append to `payments.go`)

```go
func (s *PaymentService) Report(ctx context.Context, userID, concertID, itemID uuid.UUID) (gen.PaymentItem, error) {
	_, item, err := s.loadItem(ctx, userID, concertID, itemID)
	if err != nil {
		return gen.PaymentItem{}, err
	}
	if item.UserID != userID {
		return gen.PaymentItem{}, apperr.Forbidden("not_owner", "you can only report your own payment")
	}
	if item.Status != "open" {
		return gen.PaymentItem{}, apperr.BadRequest("invalid_transition", "can only report an open item")
	}
	now := time.Now()
	return s.q.UpdatePaymentItemStatus(ctx, gen.UpdatePaymentItemStatusParams{
		ID: itemID, Status: "reported", ReportedAt: &now, ConfirmedAt: nil})
}

func (s *PaymentService) UnReport(ctx context.Context, userID, concertID, itemID uuid.UUID) (gen.PaymentItem, error) {
	_, item, err := s.loadItem(ctx, userID, concertID, itemID)
	if err != nil {
		return gen.PaymentItem{}, err
	}
	if item.UserID != userID {
		return gen.PaymentItem{}, apperr.Forbidden("not_owner", "you can only change your own payment")
	}
	if item.Status != "reported" {
		return gen.PaymentItem{}, apperr.BadRequest("invalid_transition", "can only un-report a reported item")
	}
	return s.q.UpdatePaymentItemStatus(ctx, gen.UpdatePaymentItemStatusParams{
		ID: itemID, Status: "open", ReportedAt: nil, ConfirmedAt: nil})
}

func (s *PaymentService) Confirm(ctx context.Context, userID, concertID, itemID uuid.UUID) (gen.PaymentItem, error) {
	_, item, err := s.loadItemAsResponsible(ctx, userID, concertID, itemID)
	if err != nil {
		return gen.PaymentItem{}, err
	}
	if item.Status == "confirmed" {
		return gen.PaymentItem{}, apperr.BadRequest("invalid_transition", "already confirmed")
	}
	now := time.Now()
	return s.q.UpdatePaymentItemStatus(ctx, gen.UpdatePaymentItemStatusParams{
		ID: itemID, Status: "confirmed", ReportedAt: item.ReportedAt, ConfirmedAt: &now})
}

func (s *PaymentService) UnConfirm(ctx context.Context, userID, concertID, itemID uuid.UUID) (gen.PaymentItem, error) {
	_, item, err := s.loadItemAsResponsible(ctx, userID, concertID, itemID)
	if err != nil {
		return gen.PaymentItem{}, err
	}
	if item.Status != "confirmed" {
		return gen.PaymentItem{}, apperr.BadRequest("invalid_transition", "item is not confirmed")
	}
	status := "open"
	if item.ReportedAt != nil {
		status = "reported"
	}
	return s.q.UpdatePaymentItemStatus(ctx, gen.UpdatePaymentItemStatusParams{
		ID: itemID, Status: status, ReportedAt: item.ReportedAt, ConfirmedAt: nil})
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/service/ -run 'ReportConfirm|InvalidTransition|DirectConfirm'
```
Expected: PASS. Then `cd backend && go test ./...`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service && git commit -m "feat: payment state machine (report/un-report/confirm/un-confirm)"
```

## Phase 2 — HTTP handlers & wiring

### Task 2.1: Payment HTTP handlers

**Files:**
- Create: `backend/internal/httpapi/payment_handlers.go`
- Test: `backend/internal/httpapi/payment_handlers_test.go`

**Interfaces:**
- Consumes: `service.PaymentService` (all methods from Phase 1), `UserID`, `WriteJSON`/`WriteError`, `apperr`, chi URL params, `WithUserIDForTest`.
- Produces:
  - `type PaymentHandlers struct { Payments *service.PaymentService }`
  - Handlers: `Activate` (POST, 201), `Get` (GET, 200), `SetLink` (PATCH, 200), `Deactivate` (DELETE, 204), `AddItem` (POST items, 201), `SetAmount` (PATCH item, 200), `RemoveItem` (DELETE item, 204), `Report`/`UnReport`/`Confirm`/`UnConfirm` (200). Bad UUIDs / malformed bodies → 400; all service errors via `WriteError`.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/httpapi/payment_handlers_test.go
package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/httpapi"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

// concertReq builds a request carrying userID + a chi {concertID} param.
func concertReq(method, body string, userID, concertID uuid.UUID) *http.Request {
	r := httpapi.WithUserIDForTest(httptest.NewRequest(method, "/p", strings.NewReader(body)), userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("concertID", concertID.String())
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func paymentTestStack(t *testing.T) (*httpapi.PaymentHandlers, *gen.Queries, gen.Concert, gen.User) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "PH" + string(rune('A'+n)) })
	cs := service.NewConcertService(q, gs)
	ps := service.NewPaymentService(pool, q, cs)
	ctx := context.Background()
	owner, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "o", DisplayName: "O"})
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: owner.ID, Status: "yes"})
	return &httpapi.PaymentHandlers{Payments: ps}, q, c, owner
}

func TestActivateAndGetWithLink(t *testing.T) {
	h, _, c, owner := paymentTestStack(t)
	rec := httptest.NewRecorder()
	h.Activate(rec, concertReq(http.MethodPost, `{"default_amount_cents":4500,"payment_link":"https://paypal.me/o/45"}`, owner.ID, c.ID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("activate status %d body %s", rec.Code, rec.Body.String())
	}
	getRec := httptest.NewRecorder()
	h.Get(getRec, concertReq(http.MethodGet, "", owner.ID, c.ID))
	if getRec.Code != http.StatusOK ||
		!strings.Contains(getRec.Body.String(), "paypal.me") ||
		!strings.Contains(getRec.Body.String(), `"is_responsible":true`) {
		t.Fatalf("get failed: %d %s", getRec.Code, getRec.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/httpapi/ -run 'ActivateAndGet'
```
Expected: FAIL — `undefined: httpapi.PaymentHandlers`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/httpapi/payment_handlers.go
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type PaymentHandlers struct {
	Payments *service.PaymentService
}

func concertID(r *http.Request) (uuid.UUID, error) { return uuid.Parse(chi.URLParam(r, "concertID")) }
func itemID(r *http.Request) (uuid.UUID, error)    { return uuid.Parse(chi.URLParam(r, "itemID")) }

func (h *PaymentHandlers) Activate(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	var b struct {
		DefaultAmountCents int32   `json:"default_amount_cents"`
		PaymentLink        *string `json:"payment_link"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "malformed json"))
		return
	}
	col, err := h.Payments.Activate(r.Context(), uid, cid, b.DefaultAmountCents, b.PaymentLink)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, col)
}

func (h *PaymentHandlers) Get(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	view, err := h.Payments.Get(r.Context(), uid, cid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, view)
}

func (h *PaymentHandlers) SetLink(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	var b struct {
		PaymentLink *string `json:"payment_link"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "malformed json"))
		return
	}
	col, err := h.Payments.SetPaymentLink(r.Context(), uid, cid, b.PaymentLink)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, col)
}

func (h *PaymentHandlers) Deactivate(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	if err := h.Payments.Deactivate(r.Context(), uid, cid); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *PaymentHandlers) AddItem(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	var b struct {
		UserID      string `json:"user_id"`
		AmountCents *int32 `json:"amount_cents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "malformed json"))
		return
	}
	target, err := uuid.Parse(b.UserID)
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_user", "bad user id"))
		return
	}
	row, err := h.Payments.AddItem(r.Context(), uid, cid, target, b.AmountCents)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, row)
}

func (h *PaymentHandlers) SetAmount(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	iid, err2 := itemID(r)
	if err != nil || err2 != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad id"))
		return
	}
	var b struct {
		AmountCents int32 `json:"amount_cents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "malformed json"))
		return
	}
	item, err := h.Payments.SetAmount(r.Context(), uid, cid, iid, b.AmountCents)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, item)
}

func (h *PaymentHandlers) RemoveItem(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	iid, err2 := itemID(r)
	if err != nil || err2 != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad id"))
		return
	}
	if err := h.Payments.RemoveItem(r.Context(), uid, cid, iid); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// The four state-machine transitions share parsing + response shape, so they
// delegate to a small typed helper.
func (h *PaymentHandlers) Report(w http.ResponseWriter, r *http.Request)    { h.transition(w, r, h.Payments.Report) }
func (h *PaymentHandlers) UnReport(w http.ResponseWriter, r *http.Request)  { h.transition(w, r, h.Payments.UnReport) }
func (h *PaymentHandlers) Confirm(w http.ResponseWriter, r *http.Request)   { h.transition(w, r, h.Payments.Confirm) }
func (h *PaymentHandlers) UnConfirm(w http.ResponseWriter, r *http.Request) { h.transition(w, r, h.Payments.UnConfirm) }

func (h *PaymentHandlers) transition(w http.ResponseWriter, r *http.Request,
	fn func(ctx context.Context, uid, cid, iid uuid.UUID) (gen.PaymentItem, error)) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	iid, err2 := itemID(r)
	if err != nil || err2 != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad id"))
		return
	}
	item, err := fn(r.Context(), uid, cid, iid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, item)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/httpapi/ -run 'ActivateAndGet' && go test ./...
```
Expected: PASS, full module green.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/payment_handlers.go backend/internal/httpapi/payment_handlers_test.go && git commit -m "feat: payment http handlers"
```

---

### Task 2.2: Wire payment routes into the router and main

**Files:**
- Modify: `backend/internal/httpapi/router.go`
- Modify: `backend/cmd/api/main.go`
- Test: `backend/internal/httpapi/router_test.go` (add a route-presence assertion)

**Interfaces:**
- Consumes: `PaymentHandlers`.
- Produces: `Deps` gains `Payments *PaymentHandlers`; the `/api` tree registers the payment routes; `main.go` constructs and injects the service + handlers.

- [ ] **Step 1: Write the failing test**

```go
// append to backend/internal/httpapi/router_test.go
func TestPaymentRouteRequiresSession(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Deps{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/concerts/"+uuid.NewString()+"/payments", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without session, got %d", rec.Code)
	}
}
```
> Add `"github.com/google/uuid"` to the router_test imports if not present.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/httpapi/ -run 'PaymentRoute'
```
Expected: FAIL — the route isn't registered (404, not 401) until the nil-Deps guard covers it; once `Deps.Payments` routes exist under the guarded `/api` tree it returns 401. (With empty `Deps`, the existing `/api` catch-all 401 guard from Cycle 1 already returns 401 — so this test passes only once you confirm the payment path is under `/api`. If it currently 404s, the route is mis-scoped.)

- [ ] **Step 3: Write minimal implementation**

In `router.go`, add the field to `Deps`:
```go
type Deps struct {
	Auth     *AuthHandlers
	Groups   *GroupHandlers
	Concerts *ConcertHandlers
	RSVPs    *RSVPHandlers
	Payments *PaymentHandlers
	GroupSvc *service.GroupService
	Ready    func(ctx context.Context) error
}
```
Inside the `/api` route group (the same closure that registers `/concerts/{concertID}`), add:
```go
if d.Payments != nil {
	api.Route("/concerts/{concertID}/payments", func(p chi.Router) {
		p.Post("/", d.Payments.Activate)
		p.Get("/", d.Payments.Get)
		p.Patch("/", d.Payments.SetLink)
		p.Delete("/", d.Payments.Deactivate)
		p.Post("/items", d.Payments.AddItem)
		p.Patch("/items/{itemID}", d.Payments.SetAmount)
		p.Delete("/items/{itemID}", d.Payments.RemoveItem)
		p.Post("/items/{itemID}/report", d.Payments.Report)
		p.Delete("/items/{itemID}/report", d.Payments.UnReport)
		p.Post("/items/{itemID}/confirm", d.Payments.Confirm)
		p.Delete("/items/{itemID}/confirm", d.Payments.UnConfirm)
	})
}
```
> Membership is gated in the service via `ConcertService.Get` (concert → group), consistent with the Cycle 1 concert/RSVP routes — no `RequireGroupMember` middleware needed here because the group is derived from the concert, not the URL.

In `cmd/api/main.go`, after the existing service construction, add:
```go
payments := service.NewPaymentService(pool, q, concerts)
```
and add to the `Deps{...}` literal:
```go
Payments: &httpapi.PaymentHandlers{Payments: payments},
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/httpapi/ -run 'PaymentRoute|Health|Protected' && go build ./cmd/api && go vet ./... && go test ./...
```
Expected: all PASS, binary builds, full module green.

- [ ] **Step 5: Commit**

```bash
git add backend && git commit -m "feat: wire payment routes and service into router and main"
```

---

## Phase 3 — Frontend

### Task 3.1: Payment API client area

**Files:**
- Modify: `frontend/src/api/client.ts`
- Test: `frontend/src/api/payments.test.ts`

**Interfaces:**
- Produces (typed helpers + types reused by the UI):
  - Types: `PaymentItem = { id, user_id, display_name, avatar_url?, amount_cents, status: "open"|"reported"|"confirmed", reported_at?: string|null, confirmed_at?: string|null }`; `PaymentSummary = { outstanding_cents, confirmed_cents, open_count, reported_count, confirmed_count }`; `PaymentView = { responsible_user_id, default_amount_cents, payment_link: string|null, is_responsible, items: PaymentItem[], summary: PaymentSummary|null }`.
  - Helpers: `activatePayments(concertId, default_amount_cents, payment_link?)`, `getPayments(concertId)`, `setPaymentLink(concertId, payment_link)`, `deactivatePayments(concertId)`, `addPaymentItem(concertId, user_id, amount_cents?)`, `setPaymentAmount(concertId, itemId, amount_cents)`, `removePaymentItem(concertId, itemId)`, `reportPayment(concertId, itemId)`, `unreportPayment(concertId, itemId)`, `confirmPayment(concertId, itemId)`, `unconfirmPayment(concertId, itemId)`.

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/api/payments.test.ts
import { afterEach, expect, test, vi } from "vitest";
import { getPayments, activatePayments } from "./client";

afterEach(() => vi.restoreAllMocks());

test("getPayments returns the view incl. payment_link", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify({
      responsible_user_id: "u1", default_amount_cents: 4500, payment_link: "https://paypal.me/o/45",
      is_responsible: false, items: [], summary: null,
    }), { status: 200 }),
  ));
  const v = await getPayments("c1");
  expect(v.payment_link).toBe("https://paypal.me/o/45");
  expect(v.is_responsible).toBe(false);
});

test("activatePayments posts amount + link", async () => {
  const fetchMock = vi.fn(async () =>
    new Response(JSON.stringify({ id: "col1" }), { status: 201 }));
  vi.stubGlobal("fetch", fetchMock);
  await activatePayments("c1", 4500, "https://paypal.me/o/45");
  const [, init] = fetchMock.mock.calls[0];
  expect(init.method).toBe("POST");
  expect(JSON.parse(init.body as string)).toMatchObject({ default_amount_cents: 4500, payment_link: "https://paypal.me/o/45" });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd frontend && npm run test -- --run payments
```
Expected: FAIL — `getPayments` not exported.

- [ ] **Step 3: Write minimal implementation** (append to `frontend/src/api/client.ts`)

```ts
export type PaymentItem = {
  id: string; user_id: string; display_name: string; avatar_url?: string;
  amount_cents: number; status: "open" | "reported" | "confirmed";
  reported_at?: string | null; confirmed_at?: string | null;
};
export type PaymentSummary = {
  outstanding_cents: number; confirmed_cents: number;
  open_count: number; reported_count: number; confirmed_count: number;
};
export type PaymentView = {
  responsible_user_id: string; default_amount_cents: number; payment_link: string | null;
  is_responsible: boolean; items: PaymentItem[]; summary: PaymentSummary | null;
};

const base = (concertId: string) => `/api/concerts/${concertId}/payments`;

export const getPayments = (concertId: string) => apiFetch<PaymentView>(base(concertId));
export const activatePayments = (concertId: string, default_amount_cents: number, payment_link?: string) =>
  apiFetch<{ id: string }>(base(concertId), { method: "POST", body: JSON.stringify({ default_amount_cents, payment_link: payment_link ?? null }) });
export const setPaymentLink = (concertId: string, payment_link: string | null) =>
  apiFetch<{ id: string }>(base(concertId), { method: "PATCH", body: JSON.stringify({ payment_link }) });
export const deactivatePayments = (concertId: string) =>
  apiFetch<void>(base(concertId), { method: "DELETE" });
export const addPaymentItem = (concertId: string, user_id: string, amount_cents?: number) =>
  apiFetch<PaymentItem>(`${base(concertId)}/items`, { method: "POST", body: JSON.stringify({ user_id, amount_cents: amount_cents ?? null }) });
export const setPaymentAmount = (concertId: string, itemId: string, amount_cents: number) =>
  apiFetch<PaymentItem>(`${base(concertId)}/items/${itemId}`, { method: "PATCH", body: JSON.stringify({ amount_cents }) });
export const removePaymentItem = (concertId: string, itemId: string) =>
  apiFetch<void>(`${base(concertId)}/items/${itemId}`, { method: "DELETE" });
export const reportPayment = (concertId: string, itemId: string) =>
  apiFetch<PaymentItem>(`${base(concertId)}/items/${itemId}/report`, { method: "POST" });
export const unreportPayment = (concertId: string, itemId: string) =>
  apiFetch<PaymentItem>(`${base(concertId)}/items/${itemId}/report`, { method: "DELETE" });
export const confirmPayment = (concertId: string, itemId: string) =>
  apiFetch<PaymentItem>(`${base(concertId)}/items/${itemId}/confirm`, { method: "POST" });
export const unconfirmPayment = (concertId: string, itemId: string) =>
  apiFetch<PaymentItem>(`${base(concertId)}/items/${itemId}/confirm`, { method: "DELETE" });
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd frontend && npm run test -- --run payments && npm run build
```
Expected: PASS + build clean.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api && git commit -m "feat: payment api client area"
```

---

### Task 3.2: PaymentSection component on ConcertDetail

**Files:**
- Create: `frontend/src/components/PaymentSection.tsx`
- Modify: `frontend/src/routes/ConcertDetail.tsx`
- Test: `frontend/src/components/PaymentSection.test.tsx`

**Interfaces:**
- Consumes: the payment client helpers/types from Task 3.1, TanStack Query.
- Produces: `function PaymentSection({ concertId }: { concertId: string })` — renders by role/state: inactive (activate button + amount/link dialog), responsible (item list + amounts + add/remove + confirm/un-confirm + summary + editable link), own-item (amount, status, bezahlt/zurücknehmen, and a "Per PayPal zahlen" link when `payment_link` is set), no-item (quiet note). Mounted in `ConcertDetail` below the RSVP list.

- [ ] **Step 1: Write the failing tests**

```tsx
// frontend/src/components/PaymentSection.test.tsx
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, test, vi } from "vitest";
import PaymentSection from "./PaymentSection";

afterEach(() => vi.restoreAllMocks());

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

test("member with an item sees amount and a PayPal pay link", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify({
      responsible_user_id: "u1", default_amount_cents: 4500, payment_link: "https://paypal.me/o/45",
      is_responsible: false,
      items: [{ id: "i1", user_id: "me", display_name: "Me", amount_cents: 4500, status: "open" }],
      summary: null,
    }), { status: 200 }),
  ));
  render(wrap(<PaymentSection concertId="c1" />));
  await waitFor(() => expect(screen.getByText(/45,00/)).toBeInTheDocument());
  const link = screen.getByRole("link", { name: /paypal/i });
  expect(link).toHaveAttribute("href", "https://paypal.me/o/45");
});

test("inactive state shows the activate button", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify({ error: { code: "payment_not_active", message: "no" } }), { status: 404 }),
  ));
  render(wrap(<PaymentSection concertId="c1" />));
  await waitFor(() => expect(screen.getByText(/Tickets/i)).toBeInTheDocument());
});
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd frontend && npm run test -- --run PaymentSection
```
Expected: FAIL — cannot find `./PaymentSection`.

- [ ] **Step 3: Write minimal implementation**

```tsx
// frontend/src/components/PaymentSection.tsx
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  getPayments, activatePayments, setPaymentLink, deactivatePayments,
  addPaymentItem, setPaymentAmount, removePaymentItem,
  reportPayment, unreportPayment, confirmPayment, unconfirmPayment,
  type PaymentView, type ApiError,
} from "../api/client";

const eur = (cents: number) => (cents / 100).toLocaleString("de-DE", { minimumFractionDigits: 2, maximumFractionDigits: 2 }) + " €";

export default function PaymentSection({ concertId }: { concertId: string }) {
  const qc = useQueryClient();
  const key = ["payments", concertId];
  const invalidate = () => qc.invalidateQueries({ queryKey: key });
  const { data, error, isLoading } = useQuery<PaymentView, ApiError>({
    queryKey: key, queryFn: () => getPayments(concertId), retry: false,
  });
  const [amount, setAmount] = useState("45");
  const [link, setLink] = useState("");

  const activate = useMutation({
    mutationFn: () => activatePayments(concertId, Math.round(parseFloat(amount || "0") * 100), link || undefined),
    onSuccess: invalidate,
  });

  if (isLoading) return <section><h2>Bezahlung</h2><p>Lädt…</p></section>;

  // 404 → not active yet
  if (error && error.code === "payment_not_active") {
    return (
      <section>
        <h2>Bezahlung</h2>
        <form onSubmit={(e) => { e.preventDefault(); activate.mutate(); }}>
          <input aria-label="Standardbetrag (€)" value={amount} onChange={(e) => setAmount(e.target.value)} />
          <input aria-label="PayPal-Link (optional)" value={link} onChange={(e) => setLink(e.target.value)} placeholder="https://paypal.me/…" />
          <button type="submit">Ich kümmere mich um die Tickets</button>
        </form>
      </section>
    );
  }
  if (!data) return null;

  return (
    <section>
      <h2>Bezahlung</h2>
      {data.is_responsible
        ? <ResponsibleView concertId={concertId} data={data} onChange={invalidate} />
        : <MemberView concertId={concertId} data={data} onChange={invalidate} />}
    </section>
  );
}

function MemberView({ concertId, data, onChange }: { concertId: string; data: PaymentView; onChange: () => void }) {
  const item = data.items[0];
  const report = useMutation({ mutationFn: () => reportPayment(concertId, item!.id), onSuccess: onChange });
  const unreport = useMutation({ mutationFn: () => unreportPayment(concertId, item!.id), onSuccess: onChange });
  if (!item) return <p>Kein Posten für dich.</p>;
  return (
    <div>
      <p>Dein Anteil: <strong>{eur(item.amount_cents)}</strong> — Status: {item.status}</p>
      {item.status === "open" && <button onClick={() => report.mutate()}>bezahlt</button>}
      {item.status === "reported" && <button onClick={() => unreport.mutate()}>zurücknehmen</button>}
      {item.status === "confirmed" && <span>✅ bestätigt</span>}
      {data.payment_link && (
        <a href={data.payment_link} target="_blank" rel="noreferrer">Per PayPal zahlen</a>
      )}
    </div>
  );
}

function ResponsibleView({ concertId, data, onChange }: { concertId: string; data: PaymentView; onChange: () => void }) {
  const confirm = useMutation({ mutationFn: (id: string) => confirmPayment(concertId, id), onSuccess: onChange });
  const unconfirm = useMutation({ mutationFn: (id: string) => unconfirmPayment(concertId, id), onSuccess: onChange });
  const remove = useMutation({ mutationFn: (id: string) => removePaymentItem(concertId, id), onSuccess: onChange });
  const setAmt = useMutation({ mutationFn: (v: { id: string; cents: number }) => setPaymentAmount(concertId, v.id, v.cents), onSuccess: onChange });
  const deactivate = useMutation({ mutationFn: () => deactivatePayments(concertId), onSuccess: onChange });
  const [linkEdit, setLinkEdit] = useState(data.payment_link ?? "");
  const saveLink = useMutation({ mutationFn: () => setPaymentLink(concertId, linkEdit || null), onSuccess: onChange });

  return (
    <div>
      {data.summary && (
        <p>{eur(data.summary.outstanding_cents)} ausstehend · {data.summary.confirmed_count} bestätigt</p>
      )}
      <label>PayPal-Link:
        <input aria-label="PayPal-Link" value={linkEdit} onChange={(e) => setLinkEdit(e.target.value)} />
        <button onClick={() => saveLink.mutate()}>speichern</button>
      </label>
      <ul>
        {data.items.map((it) => (
          <li key={it.id}>
            {it.display_name}: {eur(it.amount_cents)} ({it.status})
            <input aria-label={`Betrag ${it.display_name}`} defaultValue={(it.amount_cents / 100).toString()}
              onBlur={(e) => setAmt.mutate({ id: it.id, cents: Math.round(parseFloat(e.target.value || "0") * 100) })} />
            {it.status !== "confirmed"
              ? <button onClick={() => confirm.mutate(it.id)}>bestätigen</button>
              : <button onClick={() => unconfirm.mutate(it.id)}>Bestätigung zurücknehmen</button>}
            <button onClick={() => remove.mutate(it.id)}>entfernen</button>
          </li>
        ))}
      </ul>
      <button onClick={() => deactivate.mutate()}>Sammeln beenden</button>
    </div>
  );
}
```
> The `AddItem` affordance (responsible adds a not-yet-listed member) is intentionally omitted from the minimal render to keep the component focused; the API + service support it. Add a member-picker in a follow-up if needed — note this in the task report so the reviewer knows it was a deliberate scope trim, not a miss. The `addPaymentItem` import may be unused as a result — drop it from the import list if the linter/build flags it.

- [ ] **Step 4: Mount in ConcertDetail**

In `frontend/src/routes/ConcertDetail.tsx`, import and render the section below the RSVP list:
```tsx
import PaymentSection from "../components/PaymentSection";
// …inside the returned JSX, after the "Wer kommt mit" list:
<PaymentSection concertId={id} />
```
(`id` is the concert id already read via `useParams` in ConcertDetail.)

- [ ] **Step 5: Run tests + build to verify it passes**

```bash
cd frontend && npm run test -- --run && npm run build
```
Expected: all tests PASS, `tsc -b` + vite build clean.

- [ ] **Step 6: Commit**

```bash
git add frontend && git commit -m "feat: payment section on concert detail (responsible/member/inactive views, paypal link)"
```

---

## Phase 4 — Deploy sync

### Task 4.1: Sync the new migration into the kustomize deploy dir

**Files:**
- Create: `deploy/base/migrations/0002_payments.up.sql`, `deploy/base/migrations/0002_payments.down.sql` (copies)
- Modify: `deploy/base/kustomization.yaml`

**Interfaces:**
- Produces: the K8s migrate Job applies migration 0002.

- [ ] **Step 1: Sync the migration copies**

```bash
make sync-migrations
```
(This copies `backend/internal/store/migrations/*.sql` into `deploy/base/migrations/`, per the Cycle 1 guardrail.)

- [ ] **Step 2: List the new files in the configMapGenerator**

In `deploy/base/kustomization.yaml`, add the two new files under `configMapGenerator.files`:
```yaml
      - migrations/0002_payments.up.sql
      - migrations/0002_payments.down.sql
```

- [ ] **Step 3: Verify the render**

```bash
kubectl kustomize deploy/overlays/dev > /dev/null && echo RENDER_OK
```
Expected: `RENDER_OK`.

- [ ] **Step 4: Commit**

```bash
git add deploy && git commit -m "chore: sync payment migration into kustomize deploy dir"
```

---

## Self-Review

**Spec coverage (each spec section → task):**
- §2 decisions (amounts, opt-in responsible, default+override, one-click report, two-stage+direct-confirm, reversibility, no RSVP auto-sync, yes-RSVP seeding, visibility C, **payment link**) → Tasks 1.1–1.3, 2.1; payment link → 0.1/0.2/1.1/1.2/2.1/3.1/3.2.
- §4 data model (two tables incl. `payment_link`) → Task 0.1.
- §5 state machine → Task 1.3.
- §6 authorization → Tasks 1.1 (`requireResponsible`), 1.2 (`loadItemAsResponsible`), 1.3 (owner/responsible checks).
- §7 API (every endpoint incl. PATCH link) → Tasks 2.1 (handlers), 2.2 (routes).
- §8 frontend (3 views + activate + pay link) → Tasks 3.1, 3.2.
- §9 test strategy (TDD, testcontainers, negative paths) → throughout; negative paths in 1.1–1.3.
- §10 build order + deploy sync → Phases 0→3 + Task 4.1.

**Resolved during planning:**
- Item-level routes live under `/api/concerts/{concertID}/payments/items/{itemID}` so membership is gated via the concert (no `RequireGroupMember` middleware needed — service gates).
- `payment_link` added end-to-end after the mid-plan requirement (collector's pay-me URL, visible to members).
- The four state-machine handlers delegate to a single typed `transition` helper (Task 2.1) — short and clearer than per-handler duplication.

**Known deliberate scope trims (note in task reports):** the responsible "add a not-yet-listed member" UI (member picker) is omitted from the minimal PaymentSection render though the API supports it; amount pre-fill on the PayPal link is not appended (link opened as-is).


