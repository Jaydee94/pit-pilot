// backend/internal/service/payments.go
package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type PaymentSummary struct {
	OutstandingCents int64 `json:"outstanding_cents"`
	ConfirmedCents   int64 `json:"confirmed_cents"`
	OpenCount        int   `json:"open_count"`
	ReportedCount    int   `json:"reported_count"`
	ConfirmedCount   int   `json:"confirmed_count"`
}

type PaymentView struct {
	ResponsibleUserID  uuid.UUID                 `json:"responsible_user_id"`
	DefaultAmountCents int32                     `json:"default_amount_cents"`
	PaymentLink        *string                   `json:"payment_link"`
	IsResponsible      bool                      `json:"is_responsible"`
	Items              []gen.ListPaymentItemsRow `json:"items"`
	Summary            *PaymentSummary           `json:"summary"`
}

type PaymentService struct {
	pool     *pgxpool.Pool
	q        *gen.Queries
	concerts *ConcertService
}

func NewPaymentService(pool *pgxpool.Pool, q *gen.Queries, concerts *ConcertService) *PaymentService {
	return &PaymentService{pool: pool, q: q, concerts: concerts}
}

func summarize(items []gen.ListPaymentItemsRow) *PaymentSummary {
	s := &PaymentSummary{}
	for _, it := range items {
		switch it.Status {
		case "confirmed":
			s.ConfirmedCents += int64(it.AmountCents)
			s.ConfirmedCount++
		case "reported":
			s.OutstandingCents += int64(it.AmountCents)
			s.ReportedCount++
		default:
			s.OutstandingCents += int64(it.AmountCents)
			s.OpenCount++
		}
	}
	return s
}

func (s *PaymentService) Activate(ctx context.Context, userID, concertID uuid.UUID, defaultCents int32, paymentLink *string) (gen.PaymentCollection, error) {
	if _, err := s.concerts.Get(ctx, userID, concertID); err != nil {
		return gen.PaymentCollection{}, err
	}
	_, err := s.q.GetPaymentCollectionByConcert(ctx, concertID)
	if err == nil {
		return gen.PaymentCollection{}, apperr.Conflict("already_active", "payment tracking is already active")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return gen.PaymentCollection{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return gen.PaymentCollection{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := s.q.WithTx(tx)

	col, err := qtx.CreatePaymentCollection(ctx, gen.CreatePaymentCollectionParams{
		ConcertID: concertID, ResponsibleUserID: userID, DefaultAmountCents: defaultCents, PaymentLink: paymentLink,
	})
	if err != nil {
		return gen.PaymentCollection{}, err
	}
	yes, err := qtx.ListYesRsvpUserIDs(ctx, concertID)
	if err != nil {
		return gen.PaymentCollection{}, err
	}
	for _, uid := range yes {
		if _, err := qtx.CreatePaymentItem(ctx, gen.CreatePaymentItemParams{
			CollectionID: col.ID, UserID: uid, AmountCents: defaultCents,
		}); err != nil {
			return gen.PaymentCollection{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return gen.PaymentCollection{}, err
	}
	return col, nil
}

func (s *PaymentService) Get(ctx context.Context, userID, concertID uuid.UUID) (PaymentView, error) {
	if _, err := s.concerts.Get(ctx, userID, concertID); err != nil {
		return PaymentView{}, err
	}
	col, err := s.q.GetPaymentCollectionByConcert(ctx, concertID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentView{}, apperr.NotFound("payment_not_active", "no payment tracking for this concert")
	}
	if err != nil {
		return PaymentView{}, err
	}
	view := PaymentView{
		ResponsibleUserID:  col.ResponsibleUserID,
		DefaultAmountCents: col.DefaultAmountCents,
		PaymentLink:        col.PaymentLink,
		IsResponsible:      col.ResponsibleUserID == userID,
		Items:              []gen.ListPaymentItemsRow{},
	}
	if view.IsResponsible {
		items, err := s.q.ListPaymentItems(ctx, col.ID)
		if err != nil {
			return PaymentView{}, err
		}
		view.Items = items
		view.Summary = summarize(items)
		return view, nil
	}
	own, err := s.q.GetPaymentItemForUser(ctx, gen.GetPaymentItemForUserParams{CollectionID: col.ID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return view, nil // member with no item
	}
	if err != nil {
		return PaymentView{}, err
	}
	// sqlc generates a distinct `GetPaymentItemForUserRow` for the :one query with
	// fields identical to `ListPaymentItemsRow`; convert so Items stays one slice type.
	view.Items = []gen.ListPaymentItemsRow{gen.ListPaymentItemsRow(own)}
	return view, nil
}

// requireResponsible gates membership, loads the active collection, and verifies
// the caller is its responsible user.
func (s *PaymentService) requireResponsible(ctx context.Context, userID, concertID uuid.UUID) (gen.PaymentCollection, error) {
	if _, err := s.concerts.Get(ctx, userID, concertID); err != nil {
		return gen.PaymentCollection{}, err
	}
	col, err := s.q.GetPaymentCollectionByConcert(ctx, concertID)
	if errors.Is(err, pgx.ErrNoRows) {
		return gen.PaymentCollection{}, apperr.NotFound("payment_not_active", "no payment tracking for this concert")
	}
	if err != nil {
		return gen.PaymentCollection{}, err
	}
	if col.ResponsibleUserID != userID {
		return gen.PaymentCollection{}, apperr.Forbidden("not_responsible", "only the ticket organizer can do this")
	}
	return col, nil
}

// loadItem gates membership, loads the active collection and the item, and
// verifies the item belongs to that collection.
func (s *PaymentService) loadItem(ctx context.Context, userID, concertID, itemID uuid.UUID) (gen.PaymentCollection, gen.PaymentItem, error) {
	if _, err := s.concerts.Get(ctx, userID, concertID); err != nil {
		return gen.PaymentCollection{}, gen.PaymentItem{}, err
	}
	col, err := s.q.GetPaymentCollectionByConcert(ctx, concertID)
	if errors.Is(err, pgx.ErrNoRows) {
		return gen.PaymentCollection{}, gen.PaymentItem{}, apperr.NotFound("payment_not_active", "no payment tracking for this concert")
	}
	if err != nil {
		return gen.PaymentCollection{}, gen.PaymentItem{}, err
	}
	item, err := s.q.GetPaymentItem(ctx, itemID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && item.CollectionID != col.ID) {
		return gen.PaymentCollection{}, gen.PaymentItem{}, apperr.NotFound("item_not_found", "payment item not found")
	}
	if err != nil {
		return gen.PaymentCollection{}, gen.PaymentItem{}, err
	}
	return col, item, nil
}
