// backend/internal/service/rsvps.go
package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/notify"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type RSVPService struct {
	q        *gen.Queries
	concerts *ConcertService
	enqueuer notify.Enqueuer
	pool     *pgxpool.Pool
}

func NewRSVPService(q *gen.Queries, concerts *ConcertService) *RSVPService {
	return &RSVPService{q: q, concerts: concerts}
}

func (s *RSVPService) SetEnqueuer(e notify.Enqueuer, pool *pgxpool.Pool) {
	s.enqueuer = e
	s.pool = pool
}

func (s *RSVPService) Set(ctx context.Context, userID, concertID uuid.UUID, status string) (gen.Rsvp, error) {
	if status != "yes" && status != "no" {
		return gen.Rsvp{}, apperr.BadRequest("invalid_status", "status must be 'yes' or 'no'")
	}
	// Get gates membership (and existence) for us; keep c for the notification message.
	c, err := s.concerts.Get(ctx, userID, concertID)
	if err != nil {
		return gen.Rsvp{}, err
	}
	if s.enqueuer == nil {
		return s.q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: concertID, UserID: userID, Status: status})
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return gen.Rsvp{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := s.q.WithTx(tx)
	rsvp, err := qtx.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: concertID, UserID: userID, Status: status})
	if err != nil {
		return gen.Rsvp{}, err
	}
	if status == "yes" {
		others, err := qtx.ListRSVPsForConcert(ctx, concertID)
		if err != nil {
			return gen.Rsvp{}, err
		}
		var rows []notify.Notification
		for _, o := range others {
			if o.ID == userID || o.Status != "yes" {
				continue
			}
			rows = append(rows, notify.Notification{
				UserID: o.ID, Type: "rsvp_changed",
				Title: "Neue Zusage: " + c.Artist,
				Body:  "Jemand kommt auch zu " + c.Artist + " mit.",
				URL:   "/concerts/" + concertID.String(),
			})
		}
		if err := s.enqueuer.Enqueue(ctx, qtx, rows); err != nil {
			return gen.Rsvp{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return gen.Rsvp{}, err
	}
	return rsvp, nil
}

func (s *RSVPService) List(ctx context.Context, userID, concertID uuid.UUID) ([]gen.ListRSVPsForConcertRow, error) {
	if _, err := s.concerts.Get(ctx, userID, concertID); err != nil {
		return nil, err
	}
	return s.q.ListRSVPsForConcert(ctx, concertID)
}
