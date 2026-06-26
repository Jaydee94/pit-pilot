// backend/internal/httpapi/router_test.go
package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/httpapi"
)

func TestHealthEndpoints(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Deps{})
	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s returned %d", path, rec.Code)
		}
	}
}

func TestProtectedRouteRequiresSession(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Deps{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/groups", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without session, got %d", rec.Code)
	}
}
