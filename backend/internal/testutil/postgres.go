// backend/internal/testutil/postgres.go
package testutil

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// NewPostgres starts a disposable Postgres container, applies all migrations,
// and returns a connected pool. Cleanup is registered on t.
func NewPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	// API note (testcontainers-go v0.43.0): WithDatabase/WithUsername/WithPassword
	// were removed; credentials are set via WithEnv. BasicWaitStrategies() is the
	// recommended reliable wait for Postgres (waits for "ready to accept connections"
	// twice, then for the port to be reachable — important on macOS).
	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		testcontainers.WithEnv(map[string]string{
			"POSTGRES_USER":     "pp",
			"POSTGRES_PASSWORD": "pp",
			"POSTGRES_DB":       "pp",
		}),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	// golang-migrate's pgx/v5 driver registers under the scheme "pgx5".
	// ConnectionString returns "postgres://...", so swap the scheme.
	migrateDSN := strings.Replace(dsn, "postgres://", "pgx5://", 1)

	_, file, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(file), "..", "store", "migrations")
	m, err := migrate.New("file://"+migrationsDir, migrateDSN)
	if err != nil {
		t.Fatalf("migrate init: %v", err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
