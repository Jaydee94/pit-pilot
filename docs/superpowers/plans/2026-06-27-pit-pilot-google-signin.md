# Real "Sign in with Google" Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the placeholder login with the real Google Sign-In flow (Google Identity Services official button + One Tap) against the existing ID-token-verifying backend.

**Architecture:** Backend gains one public endpoint `GET /auth/config` returning the public Google client ID. The frontend fetches it, loads Google Identity Services (GIS), renders the official button + One Tap, and POSTs the returned ID token to the existing `POST /auth/google` (unchanged). The dev paste-token field and the non-functional Apple button are removed.

**Tech Stack:** Go 1.26 (chi). React 19 + Vite + TypeScript + Vitest. Google Identity Services (`https://accounts.google.com/gsi/client`, no npm dependency — a minimal ambient type declaration).

## Global Constraints

- Go module `github.com/jaydee94/pit-pilot/backend`, Go floor 1.26. `GET /auth/config` is PUBLIC (top-level, NOT under the session-gated `/api`) and returns ONLY the public client ID — never a secret.
- The same `cfg.GoogleClientID` is used by the GIS init (as audience) and the backend verifier; they must match.
- With `GOOGLE_CLIENT_ID` unset, `/auth/config` returns an empty string and the login screen shows a "not configured" note (no crash).
- Strict TDD: failing test first, then implement. Backend tests that need a DB use testcontainers (Docker); the `/auth/config` handler needs no DB. The frontend build (`npm run build` = `tsc -b && vite build`) type-checks test files, so tests must compile.
- Layer discipline: handlers parse + `WriteJSON`/`WriteError`, no SQL. Login copy stays German.

## Existing code this builds on

- `backend/internal/httpapi/auth_handlers.go` — `type AuthHandlers struct { Users *service.UserService; Sessions *auth.SessionManager; CookieSecure bool }`; `Login(provider)`/`Logout`/`Me`. Add a `Config` method + a `GoogleClientID string` field.
- `backend/internal/httpapi/router.go` — top-level `if d.Auth != nil { r.Post("/auth/google", ...); r.Post("/auth/apple", ...) }`. Add the public `r.Get("/auth/config", d.Auth.Config)` there.
- `backend/cmd/api/main.go` — builds `&httpapi.AuthHandlers{Users: users, Sessions: sessions, CookieSecure: cfg.CookieSecure}`. Add `GoogleClientID: cfg.GoogleClientID`.
- `frontend/src/api/client.ts` — `apiFetch`, `login(provider, idToken)` (POSTs `{id_token}` to `/auth/${provider}`).
- `frontend/src/routes/Login.tsx` — current placeholder (dev textarea + provider buttons). `frontend/src/auth/AuthContext.tsx` — `useAuth()` exposing `refresh()`.

---

## Task 1: Backend `GET /auth/config` endpoint

**Files:**
- Modify: `backend/internal/httpapi/auth_handlers.go`
- Modify: `backend/internal/httpapi/router.go`
- Modify: `backend/cmd/api/main.go`
- Test: `backend/internal/httpapi/auth_handlers_test.go` (add a test)

**Interfaces:**
- Produces: `AuthHandlers` gains `GoogleClientID string`; `func (a *AuthHandlers) Config(w http.ResponseWriter, r *http.Request)` → `200 {"google_client_id": "<id>"}`. Route `GET /auth/config` (public).

- [ ] **Step 1: Write the failing test** (append to `auth_handlers_test.go`)

```go
func TestAuthConfigReturnsClientID(t *testing.T) {
	h := &httpapi.AuthHandlers{GoogleClientID: "test-client-id.apps.googleusercontent.com"}
	rec := httptest.NewRecorder()
	h.Config(rec, httptest.NewRequest(http.MethodGet, "/auth/config", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "test-client-id.apps.googleusercontent.com") {
		t.Fatalf("config: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"google_client_id"`) {
		t.Fatalf("missing field: %s", rec.Body.String())
	}
}

func TestAuthConfigEmptyWhenUnset(t *testing.T) {
	h := &httpapi.AuthHandlers{}
	rec := httptest.NewRecorder()
	h.Config(rec, httptest.NewRequest(http.MethodGet, "/auth/config", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"google_client_id":""`) {
		t.Fatalf("expected empty client id, got %s", rec.Body.String())
	}
}
```
> Ensure `net/http`, `net/http/httptest`, `strings`, and the `httpapi` import are present in the test file (the existing auth tests already use them).

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/httpapi/ -run AuthConfig
```
Expected: FAIL — `h.Config undefined` / `GoogleClientID` not a field.

- [ ] **Step 3: Write minimal implementation**

In `auth_handlers.go`, add the field and the handler:
```go
// in the AuthHandlers struct, after CookieSecure:
	GoogleClientID string
```
```go
// new method:
func (a *AuthHandlers) Config(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"google_client_id": a.GoogleClientID})
}
```
In `router.go`, inside the existing top-level `if d.Auth != nil { ... }` block (next to the `/auth/google` + `/auth/apple` posts), add:
```go
		r.Get("/auth/config", d.Auth.Config)
```
In `cmd/api/main.go`, add the field to the `AuthHandlers` literal:
```go
		Auth: &httpapi.AuthHandlers{Users: users, Sessions: sessions, CookieSecure: cfg.CookieSecure, GoogleClientID: cfg.GoogleClientID},
```

- [ ] **Step 4: Run test + build to verify it passes**

```bash
cd backend && go test ./internal/httpapi/ -run AuthConfig && go build ./cmd/api && go vet ./... && go test ./...
```
Expected: PASS, builds, vet clean, full module green.

- [ ] **Step 5: Commit**

```bash
git add backend && git commit -m "feat: public GET /auth/config returning the google client id"
```

---

## Task 2: Frontend `getAuthConfig` client helper

**Files:**
- Modify: `frontend/src/api/client.ts`
- Test: `frontend/src/api/authconfig.test.ts`

**Interfaces:**
- Produces: `getAuthConfig()` → `Promise<{ google_client_id: string }>` (GET `/auth/config`).

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/api/authconfig.test.ts
import { afterEach, expect, test, vi } from "vitest";
import { getAuthConfig } from "./client";

afterEach(() => vi.restoreAllMocks());

test("getAuthConfig returns the google client id", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify({ google_client_id: "gid.apps.googleusercontent.com" }), { status: 200 })));
  expect((await getAuthConfig()).google_client_id).toBe("gid.apps.googleusercontent.com");
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd frontend && npm run test -- --run authconfig
```
Expected: FAIL — `getAuthConfig` not exported.

- [ ] **Step 3: Append to `client.ts`**

```ts
export const getAuthConfig = () => apiFetch<{ google_client_id: string }>("/auth/config");
```

- [ ] **Step 4: Run test + build**

```bash
cd frontend && npm run test -- --run authconfig && npm run build
```
Expected: PASS + build clean.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api && git commit -m "feat: getAuthConfig client helper"
```

---

## Task 3: GIS wrapper module + ambient types

**Files:**
- Create: `frontend/src/auth/google.ts`
- Test: `frontend/src/auth/google.test.ts`

**Interfaces:**
- Produces: `async function initGoogleSignIn(opts: { clientId: string; buttonParent: HTMLElement; onCredential: (idToken: string) => void }): Promise<void>` — loads GIS, calls `google.accounts.id.initialize({client_id, callback})`, `renderButton(buttonParent, …)`, `prompt()`. The `callback` extracts `credential` (the ID token) and calls `onCredential`.
- A minimal ambient declaration of `window.google.accounts.id`.

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/auth/google.test.ts
import { afterEach, expect, test, vi } from "vitest";
import { initGoogleSignIn } from "./google";

afterEach(() => {
  vi.restoreAllMocks();
  // @ts-expect-error cleanup injected global
  delete (window as unknown as { google?: unknown }).google;
});

test("initializes GIS and forwards the credential", async () => {
  const initialize = vi.fn();
  const renderButton = vi.fn();
  const prompt = vi.fn();
  (window as unknown as { google: unknown }).google = { accounts: { id: { initialize, renderButton, prompt } } };

  const received: string[] = [];
  const parent = document.createElement("div");
  await initGoogleSignIn({ clientId: "gid", buttonParent: parent, onCredential: (t) => received.push(t) });

  expect(initialize).toHaveBeenCalledTimes(1);
  const cfg = initialize.mock.calls[0]?.[0] as { client_id: string; callback: (r: { credential: string }) => void };
  expect(cfg.client_id).toBe("gid");
  expect(renderButton).toHaveBeenCalledWith(parent, expect.any(Object));
  expect(prompt).toHaveBeenCalled();

  cfg.callback({ credential: "id-token-123" });
  expect(received).toEqual(["id-token-123"]);
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd frontend && npm run test -- --run auth/google
```
Expected: FAIL — cannot find `./google`.

- [ ] **Step 3: Write the implementation**

```ts
// frontend/src/auth/google.ts
type GoogleCredentialResponse = { credential: string };

interface GoogleIdApi {
  initialize(cfg: { client_id: string; callback: (resp: GoogleCredentialResponse) => void }): void;
  renderButton(parent: HTMLElement, options: Record<string, unknown>): void;
  prompt(): void;
}

declare global {
  interface Window {
    google?: { accounts: { id: GoogleIdApi } };
  }
}

const GIS_SRC = "https://accounts.google.com/gsi/client";

function loadGisScript(): Promise<void> {
  if (window.google?.accounts?.id) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const existing = document.querySelector<HTMLScriptElement>(`script[src="${GIS_SRC}"]`);
    if (existing) {
      existing.addEventListener("load", () => resolve());
      existing.addEventListener("error", () => reject(new Error("failed to load Google Identity Services")));
      return;
    }
    const s = document.createElement("script");
    s.src = GIS_SRC;
    s.async = true;
    s.defer = true;
    s.onload = () => resolve();
    s.onerror = () => reject(new Error("failed to load Google Identity Services"));
    document.head.appendChild(s);
  });
}

export async function initGoogleSignIn(opts: {
  clientId: string;
  buttonParent: HTMLElement;
  onCredential: (idToken: string) => void;
}): Promise<void> {
  await loadGisScript();
  const id = window.google?.accounts?.id;
  if (!id) throw new Error("Google Identity Services unavailable");
  id.initialize({
    client_id: opts.clientId,
    callback: (resp) => opts.onCredential(resp.credential),
  });
  id.renderButton(opts.buttonParent, { type: "standard", theme: "outline", size: "large", text: "signin_with" });
  id.prompt();
}
```

- [ ] **Step 4: Run test + build**

```bash
cd frontend && npm run test -- --run auth/google && npm run build
```
Expected: PASS + build clean (the `declare global` augments the `Window` type for `tsc -b`).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/auth/google.ts frontend/src/auth/google.test.ts && git commit -m "feat: Google Identity Services wrapper"
```

---

## Task 4: Rebuild the Login screen

**Files:**
- Modify: `frontend/src/routes/Login.tsx`
- Create: `frontend/src/routes/Login.test.tsx`
- Modify (if needed): `frontend/src/App.test.tsx`

**Interfaces:**
- Consumes: `getAuthConfig`, `login` (client), `initGoogleSignIn` (Task 3), `useAuth().refresh`, `useNavigate`.
- Produces: a Login screen that, on mount, fetches the client ID, then (if present) initializes GIS into a ref'd `<div>`; on credential, calls `login("google", credential)` → `refresh()` → navigate `/groups`. If the client ID is empty, shows a "nicht konfiguriert" note. No dev textarea, no Apple button.

- [ ] **Step 1: Write the failing tests**

```tsx
// frontend/src/routes/Login.test.tsx
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, test, vi } from "vitest";
import Login from "./Login";
import { AuthProvider } from "../auth/AuthContext";

afterEach(() => {
  vi.restoreAllMocks();
  // @ts-expect-error cleanup
  delete (window as unknown as { google?: unknown }).google;
});

function wrap() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <AuthProvider>
        <MemoryRouter><Login /></MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>
  );
}

test("initializes the Google button when a client id is configured", async () => {
  const initialize = vi.fn();
  (window as unknown as { google: unknown }).google = {
    accounts: { id: { initialize, renderButton: vi.fn(), prompt: vi.fn() } },
  };
  // AuthProvider calls me() on mount, then Login calls /auth/config
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (String(url).endsWith("/auth/config")) {
      return new Response(JSON.stringify({ google_client_id: "gid" }), { status: 200 });
    }
    return new Response(JSON.stringify({ error: { code: "x", message: "x" } }), { status: 401 });
  }));
  render(wrap());
  await waitFor(() => expect(initialize).toHaveBeenCalled());
});

test("shows a not-configured note when the client id is empty", async () => {
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (String(url).endsWith("/auth/config")) {
      return new Response(JSON.stringify({ google_client_id: "" }), { status: 200 });
    }
    return new Response(JSON.stringify({ error: { code: "x", message: "x" } }), { status: 401 });
  }));
  render(wrap());
  await waitFor(() => expect(screen.getByText(/nicht konfiguriert/i)).toBeInTheDocument());
});
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd frontend && npm run test -- --run Login
```
Expected: FAIL — the current Login has no `/auth/config` fetch / GIS init / not-configured note.

- [ ] **Step 3: Rewrite `Login.tsx`**

```tsx
// frontend/src/routes/Login.tsx
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { login, getAuthConfig } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { initGoogleSignIn } from "../auth/google";

export default function Login() {
  const [clientId, setClientId] = useState<string | null>(null);
  const [configured, setConfigured] = useState<boolean | null>(null);
  const [err, setErr] = useState("");
  const btnRef = useRef<HTMLDivElement>(null);
  const nav = useNavigate();
  const { refresh } = useAuth();

  useEffect(() => {
    getAuthConfig()
      .then((c) => {
        setClientId(c.google_client_id || null);
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
          setErr((e as { message?: string })?.message ?? "Login fehlgeschlagen");
        }
      },
    }).catch(() => setErr("Google-Login konnte nicht geladen werden"));
  }, [clientId, refresh, nav]);

  return (
    <main>
      <h1>pit-pilot</h1>
      {configured === false && <p>Google-Login ist nicht konfiguriert.</p>}
      <div ref={btnRef} />
      {err && <p role="alert">{err}</p>}
    </main>
  );
}
```

- [ ] **Step 4: Keep `App.test.tsx` green**

`App.test.tsx` renders `<Login/>` and asserts the "pit-pilot" heading. With the rewrite, Login mounts and calls `getAuthConfig()`; the existing App.test stubs `fetch` to 401, so `getAuthConfig` rejects → `configured=false` → the heading + not-configured note render and no GIS init runs (no `window.google`). Run it:
```bash
cd frontend && npm run test -- --run App
```
Expected: PASS. If it fails because the heading assertion changed, leave the heading assertion as-is — the rewritten Login still renders `<h1>pit-pilot</h1>`, so no edit should be needed; only edit App.test.tsx if a concrete failure shows otherwise.

- [ ] **Step 5: Run full suite + build**

```bash
cd frontend && npm run test -- --run && npm run build
```
Expected: all tests PASS, `tsc -b` + vite build clean.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/routes/Login.tsx frontend/src/routes/Login.test.tsx frontend/src/App.test.tsx && git commit -m "feat: real Sign in with Google on the login screen"
```

---

## Task 5: Document the Google Cloud Console setup

**Files:**
- Modify: `.env.example`

- [ ] **Step 1: Append the setup notes**

Add to `.env.example`, replacing the existing `GOOGLE_CLIENT_ID=` line's comment block with:
```dotenv
# OAuth — REQUIRED for real "Sign in with Google".
# Create an OAuth 2.0 "Web application" client in the Google Cloud Console
# (APIs & Services -> Credentials), and under "Authorized JavaScript origins"
# add the app origin(s): http://localhost:8080 for the local compose stack,
# plus your real domain in production. Paste the generated client ID here.
# The same value is used by the browser (GIS) and the backend verifier.
# The api also contacts Google OIDC discovery at startup, so it needs internet.
GOOGLE_CLIENT_ID=
APPLE_CLIENT_ID=
```
(Keep the rest of `.env.example` — SESSION_SIGNING_KEY, VAPID_* — unchanged.)

- [ ] **Step 2: Commit**

```bash
git add .env.example && git commit -m "docs: Google Cloud Console setup for Sign in with Google"
```

---

## Self-Review

**Spec coverage (each spec section → task):**
- §3 backend `/auth/config` + `GoogleClientID` field + route + wiring → Task 1.
- §4 frontend: `getAuthConfig` → Task 2; GIS wrapper + ambient types → Task 3; Login rebuild (button + One Tap, remove dev textarea + Apple button, not-configured note) → Task 4.
- §5 config/deploy docs → Task 5.
- §6 tests → backend handler test (Task 1), `getAuthConfig` test (Task 2), `google.ts` test (Task 3), Login tests + App.test kept green (Task 4).
- §1 flow / §2 decisions (GIS, public endpoint, same client id, cleanup) → realized across Tasks 1–4.

**Resolved during planning:**
- The GIS script is never loaded in jsdom tests — `window.google` is injected directly, and `loadGisScript` resolves immediately when `window.google` is already present.
- No npm dependency for GIS — a `declare global` ambient type in `google.ts` satisfies `tsc -b`.

**Known manual step (out of code scope):** creating the real Google OAuth Web client ID + Authorized JavaScript origins in the Google Cloud Console — documented in `.env.example` (Task 5). Without it, the login shows the "not configured" note and the rest of the app works.
