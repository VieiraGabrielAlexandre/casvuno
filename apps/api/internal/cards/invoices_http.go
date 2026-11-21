package cards

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/httpx"
)

type invoiceResponse struct {
	ID                    string     `json:"id"`
	CreditCardID          string     `json:"creditCardId"`
	CycleStartOn          string     `json:"cycleStartOn"`
	ClosingOn             string     `json:"closingOn"`
	DueOn                 string     `json:"dueOn"`
	ReferenceMonth        string     `json:"referenceMonth"`
	Status                string     `json:"status"`
	PaymentStatus         string     `json:"paymentStatus"`
	ClosingPending        bool       `json:"closingPending"`
	InstallmentsCount     int        `json:"installmentsCount"`
	InstallmentsCents     int64      `json:"installmentsCents"`
	AdjustmentsCents      int64      `json:"adjustmentsCents"`
	TotalCents            int64      `json:"totalCents"`
	PaidCents             int64      `json:"paidCents"`
	OutstandingCents      int64      `json:"outstandingCents"`
	ClosedBaseAmountCents *int64     `json:"closedBaseAmountCents"`
	ClosedAt              *time.Time `json:"closedAt"`
	ClosedByMemberID      *string    `json:"closedByMemberId"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

func toInvoiceResponse(i Invoice, today time.Time) invoiceResponse {
	r := invoiceResponse{
		ID: i.ID.String(), CreditCardID: i.CardID.String(), CycleStartOn: kernel.FormatDate(i.CycleStartOn),
		ClosingOn: kernel.FormatDate(i.ClosingOn), DueOn: kernel.FormatDate(i.DueOn),
		ReferenceMonth: i.DueOn.Format("2006-01"), // ADR 0003: the month of the due date
		Status:         i.Status, PaymentStatus: i.PaymentStatus(), ClosingPending: i.ClosingPending(today),
		InstallmentsCount: i.InstallmentsCount, InstallmentsCents: i.InstallmentsCents, AdjustmentsCents: i.AdjustmentsCents,
		TotalCents: i.TotalCents(), PaidCents: i.PaidCents, OutstandingCents: i.OutstandingCents(),
		ClosedBaseAmountCents: i.ClosedBaseAmountCents, ClosedAt: i.ClosedAt,
		CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt,
	}
	if i.ClosedByMemberID != nil {
		id := i.ClosedByMemberID.String()
		r.ClosedByMemberID = &id
	}
	return r
}

type invoiceInstallmentResponse struct {
	ID                  string  `json:"id"`
	TransactionID       string  `json:"transactionId"`
	Number              int     `json:"number"`
	Total               int     `json:"total"`
	AmountCents         int64   `json:"amountCents"`
	Status              string  `json:"status"`
	OccurredOn          string  `json:"occurredOn"`
	Description         string  `json:"description"`
	Merchant            *string `json:"merchant"`
	CategoryID          string  `json:"categoryId"`
	Scope               string  `json:"scope"`
	PersonMemberID      *string `json:"personMemberId"`
	PurchaseAmountCents int64   `json:"purchaseAmountCents"`
}

type invoicePaymentResponse struct {
	ID                string    `json:"id"`
	AmountCents       int64     `json:"amountCents"`
	PaidOn            string    `json:"paidOn"`
	AccountID         string    `json:"accountId"`
	MovementID        string    `json:"movementId"`
	CreatedByMemberID string    `json:"createdByMemberId"`
	CreatedAt         time.Time `json:"createdAt"`
}

type invoiceAdjustmentResponse struct {
	ID                string    `json:"id"`
	AmountCents       int64     `json:"amountCents"`
	Reason            string    `json:"reason"`
	CreatedByMemberID string    `json:"createdByMemberId"`
	CreatedAt         time.Time `json:"createdAt"`
}

// invoiceDetailResponse follows ObligationDetail: the invoice, its lines, its payments and
// its adjustments.
type invoiceDetailResponse struct {
	Invoice      invoiceResponse              `json:"invoice"`
	Installments []invoiceInstallmentResponse `json:"installments"`
	Payments     []invoicePaymentResponse     `json:"payments"`
	Adjustments  []invoiceAdjustmentResponse  `json:"adjustments"`
}

func toInvoiceDetailResponse(d InvoiceDetail, today time.Time) invoiceDetailResponse {
	out := invoiceDetailResponse{
		Invoice:      toInvoiceResponse(d.Invoice, today),
		Installments: make([]invoiceInstallmentResponse, 0, len(d.Installments)),
		Payments:     make([]invoicePaymentResponse, 0, len(d.Payments)),
		Adjustments:  make([]invoiceAdjustmentResponse, 0, len(d.Adjustments)),
	}
	for _, p := range d.Payments {
		out.Payments = append(out.Payments, invoicePaymentResponse{
			ID: p.ID.String(), AmountCents: p.AmountCents, PaidOn: kernel.FormatDate(p.PaidOn), AccountID: p.AccountID.String(),
			MovementID: p.MovementID.String(), CreatedByMemberID: p.CreatedByMemberID.String(), CreatedAt: p.CreatedAt,
		})
	}
	for _, a := range d.Adjustments {
		out.Adjustments = append(out.Adjustments, invoiceAdjustmentResponse{
			ID: a.ID.String(), AmountCents: a.AmountCents, Reason: a.Reason,
			CreatedByMemberID: a.CreatedByMemberID.String(), CreatedAt: a.CreatedAt,
		})
	}
	for _, i := range d.Installments {
		item := invoiceInstallmentResponse{
			ID: i.ID.String(), TransactionID: i.TransactionID.String(), Number: i.Number, Total: i.Total,
			AmountCents: i.AmountCents, Status: i.Status, OccurredOn: kernel.FormatDate(i.OccurredOn),
			Description: i.Description, Merchant: i.Merchant, CategoryID: i.CategoryID.String(), Scope: i.Scope,
			PurchaseAmountCents: i.PurchaseAmountCents,
		}
		if i.PersonMemberID != nil {
			id := i.PersonMemberID.String()
			item.PersonMemberID = &id
		}
		out.Installments = append(out.Installments, item)
	}
	return out
}

type invoiceCursor struct {
	DueOn string `json:"d"`
	ID    string `json:"i"`
}

func (h *Handler) listInvoices(w http.ResponseWriter, r *http.Request) {
	cardID, ok := h.cardID(w, r)
	if !ok {
		return
	}
	q, err := parseInvoiceQuery(r)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	list, next, err := h.svc.ListInvoices(r.Context(), m, cardID, q)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	today := h.svc.Today(m)
	page := httpx.Page[invoiceResponse]{Items: make([]invoiceResponse, 0, len(list))}
	for _, i := range list {
		page.Items = append(page.Items, toInvoiceResponse(i, today))
	}
	if next != nil {
		c := httpx.EncodeCursor(invoiceCursor{DueOn: kernel.FormatDate(next.DueOn), ID: next.ID.String()})
		page.NextCursor = &c
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func parseInvoiceQuery(r *http.Request) (InvoiceQuery, error) {
	limit, err := httpx.ParseLimit(r)
	if err != nil {
		return InvoiceQuery{}, err
	}
	q := InvoiceQuery{Limit: limit}
	values := r.URL.Query()
	for field, dst := range map[string]**time.Time{"from": &q.From, "to": &q.To} {
		if raw := values.Get(field); raw != "" {
			d, err := kernel.ParseDate(raw)
			if err != nil {
				return q, validation(field, "date=YYYY-MM-DD", "Data inválida. Use AAAA-MM-DD.")
			}
			*dst = &d
		}
	}
	if q.From != nil && q.To != nil && q.From.After(*q.To) {
		return q, validation("from", "before_or_equal_to", "A data inicial deve ser anterior à final.")
	}
	var pos invoiceCursor
	if err := httpx.DecodeCursor(r, &pos); err != nil {
		return q, err
	}
	if pos.ID != "" {
		d, errDate := kernel.ParseDate(pos.DueOn)
		id, errID := uuid.Parse(pos.ID)
		if errDate != nil || errID != nil {
			return q, validation("cursor", "invalid", "Cursor de paginação inválido.")
		}
		q.After = &InvoicePosition{DueOn: d, ID: id}
	}
	return q, nil
}

func (h *Handler) getInvoice(w http.ResponseWriter, r *http.Request) {
	id, ok := h.invoiceID(w, r)
	if !ok {
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	d, err := h.svc.GetInvoice(r.Context(), m, id)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toInvoiceDetailResponse(d, h.svc.Today(m)))
}

func (h *Handler) closeInvoice(w http.ResponseWriter, r *http.Request) {
	id, ok := h.invoiceID(w, r)
	if !ok {
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	d, err := h.svc.CloseInvoice(r.Context(), m, id)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toInvoiceDetailResponse(d, h.svc.Today(m)))
}

type payInvoiceRequest struct {
	AmountCents int64  `json:"amountCents" validate:"required"`
	PaidOn      string `json:"paidOn" validate:"required"`
	AccountID   string `json:"accountId" validate:"required,uuid"`
}

func (h *Handler) payInvoice(w http.ResponseWriter, r *http.Request) {
	id, ok := h.invoiceID(w, r)
	if !ok {
		return
	}
	var req payInvoiceRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	paidOn, err := kernel.ParseDate(req.PaidOn)
	if err != nil {
		httpx.WriteError(w, r, h.log, validation("paidOn", "date=YYYY-MM-DD", "Data inválida. Use AAAA-MM-DD."))
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	d, err := h.svc.PayInvoice(r.Context(), m, id, PaymentInput{
		AmountCents: req.AmountCents, PaidOn: paidOn, AccountID: uuid.MustParse(req.AccountID), // validated by tags
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toInvoiceDetailResponse(d, h.svc.Today(m)))
}

type adjustInvoiceRequest struct {
	AmountCents int64  `json:"amountCents" validate:"required"`
	Reason      string `json:"reason" validate:"required"`
}

func (h *Handler) adjustInvoice(w http.ResponseWriter, r *http.Request) {
	id, ok := h.invoiceID(w, r)
	if !ok {
		return
	}
	var req adjustInvoiceRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	d, err := h.svc.AdjustInvoice(r.Context(), m, id, AdjustmentInput(req))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toInvoiceDetailResponse(d, h.svc.Today(m)))
}

func (h *Handler) invoiceID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "invoiceId"))
	if err != nil {
		httpx.WriteError(w, r, h.log, errInvoiceNotFound)
		return uuid.Nil, false
	}
	return id, true
}
