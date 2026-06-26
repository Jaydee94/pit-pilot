# pit-pilot Cycle 3 — Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Web Push notifications to pit-pilot for five triggers (new concert, deadline/concert-soon reminders, payment events, RSVP activity) via a transactional outbox + in-process worker, on the Cycle 1/2 stack.

**Architecture:** Domain events write rows into a `notifications` outbox table in the same transaction as the domain change (via an injected `notify.Enqueuer`); an in-process worker goroutine (started in each API replica) scans for time-based reminders under a Postgres advisory lock, then claims pending rows with `FOR UPDATE SKIP LOCKED` and sends VAPID Web Push to each of the user's `push_subscriptions` (deleting dead ones). Reminders carry an idempotency `dedup_key`. The PWA gains a custom service worker (push + click handlers) and a subscribe toggle.

**Tech Stack:** Go 1.26, chi v5, pgx v5 + sqlc, golang-migrate, `SherClockHolmes/webpush-go` (latest), testcontainers-go; React 19 + Vite + `vite-plugin-pwa` (injectManifest) + Vitest. All else already in the repo.

## Global Constraints

- Go module path: `github.com/jaydee94/pit-pilot/backend`; Go floor `1.26`.
- **Always use the latest stable version of every library/tool** (`@latest` for sqlc and `go get`); version numbers here are illustrative.
- Strict TDD: failing test first, watch it fail, implement minimally, watch it pass, commit. No implementation code without a failing test first. Docker is available — testcontainers DB tests must genuinely pass.
- Push only; all-or-nothing (no per-type preferences); Web Push via VAPID. UUID PKs.
- Notification `type` ∈ `'concert_new','deadline_soon','concert_soon','payment','rsvp_changed'`. Notification `status` ∈ `'pending','sent','failed'`.
- `dedup_key` is set (deterministic) ONLY for reminders; event-driven rows leave it NULL (NULLs are distinct, so no false dedup). Reminder timing is fixed at 24h.
- **Optional-dependency wiring (avoid breaking existing tests):** existing service constructors (`NewConcertService`, `NewRSVPService`, `NewPaymentService`) are NOT changed. Each service gains an unexported `enqueuer notify.Enqueuer` field set via a `SetEnqueuer(notify.Enqueuer)` method; when nil, enqueue is skipped. `main.go` calls `SetEnqueuer`; notification tests set a `notify.FakeEnqueuer`.
- API errors use JSON `{"error":{"code","message"}}`. Push/subscription routes live under `/api` (session-gated). Layer discipline: handlers never touch SQL; services never import `net/http`.
- Every task ends on a green test run and a commit. After the backend lands, sync the new migration into `deploy/base/migrations/` (`make sync-migrations`) and list it in the kustomize `configMapGenerator`.

## Existing patterns to follow (read before starting)

- Migrations `backend/internal/store/migrations/000{1,2}_*.sql`; `backend/sqlc.yaml` (overrides: uuid→google/uuid, timestamptz→time.Time non-null, timestamptz nullable→*time.Time, `emit_pointers_for_null_types` so nullable cols are pointers). Generated store: `internal/store/gen` (`gen.New(db)`, `(*Queries).WithTx(tx)`).
- Services: `service/groups.go` (transaction pattern via `pool.Begin`+`WithTx`+`Commit`, `RequireMembership`), `service/concerts.go` (`ConcertService.Get` gates membership), `service/payments.go` (transactional outbox-style aggregate). Typed errors via `apperr`.
- Handlers: `httpapi/{render,middleware,payment_handlers}.go` (`WriteJSON`, `WriteError`, `UserID(r)`), test helper `WithUserIDForTest` (export_test.go). Router `httpapi/router.go` (`Deps` + `NewRouter`). Entry `cmd/api/main.go` (config → pgxpool → gen → services → Deps → http.Server). Config `internal/config/config.go`.
- Test harness `testutil.NewPostgres(t)`. Frontend `frontend/src/api/client.ts`, `vite.config.ts` (currently `VitePWA({registerType:"autoUpdate"})`), TanStack Query.

## File Structure

```
backend/
  internal/store/migrations/0003_notifications.up.sql / .down.sql
  internal/store/queries/push_subscriptions.sql
  internal/store/queries/notifications.sql
  internal/store/gen/                                   # regenerated
  internal/notify/notify.go                             # Notification type, Enqueuer iface, OutboxEnqueuer, FakeEnqueuer
  internal/push/push.go                                 # Subscription, Payload, Pusher iface, FakePusher
  internal/push/webpush.go                              # WebPusher (webpush-go impl)
  internal/service/subscriptions.go                     # SubscriptionService (save/delete/list)
  internal/service/reminders.go                         # reminder scan (ScanDue)
  internal/service/concerts.go  rsvps.go  payments.go   # MODIFY: SetEnqueuer + enqueue hooks
  internal/worker/worker.go                             # Tick (scan→send→prune) + Run loop
  internal/httpapi/push_handlers.go                     # subscribe/unsubscribe/vapid-key
  internal/httpapi/router.go  + cmd/api/main.go         # MODIFY: routes + wiring + start worker
  internal/config/config.go                             # MODIFY: VAPID keys
frontend/
  src/api/client.ts                                     # MODIFY: push helpers
  src/sw.ts                                             # custom service worker (push + click)
  vite.config.ts                                        # MODIFY: injectManifest
  src/components/PushToggle.tsx                         # subscribe toggle
  src/routes/Groups.tsx                                 # MODIFY: mount PushToggle
deploy/base/migrations/                                 # sync 0003 (final task)
deploy/overlays/dev/secret.example.env                 # MODIFY: VAPID placeholders
```

---

## Phase 0 — Schema & Store

### Task 0.1: Migration for notifications tables

**Files:**
- Create: `backend/internal/store/migrations/0003_notifications.up.sql`
- Create: `backend/internal/store/migrations/0003_notifications.down.sql`

**Interfaces:**
- Produces: `push_subscriptions` and `notifications` tables consumed by Task 0.2.

- [ ] **Step 1: Write the up migration**

```sql
-- backend/internal/store/migrations/0003_notifications.up.sql
CREATE TABLE push_subscriptions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint   text NOT NULL UNIQUE,
    p256dh     text NOT NULL,
    auth       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_push_subscriptions_user ON push_subscriptions (user_id);

CREATE TABLE notifications (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type       text NOT NULL,
    title      text NOT NULL,
    body       text NOT NULL,
    url        text,
    dedup_key  text UNIQUE,
    status     text NOT NULL DEFAULT 'pending',
    attempts   int  NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    sent_at    timestamptz,
    CHECK (type IN ('concert_new','deadline_soon','concert_soon','payment','rsvp_changed')),
    CHECK (status IN ('pending','sent','failed'))
);
CREATE INDEX idx_notifications_pending ON notifications (status) WHERE status = 'pending';
```

- [ ] **Step 2: Write the down migration**

```sql
-- backend/internal/store/migrations/0003_notifications.down.sql
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS push_subscriptions;
```

- [ ] **Step 3: Verify the harness applies all migrations**

```bash
cd backend && go test ./internal/testutil/... -run TestNewPostgres
```
Expected: PASS (applies 0001 + 0002 + 0003).

- [ ] **Step 4: Commit**

```bash
git add backend/internal/store/migrations && git commit -m "feat: notifications + push_subscriptions migration"
```

---

### Task 0.2: sqlc queries for subscriptions & notifications

**Files:**
- Create: `backend/internal/store/queries/push_subscriptions.sql`
- Create: `backend/internal/store/queries/notifications.sql`
- Generate: regenerate `backend/internal/store/gen/`
- Test: `backend/internal/store/gen/notifications_gen_test.go`

**Interfaces:**
- Produces (generated, consumed later):
  - `UpsertPushSubscription(ctx, UpsertPushSubscriptionParams{UserID uuid.UUID; Endpoint, P256dh, Auth string}) (PushSubscription, error)`
  - `DeletePushSubscription(ctx, DeletePushSubscriptionParams{UserID uuid.UUID; Endpoint string}) error`
  - `DeletePushSubscriptionByID(ctx, uuid.UUID) error`
  - `ListPushSubscriptionsForUser(ctx, uuid.UUID) ([]PushSubscription, error)`
  - `InsertNotification(ctx, InsertNotificationParams{UserID uuid.UUID; Type, Title, Body string; Url, DedupKey *string}) error`
  - `ClaimPendingNotifications(ctx, int32) ([]Notification, error)` (FOR UPDATE SKIP LOCKED; called inside a tx)
  - `MarkNotificationSent(ctx, uuid.UUID) error`; `MarkNotificationFailed(ctx, uuid.UUID) error`
  - `ScanDeadlineReminders(ctx) error`; `ScanConcertReminders(ctx) error`
  - `PruneOldNotifications(ctx) error`

- [ ] **Step 1: Write `push_subscriptions.sql`** (no leading path comment)

```sql
-- name: UpsertPushSubscription :one
INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth)
VALUES ($1, $2, $3, $4)
ON CONFLICT (endpoint) DO UPDATE
    SET user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth
RETURNING *;

-- name: DeletePushSubscription :exec
DELETE FROM push_subscriptions WHERE user_id = $1 AND endpoint = $2;

-- name: DeletePushSubscriptionByID :exec
DELETE FROM push_subscriptions WHERE id = $1;

-- name: ListPushSubscriptionsForUser :many
SELECT * FROM push_subscriptions WHERE user_id = $1;
```

- [ ] **Step 2: Write `notifications.sql`**

```sql
-- name: InsertNotification :exec
INSERT INTO notifications (user_id, type, title, body, url, dedup_key)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (dedup_key) DO NOTHING;

-- name: ClaimPendingNotifications :many
SELECT * FROM notifications
WHERE status = 'pending'
ORDER BY created_at
FOR UPDATE SKIP LOCKED
LIMIT $1;

-- name: MarkNotificationSent :exec
UPDATE notifications SET status = 'sent', sent_at = now(), attempts = attempts + 1 WHERE id = $1;

-- name: MarkNotificationFailed :exec
UPDATE notifications SET status = 'failed', attempts = attempts + 1 WHERE id = $1;

-- name: ScanDeadlineReminders :exec
INSERT INTO notifications (user_id, type, title, body, url, dedup_key)
SELECT gm.user_id, 'deadline_soon',
       'Anmeldeschluss bald: ' || c.artist,
       'Der Anmeldeschluss für ' || c.artist || ' ist bald. Kommst du mit?',
       '/concerts/' || c.id::text,
       'deadline_soon:' || c.id::text || ':' || gm.user_id::text
FROM concerts c
JOIN group_members gm ON gm.group_id = c.group_id
LEFT JOIN rsvps r ON r.concert_id = c.id AND r.user_id = gm.user_id
WHERE c.rsvp_deadline > now() AND c.rsvp_deadline <= now() + interval '24 hours'
  AND r.user_id IS NULL
ON CONFLICT (dedup_key) DO NOTHING;

-- name: ScanConcertReminders :exec
INSERT INTO notifications (user_id, type, title, body, url, dedup_key)
SELECT r.user_id, 'concert_soon',
       c.artist || ' ist bald!',
       c.artist || ' steht kurz bevor. Bis dann!',
       '/concerts/' || c.id::text,
       'concert_soon:' || c.id::text || ':' || r.user_id::text
FROM concerts c
JOIN rsvps r ON r.concert_id = c.id AND r.status = 'yes'
WHERE c.event_at > now() AND c.event_at <= now() + interval '24 hours'
ON CONFLICT (dedup_key) DO NOTHING;

-- name: PruneOldNotifications :exec
DELETE FROM notifications
WHERE status IN ('sent','failed') AND created_at < now() - interval '30 days';
```

- [ ] **Step 3: Regenerate**

```bash
cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
```

- [ ] **Step 4: Write the failing test**

```go
// backend/internal/store/gen/notifications_gen_test.go
package gen_test

import (
	"context"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestInsertAndClaimNotification(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	u, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "n", DisplayName: "N"})

	if err := q.InsertNotification(ctx, gen.InsertNotificationParams{
		UserID: u.ID, Type: "concert_new", Title: "T", Body: "B"}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rows, err := q.ClaimPendingNotifications(ctx, 10)
	if err != nil || len(rows) != 1 || rows[0].Status != "pending" {
		t.Fatalf("claim: %v err=%v", rows, err)
	}
	if err := q.MarkNotificationSent(ctx, rows[0].ID); err != nil {
		t.Fatalf("mark sent: %v", err)
	}
}

func TestUpsertPushSubscriptionIdempotentOnEndpoint(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	u, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "n", DisplayName: "N"})
	p := gen.UpsertPushSubscriptionParams{UserID: u.ID, Endpoint: "https://push/x", P256dh: "k", Auth: "a"}
	s1, _ := q.UpsertPushSubscription(ctx, p)
	p.Auth = "a2"
	s2, err := q.UpsertPushSubscription(ctx, p)
	if err != nil || s1.ID != s2.ID || s2.Auth != "a2" {
		t.Fatalf("upsert not idempotent: %v %v err=%v", s1, s2, err)
	}
}
```

- [ ] **Step 5: Run test to verify it passes** (FAIL before generation, PASS after)

```bash
cd backend && go test ./internal/store/gen/ -run 'Notification|PushSubscription' && go test ./...
```
Expected: PASS, full module green.

- [ ] **Step 6: Commit**

```bash
git add backend && git commit -m "feat: sqlc queries for notifications and push subscriptions"
```

## Phase 1 — Config, Subscription service & Push API

### Task 1.1: VAPID config

**Files:**
- Modify: `backend/internal/config/config.go`
- Test: `backend/internal/config/config_test.go` (add a test)

**Interfaces:**
- Produces: `Config` gains `VapidPublicKey, VapidPrivateKey, VapidSubject string` (read from `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY`, `VAPID_SUBJECT`; optional — empty is allowed so the app boots without push configured).

- [ ] **Step 1: Write the failing test** (append to `config_test.go`)

```go
func TestLoadReadsVapid(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL":        "postgres://localhost/pp",
		"SESSION_SIGNING_KEY": "k",
		"VAPID_PUBLIC_KEY":    "pub",
		"VAPID_PRIVATE_KEY":   "priv",
		"VAPID_SUBJECT":       "mailto:a@x.io",
	}
	cfg, err := Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.VapidPublicKey != "pub" || cfg.VapidPrivateKey != "priv" || cfg.VapidSubject != "mailto:a@x.io" {
		t.Fatalf("vapid not parsed: %+v", cfg)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/config/ -run Vapid
```
Expected: FAIL — `cfg.VapidPublicKey undefined`.

- [ ] **Step 3: Add the fields** to the `Config` struct and to `Load`'s construction:

```go
// in the Config struct, alongside the existing fields:
	VapidPublicKey  string
	VapidPrivateKey string
	VapidSubject    string
```
```go
// in Load, alongside the existing getenv assignments:
		VapidPublicKey:  getenv("VAPID_PUBLIC_KEY"),
		VapidPrivateKey: getenv("VAPID_PRIVATE_KEY"),
		VapidSubject:    getenv("VAPID_SUBJECT"),
```
(No required-check — these are optional.)

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/config/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config && git commit -m "feat: VAPID config fields"
```

---

### Task 1.2: Subscription service

**Files:**
- Create: `backend/internal/service/subscriptions.go`
- Test: `backend/internal/service/subscriptions_test.go`

**Interfaces:**
- Consumes: `*gen.Queries` (Upsert/Delete/List push-subscription queries).
- Produces:
  - `type SubscriptionService struct { ... }`
  - `func NewSubscriptionService(q *gen.Queries) *SubscriptionService`
  - `func (s *SubscriptionService) Save(ctx, userID uuid.UUID, endpoint, p256dh, auth string) (gen.PushSubscription, error)` — validates non-empty fields (else `apperr.BadRequest`), upserts.
  - `func (s *SubscriptionService) Delete(ctx, userID uuid.UUID, endpoint string) error` — scoped to the user.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/service/subscriptions_test.go
package service_test

import (
	"context"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestSaveAndDeleteSubscription(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	svc := service.NewSubscriptionService(q)
	ctx := context.Background()
	u := seedUser(t, q, "u")

	sub, err := svc.Save(ctx, u.ID, "https://push/x", "k", "a")
	if err != nil || sub.UserID != u.ID {
		t.Fatalf("save: %v err=%v", sub, err)
	}
	if err := svc.Delete(ctx, u.ID, "https://push/x"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestSaveRejectsEmptyEndpoint(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	svc := service.NewSubscriptionService(q)
	u := seedUser(t, q, "u")
	_, err := svc.Save(context.Background(), u.ID, "", "k", "a")
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 400 {
		t.Fatalf("expected 400, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/service/ -run Subscription
```
Expected: FAIL — `undefined: service.NewSubscriptionService`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/service/subscriptions.go
package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type SubscriptionService struct {
	q *gen.Queries
}

func NewSubscriptionService(q *gen.Queries) *SubscriptionService {
	return &SubscriptionService{q: q}
}

func (s *SubscriptionService) Save(ctx context.Context, userID uuid.UUID, endpoint, p256dh, auth string) (gen.PushSubscription, error) {
	if endpoint == "" || p256dh == "" || auth == "" {
		return gen.PushSubscription{}, apperr.BadRequest("invalid_subscription", "endpoint, p256dh and auth are required")
	}
	return s.q.UpsertPushSubscription(ctx, gen.UpsertPushSubscriptionParams{
		UserID: userID, Endpoint: endpoint, P256dh: p256dh, Auth: auth})
}

func (s *SubscriptionService) Delete(ctx context.Context, userID uuid.UUID, endpoint string) error {
	return s.q.DeletePushSubscription(ctx, gen.DeletePushSubscriptionParams{UserID: userID, Endpoint: endpoint})
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/service/ -run Subscription
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service && git commit -m "feat: push subscription service"
```

---

### Task 1.3: Push HTTP handlers + routes + wiring

**Files:**
- Create: `backend/internal/httpapi/push_handlers.go`
- Modify: `backend/internal/httpapi/router.go` (Deps + routes)
- Modify: `backend/cmd/api/main.go` (construct + wire)
- Test: `backend/internal/httpapi/push_handlers_test.go`

**Interfaces:**
- Consumes: `service.SubscriptionService`, `UserID`, `WriteJSON`/`WriteError`, `apperr`, `WithUserIDForTest`.
- Produces:
  - `type PushHandlers struct { Subs *service.SubscriptionService; VapidPublicKey string }`
  - `VapidKey` (GET → `{"public_key": ...}`), `Subscribe` (POST body `{endpoint, keys:{p256dh, auth}}` → 201), `Unsubscribe` (DELETE body `{endpoint}` → 204).
  - `Deps` gains `Push *PushHandlers`; routes `GET /api/push/vapid-public-key`, `POST /api/push/subscriptions`, `DELETE /api/push/subscriptions`.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/httpapi/push_handlers_test.go
package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/httpapi"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestSubscribeAndVapidKey(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	h := &httpapi.PushHandlers{Subs: service.NewSubscriptionService(q), VapidPublicKey: "PUBKEY"}

	keyRec := httptest.NewRecorder()
	h.VapidKey(keyRec, httptest.NewRequest(http.MethodGet, "/api/push/vapid-public-key", nil))
	if keyRec.Code != http.StatusOK || !strings.Contains(keyRec.Body.String(), "PUBKEY") {
		t.Fatalf("vapid key: %d %s", keyRec.Code, keyRec.Body.String())
	}

	u, _ := q.UpsertUser(httptest.NewRequest(http.MethodGet, "/", nil).Context(),
		gen.UpsertUserParams{Provider: "google", ProviderSub: "u", DisplayName: "U"})
	body := `{"endpoint":"https://push/x","keys":{"p256dh":"k","auth":"a"}}`
	req := httpapi.WithUserIDForTest(httptest.NewRequest(http.MethodPost, "/api/push/subscriptions", strings.NewReader(body)), u.ID)
	rec := httptest.NewRecorder()
	h.Subscribe(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("subscribe: %d %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/httpapi/ -run SubscribeAndVapid
```
Expected: FAIL — `undefined: httpapi.PushHandlers`.

- [ ] **Step 3: Write the handlers**

```go
// backend/internal/httpapi/push_handlers.go
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
)

type PushHandlers struct {
	Subs           *service.SubscriptionService
	VapidPublicKey string
}

func (h *PushHandlers) VapidKey(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"public_key": h.VapidPublicKey})
}

func (h *PushHandlers) Subscribe(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	var b struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "malformed json"))
		return
	}
	sub, err := h.Subs.Save(r.Context(), uid, b.Endpoint, b.Keys.P256dh, b.Keys.Auth)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{"id": sub.ID})
}

func (h *PushHandlers) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	var b struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "malformed json"))
		return
	}
	if err := h.Subs.Delete(r.Context(), uid, b.Endpoint); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Wire routes + main**

In `router.go`, add `Push *PushHandlers` to `Deps`, and inside the `/api` group (after the payments block):
```go
		if d.Push != nil {
			api.Get("/push/vapid-public-key", d.Push.VapidKey)
			api.Post("/push/subscriptions", d.Push.Subscribe)
			api.Delete("/push/subscriptions", d.Push.Unsubscribe)
		}
```
In `cmd/api/main.go`, after the services are built, add:
```go
	subs := service.NewSubscriptionService(q)
```
and add to the `Deps{...}` literal:
```go
		Push: &httpapi.PushHandlers{Subs: subs, VapidPublicKey: cfg.VapidPublicKey},
```

- [ ] **Step 5: Run test + build to verify**

```bash
cd backend && go test ./internal/httpapi/ -run SubscribeAndVapid && go build ./cmd/api && go test ./...
```
Expected: PASS, builds, full module green.

- [ ] **Step 6: Commit**

```bash
git add backend && git commit -m "feat: push subscription handlers, routes, wiring"
```

---

## Phase 2 — notify package (outbox enqueuer)

### Task 2.1: Notification type + Enqueuer + OutboxEnqueuer + FakeEnqueuer

**Files:**
- Create: `backend/internal/notify/notify.go`
- Test: `backend/internal/notify/notify_test.go`

**Interfaces:**
- Consumes: `*gen.Queries` (`InsertNotification`).
- Produces:
  - `type Notification struct { UserID uuid.UUID; Type, Title, Body, URL string; DedupKey *string }`
  - `type Enqueuer interface { Enqueue(ctx context.Context, q *gen.Queries, rows []Notification) error }`
  - `type OutboxEnqueuer struct{}` implementing Enqueuer (inserts each row via `q.InsertNotification`, mapping empty URL → nil).
  - `type FakeEnqueuer struct { Rows []Notification }` implementing Enqueuer (records rows; ignores q).

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/notify/notify_test.go
package notify_test

import (
	"context"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/notify"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestOutboxEnqueuerInserts(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	u, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "u", DisplayName: "U"})

	var e notify.Enqueuer = notify.OutboxEnqueuer{}
	url := "/concerts/1"
	if err := e.Enqueue(ctx, q, []notify.Notification{
		{UserID: u.ID, Type: "concert_new", Title: "T", Body: "B", URL: url},
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	rows, _ := q.ClaimPendingNotifications(ctx, 10)
	if len(rows) != 1 || rows[0].Url == nil || *rows[0].Url != "/concerts/1" {
		t.Fatalf("expected 1 row with url, got %v", rows)
	}
}

func TestFakeEnqueuerRecords(t *testing.T) {
	f := &notify.FakeEnqueuer{}
	_ = f.Enqueue(context.Background(), nil, []notify.Notification{{Type: "payment"}})
	if len(f.Rows) != 1 || f.Rows[0].Type != "payment" {
		t.Fatalf("fake did not record: %v", f.Rows)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/notify/...
```
Expected: FAIL — `undefined: notify.OutboxEnqueuer`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/notify/notify.go
package notify

import (
	"context"

	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type Notification struct {
	UserID   uuid.UUID
	Type     string
	Title    string
	Body     string
	URL      string
	DedupKey *string
}

type Enqueuer interface {
	Enqueue(ctx context.Context, q *gen.Queries, rows []Notification) error
}

type OutboxEnqueuer struct{}

func (OutboxEnqueuer) Enqueue(ctx context.Context, q *gen.Queries, rows []Notification) error {
	for _, n := range rows {
		var url *string
		if n.URL != "" {
			u := n.URL
			url = &u
		}
		if err := q.InsertNotification(ctx, gen.InsertNotificationParams{
			UserID: n.UserID, Type: n.Type, Title: n.Title, Body: n.Body, Url: url, DedupKey: n.DedupKey,
		}); err != nil {
			return err
		}
	}
	return nil
}

type FakeEnqueuer struct {
	Rows []Notification
}

func (f *FakeEnqueuer) Enqueue(_ context.Context, _ *gen.Queries, rows []Notification) error {
	f.Rows = append(f.Rows, rows...)
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/notify/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/notify && git commit -m "feat: notify package (outbox enqueuer + fake)"
```

---

## Phase 3 — push package (sender)

### Task 3.1: Pusher interface + FakePusher + WebPusher

**Files:**
- Create: `backend/internal/push/push.go`
- Create: `backend/internal/push/webpush.go`
- Test: `backend/internal/push/push_test.go`

**Interfaces:**
- Produces:
  - `type Subscription struct { Endpoint, P256dh, Auth string }`
  - `type Payload struct { Title, Body, URL string }`
  - `type Pusher interface { Send(ctx context.Context, sub Subscription, p Payload) (dead bool, err error) }` (`dead==true` ⇒ the subscription is gone (404/410) and should be deleted)
  - `type FakePusher struct { Dead bool; Err error; Sent []SentRecord }` (records calls; returns configured Dead/Err)
  - `type SentRecord struct { Sub Subscription; Payload Payload }`
  - `func NewWebPusher(publicKey, privateKey, subject string) *WebPusher` implementing Pusher via `webpush-go`.

- [ ] **Step 1: Add dependency**

```bash
cd backend && go get github.com/SherClockHolmes/webpush-go@latest
```

- [ ] **Step 2: Write the failing test**

```go
// backend/internal/push/push_test.go
package push_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/push"
)

func TestFakePusherRecordsAndReportsDead(t *testing.T) {
	var p push.Pusher = &push.FakePusher{Dead: true}
	dead, err := p.Send(context.Background(), push.Subscription{Endpoint: "e"}, push.Payload{Title: "T"})
	if err != nil || !dead {
		t.Fatalf("expected dead=true err=nil, got dead=%v err=%v", dead, err)
	}
}

func TestFakePusherReturnsErr(t *testing.T) {
	f := &push.FakePusher{Err: errors.New("boom")}
	if _, err := f.Send(context.Background(), push.Subscription{}, push.Payload{}); err == nil {
		t.Fatal("expected error")
	}
	if len(f.Sent) != 1 {
		t.Fatalf("expected 1 recorded send, got %d", len(f.Sent))
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd backend && go test ./internal/push/...
```
Expected: FAIL — `undefined: push.FakePusher`.

- [ ] **Step 4: Write the implementation**

```go
// backend/internal/push/push.go
package push

import "context"

type Subscription struct {
	Endpoint string
	P256dh   string
	Auth     string
}

type Payload struct {
	Title string
	Body  string
	URL   string
}

type Pusher interface {
	Send(ctx context.Context, sub Subscription, p Payload) (dead bool, err error)
}

type SentRecord struct {
	Sub     Subscription
	Payload Payload
}

type FakePusher struct {
	Dead bool
	Err  error
	Sent []SentRecord
}

func (f *FakePusher) Send(_ context.Context, sub Subscription, p Payload) (bool, error) {
	f.Sent = append(f.Sent, SentRecord{Sub: sub, Payload: p})
	return f.Dead, f.Err
}
```

```go
// backend/internal/push/webpush.go
package push

import (
	"context"
	"encoding/json"
	"fmt"

	webpush "github.com/SherClockHolmes/webpush-go"
)

type WebPusher struct {
	publicKey  string
	privateKey string
	subject    string
}

func NewWebPusher(publicKey, privateKey, subject string) *WebPusher {
	return &WebPusher{publicKey: publicKey, privateKey: privateKey, subject: subject}
}

func (w *WebPusher) Send(ctx context.Context, sub Subscription, p Payload) (bool, error) {
	body, err := json.Marshal(map[string]string{"title": p.Title, "body": p.Body, "url": p.URL})
	if err != nil {
		return false, err
	}
	resp, err := webpush.SendNotificationWithContext(ctx, body, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, &webpush.Options{
		Subscriber:      w.subject,
		VAPIDPublicKey:  w.publicKey,
		VAPIDPrivateKey: w.privateKey,
		TTL:             30,
	})
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 || resp.StatusCode == 410 {
		return true, nil
	}
	if resp.StatusCode >= 400 {
		return false, fmt.Errorf("push failed: status %d", resp.StatusCode)
	}
	return false, nil
}
```
> If the latest `webpush-go` renamed `SendNotificationWithContext`/`Options` fields, adapt to the current API (same behavior: send the JSON body with VAPID options; treat 404/410 as dead). Note any adaptation in the report. `WebPusher` is not unit-tested (it makes real network calls); it is exercised indirectly via the worker tests' `FakePusher`.

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/push/... && go build ./...
```
Expected: PASS, builds.

- [ ] **Step 6: Commit**

```bash
git add backend && git commit -m "feat: push sender (Pusher iface, FakePusher, webpush-go impl)"
```

## Phase 4 — Event hooks (transactional enqueue)

Each task adds a `SetEnqueuer` setter and an enqueue path to an existing service. When the enqueuer is nil (the default, as in all existing tests), behavior is unchanged. When set, the domain write + enqueue run in ONE transaction. Tests construct the service normally, call `SetEnqueuer` with a `notify.FakeEnqueuer`, perform the action, and assert the recorded rows.

### Task 4.1: concert_new on ConcertService.Create

**Files:**
- Modify: `backend/internal/service/concerts.go`
- Test: `backend/internal/service/concerts_test.go` (add a test)

**Interfaces:**
- Consumes: `notify.Enqueuer`, `*pgxpool.Pool`, `gen.Queries.WithTx`, `gen.Queries.ListGroupMembers`.
- Produces: `func (s *ConcertService) SetEnqueuer(e notify.Enqueuer, pool *pgxpool.Pool)`; `Create` enqueues one `concert_new` row per group member except the creator when an enqueuer is set.

- [ ] **Step 1: Write the failing test**

```go
// append to backend/internal/service/concerts_test.go
func TestCreateConcertEnqueuesConcertNew(t *testing.T) {
	cs, gs, q := newConcertSetup(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	friend := seedUser(t, q, "friend")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)

	fake := &notify.FakeEnqueuer{}
	cs.SetEnqueuer(fake, testPool(t)) // testPool returns the same pool the setup used

	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	if _, err := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(fake.Rows) != 1 || fake.Rows[0].UserID != friend.ID || fake.Rows[0].Type != "concert_new" {
		t.Fatalf("expected 1 concert_new to friend, got %+v", fake.Rows)
	}
}
```
> Helper: `newConcertSetup` currently returns `(cs, gs, q)` built on a fresh pool; it does NOT expose the pool. Change `newConcertSetup` to also return the pool (`func newConcertSetup(t) (*service.ConcertService, *service.GroupService, *gen.Queries, *pgxpool.Pool)`) and update its existing callers to take the extra return (use `_` where unused), OR add a parallel helper. Pass that pool to `SetEnqueuer`. (Pick whichever keeps the existing concert tests compiling; the simplest is to widen the return and patch the 2–3 call sites with a trailing `_`.) Import `github.com/jackc/pgx/v5/pgxpool` and `notify`.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/service/ -run CreateConcertEnqueues
```
Expected: FAIL — `cs.SetEnqueuer undefined`.

- [ ] **Step 3: Write minimal implementation**

Add fields + setter and the enqueue path to `concerts.go`:
```go
// add to the ConcertService struct:
	enqueuer notify.Enqueuer
	pool     *pgxpool.Pool

// add the setter:
func (s *ConcertService) SetEnqueuer(e notify.Enqueuer, pool *pgxpool.Pool) {
	s.enqueuer = e
	s.pool = pool
}
```
Refactor `Create` so that, after membership + validation, it branches:
```go
	params := gen.CreateConcertParams{
		GroupID: groupID, Artist: in.Artist, EventAt: in.EventAt,
		Venue: strPtr(in.Venue), City: strPtr(in.City), TicketUrl: strPtr(in.TicketURL),
		PriceCents: in.PriceCents, Notes: strPtr(in.Notes),
		RsvpDeadline: in.RSVPDeadline, CreatedBy: userID,
	}
	if s.enqueuer == nil {
		return s.q.CreateConcert(ctx, params)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return gen.Concert{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := s.q.WithTx(tx)
	c, err := qtx.CreateConcert(ctx, params)
	if err != nil {
		return gen.Concert{}, err
	}
	members, err := qtx.ListGroupMembers(ctx, groupID)
	if err != nil {
		return gen.Concert{}, err
	}
	var rows []notify.Notification
	for _, m := range members {
		if m.ID == userID {
			continue
		}
		rows = append(rows, notify.Notification{
			UserID: m.ID, Type: "concert_new",
			Title:  "Neues Konzert: " + c.Artist,
			Body:   c.Artist + " wurde in deiner Gruppe eingeplant.",
			URL:    "/concerts/" + c.ID.String(),
		})
	}
	if err := s.enqueuer.Enqueue(ctx, qtx, rows); err != nil {
		return gen.Concert{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return gen.Concert{}, err
	}
	return c, nil
```
(Keep the existing membership check + field validation above this block unchanged; they already build `in`/validate. Move the `CreateConcert` call into the branch as shown. Import `notify` and `pgxpool`.)

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/service/ -run Concert && go test ./...
```
Expected: PASS, full module green.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service && git commit -m "feat: enqueue concert_new on concert create"
```

---

### Task 4.2: rsvp_changed on RSVPService.Set

**Files:**
- Modify: `backend/internal/service/rsvps.go`
- Test: `backend/internal/service/rsvps_test.go` (add a test)

**Interfaces:**
- Consumes: `notify.Enqueuer`, `*pgxpool.Pool`, `gen.Queries.WithTx/UpsertRSVP/ListRSVPsForConcert`, `ConcertService.Get`.
- Produces: `func (s *RSVPService) SetEnqueuer(e notify.Enqueuer, pool *pgxpool.Pool)`; `Set` enqueues `rsvp_changed` to the other `yes`-responders when the caller sets `yes` and an enqueuer is set.

- [ ] **Step 1: Write the failing test**

```go
// append to backend/internal/service/rsvps_test.go
func TestSetYesEnqueuesRsvpChanged(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "RX" + string(rune('A'+n)) })
	cs := service.NewConcertService(q, gs)
	rs := service.NewRSVPService(q, cs)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	friend := seedUser(t, q, "friend")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})
	_, _ = rs.Set(ctx, owner.ID, c.ID, "yes") // owner already in

	fake := &notify.FakeEnqueuer{}
	rs.SetEnqueuer(fake, pool)
	if _, err := rs.Set(ctx, friend.ID, c.ID, "yes"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if len(fake.Rows) != 1 || fake.Rows[0].UserID != owner.ID || fake.Rows[0].Type != "rsvp_changed" {
		t.Fatalf("expected 1 rsvp_changed to owner, got %+v", fake.Rows)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/service/ -run SetYesEnqueues
```
Expected: FAIL — `rs.SetEnqueuer undefined`.

- [ ] **Step 3: Write minimal implementation**

Add to `rsvps.go`:
```go
// add to RSVPService struct:
	enqueuer notify.Enqueuer
	pool     *pgxpool.Pool

func (s *RSVPService) SetEnqueuer(e notify.Enqueuer, pool *pgxpool.Pool) {
	s.enqueuer = e
	s.pool = pool
}
```
In `Set`, after the status validation and the `c, err := s.concerts.Get(ctx, userID, concertID)` gate (use `Get` so you have the concert for the message; it already gates membership), branch:
```go
	if s.enqueuer == nil {
		return s.q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: concertID, UserID: userID, Status: status})
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return gen.Rsvp{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := s.q.WithTx(tx)
	rsvp, err := qtx.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: concertID, UserID: userID, Status: status})
	if err != nil {
		return gen.Rsvp{}, err
	}
	if status == "yes" {
		others, err := qtx.ListRSVPsForConcert(ctx, concertID)
		if err != nil {
			return gen.Rsvp{}, err
		}
		var rows []notify.Notification
		for _, o := range others {
			if o.ID == userID || o.Status != "yes" {
				continue
			}
			rows = append(rows, notify.Notification{
				UserID: o.ID, Type: "rsvp_changed",
				Title:  "Neue Zusage: " + c.Artist,
				Body:   "Jemand kommt auch zu " + c.Artist + " mit.",
				URL:    "/concerts/" + concertID.String(),
			})
		}
		if err := s.enqueuer.Enqueue(ctx, qtx, rows); err != nil {
			return gen.Rsvp{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return gen.Rsvp{}, err
	}
	return rsvp, nil
```
> If `Set` currently calls `concerts.Get` only for gating and discards the concert, change it to keep `c` (the concert) for the message. The non-enqueuer path stays as the plain `UpsertRSVP`. Import `notify` and `pgxpool`.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/service/ -run RSVP && go test ./...
```
Expected: PASS, full module green.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service && git commit -m "feat: enqueue rsvp_changed on yes RSVP"
```

---

### Task 4.3: payment notifications on PaymentService

**Files:**
- Modify: `backend/internal/service/payments.go`
- Test: `backend/internal/service/payments_test.go` (add a test)

**Interfaces:**
- Consumes: `notify.Enqueuer` (PaymentService already has `pool` + `q`).
- Produces: `func (s *PaymentService) SetEnqueuer(e notify.Enqueuer)`; a `payment` ("bitte bezahlen") row is enqueued when an item is created (Activate seeding + AddItem); a `payment` ("Eingang bestätigt") row when `Confirm` succeeds — all when an enqueuer is set.

- [ ] **Step 1: Write the failing test**

```go
// append to backend/internal/service/payments_test.go
func TestPaymentEnqueuesOnConfirm(t *testing.T) {
	ps, _, c, owner, friend, item := paySetupWithItem(t)
	ctx := context.Background()
	fake := &notify.FakeEnqueuer{}
	ps.SetEnqueuer(fake)
	if _, err := ps.Confirm(ctx, owner.ID, c.ID, item.ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if len(fake.Rows) != 1 || fake.Rows[0].UserID != friend.ID || fake.Rows[0].Type != "payment" {
		t.Fatalf("expected 1 payment notif to friend, got %+v", fake.Rows)
	}
}

func TestPaymentEnqueuesOnAddItem(t *testing.T) {
	ps, gs, q, c, owner := paySetup(t)
	ctx := context.Background()
	member := seedUser(t, q, "member")
	g, _ := q.GetGroup(ctx, c.GroupID)
	_, _ = gs.Join(ctx, member.ID, g.InviteCode)
	_, _ = ps.Activate(ctx, owner.ID, c.ID, 4500, nil)
	fake := &notify.FakeEnqueuer{}
	ps.SetEnqueuer(fake)
	if _, err := ps.AddItem(ctx, owner.ID, c.ID, member.ID, nil); err != nil {
		t.Fatalf("add item: %v", err)
	}
	if len(fake.Rows) != 1 || fake.Rows[0].UserID != member.ID || fake.Rows[0].Type != "payment" {
		t.Fatalf("expected 1 payment notif to member, got %+v", fake.Rows)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/service/ -run PaymentEnqueues
```
Expected: FAIL — `ps.SetEnqueuer undefined`.

- [ ] **Step 3: Write minimal implementation**

Add to `payments.go`:
```go
// add to PaymentService struct:
	enqueuer notify.Enqueuer

func (s *PaymentService) SetEnqueuer(e notify.Enqueuer) { s.enqueuer = e }

// helper for the two payment notification kinds
func payNotif(userID uuid.UUID, concertID uuid.UUID, title, body string) notify.Notification {
	return notify.Notification{UserID: userID, Type: "payment", Title: title, Body: body, URL: "/concerts/" + concertID.String()}
}
```
- **Confirm:** wrap the existing single `UpdatePaymentItemStatus` in a transaction when `s.enqueuer != nil`, and after the update enqueue one row to `item.UserID`:
  ```go
  // after the status checks in Confirm, when enqueuer is set:
  tx, err := s.pool.Begin(ctx); if err != nil { return gen.PaymentItem{}, err }
  defer tx.Rollback(ctx) //nolint:errcheck
  qtx := s.q.WithTx(tx)
  now := time.Now()
  updated, err := qtx.UpdatePaymentItemStatus(ctx, gen.UpdatePaymentItemStatusParams{ID: itemID, Status: "confirmed", ReportedAt: item.ReportedAt, ConfirmedAt: &now})
  if err != nil { return gen.PaymentItem{}, err }
  if err := s.enqueuer.Enqueue(ctx, qtx, []notify.Notification{
      payNotif(item.UserID, concertID, "Zahlung bestätigt", "Dein Ticket-Anteil wurde als bezahlt bestätigt.")}); err != nil {
      return gen.PaymentItem{}, err }
  if err := tx.Commit(ctx); err != nil { return gen.PaymentItem{}, err }
  return updated, nil
  // (keep the existing non-enqueuer path returning the plain UpdatePaymentItemStatus)
  ```
- **AddItem:** when `s.enqueuer != nil`, after creating the item, enqueue a "bitte bezahlen" row to `targetUserID`. AddItem already re-reads the row via `GetPaymentItemForUser`; do the create + enqueue inside a transaction (begin/WithTx/commit) so they are atomic; enqueue:
  ```go
  s.enqueuer.Enqueue(ctx, qtx, []notify.Notification{
      payNotif(targetUserID, concertID, "Bezahlung offen", "Bitte begleiche deinen Ticket-Anteil.")})
  ```
- **Activate (seeding):** inside the existing transaction, after seeding the items, enqueue one "bitte bezahlen" row per seeded user when `s.enqueuer != nil` (the loop already iterates `yes` user ids; collect rows and enqueue via `qtx` before `tx.Commit`).

> Keep all existing authorization/validation unchanged; only add the transactional enqueue branches. The non-enqueuer paths stay exactly as they are so existing tests pass. Import `notify`.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/service/ -run Payment && go test ./...
```
Expected: PASS, full module green.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service && git commit -m "feat: enqueue payment notifications on item create and confirm"
```

---

## Phase 5 — Reminder scan behavior

### Task 5.1: Verify reminder-scan recipients & idempotency

**Files:**
- Test: `backend/internal/store/gen/reminders_gen_test.go`

**Interfaces:**
- Consumes: the generated `ScanDeadlineReminders` / `ScanConcertReminders` (from Task 0.2) — this task pins their behavior with an integration test (and fixes the SQL if the test reveals a problem).

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/store/gen/reminders_gen_test.go
package gen_test

import (
	"context"
	"testing"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestReminderScanRecipientsAndIdempotency(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	owner, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "o", DisplayName: "O"})
	noResp, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "nr", DisplayName: "NoResp"})
	g, _ := q.CreateGroup(ctx, gen.CreateGroupParams{Name: "G", InviteCode: "RM", CreatedBy: owner.ID})
	_ = q.AddGroupMember(ctx, gen.AddGroupMemberParams{GroupID: g.ID, UserID: owner.ID, Role: "admin"})
	_ = q.AddGroupMember(ctx, gen.AddGroupMemberParams{GroupID: g.ID, UserID: noResp.ID, Role: "member"})

	soon := time.Now().Add(12 * time.Hour) // within the 24h window
	c, _ := q.CreateConcert(ctx, gen.CreateConcertParams{
		GroupID: g.ID, Artist: "Tool", EventAt: soon.Add(48 * time.Hour),
		RsvpDeadline: soon, CreatedBy: owner.ID})
	// owner says yes; noResp has no rsvp
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: owner.ID, Status: "yes"})

	if err := q.ScanDeadlineReminders(ctx); err != nil {
		t.Fatalf("scan deadline: %v", err)
	}
	rows, _ := q.ClaimPendingNotifications(ctx, 50)
	// exactly one deadline_soon, to the no-RSVP member (not the owner who said yes)
	var deadlineTo []string
	for _, r := range rows {
		if r.Type == "deadline_soon" {
			deadlineTo = append(deadlineTo, r.UserID.String())
		}
	}
	if len(deadlineTo) != 1 || deadlineTo[0] != noResp.ID.String() {
		t.Fatalf("deadline_soon recipients wrong: %v", deadlineTo)
	}
	// idempotency: a second scan inserts nothing new
	before := len(rows)
	if err := q.ScanDeadlineReminders(ctx); err != nil {
		t.Fatalf("scan2: %v", err)
	}
	rows2, _ := q.ClaimPendingNotifications(ctx, 50)
	if len(rows2) != before {
		t.Fatalf("second scan was not idempotent: %d -> %d", before, len(rows2))
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

```bash
cd backend && go test ./internal/store/gen/ -run ReminderScan
```
Expected: PASS. If it fails, fix `ScanDeadlineReminders` in `notifications.sql`, regenerate (`sqlc generate`), and re-run. (Common fix: the `c.id::text`/`user_id::text` casts in the `dedup_key` concatenation.)

- [ ] **Step 3: Commit**

```bash
git add backend && git commit -m "test: pin reminder-scan recipients and idempotency"
```

## Phase 6 — Worker

### Task 6.1: Worker loop (scan → send → prune)

**Files:**
- Create: `backend/internal/worker/worker.go`
- Test: `backend/internal/worker/worker_test.go`

**Interfaces:**
- Consumes: `*pgxpool.Pool`, `*gen.Queries` (scan/claim/mark/prune/list-subs/delete-sub), `push.Pusher`.
- Produces:
  - `func New(pool *pgxpool.Pool, q *gen.Queries, pusher push.Pusher) *Worker`
  - `func (w *Worker) Tick(ctx context.Context) error` — scan (advisory lock) → send pending (SKIP LOCKED) → prune.
  - `func (w *Worker) Run(ctx context.Context, interval time.Duration)` — ticker loop until ctx done.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/worker/worker_test.go
package worker_test

import (
	"context"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/push"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
	"github.com/jaydee94/pit-pilot/backend/internal/worker"
)

func seedNotif(t *testing.T, q *gen.Queries) (gen.User, gen.PushSubscription) {
	ctx := context.Background()
	u, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "w", DisplayName: "W"})
	sub, _ := q.UpsertPushSubscription(ctx, gen.UpsertPushSubscriptionParams{UserID: u.ID, Endpoint: "https://push/x", P256dh: "k", Auth: "a"})
	_ = q.InsertNotification(ctx, gen.InsertNotificationParams{UserID: u.ID, Type: "concert_new", Title: "T", Body: "B"})
	return u, sub
}

func TestWorkerSendsPending(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	u, _ := seedNotif(t, q)
	fp := &push.FakePusher{}
	if err := worker.New(pool, q, fp).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(fp.Sent) != 1 {
		t.Fatalf("expected 1 send, got %d", len(fp.Sent))
	}
	rem, _ := q.ClaimPendingNotifications(context.Background(), 10)
	if len(rem) != 0 {
		t.Fatalf("expected no pending left, got %d", len(rem))
	}
	_ = u
}

func TestWorkerDeletesDeadSubscription(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	u, _ := seedNotif(t, q)
	fp := &push.FakePusher{Dead: true}
	if err := worker.New(pool, q, fp).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	subs, _ := q.ListPushSubscriptionsForUser(context.Background(), u.ID)
	if len(subs) != 0 {
		t.Fatalf("expected dead subscription deleted, got %d", len(subs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/worker/...
```
Expected: FAIL — `undefined: worker.New`.

- [ ] **Step 3: Write minimal implementation**

```go
// backend/internal/worker/worker.go
package worker

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jaydee94/pit-pilot/backend/internal/push"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

// scanLockKey is an arbitrary app-wide key for the advisory lock guarding the scan.
const scanLockKey int64 = 0x70697470 // "pitp"

type Worker struct {
	pool   *pgxpool.Pool
	q      *gen.Queries
	pusher push.Pusher
	batch  int32
}

func New(pool *pgxpool.Pool, q *gen.Queries, pusher push.Pusher) *Worker {
	return &Worker{pool: pool, q: q, pusher: pusher, batch: 100}
}

func (w *Worker) Tick(ctx context.Context) error {
	if err := w.scan(ctx); err != nil {
		return err
	}
	if err := w.sendPending(ctx); err != nil {
		return err
	}
	return w.q.PruneOldNotifications(ctx)
}

func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = w.Tick(ctx) // best-effort; next tick retries
		}
	}
}

func (w *Worker) scan(ctx context.Context) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var got bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", scanLockKey).Scan(&got); err != nil {
		return err
	}
	if !got {
		return nil // another replica is scanning this tick
	}
	qtx := w.q.WithTx(tx)
	if err := qtx.ScanDeadlineReminders(ctx); err != nil {
		return err
	}
	if err := qtx.ScanConcertReminders(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (w *Worker) sendPending(ctx context.Context) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := w.q.WithTx(tx)
	rows, err := qtx.ClaimPendingNotifications(ctx, w.batch)
	if err != nil {
		return err
	}
	for _, n := range rows {
		subs, err := qtx.ListPushSubscriptionsForUser(ctx, n.UserID)
		if err != nil {
			return err
		}
		liveFailures := 0
		for _, sub := range subs {
			dead, serr := w.pusher.Send(ctx, push.Subscription{Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth}, payloadOf(n))
			if dead {
				if err := qtx.DeletePushSubscriptionByID(ctx, sub.ID); err != nil {
					return err
				}
				continue
			}
			if serr != nil {
				liveFailures++
			}
		}
		if liveFailures == 0 {
			if err := qtx.MarkNotificationSent(ctx, n.ID); err != nil {
				return err
			}
		} else {
			if err := qtx.MarkNotificationFailed(ctx, n.ID); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func payloadOf(n gen.Notification) push.Payload {
	url := ""
	if n.Url != nil {
		url = *n.Url
	}
	return push.Payload{Title: n.Title, Body: n.Body, URL: url}
}
```
> Note: `failed` rows are terminal (not re-claimed) — this MVP does not retry transient send failures; a retry/backoff loop is a documented follow-up. A user with no live subscriptions (none, or all dead) is marked `sent` (nothing to deliver), so it is not re-attempted.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/worker/... && go test ./...
```
Expected: PASS, full module green.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/worker && git commit -m "feat: notification worker (scan, send via SKIP LOCKED, dead-sub cleanup, prune)"
```

---

### Task 6.2: Wire worker + enqueuer into main

**Files:**
- Modify: `backend/cmd/api/main.go`
- (no new test; verified by `go build` + `go vet` + the full suite)

**Interfaces:**
- Consumes: `notify.OutboxEnqueuer`, the service `SetEnqueuer` methods, `push.NewWebPusher`, `worker.New`/`Run`.

- [ ] **Step 1: Wire it**

In `cmd/api/main.go`, after the services + `subs` are constructed and BEFORE building the router, add:
```go
	enq := notify.OutboxEnqueuer{}
	concerts.SetEnqueuer(enq, pool)
	rsvps.SetEnqueuer(enq, pool)
	payments.SetEnqueuer(enq)

	if cfg.VapidPrivateKey != "" {
		pusher := push.NewWebPusher(cfg.VapidPublicKey, cfg.VapidPrivateKey, cfg.VapidSubject)
		go worker.New(pool, q, pusher).Run(ctx, 60*time.Second)
	}
```
Add imports: `github.com/jaydee94/pit-pilot/backend/internal/notify`, `.../internal/push`, `.../internal/worker`. (`time` and `context` are already imported.)

- [ ] **Step 2: Verify build + vet + full suite**

```bash
cd backend && go build ./cmd/api && go vet ./... && rm -f api && go test ./...
```
Expected: builds, vet clean, full module green.

- [ ] **Step 3: Commit**

```bash
git add backend/cmd/api/main.go && git commit -m "feat: wire notification enqueuer and worker into main"
```

---

## Phase 7 — Frontend

### Task 7.1: Push API client

**Files:**
- Modify: `frontend/src/api/client.ts`
- Test: `frontend/src/api/push.test.ts`

**Interfaces:**
- Produces: `getVapidPublicKey()`, `savePushSubscription(sub)`, `deletePushSubscription(endpoint)`.

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/api/push.test.ts
import { afterEach, expect, test, vi } from "vitest";
import { getVapidPublicKey, savePushSubscription } from "./client";

afterEach(() => vi.restoreAllMocks());

test("getVapidPublicKey returns the key", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify({ public_key: "PUB" }), { status: 200 })));
  expect((await getVapidPublicKey()).public_key).toBe("PUB");
});

test("savePushSubscription posts the subscription json", async () => {
  const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) =>
    new Response(JSON.stringify({ id: "1" }), { status: 201 }));
  vi.stubGlobal("fetch", fetchMock);
  await savePushSubscription({ endpoint: "e", keys: { p256dh: "k", auth: "a" } });
  const init = fetchMock.mock.calls[0]?.[1];
  if (!init) throw new Error("no init");
  expect(init.method).toBe("POST");
  expect(JSON.parse(init.body as string)).toMatchObject({ endpoint: "e", keys: { p256dh: "k", auth: "a" } });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd frontend && npm run test -- --run push
```
Expected: FAIL — `getVapidPublicKey` not exported.

- [ ] **Step 3: Append to `client.ts`**

```ts
export type PushSubscriptionJSON = { endpoint: string; keys: { p256dh: string; auth: string } };

export const getVapidPublicKey = () => apiFetch<{ public_key: string }>("/api/push/vapid-public-key");
export const savePushSubscription = (sub: PushSubscriptionJSON) =>
  apiFetch<{ id: string }>("/api/push/subscriptions", { method: "POST", body: JSON.stringify(sub) });
export const deletePushSubscription = (endpoint: string) =>
  apiFetch<void>("/api/push/subscriptions", { method: "DELETE", body: JSON.stringify({ endpoint }) });
```

- [ ] **Step 4: Run test + build**

```bash
cd frontend && npm run test -- --run push && npm run build
```
Expected: PASS + build clean.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api && git commit -m "feat: push api client helpers"
```

---

### Task 7.2: Service worker (push + click) via injectManifest

**Files:**
- Create: `frontend/src/sw.ts`
- Modify: `frontend/vite.config.ts`
- (verified by `npm run build`; the SW is browser-only and not unit-tested)

**Interfaces:**
- Produces: a custom service worker that precaches (Workbox) and handles `push` (show notification) + `notificationclick` (focus/navigate to the deep-link URL).

- [ ] **Step 1: Write the service worker**

```ts
// frontend/src/sw.ts
/// <reference lib="webworker" />
import { precacheAndRoute } from "workbox-precaching";

declare let self: ServiceWorkerGlobalScope;

precacheAndRoute(self.__WB_MANIFEST);

self.addEventListener("push", (event: PushEvent) => {
  const data = (event.data && event.data.json()) || {};
  event.waitUntil(
    self.registration.showNotification(data.title || "pit-pilot", {
      body: data.body,
      data: { url: data.url || "/" },
    }),
  );
});

self.addEventListener("notificationclick", (event: NotificationEvent) => {
  event.notification.close();
  const url = (event.notification.data && event.notification.data.url) || "/";
  event.waitUntil(
    (async () => {
      const wins = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
      for (const c of wins) {
        await (c as WindowClient).focus();
        await (c as WindowClient).navigate(url);
        return;
      }
      await self.clients.openWindow(url);
    })(),
  );
});
```

- [ ] **Step 2: Switch `vite.config.ts` to injectManifest**

Replace the existing `VitePWA({ registerType: "autoUpdate" })` with:
```ts
    VitePWA({
      strategies: "injectManifest",
      srcDir: "src",
      filename: "sw.ts",
      registerType: "autoUpdate",
      injectManifest: { globPatterns: ["**/*.{js,css,html,svg,png,ico}"] },
    }),
```
(`vite-plugin-pwa` bundles `workbox-precaching`; if `npm run build` reports it missing, `npm install -D workbox-precaching@latest`.)

- [ ] **Step 3: Verify the build compiles the SW**

```bash
cd frontend && npm run build
```
Expected: build succeeds and emits `dist/sw.js` (injectManifest compiled `src/sw.ts`). If `tsc -b` complains about the worker lib types, add `"WebWorker"` to `tsconfig.json` `compilerOptions.lib` (alongside DOM) — note the change.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/sw.ts frontend/vite.config.ts frontend/tsconfig.json && git commit -m "feat: custom service worker with push + notificationclick handlers"
```

---

### Task 7.3: Push subscribe toggle

**Files:**
- Create: `frontend/src/components/PushToggle.tsx`
- Modify: `frontend/src/routes/Groups.tsx` (mount it)
- Test: `frontend/src/components/PushToggle.test.tsx`

**Interfaces:**
- Consumes: `getVapidPublicKey`, `savePushSubscription`, `deletePushSubscription`.
- Produces: `function PushToggle()` — renders an enable/disable button; gracefully renders an "unsupported" note when the browser lacks push; enabling requests permission, subscribes via the service worker registration, and saves the subscription.

- [ ] **Step 1: Write the failing test**

```tsx
// frontend/src/components/PushToggle.test.tsx
import { render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import PushToggle from "./PushToggle";

afterEach(() => vi.restoreAllMocks());

test("renders an unsupported note when push is unavailable", () => {
  vi.stubGlobal("navigator", {});             // no serviceWorker
  render(<PushToggle />);
  expect(screen.getByText(/nicht unterstützt/i)).toBeInTheDocument();
});

test("renders an enable button when supported and not yet granted", () => {
  vi.stubGlobal("navigator", { serviceWorker: { ready: Promise.resolve({}) } });
  vi.stubGlobal("Notification", { permission: "default" } as unknown as typeof Notification);
  render(<PushToggle />);
  expect(screen.getByRole("button", { name: /benachrichtigungen aktivieren/i })).toBeInTheDocument();
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd frontend && npm run test -- --run PushToggle
```
Expected: FAIL — cannot find `./PushToggle`.

- [ ] **Step 3: Write the component**

```tsx
// frontend/src/components/PushToggle.tsx
import { useState } from "react";
import { getVapidPublicKey, savePushSubscription, deletePushSubscription } from "../api/client";

function urlBase64ToUint8Array(base64: string): Uint8Array {
  const padding = "=".repeat((4 - (base64.length % 4)) % 4);
  const b64 = (base64 + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(b64);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

const supported = () =>
  typeof navigator !== "undefined" && "serviceWorker" in navigator &&
  typeof Notification !== "undefined" && "PushManager" in (globalThis as unknown as Record<string, unknown>);

export default function PushToggle() {
  const [status, setStatus] = useState("");
  if (!supported()) {
    return <p>Push-Benachrichtigungen werden von diesem Browser nicht unterstützt.</p>;
  }

  const enable = async () => {
    try {
      const perm = await Notification.requestPermission();
      if (perm !== "granted") { setStatus("Erlaubnis verweigert"); return; }
      const reg = await navigator.serviceWorker.ready;
      const { public_key } = await getVapidPublicKey();
      const sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(public_key),
      });
      const j = sub.toJSON() as { endpoint?: string; keys?: { p256dh?: string; auth?: string } };
      await savePushSubscription({
        endpoint: j.endpoint ?? "",
        keys: { p256dh: j.keys?.p256dh ?? "", auth: j.keys?.auth ?? "" },
      });
      setStatus("Benachrichtigungen aktiv");
    } catch {
      setStatus("Aktivierung fehlgeschlagen");
    }
  };

  const disable = async () => {
    const reg = await navigator.serviceWorker.ready;
    const sub = await reg.pushManager.getSubscription();
    if (sub) { await deletePushSubscription(sub.endpoint); await sub.unsubscribe(); }
    setStatus("Benachrichtigungen aus");
  };

  return (
    <div>
      <button onClick={enable}>Benachrichtigungen aktivieren</button>
      <button onClick={disable}>aus</button>
      {status && <span role="status">{status}</span>}
    </div>
  );
}
```

- [ ] **Step 4: Mount in Groups.tsx**

Add the import and render it in the Groups screen header:
```tsx
import PushToggle from "../components/PushToggle";
// …inside the returned JSX, after the <h1>Meine Gruppen</h1>:
<PushToggle />
```

- [ ] **Step 5: Run tests + build**

```bash
cd frontend && npm run test -- --run && npm run build
```
Expected: all tests PASS, build clean. (The enable/disable flow itself is exercised manually in a real browser; the unit tests cover the supported/unsupported rendering branches.)

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/PushToggle.tsx frontend/src/components/PushToggle.test.tsx frontend/src/routes/Groups.tsx && git commit -m "feat: push subscribe toggle on groups screen"
```

---

## Phase 8 — Deploy

### Task 8.1: VAPID secrets + sync migration

**Files:**
- Modify: `deploy/overlays/dev/secret.example.env`
- Create: `deploy/base/migrations/0003_notifications.up.sql` + `.down.sql` (copies)
- Modify: `deploy/base/kustomization.yaml`

- [ ] **Step 1: Add VAPID placeholders to the dev secret example**

Append to `deploy/overlays/dev/secret.example.env`:
```dotenv
VAPID_PUBLIC_KEY=
VAPID_PRIVATE_KEY=
VAPID_SUBJECT=mailto:dev@example.com
```
> Generate a real keypair once for a deployment, e.g. `go run github.com/SherClockHolmes/webpush-go/cmd/...` or any VAPID generator, and place the values in the real (gitignored) `secret.env`. Empty keys mean the worker does not start (no push) — the rest of the app still runs.

- [ ] **Step 2: Sync the migration + list it**

```bash
make sync-migrations
```
Then add to `deploy/base/kustomization.yaml` under `configMapGenerator.files`:
```yaml
      - migrations/0003_notifications.up.sql
      - migrations/0003_notifications.down.sql
```

- [ ] **Step 3: Verify the render**

```bash
kubectl kustomize deploy/overlays/dev > /dev/null && echo RENDER_OK
```
Expected: `RENDER_OK`.

- [ ] **Step 4: Commit**

```bash
git add deploy && git commit -m "chore: VAPID secrets placeholder + sync notifications migration"
```

---

## Self-Review

**Spec coverage (each spec section → task):**
- §1 triggers (1 concert_new, 2 deadline_soon, 3 concert_soon, 4 payment, 5 rsvp_changed) → 4.1, 5.1, 5.1, 4.3, 4.2 respectively.
- §2 architecture (transactional outbox + in-process worker; multi-replica safety) → notify (2.1), worker advisory-lock + SKIP LOCKED (6.1), enqueue hooks (4.x).
- §3 data model (push_subscriptions, notifications, dedup_key, partial index) → 0.1; queries 0.2.
- §4 enqueue/scan/worker/sender → 2.1 (enqueuer), 0.2+5.1 (scan), 6.1 (worker), 3.1 (sender + FakePusher).
- §5 API (vapid-key, subscribe, unsubscribe) → 1.3.
- §6 frontend (SW, toggle, client) → 7.1, 7.2, 7.3.
- §7 config/deploy (VAPID, worker in-process, migration sync) → 1.1, 6.2, 8.1.
- §8 test strategy → throughout (sqlc gen tests, FakeEnqueuer service tests, scan integration test, worker+FakePusher tests, handler tests, frontend client + toggle).
- §9 build order → Phases 0→8.

**Resolved during planning:**
- Optional-dependency wiring via `SetEnqueuer` (concert/rsvp gain a pool param; payment already has one) avoids changing existing constructor signatures and keeps all prior tests compiling.
- `dedup_key` NULL for event-driven (NULLs distinct → no false dedup), deterministic for reminders.
- Pruning is age-based (created_at < now-30d) per the spec's "or older than a retention window" — no concert_id needed on `notifications`; safe because reminders are created within 24h of the event.
- `webpush-go` `WebPusher` is exercised via the worker's `FakePusher` (no live-network unit test).

**Known follow-ups (documented, non-blocking):** no retry/backoff for `failed` sends (terminal); the heavy browser subscribe flow is manually tested (units cover the branches); no per-type preferences (out of scope).



