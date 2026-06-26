// backend/internal/httpapi/middleware.go
package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
)

type ctxKey int

const userIDKey ctxKey = iota

func UserID(r *http.Request) (uuid.UUID, bool) {
	id, ok := r.Context().Value(userIDKey).(uuid.UUID)
	return id, ok
}

func withUserID(r *http.Request, id uuid.UUID) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userIDKey, id))
}

func (a *AuthHandlers) SessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("pp_session")
		if err != nil {
			WriteError(w, apperr.Unauthorized("no_session", "authentication required"))
			return
		}
		id, err := a.Sessions.Validate(c.Value)
		if err != nil {
			WriteError(w, apperr.Unauthorized("invalid_session", "session invalid or expired"))
			return
		}
		next.ServeHTTP(w, withUserID(r, id))
	})
}

// RequireGroupMember ensures the session user is a member of {groupID}.
func RequireGroupMember(groups *service.GroupService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid, ok := UserID(r)
			if !ok {
				WriteError(w, apperr.Unauthorized("no_session", "authentication required"))
				return
			}
			gid, err := uuid.Parse(chi.URLParam(r, "groupID"))
			if err != nil {
				WriteError(w, apperr.BadRequest("invalid_id", "bad group id"))
				return
			}
			if _, err := groups.RequireMembership(r.Context(), uid, gid); err != nil {
				WriteError(w, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
