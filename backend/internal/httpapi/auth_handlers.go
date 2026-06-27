// backend/internal/httpapi/auth_handlers.go
package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/auth"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
)

type AuthHandlers struct {
	Users          *service.UserService
	Sessions       *auth.SessionManager
	CookieSecure   bool
	GoogleClientID string
}

func (a *AuthHandlers) Login(provider string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDToken string `json:"id_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.IDToken == "" {
			WriteError(w, apperr.BadRequest("invalid_body", "id_token required"))
			return
		}
		user, err := a.Users.LoginWithIDToken(r.Context(), provider, body.IDToken)
		if err != nil {
			WriteError(w, err)
			return
		}
		token, err := a.Sessions.Issue(user.ID)
		if err != nil {
			WriteError(w, err)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: "pp_session", Value: token, Path: "/",
			HttpOnly: true, Secure: a.CookieSecure, SameSite: http.SameSiteLaxMode,
			Expires: time.Now().Add(30 * 24 * time.Hour), MaxAge: 30 * 24 * 60 * 60,
		})
		WriteJSON(w, http.StatusOK, map[string]any{
			"id": user.ID, "display_name": user.DisplayName, "avatar_url": user.AvatarUrl,
		})
	}
}

func (a *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: "pp_session", Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: a.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (a *AuthHandlers) Me(w http.ResponseWriter, r *http.Request) {
	id, ok := UserID(r)
	if !ok {
		WriteError(w, apperr.Unauthorized("no_session", "authentication required"))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (a *AuthHandlers) Config(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"google_client_id": a.GoogleClientID})
}
