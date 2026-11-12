package ledger

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/httpx"
)

type templateResponse struct {
	ID                 string    `json:"id"`
	Label              string    `json:"label"`
	Kind               string    `json:"kind"`
	Description        string    `json:"description"`
	DefaultAmountCents *int64    `json:"defaultAmountCents"`
	CategoryID         string    `json:"categoryId"`
	Scope              string    `json:"scope"`
	PersonMemberID     *string   `json:"personMemberId"`
	PaymentMethod      string    `json:"paymentMethod"`
	AccountID          *string   `json:"accountId"`
	SortOrder          int32     `json:"sortOrder"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

func toTemplateResponse(t QuickTemplate) templateResponse {
	return templateResponse{
		ID: t.ID.String(), Label: t.Label, Kind: t.Kind, Description: t.Description,
		DefaultAmountCents: t.DefaultAmountCents, CategoryID: t.CategoryID.String(), Scope: t.Scope,
		PersonMemberID: uuidString(t.PersonMemberID), PaymentMethod: t.PaymentMethod, AccountID: uuidString(t.AccountID),
		SortOrder: t.SortOrder, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

func (h *Handler) listTemplates(w http.ResponseWriter, r *http.Request) {
	m, _ := authctx.MembershipFrom(r.Context())
	list, err := h.svc.ListTemplates(r.Context(), m)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out := struct {
		Items []templateResponse `json:"items"`
	}{Items: make([]templateResponse, 0, len(list))}
	for _, t := range list {
		out.Items = append(out.Items, toTemplateResponse(t))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type createTemplateRequest struct {
	Label              string `json:"label" validate:"required"`
	Kind               string `json:"kind" validate:"required"`
	Description        string `json:"description" validate:"required"`
	DefaultAmountCents *int64 `json:"defaultAmountCents,omitempty"`
	CategoryID         string `json:"categoryId" validate:"required,uuid"`
	Scope              string `json:"scope" validate:"required"`
	PersonMemberID     string `json:"personMemberId,omitempty" validate:"omitempty,uuid"`
	PaymentMethod      string `json:"paymentMethod" validate:"required"`
	AccountID          string `json:"accountId,omitempty" validate:"omitempty,uuid"`
	SortOrder          *int32 `json:"sortOrder,omitempty"`
}

func (h *Handler) createTemplate(w http.ResponseWriter, r *http.Request) {
	var req createTemplateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	t, err := h.svc.CreateTemplate(r.Context(), m, TemplateInput{
		Label: req.Label, Kind: req.Kind, Description: req.Description, DefaultAmountCents: req.DefaultAmountCents,
		CategoryID: uuid.MustParse(req.CategoryID), // validated by tags
		Scope:      req.Scope, PersonMemberID: optionalUUID(req.PersonMemberID), PaymentMethod: req.PaymentMethod,
		AccountID: optionalUUID(req.AccountID), SortOrder: req.SortOrder,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toTemplateResponse(t))
}

// updateTemplateRequest: absent fields stay; "" clears personMemberId/accountId and
// defaultAmountCents 0 clears the amount.
type updateTemplateRequest struct {
	Label              *string `json:"label,omitempty"`
	Description        *string `json:"description,omitempty"`
	DefaultAmountCents *int64  `json:"defaultAmountCents,omitempty"`
	CategoryID         *string `json:"categoryId,omitempty" validate:"omitempty,uuid"`
	Scope              *string `json:"scope,omitempty"`
	PersonMemberID     *string `json:"personMemberId,omitempty"`
	PaymentMethod      *string `json:"paymentMethod,omitempty"`
	AccountID          *string `json:"accountId,omitempty"`
	SortOrder          *int32  `json:"sortOrder,omitempty"`
}

func (h *Handler) updateTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := h.templateID(w, r)
	if !ok {
		return
	}
	var req updateTemplateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	in := TemplateUpdate{
		Label: req.Label, Description: req.Description, DefaultAmountCents: req.DefaultAmountCents, Scope: req.Scope,
		PersonMemberID: req.PersonMemberID, PaymentMethod: req.PaymentMethod, AccountID: req.AccountID, SortOrder: req.SortOrder,
	}
	if req.CategoryID != nil {
		cid := uuid.MustParse(*req.CategoryID) // validated by tags
		in.CategoryID = &cid
	}
	m, _ := authctx.MembershipFrom(r.Context())
	t, err := h.svc.UpdateTemplate(r.Context(), m, id, in)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toTemplateResponse(t))
}

func (h *Handler) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := h.templateID(w, r)
	if !ok {
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	if err := h.svc.DeleteTemplate(r.Context(), m, id); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) templateID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "templateId"))
	if err != nil {
		httpx.WriteError(w, r, h.log, errTemplateNotFound)
		return uuid.Nil, false
	}
	return id, true
}
