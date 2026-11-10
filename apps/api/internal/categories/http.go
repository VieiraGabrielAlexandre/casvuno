package categories

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

// Handler exposes category routes. Any active member may manage categories (they are part
// of operating the family finances, not household administration).
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler builds the categories HTTP handler.
func NewHandler(svc *Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Routes registers routes relative to /households/{householdId}; the caller must have
// applied households.RequireMember.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/categories", h.list)
	r.Post("/categories", h.create)
	r.Patch("/categories/{categoryId}", h.update)
	r.Delete("/categories/{categoryId}", h.archive)
}

type categoryResponse struct {
	ID        string    `json:"id"`
	ParentID  *string   `json:"parentId"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Icon      *string   `json:"icon"`
	Color     *string   `json:"color"`
	IsActive  bool      `json:"isActive"`
	SortOrder int       `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
}

func toResponse(c Category) categoryResponse {
	var parent *string
	if c.ParentID != nil {
		p := c.ParentID.String()
		parent = &p
	}
	return categoryResponse{
		ID: c.ID.String(), ParentID: parent, Name: c.Name, Kind: string(c.Kind), Icon: c.Icon, Color: c.Color,
		IsActive: c.IsActive, SortOrder: c.SortOrder, CreatedAt: c.CreatedAt,
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	var kind *Kind
	if v := r.URL.Query().Get("kind"); v != "" {
		k := Kind(v)
		if !k.valid() {
			httpx.WriteError(w, r, h.log, validation("kind", "oneof=INCOME EXPENSE", "Tipo de categoria inválido."))
			return
		}
		kind = &k
	}
	includeArchived := r.URL.Query().Get("includeArchived") == "true"

	m, _ := authctx.MembershipFrom(r.Context())
	list, err := h.svc.List(r.Context(), m, kind, includeArchived)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	items := make([]categoryResponse, 0, len(list))
	for _, c := range list {
		items = append(items, toResponse(c))
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []categoryResponse `json:"items"`
	}{items})
}

type createRequest struct {
	Name      string  `json:"name" validate:"required,max=60"`
	Kind      string  `json:"kind" validate:"required,oneof=INCOME EXPENSE"`
	ParentID  *string `json:"parentId,omitempty" validate:"omitempty,uuid"`
	Icon      *string `json:"icon,omitempty" validate:"omitempty,max=32"`
	Color     *string `json:"color,omitempty" validate:"omitempty,len=7"`
	SortOrder int     `json:"sortOrder,omitempty" validate:"min=0,max=10000"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	var parent *uuid.UUID
	if req.ParentID != nil {
		id := uuid.MustParse(*req.ParentID) // validated by the uuid tag
		parent = &id
	}
	m, _ := authctx.MembershipFrom(r.Context())
	c, err := h.svc.Create(r.Context(), m, CreateInput{
		Name: req.Name, Kind: Kind(req.Kind), ParentID: parent, Icon: req.Icon, Color: req.Color, SortOrder: req.SortOrder,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(c))
}

// Icon and Color accept "" to clear the value, so their format is validated by the service.
type updateRequest struct {
	Name      *string `json:"name,omitempty" validate:"omitempty,max=60"`
	Icon      *string `json:"icon,omitempty"`
	Color     *string `json:"color,omitempty"`
	SortOrder *int    `json:"sortOrder,omitempty" validate:"omitempty,min=0,max=10000"`
	IsActive  *bool   `json:"isActive,omitempty"`
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.categoryID(w, r)
	if !ok {
		return
	}
	var req updateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	c, err := h.svc.Update(r.Context(), m, id, UpdateInput(req))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(c))
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request) {
	id, ok := h.categoryID(w, r)
	if !ok {
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	if err := h.svc.Archive(r.Context(), m, id); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) categoryID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "categoryId"))
	if err != nil {
		httpx.WriteError(w, r, h.log, kernel.NewError(kernel.ErrNotFound, "CATEGORY_NOT_FOUND", "Categoria não encontrada."))
		return uuid.Nil, false
	}
	return id, true
}
