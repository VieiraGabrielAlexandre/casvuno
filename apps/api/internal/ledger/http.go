package ledger

import (
	"log/slog"
	"net/http"
	"slices"
	"strings"
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
	r.Get("/transactions", h.list)
	r.Get("/transactions/{transactionId}", h.get)
	r.Patch("/transactions/{transactionId}", h.update)
	r.With(h.idem.Require).Post("/transactions/{transactionId}/void", h.void) // ADR 0007

	r.Get("/obligations", h.listObligations)
	r.Get("/obligations/{obligationId}", h.getObligation)
	r.Patch("/obligations/{obligationId}", h.updateObligation)
	r.Post("/obligations/{obligationId}/cancel", h.cancelObligation)
	r.With(h.idem.Require).Post("/obligations/{obligationId}/payments", h.payObligation) // ADR 0007
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
	return transactionResponse{
		ID: t.ID.String(), Kind: t.Kind, Scope: t.Scope, PersonMemberID: uuidString(t.PersonMemberID),
		CreatedByMemberID: t.CreatedByMemberID.String(), Description: t.Description, Merchant: t.Merchant,
		Notes: t.Notes, AmountCents: t.AmountCents, CategoryID: t.CategoryID.String(),
		OccurredOn: kernel.FormatDate(t.OccurredOn), PaymentMethod: t.PaymentMethod, AccountID: t.AccountID.String(),
		Status: t.Status, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

type obligationResponse struct {
	ID                string    `json:"id"`
	TransactionID     *string   `json:"transactionId"`
	Kind              string    `json:"kind"`
	Source            string    `json:"source"`
	Status            string    `json:"status"`
	PaymentStatus     string    `json:"paymentStatus"`
	IsOverdue         bool      `json:"isOverdue"`
	AmountCents       int64     `json:"amountCents"`
	PaidCents         int64     `json:"paidCents"`
	OutstandingCents  int64     `json:"outstandingCents"`
	DueOn             string    `json:"dueOn"`
	Description       string    `json:"description"`
	CategoryID        string    `json:"categoryId"`
	Scope             string    `json:"scope"`
	PersonMemberID    *string   `json:"personMemberId"`
	PaymentMethod     string    `json:"paymentMethod"`
	AccountID         *string   `json:"accountId"`
	CompetenceOn      string    `json:"competenceOn"`
	CreatedByMemberID string    `json:"createdByMemberId"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

func toObligationResponse(o Obligation, today time.Time) obligationResponse {
	return obligationResponse{
		ID: o.ID.String(), TransactionID: uuidString(o.TransactionID), Kind: o.Kind, Source: o.Source,
		Status: o.Status, PaymentStatus: o.PaymentStatus(), IsOverdue: o.IsOverdue(today),
		AmountCents: o.AmountCents, PaidCents: o.PaidCents, OutstandingCents: o.OutstandingCents(),
		DueOn: kernel.FormatDate(o.DueOn), Description: o.Description, CategoryID: o.CategoryID.String(),
		Scope: o.Scope, PersonMemberID: uuidString(o.PersonMemberID), PaymentMethod: o.PaymentMethod,
		AccountID: uuidString(o.AccountID), CompetenceOn: kernel.FormatDate(o.CompetenceOn),
		CreatedByMemberID: o.CreatedByMemberID.String(), CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt,
	}
}

type paymentResponse struct {
	ID                string    `json:"id"`
	AmountCents       int64     `json:"amountCents"`
	PaidOn            string    `json:"paidOn"`
	AccountID         string    `json:"accountId"`
	MovementID        string    `json:"movementId"`
	CreatedByMemberID string    `json:"createdByMemberId"`
	CreatedAt         time.Time `json:"createdAt"`
}

type obligationDetailResponse struct {
	Obligation obligationResponse `json:"obligation"`
	Payments   []paymentResponse  `json:"payments"`
}

func toDetailResponse(d ObligationDetail, today time.Time) obligationDetailResponse {
	out := obligationDetailResponse{
		Obligation: toObligationResponse(d.Obligation, today),
		Payments:   make([]paymentResponse, 0, len(d.Payments)),
	}
	for _, p := range d.Payments {
		out.Payments = append(out.Payments, paymentResponse{
			ID: p.ID.String(), AmountCents: p.AmountCents, PaidOn: kernel.FormatDate(p.PaidOn),
			AccountID: p.AccountID.String(), MovementID: p.MovementID.String(),
			CreatedByMemberID: p.CreatedByMemberID.String(), CreatedAt: p.CreatedAt,
		})
	}
	return out
}

// entryResponse: an immediate entry has its transaction; a forecast has none yet.
type entryResponse struct {
	Transaction *transactionResponse `json:"transaction"`
	Obligation  obligationResponse   `json:"obligation"`
}

// The payment object is validated by the service, which reports nested field names
// ("payment.accountId") that the web form can map.
type paymentRequest struct {
	Method    string `json:"method"`
	AccountID string `json:"accountId,omitempty"`
	Mode      string `json:"mode"`
	DueOn     string `json:"dueOn,omitempty"`
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
	occurredOn, err := parseDate("occurredOn", req.OccurredOn)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	payment := Payment{Method: req.Payment.Method, Mode: req.Payment.Mode}
	if payment.AccountID, err = parseOptionalID("payment.accountId", req.Payment.AccountID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if req.Payment.DueOn != "" {
		d, err := parseDate("payment.dueOn", req.Payment.DueOn)
		if err != nil {
			httpx.WriteError(w, r, h.log, err)
			return
		}
		payment.DueOn = &d
	}
	m, _ := authctx.MembershipFrom(r.Context())
	e, err := h.svc.Create(r.Context(), m, CreateInput{
		Kind: req.Kind, Scope: req.Scope, PersonMemberID: optionalUUID(req.PersonMemberID), Description: req.Description,
		Merchant: req.Merchant, Notes: req.Notes, AmountCents: req.AmountCents,
		CategoryID: uuid.MustParse(req.CategoryID), // validated by tags
		OccurredOn: occurredOn, Payment: payment,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out := entryResponse{Obligation: toObligationResponse(e.Obligation, h.svc.Today(m))}
	if e.Transaction != nil {
		t := toResponse(*e.Transaction)
		out.Transaction = &t
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := h.transactionID(w, r)
	if !ok {
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

type transactionCursor struct {
	OccurredOn string `json:"d"`
	ID         string `json:"i"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q, err := parseTransactionQuery(r)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	list, next, err := h.svc.ListTransactions(r.Context(), m, q)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	page := httpx.Page[transactionResponse]{Items: make([]transactionResponse, 0, len(list))}
	for _, t := range list {
		page.Items = append(page.Items, toResponse(t))
	}
	if next != nil {
		c := httpx.EncodeCursor(transactionCursor{OccurredOn: kernel.FormatDate(next.OccurredOn), ID: next.ID.String()})
		page.NextCursor = &c
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func parseTransactionQuery(r *http.Request) (TransactionQuery, error) {
	limit, err := httpx.ParseLimit(r)
	if err != nil {
		return TransactionQuery{}, err
	}
	values := r.URL.Query()
	q := TransactionQuery{Status: values.Get("status"), Limit: limit}
	enums := []struct {
		field   string
		allowed []string
		dst     **string
	}{
		{"kind", []string{KindExpense, KindIncome}, &q.Kind},
		{"scope", []string{ScopePersonal, ScopeFamily}, &q.Scope},
		{"paymentMethod", []string{"PIX", "DEBIT", "CASH", "BANK_TRANSFER", "BOLETO", "OTHER"}, &q.PaymentMethod},
	}
	for _, e := range enums {
		if v := values.Get(e.field); v != "" {
			if !slices.Contains(e.allowed, v) {
				return q, validation(e.field, "oneof="+strings.Join(e.allowed, " "), "Filtro inválido.")
			}
			*e.dst = &v
		}
	}
	for field, dst := range map[string]**uuid.UUID{"categoryId": &q.CategoryID, "accountId": &q.AccountID, "personMemberId": &q.PersonMemberID} {
		if *dst, err = parseOptionalID(field, values.Get(field)); err != nil {
			return q, err
		}
	}
	for field, dst := range map[string]**time.Time{"from": &q.From, "to": &q.To} {
		if raw := values.Get(field); raw != "" {
			d, err := parseDate(field, raw)
			if err != nil {
				return q, err
			}
			*dst = &d
		}
	}
	if q.From != nil && q.To != nil && q.From.After(*q.To) {
		return q, validation("from", "before_or_equal_to", "A data inicial deve ser anterior à final.")
	}
	if text := values.Get("q"); text != "" {
		q.Text = &text
	}
	var pos transactionCursor
	if err := httpx.DecodeCursor(r, &pos); err != nil {
		return q, err
	}
	if pos.ID != "" {
		d, errDate := kernel.ParseDate(pos.OccurredOn)
		id, errID := uuid.Parse(pos.ID)
		if errDate != nil || errID != nil {
			return q, validation("cursor", "invalid", "Cursor de paginação inválido.")
		}
		q.After = &TransactionPosition{OccurredOn: d, ID: id}
	}
	return q, nil
}

// updateRequest: absent fields stay; "" clears merchant, notes and personMemberId.
type updateRequest struct {
	Description    *string `json:"description,omitempty"`
	Merchant       *string `json:"merchant,omitempty"`
	Notes          *string `json:"notes,omitempty"`
	CategoryID     *string `json:"categoryId,omitempty" validate:"omitempty,uuid"`
	Scope          *string `json:"scope,omitempty"`
	PersonMemberID *string `json:"personMemberId,omitempty"`
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.transactionID(w, r)
	if !ok {
		return
	}
	var req updateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	in := TransactionUpdate{
		Description: req.Description, Merchant: req.Merchant, Notes: req.Notes, Scope: req.Scope,
		PersonMemberID: req.PersonMemberID,
	}
	if req.CategoryID != nil {
		cid := uuid.MustParse(*req.CategoryID) // validated by tags
		in.CategoryID = &cid
	}
	m, _ := authctx.MembershipFrom(r.Context())
	t, err := h.svc.Update(r.Context(), m, id, in)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(t))
}

func (h *Handler) void(w http.ResponseWriter, r *http.Request) {
	id, ok := h.transactionID(w, r)
	if !ok {
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	t, err := h.svc.Void(r.Context(), m, id)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(t))
}

func (h *Handler) transactionID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "transactionId"))
	if err != nil {
		httpx.WriteError(w, r, h.log, errNotFound)
		return uuid.Nil, false
	}
	return id, true
}

type obligationCursor struct {
	DueOn string `json:"d"`
	ID    string `json:"i"`
}

func (h *Handler) listObligations(w http.ResponseWriter, r *http.Request) {
	q, err := parseObligationQuery(r)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	list, next, err := h.svc.ListObligations(r.Context(), m, q)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	today := h.svc.Today(m)
	page := httpx.Page[obligationResponse]{Items: make([]obligationResponse, 0, len(list))}
	for _, o := range list {
		page.Items = append(page.Items, toObligationResponse(o, today))
	}
	if next != nil {
		c := httpx.EncodeCursor(obligationCursor{DueOn: kernel.FormatDate(next.DueOn), ID: next.ID.String()})
		page.NextCursor = &c
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func parseObligationQuery(r *http.Request) (ObligationQuery, error) {
	limit, err := httpx.ParseLimit(r)
	if err != nil {
		return ObligationQuery{}, err
	}
	values := r.URL.Query()
	q := ObligationQuery{View: values.Get("view"), Limit: limit}
	if q.View == "" {
		q.View = ViewOpen
	}
	if kind := values.Get("kind"); kind != "" {
		if kind != "PAYABLE" && kind != "RECEIVABLE" {
			return q, validation("kind", "oneof=PAYABLE RECEIVABLE", "Tipo inválido.")
		}
		q.Kind = &kind
	}
	for field, dst := range map[string]**time.Time{"from": &q.From, "to": &q.To} {
		if raw := values.Get(field); raw != "" {
			d, err := parseDate(field, raw)
			if err != nil {
				return q, err
			}
			*dst = &d
		}
	}
	if q.From != nil && q.To != nil && q.From.After(*q.To) {
		return q, validation("from", "before_or_equal_to", "A data inicial deve ser anterior à final.")
	}
	var pos obligationCursor
	if err := httpx.DecodeCursor(r, &pos); err != nil {
		return q, err
	}
	if pos.ID != "" {
		d, errDate := kernel.ParseDate(pos.DueOn)
		id, errID := uuid.Parse(pos.ID)
		if errDate != nil || errID != nil {
			return q, validation("cursor", "invalid", "Cursor de paginação inválido.")
		}
		q.After = &ObligationPosition{DueOn: d, ID: id}
	}
	return q, nil
}

func (h *Handler) getObligation(w http.ResponseWriter, r *http.Request) {
	id, ok := h.obligationID(w, r)
	if !ok {
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	d, err := h.svc.GetObligation(r.Context(), m, id)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDetailResponse(d, h.svc.Today(m)))
}

type payRequest struct {
	AmountCents int64  `json:"amountCents" validate:"required"`
	PaidOn      string `json:"paidOn" validate:"required"`
	AccountID   string `json:"accountId,omitempty" validate:"omitempty,uuid"`
}

func (h *Handler) payObligation(w http.ResponseWriter, r *http.Request) {
	id, ok := h.obligationID(w, r)
	if !ok {
		return
	}
	var req payRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	paidOn, err := parseDate("paidOn", req.PaidOn)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	d, err := h.svc.PayObligation(r.Context(), m, id, PaymentInput{
		AmountCents: req.AmountCents, PaidOn: paidOn, AccountID: optionalUUID(req.AccountID),
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toDetailResponse(d, h.svc.Today(m)))
}

type updateObligationRequest struct {
	AmountCents *int64  `json:"amountCents,omitempty"`
	DueOn       *string `json:"dueOn,omitempty"`
	Description *string `json:"description,omitempty"`
}

func (h *Handler) updateObligation(w http.ResponseWriter, r *http.Request) {
	id, ok := h.obligationID(w, r)
	if !ok {
		return
	}
	var req updateObligationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	in := ForecastUpdate{AmountCents: req.AmountCents, Description: req.Description}
	if req.DueOn != nil {
		d, err := parseDate("dueOn", *req.DueOn)
		if err != nil {
			httpx.WriteError(w, r, h.log, err)
			return
		}
		in.DueOn = &d
	}
	m, _ := authctx.MembershipFrom(r.Context())
	d, err := h.svc.UpdateForecast(r.Context(), m, id, in)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDetailResponse(d, h.svc.Today(m)))
}

func (h *Handler) cancelObligation(w http.ResponseWriter, r *http.Request) {
	id, ok := h.obligationID(w, r)
	if !ok {
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	d, err := h.svc.CancelForecast(r.Context(), m, id)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDetailResponse(d, h.svc.Today(m)))
}

func (h *Handler) obligationID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "obligationId"))
	if err != nil {
		httpx.WriteError(w, r, h.log, errObligationNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func parseDate(field, raw string) (time.Time, error) {
	d, err := kernel.ParseDate(raw)
	if err != nil {
		return time.Time{}, validation(field, "date=YYYY-MM-DD", "Data inválida. Use AAAA-MM-DD.")
	}
	return d, nil
}

func parseOptionalID(field, raw string) (*uuid.UUID, error) {
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, validation(field, "uuid", "Identificador inválido.")
	}
	return &id, nil
}

// optionalUUID parses an ID already validated by the `uuid` tag; empty means absent.
func optionalUUID(raw string) *uuid.UUID {
	if raw == "" {
		return nil
	}
	id := uuid.MustParse(raw)
	return &id
}

func uuidString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}
