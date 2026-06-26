# pit-pilot — Cycle 3 Design: Notifications

**Date:** 2026-06-26
**Status:** Approved design, pending implementation plan
**Scope:** Cycle 3 of the pit-pilot platform — Web Push notifications, built on the Cycle 1 (coordination) and Cycle 2 (payments) features.

---

## 1. Product Vision & Scope

Users should be notified about new concert dates, upcoming concerts, RSVP activity, and payment events — without opening the app. Cycle 3 delivers **Web Push notifications** for five triggers, push-only, on the existing Go API + Postgres + React PWA + Kubernetes stack.

### The five triggers

| # | Type | Source | Recipients |
|---|------|--------|-----------|
| 1 | `concert_new` | `ConcertService.Create` (event-driven) | all group members except the creator |
| 2 | `deadline_soon` | reminder scan (~24h before `rsvp_deadline`) | group members with **no** RSVP |
| 3 | `concert_soon` | reminder scan (~24h before `event_at`) | members who RSVP'd `yes` |
| 4 | `payment` | `PaymentService`: item created → "bitte bezahlen"; `Confirm` → "Eingang bestätigt" | the affected debtor |
| 5 | `rsvp_changed` | `RSVPService.Set` (event-driven) | other `yes`-responders of that concert |

These split into **event-driven** (1, 4, 5 — fire when a domain action commits) and **time-driven reminders** (2, 3 — a periodic scanner finds concerts whose deadline/event is approaching).

### Key product decisions (settled in brainstorming)

- **Push only.** No email, no in-app inbox (per the Cycle 1 decision).
- **Web Push (VAPID)** for the PWA. FCM/APNs come later with native apps.
- **All-or-nothing.** Enabling push (browser permission + subscription) opts the user into all five trigger types. No per-type preferences (a later cycle if wanted).
- **Reminder timing is fixed at ~24h** before the deadline/event (not configurable in this cycle).
- **Deadline reminder targets members with no RSVP at all** (those who said "no" are left alone).
- **Multi-device:** a user may have many subscriptions (phone + laptop); all are served. Dead subscriptions (push service returns 404/410) are auto-deleted on send.
- **Click-through:** notifications carry a deep-link URL; tapping opens the relevant concert (or payment area). 
- **Outbox retention:** sent/failed rows are kept (needed for reminder idempotency and debugging) and pruned once the concert is over.

### Explicitly out of scope (later cycles)

Per-type notification preferences; email; in-app notification inbox; FCM/APNs (native apps); digest/batching; configurable reminder lead times.

---

## 2. Architecture

Extends the Cycle 1/2 backend and PWA. Two design pillars settled during brainstorming:

- **Transactional outbox + in-process worker.** A domain event writes a row into a `notifications` outbox table **in the same transaction** as the domain change; an in-process worker (a ticker goroutine started in each API replica) sends pending rows. This decouples the triggering request from push delivery and gives retry + dead-subscription cleanup, with no external broker (everything in Postgres). The same worker also runs the time-based reminder scan.
- **Multi-replica safety via Postgres.** The reminder scan runs under a Postgres advisory lock (only one replica scans per tick); sends claim rows with `SELECT … FOR UPDATE SKIP LOCKED`; reminders carry an idempotency `dedup_key` so repeated scans are no-ops.

New backend units (mirroring the existing three-layer pattern):
- `store/queries/notifications.sql` + `push_subscriptions.sql` → generated `gen` queries.
- `notify` package: `Enqueuer` interface (`Enqueue(ctx, tx, rows)`), an outbox implementation, the `Notification` row type, and a `FakeEnqueuer` for tests.
- `push` package: `Pusher` interface (`Send(ctx, sub, payload) (dead bool, err error)`), a `webpush-go` implementation, and a `FakePusher` for tests.
- `worker` package: the loop (scan → send → prune), startable from `main.go`.
- `service/notifications.go` (or methods): the reminder scan + the recipient queries; subscription management.
- `httpapi/push_handlers.go`: subscription + VAPID-key endpoints.
- Small edits to `ConcertService.Create`, `RSVPService.Set`, `PaymentService` to enqueue within their transactions (some gain a transaction wrapper, like `GroupService.Create`).

New frontend units:
- A custom service worker (`src/sw.ts`, via `vite-plugin-pwa` `injectManifest`) with `push` + `notificationclick` handlers.
- A push-subscription toggle component + API client helpers.

---

## 3. Data Model (Postgres)

Two new tables, attaching to `users`. No changes to existing tables.

```
push_subscriptions
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid()
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE
  endpoint    text NOT NULL UNIQUE          -- push endpoint URL (unique per browser/device)
  p256dh      text NOT NULL                 -- client public key (from the browser subscription)
  auth        text NOT NULL                 -- auth secret (from the browser subscription)
  created_at  timestamptz NOT NULL DEFAULT now()
  -- many rows per user (multi-device)

notifications                                -- the outbox
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid()
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE   -- recipient
  type        text NOT NULL CHECK (type IN ('concert_new','deadline_soon','concert_soon','payment','rsvp_changed'))
  title       text NOT NULL
  body        text NOT NULL
  url         text                          -- deep-link path, e.g. /concerts/{id}
  dedup_key   text UNIQUE                   -- idempotency for reminders; NULL for event-driven rows
  status      text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sent','failed'))
  attempts    int  NOT NULL DEFAULT 0
  created_at  timestamptz NOT NULL DEFAULT now()
  sent_at     timestamptz

CREATE INDEX idx_notifications_pending ON notifications (status) WHERE status = 'pending';
```

- `endpoint UNIQUE` → a browser re-registers idempotently (upsert); one subscription per endpoint.
- `dedup_key UNIQUE`, nullable → reminders (2, 3) set a deterministic key (e.g. `deadline_soon:{concertID}:{userID}`) so repeated scans `INSERT … ON CONFLICT DO NOTHING` become no-ops. Event-driven rows (1, 4, 5) leave `dedup_key` NULL — Postgres treats NULLs as distinct, so legitimately repeated events are never deduped away.
- Partial index on `status = 'pending'` for fast worker pickup.
- The recipient is `user_id`; the sender resolves that user's active `push_subscriptions` at send time (multi-device).

---

## 4. Enqueue, Reminder Scan, Worker & Sender

### Transactional outbox (event-driven: triggers 1/4/5)

`Enqueuer` interface: `Enqueue(ctx context.Context, tx pgx.Tx, rows []notify.Notification) error` — writes outbox rows **through the supplied transaction**, so they commit atomically with the domain change. Service edits (each gains an injected enqueuer field):

- `ConcertService.Create` → after the insert, in the same tx: one `concert_new` row per group member except the creator.
- `RSVPService.Set` → when a user sets `yes`: one `rsvp_changed` row per other `yes`-responder of that concert.
- `PaymentService` → on item creation (activation seeding + `AddItem`): a `payment` row ("bitte bezahlen") to the item's user; on `Confirm`: a `payment` row ("Eingang bestätigt") to the item's user.

`ConcertService.Create` and `RSVPService.Set` do not currently run in an explicit transaction; where needed they gain a small transaction wrapper (as `GroupService.Create` already has). Tests use a `FakeEnqueuer` that records the rows, so a service test asserts "action X enqueues the right rows".

### Reminder scan (time-driven: triggers 2/3)

`INSERT … SELECT … ON CONFLICT (dedup_key) DO NOTHING`:
- `deadline_soon`: concerts with `rsvp_deadline` in `(now, now+24h]`, one row per group member with **no** RSVP, `dedup_key = 'deadline_soon:{concertID}:{userID}'`.
- `concert_soon`: concerts with `event_at` in `(now, now+24h]`, one row per `yes`-responder, `dedup_key = 'concert_soon:{concertID}:{userID}'`.

### Worker loop (started in each API replica; ticker e.g. every 60s)

1. **Scan** under a Postgres advisory lock (`pg_try_advisory_lock`) — only one replica scans per tick; the idempotency unique is the backstop.
2. **Send:** `SELECT … WHERE status='pending' FOR UPDATE SKIP LOCKED LIMIT N` → for each row, deliver to all the user's `push_subscriptions` → set `status='sent'`/`'failed'`, `attempts++`. On 404/410 from the push service, delete that subscription.
3. **Prune:** delete sent/failed rows whose concert is over (or older than a retention window).

### Sender (VAPID)

Behind a `Pusher` interface: `Send(ctx, sub, payload) (dead bool, err error)` — `dead` signals a 404/410 so the worker deletes the subscription. The real implementation uses `webpush-go` (latest); the VAPID keypair comes from config/secret. Tests use a `FakePusher` (configurable success / dead / error) so worker tests need no real network.

---

## 5. API

All under `/api` (session-gated):

| Method & path | Purpose |
|---|---|
| `GET /api/push/vapid-public-key` | returns the VAPID public key (the browser needs it to subscribe) |
| `POST /api/push/subscriptions` | body `{endpoint, keys:{p256dh, auth}}` (shape of `PushSubscription.toJSON()`) → upsert for the session user |
| `DELETE /api/push/subscriptions` | body `{endpoint}` → delete the subscription (unsubscribe) |

Errors use the existing JSON shape `{"error":{"code","message"}}`.

---

## 6. Frontend

- **Service worker** via `vite-plugin-pwa` in **`injectManifest`** mode with a custom `src/sw.ts`: Workbox precaching **plus** a `push` handler (`self.registration.showNotification(title, {body, data:{url}})`) and a `notificationclick` handler (focus an existing client at the URL or `clients.openWindow(url)`).
- **"Benachrichtigungen aktivieren" toggle** (e.g. on the Groups screen): checks `Notification.permission` + any existing subscription; enabling = `Notification.requestPermission()` → `registration.pushManager.subscribe({userVisibleOnly:true, applicationServerKey})` (key fetched from the VAPID endpoint, base64url-decoded) → POST the subscription; disabling = `subscription.unsubscribe()` + DELETE.
- **API client:** `getVapidPublicKey`, `savePushSubscription`, `deletePushSubscription`.

---

## 7. Configuration & Deployment

- New config/secrets: `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY`, `VAPID_SUBJECT` (a `mailto:` URL). Generated once (offline) and stored as a Kubernetes secret; added to the dev overlay's `secret.example.env` as placeholders.
- The worker runs inside the API process, so no new deployable. (Multi-replica safe per §4.)
- The Cycle-3 migration must be synced into `deploy/base/migrations/` (`make sync-migrations`) and listed in the kustomize `configMapGenerator` — the established deploy step.

---

## 8. Test Strategy (TDD is mandatory)

Strict Red → Green → Refactor, same harness as prior cycles.

- **Backend:**
  - **sqlc queries** (subscriptions upsert/delete/list-by-user; outbox insert/claim/mark/prune; reminder-scan inserts) against testcontainers Postgres.
  - **Enqueue tests** with a `FakeEnqueuer`: each domain action enqueues the correct rows to the correct recipients (concert_new to members-except-creator; rsvp_changed to other yes-responders; payment on item-create and on confirm).
  - **Reminder-scan tests**: concerts seeded with deadlines/events inside and outside the 24h window; assert correct recipients (no-RSVP for deadline, yes for concert-soon) and **idempotency** (a second scan inserts nothing).
  - **Worker/send tests** with a `FakePusher`: pending rows transition to `sent`; a 410 deletes the offending subscription; `SKIP LOCKED` claim works.
  - **Subscription + VAPID-key handlers** via `httptest`.
- **Frontend:** the subscribe toggle (mocking `Notification`, `navigator.serviceWorker`, `PushManager`) and the API client (`fetch` mocked).
- **CI** runs backend + frontend tests on every push.

---

## 9. Build Order Within Cycle 3

1. Migration + sqlc for `push_subscriptions` and `notifications`.
2. Subscription API (handlers + routes) + VAPID-key endpoint + config.
3. `notify.Enqueuer` (outbox impl + `FakeEnqueuer`) and the `notify.Notification` type.
4. `push.Pusher` (`webpush-go` impl + `FakePusher`).
5. Event hooks: enqueue in `ConcertService.Create`, `RSVPService.Set`, `PaymentService` (with transaction wrappers where needed).
6. Reminder scan queries + service.
7. Worker loop (scan + send + prune) wired into `main.go`.
8. Frontend: API client, the service worker (`injectManifest`), the subscribe toggle.
9. Deploy: VAPID secrets in the overlay; sync the migration into the kustomize deploy dir.

Each step is built test-first.
