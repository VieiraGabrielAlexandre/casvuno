package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/ledger/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/audit"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
)

// Payment situation, derived from the payments (I-02); never stored.
const (
	PaymentUnpaid        = "UNPAID"
	PaymentPartiallyPaid = "PARTIALLY_PAID"
	PaymentPaid          = "PAID"
)

// Obligation is something payable or receivable directly through an account. PaidCents is
// always the sum of its payments.
type Obligation struct {
	ID                uuid.UUID
	TransactionID     *uuid.UUID
	Kind              string
	Source            string
	Status            string
	AmountCents       int64
	DueOn             time.Time
	Description       string
	CategoryID        uuid.UUID
	Scope             string
	PersonMemberID    *uuid.UUID
	PaymentMethod     string
	AccountID         *uuid.UUID
	CompetenceOn      time.Time
	CreatedByMemberID uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
	PaidCents         int64
}

// OutstandingCents is what is still to be paid (0 for cancelled obligations).
func (o Obligation) OutstandingCents() int64 {
	if o.Status == StatusCancelled {
		return 0
	}
	return o.AmountCents - o.PaidCents
}

// PaymentStatus derives UNPAID, PARTIALLY_PAID or PAID from the payments.
func (o Obligation) PaymentStatus() string {
	switch {
	case o.PaidCents == 0:
		return PaymentUnpaid
	case o.PaidCents < o.AmountCents:
		return PaymentPartiallyPaid
	default:
		return PaymentPaid
	}
}

// IsOverdue: due before today (household date) and still outstanding.
func (o Obligation) IsOverdue(today time.Time) bool {
	return o.OutstandingCents() > 0 && o.DueOn.Before(today)
}

// ObligationPayment is one (partial or total) payment of an obligation.
type ObligationPayment struct {
	ID                uuid.UUID
	AmountCents       int64
	PaidOn            time.Time
	AccountID         uuid.UUID
	MovementID        uuid.UUID
	CreatedByMemberID uuid.UUID
	CreatedAt         time.Time
}

// ObligationDetail is an obligation with its payments.
type ObligationDetail struct {
	Obligation Obligation
	Payments   []ObligationPayment
}

// Obligation listing views.
const (
	ViewOpen    = "open"    // not cancelled and not fully paid
	ViewOverdue = "overdue" // open and due before today
	ViewAll     = "all"
)

// ObligationPosition is the keyset position of the last obligation of a page.
type ObligationPosition struct {
	DueOn time.Time
	ID    uuid.UUID
}

// ObligationQuery filters and paginates obligations (by due date, oldest first).
type ObligationQuery struct {
	View     string
	Kind     *string
	From, To *time.Time
	After    *ObligationPosition
	Limit    int
}

// PaymentInput is a payment of an obligation. AccountID defaults to the obligation's account.
type PaymentInput struct {
	AmountCents int64
	PaidOn      time.Time
	AccountID   *uuid.UUID
}

// ForecastUpdate holds the editable fields of a forecast (I-16): amount, due date and description.
type ForecastUpdate struct {
	AmountCents *int64
	DueOn       *time.Time
	Description *string
}

var errObligationNotFound = kernel.NewError(kernel.ErrNotFound, "OBLIGATION_NOT_FOUND", "Conta a pagar/receber não encontrada.")

// Today is the household's current calendar date (RN-005).
func (s *Service) Today(m authctx.Membership) time.Time {
	return kernel.Today(s.clock.Now(), m.Timezone)
}

// ListObligations lists obligations by due date, keyset-paginated, and the next position.
func (s *Service) ListObligations(ctx context.Context, m authctx.Membership, q ObligationQuery) ([]Obligation, *ObligationPosition, error) {
	params := store.ListObligationsParams{
		HouseholdID: m.HouseholdID, Kind: q.Kind, FromDate: q.From, ToDate: q.To,
		RowLimit: int32(q.Limit + 1), //nolint:gosec // limit validated (<= 100)
	}
	switch q.View {
	case ViewOpen:
		params.OnlyOpen = true
	case ViewOverdue:
		today := s.Today(m)
		params.OnlyOpen, params.OverdueBefore = true, &today
	case ViewAll:
	default:
		return nil, nil, validation("view", "oneof=open overdue all", "Filtro inválido.")
	}
	if q.After != nil {
		params.AfterDueOn, params.AfterID = &q.After.DueOn, &q.After.ID
	}
	rows, err := store.New(s.tx.Conn(ctx)).ListObligations(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list obligations: %w", err)
	}
	var next *ObligationPosition
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		last := rows[len(rows)-1]
		next = &ObligationPosition{DueOn: last.DueOn, ID: last.ID}
	}
	out := make([]Obligation, 0, len(rows))
	for _, r := range rows {
		out = append(out, Obligation(r))
	}
	return out, next, nil
}

// GetObligation returns an obligation of the household with its payments.
func (s *Service) GetObligation(ctx context.Context, m authctx.Membership, id uuid.UUID) (ObligationDetail, error) {
	o, err := s.obligation(ctx, m, id)
	if err != nil {
		return ObligationDetail{}, err
	}
	rows, err := store.New(s.tx.Conn(ctx)).ListObligationPayments(ctx, store.ListObligationPaymentsParams{
		HouseholdID: m.HouseholdID, ObligationID: id,
	})
	if err != nil {
		return ObligationDetail{}, fmt.Errorf("list obligation payments: %w", err)
	}
	payments := make([]ObligationPayment, 0, len(rows))
	for _, r := range rows {
		payments = append(payments, ObligationPayment{
			ID: r.ID, AmountCents: r.AmountCents, PaidOn: r.PaidOn, AccountID: r.AccountID,
			MovementID: r.AccountMovementID, CreatedByMemberID: r.CreatedByMemberID, CreatedAt: r.CreatedAt,
		})
	}
	return ObligationDetail{Obligation: o, Payments: payments}, nil
}

// PayObligation records a (partial or total) payment: one payment row and its cash movement
// (OUT for payables, IN for receivables), never above the outstanding amount (RN-020, T17).
// The first payment of a forecast confirms it (I-08): it creates the transaction from the
// draft (whole amount, occurred_on = competence date) and links it, so consumption appears
// once, only when it happens (T22). All in one SQL transaction, under a lock on the obligation.
func (s *Service) PayObligation(ctx context.Context, m authctx.Membership, id uuid.UUID, in PaymentInput) (ObligationDetail, error) {
	if err := validAmount("amountCents", in.AmountCents); err != nil {
		return ObligationDetail{}, err
	}
	ids, err := newIDs(2)
	if err != nil {
		return ObligationDetail{}, err
	}
	paymentID, txID := ids[0], ids[1]

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		o, err := s.lockObligation(ctx, q, m, id)
		if err != nil {
			return err
		}
		switch outstanding := o.OutstandingCents(); {
		case o.Status == StatusCancelled:
			return kernel.NewError(kernel.ErrInvariant, "OBLIGATION_CANCELLED", "Esta conta foi cancelada e não aceita pagamentos.")
		case outstanding == 0:
			return kernel.NewError(kernel.ErrInvariant, "OBLIGATION_ALREADY_PAID", "Esta conta já está quitada.")
		case in.AmountCents > outstanding:
			return kernel.NewError(kernel.ErrInvariant, "AMOUNT_EXCEEDS_OUTSTANDING",
				"O valor é maior que o saldo em aberto.", kernel.FieldError{Field: "amountCents", Reason: "exceeds_outstanding"})
		}
		accountID := in.AccountID
		if accountID == nil {
			accountID = o.AccountID
		}
		if accountID == nil {
			return validation("accountId", "required", "Escolha a conta do pagamento.")
		}

		// The account is locked by the movement before any row referencing it is inserted.
		movementID, err := s.cash.PostCash(ctx, m.HouseholdID, CashMovement{
			AccountID: *accountID, Incoming: o.Kind == "RECEIVABLE", AmountCents: in.AmountCents,
			PostedOn: in.PaidOn, Description: o.Description, Field: "accountId",
		})
		if err != nil {
			return err
		}
		if o.Status == StatusPlanned {
			if err := s.confirm(ctx, q, m, o, txID, *accountID); err != nil {
				return err
			}
		}
		if err := q.CreateObligationPayment(ctx, store.CreateObligationPaymentParams{
			ID: paymentID, HouseholdID: m.HouseholdID, ObligationID: o.ID, AccountMovementID: movementID,
			AmountCents: in.AmountCents, PaidOn: in.PaidOn, CreatedByMemberID: m.MemberID,
		}); err != nil {
			return fmt.Errorf("create obligation payment: %w", err)
		}
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "obligation", EntityID: o.ID, Action: "obligation.paid",
			Metadata: map[string]any{"partial": in.AmountCents < o.OutstandingCents()},
		})
	})
	if err != nil {
		return ObligationDetail{}, err
	}
	return s.GetObligation(ctx, m, id)
}

// confirm turns a forecast into a real entry: the transaction is created from the draft and
// linked to the obligation, which becomes CONFIRMED (single confirmation path, I-08).
func (s *Service) confirm(ctx context.Context, q *store.Queries, m authctx.Membership, o Obligation, txID, paidFrom uuid.UUID) error {
	accountID := paidFrom
	if o.AccountID != nil {
		accountID = *o.AccountID
	}
	if err := q.CreateTransaction(ctx, store.CreateTransactionParams{
		ID: txID, HouseholdID: m.HouseholdID, Kind: transactionKind(o.Kind), Scope: o.Scope,
		PersonMemberID: o.PersonMemberID, CreatedByMemberID: o.CreatedByMemberID, Description: o.Description,
		AmountCents: o.AmountCents, CategoryID: o.CategoryID, OccurredOn: o.CompetenceOn,
		PaymentMethod: o.PaymentMethod, AccountID: accountID,
	}); err != nil {
		return fmt.Errorf("create transaction: %w", err)
	}
	if err := q.ConfirmObligation(ctx, store.ConfirmObligationParams{
		HouseholdID: m.HouseholdID, ID: o.ID, TransactionID: &txID,
	}); err != nil {
		return fmt.Errorf("confirm obligation: %w", err)
	}
	return s.audit.Record(ctx, audit.Event{
		HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
		EntityType: "transaction", EntityID: txID, Action: "transaction.created",
		Metadata: map[string]any{"kind": transactionKind(o.Kind), "mode": ModePlanned, "obligationId": o.ID.String()},
	})
}

// UpdateForecast edits a forecast (PLANNED, hence without payments; I-16). Anything already
// confirmed is history: it is corrected by void + new entry (F3.3).
func (s *Service) UpdateForecast(ctx context.Context, m authctx.Membership, id uuid.UUID, in ForecastUpdate) (ObligationDetail, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		o, err := s.lockObligation(ctx, q, m, id)
		if err != nil {
			return err
		}
		if o.Status != StatusPlanned {
			return kernel.NewError(kernel.ErrInvariant, "OBLIGATION_NOT_PLANNED",
				"Só é possível editar uma previsão ainda não paga. Para corrigir um lançamento pago, cancele-o e lance de novo.")
		}
		next := store.UpdatePlannedObligationParams{
			HouseholdID: m.HouseholdID, ID: id, AmountCents: o.AmountCents, DueOn: o.DueOn, Description: o.Description,
		}
		changed := []string{}
		if in.AmountCents != nil {
			if err := validAmount("amountCents", *in.AmountCents); err != nil {
				return err
			}
			next.AmountCents = *in.AmountCents
			changed = append(changed, "amountCents")
		}
		if in.DueOn != nil {
			next.DueOn = *in.DueOn
			changed = append(changed, "dueOn")
		}
		if in.Description != nil {
			if next.Description, err = validDescription(*in.Description); err != nil {
				return err
			}
			changed = append(changed, "description")
		}
		if err := q.UpdatePlannedObligation(ctx, next); err != nil {
			return fmt.Errorf("update obligation: %w", err)
		}
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "obligation", EntityID: id, Action: "obligation.updated",
			Metadata: map[string]any{"fields": changed},
		})
	})
	if err != nil {
		return ObligationDetail{}, err
	}
	return s.GetObligation(ctx, m, id)
}

// lockObligation locks the obligation (FOR UPDATE) and reads it with its paid amount, so
// concurrent payments and edits of the same obligation are serialised (ADR 0007).
func (s *Service) lockObligation(ctx context.Context, q *store.Queries, m authctx.Membership, id uuid.UUID) (Obligation, error) {
	if _, err := q.LockObligation(ctx, store.LockObligationParams{HouseholdID: m.HouseholdID, ID: id}); errors.Is(err, pgx.ErrNoRows) {
		return Obligation{}, errObligationNotFound
	} else if err != nil {
		return Obligation{}, fmt.Errorf("lock obligation: %w", err)
	}
	return s.obligation(ctx, m, id)
}

func (s *Service) obligation(ctx context.Context, m authctx.Membership, id uuid.UUID) (Obligation, error) {
	row, err := store.New(s.tx.Conn(ctx)).GetObligation(ctx, store.GetObligationParams{HouseholdID: m.HouseholdID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Obligation{}, errObligationNotFound
	}
	if err != nil {
		return Obligation{}, fmt.Errorf("get obligation: %w", err)
	}
	return Obligation(row), nil
}
