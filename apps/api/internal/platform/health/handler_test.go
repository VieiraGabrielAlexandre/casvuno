package health

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type pingFunc func(context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }

func TestHandler(t *testing.T) {
	tests := []struct {
		name       string
		pingErr    error
		wantCode   int
		wantStatus string
		wantDB     string
	}{
		{"database up", nil, http.StatusOK, "ok", "ok"},
		{"database down", errors.New("connection refused"), http.StatusServiceUnavailable, "unavailable", "unavailable"},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := Handler(pingFunc(func(context.Context) error { return tt.pingErr }), log)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil))

			if rec.Code != tt.wantCode {
				t.Fatalf("status code = %d, want %d", rec.Code, tt.wantCode)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("content type = %q", ct)
			}
			var body response
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("invalid json: %v", err)
			}
			if body.Status != tt.wantStatus || body.Checks["database"] != tt.wantDB {
				t.Fatalf("unexpected body: %+v", body)
			}
			if tt.pingErr != nil && strings.Contains(rec.Body.String(), tt.pingErr.Error()) {
				t.Fatal("error details must not leak in the response")
			}
		})
	}
}
