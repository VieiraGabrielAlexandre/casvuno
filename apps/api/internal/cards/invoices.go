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
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/audit"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
)

var errInvoiceNotFound = kernel.NewError(kernel.ErrNotFound, "INVOICE_NOT_FOUND", "Fatura não encontrada.")

// Invoice is a card invoice with its totals. Until invoice payments and adjustments exist
// (F4.5), the total is the sum of its ACTIVE installments and nothing has been paid.
type Invoice struct {
	ID                    uuid.UUID
	CardID                uuid.UUID
	CycleStartOn          time.Time
	ClosingOn             time.Time
	DueOn                 time.Time
	Status                string
	ClosedBaseAmountCents *int64
	ClosedAt              *time.Time
	ClosedByMemberID      *uuid.UUID
	InstallmentsCents     int64
	InstallmentsCount     int
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// TotalCents is what the invoice charges: its active installments (+ adjustments, F4.5).
func (i Invoice) TotalCents() int64 { return i.InstallmentsCents }

// PaidCents is the sum of the invoice payments (F4.5; none can exist yet).
func (i Invoice) PaidCents() int64 { return 0 }

// OutstandingCents is what is left to pay (INV-06).
func (i Invoice) OutstandingCents() int64 { return i.TotalCents() - i.PaidCents() }

// PaymentStatus is derived from the balance, never stored (I-03).
func (i Invoice) PaymentStatus() string { return domain.PaymentStatus(i.TotalCents(), i.PaidCents()) }

// ClosingPending is true for an OPEN invoice whose closing date has arrived: the UI asks the
// family to check the bank statement and close it (I-15; there is no auto-close).
func (i Invoice) ClosingPending(today time.Time) bool {
	return i.Status == InvoiceOpen && domain.Closable(i.ClosingOn, today)
}

// InvoiceInstallment is one line of an invoice with the purchase it belongs to.
type InvoiceInstallment struct {
	ID                  uuid.UUID
	TransactionID       uuid.UUID
	Number              int
	Total               int
	AmountCents         int64
	Status              string
	OccurredOn          time.Time
	Description         string
	Merchant            *string
	CategoryID          uuid.UUID
	Scope               string
	PersonMemberID      *uuid.UUID
	PurchaseAmountCents int64
}

// InvoiceDetail is an invoice with all its installments (ACTIVE and CANCELLED).
type InvoiceDetail struct {
	Invoice      Invoice
	Installments []InvoiceInstallment
}

// InvoicePosition is the keyset position of the invoice list (due date, id).
type InvoicePosition struct {
	DueOn time.Time
	ID    uuid.UUID
}

// InvoiceQuery filters a card's invoices by due date (the reference month is the month of
// due_on, ADR 0003).
type InvoiceQuery struct {
	From, To *time.Time
	After    *InvoicePosition
	Limit    int
}

// Today is the household's current date (its timezone).
func (s *Service) Today(m authctx.Membership) time.Time {
	return kernel.Today(s.clock.Now(), m.Timezone)
}

// ListInvoices lists a card's invoices by due date, keyset-paginated, and the next position.
// Archived cards still list their invoices.
func (s *Service) ListInvoices(ctx context.Context, m authctx.Membership, cardID uuid.UUID, q InvoiceQuery) ([]Invoice, *InvoicePosition, error) {
	if _, err := s.Get(ctx, m, cardID); err != nil {
		return nil, nil, err
	}
	params := store.ListInvoicesParams{
		HouseholdID: m.HouseholdID, CreditCardID: cardID, FromDate: q.From, ToDate: q.To,
		RowLimit: int32(q.Limit + 1), //nolint:gosec // limit validated (<= 100)
	}
	if q.After != nil {
		params.AfterDueOn, params.AfterID = &q.After.DueOn, &q.After.ID
	}
	rows, err := store.New(s.tx.Conn(ctx)).ListInvoices(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list invoices: %w", err)
	}
	var next *InvoicePosition
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		last := rows[len(rows)-1]
		next = &InvoicePosition{DueOn: last.DueOn, ID: last.ID}
	}
	out := make([]Invoice, 0, len(rows))
	for _, r := range rows {
		out = append(out, toInvoice(store.GetInvoiceRow(r)))
	}
	return out, next, nil
}

// GetInvoice returns an invoice of the household with its installments. The list is not
// paginated: one invoice holds one card cycle of purchases.
func (s *Service) GetInvoice(ctx context.Context, m authctx.Membership, id uuid.UUID) (InvoiceDetail, error) {
	q := store.New(s.tx.Conn(ctx))
	row, err := q.GetInvoice(ctx, store.GetInvoiceParams{HouseholdID: m.HouseholdID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return InvoiceDetail{}, errInvoiceNotFound
	}
	if err != nil {
		return InvoiceDetail{}, fmt.Errorf("get invoice: %w", err)
	}
	rows, err := q.ListInvoiceInstallments(ctx, store.ListInvoiceInstallmentsParams{HouseholdID: m.HouseholdID, CardInvoiceID: id})
	if err != nil {
		return InvoiceDetail{}, fmt.Errorf("list invoice installments: %w", err)
	}
	items := make([]InvoiceInstallment, 0, len(rows))
	for _, r := range rows {
		items = append(items, InvoiceInstallment{
			ID: r.ID, TransactionID: r.TransactionID, Number: int(r.InstallmentNumber), Total: int(r.InstallmentTotal),
			AmountCents: r.AmountCents, Status: r.Status, OccurredOn: r.OccurredOn, Description: r.Description,
			Merchant: r.Merchant, CategoryID: r.CategoryID, Scope: r.Scope, PersonMemberID: r.PersonMemberID,
			PurchaseAmountCents: r.PurchaseAmountCents,
		})
	}
	return InvoiceDetail{Invoice: toInvoice(row), Installments: items}, nil
}

// CloseInvoice closes an OPEN invoice from its closing date on (I-15, ADR 0003) and freezes
// its base: the sum of its ACTIVE installments (INV-15). The invoice is locked (FOR UPDATE)
// first, and purchases lock it before placing installments, so none enters or leaves while
// the base is computed. Closing an invoice already CLOSED changes nothing (200).
func (s *Service) CloseInvoice(ctx context.Context, m authctx.Membership, id uuid.UUID) (InvoiceDetail, error) {
	today := s.Today(m)
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		inv, err := q.LockInvoice(ctx, store.LockInvoiceParams{HouseholdID: m.HouseholdID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return errInvoiceNotFound
		}
		if err != nil {
			return fmt.Errorf("lock invoice: %w", err)
		}
		if inv.Status == InvoiceClosed {
			return nil
		}
		if !domain.Closable(inv.ClosingOn, today) {
			return kernel.NewError(kernel.ErrInvariant, "INVOICE_NOT_CLOSABLE_YET",
				fmt.Sprintf("Esta fatura só pode ser fechada a partir de %s.", inv.ClosingOn.Format("02/01/2006")))
		}
		base, err := q.SumActiveInstallments(ctx, store.SumActiveInstallmentsParams{HouseholdID: m.HouseholdID, CardInvoiceID: id})
		if err != nil {
			return fmt.Errorf("sum installments: %w", err)
		}
		if err := q.CloseInvoice(ctx, store.CloseInvoiceParams{
			HouseholdID: m.HouseholdID, ID: id, ClosedBaseAmountCents: &base, ClosedByMemberID: &m.MemberID,
		}); err != nil {
			return fmt.Errorf("close invoice: %w", err)
		}
		// No amount in the audit trail (ADR 0012).
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "card_invoice", EntityID: id, Action: "card_invoice.closed",
		})
	})
	if err != nil {
		return InvoiceDetail{}, err
	}
	return s.GetInvoice(ctx, m, id)
}

func toInvoice(r store.GetInvoiceRow) Invoice {
	return Invoice{
		ID: r.ID, CardID: r.CreditCardID, CycleStartOn: r.CycleStartOn, ClosingOn: r.ClosingOn, DueOn: r.DueOn,
		Status: r.Status, ClosedBaseAmountCents: r.ClosedBaseAmountCents, ClosedAt: r.ClosedAt,
		ClosedByMemberID: r.ClosedByMemberID, InstallmentsCents: r.InstallmentsCents,
		InstallmentsCount: int(r.InstallmentsCount), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}
