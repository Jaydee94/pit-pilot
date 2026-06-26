# Backend Hardening Report

Date: 2026-06-26

## Fix 1 — GET /api/groups/{groupID}

### Service layer (`internal/service/groups.go`)
Added `GroupService.Get(ctx, userID, groupID)` which:
1. Calls `RequireMembership` to enforce membership (returns 403 for non-members)
2. Calls `q.GetGroup` — maps `pgx.ErrNoRows` to 404

### Handler layer (`internal/httpapi/group_handlers.go`)
Added `GroupHandlers.Get` which:
1. Parses `chi.URLParam(r, "groupID")` — bad UUID → 400
2. Calls `h.Groups.Get` — errors forwarded via `WriteError`
3. Success → `WriteJSON(w, 200, g)`

### Router (`internal/httpapi/router.go`)
Registered `gr.Get("/", d.Groups.Get)` inside the `RequireGroupMember`-protected `/api/groups/{groupID}` subrouter.

### TDD RED/GREEN

**RED:**
```
internal/service/groups_test.go:122:18: svc.Get undefined
internal/service/groups_test.go:131:15: svc.Get undefined
internal/httpapi/group_handlers_test.go:145:4: h.Get undefined
```

**GREEN:**
```
--- PASS: TestGroupServiceGet (1.12s)
--- PASS: TestGetGroupHandler (1.17s)
```

---

## Fix 2 — /readyz must check DB

### Router (`internal/httpapi/router.go`)
- Added `Ready func(ctx context.Context) error` field to `Deps` struct
- Updated `/readyz` handler: calls `d.Ready(r.Context())` if non-nil; on error returns 503 with `{"error":{"code":"not_ready","message":"database unavailable"}}`; otherwise 200

### main.go (`cmd/api/main.go`)
- Added `pool.Ping(ctx)` startup check after `pgxpool.New` — fatal on failure
- Passed `Ready: func(c context.Context) error { return pool.Ping(c) }` in `Deps{}`

### TDD RED/GREEN

**RED:**
```
internal/httpapi/router_test.go:36:3: unknown field Ready in struct literal of type httpapi.Deps
```

**GREEN:**
```
--- PASS: TestReadyzReportsDBFailure (0.00s)
--- PASS: TestHealthEndpoints (0.00s)  (still passes; nil Ready → 200)
```

---

## Fix 3 — HTTP server timeouts

### main.go (`cmd/api/main.go`)
Replaced bare `http.ListenAndServe` with a configured `*http.Server`:
- `ReadHeaderTimeout: 10s`
- `ReadTimeout:       15s`
- `WriteTimeout:      30s`
- `IdleTimeout:       60s`

No test needed (unit-testable only via integration/load test); verified via `go vet` and `go build`.

---

## Full `go test ./...` result

```
?   github.com/jaydee94/pit-pilot/backend/cmd/api   [no test files]
ok  github.com/jaydee94/pit-pilot/backend/internal/apperr    (cached)
ok  github.com/jaydee94/pit-pilot/backend/internal/auth      (cached)
ok  github.com/jaydee94/pit-pilot/backend/internal/config    (cached)
ok  github.com/jaydee94/pit-pilot/backend/internal/httpapi   10.546s
ok  github.com/jaydee94/pit-pilot/backend/internal/service   20.610s
ok  github.com/jaydee94/pit-pilot/backend/internal/store/gen (cached)
ok  github.com/jaydee94/pit-pilot/backend/internal/testutil  (cached)
ok  github.com/jaydee94/pit-pilot/backend/internal/version   (cached)
```

## `go vet ./... && go build ./cmd/api`

```
vet+build OK
```

All packages pass vet with zero warnings. Binary builds successfully (17 MB). Stray `api` binary removed.
