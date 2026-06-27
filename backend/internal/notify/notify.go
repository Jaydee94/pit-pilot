package notify

import (
	"context"

	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type Notification struct {
	UserID   uuid.UUID
	Type     string
	Title    string
	Body     string
	URL      string
	DedupKey *string
}

type Enqueuer interface {
	Enqueue(ctx context.Context, q *gen.Queries, rows []Notification) error
}

type OutboxEnqueuer struct{}

func (OutboxEnqueuer) Enqueue(ctx context.Context, q *gen.Queries, rows []Notification) error {
	for _, n := range rows {
		var url *string
		if n.URL != "" {
			u := n.URL
			url = &u
		}
		if err := q.InsertNotification(ctx, gen.InsertNotificationParams{
			UserID: n.UserID, Type: n.Type, Title: n.Title, Body: n.Body, Url: url, DedupKey: n.DedupKey,
		}); err != nil {
			return err
		}
	}
	return nil
}

type FakeEnqueuer struct {
	Rows []Notification
}

func (f *FakeEnqueuer) Enqueue(_ context.Context, _ *gen.Queries, rows []Notification) error {
	f.Rows = append(f.Rows, rows...)
	return nil
}
