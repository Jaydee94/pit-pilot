// backend/internal/httpapi/group_handlers.go
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
)

type GroupHandlers struct {
	Groups *service.GroupService
}

func (h *GroupHandlers) Create(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "name required"))
		return
	}
	g, err := h.Groups.Create(r.Context(), uid, body.Name)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, g)
}

func (h *GroupHandlers) List(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	groups, err := h.Groups.ListForUser(r.Context(), uid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, groups)
}

func (h *GroupHandlers) Join(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "code required"))
		return
	}
	g, err := h.Groups.Join(r.Context(), uid, body.Code)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, g)
}

func (h *GroupHandlers) Members(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	gid, err := uuid.Parse(chi.URLParam(r, "groupID"))
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad group id"))
		return
	}
	members, err := h.Groups.Members(r.Context(), uid, gid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, members)
}

func (h *GroupHandlers) Invite(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	gid, err := uuid.Parse(chi.URLParam(r, "groupID"))
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad group id"))
		return
	}
	g, err := h.Groups.RegenerateInvite(r.Context(), uid, gid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"invite_code": g.InviteCode})
}

func (h *GroupHandlers) Get(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserID(r)
	gid, err := uuid.Parse(chi.URLParam(r, "groupID"))
	if err != nil {
		WriteError(w, apperr.BadRequest("invalid_id", "bad group id"))
		return
	}
	g, err := h.Groups.Get(r.Context(), uid, gid)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, g)
}
