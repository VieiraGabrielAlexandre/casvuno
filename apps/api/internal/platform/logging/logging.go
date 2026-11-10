// Package logging builds the process-wide structured logger.
package logging

import (
	"log/slog"
	"os"

	"casvuno/api/internal/platform/config"
)

// New returns a JSON logger in production and a human-readable text logger otherwise.
func New(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.IsProduction() {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
