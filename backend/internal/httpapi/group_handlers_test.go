// backend/internal/httpapi/group_handlers_test.go
package httpapi_test

import (
	"context"
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
}

func TestRequireGroupMember(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	codes := []string{"AAA111", "BBB222"}
	i := 0
	gs := service.NewGroupService(pool, q, func() string { c := codes[i%len(codes)]; i++; return c })

	ctx := context.Background()
	owner, err := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "owner-rgm", DisplayName: "Owner"})
	if err != nil {
		t.Fatalf("upsert owner: %v", err)
	}
	nonMember, err := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "stranger-rgm", DisplayName: "Stranger"})
	if err != nil {
		t.Fatalf("upsert non-member: %v", err)
	}

	g, err := gs.Create(ctx, owner.ID, "MiddlewareTestGroup")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	r := chi.NewRouter()
	r.With(httpapi.RequireGroupMember(gs)).Get("/groups/{groupID}/x",
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	// 401: no session user in context
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/groups/"+g.ID.String()+"/x", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("401 case: got %d, want 401 (body: %s)", rec.Code, rec.Body.String())
	}

	// 400: invalid UUID in path (valid session user injected)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/groups/not-a-uuid/x", nil)
	req = httpapi.WithUserIDForTest(req, owner.ID)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("400 case: got %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}

	// 403: valid session user who is NOT a member
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/groups/"+g.ID.String()+"/x", nil)
	req = httpapi.WithUserIDForTest(req, nonMember.ID)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("403 case: got %d, want 403 (body: %s)", rec.Code, rec.Body.String())
	}

	// 200: valid session user who IS a member (owner)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/groups/"+g.ID.String()+"/x", nil)
	req = httpapi.WithUserIDForTest(req, owner.ID)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("200 case: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestGetGroupHandler(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	codes := []string{"INV001", "INV002"}
	i := 0
	gs := service.NewGroupService(pool, q, func() string { c := codes[i%len(codes)]; i++; return c })
	h := &httpapi.GroupHandlers{Groups: gs}

	ctx := context.Background()
	owner, err := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "owner-get-h", DisplayName: "Owner"})
	if err != nil {
		t.Fatalf("upsert owner: %v", err)
	}
	nonMember, err := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "stranger-get-h", DisplayName: "Stranger"})
	if err != nil {
		t.Fatalf("upsert non-member: %v", err)
	}
	g, err := gs.Create(ctx, owner.ID, "TestGetGroup")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	buildReq := func(userID uuid.UUID) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/groups/"+g.ID.String(), nil)
		req = httpapi.WithUserIDForTest(req, userID)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("groupID", g.ID.String())
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		return req
	}

	// owner gets 200 with invite_code in body
	rec := httptest.NewRecorder()
	h.Get(rec, buildReq(owner.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("owner Get: got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invite_code") {
		t.Fatalf("owner Get: response missing invite_code field: %s", rec.Body.String())
	}

	// non-member gets 403
	rec = httptest.NewRecorder()
	h.Get(rec, buildReq(nonMember.ID))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-member Get: got %d, want 403 body=%s", rec.Code, rec.Body.String())
	}
}
