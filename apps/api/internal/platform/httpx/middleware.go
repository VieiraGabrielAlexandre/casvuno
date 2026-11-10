package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/google/uuid"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
)

type requestIDKey struct{}

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RequestID returns the request ID assigned by WithRequestID.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// WithRequestID accepts a well-formed incoming X-Request-Id or generates one, stores it in
// the context and echoes it in the response header.
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if !validRequestID.MatchString(id) {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// Recoverer turns panics into a logged 500 with the standard envelope.
func Recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
						panic(rec)
					}
					WriteError(w, r, log, fmt.Errorf("panic: %v", rec))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// APIHeaders sets security and caching headers for JSON API responses. API responses are
// private and never cached (they carry financial data from F2 on).
func APIHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// SameOrigin is the CSRF defence for cookie-authenticated, state-changing requests (ADR 0005):
// unsafe methods must come from appOrigin (Origin header) or declare Sec-Fetch-Site: same-origin.
func SameOrigin(appOrigin string, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			origin := r.Header.Get("Origin")
			allowed := origin == appOrigin ||
				(origin == "" && r.Header.Get("Sec-Fetch-Site") == "same-origin")
			if !allowed {
				WriteError(w, r, log, kernel.NewError(kernel.ErrForbidden, "CROSS_ORIGIN_REQUEST",
					"Requisição de origem não permitida."))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// NotFound and MethodNotAllowed keep unknown API routes inside the JSON envelope.
func NotFound(log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, log, kernel.NewError(kernel.ErrNotFound, "NOT_FOUND", "Rota não encontrada."))
	}
}

// MethodNotAllowed answers 405 in the JSON envelope.
func MethodNotAllowed(log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusMethodNotAllowed, errorEnvelope{Error: errorPayload{
			Code: "METHOD_NOT_ALLOWED", Message: "Método não permitido.", RequestID: RequestID(r.Context()),
		}})
	}
}
