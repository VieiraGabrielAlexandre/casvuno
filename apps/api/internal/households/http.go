package households

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/httpx"
)

// Handler exposes household routes. Every route expects an authenticated user in the context.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler builds the households HTTP handler.
func NewHandler(svc *Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Routes registers household routes on a router already protected by authentication.
// Routes under /households/{householdId} require an active membership (ADR 0006).
func (h *Handler) Routes(r chi.Router) {
	r.Post("/households", h.create)
	r.Get("/households", h.list)
	r.Post("/invites/accept", h.acceptInvite)

	r.Route("/households/{householdId}", func(r chi.Router) {
		r.Use(h.RequireMember)
		r.Get("/", h.get)
		r.Get("/members", h.members)

		r.Group(func(r chi.Router) {
			r.Use(RequireOwner(h.log))
			r.Patch("/", h.update)
			r.Delete("/members/{memberId}", h.deactivateMember)
			r.Post("/invites", h.createInvite)
			r.Get("/invites", h.listInvites)
			r.Delete("/invites/{inviteId}", h.revokeInvite)
		})
	})
}

// RequireMember loads the caller's active membership in {householdId}. Unknown households,
// malformed IDs and households the caller does not belong to all answer 404.
func (h *Handler) RequireMember(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := authctx.UserFrom(r.Context())
		if !ok {
			httpx.WriteError(w, r, h.log, kernel.ErrUnauthenticated)
			return
		}
		householdID, err := uuid.Parse(chi.URLParam(r, "householdId"))
		if err != nil {
			httpx.WriteError(w, r, h.log, errHouseholdNotFound)
			return
		}
		m, err := h.svc.Membership(r.Context(), householdID, user.ID)
		if err != nil {
			httpx.WriteError(w, r, h.log, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(authctx.WithMembership(r.Context(), m)))
	})
}

// RequireOwner allows only OWNER members; it must run after RequireMember.
func RequireOwner(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			m, ok := authctx.MembershipFrom(r.Context())
			if !ok || m.Role != authctx.RoleOwner {
				httpx.WriteError(w, r, log, kernel.NewError(kernel.ErrForbidden, "OWNER_REQUIRED",
					"Apenas responsáveis (OWNER) podem fazer isso."))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type householdResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Timezone  string    `json:"timezone"`
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"createdAt"`
	MemberID  string    `json:"memberId"`
	Role      string    `json:"role"`
}

func toHouseholdResponse(h Household) householdResponse {
	return householdResponse{
		ID: h.ID.String(), Name: h.Name, Timezone: h.Timezone, Currency: h.Currency,
		CreatedAt: h.CreatedAt, MemberID: h.MemberID.String(), Role: string(h.Role),
	}
}

type listResponse[T any] struct {
	Items []T `json:"items"`
}

type createHouseholdRequest struct {
	Name     string `json:"name" validate:"required,max=100"`
	Timezone string `json:"timezone,omitempty" validate:"omitempty,max=64"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createHouseholdRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	user, _ := authctx.UserFrom(r.Context())
	hh, err := h.svc.Create(r.Context(), user, req.Name, req.Timezone)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toHouseholdResponse(hh))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user, _ := authctx.UserFrom(r.Context())
	list, err := h.svc.ListForUser(r.Context(), user.ID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	items := make([]householdResponse, 0, len(list))
	for _, hh := range list {
		items = append(items, toHouseholdResponse(hh))
	}
	httpx.WriteJSON(w, http.StatusOK, listResponse[householdResponse]{Items: items})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	m, _ := authctx.MembershipFrom(r.Context())
	hh, err := h.svc.Get(r.Context(), m)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toHouseholdResponse(hh))
}

type updateHouseholdRequest struct {
	Name     *string `json:"name,omitempty" validate:"omitempty,max=100"`
	Timezone *string `json:"timezone,omitempty" validate:"omitempty,max=64"`
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req updateHouseholdRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	hh, err := h.svc.Update(r.Context(), m, UpdateInput(req))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toHouseholdResponse(hh))
}

type memberResponse struct {
	ID       string    `json:"id"`
	UserID   string    `json:"userId"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	Status   string    `json:"status"`
	JoinedAt time.Time `json:"joinedAt"`
}

func (h *Handler) members(w http.ResponseWriter, r *http.Request) {
	m, _ := authctx.MembershipFrom(r.Context())
	list, err := h.svc.Members(r.Context(), m)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	items := make([]memberResponse, 0, len(list))
	for _, mb := range list {
		items = append(items, memberResponse{
			ID: mb.ID.String(), UserID: mb.UserID.String(), Name: mb.Name, Email: mb.Email,
			Role: string(mb.Role), Status: mb.Status, JoinedAt: mb.JoinedAt,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, listResponse[memberResponse]{Items: items})
}

func (h *Handler) deactivateMember(w http.ResponseWriter, r *http.Request) {
	memberID, err := uuid.Parse(chi.URLParam(r, "memberId"))
	if err != nil {
		httpx.WriteError(w, r, h.log, kernel.NewError(kernel.ErrNotFound, "MEMBER_NOT_FOUND", "Membro não encontrado."))
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	if err := h.svc.DeactivateMember(r.Context(), m, memberID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type inviteResponse struct {
	ID        string    `json:"id"`
	Email     *string   `json:"email"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

type createdInviteResponse struct {
	inviteResponse
	Token string `json:"token"`
	URL   string `json:"url"`
}

type createInviteRequest struct {
	Email *string `json:"email,omitempty" validate:"omitempty,email,max=254"`
}

func (h *Handler) createInvite(w http.ResponseWriter, r *http.Request) {
	var req createInviteRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	inv, err := h.svc.CreateInvite(r.Context(), m, req.Email)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, createdInviteResponse{
		inviteResponse: toInviteResponse(inv.Invite), Token: inv.Token, URL: inv.URL,
	})
}

func (h *Handler) listInvites(w http.ResponseWriter, r *http.Request) {
	m, _ := authctx.MembershipFrom(r.Context())
	list, err := h.svc.ListInvites(r.Context(), m)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	items := make([]inviteResponse, 0, len(list))
	for _, inv := range list {
		items = append(items, toInviteResponse(inv))
	}
	httpx.WriteJSON(w, http.StatusOK, listResponse[inviteResponse]{Items: items})
}

func (h *Handler) revokeInvite(w http.ResponseWriter, r *http.Request) {
	inviteID, err := uuid.Parse(chi.URLParam(r, "inviteId"))
	if err != nil {
		httpx.WriteError(w, r, h.log, kernel.NewError(kernel.ErrNotFound, "INVITE_NOT_FOUND", "Convite não encontrado."))
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	if err := h.svc.RevokeInvite(r.Context(), m, inviteID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type acceptInviteRequest struct {
	Token string `json:"token" validate:"required,max=128"`
}

func (h *Handler) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var req acceptInviteRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	user, _ := authctx.UserFrom(r.Context())
	hh, err := h.svc.AcceptInvite(r.Context(), user, req.Token)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toHouseholdResponse(hh))
}

func toInviteResponse(inv Invite) inviteResponse {
	return inviteResponse{ID: inv.ID.String(), Email: inv.Email, ExpiresAt: inv.ExpiresAt, CreatedAt: inv.CreatedAt}
}
