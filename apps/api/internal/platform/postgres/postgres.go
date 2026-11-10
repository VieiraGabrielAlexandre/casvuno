// Package postgres owns the PostgreSQL connection pool and schema migrations.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5:// driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"

	"casvuno/api/db"
)

// NewPool creates a lazily connecting pool. Callers decide when to Ping and must Close it.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	return pool, nil
}

// MigrateUp applies all pending migrations.
func MigrateUp(databaseURL string) error {
	return withMigrator(databaseURL, func(m *migrate.Migrate) error { return m.Up() })
}

// MigrateDown reverts the given number of migrations (steps > 0).
func MigrateDown(databaseURL string, steps int) error {
	if steps <= 0 {
		return errors.New("steps must be greater than zero")
	}
	return withMigrator(databaseURL, func(m *migrate.Migrate) error { return m.Steps(-steps) })
}

func withMigrator(databaseURL string, run func(*migrate.Migrate) error) (err error) {
	src, err := iofs.New(db.Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, migrateURL(databaseURL))
	if err != nil {
		return fmt.Errorf("init migrator: %w", err)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		err = errors.Join(err, srcErr, dbErr)
	}()

	if err := run(m); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

// migrateURL switches the scheme to the golang-migrate pgx v5 driver.
func migrateURL(databaseURL string) string {
	for _, prefix := range []string{"postgresql://", "postgres://"} {
		if rest, ok := strings.CutPrefix(databaseURL, prefix); ok {
			return "pgx5://" + rest
		}
	}
	return databaseURL
}
