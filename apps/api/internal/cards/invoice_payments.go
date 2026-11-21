package cards

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/cards/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/audit"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
)

const maxReasonLen = 200

// Cash takes the money of an invoice payment out of an account (an OUT/INVOICE_PAYMENT
// movement, INV-07). The accounts module implements it; the adapter lives in internal/app.
// It must lock the account and run inside the caller's transaction.
type Cash interface {
	PayInvoice(ctx context.Context, householdID uuid.UUID, out CashOut) (uuid.UUID, error)
}

// CashOut is the cash side of an invoice payment. Field names the request field that carried
// the account, for error details.
type CashOut struct {
	AccountID   uuid.UUID
	AmountCents int64
	PostedOn    time.Time
	Description string
	Field       string
}

// PaymentInput is a (partial or total) payment of an invoice.
type PaymentInput struct {
	AmountCents int64
	PaidOn      time.Time
	AccountID   uuid.UUID
}

// AdjustmentInput is a signed correction of an invoice balance.
type AdjustmentInput struct {
	AmountCents int64
	Reason      string
}

// PayInvoice records a payment of an OPEN (paid in advance) or CLOSED invoice: one
// invoice_payment and its OUT/INVOICE_PAYMENT movement, never above the outstanding balance
// (RN-013, RN-020, T17). Paying an invoice is cash, never a new expense (RN-014). Lock
// order: invoice (FOR UPDATE), then the account (by the movement), before the payment row.
func (s *Service) PayInvoice(ctx context.Context, m authctx.Membership, id uuid.UUID, in PaymentInput) (InvoiceDetail, error) {
	if in.AmountCents < 1 || in.AmountCents > kernel.MaxAbsCents {
		return InvoiceDetail{}, validation("amountCents", "range", "Informe um valor maior que zero.")
	}
	paymentID, err := uuid.NewV7()
	if err != nil {
		return InvoiceDetail{}, err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		locked, cur, err := s.lockInvoice(ctx, q, m.HouseholdID, id)
		if err != nil {
			return err
		}
		outstanding := cur.OutstandingCents()
		switch {
		case outstanding == 0:
			return kernel.NewError(kernel.ErrInvariant, "INVOICE_ALREADY_PAID", "Esta fatura não tem saldo em aberto.")
		case in.AmountCents > outstanding:
			return kernel.NewError(kernel.ErrInvariant, "AMOUNT_EXCEEDS_OUTSTANDING",
				"O valor é maior que o saldo em aberto da fatura.", kernel.FieldError{Field: "amountCents", Reason: "exceeds_outstanding"})
		}
		card, err := q.GetCard(ctx, store.GetCardParams{HouseholdID: m.HouseholdID, ID: locked.CreditCardID})
		if err != nil {
			return fmt.Errorf("get card: %w", err)
		}
		movementID, err := s.cash.PayInvoice(ctx, m.HouseholdID, CashOut{
			AccountID: in.AccountID, AmountCents: in.AmountCents, PostedOn: in.PaidOn,
			Description: fmt.Sprintf("Fatura %s %s", card.Name, locked.DueOn.Format("01/2006")), Field: "accountId",
		})
		if err != nil {
			return err
		}
		if err := q.CreateInvoicePayment(ctx, store.CreateInvoicePaymentParams{
			ID: paymentID, HouseholdID: m.HouseholdID, CardInvoiceID: id, AccountMovementID: movementID,
			AmountCents: in.AmountCents, PaidOn: in.PaidOn, CreatedByMemberID: m.MemberID,
		}); err != nil {
			return fmt.Errorf("create invoice payment: %w", err)
		}
		// No amount in the audit trail (ADR 0012).
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "card_invoice", EntityID: id, Action: "card_invoice.paid",
			Metadata: map[string]any{"partial": in.AmountCents < outstanding},
		})
	})
	if err != nil {
		return InvoiceDetail{}, err
	}
	return s.GetInvoice(ctx, m, id)
}

// AdjustInvoice records a signed correction of an OPEN or CLOSED invoice (fee, interest,
// refund, statement difference), with a reason; the installments never change (RN-012). A
// negative adjustment cannot leave the balance below zero (ADR 0003). The card must be
// active: its lock is shared first (card, then invoice, the purchases' order), so archiving
// (I-49) never races an adjustment that adds to the balance.
func (s *Service) AdjustInvoice(ctx context.Context, m authctx.Membership, id uuid.UUID, in AdjustmentInput) (InvoiceDetail, error) {
	if in.AmountCents == 0 || in.AmountCents > kernel.MaxAbsCents || in.AmountCents < -kernel.MaxAbsCents {
		return InvoiceDetail{}, validation("amountCents", "nonzero", "Informe um valor diferente de zero (negativo para crédito).")
	}
	reason := strings.TrimSpace(in.Reason)
	if n := utf8.RuneCountInString(reason); n == 0 || n > maxReasonLen {
		return InvoiceDetail{}, validation("reason", "length=1..200", "Informe o motivo do ajuste (até 200 caracteres).")
	}
	adjustmentID, err := uuid.NewV7()
	if err != nil {
		return InvoiceDetail{}, err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		// The invoice's card never changes (000013), so it can be read before the locks.
		inv, err := q.GetInvoice(ctx, store.GetInvoiceParams{HouseholdID: m.HouseholdID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return errInvoiceNotFound
		}
		if err != nil {
			return fmt.Errorf("get invoice: %w", err)
		}
		card, err := q.ShareCard(ctx, store.ShareCardParams{HouseholdID: m.HouseholdID, ID: inv.CreditCardID})
		if err != nil {
			return fmt.Errorf("share card: %w", err)
		}
		if !card.IsActive {
			return kernel.NewError(kernel.ErrInvariant, "CARD_ARCHIVED", "Restaure o cartão antes de ajustar suas faturas.")
		}
		_, cur, err := s.lockInvoice(ctx, q, m.HouseholdID, id)
		if err != nil {
			return err
		}
		if cur.TotalCents()+in.AmountCents < cur.PaidCents {
			return kernel.NewError(kernel.ErrInvariant, "ADJUSTMENT_EXCEEDS_BALANCE",
				"O ajuste deixaria o saldo da fatura negativo.", kernel.FieldError{Field: "amountCents", Reason: "exceeds_balance"})
		}
		if err := q.CreateInvoiceAdjustment(ctx, store.CreateInvoiceAdjustmentParams{
			ID: adjustmentID, HouseholdID: m.HouseholdID, CardInvoiceID: id, AmountCents: in.AmountCents,
			Reason: reason, CreatedByMemberID: m.MemberID,
		}); err != nil {
			return fmt.Errorf("create invoice adjustment: %w", err)
		}
		// Neither the amount nor the reason (free text) goes to the audit trail (ADR 0012).
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "card_invoice", EntityID: id, Action: "card_invoice.adjusted",
			Metadata: map[string]any{"credit": in.AmountCents < 0},
		})
	})
	if err != nil {
		return InvoiceDetail{}, err
	}
	return s.GetInvoice(ctx, m, id)
}

// lockInvoice locks an invoice of the household (FOR UPDATE) and reads its totals under the
// lock: purchases, closing, payments and adjustments all lock it before writing.
func (s *Service) lockInvoice(ctx context.Context, q *store.Queries, householdID, id uuid.UUID) (store.LockInvoiceRow, Invoice, error) {
	locked, err := q.LockInvoice(ctx, store.LockInvoiceParams{HouseholdID: householdID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return locked, Invoice{}, errInvoiceNotFound
	}
	if err != nil {
		return locked, Invoice{}, fmt.Errorf("lock invoice: %w", err)
	}
	row, err := q.GetInvoice(ctx, store.GetInvoiceParams{HouseholdID: householdID, ID: id})
	if err != nil {
		return locked, Invoice{}, fmt.Errorf("get invoice: %w", err)
	}
	return locked, toInvoice(row), nil
}
