// backend/internal/worker/worker.go
package worker

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jaydee94/pit-pilot/backend/internal/push"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

// scanLockKey is an arbitrary app-wide key for the advisory lock guarding the scan.
const scanLockKey int64 = 0x70697470 // "pitp"

type Worker struct {
	pool   *pgxpool.Pool
	q      *gen.Queries
	pusher push.Pusher
	batch  int32
}

func New(pool *pgxpool.Pool, q *gen.Queries, pusher push.Pusher) *Worker {
	return &Worker{pool: pool, q: q, pusher: pusher, batch: 100}
}

func (w *Worker) Tick(ctx context.Context) error {
	if err := w.scan(ctx); err != nil {
		return err
	}
	if err := w.sendPending(ctx); err != nil {
		return err
	}
	return w.q.PruneOldNotifications(ctx)
}

func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = w.Tick(ctx) // best-effort; next tick retries
		}
	}
}

func (w *Worker) scan(ctx context.Context) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var got bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", scanLockKey).Scan(&got); err != nil {
		return err
	}
	if !got {
		return nil // another replica is scanning this tick
	}
	qtx := w.q.WithTx(tx)
	if err := qtx.ScanDeadlineReminders(ctx); err != nil {
		return err
	}
	if err := qtx.ScanConcertReminders(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (w *Worker) sendPending(ctx context.Context) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := w.q.WithTx(tx)
	rows, err := qtx.ClaimPendingNotifications(ctx, w.batch)
	if err != nil {
		return err
	}
	for _, n := range rows {
		subs, err := qtx.ListPushSubscriptionsForUser(ctx, n.UserID)
		if err != nil {
			return err
		}
		liveFailures := 0
		for _, sub := range subs {
			dead, serr := w.pusher.Send(ctx, push.Subscription{Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth}, payloadOf(n))
			if dead {
				if err := qtx.DeletePushSubscriptionByID(ctx, sub.ID); err != nil {
					return err
				}
				continue
			}
			if serr != nil {
				liveFailures++
			}
		}
		if liveFailures == 0 {
			if err := qtx.MarkNotificationSent(ctx, n.ID); err != nil {
				return err
			}
		} else {
			if err := qtx.MarkNotificationFailed(ctx, n.ID); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func payloadOf(n gen.Notification) push.Payload {
	url := ""
	if n.Url != nil {
		url = *n.Url
	}
	return push.Payload{Title: n.Title, Body: n.Body, URL: url}
}
