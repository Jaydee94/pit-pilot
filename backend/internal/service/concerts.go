// backend/internal/service/concerts.go
package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/notify"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type ConcertInput struct {
	Artist       string
	EventAt      time.Time
	Venue        string
	City         string
	TicketURL    string
	Notes        string
	PriceCents   *int32
	RSVPDeadline time.Time
}

type ConcertService struct {
	q        *gen.Queries
	groups   *GroupService
	enqueuer notify.Enqueuer
	pool     *pgxpool.Pool
}

func NewConcertService(q *gen.Queries, groups *GroupService) *ConcertService {
	return &ConcertService{q: q, groups: groups}
}

func (s *ConcertService) SetEnqueuer(e notify.Enqueuer, pool *pgxpool.Pool) {
	s.enqueuer = e
	s.pool = pool
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *ConcertService) Create(ctx context.Context, userID, groupID uuid.UUID, in ConcertInput) (gen.Concert, error) {
	if _, err := s.groups.RequireMembership(ctx, userID, groupID); err != nil {
		return gen.Concert{}, err
	}
	if in.Artist == "" {
		return gen.Concert{}, apperr.BadRequest("invalid_artist", "artist required")
	}
	if in.EventAt.IsZero() || in.RSVPDeadline.IsZero() {
		return gen.Concert{}, apperr.BadRequest("invalid_dates", "event time and rsvp deadline required")
	}
	if in.RSVPDeadline.After(in.EventAt) {
		return gen.Concert{}, apperr.BadRequest("deadline_after_event", "rsvp deadline must be before the event")
	}
	params := gen.CreateConcertParams{
		GroupID: groupID, Artist: in.Artist, EventAt: in.EventAt,
		Venue: strPtr(in.Venue), City: strPtr(in.City), TicketUrl: strPtr(in.TicketURL),
		PriceCents: in.PriceCents, Notes: strPtr(in.Notes),
		RsvpDeadline: in.RSVPDeadline, CreatedBy: userID,
	}
	if s.enqueuer == nil {
		return s.q.CreateConcert(ctx, params)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return gen.Concert{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := s.q.WithTx(tx)
	c, err := qtx.CreateConcert(ctx, params)
	if err != nil {
		return gen.Concert{}, err
	}
	members, err := qtx.ListGroupMembers(ctx, groupID)
	if err != nil {
		return gen.Concert{}, err
	}
	var rows []notify.Notification
	for _, m := range members {
		if m.ID == userID {
			continue
		}
		rows = append(rows, notify.Notification{
			UserID: m.ID, Type: "concert_new",
			Title: "Neues Konzert: " + c.Artist,
			Body:  c.Artist + " wurde in deiner Gruppe eingeplant.",
			URL:   "/concerts/" + c.ID.String(),
		})
	}
	if err := s.enqueuer.Enqueue(ctx, qtx, rows); err != nil {
		return gen.Concert{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return gen.Concert{}, err
	}
	return c, nil
}

func (s *ConcertService) ListForGroup(ctx context.Context, userID, groupID uuid.UUID) ([]gen.Concert, error) {
	if _, err := s.groups.RequireMembership(ctx, userID, groupID); err != nil {
		return nil, err
	}
	return s.q.ListConcertsForGroup(ctx, groupID)
}

func (s *ConcertService) Get(ctx context.Context, userID, concertID uuid.UUID) (gen.Concert, error) {
	c, err := s.q.GetConcert(ctx, concertID)
	if errors.Is(err, pgx.ErrNoRows) {
		return gen.Concert{}, apperr.NotFound("concert_not_found", "concert not found")
	}
	if err != nil {
		return gen.Concert{}, err
	}
	if _, err := s.groups.RequireMembership(ctx, userID, c.GroupID); err != nil {
		return gen.Concert{}, err
	}
	return c, nil
}
