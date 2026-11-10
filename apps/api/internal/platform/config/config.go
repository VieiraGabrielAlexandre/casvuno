// Package config loads and validates process configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"
)

// Environment names accepted by APP_ENV.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
	EnvTest        = "test"
)

// Config holds every setting the process needs. It is immutable after Load.
type Config struct {
	Env             string
	HTTPAddr        string
	DatabaseURL     string
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
	// AppOrigin is the web origin (scheme://host[:port]) that serves the UI and proxies the API.
	// It is the only origin allowed on state-changing requests (CSRF, ADR 0005).
	AppOrigin string
	// RegistrationEnabled allows public sign-up; when false only invited users can register (I-13).
	RegistrationEnabled bool
}

// IsProduction reports whether the process runs with production settings.
func (c Config) IsProduction() bool { return c.Env == EnvProduction }

// SecureCookies reports whether cookies must carry the Secure attribute (HTTPS origin).
func (c Config) SecureCookies() bool { return strings.HasPrefix(c.AppOrigin, "https://") }

// Load reads configuration from the process environment.
func Load() (Config, error) { return LoadFrom(os.LookupEnv) }

// LoadFrom reads configuration using the given lookup function, which makes it testable.
// It returns every validation problem at once so misconfiguration fails fast and clearly.
func LoadFrom(lookup func(string) (string, bool)) (Config, error) {
	get := func(key, def string) string {
		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return def
	}

	var errs []error
	cfg := Config{
		Env:         get("APP_ENV", EnvDevelopment),
		HTTPAddr:    get("HTTP_ADDR", ":8080"),
		DatabaseURL: get("DATABASE_URL", ""),
	}

	switch cfg.Env {
	case EnvDevelopment, EnvProduction, EnvTest:
	default:
		errs = append(errs, fmt.Errorf("APP_ENV must be one of development, production, test; got %q", cfg.Env))
	}

	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	} else if !strings.HasPrefix(cfg.DatabaseURL, "postgres://") && !strings.HasPrefix(cfg.DatabaseURL, "postgresql://") {
		errs = append(errs, errors.New("DATABASE_URL must start with postgres:// or postgresql://"))
	}

	if err := cfg.LogLevel.UnmarshalText([]byte(get("LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
	}

	timeout, err := time.ParseDuration(get("SHUTDOWN_TIMEOUT", "10s"))
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("SHUTDOWN_TIMEOUT: %w", err))
	case timeout <= 0:
		errs = append(errs, errors.New("SHUTDOWN_TIMEOUT must be positive"))
	default:
		cfg.ShutdownTimeout = timeout
	}

	cfg.AppOrigin = strings.TrimSuffix(get("APP_ORIGIN", "http://localhost:3000"), "/")
	if u, err := url.Parse(cfg.AppOrigin); err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		u.Host == "" || u.Path != "" || u.RawQuery != "" || u.User != nil {
		errs = append(errs, fmt.Errorf("APP_ORIGIN must be an origin like https://casvuno.example; got %q", cfg.AppOrigin))
	} else if cfg.IsProduction() && u.Scheme != "https" {
		errs = append(errs, errors.New("APP_ORIGIN must use https in production"))
	}

	switch v := strings.ToLower(get("REGISTRATION_ENABLED", "true")); v {
	case "true", "1", "yes":
		cfg.RegistrationEnabled = true
	case "false", "0", "no":
		cfg.RegistrationEnabled = false
	default:
		errs = append(errs, fmt.Errorf("REGISTRATION_ENABLED must be true or false; got %q", v))
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return cfg, nil
}
