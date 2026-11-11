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
)

// Obligation lifecycle (I-02) and sources.
const (
	StatusPlanned   = "PLANNED"
	StatusConfirmed = "CONFIRMED"
	StatusCancelled = "CANCELLED"

	sourceDirect         = "DIRECT"
	sourceManualForecast = "MANUAL_FORECAST"
)

// Payment methods through an account (I-07). CREDIT_CARD arrives with card purchases (F4.3).
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

// Cash is what the ledger needs from the accounts module. Both methods lock the account
// until the caller's transaction ends and reject unknown or archived accounts with 422
// (ACCOUNT_NOT_FOUND/ACCOUNT_ARCHIVED on the given field).
type Cash interface {
	PostCash(ctx context.Context, householdID uuid.UUID, mv CashMovement) (uuid.UUID, error)
	RequireActiveAccount(ctx context.Context, householdID, accountID uuid.UUID, field string) error
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
	AccountID         uuid.UUID
	Status            string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Payment says how an entry is settled: IMMEDIATE (through AccountID, now) or PLANNED (due
// on DueOn; AccountID optional, chosen at payment time if absent). CREDIT_CARD: F4.3.
type Payment struct {
	Method    string
	AccountID *uuid.UUID
	Mode      string
	DueOn     *time.Time
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
// obligation; a forecast has only the PLANNED obligation (no consumption yet).
type Entry struct {
	Transaction *Transaction
	Obligation  Obligation
}

// Service implements ledger use cases.
type Service struct {
	tx         *postgres.TxManager
	clock      clock.Clock
	audit      *audit.Recorder
	categories CategoryChecker
	members    MemberChecker
	cash       Cash
}

// NewService builds the ledger service.
func NewService(tx *postgres.TxManager, clk clock.Clock, rec *audit.Recorder, categories CategoryChecker, members MemberChecker, cash Cash) *Service {
	return &Service{tx: tx, clock: clk, audit: rec, categories: categories, members: members, cash: cash}
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
	var txID *uuid.UUID

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.checkReferences(ctx, m.HouseholdID, in); err != nil {
			return err
		}
		if in.Payment.Mode == ModePlanned {
			return s.createForecast(ctx, m, obligationID, in)
		}
		txID = &ids[0]
		return s.createImmediate(ctx, m, ids[0], obligationID, ids[2], in)
	})
	if err != nil {
		return Entry{}, err
	}

	var out Entry
	if txID != nil {
		t, err := s.Get(ctx, m, *txID)
		if err != nil {
			return Entry{}, err
		}
		out.Transaction = &t
	}
	if out.Obligation, err = s.obligation(ctx, m, obligationID); err != nil {
		return Entry{}, err
	}
	return out, nil
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
		PaymentMethod: in.Payment.Method, AccountID: accountID,
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
// them, and normalize rejects them (they can be added when the entry is confirmed, F3.3).
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
	kind, active, found, err := s.categories.LockForEntry(ctx, householdID, in.CategoryID)
	switch {
	case err != nil:
		return err
	case !found:
		return kernel.NewError(kernel.ErrInvariant, "CATEGORY_NOT_FOUND", "Categoria não encontrada.",
			kernel.FieldError{Field: "categoryId", Reason: "not_found"})
	case kind != in.Kind:
		msg := "Escolha uma categoria de despesa."
		if in.Kind == KindIncome {
			msg = "Escolha uma categoria de receita."
		}
		return kernel.NewError(kernel.ErrInvariant, "CATEGORY_KIND_MISMATCH", msg,
			kernel.FieldError{Field: "categoryId", Reason: "kind_mismatch"})
	case !active:
		return kernel.NewError(kernel.ErrInvariant, "CATEGORY_ARCHIVED", "Essa categoria está arquivada.",
			kernel.FieldError{Field: "categoryId", Reason: "archived"})
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
	if !accountMethods[in.Payment.Method] {
		return in, validation("payment.method", "oneof=PIX DEBIT CASH BANK_TRANSFER BOLETO OTHER", "Meio de pagamento inválido.")
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
