package service

import (
	"context"
	"fmt"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/auth"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type UserService struct {
	q *gen.Queries
	v auth.IDTokenVerifier
}

func NewUserService(q *gen.Queries, v auth.IDTokenVerifier) *UserService {
	return &UserService{q: q, v: v}
}

func (s *UserService) LoginWithIDToken(ctx context.Context, provider, rawToken string) (gen.User, error) {
	id, err := s.v.Verify(ctx, provider, rawToken)
	if err != nil {
		return gen.User{}, fmt.Errorf("%w: %w",
			&apperr.Error{Code: "invalid_id_token", Message: "could not verify id token", HTTPStatus: 401},
			auth.ErrInvalidToken,
		)
	}
	var avatar *string
	if id.PictureURL != "" {
		avatar = &id.PictureURL
	}
	user, err := s.q.UpsertUser(ctx, gen.UpsertUserParams{
		Provider:    id.Provider,
		ProviderSub: id.Subject,
		Email:       id.Email,
		DisplayName: id.Name,
		AvatarUrl:   avatar,
	})
	if err != nil {
		return gen.User{}, fmt.Errorf("upsert user: %w", err)
	}
	return user, nil
}
