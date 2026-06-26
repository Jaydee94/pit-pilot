package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/httpapi"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

// concertReq builds a request carrying userID + a chi {concertID} param.
func concertReq(method, body string, userID, concertID uuid.UUID) *http.Request {
	r := httpapi.WithUserIDForTest(httptest.NewRequest(method, "/p", strings.NewReader(body)), userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("concertID", concertID.String())
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func paymentTestStack(t *testing.T) (*httpapi.PaymentHandlers, *gen.Queries, gen.Concert, gen.User) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "PH" + string(rune('A'+n)) })
	cs := service.NewConcertService(q, gs)
	ps := service.NewPaymentService(pool, q, cs)
	ctx := context.Background()
	owner, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "o", DisplayName: "O"})
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: owner.ID, Status: "yes"})
	return &httpapi.PaymentHandlers{Payments: ps}, q, c, owner
}

func TestActivateAndGetWithLink(t *testing.T) {
	h, _, c, owner := paymentTestStack(t)
	rec := httptest.NewRecorder()
	h.Activate(rec, concertReq(http.MethodPost, `{"default_amount_cents":4500,"payment_link":"https://paypal.me/o/45"}`, owner.ID, c.ID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("activate status %d body %s", rec.Code, rec.Body.String())
	}
	getRec := httptest.NewRecorder()
	h.Get(getRec, concertReq(http.MethodGet, "", owner.ID, c.ID))
	if getRec.Code != http.StatusOK ||
		!strings.Contains(getRec.Body.String(), "paypal.me") ||
		!strings.Contains(getRec.Body.String(), `"is_responsible":true`) {
		t.Fatalf("get failed: %d %s", getRec.Code, getRec.Body.String())
	}
}
