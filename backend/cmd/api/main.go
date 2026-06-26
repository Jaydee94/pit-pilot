// backend/cmd/api/main.go
package main

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jaydee94/pit-pilot/backend/internal/auth"
	"github.com/jaydee94/pit-pilot/backend/internal/config"
	"github.com/jaydee94/pit-pilot/backend/internal/httpapi"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

func newInviteCode() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()
	q := gen.New(pool)

	verifier, err := auth.NewOIDCVerifier(ctx, cfg.GoogleClientID, cfg.AppleClientID)
	if err != nil {
		log.Fatalf("oidc: %v", err)
	}
	sessions := auth.NewSessionManager(cfg.SessionSigningKey, 30*24*time.Hour, nil)

	users := service.NewUserService(q, verifier)
	groups := service.NewGroupService(pool, q, newInviteCode)
	concerts := service.NewConcertService(q, groups)
	rsvps := service.NewRSVPService(q, concerts)

	router := httpapi.NewRouter(httpapi.Deps{
		Auth:     &httpapi.AuthHandlers{Users: users, Sessions: sessions, CookieSecure: cfg.CookieSecure},
		Groups:   &httpapi.GroupHandlers{Groups: groups},
		Concerts: &httpapi.ConcertHandlers{Concerts: concerts},
		RSVPs:    &httpapi.RSVPHandlers{RSVPs: rsvps},
		GroupSvc: groups,
	})

	log.Printf("pit-pilot api listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatal(err)
	}
}
