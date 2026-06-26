// backend/internal/httpapi/concert_handlers.go
package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
)

type ConcertHandlers struct {
	Concerts *service.ConcertService
}

type concertBody struct {
	Artist       string `json:"artist"`
	EventAt      string `json:"event_at"`
	Venue        string `json:"venue"`
	City         string `json:"city"`
	TicketURL    string `json:"ticket_url"`
	PriceCents   *int32 `json:"price_cents"`
	Notes        string `json:"notes"`
	RSVPDeadline string `json:"rsvp_deadline"`
}

func (h *ConcertHandlers) Create(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	gid, err := uuid.Parse(chi.URLParam(r, "groupID"))
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad group id"))
		return
	}
	var b concertBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "malformed json"))
		return
	}
	eventAt, err1 := time.Parse(time.RFC3339, b.EventAt)
	deadline, err2 := time.Parse(time.RFC3339, b.RSVPDeadline)
	if err1 != nil || err2 != nil {
		WriteError(w, apperr.BadRequest("invalid_dates", "event_at and rsvp_deadline must be RFC3339"))
		return
	}
	c, err := h.Concerts.Create(r.Context(), uid, gid, service.ConcertInput{
		Artist: b.Artist, EventAt: eventAt, Venue: b.Venue, City: b.City,
		TicketURL: b.TicketURL, Notes: b.Notes, PriceCents: b.PriceCents, RSVPDeadline: deadline,
	})
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, c)
}

func (h *ConcertHandlers) ListForGroup(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	gid, err := uuid.Parse(chi.URLParam(r, "groupID"))
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad group id"))
		return
	}
	list, err := h.Concerts.ListForGroup(r.Context(), uid, gid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, list)
}

func (h *ConcertHandlers) Get(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := uuid.Parse(chi.URLParam(r, "concertID"))
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	c, err := h.Concerts.Get(r.Context(), uid, cid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, c)
}
