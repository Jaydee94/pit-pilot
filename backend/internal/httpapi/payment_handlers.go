package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type PaymentHandlers struct {
	Payments *service.PaymentService
}

func concertID(r *http.Request) (uuid.UUID, error) { return uuid.Parse(chi.URLParam(r, "concertID")) }
func itemID(r *http.Request) (uuid.UUID, error)    { return uuid.Parse(chi.URLParam(r, "itemID")) }

func (h *PaymentHandlers) Activate(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	var b struct {
		DefaultAmountCents int32   `json:"default_amount_cents"`
		PaymentLink        *string `json:"payment_link"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "malformed json"))
		return
	}
	col, err := h.Payments.Activate(r.Context(), uid, cid, b.DefaultAmountCents, b.PaymentLink)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, col)
}

func (h *PaymentHandlers) Get(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	view, err := h.Payments.Get(r.Context(), uid, cid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, view)
}

func (h *PaymentHandlers) SetLink(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	var b struct {
		PaymentLink *string `json:"payment_link"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "malformed json"))
		return
	}
	col, err := h.Payments.SetPaymentLink(r.Context(), uid, cid, b.PaymentLink)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, col)
}

func (h *PaymentHandlers) Deactivate(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	if err := h.Payments.Deactivate(r.Context(), uid, cid); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *PaymentHandlers) AddItem(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad concert id"))
		return
	}
	var b struct {
		UserID      string `json:"user_id"`
		AmountCents *int32 `json:"amount_cents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "malformed json"))
		return
	}
	target, err := uuid.Parse(b.UserID)
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_user", "bad user id"))
		return
	}
	row, err := h.Payments.AddItem(r.Context(), uid, cid, target, b.AmountCents)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, row)
}

func (h *PaymentHandlers) SetAmount(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	iid, err2 := itemID(r)
	if err != nil || err2 != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad id"))
		return
	}
	var b struct {
		AmountCents int32 `json:"amount_cents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "malformed json"))
		return
	}
	item, err := h.Payments.SetAmount(r.Context(), uid, cid, iid, b.AmountCents)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, item)
}

func (h *PaymentHandlers) RemoveItem(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	iid, err2 := itemID(r)
	if err != nil || err2 != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad id"))
		return
	}
	if err := h.Payments.RemoveItem(r.Context(), uid, cid, iid); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// The four state-machine transitions share parsing + response shape, so they
// delegate to a small typed helper.
func (h *PaymentHandlers) Report(w http.ResponseWriter, r *http.Request)    { h.transition(w, r, h.Payments.Report) }
func (h *PaymentHandlers) UnReport(w http.ResponseWriter, r *http.Request)  { h.transition(w, r, h.Payments.UnReport) }
func (h *PaymentHandlers) Confirm(w http.ResponseWriter, r *http.Request)   { h.transition(w, r, h.Payments.Confirm) }
func (h *PaymentHandlers) UnConfirm(w http.ResponseWriter, r *http.Request) { h.transition(w, r, h.Payments.UnConfirm) }

func (h *PaymentHandlers) transition(w http.ResponseWriter, r *http.Request,
	fn func(ctx context.Context, uid, cid, iid uuid.UUID) (gen.PaymentItem, error)) {
	uid, _ := UserID(r)
	cid, err := concertID(r)
	iid, err2 := itemID(r)
	if err != nil || err2 != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad id"))
		return
	}
	item, err := fn(r.Context(), uid, cid, iid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, item)
}
