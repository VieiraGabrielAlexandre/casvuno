package ledger

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/ledger/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/audit"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
)

// Transaction statuses (README §4.2) and the listing filter that matches both.
const (
	TxActive = "ACTIVE"
	TxVoided = "VOIDED"
	TxAny    = "ALL"
)

const maxSearchLen = 100

// TransactionPosition is the keyset position of the last transaction of a page.
type TransactionPosition struct {
	OccurredOn time.Time
	ID         uuid.UUID
}

// TransactionQuery filters and paginates transactions (newest first, RN-025). Nil filters
// are ignored; CategoryID also matches its subcategories; Text is a case-insensitive
// substring of the description, merchant or notes.
type TransactionQuery struct {
	Status         string // ACTIVE (default), VOIDED or ALL
	Kind           *string
	From, To       *time.Time
	CategoryID     *uuid.UUID
	AccountID      *uuid.UUID
	PersonMemberID *uuid.UUID
	Scope          *string
	PaymentMethod  *string
	Text           *string
	After          *TransactionPosition
	Limit          int
}

// TransactionUpdate holds the non-structural fields of a transaction (I-18). A nil field is
// left unchanged; an empty Merchant, Notes or PersonMemberID clears it. Amount, date, method
// and account change only by void + new entry.
type TransactionUpdate struct {
	Description    *string
	Merchant       *string
	Notes          *string
	CategoryID     *uuid.UUID
	Scope          *string
	PersonMemberID *string
}

// ListTransactions lists transactions, keyset-paginated, and the next position.
func (s *Service) ListTransactions(ctx context.Context, m authctx.Membership, q TransactionQuery) ([]Transaction, *TransactionPosition, error) {
	params := store.ListTransactionsParams{
		HouseholdID: m.HouseholdID, Kind: q.Kind, FromDate: q.From, ToDate: q.To, CategoryID: q.CategoryID,
		AccountID: q.AccountID, PersonMemberID: q.PersonMemberID, Scope: q.Scope, PaymentMethod: q.PaymentMethod,
		RowLimit: int32(q.Limit + 1), //nolint:gosec // limit validated (<= 100)
	}
	switch q.Status {
	case "", TxActive:
		status := TxActive
		params.Status = &status
	case TxVoided:
		params.Status = &q.Status
	case TxAny:
	default:
		return nil, nil, validation("status", "oneof=ACTIVE VOIDED ALL", "Situação inválida.")
	}
	if q.Text != nil {
		text := strings.TrimSpace(*q.Text)
		if utf8.RuneCountInString(text) > maxSearchLen {
			return nil, nil, validation("q", fmt.Sprintf("max=%d", maxSearchLen), fmt.Sprintf("Use no máximo %d caracteres na busca.", maxSearchLen))
		}
		if text != "" {
			params.Text = &text
		}
	}
	if q.After != nil {
		params.AfterOccurredOn, params.AfterID = &q.After.OccurredOn, &q.After.ID
	}
	rows, err := store.New(s.tx.Conn(ctx)).ListTransactions(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list transactions: %w", err)
	}
	var next *TransactionPosition
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		last := rows[len(rows)-1]
		next = &TransactionPosition{OccurredOn: last.OccurredOn, ID: last.ID}
	}
	out := make([]Transaction, 0, len(rows))
	for _, r := range rows {
		out = append(out, Transaction(r))
	}
	return out, next, nil
}

// Update edits the non-structural fields of an active transaction (I-18) and keeps the copy
// held by its obligation in step. A new category must be active and of the same kind; a new
// person must be an active member; PERSONAL requires a person (I-43).
func (s *Service) Update(ctx context.Context, m authctx.Membership, id uuid.UUID, in TransactionUpdate) (Transaction, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		t, err := s.lockTransaction(ctx, q, m, id)
		if err != nil {
			return err
		}
		if t.Status == TxVoided {
			return kernel.NewError(kernel.ErrInvariant, "TRANSACTION_VOIDED", "Este lançamento foi cancelado e não pode ser editado.")
		}
		next, changed, err := s.applyUpdate(ctx, m.HouseholdID, t, in)
		if err != nil {
			return err
		}
		if len(changed) == 0 {
			return nil
		}
		if err := q.UpdateTransaction(ctx, store.UpdateTransactionParams{
			HouseholdID: m.HouseholdID, ID: id, Description: next.Description, Merchant: next.Merchant, Notes: next.Notes,
			CategoryID: next.CategoryID, Scope: next.Scope, PersonMemberID: next.PersonMemberID,
		}); err != nil {
			return fmt.Errorf("update transaction: %w", err)
		}
		obligationID, err := q.GetObligationIDByTransaction(ctx, store.GetObligationIDByTransactionParams{
			HouseholdID: m.HouseholdID, TransactionID: &id,
		})
		if err != nil {
			return fmt.Errorf("get obligation of transaction: %w", err)
		}
		if err := q.UpdateObligationClassification(ctx, store.UpdateObligationClassificationParams{
			HouseholdID: m.HouseholdID, ID: obligationID, Description: next.Description, CategoryID: next.CategoryID,
			Scope: next.Scope, PersonMemberID: next.PersonMemberID,
		}); err != nil {
			return fmt.Errorf("update obligation: %w", err)
		}
		// Field names only (ADR 0012): no descriptions or notes in the audit trail.
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "transaction", EntityID: id, Action: "transaction.updated",
			Metadata: map[string]any{"fields": changed},
		})
	})
	if err != nil {
		return Transaction{}, err
	}
	return s.Get(ctx, m, id)
}

// applyUpdate validates the update against the current transaction and returns the new
// values with the names of the fields that actually changed.
func (s *Service) applyUpdate(ctx context.Context, householdID uuid.UUID, t Transaction, in TransactionUpdate) (Transaction, []string, error) {
	next := t
	changed := []string{}
	var err error
	if in.Description != nil {
		if next.Description, err = validDescription(*in.Description); err != nil {
			return t, nil, err
		}
	}
	if in.Merchant != nil {
		if next.Merchant, err = optionalText("merchant", in.Merchant, maxMerchantLen); err != nil {
			return t, nil, err
		}
	}
	if in.Notes != nil {
		if next.Notes, err = optionalText("notes", in.Notes, maxNotesLen); err != nil {
			return t, nil, err
		}
	}
	if in.Scope != nil {
		if *in.Scope != ScopeFamily && *in.Scope != ScopePersonal {
			return t, nil, validation("scope", "oneof=PERSONAL FAMILY", "Escopo inválido.")
		}
		next.Scope = *in.Scope
	}
	if in.PersonMemberID != nil {
		next.PersonMemberID = nil
		if *in.PersonMemberID != "" {
			pid, err := uuid.Parse(*in.PersonMemberID)
			if err != nil {
				return t, nil, validation("personMemberId", "uuid", "Identificador inválido.")
			}
			next.PersonMemberID = &pid
		}
	}
	if next.Scope == ScopePersonal && next.PersonMemberID == nil {
		return t, nil, validation("personMemberId", "required_for_personal", "Informe a pessoa do lançamento pessoal.")
	}
	if in.CategoryID != nil {
		next.CategoryID = *in.CategoryID
	}

	if next.Description != t.Description {
		changed = append(changed, "description")
	}
	if !equalText(next.Merchant, t.Merchant) {
		changed = append(changed, "merchant")
	}
	if !equalText(next.Notes, t.Notes) {
		changed = append(changed, "notes")
	}
	if next.Scope != t.Scope {
		changed = append(changed, "scope")
	}
	if !equalID(next.PersonMemberID, t.PersonMemberID) {
		changed = append(changed, "personMemberId")
	}
	if next.CategoryID != t.CategoryID {
		changed = append(changed, "categoryId")
	}

	// Only what changes is checked: keeping an archived category or a former member is allowed.
	ref := CreateInput{Kind: t.Kind, CategoryID: next.CategoryID}
	checkCategory := slices.Contains(changed, "categoryId")
	if slices.Contains(changed, "personMemberId") {
		ref.PersonMemberID = next.PersonMemberID
	}
	if checkCategory || ref.PersonMemberID != nil {
		if err := s.checkRefs(ctx, householdID, ref, checkCategory); err != nil {
			return t, nil, err
		}
	}
	return next, changed, nil
}

// Void cancels an active transaction outside the card (RN-019, I-18, INV-19): the
// transaction becomes VOIDED, its obligation CANCELLED, and every payment's cash movement is
// reversed (REVERSAL dated today in the household, or on the payment date if later). The
// payments stay as history. Consumption and the open commitment disappear; cash returns.
// Voiding an already voided transaction changes nothing (idempotent).
func (s *Service) Void(ctx context.Context, m authctx.Membership, id uuid.UUID) (Transaction, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		// Lock order: transaction, obligation, then the accounts (by ID, in ReverseCash).
		t, err := s.lockTransaction(ctx, q, m, id)
		if err != nil {
			return err
		}
		if t.Status == TxVoided {
			return nil
		}
		obligationID, err := q.GetObligationIDByTransaction(ctx, store.GetObligationIDByTransactionParams{
			HouseholdID: m.HouseholdID, TransactionID: &id,
		})
		if err != nil {
			return fmt.Errorf("get obligation of transaction: %w", err)
		}
		if _, err := s.lockObligation(ctx, q, m, obligationID); err != nil {
			return err
		}
		payments, err := q.ListObligationPayments(ctx, store.ListObligationPaymentsParams{
			HouseholdID: m.HouseholdID, ObligationID: obligationID,
		})
		if err != nil {
			return fmt.Errorf("list obligation payments: %w", err)
		}
		movements := make([]uuid.UUID, 0, len(payments))
		for _, p := range payments {
			movements = append(movements, p.AccountMovementID)
		}
		if len(movements) > 0 {
			if err := s.cash.ReverseCash(ctx, m.HouseholdID, movements, s.Today(m), reversalDescription(t.Description)); err != nil {
				return err
			}
		}
		if err := q.CancelObligation(ctx, store.CancelObligationParams{HouseholdID: m.HouseholdID, ID: obligationID}); err != nil {
			return fmt.Errorf("cancel obligation: %w", err)
		}
		if err := q.VoidTransaction(ctx, store.VoidTransactionParams{HouseholdID: m.HouseholdID, ID: id}); err != nil {
			return fmt.Errorf("void transaction: %w", err)
		}
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "transaction", EntityID: id, Action: "transaction.voided",
			Metadata: map[string]any{"obligationId": obligationID.String(), "reversals": len(movements)},
		})
	})
	if err != nil {
		return Transaction{}, err
	}
	return s.Get(ctx, m, id)
}

// CancelForecast cancels a forecast that was never paid (PLANNED → CANCELLED): it leaves the
// commitments and nothing else changes. A confirmed obligation is cancelled by voiding its
// transaction. Cancelling an already cancelled obligation changes nothing.
func (s *Service) CancelForecast(ctx context.Context, m authctx.Membership, id uuid.UUID) (ObligationDetail, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		o, err := s.lockObligation(ctx, q, m, id)
		if err != nil {
			return err
		}
		switch o.Status {
		case StatusCancelled:
			return nil
		case StatusConfirmed:
			return kernel.NewError(kernel.ErrInvariant, "OBLIGATION_NOT_PLANNED",
				"Esta conta já teve pagamento. Para desfazer, cancele o lançamento.")
		}
		if err := q.CancelObligation(ctx, store.CancelObligationParams{HouseholdID: m.HouseholdID, ID: id}); err != nil {
			return fmt.Errorf("cancel obligation: %w", err)
		}
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "obligation", EntityID: id, Action: "obligation.cancelled",
		})
	})
	if err != nil {
		return ObligationDetail{}, err
	}
	return s.GetObligation(ctx, m, id)
}

// lockTransaction locks the transaction (FOR UPDATE, ADR 0007) and reads it.
func (s *Service) lockTransaction(ctx context.Context, q *store.Queries, m authctx.Membership, id uuid.UUID) (Transaction, error) {
	if _, err := q.LockTransaction(ctx, store.LockTransactionParams{HouseholdID: m.HouseholdID, ID: id}); errors.Is(err, pgx.ErrNoRows) {
		return Transaction{}, errNotFound
	} else if err != nil {
		return Transaction{}, fmt.Errorf("lock transaction: %w", err)
	}
	return s.Get(ctx, m, id)
}

// reversalDescription names the reversal after the entry, within the movement length limit.
func reversalDescription(description string) string {
	const prefix = "Estorno: "
	r := []rune(description)
	if limit := maxDescriptionLen - utf8.RuneCountInString(prefix); len(r) > limit {
		r = r[:limit]
	}
	return prefix + string(r)
}

func equalText(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func equalID(a, b *uuid.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
