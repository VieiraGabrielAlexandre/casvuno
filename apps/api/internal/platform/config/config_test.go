package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}
}

func TestLoadFromDefaults(t *testing.T) {
	cfg, err := LoadFrom(lookupFrom(map[string]string{
		"DATABASE_URL": "postgres://u:p@localhost:5432/db",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != EnvDevelopment || cfg.HTTPAddr != ":8080" || cfg.LogLevel != slog.LevelInfo || cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.IsProduction() {
		t.Fatal("development config must not be production")
	}
}

func TestLoadFromOverrides(t *testing.T) {
	cfg, err := LoadFrom(lookupFrom(map[string]string{
		"APP_ENV":          "production",
		"HTTP_ADDR":        ":9000",
		"DATABASE_URL":     "postgresql://u:p@db/casvuno",
		"LOG_LEVEL":        "debug",
		"SHUTDOWN_TIMEOUT": "3s",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.IsProduction() || cfg.HTTPAddr != ":9000" || cfg.LogLevel != slog.LevelDebug || cfg.ShutdownTimeout != 3*time.Second {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoadFromReportsAllErrors(t *testing.T) {
	_, err := LoadFrom(lookupFrom(map[string]string{
		"APP_ENV":          "staging",
		"LOG_LEVEL":        "loud",
		"SHUTDOWN_TIMEOUT": "-1s",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"APP_ENV", "DATABASE_URL is required", "LOG_LEVEL", "SHUTDOWN_TIMEOUT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestLoadFromRejectsNonPostgresURL(t *testing.T) {
	_, err := LoadFrom(lookupFrom(map[string]string{"DATABASE_URL": "mysql://x"}))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL must start with") {
		t.Fatalf("expected DATABASE_URL scheme error, got %v", err)
	}
}
