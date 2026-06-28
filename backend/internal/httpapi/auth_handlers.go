// backend/internal/httpapi/auth_handlers.go
package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/auth"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type AuthHandlers struct {
	Users          *service.UserService
	Sessions       *auth.SessionManager
	CookieSecure   bool
	GoogleClientID string
	AllowDevLogin  bool
}

func (a *AuthHandlers) issueSession(w http.ResponseWriter, user gen.User) error {
	token, err := a.Sessions.Issue(user.ID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: "pp_session", Value: token, Path: "/",
		HttpOnly: true, Secure: a.CookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: time.Now().Add(30 * 24 * time.Hour), MaxAge: 30 * 24 * 60 * 60,
	})
	return nil
}

func userResponse(u gen.User) map[string]any {
	return map[string]any{"id": u.ID, "display_name": u.DisplayName, "avatar_url": u.AvatarUrl}
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
		if err := a.issueSession(w, user); err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, userResponse(user))
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
	WriteJSON(w, http.StatusOK, map[string]any{
		"google_client_id": a.GoogleClientID,
		"allow_dev_login":  a.AllowDevLogin,
	})
}

func (a *AuthHandlers) Register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "could not parse body"))
		return
	}
	user, err := a.Users.Register(r.Context(), body.Email, body.Password, body.DisplayName)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.issueSession(w, user); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, userResponse(user))
}

func (a *AuthHandlers) PasswordLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "could not parse body"))
		return
	}
	user, err := a.Users.LoginWithPassword(r.Context(), body.Email, body.Password)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.issueSession(w, user); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, userResponse(user))
}

func (a *AuthHandlers) DevLogin(w http.ResponseWriter, r *http.Request) {
	if !a.AllowDevLogin {
		WriteError(w, apperr.NotFound("not_found", "not found"))
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, apperr.BadRequest("invalid_body", "could not parse body"))
		return
	}
	user, err := a.Users.DevLogin(r.Context(), body.Name)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.issueSession(w, user); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, userResponse(user))
}
