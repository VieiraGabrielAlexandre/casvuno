// Package pgtest starts a disposable, migrated PostgreSQL for integration tests
// (Testcontainers). Import it only from tests built with the "integration" tag.
package pgtest

import (
	"context"
	"fmt"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
)

// Image must match infra/compose.yaml.
const Image = "postgres:18.6-alpine"

// Start runs a PostgreSQL container, applies all migrations and returns its URL and a stop
// function. Call stop even when err != nil.
func Start(ctx context.Context) (url string, stop func(), err error) {
	ctr, err := tcpostgres.Run(ctx, Image,
		tcpostgres.WithDatabase("casvuno"),
		tcpostgres.WithUsername("casvuno"),
		tcpostgres.WithPassword("casvuno"),
		tcpostgres.BasicWaitStrategies(),
	)
	stop = func() {
		if ctr != nil {
			_ = ctr.Terminate(context.Background())
		}
	}
	if err != nil {
		return "", stop, fmt.Errorf("start postgres: %w", err)
	}
	url, err = ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return "", stop, fmt.Errorf("connection string: %w", err)
	}
	if err := postgres.MigrateUp(url); err != nil {
		return "", stop, fmt.Errorf("migrate: %w", err)
	}
	return url, stop, nil
}
