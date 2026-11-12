package cards

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/httpx"
)

// Handler exposes card routes; any active member may operate them.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler builds the cards HTTP handler.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Routes registers routes relative to /households/{householdId} (behind RequireMember).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/cards", h.list)
	r.Post("/cards", h.create)
	r.Get("/cards/{cardId}", h.get)
	r.Patch("/cards/{cardId}", h.update)
}

type cardResponse struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	HolderMemberID string    `json:"holderMemberId"`
	LastFour       *string   `json:"lastFour"`
	LimitCents     int64     `json:"limitCents"`
	ClosingDay     int       `json:"closingDay"`
	DueDay         int       `json:"dueDay"`
	IsActive       bool      `json:"isActive"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func toCardResponse(c Card) cardResponse {
	return cardResponse{
		ID: c.ID.String(), Name: c.Name, HolderMemberID: c.HolderMemberID.String(), LastFour: c.LastFour,
		LimitCents: c.LimitCents, ClosingDay: c.ClosingDay, DueDay: c.DueDay, IsActive: c.IsActive,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	m, _ := authctx.MembershipFrom(r.Context())
	list, err := h.svc.List(r.Context(), m, r.URL.Query().Get("includeArchived") == "true")
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	items := make([]cardResponse, 0, len(list))
	for _, c := range list {
		items = append(items, toCardResponse(c))
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []cardResponse `json:"items"`
	}{items})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := h.cardID(w, r)
	if !ok {
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	c, err := h.svc.Get(r.Context(), m, id)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toCardResponse(c))
}

type createRequest struct {
	Name           string  `json:"name" validate:"required,max=60"`
	HolderMemberID string  `json:"holderMemberId" validate:"required,uuid"`
	LastFour       *string `json:"lastFour,omitempty"`
	LimitCents     int64   `json:"limitCents" validate:"required"`
	ClosingDay     int     `json:"closingDay" validate:"required,min=1,max=31"`
	DueDay         int     `json:"dueDay" validate:"required,min=1,max=31"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	c, err := h.svc.Create(r.Context(), m, CreateInput{
		Name: req.Name, HolderMemberID: uuid.MustParse(req.HolderMemberID), // validated by tags
		LastFour: req.LastFour, LimitCents: req.LimitCents, ClosingDay: req.ClosingDay, DueDay: req.DueDay,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toCardResponse(c))
}

type updateRequest struct {
	Name           *string `json:"name,omitempty" validate:"omitempty,max=60"`
	HolderMemberID *string `json:"holderMemberId,omitempty" validate:"omitempty,uuid"`
	LastFour       *string `json:"lastFour,omitempty"`
	LimitCents     *int64  `json:"limitCents,omitempty"`
	ClosingDay     *int    `json:"closingDay,omitempty" validate:"omitempty,min=1,max=31"`
	DueDay         *int    `json:"dueDay,omitempty" validate:"omitempty,min=1,max=31"`
	IsActive       *bool   `json:"isActive,omitempty"`
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.cardID(w, r)
	if !ok {
		return
	}
	var req updateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	in := UpdateInput{
		Name: req.Name, LastFour: req.LastFour, LimitCents: req.LimitCents,
		ClosingDay: req.ClosingDay, DueDay: req.DueDay, IsActive: req.IsActive,
	}
	if req.HolderMemberID != nil {
		holder := uuid.MustParse(*req.HolderMemberID) // validated by tags
		in.HolderMemberID = &holder
	}
	m, _ := authctx.MembershipFrom(r.Context())
	c, err := h.svc.Update(r.Context(), m, id, in)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toCardResponse(c))
}

func (h *Handler) cardID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "cardId"))
	if err != nil {
		httpx.WriteError(w, r, h.log, errNotFound)
		return uuid.Nil, false
	}
	return id, true
}
