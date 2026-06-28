package gen_test

import (
	"context"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestPasswordAndDummyUserQueries(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	ctx := context.Background()

	h := "argon2-hash"
	u, err := q.CreatePasswordUser(ctx, gen.CreatePasswordUserParams{
		Email: "a@example.com", DisplayName: "Alice", PasswordHash: &h,
	})
	if err != nil || u.Provider != "password" || u.ProviderSub != "a@example.com" || u.PasswordHash == nil || *u.PasswordHash != h {
		t.Fatalf("create password user: %+v err=%v", u, err)
	}

	got, err := q.GetPasswordUserByEmail(ctx, "a@example.com")
	if err != nil || got.ID != u.ID {
		t.Fatalf("get by email: %+v err=%v", got, err)
	}

	// duplicate email → unique violation
	if _, err := q.CreatePasswordUser(ctx, gen.CreatePasswordUserParams{Email: "a@example.com", DisplayName: "Dup"}); err == nil {
		t.Fatal("expected unique violation on duplicate email")
	}

	d1, err := q.UpsertDummyUser(ctx, "Tester")
	if err != nil || d1.Provider != "dummy" {
		t.Fatalf("dummy upsert: %+v err=%v", d1, err)
	}
	d2, err := q.UpsertDummyUser(ctx, "Tester")
	if err != nil || d2.ID != d1.ID {
		t.Fatalf("dummy upsert not idempotent: %v vs %v", d1.ID, d2.ID)
	}
}
