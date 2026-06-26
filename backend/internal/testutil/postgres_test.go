// backend/internal/testutil/postgres_test.go
package testutil

import (
	"context"
	"testing"
)

func TestNewPostgresRunsMigrations(t *testing.T) {
	pool := NewPostgres(t)
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables WHERE table_name='users'`).Scan(&n)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected users table to exist, got count %d", n)
	}
}
