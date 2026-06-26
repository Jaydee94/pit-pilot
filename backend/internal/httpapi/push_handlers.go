// backend/internal/httpapi/push_handlers.go
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
)

type PushHandlers struct {
	Subs           *service.SubscriptionService
	VapidPublicKey string
}

func (h *PushHandlers) VapidKey(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"public_key": h.VapidPublicKey})
}

func (h *PushHandlers) Subscribe(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	var b struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "malformed json"))
		return
	}
	sub, err := h.Subs.Save(r.Context(), uid, b.Endpoint, b.Keys.P256dh, b.Keys.Auth)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{"id": sub.ID})
}

func (h *PushHandlers) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	var b struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "malformed json"))
		return
	}
	if err := h.Subs.Delete(r.Context(), uid, b.Endpoint); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
