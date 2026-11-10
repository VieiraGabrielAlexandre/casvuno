//go:build integration

package app

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"go.uber.org/fx"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
)

// postgresImage must match infra/compose.yaml.
const postgresImage = "postgres:18.6-alpine"

func startPostgres(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, postgresImage,
		tcpostgres.WithDatabase("casvuno"),
		tcpostgres.WithUsername("casvuno"),
		tcpostgres.WithPassword("casvuno"),
		tcpostgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	return url
}

func TestMigrationsUpDownUp(t *testing.T) {
	url := startPostgres(t)

	if err := postgres.MigrateUp(url); err != nil {
		t.Fatalf("up: %v", err)
	}
	assertExtension(t, url, true)

	if err := postgres.MigrateDown(url, 1); err != nil {
		t.Fatalf("down: %v", err)
	}
	assertExtension(t, url, false)

	if err := postgres.MigrateUp(url); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if err := postgres.MigrateUp(url); err != nil {
		t.Fatalf("up must be idempotent: %v", err)
	}
	assertExtension(t, url, true)
}

func TestAppStartsServesHealthAndStops(t *testing.T) {
	url := startPostgres(t)
	cfg := testConfig(url)
	cfg.HTTPAddr = freeAddr(t)

	a := New(cfg, fx.NopLogger)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+cfg.HTTPAddr+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StatusCode != http.StatusOK || body.Status != "ok" || body.Checks["database"] != "ok" {
		t.Fatalf("unexpected health response: %d %+v", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type = %q", ct)
	}

	if err := a.Stop(ctx); err != nil {
		t.Fatalf("graceful stop: %v", err)
	}
	d := net.Dialer{Timeout: 200 * time.Millisecond}
	if conn, err := d.DialContext(context.Background(), "tcp", cfg.HTTPAddr); err == nil {
		_ = conn.Close()
		t.Fatal("server still accepting connections after stop")
	}
}

func assertExtension(t *testing.T, url string, want bool) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var got bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'citext')").Scan(&got); err != nil {
		t.Fatalf("query: %v", err)
	}
	if got != want {
		t.Fatalf("citext installed = %v, want %v", got, want)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().String()
}
