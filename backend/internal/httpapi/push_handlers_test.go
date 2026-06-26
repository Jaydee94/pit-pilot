// backend/internal/httpapi/push_handlers_test.go
package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/httpapi"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestSubscribeAndVapidKey(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	h := &httpapi.PushHandlers{Subs: service.NewSubscriptionService(q), VapidPublicKey: "PUBKEY"}

	keyRec := httptest.NewRecorder()
	h.VapidKey(keyRec, httptest.NewRequest(http.MethodGet, "/api/push/vapid-public-key", nil))
	if keyRec.Code != http.StatusOK || !strings.Contains(keyRec.Body.String(), "PUBKEY") {
		t.Fatalf("vapid key: %d %s", keyRec.Code, keyRec.Body.String())
	}

	u, _ := q.UpsertUser(httptest.NewRequest(http.MethodGet, "/", nil).Context(),
		gen.UpsertUserParams{Provider: "google", ProviderSub: "u", DisplayName: "U"})
	body := `{"endpoint":"https://push/x","keys":{"p256dh":"k","auth":"a"}}`
	req := httpapi.WithUserIDForTest(httptest.NewRequest(http.MethodPost, "/api/push/subscriptions", strings.NewReader(body)), u.ID)
	rec := httptest.NewRecorder()
	h.Subscribe(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("subscribe: %d %s", rec.Code, rec.Body.String())
	}
}
