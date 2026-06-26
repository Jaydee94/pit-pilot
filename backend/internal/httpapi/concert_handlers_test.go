// backend/internal/httpapi/concert_handlers_test.go
package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jaydee94/pit-pilot/backend/internal/httpapi"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestCreateConcertHandler(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "C" + string(rune('A'+n)) })
	cs := service.NewConcertService(q, gs)
	h := &httpapi.ConcertHandlers{Concerts: cs}

	owner, _ := q.UpsertUser(context.Background(), gen.UpsertUserParams{Provider: "google", ProviderSub: "o", DisplayName: "O"})
	g, _ := gs.Create(context.Background(), owner.ID, "Crew")

	body := `{"artist":"Tool","event_at":"2026-09-01T20:00:00Z","rsvp_deadline":"2026-08-20T20:00:00Z"}`
	r := httpapi.WithUserIDForTest(httptest.NewRequest(http.MethodPost, "/groups/x/concerts", strings.NewReader(body)), owner.ID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("groupID", g.ID.String())
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.Create(rec, r)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), "Tool") {
		t.Fatalf("create concert failed: %d %s", rec.Code, rec.Body.String())
	}
}
