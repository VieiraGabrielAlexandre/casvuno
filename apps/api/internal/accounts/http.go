package accounts

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

// Handler exposes account and transfer routes; any active member may operate them.
type Handler struct {
	svc  *Service
	idem *idempotency.Middleware
	log  *slog.Logger
}

// NewHandler builds the accounts HTTP handler.
func NewHandler(svc *Service, idem *idempotency.Middleware, log *slog.Logger) *Handler {
	return &Handler{svc: svc, idem: idem, log: log}
}

// Routes registers routes relative to /households/{householdId} (behind RequireMember).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/accounts", h.list)
	r.Post("/accounts", h.create)
	r.Get("/accounts/{accountId}", h.get)
	r.Patch("/accounts/{accountId}", h.update)
	r.Get("/accounts/{accountId}/movements", h.statement)

	r.Get("/transfers", h.listTransfers)
	r.With(h.idem.Require).Post("/transfers", h.createTransfer) // ADR 0007
}

type accountResponse struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Type                string    `json:"type"`
	Ownership           string    `json:"ownership"`
	OwnerMemberID       *string   `json:"ownerMemberId"`
	OpeningBalanceCents int64     `json:"openingBalanceCents"`
	OpenedOn            string    `json:"openedOn"`
	BalanceCents        int64     `json:"balanceCents"`
	IsActive            bool      `json:"isActive"`
	CreatedAt           time.Time `json:"createdAt"`
}

func toAccountResponse(a Account) accountResponse {
	var owner *string
	if a.OwnerMemberID != nil {
		s := a.OwnerMemberID.String()
		owner = &s
	}
	return accountResponse{
		ID: a.ID.String(), Name: a.Name, Type: a.Type, Ownership: a.Ownership, OwnerMemberID: owner,
		OpeningBalanceCents: a.OpeningBalanceCents, OpenedOn: kernel.FormatDate(a.OpenedOn),
		BalanceCents: a.BalanceCents, IsActive: a.IsActive, CreatedAt: a.CreatedAt,
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	m, _ := authctx.MembershipFrom(r.Context())
	list, total, err := h.svc.List(r.Context(), m, r.URL.Query().Get("includeArchived") == "true")
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	items := make([]accountResponse, 0, len(list))
	for _, a := range list {
		items = append(items, toAccountResponse(a))
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []accountResponse `json:"items"`
		// TotalBalanceCents is cash (caixa): Σ balances, each account counted once.
		TotalBalanceCents int64 `json:"totalBalanceCents"`
	}{items, total})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := h.accountID(w, r)
	if !ok {
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	a, err := h.svc.Get(r.Context(), m, id)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toAccountResponse(a))
}

type createRequest struct {
	Name                string  `json:"name" validate:"required,max=60"`
	Type                string  `json:"type" validate:"required"`
	Ownership           string  `json:"ownership" validate:"required"`
	OwnerMemberID       *string `json:"ownerMemberId,omitempty" validate:"omitempty,uuid"`
	OpeningBalanceCents int64   `json:"openingBalanceCents"`
	OpenedOn            string  `json:"openedOn" validate:"required"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	openedOn, err := parseDateField("openedOn", req.OpenedOn)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	a, err := h.svc.Create(r.Context(), m, CreateInput{
		Name: req.Name, Type: req.Type, Ownership: req.Ownership, OwnerMemberID: parseUUIDPtr(req.OwnerMemberID),
		OpeningBalanceCents: req.OpeningBalanceCents, OpenedOn: openedOn,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toAccountResponse(a))
}

type updateRequest struct {
	Name                *string `json:"name,omitempty" validate:"omitempty,max=60"`
	Type                *string `json:"type,omitempty"`
	Ownership           *string `json:"ownership,omitempty"`
	OwnerMemberID       *string `json:"ownerMemberId,omitempty" validate:"omitempty,uuid"`
	OpeningBalanceCents *int64  `json:"openingBalanceCents,omitempty"`
	OpenedOn            *string `json:"openedOn,omitempty"`
	IsActive            *bool   `json:"isActive,omitempty"`
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.accountID(w, r)
	if !ok {
		return
	}
	var req updateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	in := UpdateInput{
		Name: req.Name, Type: req.Type, Ownership: req.Ownership, OwnerMemberID: parseUUIDPtr(req.OwnerMemberID),
		OpeningBalanceCents: req.OpeningBalanceCents, IsActive: req.IsActive,
	}
	if req.OpenedOn != nil {
		d, err := parseDateField("openedOn", *req.OpenedOn)
		if err != nil {
			httpx.WriteError(w, r, h.log, err)
			return
		}
		in.OpenedOn = &d
	}
	m, _ := authctx.MembershipFrom(r.Context())
	a, err := h.svc.Update(r.Context(), m, id, in)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toAccountResponse(a))
}

type movementResponse struct {
	ID           string    `json:"id"`
	Direction    string    `json:"direction"`
	AmountCents  int64     `json:"amountCents"`
	PostedOn     string    `json:"postedOn"`
	Kind         string    `json:"kind"`
	TransferID   *string   `json:"transferId"`
	ReversalOfID *string   `json:"reversalOfId"`
	Description  string    `json:"description"`
	CreatedAt    time.Time `json:"createdAt"`
}

// cursorPosition is the wire form of StatementPosition inside the opaque cursor.
type cursorPosition struct {
	PostedOn string `json:"p"`
	ID       string `json:"i"`
}

func (h *Handler) statement(w http.ResponseWriter, r *http.Request) {
	id, ok := h.accountID(w, r)
	if !ok {
		return
	}
	q, err := parseStatementQuery(r)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	movements, next, err := h.svc.Statement(r.Context(), m, id, q)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	page := httpx.Page[movementResponse]{Items: make([]movementResponse, 0, len(movements))}
	for _, mv := range movements {
		page.Items = append(page.Items, movementResponse{
			ID: mv.ID.String(), Direction: mv.Direction, AmountCents: mv.AmountCents,
			PostedOn: kernel.FormatDate(mv.PostedOn), Kind: mv.Kind,
			TransferID: uuidString(mv.TransferID), ReversalOfID: uuidString(mv.ReversalOfID),
			Description: mv.Description, CreatedAt: mv.CreatedAt,
		})
	}
	if next != nil {
		c := httpx.EncodeCursor(cursorPosition{PostedOn: kernel.FormatDate(next.PostedOn), ID: next.ID.String()})
		page.NextCursor = &c
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func parseStatementQuery(r *http.Request) (StatementQuery, error) {
	return parsePageQuery(r)
}

// parsePageQuery reads limit, cursor and the from/to date filters shared by keyset lists.
func parsePageQuery(r *http.Request) (StatementQuery, error) {
	limit, err := httpx.ParseLimit(r)
	if err != nil {
		return StatementQuery{}, err
	}
	q := StatementQuery{Limit: limit}
	for field, dst := range map[string]**time.Time{"from": &q.From, "to": &q.To} {
		if raw := r.URL.Query().Get(field); raw != "" {
			d, err := parseDateField(field, raw)
			if err != nil {
				return StatementQuery{}, err
			}
			*dst = &d
		}
	}
	if q.From != nil && q.To != nil && q.From.After(*q.To) {
		return StatementQuery{}, validation("from", "before_or_equal_to", "A data inicial deve ser anterior à final.")
	}

	var pos cursorPosition
	if err := httpx.DecodeCursor(r, &pos); err != nil {
		return StatementQuery{}, err
	}
	if pos.ID != "" {
		d, errDate := kernel.ParseDate(pos.PostedOn)
		id, errID := uuid.Parse(pos.ID)
		if errDate != nil || errID != nil {
			return StatementQuery{}, validation("cursor", "invalid", "Cursor de paginação inválido.")
		}
		q.After = &StatementPosition{PostedOn: d, ID: id}
	}
	return q, nil
}

func (h *Handler) accountID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "accountId"))
	if err != nil {
		httpx.WriteError(w, r, h.log, errNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func parseDateField(field, raw string) (time.Time, error) {
	d, err := kernel.ParseDate(raw)
	if err != nil {
		return time.Time{}, validation(field, "date=YYYY-MM-DD", "Data inválida. Use AAAA-MM-DD.")
	}
	return d, nil
}

func uuidString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

// parseUUIDPtr converts an already validated (`uuid` tag) optional UUID.
func parseUUIDPtr(s *string) *uuid.UUID {
	if s == nil {
		return nil
	}
	id := uuid.MustParse(*s)
	return &id
}

type transferResponse struct {
	ID                string    `json:"id"`
	FromAccountID     string    `json:"fromAccountId"`
	FromAccountName   string    `json:"fromAccountName"`
	ToAccountID       string    `json:"toAccountId"`
	ToAccountName     string    `json:"toAccountName"`
	AmountCents       int64     `json:"amountCents"`
	PostedOn          string    `json:"postedOn"`
	Description       string    `json:"description"`
	CreatedByMemberID string    `json:"createdByMemberId"`
	CreatedAt         time.Time `json:"createdAt"`
}

func toTransferResponse(t Transfer) transferResponse {
	return transferResponse{
		ID: t.ID.String(), FromAccountID: t.FromAccountID.String(), FromAccountName: t.FromAccountName,
		ToAccountID: t.ToAccountID.String(), ToAccountName: t.ToAccountName, AmountCents: t.AmountCents,
		PostedOn: kernel.FormatDate(t.PostedOn), Description: t.Description,
		CreatedByMemberID: t.CreatedByMemberID.String(), CreatedAt: t.CreatedAt,
	}
}

type createTransferRequest struct {
	FromAccountID string `json:"fromAccountId" validate:"required,uuid"`
	ToAccountID   string `json:"toAccountId" validate:"required,uuid"`
	AmountCents   int64  `json:"amountCents" validate:"required"`
	PostedOn      string `json:"postedOn" validate:"required"`
	Description   string `json:"description,omitempty" validate:"max=200"`
}

func (h *Handler) createTransfer(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	postedOn, err := parseDateField("postedOn", req.PostedOn)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	m, _ := authctx.MembershipFrom(r.Context())
	t, err := h.svc.CreateTransfer(r.Context(), m, TransferInput{
		FromAccountID: uuid.MustParse(req.FromAccountID), ToAccountID: uuid.MustParse(req.ToAccountID), // validated by tags
		AmountCents: req.AmountCents, PostedOn: postedOn, Description: req.Description,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toTransferResponse(t))
}

func (h *Handler) listTransfers(w http.ResponseWriter, r *http.Request) {
	pq, err := parsePageQuery(r)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	q := TransferQuery{From: pq.From, To: pq.To, After: pq.After, Limit: pq.Limit}
	if raw := r.URL.Query().Get("accountId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.WriteError(w, r, h.log, validation("accountId", "uuid", "Conta inválida."))
			return
		}
		q.AccountID = &id
	}
	m, _ := authctx.MembershipFrom(r.Context())
	list, next, err := h.svc.ListTransfers(r.Context(), m, q)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	page := httpx.Page[transferResponse]{Items: make([]transferResponse, 0, len(list))}
	for _, t := range list {
		page.Items = append(page.Items, toTransferResponse(t))
	}
	if next != nil {
		c := httpx.EncodeCursor(cursorPosition{PostedOn: kernel.FormatDate(next.PostedOn), ID: next.ID.String()})
		page.NextCursor = &c
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}
