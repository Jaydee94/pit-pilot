package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/auth"
	"github.com/jaydee94/pit-pilot/backend/internal/password"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

// decoyHash equalizes LoginWithPassword timing between existing and
// non-existing accounts (defends against timing-based user enumeration).
var decoyHash, _ = password.Hash("pit-pilot-login-timing-decoy")

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

func (s *UserService) Register(ctx context.Context, email, pw, displayName string) (gen.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	displayName = strings.TrimSpace(displayName)
	if email == "" {
		return gen.User{}, apperr.BadRequest("invalid_email", "email required")
	}
	if len(pw) < 8 {
		return gen.User{}, apperr.BadRequest("weak_password", "password must be at least 8 characters")
	}
	if displayName == "" {
		return gen.User{}, apperr.BadRequest("invalid_display_name", "display name required")
	}
	hash, err := password.Hash(pw)
	if err != nil {
		return gen.User{}, fmt.Errorf("hash password: %w", err)
	}
	user, err := s.q.CreatePasswordUser(ctx, gen.CreatePasswordUserParams{
		Email: email, DisplayName: displayName, PasswordHash: &hash,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return gen.User{}, apperr.Conflict("email_taken", "email already registered")
		}
		return gen.User{}, fmt.Errorf("create password user: %w", err)
	}
	user.PasswordHash = nil
	return user, nil
}

func (s *UserService) LoginWithPassword(ctx context.Context, email, pw string) (gen.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	user, err := s.q.GetPasswordUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_, _ = password.Verify(pw, decoyHash)
			return gen.User{}, apperr.Unauthorized("invalid_credentials", "invalid email or password")
		}
		return gen.User{}, fmt.Errorf("get password user: %w", err)
	}
	if user.PasswordHash == nil {
		_, _ = password.Verify(pw, decoyHash)
		return gen.User{}, apperr.Unauthorized("invalid_credentials", "invalid email or password")
	}
	ok, err := password.Verify(pw, *user.PasswordHash)
	if err != nil || !ok {
		return gen.User{}, apperr.Unauthorized("invalid_credentials", "invalid email or password")
	}
	user.PasswordHash = nil
	return user, nil
}

func (s *UserService) DevLogin(ctx context.Context, name string) (gen.User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return gen.User{}, apperr.BadRequest("invalid_name", "name required")
	}
	return s.q.UpsertDummyUser(ctx, name)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
