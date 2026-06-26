// backend/internal/httpapi/router.go
package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
)

type Deps struct {
	Auth     *AuthHandlers
	Groups   *GroupHandlers
	Concerts *ConcertHandlers
	RSVPs    *RSVPHandlers
	GroupSvc *service.GroupService
	Ready    func(ctx context.Context) error
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if d.Ready != nil {
			if err := d.Ready(r.Context()); err != nil {
				WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
					"error": map[string]string{"code": "not_ready", "message": "database unavailable"},
				})
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	})

	if d.Auth != nil {
		r.Post("/auth/google", d.Auth.Login("google"))
		r.Post("/auth/apple", d.Auth.Login("apple"))
	}

	r.Route("/api", func(api chi.Router) {
		// Always gate /api: use real SessionMiddleware when Auth is wired,
		// otherwise install a catch-all that always 401s (nil-Deps guard).
		// chi middleware only fires for matched routes, so we need an actual
		// catch-all route (/*) to cover the nil-deps case in tests.
		if d.Auth != nil {
			api.Use(d.Auth.SessionMiddleware)
			api.Get("/me", d.Auth.Me)
			api.Post("/auth/logout", d.Auth.Logout)
		} else {
			api.HandleFunc("/*", func(w http.ResponseWriter, _ *http.Request) {
				WriteError(w, apperr.Unauthorized("no_session", "authentication required"))
			})
			return
		}

		if d.Groups != nil {
			api.Get("/groups", d.Groups.List)
			api.Post("/groups", d.Groups.Create)
			api.Post("/groups/join", d.Groups.Join)
		}

		if d.GroupSvc != nil {
			api.Route("/groups/{groupID}", func(gr chi.Router) {
				gr.Use(RequireGroupMember(d.GroupSvc))
				if d.Groups != nil {
					gr.Get("/", d.Groups.Get)
					gr.Get("/members", d.Groups.Members)
					gr.Post("/invite", d.Groups.Invite)
				}
				if d.Concerts != nil {
					gr.Get("/concerts", d.Concerts.ListForGroup)
					gr.Post("/concerts", d.Concerts.Create)
				}
			})
		}

		if d.Concerts != nil {
			api.Get("/concerts/{concertID}", d.Concerts.Get)
		}

		if d.RSVPs != nil {
			api.Put("/concerts/{concertID}/rsvp", d.RSVPs.Set)
			api.Get("/concerts/{concertID}/rsvps", d.RSVPs.List)
		}
	})

	return r
}
