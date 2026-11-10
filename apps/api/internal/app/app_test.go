package app

import (
	"context"
	"testing"
	"time"

	"go.uber.org/fx"

	"casvuno/api/internal/platform/config"
)

func testConfig(databaseURL string) config.Config {
	return config.Config{
		Env:             config.EnvTest,
		HTTPAddr:        "127.0.0.1:0",
		DatabaseURL:     databaseURL,
		ShutdownTimeout: 5 * time.Second,
	}
}

// The dependency graph must be complete and acyclic without starting anything.
func TestDependencyGraphIsValid(t *testing.T) {
	if err := fx.ValidateApp(fx.Supply(testConfig("postgres://u:p@127.0.0.1:1/db")), Module); err != nil {
		t.Fatalf("invalid fx graph: %v", err)
	}
}

// Startup must fail fast, not hang or serve traffic, when the database is unreachable.
func TestStartFailsWhenDatabaseIsUnreachable(t *testing.T) {
	a := New(testConfig("postgres://u:p@127.0.0.1:1/db?connect_timeout=1"), fx.NopLogger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.Start(ctx); err == nil {
		_ = a.Stop(ctx)
		t.Fatal("expected start to fail without a database")
	}
}
