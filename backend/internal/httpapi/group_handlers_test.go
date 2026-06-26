// backend/internal/httpapi/group_handlers_test.go
package httpapi_test

import (
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
	_ = chi.NewRouter
}
