package ledger

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/httpx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/idempotency"
)

// Handler exposes the ledger routes; any active member may operate them (RN-002).
type Handler struct {
	svc  *Service
	idem *idempotency.Middleware
	log  *slog.Logger
}

// NewHandler builds the ledger HTTP handler.
func NewHandler(svc *Service, idem *idempotency.Middleware, log *slog.Logger) *Handler {
	return &Handler{svc: svc, idem: idem, log: log}
}

// Routes registers routes relative to /households/{householdId} (behind RequireMember).
func (h *Handler) Routes(r chi.Router) {
	r.With(h.idem.Require).Post("/transactions", h.create) // ADR 0007
	r.Get("/transactions/{transactionId}", h.get)
}

type transactionResponse struct {
	ID                string    `json:"id"`
	Kind              string    `json:"kind"`
	Scope             string    `json:"scope"`
	PersonMemberID    *string   `json:"personMemberId"`
	CreatedByMemberID string    `json:"createdByMemberId"`
	Description       string    `json:"description"`
	Merchant          *string   `json:"merchant"`
	Notes             *string   `json:"notes"`
	AmountCents       int64     `json:"amountCents"`
	CategoryID        string    `json:"categoryId"`
	OccurredOn        string    `json:"occurredOn"`
	PaymentMethod     string    `json:"paymentMethod"`
	AccountID         string    `json:"accountId"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

func toResponse(t Transaction) transactionResponse {
	var person *string
	if t.PersonMemberID != nil {
		s := t.PersonMemberID.String()
		person = &s
	}
	return transactionResponse{
		ID: t.ID.String(), Kind: t.Kind, Scope: t.Scope, PersonMemberID: person,
		CreatedByMemberID: t.CreatedByMemberID.String(), Description: t.Description, Merchant: t.Merchant,
		Notes: t.Notes, AmountCents: t.AmountCents, CategoryID: t.CategoryID.String(),
		OccurredOn: kernel.FormatDate(t.OccurredOn), PaymentMethod: t.PaymentMethod, AccountID: t.AccountID.String(),
		Status: t.Status, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

// The payment object is validated by the service, which reports nested field names
// ("payment.accountId") that the web form can map.
type paymentRequest struct {
	Method    string `json:"method"`
	AccountID string `json:"accountId,omitempty"`
	Mode      string `json:"mode"`
}

type createRequest struct {
	Kind           string         `json:"kind" validate:"required"`
	Scope          string         `json:"scope" validate:"required"`
	PersonMemberID string         `json:"personMemberId,omitempty" validate:"omitempty,uuid"`
	Description    string         `json:"description" validate:"required"`
	Merchant       *string        `json:"merchant,omitempty"`
	Notes          *string        `json:"notes,omitempty"`
	AmountCents    int64          `json:"amountCents" validate:"required"`
	CategoryID     string         `json:"categoryId" validate:"required,uuid"`
	OccurredOn     string         `json:"occurredOn" validate:"required"`
	Payment        paymentRequest `json:"payment" validate:"required"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	occurredOn, err := kernel.ParseDate(req.OccurredOn)
	if err != nil {
		httpx.WriteError(w, r, h.log, validation("occurredOn", "date=YYYY-MM-DD", "Data inválida. Use AAAA-MM-DD."))
		return
	}
	var accountID *uuid.UUID
	if req.Payment.AccountID != "" {
		id, err := uuid.Parse(req.Payment.AccountID)
		if err != nil {
			httpx.WriteError(w, r, h.log, validation("payment.accountId", "uuid", "Conta inválida."))
			return
		}
		accountID = &id
	}
	m, _ := authctx.MembershipFrom(r.Context())
	t, err := h.svc.Create(r.Context(), m, CreateInput{
		Kind: req.Kind, Scope: req.Scope, PersonMemberID: optionalUUID(req.PersonMemberID), Description: req.Description,
		Merchant: req.Merchant, Notes: req.Notes, AmountCents: req.AmountCents,
		CategoryID: uuid.MustParse(req.CategoryID), // validated by tags
		OccurredOn: occurredOn,
		Payment:    Payment{Method: req.Payment.Method, AccountID: accountID, Mode: req.Payment.Mode},
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(t))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "transactionId"))
	if err != nil {
		httpx.WriteError(w, r, h.log, errNotFound)
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	t, err := h.svc.Get(r.Context(), m, id)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(t))
}

// optionalUUID parses an ID already validated by the `uuid` tag; empty means absent.
func optionalUUID(raw string) *uuid.UUID {
	if raw == "" {
		return nil
	}
	id := uuid.MustParse(raw)
	return &id
}
