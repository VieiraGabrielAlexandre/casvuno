package app

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/health"
)

func newRouter(pool *pgxpool.Pool, log *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, accessLog(log), middleware.Recoverer)

	r.Get("/health", health.Handler(pool, log))
	return r
}

// accessLog logs one line per request. It records the route pattern rather than the
// raw path and never logs bodies, headers or query strings (ADR 0012).
func accessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			route := "unmatched"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			log.InfoContext(r.Context(), "http request",
				slog.String("request_id", middleware.GetReqID(r.Context())),
				slog.String("method", r.Method),
				slog.String("route", route),
				slog.Int("status", ww.Status()),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}
