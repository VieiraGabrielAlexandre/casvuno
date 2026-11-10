// Package health exposes the GET /health endpoint.
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Pinger is the only capability the health check needs from the database.
type Pinger interface {
	Ping(ctx context.Context) error
}

type response struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

const checkTimeout = time.Second

// Handler reports whether the API can serve traffic: 200 when the database
// answers within one second, 503 otherwise. It never exposes error details.
func Handler(db Pinger, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
		defer cancel()

		status, code, dbStatus := "ok", http.StatusOK, "ok"
		if err := db.Ping(ctx); err != nil {
			log.WarnContext(r.Context(), "health check: database unavailable", slog.Any("error", err))
			status, code, dbStatus = "unavailable", http.StatusServiceUnavailable, "unavailable"
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(response{Status: status, Checks: map[string]string{"database": dbStatus}})
	}
}
