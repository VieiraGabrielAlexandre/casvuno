package cards

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/cards/domain"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/cards/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
)

// Invoice statuses (I-03): the payment situation is derived from the balance (F4.4/F4.5).
const (
	InvoiceOpen   = "OPEN"
	InvoiceClosed = "CLOSED"
)

// MaxNewInvoices bounds the invoices one purchase may create: its installments plus the
// empty cycles between the card's last invoice and the purchase, so the chain stays
// contiguous (I-48). 120 covers 60 installments after five idle years; more than that is a
// mistyped date, and accepting it would write thousands of rows (I-50).
const MaxNewInvoices = 120

// Purchase is a purchase on the card whose transaction the ledger has just written.
type Purchase struct {
	CardID        uuid.UUID
	TransactionID uuid.UUID
	OccurredOn    time.Time
	AmountCents   int64
	Installments  int
}

// PlacedInstallment is one installment and the invoice it went to.
type PlacedInstallment struct {
	Number      int
	AmountCents int64
	InvoiceID   uuid.UUID
	ClosingOn   time.Time
	DueOn       time.Time
}

// Placement is the result of PlaceInstallments.
type Placement struct {
	Installments []PlacedInstallment
	// Shifted is true when a CLOSED invoice was skipped (I-15: allocationShifted).
	Shifted bool
}

var errInconsistent = kernel.NewError(kernel.ErrInvariant, "CARD_INVOICES_INCONSISTENT",
	"As faturas deste cartão estão inconsistentes.")

// RequireActiveForPurchase locks the card (FOR UPDATE) until the caller's transaction ends
// and requires it to be active. The ledger calls it before inserting the purchase, whose
// foreign key would otherwise take a weaker lock first (lock upgrade, ADR 0007). Unknown
// cards and cards of other households are 422 on field, since the ID came in the body.
func (s *Service) RequireActiveForPurchase(ctx context.Context, householdID, cardID uuid.UUID, field string) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		_, err := lockForPurchase(ctx, store.New(s.tx.Conn(ctx)), householdID, cardID, field)
		return err
	})
}

// PlaceInstallments splits the purchase (RN-010) and writes one installment per invoice of
// the allocation (ADR 0003): existing invoices are reused, missing ones are created (with the
// empty cycles in between) and CLOSED ones are skipped (Shifted). It must run inside the
// caller's transaction, after the purchase's transaction row was inserted.
func (s *Service) PlaceInstallments(ctx context.Context, householdID uuid.UUID, p Purchase) (Placement, error) {
	parts, err := domain.Split(p.AmountCents, p.Installments)
	if err != nil {
		return Placement{}, err
	}
	var out Placement
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		// Lock order: card, then its OPEN invoices (oldest first). Purchases on one card are
		// serialised by the card lock; closing an invoice (F4.4) waits for the invoice lock.
		card, err := lockForPurchase(ctx, q, householdID, p.CardID, "payment.cardId")
		if err != nil {
			return err
		}
		ref := store.LockOpenInvoicesParams{HouseholdID: householdID, CreditCardID: p.CardID}
		if err := q.LockOpenInvoices(ctx, ref); err != nil {
			return fmt.Errorf("lock open invoices: %w", err)
		}
		rows, err := q.ListCardInvoices(ctx, store.ListCardInvoicesParams{HouseholdID: householdID, CreditCardID: p.CardID})
		if err != nil {
			return fmt.Errorf("list invoices: %w", err)
		}
		existing := make([]domain.Invoice, 0, len(rows))
		ids := make(map[string]uuid.UUID, len(rows))
		for _, r := range rows {
			existing = append(existing, domain.Invoice{
				Period: domain.Period{StartOn: r.CycleStartOn, ClosingOn: r.ClosingOn, DueOn: r.DueOn},
				Closed: r.Status == InvoiceClosed,
			})
			ids[kernel.FormatDate(r.CycleStartOn)] = r.ID
		}

		cycle := domain.Cycle{ClosingDay: int(card.ClosingDay), DueDay: int(card.DueDay)}
		alloc, err := cycle.Allocate(existing, p.OccurredOn, p.Installments)
		if err != nil {
			return err
		}
		if len(alloc.Create) > MaxNewInvoices {
			return kernel.NewError(kernel.ErrInvariant, "CARD_PURCHASE_TOO_FAR",
				"A data da compra está longe demais das faturas deste cartão. Confira a data.",
				kernel.FieldError{Field: "occurredOn", Reason: "too_far"})
		}
		for _, period := range alloc.Create {
			id, err := s.createInvoice(ctx, q, householdID, p.CardID, period)
			if err != nil {
				return err
			}
			ids[kernel.FormatDate(period.StartOn)] = id
		}

		out.Shifted = alloc.Shifted
		for i, period := range alloc.Periods {
			invoiceID, ok := ids[kernel.FormatDate(period.StartOn)]
			if !ok {
				return errInconsistent
			}
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			if err := q.CreateInstallment(ctx, store.CreateInstallmentParams{
				ID: id, HouseholdID: householdID, TransactionID: p.TransactionID, CreditCardID: p.CardID,
				CardInvoiceID: invoiceID, InstallmentNumber: int16(i + 1), //nolint:gosec // 1..60 (Split)
				InstallmentTotal: int16(p.Installments), //nolint:gosec // 1..60 (Split)
				AmountCents:      parts[i],
			}); err != nil {
				return fmt.Errorf("create installment: %w", err)
			}
			out.Installments = append(out.Installments, PlacedInstallment{
				Number: i + 1, AmountCents: parts[i], InvoiceID: invoiceID, ClosingOn: period.ClosingOn, DueOn: period.DueOn,
			})
		}
		return nil
	})
	if err != nil {
		return Placement{}, err
	}
	return out, nil
}

// createInvoice finds or creates the invoice of a period (ADR 0003) and checks that the
// stored one has the same dates and is still OPEN.
func (s *Service) createInvoice(ctx context.Context, q *store.Queries, householdID, cardID uuid.UUID, period domain.Period) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	if err := q.CreateInvoice(ctx, store.CreateInvoiceParams{
		ID: id, HouseholdID: householdID, CreditCardID: cardID,
		CycleStartOn: period.StartOn, ClosingOn: period.ClosingOn, DueOn: period.DueOn,
	}); err != nil {
		return uuid.Nil, fmt.Errorf("create invoice: %w", err)
	}
	got, err := q.GetInvoiceByStart(ctx, store.GetInvoiceByStartParams{
		HouseholdID: householdID, CreditCardID: cardID, CycleStartOn: period.StartOn,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("get invoice: %w", err)
	}
	if !got.ClosingOn.Equal(period.ClosingOn) || !got.DueOn.Equal(period.DueOn) || got.Status != InvoiceOpen {
		return uuid.Nil, errInconsistent
	}
	return got.ID, nil
}

func lockForPurchase(ctx context.Context, q *store.Queries, householdID, cardID uuid.UUID, field string) (store.LockCardForPurchaseRow, error) {
	card, err := q.LockCardForPurchase(ctx, store.LockCardForPurchaseParams{HouseholdID: householdID, ID: cardID})
	if errors.Is(err, pgx.ErrNoRows) {
		// Also covers cards of other households (ADR 0006).
		return card, kernel.NewError(kernel.ErrInvariant, "CARD_NOT_FOUND", "Cartão não encontrado.",
			kernel.FieldError{Field: field, Reason: "not_found"})
	}
	if err != nil {
		return card, fmt.Errorf("lock card: %w", err)
	}
	if !card.IsActive {
		return card, kernel.NewError(kernel.ErrInvariant, "CARD_ARCHIVED", "Não é possível lançar compras num cartão arquivado.",
			kernel.FieldError{Field: field, Reason: "archived"})
	}
	return card, nil
}

// CancelPurchase cancels the installments of a voided card purchase (RN-019, I-18). It is
// allowed only while every invoice of the purchase is OPEN and the cancellation leaves each
// one with total >= paid (INV-06); otherwise the family corrects it with an invoice
// adjustment. It must run inside the caller's transaction, after the transaction row was
// locked. Lock order: card, then the purchase's invoices (oldest first), as purchases do.
func (s *Service) CancelPurchase(ctx context.Context, householdID, cardID, transactionID uuid.UUID) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		if _, err := q.LockCard(ctx, store.LockCardParams{HouseholdID: householdID, ID: cardID}); err != nil {
			return fmt.Errorf("lock card: %w", err)
		}
		ref := store.LockPurchaseInvoicesParams{HouseholdID: householdID, TransactionID: transactionID}
		if err := q.LockPurchaseInvoices(ctx, ref); err != nil {
			return fmt.Errorf("lock purchase invoices: %w", err)
		}
		rows, err := q.ListPurchaseInstallments(ctx, store.ListPurchaseInstallmentsParams{HouseholdID: householdID, TransactionID: transactionID})
		if err != nil {
			return fmt.Errorf("list purchase installments: %w", err)
		}
		// One installment per invoice (Allocate), in installment order.
		invoices := make([]Invoice, 0, len(rows))
		cancelled := make([]int64, 0, len(rows))
		for _, r := range rows {
			if r.Status != "ACTIVE" {
				continue
			}
			row, err := q.GetInvoice(ctx, store.GetInvoiceParams{HouseholdID: householdID, ID: r.CardInvoiceID})
			if err != nil {
				return fmt.Errorf("get invoice: %w", err)
			}
			invoices = append(invoices, toInvoice(row))
			cancelled = append(cancelled, r.AmountCents)
		}
		for _, inv := range invoices {
			if inv.Status == InvoiceClosed {
				return kernel.NewError(kernel.ErrInvariant, "CARD_PURCHASE_ON_CLOSED_INVOICE",
					"Esta compra tem parcela numa fatura já fechada. Corrija com um ajuste na fatura.")
			}
		}
		for i, inv := range invoices {
			if inv.OutstandingCents() < cancelled[i] {
				return kernel.NewError(kernel.ErrInvariant, "CARD_PURCHASE_VOID_EXCEEDS_BALANCE",
					"Cancelar esta compra deixaria uma fatura com valor menor que o já pago. Corrija com um ajuste na fatura.")
			}
		}
		if err := q.CancelPurchaseInstallments(ctx, store.CancelPurchaseInstallmentsParams{
			HouseholdID: householdID, TransactionID: transactionID,
		}); err != nil {
			return fmt.Errorf("cancel installments: %w", err)
		}
		return nil
	})
}
