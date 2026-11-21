// Package ledger owns income and expense entries (ADR 0002): the economic fact
// (`transactions`, the only source of consumption), what is payable or receivable directly
// through an account (`obligations`) and how it was settled (`obligation_payments`). Cash
// moves only through the accounts module.
package ledger

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/ledger/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/audit"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/clock"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
)

// Entry kinds, scopes and payment modes (README §6.3, §7.5).
const (
	KindExpense = "EXPENSE"
	KindIncome  = "INCOME"

	ScopePersonal = "PERSONAL"
	ScopeFamily   = "FAMILY"

	// ModeImmediate: paid or received now (transaction + payment + movement).
	ModeImmediate = "IMMEDIATE"
	// ModePlanned: a forecast (PLANNED obligation only); its first payment confirms it (I-08).
	ModePlanned = "PLANNED"

	// MethodCreditCard: a purchase on a card (RN-009): the transaction and N installments on
	// the card's invoices, no obligation and no cash until the invoice is paid (F4.5).
	MethodCreditCard = "CREDIT_CARD"
	// maxInstallments mirrors cards/domain.MaxInstallments (RN-010, I-32): the ledger checks
	// the request shape before taking any lock; the cards module checks it again on Split.
	maxInstallments = 60
)

// Obligation lifecycle (I-02) and sources.
const (
	StatusPlanned   = "PLANNED"
	StatusConfirmed = "CONFIRMED"
	StatusCancelled = "CANCELLED"

	sourceDirect         = "DIRECT"
	sourceManualForecast = "MANUAL_FORECAST"
)

// Payment methods through an account (I-07). CREDIT_CARD is the card's (MethodCreditCard).
var accountMethods = map[string]bool{
	"PIX": true, "DEBIT": true, "CASH": true, "BANK_TRANSFER": true, "BOLETO": true, "OTHER": true,
}

const (
	maxDescriptionLen = 200
	maxMerchantLen    = 120
	maxNotesLen       = 1000
)

// CategoryChecker locks a category for a new entry (the categories module implements it).
// found is false for unknown IDs and for categories of other households.
type CategoryChecker interface {
	LockForEntry(ctx context.Context, householdID, categoryID uuid.UUID) (kind string, active, found bool, err error)
}

// MemberChecker tells whether a member is active (the households module implements it).
type MemberChecker interface {
	IsActiveMember(ctx context.Context, householdID, memberID uuid.UUID) (bool, error)
}

// CashMovement is a cash movement the ledger asks the accounts module to post.
type CashMovement struct {
	AccountID   uuid.UUID
	Incoming    bool // IN/DIRECT_RECEIPT when true, OUT/DIRECT_PAYMENT otherwise
	AmountCents int64
	PostedOn    time.Time
	Description string
	Field       string // request field that carried the account, for error details
}

// CardPurchase is a card purchase whose transaction the ledger has just written.
type CardPurchase struct {
	CardID        uuid.UUID
	TransactionID uuid.UUID
	OccurredOn    time.Time
	AmountCents   int64
	Installments  int
}

// CardInstallment is one installment of a card purchase and the invoice it went to.
type CardInstallment struct {
	Number      int
	AmountCents int64
	InvoiceID   uuid.UUID
	ClosingOn   time.Time
	DueOn       time.Time
}

// Cards is what the ledger needs from the cards module. RequireActiveCard locks the card
// until the caller's transaction ends and rejects unknown or archived cards with 422 on
// field. PlaceInstallments splits the purchase and writes its installments on the card's
// invoices (ADR 0003), creating missing invoices; shifted reports a skipped CLOSED invoice.
type Cards interface {
	RequireActiveCard(ctx context.Context, householdID, cardID uuid.UUID, field string) error
	PlaceInstallments(ctx context.Context, householdID uuid.UUID, p CardPurchase) (installments []CardInstallment, shifted bool, err error)
	// CancelInstallments cancels the installments of a purchase being voided (RN-019), or
	// fails with 422 when an invoice is CLOSED or would be left below what was paid.
	CancelInstallments(ctx context.Context, householdID, cardID, transactionID uuid.UUID) error
}

// Cash is what the ledger needs from the accounts module. Every method locks the accounts
// it touches until the caller's transaction ends and rejects archived accounts with 422
// ACCOUNT_ARCHIVED (PostCash and RequireActiveAccount also unknown ones, on the given field).
// ReverseCash posts one mirrored REVERSAL per movement (INV-18), dated on (or later).
type Cash interface {
	PostCash(ctx context.Context, householdID uuid.UUID, mv CashMovement) (uuid.UUID, error)
	RequireActiveAccount(ctx context.Context, householdID, accountID uuid.UUID, field string) error
	ReverseCash(ctx context.Context, householdID uuid.UUID, movementIDs []uuid.UUID, on time.Time, description string) error
}

// Transaction is an income or expense entry (the economic fact).
type Transaction struct {
	ID                uuid.UUID
	Kind              string
	Scope             string
	PersonMemberID    *uuid.UUID
	CreatedByMemberID uuid.UUID
	Description       string
	Merchant          *string
	Notes             *string
	AmountCents       int64
	CategoryID        uuid.UUID
	OccurredOn        time.Time
	PaymentMethod     string
	AccountID         *uuid.UUID // nil for a card purchase
	CreditCardID      *uuid.UUID // set only for a card purchase
	Status            string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Payment says how an entry is settled: IMMEDIATE (through AccountID, now) or PLANNED (due
// on DueOn; AccountID optional, chosen at payment time if absent). A CREDIT_CARD purchase
// goes to CardID in Installments (default 1) and has no mode: it is consumption now and a
// commitment through the invoices.
type Payment struct {
	Method       string
	AccountID    *uuid.UUID
	Mode         string
	DueOn        *time.Time
	CardID       *uuid.UUID
	Installments int
}

// CreateInput is a new income or expense entry.
type CreateInput struct {
	Kind           string
	Scope          string
	PersonMemberID *uuid.UUID
	Description    string
	Merchant       *string
	Notes          *string
	AmountCents    int64
	CategoryID     uuid.UUID
	OccurredOn     time.Time
	Payment        Payment
}

// Entry is the result of Create: an immediate entry has its transaction and its paid
// obligation; a forecast has only the PLANNED obligation (no consumption yet); a card
// purchase has its transaction and installments, and no obligation (INV-03).
type Entry struct {
	Transaction  *Transaction
	Obligation   *Obligation
	Installments []CardInstallment
	// AllocationShifted: a CLOSED invoice was skipped (I-15).
	AllocationShifted bool
}

// Service implements ledger use cases.
type Service struct {
	tx         *postgres.TxManager
	clock      clock.Clock
	audit      *audit.Recorder
	categories CategoryChecker
	members    MemberChecker
	cash       Cash
	cards      Cards
}

// NewService builds the ledger service.
func NewService(tx *postgres.TxManager, clk clock.Clock, rec *audit.Recorder, categories CategoryChecker, members MemberChecker, cash Cash, cards Cards) *Service {
	return &Service{tx: tx, clock: clk, audit: rec, categories: categories, members: members, cash: cash, cards: cards}
}

var errNotFound = kernel.NewError(kernel.ErrNotFound, "TRANSACTION_NOT_FOUND", "Lançamento não encontrado.")

// Create records an entry.
//
// IMMEDIATE (RN-008, ADR 0002 §3): the cash movement, the transaction, a CONFIRMED obligation
// and one payment for the whole amount, in one SQL transaction (INV-03/04/05). The amount
// counts once as family consumption, whoever paid or consumed it (T02, T03, RN-003).
//
// PLANNED (I-08): only a PLANNED obligation holding the draft of the future transaction. It
// is a commitment, not consumption, until its first payment (PayObligation).
// Merchant and notes of a forecast can be set after its confirmation (Update).
//
// CREDIT_CARD (RN-009, ADR 0003): the transaction and its installments on the card's
// invoices; consumption now, commitment through the invoices, no cash (T05, INV-02/03).
func (s *Service) Create(ctx context.Context, m authctx.Membership, in CreateInput) (Entry, error) {
	in, err := normalize(in)
	if err != nil {
		return Entry{}, err
	}
	ids, err := newIDs(3)
	if err != nil {
		return Entry{}, err
	}
	obligationID := ids[1]
	var (
		txID          *uuid.UUID
		hasObligation = true
		out           Entry
	)

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.checkReferences(ctx, m.HouseholdID, in); err != nil {
			return err
		}
		switch {
		case in.Payment.Method == MethodCreditCard:
			txID, hasObligation = &ids[0], false
			out.Installments, out.AllocationShifted, err = s.createCardPurchase(ctx, m, ids[0], in)
			return err
		case in.Payment.Mode == ModePlanned:
			return s.createForecast(ctx, m, obligationID, in)
		default:
			txID = &ids[0]
			return s.createImmediate(ctx, m, ids[0], obligationID, ids[2], in)
		}
	})
	if err != nil {
		return Entry{}, err
	}

	if txID != nil {
		t, err := s.Get(ctx, m, *txID)
		if err != nil {
			return Entry{}, err
		}
		out.Transaction = &t
	}
	if hasObligation {
		o, err := s.obligation(ctx, m, obligationID)
		if err != nil {
			return Entry{}, err
		}
		out.Obligation = &o
	}
	return out, nil
}

// createCardPurchase writes a purchase on the card: the transaction (method CREDIT_CARD, no
// account) and, through the cards module, one installment per invoice. No obligation and no
// cash movement: the invoice is the commitment and its payment the cash (ADR 0002, INV-03).
func (s *Service) createCardPurchase(ctx context.Context, m authctx.Membership, txID uuid.UUID, in CreateInput) ([]CardInstallment, bool, error) {
	cardID := *in.Payment.CardID
	// Lock the card before the insert below, whose foreign key takes KEY SHARE on it (a lock
	// upgrade afterwards would deadlock concurrent purchases, ADR 0007).
	if err := s.cards.RequireActiveCard(ctx, m.HouseholdID, cardID, "payment.cardId"); err != nil {
		return nil, false, err
	}
	if err := store.New(s.tx.Conn(ctx)).CreateTransaction(ctx, store.CreateTransactionParams{
		ID: txID, HouseholdID: m.HouseholdID, Kind: in.Kind, Scope: in.Scope, PersonMemberID: in.PersonMemberID,
		CreatedByMemberID: m.MemberID, Description: in.Description, Merchant: in.Merchant, Notes: in.Notes,
		AmountCents: in.AmountCents, CategoryID: in.CategoryID, OccurredOn: in.OccurredOn,
		PaymentMethod: MethodCreditCard, CreditCardID: &cardID,
	}); err != nil {
		return nil, false, fmt.Errorf("create transaction: %w", err)
	}
	installments, shifted, err := s.cards.PlaceInstallments(ctx, m.HouseholdID, CardPurchase{
		CardID: cardID, TransactionID: txID, OccurredOn: in.OccurredOn, AmountCents: in.AmountCents,
		Installments: in.Payment.Installments,
	})
	if err != nil {
		return nil, false, err
	}
	// No amount or description in the audit metadata (ADR 0012).
	err = s.audit.Record(ctx, audit.Event{
		HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
		EntityType: "transaction", EntityID: txID, Action: "transaction.created",
		Metadata: map[string]any{
			"kind": in.Kind, "method": MethodCreditCard, "creditCardId": cardID.String(),
			"installments": in.Payment.Installments, "allocationShifted": shifted,
		},
	})
	return installments, shifted, err
}

func (s *Service) createImmediate(ctx context.Context, m authctx.Membership, txID, obligationID, paymentID uuid.UUID, in CreateInput) error {
	accountID := *in.Payment.AccountID
	// Cash first: posting locks the account (FOR UPDATE) and rejects unknown or archived
	// accounts with 422. Locking it before the inserts below also avoids a lock upgrade
	// (their foreign keys take KEY SHARE on the account), which deadlocked concurrent entries.
	movementID, err := s.cash.PostCash(ctx, m.HouseholdID, CashMovement{
		AccountID: accountID, Incoming: in.Kind == KindIncome, AmountCents: in.AmountCents,
		PostedOn: in.OccurredOn, Description: in.Description, Field: "payment.accountId",
	})
	if err != nil {
		return err
	}
	q := store.New(s.tx.Conn(ctx))
	if err := q.CreateTransaction(ctx, store.CreateTransactionParams{
		ID: txID, HouseholdID: m.HouseholdID, Kind: in.Kind, Scope: in.Scope, PersonMemberID: in.PersonMemberID,
		CreatedByMemberID: m.MemberID, Description: in.Description, Merchant: in.Merchant, Notes: in.Notes,
		AmountCents: in.AmountCents, CategoryID: in.CategoryID, OccurredOn: in.OccurredOn,
		PaymentMethod: in.Payment.Method, AccountID: &accountID,
	}); err != nil {
		return fmt.Errorf("create transaction: %w", err)
	}
	if err := q.CreateObligation(ctx, store.CreateObligationParams{
		ID: obligationID, HouseholdID: m.HouseholdID, TransactionID: &txID, Kind: obligationKind(in.Kind),
		Source: sourceDirect, Status: StatusConfirmed, AmountCents: in.AmountCents, DueOn: in.OccurredOn,
		Description: in.Description, CategoryID: in.CategoryID, Scope: in.Scope, PersonMemberID: in.PersonMemberID,
		PaymentMethod: in.Payment.Method, AccountID: &accountID, CompetenceOn: in.OccurredOn,
		CreatedByMemberID: m.MemberID,
	}); err != nil {
		return fmt.Errorf("create obligation: %w", err)
	}
	if err := q.CreateObligationPayment(ctx, store.CreateObligationPaymentParams{
		ID: paymentID, HouseholdID: m.HouseholdID, ObligationID: obligationID, AccountMovementID: movementID,
		AmountCents: in.AmountCents, PaidOn: in.OccurredOn, CreatedByMemberID: m.MemberID,
	}); err != nil {
		return fmt.Errorf("create obligation payment: %w", err)
	}
	// No amount or description in the audit metadata (ADR 0012): the row is in the ledger.
	return s.audit.Record(ctx, audit.Event{
		HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
		EntityType: "transaction", EntityID: txID, Action: "transaction.created",
		Metadata: map[string]any{"kind": in.Kind, "mode": ModeImmediate},
	})
}

// createForecast stores a PLANNED obligation with the draft of the future transaction
// (I-08). The merchant and notes of a forecast are not kept: the draft has no columns for
// them, and normalize rejects them (they can be added to the confirmed transaction, Update).
func (s *Service) createForecast(ctx context.Context, m authctx.Membership, obligationID uuid.UUID, in CreateInput) error {
	if in.Payment.AccountID != nil {
		if err := s.cash.RequireActiveAccount(ctx, m.HouseholdID, *in.Payment.AccountID, "payment.accountId"); err != nil {
			return err
		}
	}
	if err := store.New(s.tx.Conn(ctx)).CreateObligation(ctx, store.CreateObligationParams{
		ID: obligationID, HouseholdID: m.HouseholdID, Kind: obligationKind(in.Kind),
		Source: sourceManualForecast, Status: StatusPlanned, AmountCents: in.AmountCents, DueOn: *in.Payment.DueOn,
		Description: in.Description, CategoryID: in.CategoryID, Scope: in.Scope, PersonMemberID: in.PersonMemberID,
		PaymentMethod: in.Payment.Method, AccountID: in.Payment.AccountID, CompetenceOn: in.OccurredOn,
		CreatedByMemberID: m.MemberID,
	}); err != nil {
		return fmt.Errorf("create obligation: %w", err)
	}
	return s.audit.Record(ctx, audit.Event{
		HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
		EntityType: "obligation", EntityID: obligationID, Action: "obligation.created",
		Metadata: map[string]any{"kind": in.Kind, "mode": ModePlanned},
	})
}

// Get returns one transaction of the household.
func (s *Service) Get(ctx context.Context, m authctx.Membership, id uuid.UUID) (Transaction, error) {
	row, err := store.New(s.tx.Conn(ctx)).GetTransaction(ctx, store.GetTransactionParams{HouseholdID: m.HouseholdID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Transaction{}, errNotFound
	}
	if err != nil {
		return Transaction{}, fmt.Errorf("get transaction: %w", err)
	}
	return Transaction(row), nil
}

// checkReferences validates the IDs that came in the body against the household (ADR 0006):
// the category (same kind, active, locked against archiving) and the person.
func (s *Service) checkReferences(ctx context.Context, householdID uuid.UUID, in CreateInput) error {
	return s.checkRefs(ctx, householdID, in, true)
}

// checkRefs checks the person when given and, if checkCategory, the category.
func (s *Service) checkRefs(ctx context.Context, householdID uuid.UUID, in CreateInput, checkCategory bool) error {
	if checkCategory {
		if err := s.checkCategory(ctx, householdID, in.Kind, in.CategoryID); err != nil {
			return err
		}
	}
	if in.PersonMemberID != nil {
		ok, err := s.members.IsActiveMember(ctx, householdID, *in.PersonMemberID)
		if err != nil {
			return err
		}
		if !ok {
			return kernel.NewError(kernel.ErrInvariant, "PERSON_NOT_MEMBER", "A pessoa deve ser um membro ativo da família.",
				kernel.FieldError{Field: "personMemberId", Reason: "not_active_member"})
		}
	}
	return nil
}

func (s *Service) checkCategory(ctx context.Context, householdID uuid.UUID, entryKind string, categoryID uuid.UUID) error {
	kind, active, found, err := s.categories.LockForEntry(ctx, householdID, categoryID)
	switch {
	case err != nil:
		return err
	case !found:
		return kernel.NewError(kernel.ErrInvariant, "CATEGORY_NOT_FOUND", "Categoria não encontrada.",
			kernel.FieldError{Field: "categoryId", Reason: "not_found"})
	case kind != entryKind:
		msg := "Escolha uma categoria de despesa."
		if entryKind == KindIncome {
			msg = "Escolha uma categoria de receita."
		}
		return kernel.NewError(kernel.ErrInvariant, "CATEGORY_KIND_MISMATCH", msg,
			kernel.FieldError{Field: "categoryId", Reason: "kind_mismatch"})
	case !active:
		return kernel.NewError(kernel.ErrInvariant, "CATEGORY_ARCHIVED", "Essa categoria está arquivada.",
			kernel.FieldError{Field: "categoryId", Reason: "archived"})
	}
	return nil
}

// normalize validates the input shape and trims texts (empty optional texts become nil).
func normalize(in CreateInput) (CreateInput, error) {
	if in.Kind != KindExpense && in.Kind != KindIncome {
		return in, validation("kind", "oneof=EXPENSE INCOME", "Tipo de lançamento inválido.")
	}
	switch in.Scope {
	case ScopeFamily:
	case ScopePersonal:
		if in.PersonMemberID == nil {
			return in, validation("personMemberId", "required_for_personal", "Informe a pessoa do lançamento pessoal.")
		}
	default:
		return in, validation("scope", "oneof=PERSONAL FAMILY", "Escopo inválido.")
	}
	if err := validAmount("amountCents", in.AmountCents); err != nil {
		return in, err
	}
	var err error
	if in.Description, err = validDescription(in.Description); err != nil {
		return in, err
	}
	if in.Merchant, err = optionalText("merchant", in.Merchant, maxMerchantLen); err != nil {
		return in, err
	}
	if in.Notes, err = optionalText("notes", in.Notes, maxNotesLen); err != nil {
		return in, err
	}
	if in.Payment.Method == MethodCreditCard {
		return normalizeCardPurchase(in)
	}
	if !accountMethods[in.Payment.Method] {
		return in, validation("payment.method", "oneof=PIX DEBIT CASH BANK_TRANSFER BOLETO CREDIT_CARD OTHER", "Meio de pagamento inválido.")
	}
	if in.Payment.CardID != nil {
		return in, validation("payment.cardId", "only_for_credit_card", "Cartão só vale para pagamento no crédito.")
	}
	if in.Payment.Installments != 0 {
		return in, validation("payment.installments", "only_for_credit_card", "Parcelas só valem para pagamento no crédito.")
	}
	switch in.Payment.Mode {
	case ModeImmediate:
		if in.Payment.AccountID == nil {
			return in, validation("payment.accountId", "required", "Escolha a conta.")
		}
		if in.Payment.DueOn != nil {
			return in, validation("payment.dueOn", "only_for_planned", "Vencimento só vale para lançamento previsto.")
		}
	case ModePlanned:
		if in.Payment.DueOn == nil {
			return in, validation("payment.dueOn", "required_for_planned", "Informe o vencimento.")
		}
		if in.Merchant != nil || in.Notes != nil {
			field := "merchant"
			if in.Merchant == nil {
				field = "notes"
			}
			return in, validation(field, "not_for_planned", "Estabelecimento e observações são informados quando o lançamento for pago.")
		}
	default:
		return in, validation("payment.mode", "oneof=IMMEDIATE PLANNED", "Modo de pagamento inválido.")
	}
	return in, nil
}

// normalizeCardPurchase validates the payment of a card purchase: an expense on a card, in
// 1..60 installments of at least one cent each (RN-010, I-48), with no account, mode or due
// date (the invoices give the due dates).
func normalizeCardPurchase(in CreateInput) (CreateInput, error) {
	p := in.Payment
	switch {
	case in.Kind != KindExpense:
		return in, validation("payment.method", "expense_only", "Receita não pode ser lançada no cartão de crédito.")
	case p.CardID == nil:
		return in, validation("payment.cardId", "required", "Escolha o cartão.")
	case p.AccountID != nil:
		return in, validation("payment.accountId", "not_for_credit_card", "Compra no cartão não usa conta.")
	case p.DueOn != nil:
		return in, validation("payment.dueOn", "not_for_credit_card", "O vencimento da compra no cartão vem da fatura.")
	case p.Mode != "" && p.Mode != ModeImmediate:
		return in, validation("payment.mode", "not_for_credit_card", "Compra no cartão não pode ser prevista.")
	}
	if in.Payment.Installments == 0 {
		in.Payment.Installments = 1
	}
	if n := in.Payment.Installments; n < 1 || n > maxInstallments {
		return in, kernel.NewError(kernel.ErrValidation, "INVALID_INSTALLMENTS", "O número de parcelas deve ficar entre 1 e 60.",
			kernel.FieldError{Field: "payment.installments", Reason: "range"})
	}
	if in.AmountCents < int64(in.Payment.Installments) {
		return in, kernel.NewError(kernel.ErrValidation, "INSTALLMENT_TOO_SMALL", "Cada parcela precisa ter pelo menos R$ 0,01.",
			kernel.FieldError{Field: "payment.installments", Reason: "too_small"})
	}
	in.Payment.Mode = ""
	return in, nil
}

func validAmount(field string, cents int64) error {
	if cents <= 0 || cents > kernel.MaxAbsCents {
		return validation(field, "range=1..1e15", "Informe um valor maior que zero.")
	}
	return nil
}

func validDescription(d string) (string, error) {
	d = strings.TrimSpace(d)
	if n := utf8.RuneCountInString(d); n == 0 || n > maxDescriptionLen {
		return "", validation("description", "length=1..200", "Informe uma descrição com até 200 caracteres.")
	}
	return d, nil
}

func optionalText(field string, v *string, maxLen int) (*string, error) {
	if v == nil {
		return nil, nil
	}
	t := strings.TrimSpace(*v)
	if t == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(t) > maxLen {
		return nil, validation(field, fmt.Sprintf("max=%d", maxLen), fmt.Sprintf("Use no máximo %d caracteres.", maxLen))
	}
	return &t, nil
}

func obligationKind(kind string) string {
	if kind == KindIncome {
		return "RECEIVABLE"
	}
	return "PAYABLE"
}

func transactionKind(obligationKind string) string {
	if obligationKind == "RECEIVABLE" {
		return KindIncome
	}
	return KindExpense
}

func newIDs(n int) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, n)
	for i := range ids {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

func validation(field, reason, message string) error {
	return kernel.NewError(kernel.ErrValidation, "VALIDATION_ERROR", message, kernel.FieldError{Field: field, Reason: reason})
}
