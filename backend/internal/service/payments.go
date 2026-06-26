// backend/internal/service/payments.go
package service

import (
	"context"
	"errors"
	"time"

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
	if defaultCents < 0 {
		return gen.PaymentCollection{}, apperr.BadRequest("invalid_amount", "amount must be non-negative")
	}
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

func (s *PaymentService) AddItem(ctx context.Context, userID, concertID, targetUserID uuid.UUID, amountCents *int32) (gen.ListPaymentItemsRow, error) {
	col, err := s.requireResponsible(ctx, userID, concertID)
	if err != nil {
		return gen.ListPaymentItemsRow{}, err
	}
	amount := col.DefaultAmountCents
	if amountCents != nil {
		amount = *amountCents
	}
	if amount < 0 {
		return gen.ListPaymentItemsRow{}, apperr.BadRequest("invalid_amount", "amount must be non-negative")
	}
	// The target must be a member of the concert's group (concerts.Get gates on
	// the passed user's membership, so a non-member surfaces as 403/404 there).
	if _, err := s.concerts.Get(ctx, targetUserID, concertID); err != nil {
		if e, ok := apperr.As(err); ok && (e.HTTPStatus == 403 || e.HTTPStatus == 404) {
			return gen.ListPaymentItemsRow{}, apperr.BadRequest("target_not_member", "user is not a member of this group")
		}
		return gen.ListPaymentItemsRow{}, err
	}
	if _, err := s.q.GetPaymentItemForUser(ctx, gen.GetPaymentItemForUserParams{CollectionID: col.ID, UserID: targetUserID}); err == nil {
		return gen.ListPaymentItemsRow{}, apperr.Conflict("item_exists", "this person already has a payment item")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return gen.ListPaymentItemsRow{}, err
	}
	if _, err := s.q.CreatePaymentItem(ctx, gen.CreatePaymentItemParams{
		CollectionID: col.ID, UserID: targetUserID, AmountCents: amount}); err != nil {
		return gen.ListPaymentItemsRow{}, err
	}
	row, err := s.q.GetPaymentItemForUser(ctx, gen.GetPaymentItemForUserParams{CollectionID: col.ID, UserID: targetUserID})
	// GetPaymentItemForUser returns gen.GetPaymentItemForUserRow (identical fields); convert.
	return gen.ListPaymentItemsRow(row), err
}

func (s *PaymentService) SetAmount(ctx context.Context, userID, concertID, itemID uuid.UUID, amountCents int32) (gen.PaymentItem, error) {
	if _, _, err := s.loadItemAsResponsible(ctx, userID, concertID, itemID); err != nil {
		return gen.PaymentItem{}, err
	}
	if amountCents < 0 {
		return gen.PaymentItem{}, apperr.BadRequest("invalid_amount", "amount must be non-negative")
	}
	return s.q.UpdatePaymentItemAmount(ctx, gen.UpdatePaymentItemAmountParams{ID: itemID, AmountCents: amountCents})
}

func (s *PaymentService) RemoveItem(ctx context.Context, userID, concertID, itemID uuid.UUID) error {
	if _, _, err := s.loadItemAsResponsible(ctx, userID, concertID, itemID); err != nil {
		return err
	}
	return s.q.DeletePaymentItem(ctx, itemID)
}

func (s *PaymentService) Deactivate(ctx context.Context, userID, concertID uuid.UUID) error {
	col, err := s.requireResponsible(ctx, userID, concertID)
	if err != nil {
		return err
	}
	return s.q.DeletePaymentCollection(ctx, col.ID)
}

func (s *PaymentService) SetPaymentLink(ctx context.Context, userID, concertID uuid.UUID, link *string) (gen.PaymentCollection, error) {
	col, err := s.requireResponsible(ctx, userID, concertID)
	if err != nil {
		return gen.PaymentCollection{}, err
	}
	return s.q.UpdatePaymentCollectionLink(ctx, gen.UpdatePaymentCollectionLinkParams{ID: col.ID, PaymentLink: link})
}

// loadItemAsResponsible loads an item and verifies the caller is the collection's responsible user.
func (s *PaymentService) loadItemAsResponsible(ctx context.Context, userID, concertID, itemID uuid.UUID) (gen.PaymentCollection, gen.PaymentItem, error) {
	col, item, err := s.loadItem(ctx, userID, concertID, itemID)
	if err != nil {
		return gen.PaymentCollection{}, gen.PaymentItem{}, err
	}
	if col.ResponsibleUserID != userID {
		return gen.PaymentCollection{}, gen.PaymentItem{}, apperr.Forbidden("not_responsible", "only the ticket organizer can do this")
	}
	return col, item, nil
}

func (s *PaymentService) Report(ctx context.Context, userID, concertID, itemID uuid.UUID) (gen.PaymentItem, error) {
	_, item, err := s.loadItem(ctx, userID, concertID, itemID)
	if err != nil {
		return gen.PaymentItem{}, err
	}
	if item.UserID != userID {
		return gen.PaymentItem{}, apperr.Forbidden("not_owner", "you can only report your own payment")
	}
	if item.Status != "open" {
		return gen.PaymentItem{}, apperr.BadRequest("invalid_transition", "can only report an open item")
	}
	now := time.Now()
	return s.q.UpdatePaymentItemStatus(ctx, gen.UpdatePaymentItemStatusParams{
		ID: itemID, Status: "reported", ReportedAt: &now, ConfirmedAt: nil})
}

func (s *PaymentService) UnReport(ctx context.Context, userID, concertID, itemID uuid.UUID) (gen.PaymentItem, error) {
	_, item, err := s.loadItem(ctx, userID, concertID, itemID)
	if err != nil {
		return gen.PaymentItem{}, err
	}
	if item.UserID != userID {
		return gen.PaymentItem{}, apperr.Forbidden("not_owner", "you can only change your own payment")
	}
	if item.Status != "reported" {
		return gen.PaymentItem{}, apperr.BadRequest("invalid_transition", "can only un-report a reported item")
	}
	return s.q.UpdatePaymentItemStatus(ctx, gen.UpdatePaymentItemStatusParams{
		ID: itemID, Status: "open", ReportedAt: nil, ConfirmedAt: nil})
}

func (s *PaymentService) Confirm(ctx context.Context, userID, concertID, itemID uuid.UUID) (gen.PaymentItem, error) {
	_, item, err := s.loadItemAsResponsible(ctx, userID, concertID, itemID)
	if err != nil {
		return gen.PaymentItem{}, err
	}
	if item.Status == "confirmed" {
		return gen.PaymentItem{}, apperr.BadRequest("invalid_transition", "already confirmed")
	}
	now := time.Now()
	return s.q.UpdatePaymentItemStatus(ctx, gen.UpdatePaymentItemStatusParams{
		ID: itemID, Status: "confirmed", ReportedAt: item.ReportedAt, ConfirmedAt: &now})
}

func (s *PaymentService) UnConfirm(ctx context.Context, userID, concertID, itemID uuid.UUID) (gen.PaymentItem, error) {
	_, item, err := s.loadItemAsResponsible(ctx, userID, concertID, itemID)
	if err != nil {
		return gen.PaymentItem{}, err
	}
	if item.Status != "confirmed" {
		return gen.PaymentItem{}, apperr.BadRequest("invalid_transition", "item is not confirmed")
	}
	status := "open"
	if item.ReportedAt != nil {
		status = "reported"
	}
	return s.q.UpdatePaymentItemStatus(ctx, gen.UpdatePaymentItemStatusParams{
		ID: itemID, Status: status, ReportedAt: item.ReportedAt, ConfirmedAt: nil})
}
