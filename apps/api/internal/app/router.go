package app

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/households"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/identity"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/config"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/health"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/httpx"
)

func newRouter(cfg config.Config, pool *pgxpool.Pool, log *slog.Logger, ids *identity.Handler, hh *households.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.WithRequestID, accessLog(log), httpx.Recoverer(log))

	r.Get("/health", health.Handler(pool, log))

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(httpx.APIHeaders, httpx.SameOrigin(cfg.AppOrigin, log))
		r.NotFound(httpx.NotFound(log))
		r.MethodNotAllowed(httpx.MethodNotAllowed(log))

		ids.Routes(r) // public: register, login, logout; /auth/me authenticates itself

		r.Group(func(r chi.Router) {
			r.Use(ids.RequireUser)
			hh.Routes(r)
		})
	})
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
				slog.String("request_id", httpx.RequestID(r.Context())),
				slog.String("method", r.Method),
				slog.String("route", route),
				slog.Int("status", ww.Status()),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}
