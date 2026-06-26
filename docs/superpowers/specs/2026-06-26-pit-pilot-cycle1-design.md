# pit-pilot — Cycle 1 Design (Vertical MVP)

**Date:** 2026-06-26
**Status:** Approved design, pending implementation plan
**Scope:** Cycle 1 of the pit-pilot platform — a vertical MVP slice through the core flow.

---

## 1. Product Vision

pit-pilot is a Kubernetes-native application that helps groups who go to concerts together
plan and manage upcoming concerts. The central loop is **coordination**: someone creates a
concert date, and the others say whether they are coming (Yes/No).

The full platform spans several subsystems, each of which gets its own spec → plan →
implementation cycle:

| # | Building block | Status |
|---|----------------|--------|
| **1** | **Backend (Go) + Postgres + Kubernetes deployment + minimal React PWA** | **This spec** |
| 2 | Payment / ticket tracking | Later cycle |
| 3 | Push notifications + reminders | Later cycle |
| 4 | Native mobile apps (Android + iOS) | Later cycle |

This document specifies **Cycle 1 only**: a thin, complete, end-to-end slice through the
core flow, deployable on Kubernetes.

### Core flow delivered in Cycle 1

> Login (Google/Apple) → see my groups / create a group → join via invite code →
> create a concert in a group → members RSVP Yes/No → everyone sees who is coming.

### Explicitly out of scope for Cycle 1

The following are deferred to later cycles. The data model is shaped so they attach cleanly
without rework, but no logic for them is built now:

- Payment / ticket-payment tracking
- Push notifications and deadline/concert reminders
- Native mobile apps
- External concert API integration (e.g. Songkick/Bandsintown)

---

## 2. Key Product Decisions

These were settled during brainstorming and drive the design:

- **Groups:** A user can belong to **multiple** groups. Each concert belongs to exactly one
  group. Join via **invite link/code**. Group roles are **admin** and **member**.
- **Concerts:** Created **manually** (no external API in Cycle 1).
- **RSVP:** **Yes / No** only. An RSVP **deadline is required** per concert. No seat limits,
  no plus-ones — each member answers only for themselves.
- **Payment (later):** status-only (paid/open), two-stage flow — a member reports
  "transferred", the ticket-responsible person confirms receipt. Three states:
  `open → reported → confirmed`. "Ticket-responsible" is a **per-concert** role.
- **Notifications (later):** push only. Triggers: new concert in my group; deadline-approaching
  reminder; concert-soon reminder; payment events; someone RSVPs to a concert I am attending.
- **Auth:** Social login via **Google + Apple**. Profile = display name + avatar (typically
  sourced from the provider).
- **Stack:** **Go** backend, **Postgres**, **React PWA** (responsive: mobile + large screens).
  Native apps deferred. Push for the PWA will use the **Web Push API (VAPID)**; FCM/APNs come
  with the native apps later.

---

## 3. Architecture

```
[ React PWA (Vite) ]  ──HTTPS/JSON──►  [ Go API service ]  ──►  [ Postgres ]
   Browser, responsive                  REST, session cookie       (StatefulSet/PVC)
        │                                      │
        └── OIDC with Google/Apple ────────────┘ (backend verifies provider ID token)

   All running in Kubernetes: Ingress → API + Frontend; Secrets for OAuth / DB / signing key
```

- **Go API:** a single service (REST/JSON). Router: `chi` (or `gin`). DB access via `pgx` +
  `sqlc` (type-safe queries). Migrations via `golang-migrate`.
- **Postgres:** in Kubernetes as a StatefulSet with a PVC (dev). A production overlay may point
  at a managed database instead.
- **Frontend:** React PWA, responsive for mobile + large screens, served as static assets
  (nginx container) behind the Ingress.
- **Auth:** the client obtains an ID token from Google/Apple → the backend verifies the
  signature against the provider JWKS → upserts the user → issues its own **session JWT in an
  httpOnly cookie**.

### Backend layering

Three layers, separately testable:

- **HTTP handlers** — request parsing, validation at the boundary, response shaping.
- **Service layer** — domain logic, authorization rules, orchestration. No HTTP, no SQL
  details leaking in.
- **Store layer** — `sqlc`-generated, type-safe Postgres queries.

---

## 4. Data Model (Postgres)

UUID primary keys. Money stored as integer cents (never floats). Fields needed by later
features that belong on the concert/RSVP model are added now; dedicated payment tables come in
a later cycle.

```
users
  id            uuid pk
  provider      text         -- 'google' | 'apple'
  provider_sub  text         -- subject claim from provider (unique per provider)
  email         text
  display_name  text
  avatar_url    text null
  created_at    timestamptz
  UNIQUE(provider, provider_sub)

groups
  id            uuid pk
  name          text
  invite_code   text UNIQUE  -- short code/slug for joining
  created_by    uuid -> users.id
  created_at    timestamptz

group_members
  group_id      uuid -> groups.id
  user_id       uuid -> users.id
  role          text         -- 'admin' | 'member'
  joined_at     timestamptz
  PRIMARY KEY (group_id, user_id)

concerts
  id            uuid pk
  group_id      uuid -> groups.id
  artist        text
  event_at      timestamptz  -- date/time of the concert
  venue         text null
  city          text null
  ticket_url    text null
  price_cents   int null     -- price in cents (null = unknown)
  notes         text null
  rsvp_deadline timestamptz null  -- field present now; reminder job comes later
  created_by    uuid -> users.id
  created_at    timestamptz

rsvps
  concert_id    uuid -> concerts.id
  user_id       uuid -> users.id
  status        text         -- 'yes' | 'no'
  responded_at  timestamptz
  PRIMARY KEY (concert_id, user_id)
```

**Notes:**
- `invite_code` lives on the group → join by code.
- `role` on `group_members` → admin/member distinction.
- Future payment table will be `concert_payments (concert_id, user_id, status open|reported|confirmed, …)`,
  attaching to `concerts`/`users` with no restructuring.

> Open decision for the plan phase: although the RSVP deadline is a required *product* concept,
> `rsvp_deadline` is modeled as nullable to keep the column future-proof. The API will enforce
> presence on concert creation; revisit making the column `NOT NULL` during planning.

---

## 5. Authentication Flow

```
1. Client starts sign-in with Google or Apple → receives an ID token (JWT) from the provider.
2. Client POSTs the ID token to  POST /auth/{google|apple}.
3. Backend:
     - fetches the provider JWKS (cached), verifies the signature, checks iss/aud/exp
     - upserts the user by (provider, provider_sub)
     - signs its own session JWT → sets it as an httpOnly, Secure, SameSite=Lax cookie
4. Subsequent requests: the cookie is sent automatically; middleware validates the session JWT.
5. POST /auth/logout clears the cookie.
```

- **Why a backend session JWT instead of forwarding the provider token:** the backend stays
  independent of the provider token lifecycle, sessions are uniform, and middleware is simple.
- **Signing key** is a Kubernetes Secret. Cookie expiry e.g. 30 days with silent renewal.
- Auth verification (signature, iss/aud/exp, expired/tampered tokens) is tested in isolation.

---

## 6. API Surface (REST/JSON)

| Method & path | Purpose | Auth |
|---|---|---|
| `POST /auth/google` · `POST /auth/apple` | ID token → session cookie | public |
| `POST /auth/logout` | end session | session |
| `GET /me` | own profile | session |
| `GET /groups` | my groups | session |
| `POST /groups` | create group (creator becomes `admin`) | session |
| `GET /groups/{id}` | group details (members only) | member |
| `GET /groups/{id}/members` | member list | member |
| `POST /groups/{id}/invite` | (re)generate / fetch invite code | admin |
| `POST /groups/join` | `{code}` → join | session |
| `GET /groups/{id}/concerts` | concerts in the group | member |
| `POST /groups/{id}/concerts` | create concert | member |
| `GET /concerts/{id}` | concert details + RSVP summary | member |
| `PUT /concerts/{id}/rsvp` | set/change `{status: yes\|no}` | member |
| `GET /concerts/{id}/rsvps` | who is coming | member |

### Authorization (cross-cutting)

- Middleware chain: `RequireSession` → `RequireGroupMember(groupID)` → optionally
  `RequireGroupAdmin`.
- Concert endpoints derive the group from the concert and check membership.

### Conventions

- Errors as JSON `{ "error": { "code": "...", "message": "..." } }` with appropriate HTTP
  status codes (400/401/403/404/409).
- Request validation at the handler boundary; domain logic in the service layer, separated from
  HTTP handlers and the store.

---

## 7. Frontend (React PWA)

- **Stack:** React + **Vite**, TypeScript, React Router, TanStack Query (server-state/caching),
  PWA via `vite-plugin-pwa` (Cycle 1: app shell / installability only; Web Push arrives in the
  notifications cycle).
- **Responsive:** mobile-first, with a breakpoint switch to a desktop layout for large screens.
- **Screens (core flow):**
  1. **Login** — Google / Apple buttons
  2. **Groups list** — my crews + "create group" + "join by code"
  3. **Group detail** — members + concert list + "create concert"; share invite code (admin)
  4. **Concert form** — artist, date/time, venue, city, ticket link, price, notes, deadline
  5. **Concert detail** — key facts + **Yes/No toggle** + "coming / not coming" lists
- **Styling:** intentionally lean in Cycle 1 (Tailwind or CSS modules); real visual design comes
  later via the `frontend-design` skill.
- **API:** cookie-based (`credentials: include`), thin typed API client.

---

## 8. Kubernetes Deployment

- **Containers:** Go binary in a multi-stage Dockerfile → **distroless** image. Frontend as
  static assets in an **nginx** container.
- **Manifests:** start with **Kustomize** (`base/` + `overlays/dev`); a Helm chart is optional
  later. Components:
  - `Deployment` + `Service` for the API, with **liveness/readiness probes** (`/healthz`,
    `/readyz`)
  - `Deployment` + `Service` for the frontend (nginx)
  - **Postgres** as a `StatefulSet` + `PVC` (dev); a prod overlay may point at a managed DB
  - `Ingress` → routes `/api` to the API, `/` to the frontend
  - `Secret` for OAuth client IDs/secrets, DB credentials, session signing key; `ConfigMap` for
    non-secret config
- **Migrations:** `golang-migrate`, run as a Kubernetes `Job` (or init container) before the API
  rollout.
- **Local development:** **kind** or minikube; optionally **Tilt/Skaffold** for hot reload
  (recommended, not required).

---

## 9. Test Strategy (TDD is mandatory)

Strict **Red → Green → Refactor** for every unit. The `superpowers:test-driven-development`
skill is wired into the implementation plan for every building block. No implementation code
lands without a failing test first.

- **Backend:**
  - **Unit tests** for service/domain logic (pure functions, no DB).
  - **Integration tests** against real Postgres via **testcontainers-go** (migrations + store
    queries + authorization rules).
  - **Handler/API tests** via `httptest`, including auth middleware (provider JWKS mocked).
- **Frontend:** **Vitest** + React Testing Library for components/flows; API mocked (MSW).
- **Auth verification** tested in isolation (token signature, iss/aud/exp, expired/tampered
  tokens).
- **CI:** runs `go test ./...` + frontend tests + lint on every push (details settled in the
  plan phase).

---

## 10. Build Order Within Cycle 1

The implementation plan will sequence the vertical slice roughly as:

1. Project scaffolding (Go module, migrations tooling, Postgres test harness, CI skeleton).
2. Auth: provider token verification + session issuance + middleware.
3. Groups: create / list / invite / join, with membership authorization.
4. Concerts: create / list / detail within a group.
5. RSVP: set/change status, who-is-coming view.
6. Minimal React PWA wiring each screen to the API.
7. Kubernetes manifests + Dockerfiles + local cluster bring-up.

Each step is built test-first.
