// backend/internal/httpapi/auth_handlers_test.go
package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/auth"
	"github.com/jaydee94/pit-pilot/backend/internal/httpapi"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func newAuthHandlers(t *testing.T) *httpapi.AuthHandlers {
	pool := testutil.NewPostgres(t)
	v := &auth.FakeVerifier{Identities: map[string]auth.Identity{
		"good": {Provider: "google", Subject: "s1", Email: "a@x.io", Name: "Ann"},
	}}
	return &httpapi.AuthHandlers{
		Users:    service.NewUserService(gen.New(pool), v),
		Sessions: auth.NewSessionManager([]byte("k"), time.Hour, nil),
	}
}

func TestLoginSetsCookie(t *testing.T) {
	h := newAuthHandlers(t)
	req := httptest.NewRequest(http.MethodPost, "/auth/google",
		strings.NewReader(`{"id_token":"good"}`))
	rec := httptest.NewRecorder()
	h.Login("google")(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	raw := rec.Header().Get("Set-Cookie")
	for _, want := range []string{"pp_session=", "HttpOnly", "SameSite=Lax"} {
		if !strings.Contains(raw, want) {
			t.Errorf("Set-Cookie missing %q; got %q", want, raw)
		}
	}
}

func TestLoginRejectsBadToken(t *testing.T) {
	h := newAuthHandlers(t)
	req := httptest.NewRequest(http.MethodPost, "/auth/google",
		strings.NewReader(`{"id_token":"nope"}`))
	rec := httptest.NewRecorder()
	h.Login("google")(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestSessionMiddlewareInjectsUser(t *testing.T) {
	h := newAuthHandlers(t)
	// login to obtain a cookie
	loginReq := httptest.NewRequest(http.MethodPost, "/auth/google", strings.NewReader(`{"id_token":"good"}`))
	loginRec := httptest.NewRecorder()
	h.Login("google")(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	var sawUser bool
	protected := h.SessionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := httpapi.UserID(r); ok {
			sawUser = true
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req = req.WithContext(context.Background())
	req.AddCookie(cookie)
	protected.ServeHTTP(httptest.NewRecorder(), req)
	if !sawUser {
		t.Fatal("expected user id injected into context")
	}
}

func TestSessionMiddlewareRejectsMissingCookie(t *testing.T) {
	h := newAuthHandlers(t)
	rec := httptest.NewRecorder()
	h.SessionMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
