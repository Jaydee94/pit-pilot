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
	"github.com/jaydee94/pit-pilot/backend/internal/notify"
	"github.com/jaydee94/pit-pilot/backend/internal/push"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/worker"
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
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("db ping: %v", err)
	}
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
	payments := service.NewPaymentService(pool, q, concerts)
	subs := service.NewSubscriptionService(q)

	enq := notify.OutboxEnqueuer{}
	concerts.SetEnqueuer(enq, pool)
	rsvps.SetEnqueuer(enq, pool)
	payments.SetEnqueuer(enq)

	if cfg.VapidPrivateKey != "" {
		pusher := push.NewWebPusher(cfg.VapidPublicKey, cfg.VapidPrivateKey, cfg.VapidSubject)
		go worker.New(pool, q, pusher).Run(ctx, 60*time.Second)
	}

	router := httpapi.NewRouter(httpapi.Deps{
		Auth:     &httpapi.AuthHandlers{Users: users, Sessions: sessions, CookieSecure: cfg.CookieSecure, GoogleClientID: cfg.GoogleClientID},
		Groups:   &httpapi.GroupHandlers{Groups: groups},
		Concerts: &httpapi.ConcertHandlers{Concerts: concerts},
		RSVPs:    &httpapi.RSVPHandlers{RSVPs: rsvps},
		Payments: &httpapi.PaymentHandlers{Payments: payments},
		Push:     &httpapi.PushHandlers{Subs: subs, VapidPublicKey: cfg.VapidPublicKey},
		GroupSvc: groups,
		Ready:    func(c context.Context) error { return pool.Ping(c) },
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("pit-pilot api listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
