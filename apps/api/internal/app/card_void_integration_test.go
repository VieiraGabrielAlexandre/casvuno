//go:build integration

package app

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func (c *client) card(householdID, cardID string) cardDTO {
	c.api.t.Helper()
	var card cardDTO
	c.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+householdID+"/cards/"+cardID, nil).decode(c.api.t, &card)
	return card
}

// F4.6 / RN-019: voiding a card purchase cancels its installments (they leave the invoices),
// takes it out of the consumption and moves no cash. The estimated available limit follows
// (RN-018).
func TestVoidCardPurchase(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 100000})
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil)) // limit R$ 5.000
	tv := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 120000, 6, "2026-11-10"))
	owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 300, 1, "2026-11-12"))
	movements := api.count("SELECT count(*) FROM account_movements WHERE household_id = $1", h.ID)

	if c := owner.card(h.ID, card.ID); c.OutstandingCents != 120300 || c.EstimatedAvailableLimitCents != 500000-120300 {
		t.Fatalf("card before void = %+v", c)
	}
	voided := owner.voidTransaction(h.ID, tv.Transaction.ID)
	if voided.Status != "VOIDED" {
		t.Fatalf("voided = %+v", voided)
	}
	first := owner.invoice(h.ID, tv.Installments[0].InvoiceID)
	if first.Invoice.TotalCents != 300 || first.Invoice.InstallmentsCount != 1 || len(first.Installments) != 2 {
		t.Fatalf("first invoice = %+v", first)
	}
	for _, line := range first.Installments {
		if line.TransactionID == tv.Transaction.ID && line.Status != "CANCELLED" {
			t.Fatalf("installment of the voided purchase = %+v", line)
		}
	}
	if last := owner.invoice(h.ID, tv.Installments[5].InvoiceID); last.Invoice.TotalCents != 0 || last.Invoice.PaymentStatus != "PAID" {
		t.Fatalf("last invoice = %+v", last.Invoice)
	}
	if c := api.consumption(h.ID); c != 300 {
		t.Fatalf("consumption = %d", c)
	}
	if n := api.count("SELECT count(*) FROM account_movements WHERE household_id = $1", h.ID); n != movements {
		t.Fatalf("void of a card purchase moved cash: %d movements", n-movements)
	}
	if b := owner.balance(h.ID, acc.ID); b != 100000 {
		t.Fatalf("balance = %d", b)
	}
	if c := owner.card(h.ID, card.ID); c.OutstandingCents != 300 || c.EstimatedAvailableLimitCents != 499700 {
		t.Fatalf("card after void = %+v", c)
	}

	// Voiding again changes nothing; the audit records one void, without the amount.
	owner.voidTransaction(h.ID, tv.Transaction.ID)
	if a := api.auditActions(tv.Transaction.ID); !slices.Equal(a, []string{"transaction.created", "transaction.voided"}) {
		t.Fatalf("audit = %v", a)
	}
	var meta string
	if err := api.pool.QueryRow(context.Background(),
		"SELECT metadata::text FROM audit_events WHERE entity_id = $1 AND action = 'transaction.voided'", tv.Transaction.ID).Scan(&meta); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(meta, "120000") || strings.Contains(meta, "Compra") {
		t.Fatalf("audit metadata leaks: %s", meta)
	}
	assertInvariants(t)
}

// I-18: the void is blocked while an installment is on a CLOSED invoice, or when it would
// leave an invoice below what was already paid; the correction is an invoice adjustment.
func TestVoidCardPurchaseBlocked(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 100000})
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	a := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 1000, 1, "2026-11-10"))
	b := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 500, 1, "2026-11-11"))
	invoiceID := a.Installments[0].InvoiceID
	path := func(id string) string { return "/api/v1/households/" + h.ID + "/transactions/" + id + "/void" }

	// Paid R$ 8,00 of R$ 15,00: voiding B (R$ 5,00) leaves R$ 10,00 >= R$ 8,00; voiding A after
	// that would leave R$ 0,00 < R$ 8,00.
	owner.payInvoice(h.ID, invoiceID, payBody(acc.ID, 800, "2026-11-12"))
	owner.voidTransaction(h.ID, b.Transaction.ID)
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, path(a.Transaction.ID), nil,
		"Idempotency-Key", uuid.NewString()).errorCode(t); code != "CARD_PURCHASE_VOID_EXCEEDS_BALANCE" {
		t.Fatalf("got %s", code)
	}
	// A credit adjustment is the way to correct it.
	if d := owner.adjustInvoice(h.ID, invoiceID, -200, "Devolução parcial"); d.Invoice.OutstandingCents != 0 {
		t.Fatalf("after adjustment = %+v", d.Invoice)
	}

	// A purchase with an installment on a CLOSED invoice cannot be voided; nothing changes.
	c := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 2000, 2, "2026-11-20"))
	api.setToday("2026-12-03")
	owner.closeInvoice(h.ID, invoiceID)
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, path(c.Transaction.ID), nil,
		"Idempotency-Key", uuid.NewString()).errorCode(t); code != "CARD_PURCHASE_ON_CLOSED_INVOICE" {
		t.Fatalf("got %s", code)
	}
	var got transactionDTO
	owner.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+h.ID+"/transactions/"+c.Transaction.ID, nil).decode(t, &got)
	if got.Status != "ACTIVE" || api.count("SELECT count(*) FROM card_installments WHERE transaction_id = $1 AND status = 'ACTIVE'", c.Transaction.ID) != 2 {
		t.Fatalf("blocked void changed the purchase: %+v", got)
	}
	assertInvariants(t)
}

// RN-018 / I-17: estimated available limit = limit − what is left to pay on all invoices
// (future ones included); payments free it, adjustments use it, and it may go negative.
func TestEstimatedAvailableLimit(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 100000})
	card := owner.createCard(h.ID, cardBody(h.MemberID, map[string]any{"limitCents": 100000}))
	p := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 60000, 3, "2026-11-10"))

	if c := owner.card(h.ID, card.ID); c.OutstandingCents != 60000 || c.EstimatedAvailableLimitCents != 40000 {
		t.Fatalf("after purchase = %+v", c)
	}
	owner.payInvoice(h.ID, p.Installments[0].InvoiceID, payBody(acc.ID, 20000, "2026-11-12"))
	owner.adjustInvoice(h.ID, p.Installments[1].InvoiceID, 990, "Anuidade")
	if c := owner.card(h.ID, card.ID); c.OutstandingCents != 40990 || c.EstimatedAvailableLimitCents != 59010 {
		t.Fatalf("after payment and fee = %+v", c)
	}
	// Lowering the limit below what is owed: negative, not hidden.
	owner.expect(http.StatusOK, http.MethodPatch, "/api/v1/households/"+h.ID+"/cards/"+card.ID, map[string]any{"limitCents": 30000})
	list := owner.cards(h.ID, "")
	if len(list) != 1 || list[0].EstimatedAvailableLimitCents != 30000-40990 || list[0].OutstandingCents != 40990 {
		t.Fatalf("list = %+v", list)
	}
	assertInvariants(t)
}

// ADR 0007: a void waits for an invoice payment in progress (which holds the invoice lock)
// and then sees what was paid, instead of leaving the invoice below it.
func TestVoidCardPurchaseWaitsForAPaymentInProgress(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 100000})
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	p := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 20000, 1, "2026-11-10"))
	invoiceID := p.Installments[0].InvoiceID

	ctx := context.Background()
	tx, err := api.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	movementID := uuid.NewString()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{"SELECT 1 FROM card_invoices WHERE id = $1 FOR UPDATE", []any{invoiceID}},
		{"SELECT 1 FROM accounts WHERE id = $1 FOR UPDATE", []any{acc.ID}},
		{`INSERT INTO account_movements (id, household_id, account_id, direction, amount_cents, posted_on, kind, description)
		  VALUES ($1, $2, $3, 'OUT', 20000, '2026-11-12', 'INVOICE_PAYMENT', 'Fatura')`, []any{movementID, h.ID, acc.ID}},
		{`INSERT INTO invoice_payments (id, household_id, card_invoice_id, account_movement_id, amount_cents, paid_on, created_by_member_id)
		  VALUES (gen_random_uuid(), $1, $2, $3, 20000, '2026-11-12', $4)`, []any{h.ID, invoiceID, movementID, h.MemberID}},
	} {
		if _, err := tx.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}

	done := make(chan response, 1)
	go func() {
		// Sent from a defer: a failed contract check ends this goroutine (t.Fatal) but the
		// test still receives a response instead of hanging.
		r := response{status: -1}
		defer func() { done <- r }()
		r = owner.do(http.MethodPost, "/api/v1/households/"+h.ID+"/transactions/"+p.Transaction.ID+"/void", nil,
			"Idempotency-Key", uuid.NewString())
	}()
	select {
	case r := <-done:
		t.Fatalf("void did not wait for the payment: %d %s", r.status, r.body)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.status != http.StatusUnprocessableEntity || r.errorCode(t) != "CARD_PURCHASE_VOID_EXCEEDS_BALANCE" {
		t.Fatalf("void = %d %s", r.status, r.body)
	}
	assertInvariants(t)
}

// INV-06 in the database also covers cancelling installments (000015).
func TestCancelledInstallmentsKeepTheInvoiceBalance(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 100000})
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	p := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 20000, 1, "2026-11-10"))
	owner.payInvoice(h.ID, p.Installments[0].InvoiceID, payBody(acc.ID, 100, "2026-11-12"))

	if _, err := api.pool.Exec(context.Background(),
		"UPDATE card_installments SET status = 'CANCELLED' WHERE household_id = $1 AND transaction_id = $2", h.ID, p.Transaction.ID); err == nil {
		t.Fatal("cancelling a paid installment below the paid amount: the database must reject it")
	}
	assertInvariants(t)
}
