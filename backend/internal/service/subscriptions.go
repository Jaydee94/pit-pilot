package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type SubscriptionService struct {
	q *gen.Queries
}

func NewSubscriptionService(q *gen.Queries) *SubscriptionService {
	return &SubscriptionService{q: q}
}

func (s *SubscriptionService) Save(ctx context.Context, userID uuid.UUID, endpoint, p256dh, auth string) (gen.PushSubscription, error) {
	if endpoint == "" || p256dh == "" || auth == "" {
		return gen.PushSubscription{}, apperr.BadRequest("invalid_subscription", "endpoint, p256dh and auth are required")
	}
	return s.q.UpsertPushSubscription(ctx, gen.UpsertPushSubscriptionParams{
		UserID: userID, Endpoint: endpoint, P256dh: p256dh, Auth: auth})
}

func (s *SubscriptionService) Delete(ctx context.Context, userID uuid.UUID, endpoint string) error {
	return s.q.DeletePushSubscription(ctx, gen.DeletePushSubscriptionParams{UserID: userID, Endpoint: endpoint})
}
