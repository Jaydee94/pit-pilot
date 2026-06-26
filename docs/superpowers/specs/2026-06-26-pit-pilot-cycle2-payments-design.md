# pit-pilot — Cycle 2 Design: Payment Tracking

**Date:** 2026-06-26
**Status:** Approved design, pending implementation plan
**Scope:** Cycle 2 of the pit-pilot platform — per-concert ticket-payment tracking, built on the Cycle 1 MVP.

---

## 1. Product Vision & Scope

Cycle 1 delivered the core coordination loop (login → groups → concerts → Yes/No RSVP). Cycle 2 adds the second feature block: **when one person buys the tickets for the group, they can track who still owes what.** No real money flows through the app — this is status + amount tracking only.

Built on the Cycle 1 stack: Go API (chi + pgx/sqlc + golang-migrate) + Postgres + React PWA + Kubernetes.

### Core flow

> A member opens a concert → clicks **"Ich kümmere mich um die Tickets"** and sets a default amount → tracking activates and the list is **pre-filled from the current Yes-RSVPs** at the default amount → the responsible person adjusts per-person amounts / adds / removes people → each member sees **only their own** item and clicks **"bezahlt"** (reported) → the responsible person **confirms** receipt. Reporting and confirming are both **reversible**.

### Explicitly out of scope for Cycle 2 (later cycles)

- Push notifications for payment events ("please pay", "receipt confirmed") — belongs to the Notifications subsystem.
- Transfer of the responsible role to another member.
- Partial payments.
- Multiple responsible people per concert.
- Real money movement / payment-provider integration.

---

## 2. Key Product Decisions

Settled during brainstorming (these refine / upgrade the placeholder assumptions from the Cycle 1 spec):

- **Amounts, not status-only.** Each participant has a per-person amount (upgrade from Cycle 1's status-only assumption). The app sums outstanding/confirmed money. Still no real payment flow.
- **Activation is opt-in per concert.** A member clicks "I'll handle the tickets" and becomes the responsible person; that creates the tracking for this concert. Not every concert has tracking.
- **One responsible per concert** (enforced by a unique constraint on `concert_id`). Switching responsible is out of scope for Cycle 2 (the responsible may deactivate instead).
- **Amount entry: default-for-all, overridable per person.** The responsible sets one default amount at activation; every item is seeded with it; individual amounts can be edited.
- **Reporting: one click for the full amount** (no partial payments, no method/note).
- **Two-stage with direct-confirm.** Member reports "paid"; responsible confirms receipt. The responsible may also confirm directly (e.g. cash in hand) without a prior report.
- **Reversibility:** the member can un-report while not yet confirmed; the responsible can un-confirm. (5a)
- **RSVP changes do not auto-touch payment items.** The responsible manages the list manually after seeding. (5b — no auto-sync logic)
- **Seeding: pre-filled from Yes-RSVPs** at the default amount on activation; the responsible then adjusts. (6)
- **Visibility: responsible + the affected person only.** Each member sees only their own item; the responsible sees the full named list plus a summary. No group-wide overview for non-responsible members. (Visibility option C)
- **Payment link (PayPal etc.).** The responsible person can store an optional pay-me link (e.g. a PayPal.me URL) on the collection — set at activation or edited later. Every owing member sees it on their own item and can open it to transfer directly. Stored as a generic URL (not PayPal-specific in the model); the UI labels it "Per PayPal zahlen". Link scope is per-collection (a reusable profile-level link is a later cycle, as it would touch the Cycle 1 user profile).

---

## 3. Architecture

Extends the Cycle 1 backend and frontend; no change to existing tables. Same three-layer Go backend (HTTP handlers → service → sqlc store) and the same React PWA. The payment data model uses a **"collection + items"** aggregate (chosen over columns-on-concert or a single denormalized table) because the collection is the natural aggregate root for the per-concert responsible role: it keeps "is tracking active?" and authorization (`collection.responsible_user_id`) clean and isolates payment concerns from the `concerts` entity.

New backend units (mirroring the Cycle 1 layering):
- `store/queries/payments.sql` → generated `gen` queries.
- `service/payments.go` — `PaymentService`: activation/seeding, item management, the state machine, and all authorization rules.
- `httpapi/payment_handlers.go` — HTTP handlers + routes.

New frontend units:
- `api/client.ts` — a `payments` section (helpers + types).
- A payment section on the existing `ConcertDetail` screen, rendered by role/state.

---

## 4. Data Model (Postgres)

Two new tables. UUID PKs, money as integer cents. No changes to existing tables; both cascade from the concert.

```
payment_collections
  id                   uuid pk
  concert_id           uuid NOT NULL UNIQUE REFERENCES concerts(id) ON DELETE CASCADE  -- max 1 per concert = "tracking active?"
  responsible_user_id  uuid NOT NULL REFERENCES users(id)
  default_amount_cents int NOT NULL
  payment_link         text NULL                          -- optional pay-me URL (e.g. PayPal.me), set by the responsible
  created_at           timestamptz NOT NULL DEFAULT now()

payment_items
  id            uuid pk
  collection_id uuid NOT NULL REFERENCES payment_collections(id) ON DELETE CASCADE
  user_id       uuid NOT NULL REFERENCES users(id)
  amount_cents  int  NOT NULL                                       -- seeded from default, overridable
  status        text NOT NULL DEFAULT 'open' CHECK (status IN ('open','reported','confirmed'))
  reported_at   timestamptz NULL                                    -- audit trail
  confirmed_at  timestamptz NULL
  UNIQUE (collection_id, user_id)
```

- `concert_id UNIQUE` enforces one collection (one responsible) per concert at the DB level.
- `amount_cents` per item; seeded from `default_amount_cents`, editable.
- `status` is a CHECK-constrained enum; `reported_at`/`confirmed_at` are nullable timestamps kept as an audit trail.
- Deleting a concert cascades to the collection and its items.

---

## 5. State Machine

Per item — `open → reported → confirmed`, reversible in both directions:

| Action | Who | From → To | Effect |
|---|---|---|---|
| **report** | item owner (self) | `open` → `reported` | `reported_at = now` |
| **un-report** | item owner | `reported` → `open` | `reported_at = NULL` |
| **confirm** | responsible | `open` / `reported` → `confirmed` | `confirmed_at = now` |
| **un-confirm** | responsible | `confirmed` → `reported` (if `reported_at` set) else `open` | `confirmed_at = NULL` |

- The responsible may **confirm directly** (e.g. cash in hand) without a prior report — they are the source of truth on receipt.
- The owner may toggle report/un-report only while the item is **not** `confirmed`.

Invalid transitions (e.g. report on a confirmed item, confirm by a non-responsible) return a typed error → `400` (bad transition) or `403` (wrong actor).

---

## 6. Authorization

Every payment operation is additionally gated by **group membership of the concert** (derived concert → group, exactly as Cycle 1's `ConcertService.Get` gate). On top of that:

| Operation | Allowed for |
|---|---|
| Activate tracking (create collection) | any **group member** → becomes the responsible |
| Add/remove items, set amounts | the **responsible** only |
| Confirm / un-confirm | the **responsible** only |
| Deactivate (delete collection) | the **responsible** only |
| Report / un-report own item | the **item owner** only |
| View | responsible: **all** items + summary; member: **only their own** item (visibility C) |

The service layer derives authorization from `collection.responsible_user_id` and `item.user_id`, analogous to Cycle 1's `RequireMembership`.

---

## 7. API Surface (REST/JSON)

All under `/api/concerts/{concertID}/payments`; membership is gated via the concert → group lookup, finer payment authorization in the service layer.

| Method & path | Purpose | Auth |
|---|---|---|
| `POST /api/concerts/{concertID}/payments` | activate; body `{default_amount_cents, payment_link?}` → creator becomes responsible, items seeded from Yes-RSVPs; **409** if already active | member |
| `GET /api/concerts/{concertID}/payments` | view (responsible: all items + summary; member: own item only); both see `payment_link`; **404** if inactive | member |
| `PATCH /api/concerts/{concertID}/payments` | set/clear the pay-me link; body `{payment_link}` (null clears) | responsible |
| `DELETE /api/concerts/{concertID}/payments` | deactivate (collection + items removed) | responsible |
| `POST /api/concerts/{concertID}/payments/items` | add item; body `{user_id, amount_cents?}` (default if omitted) | responsible |
| `PATCH /api/concerts/{concertID}/payments/items/{itemID}` | set amount; body `{amount_cents}` | responsible |
| `DELETE /api/concerts/{concertID}/payments/items/{itemID}` | remove item | responsible |
| `POST /api/concerts/{concertID}/payments/items/{itemID}/report` | report paid | item owner |
| `DELETE /api/concerts/{concertID}/payments/items/{itemID}/report` | un-report | item owner |
| `POST /api/concerts/{concertID}/payments/items/{itemID}/confirm` | confirm receipt | responsible |
| `DELETE /api/concerts/{concertID}/payments/items/{itemID}/confirm` | un-confirm | responsible |

- Errors use the existing JSON shape `{"error":{"code","message"}}` with status 400/403/404/409.
- The `GET` response, for the responsible, includes a summary: `outstanding_cents`, `confirmed_cents`, and counts per status. For a plain member it includes only their own item (and the collection's existence), never others' data. Both responsible and member responses include the collection's `payment_link` (so a member can pay).

---

## 8. Frontend

A **payment section on the existing ConcertDetail screen**, rendered by role/state:

- **Inactive:** a "Ich kümmere mich um die Tickets" button → a dialog for the default amount and an optional pay-me link (PayPal.me) → activates.
- **Active & I am responsible:** a management view — list of all items (name, amount, status), inline amount editing, add/remove people, confirm/un-confirm buttons, a summary line ("X € ausstehend, Y/N bestätigt"), and an editable pay-me link field.
- **Active & I have an item:** my own item only — amount, status, a "bezahlt" / "zurücknehmen" button, and — if the collection has a pay-me link — a "Per PayPal zahlen" button opening that link in a new tab. No one else's data.
- **Active & I have no item:** a quiet "kein Posten für dich" note.

Money is carried as integer cents in the API client and formatted as "€" in the UI. The client gains a `payments` area (typed helpers + types) reusing the Cycle 1 patterns (cookie auth, TanStack Query, query invalidation on mutations).

---

## 9. Test Strategy (TDD is mandatory)

Strict Red → Green → Refactor, same discipline and harness as Cycle 1.

- **Backend:**
  - **sqlc queries** against testcontainers Postgres (activation/seeding, item CRUD, state transitions, summary aggregation).
  - **Service tests** covering the **entire state machine and every authorization rule**, including negative paths: non-responsible confirms → 403; reporting someone else's item → 403; activating twice → 409; invalid transition (e.g. report on a confirmed item) → 400.
  - **Handler tests** via `httptest`.
- **Frontend:** Vitest + React Testing Library for the three views (responsible / own-item / no-item), `fetch` mocked.
- **CI:** the existing pipeline runs backend + frontend tests on every push.

---

## 10. Build Order Within Cycle 2

The implementation plan will sequence roughly as:

1. Migration + sqlc queries for `payment_collections` / `payment_items` (+ the Yes-RSVP seeding query and the summary aggregation).
2. `PaymentService`: activation/seeding, item management, the state machine, authorization — test-first.
3. HTTP handlers + routes wired under `/api/concerts/{concertID}/payments`.
4. Frontend API-client `payments` area.
5. The ConcertDetail payment section (the three role/state views).

Each step is built test-first. After this cycle, the migration files must also be synced into `deploy/base/migrations/` (via `make sync-migrations`) and listed in the kustomize `configMapGenerator` — the known Cycle 1 deploy follow-up.
