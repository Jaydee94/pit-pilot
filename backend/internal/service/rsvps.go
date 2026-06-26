// backend/internal/service/rsvps.go
package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type RSVPService struct {
	q        *gen.Queries
	concerts *ConcertService
}

func NewRSVPService(q *gen.Queries, concerts *ConcertService) *RSVPService {
	return &RSVPService{q: q, concerts: concerts}
}

func (s *RSVPService) Set(ctx context.Context, userID, concertID uuid.UUID, status string) (gen.Rsvp, error) {
	if status != "yes" && status != "no" {
		return gen.Rsvp{}, apperr.BadRequest("invalid_status", "status must be 'yes' or 'no'")
	}
	// Get gates membership (and existence) for us.
	if _, err := s.concerts.Get(ctx, userID, concertID); err != nil {
		return gen.Rsvp{}, err
	}
	return s.q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: concertID, UserID: userID, Status: status})
}

func (s *RSVPService) List(ctx context.Context, userID, concertID uuid.UUID) ([]gen.ListRSVPsForConcertRow, error) {
	if _, err := s.concerts.Get(ctx, userID, concertID); err != nil {
		return nil, err
	}
	return s.q.ListRSVPsForConcert(ctx, concertID)
}
