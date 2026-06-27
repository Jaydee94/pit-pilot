# pit-pilot — Real "Sign in with Google" Design

**Date:** 2026-06-27
**Status:** Approved design, pending implementation plan
**Scope:** Replace the placeholder login (dev paste-token field + non-functional provider buttons) with the real Google Sign-In flow as seen on other websites, on the existing Cycle 1 auth backend.

---

## 1. Vision & Scope

The Cycle 1 backend already verifies Google OIDC **ID tokens** (`POST /auth/google` → verify against Google's JWKS with the configured client ID as audience → issue the app's own `pp_session` session-JWT cookie). What is missing is the **browser-side Google Sign-In** that produces a real ID token; today the "Mit Google anmelden" button just submits whatever is typed into a dev textarea.

This feature wires up **Google Identity Services (GIS)** — the official "Sign in with Google" button plus One Tap — so a user signs in with their real Google account.

### Flow

> Login page mounts → fetches the public Google client ID from `GET /auth/config` → loads Google's GIS script → renders the official **Sign in with Google** button and triggers **One Tap** → user picks their account → the GIS callback receives the **ID token (`credential`)** → the frontend POSTs it to the existing `POST /auth/google` → the backend verifies it (unchanged) and sets the `pp_session` cookie → navigate to `/groups`.

### Explicitly out of scope

- **Apple Sign-In** (a separate later feature — the non-functional Apple button is removed).
- The OAuth authorization-code / redirect flow (GIS returns the ID token client-side, which the existing backend already consumes).
- **Nonce** replay protection (the backend already verifies signature / `aud` / `exp`; a nonce is a later hardening).
- The dev paste-token affordance is removed.

---

## 2. Key Decisions (settled in brainstorming)

- **Integration:** Google Identity Services (GIS) — official button **and** One Tap (button is the reliable fallback when One Tap is suppressed by the browser/FedCM).
- **Client-ID delivery:** a runtime public endpoint `GET /auth/config` (no rebuild per environment; the client ID is public). Not a build-time `VITE_*` var.
- **Login screen cleanup:** remove the dev paste-token textarea entirely and remove the non-functional Apple button. The screen becomes the Google button + One Tap.
- The same `cfg.GoogleClientID` is used by the GIS init (audience) and the backend verifier — they must match.

---

## 3. Backend

The backend already verifies ID tokens; the only addition is a **public** config endpoint so the frontend can obtain the client ID before login.

| Method & path | Purpose | Auth |
|---|---|---|
| `GET /auth/config` | returns `{"google_client_id": "<cfg.GoogleClientID>"}` | public (top-level, NOT under `/api`) |

- A new `AuthHandlers.Config` handler. `AuthHandlers` gains a `GoogleClientID string` field, wired in `cmd/api/main.go` from `cfg.GoogleClientID` (alongside the existing `CookieSecure`). It returns only the **public** client ID — never any secret.
- The route is registered at the top level next to `POST /auth/google` / `POST /auth/apple` (the user is not yet authenticated at login, so it cannot live under the session-gated `/api`).
- `POST /auth/google` is unchanged.

---

## 4. Frontend

`Login.tsx` is rebuilt; the dev textarea and the Apple button are removed.

- New client helper `getAuthConfig()` → `GET /auth/config` returning `{ google_client_id: string }`.
- A small, testable GIS wrapper module `src/auth/google.ts` encapsulates the Google interaction: dynamically load the GIS script (`https://accounts.google.com/gsi/client`) once, call `google.accounts.id.initialize({ client_id, callback })`, `renderButton(targetEl, ...)`, and `prompt()` (One Tap). A minimal ambient TypeScript declaration types `window.google.accounts.id` (no heavyweight dependency).
- On mount `Login.tsx` fetches the client ID:
  - **Empty client ID** → render a "Google-Login nicht konfiguriert" note instead of a broken button.
  - **Present** → initialize GIS, render the button into a ref'd `<div>`, and trigger One Tap.
- The GIS `callback({ credential })` calls the existing `login("google", credential)` client helper (which POSTs `{ id_token }`), then `refresh()` (AuthContext), then navigates to `/groups`. Errors are shown via the existing error display.

---

## 5. Configuration & Deployment

- `GOOGLE_CLIENT_ID` is already a config/env variable (`config.go`, the compose stack, and the Kustomize secret). Real login requires a real **OAuth Web client ID** from the Google Cloud Console, and that console entry must list the app origin under **Authorized JavaScript origins** (e.g. `http://localhost:8080` locally, the real domain in production). Creating the client ID is a manual Google Console task; the required steps are documented in `.env.example`.
- With `GOOGLE_CLIENT_ID` unset, `/auth/config` returns an empty string and the login screen shows the "not configured" note (no crash). The rest of the app runs normally.

---

## 6. Test Strategy (test-first)

- **Backend:** a handler test for `GET /auth/config` asserting it returns the configured client ID as JSON (and an empty string when unset). `POST /auth/google` is unchanged and already tested.
- **Frontend:**
  - `getAuthConfig()` client helper (fetch mocked).
  - `src/auth/google.ts`: tested with a mocked `window.google.accounts.id` — assert `initialize` is called with the client ID, and that invoking the supplied callback with a `credential` triggers the login handler.
  - `Login.tsx`: render tests — with a client ID present, GIS init / the button container is exercised; with an empty client ID, the "not configured" note renders. The external GIS script is not loaded in tests (`window.google` is injected directly).
  - The existing `App.test.tsx` / Login-related test is updated (no dev textarea).
- **CI** runs backend + frontend tests on every push.

---

## 7. Build Order

1. Backend: `AuthHandlers.Config` + the `GoogleClientID` field + the `GET /auth/config` route + main wiring (test-first).
2. Frontend: `getAuthConfig()` client helper.
3. Frontend: `src/auth/google.ts` GIS wrapper + ambient types.
4. Frontend: rebuild `Login.tsx` (Google button + One Tap; remove dev textarea + Apple button); update the affected test.
5. `.env.example`: document the Google Cloud Console setup (Web client ID + Authorized JavaScript origins).

Each step is built test-first.
