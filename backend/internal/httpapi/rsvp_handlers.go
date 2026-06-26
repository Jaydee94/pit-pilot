// backend/internal/httpapi/rsvp_handlers.go
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
)

type RSVPHandlers struct {
	RSVPs *service.RSVPService
}

func (h *RSVPHandlers) Set(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := uuid.Parse(chi.URLParam(r, "concertID"))
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	var b struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "status required"))
		return
	}
	rsvp, err := h.RSVPs.Set(r.Context(), uid, cid, b.Status)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, rsvp)
}

func (h *RSVPHandlers) List(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := uuid.Parse(chi.URLParam(r, "concertID"))
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	rows, err := h.RSVPs.List(r.Context(), uid, cid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, rows)
}
