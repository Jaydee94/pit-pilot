// backend/internal/httpapi/rsvp_handlers_test.go
package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jaydee94/pit-pilot/backend/internal/httpapi"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestSetRSVPHandler(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "H" + string(rune('A'+n)) })
	cs := service.NewConcertService(q, gs)
	rs := service.NewRSVPService(q, cs)
	h := &httpapi.RSVPHandlers{RSVPs: rs}

	owner, _ := q.UpsertUser(context.Background(), gen.UpsertUserParams{Provider: "google", ProviderSub: "o", DisplayName: "O"})
	g, _ := gs.Create(context.Background(), owner.ID, "Crew")
	when := mustTime("2026-09-01T20:00:00Z")
	c, _ := cs.Create(context.Background(), owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})

	r := httpapi.WithUserIDForTest(httptest.NewRequest(http.MethodPut, "/concerts/x/rsvp", strings.NewReader(`{"status":"yes"}`)), owner.ID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("concertID", c.ID.String())
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.Set(rec, r)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "yes") {
		t.Fatalf("set rsvp failed: %d %s", rec.Code, rec.Body.String())
	}
}
