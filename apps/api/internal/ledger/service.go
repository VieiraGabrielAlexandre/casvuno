// Package ledger owns income and expense entries (ADR 0002): the economic fact
// (`transactions`, the only source of consumption), the obligation it settles and how it was
// paid (`obligations`, `obligation_payments`). Cash moves only through the accounts module.
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
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
)

// Entry kinds, scopes and payment modes (README §6.3, §7.5).
const (
	KindExpense = "EXPENSE"
	KindIncome  = "INCOME"

	ScopePersonal = "PERSONAL"
	ScopeFamily   = "FAMILY"

	ModeImmediate = "IMMEDIATE"
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

// CashPoster posts a cash movement inside the caller's transaction and returns its ID. The
// account must be active and of the household (422 ACCOUNT_NOT_FOUND/ACCOUNT_ARCHIVED).
type CashPoster interface {
	PostCash(ctx context.Context, householdID uuid.UUID, mv CashMovement) (uuid.UUID, error)
}

// Transaction is an income or expense entry.
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

// Payment says how an entry is settled. Only IMMEDIATE (paid now, through an account)
// exists so far; PLANNED arrives in F3.2 and CREDIT_CARD in F4.3.
type Payment struct {
	Method    string
	AccountID *uuid.UUID
	Mode      string
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

// Service implements ledger use cases.
type Service struct {
	tx         *postgres.TxManager
	audit      *audit.Recorder
	categories CategoryChecker
	members    MemberChecker
	cash       CashPoster
}

// NewService builds the ledger service.
func NewService(tx *postgres.TxManager, rec *audit.Recorder, categories CategoryChecker, members MemberChecker, cash CashPoster) *Service {
	return &Service{tx: tx, audit: rec, categories: categories, members: members, cash: cash}
}

var errNotFound = kernel.NewError(kernel.ErrNotFound, "TRANSACTION_NOT_FOUND", "Lançamento não encontrado.")

// Create records an immediate expense or income (RN-008, ADR 0002 §3): the transaction, a
// CONFIRMED obligation fully paid by one payment, and the cash movement on the account, all
// in one SQL transaction (INV-03, INV-04, INV-05). The whole amount counts once as family
// consumption, whoever paid or consumed it (T02, T03, RN-003).
func (s *Service) Create(ctx context.Context, m authctx.Membership, in CreateInput) (Transaction, error) {
	in, err := normalize(in)
	if err != nil {
		return Transaction{}, err
	}
	ids, err := newIDs(3)
	if err != nil {
		return Transaction{}, err
	}
	txID, obligationID, paymentID := ids[0], ids[1], ids[2]
	accountID := *in.Payment.AccountID

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.checkReferences(ctx, m.HouseholdID, in); err != nil {
			return err
		}
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
			Source: "DIRECT", Status: "CONFIRMED", AmountCents: in.AmountCents, DueOn: in.OccurredOn,
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
			Metadata: map[string]any{"kind": in.Kind, "mode": in.Payment.Mode},
		})
	})
	if err != nil {
		return Transaction{}, err
	}
	return s.Get(ctx, m, txID)
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
	if in.AmountCents <= 0 || in.AmountCents > kernel.MaxAbsCents {
		return in, validation("amountCents", "range=1..1e15", "Informe um valor maior que zero.")
	}
	in.Description = strings.TrimSpace(in.Description)
	if n := utf8.RuneCountInString(in.Description); n == 0 || n > maxDescriptionLen {
		return in, validation("description", "length=1..200", "Informe uma descrição com até 200 caracteres.")
	}
	var err error
	if in.Merchant, err = optionalText("merchant", in.Merchant, maxMerchantLen); err != nil {
		return in, err
	}
	if in.Notes, err = optionalText("notes", in.Notes, maxNotesLen); err != nil {
		return in, err
	}
	if in.Payment.Mode != ModeImmediate {
		return in, validation("payment.mode", "oneof=IMMEDIATE", "Modo de pagamento inválido.")
	}
	if !accountMethods[in.Payment.Method] {
		return in, validation("payment.method", "oneof=PIX DEBIT CASH BANK_TRANSFER BOLETO OTHER", "Meio de pagamento inválido.")
	}
	if in.Payment.AccountID == nil {
		return in, validation("payment.accountId", "required", "Escolha a conta.")
	}
	return in, nil
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
