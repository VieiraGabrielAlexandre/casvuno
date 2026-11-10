package identity

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/clock"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/httpx"
)

// Handler exposes /auth routes and the authentication middleware.
type Handler struct {
	svc    *Service
	clock  clock.Clock
	log    *slog.Logger
	secure bool
}

// NewHandler builds the identity HTTP handler. secureCookies must be true behind HTTPS.
func NewHandler(svc *Service, clk clock.Clock, log *slog.Logger, secureCookies bool) *Handler {
	return &Handler{svc: svc, clock: clk, log: log, secure: secureCookies}
}

// Routes registers the /auth endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/auth/register", h.register)
	r.Post("/auth/login", h.login)
	r.Post("/auth/logout", h.logout)
	r.With(h.RequireUser).Get("/auth/me", h.me)
}

type userResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type registerRequest struct {
	Name        string `json:"name" validate:"required,max=100"`
	Email       string `json:"email" validate:"required,email,max=254"`
	Password    string `json:"password" validate:"required"`
	InviteToken string `json:"inviteToken,omitempty" validate:"omitempty,max=128"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	user, sess, err := h.svc.Register(r.Context(), RegisterInput(req))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	h.setSessionCookie(w, sess)
	httpx.WriteJSON(w, http.StatusCreated, toUserResponse(user.ID.String(), user.Name, user.Email))
}

type loginRequest struct {
	Email    string `json:"email" validate:"required,max=254"`
	Password string `json:"password" validate:"required"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	user, sess, err := h.svc.Login(r.Context(), req.Email, req.Password, clientIP(r))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	h.setSessionCookie(w, sess)
	httpx.WriteJSON(w, http.StatusOK, toUserResponse(user.ID.String(), user.Name, user.Email))
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(h.cookieName()); err == nil {
		if err := h.svc.Logout(r.Context(), c.Value); err != nil {
			httpx.WriteError(w, r, h.log, err)
			return
		}
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	u, _ := authctx.UserFrom(r.Context())
	httpx.WriteJSON(w, http.StatusOK, toUserResponse(u.ID.String(), u.Name, u.Email))
}

// RequireUser authenticates the session cookie, renews it when due and stores the user in
// the request context. Requests without a valid session get 401 and the cookie is cleared.
func (h *Handler) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if c, err := r.Cookie(h.cookieName()); err == nil {
			token = c.Value
		}
		user, renewed, err := h.svc.Authenticate(r.Context(), token)
		if err != nil {
			if errors.Is(err, kernel.ErrUnauthenticated) && token != "" {
				h.clearSessionCookie(w)
			}
			httpx.WriteError(w, r, h.log, err)
			return
		}
		if renewed != nil {
			h.setSessionCookie(w, *renewed)
		}
		next.ServeHTTP(w, r.WithContext(authctx.WithUser(r.Context(), user)))
	})
}

// cookieName uses the __Host- prefix over HTTPS: the browser then requires Secure, Path=/
// and no Domain, so subdomains cannot set or override the session cookie.
func (h *Handler) cookieName() string {
	if h.secure {
		return "__Host-casvuno_session"
	}
	return "casvuno_session"
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, s Session) {
	// Secure follows APP_ORIGIN: always true over HTTPS (required in production), false only for local http dev.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: see comment above
		Name:     h.cookieName(),
		Value:    s.Token,
		Path:     "/",
		Expires:  s.ExpiresAt,
		MaxAge:   max(int(s.ExpiresAt.Sub(h.clock.Now())/time.Second), 1),
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure follows APP_ORIGIN, as in setSessionCookie
		Name: h.cookieName(), Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode,
	})
}

func toUserResponse(id, name, email string) userResponse {
	return userResponse{ID: id, Name: name, Email: email}
}

// clientIP is the TCP peer address. Behind a reverse proxy this becomes the proxy address;
// trusted forwarding headers will be handled with the production proxy (F8.2).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
