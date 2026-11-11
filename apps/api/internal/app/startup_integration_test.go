//go:build integration

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/fx"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres/pgtest"
)

// databaseURL points to a migrated database shared by the integration tests of this package.
// Tests isolate their data by creating their own users and households (no truncation).
var databaseURL string

func TestMain(m *testing.M) {
	os.Exit(runWithPostgres(m))
}

func runWithPostgres(m *testing.M) int {
	url, stop, err := pgtest.Start(context.Background())
	defer stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	databaseURL = url
	code := m.Run()
	// Every test left the shared database in a consistent state (infra/sql/check-invariants.sql).
	if violations, err := invariantViolations(context.Background(), url); err != nil || len(violations) > 0 {
		fmt.Fprintln(os.Stderr, "financial invariants violated after the test run:", violations, err)
		return 1
	}
	return code
}

// invariantViolations runs infra/sql/check-invariants.sql and returns one line per violation.
func invariantViolations(ctx context.Context, url string) ([]string, error) {
	sql, err := os.ReadFile("../../../../infra/sql/check-invariants.sql")
	if err != nil {
		return nil, err
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, string(sql))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var inv, ref, detail string
		if err := rows.Scan(&inv, &ref, &detail); err != nil {
			return nil, err
		}
		out = append(out, inv+" "+ref+": "+detail)
	}
	return out, rows.Err()
}

func assertInvariants(t *testing.T) {
	t.Helper()
	violations, err := invariantViolations(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("financial invariants violated: %v", violations)
	}
}

// TestMigrationsUpDownUp runs the full migration cycle on a separate, empty database.
func TestMigrationsUpDownUp(t *testing.T) {
	url := createDatabase(t, "migrations_cycle")

	if err := postgres.MigrateUp(url); err != nil {
		t.Fatalf("up: %v", err)
	}
	assertSchema(t, url, true)

	if err := postgres.MigrateDown(url, 7); err != nil {
		t.Fatalf("down: %v", err)
	}
	assertSchema(t, url, false)

	if err := postgres.MigrateUp(url); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if err := postgres.MigrateUp(url); err != nil {
		t.Fatalf("up must be idempotent: %v", err)
	}
	assertSchema(t, url, true)
}

func TestAppStartsServesHealthAndStops(t *testing.T) {
	cfg := testConfig(databaseURL)
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

	if err := a.Stop(ctx); err != nil {
		t.Fatalf("graceful stop: %v", err)
	}
	d := net.Dialer{Timeout: 200 * time.Millisecond}
	if conn, err := d.DialContext(context.Background(), "tcp", cfg.HTTPAddr); err == nil {
		_ = conn.Close()
		t.Fatal("server still accepting connections after stop")
	}
}

// createDatabase creates an empty database in the shared container and returns its URL.
func createDatabase(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+name); err != nil {
		t.Fatalf("drop database: %v", err)
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	return strings.Replace(databaseURL, "/casvuno?", "/"+name+"?", 1)
}

// assertSchema checks that the F1 tables exist (or not) and that citext (migration 1) stays.
func assertSchema(t *testing.T, url string, want bool) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	for _, table := range []string{"users", "sessions", "households", "household_members", "household_invites", "audit_events", "categories", "idempotency_keys", "accounts", "account_movements", "transfers"} {
		var exists bool
		if err := conn.QueryRow(ctx, "SELECT to_regclass('public.' || $1) IS NOT NULL", table).Scan(&exists); err != nil {
			t.Fatalf("query: %v", err)
		}
		if exists != want {
			t.Errorf("table %s exists = %v, want %v", table, exists, want)
		}
	}
	var citext bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'citext')").Scan(&citext); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !citext {
		t.Error("citext (migration 1) must remain after reverting migrations 2-8")
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
