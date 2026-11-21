//go:build integration

package app

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func payBody(accountID string, cents int64, paidOn string) map[string]any {
	return map[string]any{"amountCents": cents, "paidOn": paidOn, "accountId": accountID}
}

// payInvoice pays an invoice with a fresh Idempotency-Key.
func (c *client) payInvoice(householdID, invoiceID string, body map[string]any) invoiceDetailDTO {
	c.api.t.Helper()
	var d invoiceDetailDTO
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/households/"+householdID+"/invoices/"+invoiceID+"/payments", body,
		"Idempotency-Key", uuid.NewString()).decode(c.api.t, &d)
	return d
}

// adjustInvoice adjusts an invoice with a fresh Idempotency-Key.
func (c *client) adjustInvoice(householdID, invoiceID string, cents int64, reason string) invoiceDetailDTO {
	c.api.t.Helper()
	var d invoiceDetailDTO
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/households/"+householdID+"/invoices/"+invoiceID+"/adjustments",
		map[string]any{"amountCents": cents, "reason": reason}, "Idempotency-Key", uuid.NewString()).decode(c.api.t, &d)
	return d
}

// T07/T08 (F4.5): paying an invoice is cash, never consumption. A partial payment leaves the
// rest outstanding; nothing above the outstanding is accepted; an OPEN invoice can be paid
// in advance (RN-013, RN-014, ADR 0003).
func TestPayInvoice(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 100000})
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	p := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 120000, 6, "2026-11-10"))
	first, last := p.Installments[0].InvoiceID, p.Installments[5].InvoiceID
	base := "/api/v1/households/" + h.ID + "/invoices/" + first + "/payments"

	// T08: R$ 50 of R$ 200 → R$ 150 outstanding, partially paid.
	d := owner.payInvoice(h.ID, first, payBody(acc.ID, 5000, "2026-11-15"))
	inv := d.Invoice
	if inv.PaidCents != 5000 || inv.OutstandingCents != 15000 || inv.TotalCents != 20000 || inv.PaymentStatus != "PARTIALLY_PAID" ||
		len(d.Payments) != 1 || d.Payments[0].AccountID != acc.ID || d.Payments[0].PaidOn != "2026-11-15" || d.Payments[0].MovementID == "" {
		t.Fatalf("after R$ 50 = %+v", d)
	}
	// T07: cash goes down, consumption does not go up (RN-014).
	if b := owner.balance(h.ID, acc.ID); b != 95000 {
		t.Fatalf("balance = %d", b)
	}
	if c := api.consumption(h.ID); c != 120000 {
		t.Fatalf("consumption = %d", c)
	}
	if n := api.count("SELECT count(*) FROM account_movements WHERE household_id = $1 AND kind = 'INVOICE_PAYMENT' AND direction = 'OUT' AND description = 'Fatura Nubank 12/2026'", h.ID); n != 1 {
		t.Fatalf("invoice payment movements = %d", n)
	}

	// Never above the outstanding (RN-013); the invoice may be paid after closing.
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, base, payBody(acc.ID, 15001, "2026-12-05"),
		"Idempotency-Key", uuid.NewString()).errorCode(t); code != "AMOUNT_EXCEEDS_OUTSTANDING" {
		t.Fatalf("got %s", code)
	}
	api.setToday("2026-12-03")
	owner.closeInvoice(h.ID, first)
	key := uuid.NewString()
	var paid invoiceDetailDTO
	owner.expect(http.StatusCreated, http.MethodPost, base, payBody(acc.ID, 15000, "2026-12-05"), "Idempotency-Key", key).decode(t, &paid)
	if paid.Invoice.PaymentStatus != "PAID" || paid.Invoice.OutstandingCents != 0 || len(paid.Payments) != 2 {
		t.Fatalf("paid = %+v", paid.Invoice)
	}
	// Same key: replay, no second payment.
	r := owner.expect(http.StatusCreated, http.MethodPost, base, payBody(acc.ID, 15000, "2026-12-05"), "Idempotency-Key", key)
	if r.header.Get("Idempotent-Replayed") != "true" {
		t.Fatal("not replayed")
	}
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, base, payBody(acc.ID, 1, "2026-12-05"),
		"Idempotency-Key", uuid.NewString()).errorCode(t); code != "INVOICE_ALREADY_PAID" {
		t.Fatalf("got %s", code)
	}
	// Paying a future (OPEN) invoice in advance.
	if d := owner.payInvoice(h.ID, last, payBody(acc.ID, 20000, "2026-12-05")); d.Invoice.Status != "OPEN" || d.Invoice.PaymentStatus != "PAID" {
		t.Fatalf("advance = %+v", d.Invoice)
	}
	if b := owner.balance(h.ID, acc.ID); b != 100000-40000 {
		t.Fatalf("balance = %d", b)
	}
	if c := api.consumption(h.ID); c != 120000 {
		t.Fatalf("consumption = %d", c)
	}

	// Validation and unknown references: nothing written.
	archived := owner.createAccount(h.ID, map[string]any{"name": "Velha"})
	owner.expect(http.StatusOK, http.MethodPatch, "/api/v1/households/"+h.ID+"/accounts/"+archived.ID, map[string]any{"isActive": false})
	second := "/api/v1/households/" + h.ID + "/invoices/" + p.Installments[1].InvoiceID + "/payments"
	for _, tc := range []struct {
		name   string
		path   string
		body   map[string]any
		status int
		code   string
		field  string
	}{
		{"zero", second, payBody(acc.ID, 0, "2026-12-05"), 400, "VALIDATION_ERROR", "amountCents"},
		{"negative", second, payBody(acc.ID, -5, "2026-12-05"), 400, "VALIDATION_ERROR", "amountCents"},
		{"bad date", second, payBody(acc.ID, 100, "05/12/2026"), 400, "VALIDATION_ERROR", "paidOn"},
		{"no account", second, map[string]any{"amountCents": 100, "paidOn": "2026-12-05"}, 400, "VALIDATION_ERROR", "accountId"},
		{"unknown account", second, payBody(uuid.NewString(), 100, "2026-12-05"), 422, "ACCOUNT_NOT_FOUND", "accountId"},
		{"archived account", second, payBody(archived.ID, 100, "2026-12-05"), 422, "ACCOUNT_ARCHIVED", "accountId"},
		{"unknown invoice", "/api/v1/households/" + h.ID + "/invoices/" + uuid.NewString() + "/payments", payBody(acc.ID, 100, "2026-12-05"), 404, "INVOICE_NOT_FOUND", ""},
	} {
		r := owner.expect(tc.status, http.MethodPost, tc.path, tc.body, "Idempotency-Key", uuid.NewString())
		if code := r.errorCode(t); code != tc.code || (tc.field != "" && !slices.Contains(r.errorFields(t), tc.field)) {
			t.Errorf("%s: %s", tc.name, r.body)
		}
	}
	if n := api.count("SELECT count(*) FROM invoice_payments WHERE household_id = $1", h.ID); n != 3 {
		t.Fatalf("payments = %d, want 3", n)
	}
	// Audit: one event per payment, without amounts.
	if a := api.auditActions(first); !slices.Equal(a, []string{"card_invoice.paid", "card_invoice.closed", "card_invoice.paid"}) {
		t.Fatalf("audit = %v", a)
	}
	var meta string
	if err := api.pool.QueryRow(context.Background(),
		"SELECT string_agg(metadata::text, ' ') FROM audit_events WHERE entity_id = $1", first).Scan(&meta); err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"5000", "15000"} {
		if strings.Contains(meta, leak) {
			t.Fatalf("audit metadata leaks %q: %s", leak, meta)
		}
	}
	assertInvariants(t)
}

// RN-012: an adjustment corrects the balance with a sign and a reason, on an OPEN or CLOSED
// invoice, without touching the installments; it never leaves the balance below zero.
func TestAdjustInvoice(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 100000})
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	p := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 40000, 2, "2026-11-10"))
	first := p.Installments[0].InvoiceID
	base := "/api/v1/households/" + h.ID + "/invoices/" + first + "/adjustments"

	d := owner.adjustInvoice(h.ID, first, 990, "Anuidade")
	if d.Invoice.AdjustmentsCents != 990 || d.Invoice.TotalCents != 20990 || d.Invoice.InstallmentsCents != 20000 ||
		len(d.Adjustments) != 1 || d.Adjustments[0].Reason != "Anuidade" || d.Adjustments[0].AmountCents != 990 {
		t.Fatalf("after fee = %+v", d)
	}
	d = owner.adjustInvoice(h.ID, first, -500, "  Estorno da loja  ")
	if d.Invoice.TotalCents != 20490 || d.Adjustments[1].Reason != "Estorno da loja" {
		t.Fatalf("after refund = %+v", d)
	}
	// Consumption is the purchases only (INV-10); the adjustment is commitment.
	if c := api.consumption(h.ID); c != 40000 {
		t.Fatalf("consumption = %d", c)
	}

	// Close, then adjust the CLOSED invoice: the frozen base stays (INV-15).
	api.setToday("2026-12-03")
	owner.closeInvoice(h.ID, first)
	d = owner.adjustInvoice(h.ID, first, 1000, "Juros")
	if d.Invoice.Status != "CLOSED" || *d.Invoice.ClosedBaseAmountCents != 20000 || d.Invoice.TotalCents != 21490 {
		t.Fatalf("closed + adjustment = %+v", d.Invoice)
	}
	// Pay it all; a credit afterwards would make the balance negative.
	owner.payInvoice(h.ID, first, payBody(acc.ID, 21490, "2026-12-05"))
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, base, map[string]any{"amountCents": -1, "reason": "Crédito"},
		"Idempotency-Key", uuid.NewString()).errorCode(t); code != "ADJUSTMENT_EXCEEDS_BALANCE" {
		t.Fatalf("got %s", code)
	}
	// A late fee on a paid invoice reopens a balance.
	if d := owner.adjustInvoice(h.ID, first, 300, "Multa"); d.Invoice.OutstandingCents != 300 || d.Invoice.PaymentStatus != "PARTIALLY_PAID" {
		t.Fatalf("after late fee = %+v", d.Invoice)
	}
	// A credit down to exactly the paid amount is accepted.
	if d := owner.adjustInvoice(h.ID, first, -300, "Multa indevida"); d.Invoice.OutstandingCents != 0 || d.Invoice.PaymentStatus != "PAID" {
		t.Fatalf("after credit = %+v", d.Invoice)
	}

	for _, tc := range []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"zero", map[string]any{"amountCents": 0, "reason": "x"}, "amountCents"},
		{"no reason", map[string]any{"amountCents": 100}, "reason"},
		{"blank reason", map[string]any{"amountCents": 100, "reason": "   "}, "reason"},
		{"long reason", map[string]any{"amountCents": 100, "reason": strings.Repeat("a", 201)}, "reason"},
	} {
		r := owner.expect(http.StatusBadRequest, http.MethodPost, base, tc.body, "Idempotency-Key", uuid.NewString())
		if !slices.Contains(r.errorFields(t), tc.field) {
			t.Errorf("%s: %s", tc.name, r.body)
		}
	}
	// Audit without amount or reason (free text).
	var meta string
	if err := api.pool.QueryRow(context.Background(),
		"SELECT string_agg(metadata::text, ' ') FROM audit_events WHERE entity_id = $1 AND action = 'card_invoice.adjusted'", first).Scan(&meta); err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"Anuidade", "990", "Juros"} {
		if strings.Contains(meta, leak) {
			t.Fatalf("audit metadata leaks %q: %s", leak, meta)
		}
	}
	assertInvariants(t)
}

// I-49: a card is archived only when nothing is left to pay on its invoices (future
// installments included). An archived card keeps its invoices and accepts no adjustment.
func TestArchiveCardRequiresNoOpenBalance(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 100000})
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	p := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 3000, 2, "2026-11-10"))
	path := "/api/v1/households/" + h.ID + "/cards/" + card.ID

	owner.payInvoice(h.ID, p.Installments[0].InvoiceID, payBody(acc.ID, 1500, "2026-11-12"))
	// The second installment (a future invoice) is still open.
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPatch, path, map[string]any{"isActive": false}).errorCode(t); code != "CARD_HAS_OPEN_BALANCE" {
		t.Fatalf("got %s", code)
	}
	owner.payInvoice(h.ID, p.Installments[1].InvoiceID, payBody(acc.ID, 1500, "2026-11-12"))
	owner.expect(http.StatusOK, http.MethodPatch, path, map[string]any{"isActive": false})

	// The archived card lists its invoices and accepts no adjustment.
	if l, _ := owner.cardInvoices(h.ID, card.ID, ""); len(l) != 2 {
		t.Fatalf("archived card lists %d invoices", len(l))
	}
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost,
		"/api/v1/households/"+h.ID+"/invoices/"+p.Installments[0].InvoiceID+"/adjustments",
		map[string]any{"amountCents": 100, "reason": "Tarifa"}, "Idempotency-Key", uuid.NewString()).errorCode(t); code != "CARD_ARCHIVED" {
		t.Fatalf("got %s", code)
	}
	owner.expect(http.StatusOK, http.MethodPatch, path, map[string]any{"isActive": true})
	assertInvariants(t)
}

// T17 on invoices: N payments of the whole balance at the same time, with different keys:
// exactly one is accepted and the cash moves once (invoice lock, ADR 0007).
func TestConcurrentInvoicePayments(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 100000})
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	invoiceID := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 20000, 1, "2026-11-10")).Installments[0].InvoiceID

	const n = 10
	var wg sync.WaitGroup
	codes := make(chan string, n)
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r := owner.do(http.MethodPost, "/api/v1/households/"+h.ID+"/invoices/"+invoiceID+"/payments",
				payBody(acc.ID, 20000, "2026-11-12"), "Idempotency-Key", uuid.NewString())
			if r.status == http.StatusCreated {
				codes <- "CREATED"
				return
			}
			codes <- r.errorCode(t)
		}()
	}
	close(start)
	wg.Wait()
	close(codes)
	created := 0
	for c := range codes {
		switch c {
		case "CREATED":
			created++
		case "INVOICE_ALREADY_PAID", "AMOUNT_EXCEEDS_OUTSTANDING":
		default:
			t.Fatalf("unexpected result %s", c)
		}
	}
	if created != 1 {
		t.Fatalf("%d payments accepted, want 1", created)
	}
	if b := owner.balance(h.ID, acc.ID); b != 80000 {
		t.Fatalf("balance = %d", b)
	}
	assertInvariants(t)
}

// The database keeps payments and adjustments as history and never lets an invoice be paid
// beyond its total (INV-06), even if a bug skipped the service.
func TestInvoicePaymentConstraints(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 100000})
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	invoiceID := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 20000, 1, "2026-11-10")).Installments[0].InvoiceID
	owner.payInvoice(h.ID, invoiceID, payBody(acc.ID, 15000, "2026-11-12"))
	owner.adjustInvoice(h.ID, invoiceID, 100, "Tarifa")
	ctx := context.Background()

	for name, sql := range map[string]string{
		"payment edited":     `UPDATE invoice_payments SET amount_cents = 1 WHERE household_id = $1`,
		"payment deleted":    `DELETE FROM invoice_payments WHERE household_id = $1`,
		"adjustment edited":  `UPDATE invoice_adjustments SET amount_cents = 1 WHERE household_id = $1`,
		"adjustment deleted": `DELETE FROM invoice_adjustments WHERE household_id = $1`,
		"zero adjustment": `INSERT INTO invoice_adjustments (id, household_id, card_invoice_id, amount_cents, reason, created_by_member_id)
			VALUES (gen_random_uuid(), $1, '` + invoiceID + `', 0, 'x', '` + h.MemberID + `')`,
		"credit below the paid amount": `INSERT INTO invoice_adjustments (id, household_id, card_invoice_id, amount_cents, reason, created_by_member_id)
			VALUES (gen_random_uuid(), $1, '` + invoiceID + `', -5101, 'x', '` + h.MemberID + `')`,
	} {
		if _, err := api.pool.Exec(ctx, sql, h.ID); err == nil {
			t.Errorf("%s: the database must reject it", name)
		}
	}
	// A payment beyond the total, with its movement, in one transaction rolled back.
	tx, err := api.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	movementID := uuid.NewString()
	if _, err := tx.Exec(ctx, `INSERT INTO account_movements (id, household_id, account_id, direction, amount_cents, posted_on, kind, description)
		VALUES ($1, $2, $3, 'OUT', 5101, '2026-11-12', 'INVOICE_PAYMENT', 'x')`, movementID, h.ID, acc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO invoice_payments (id, household_id, card_invoice_id, account_movement_id, amount_cents, paid_on, created_by_member_id)
		VALUES (gen_random_uuid(), $1, $2, $3, 5101, '2026-11-12', $4)`, h.ID, invoiceID, movementID, h.MemberID); err == nil {
		t.Error("overpayment: the database must reject it")
	}
	_ = tx.Rollback(ctx)
	assertInvariants(t)
}

// ADR 0007: a payment waits for another one in progress on the same invoice (which holds the
// invoice lock) and then sees the new balance, instead of paying twice.
func TestInvoicePaymentWaitsForAPaymentInProgress(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 100000})
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	invoiceID := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 20000, 1, "2026-11-10")).Installments[0].InvoiceID

	// What PayInvoice does in another request: lock the invoice, move the cash, record it.
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
		r = owner.do(http.MethodPost, "/api/v1/households/"+h.ID+"/invoices/"+invoiceID+"/payments",
			payBody(acc.ID, 20000, "2026-11-12"), "Idempotency-Key", uuid.NewString())
	}()
	select {
	case r := <-done:
		t.Fatalf("payment did not wait for the one in progress: %d %s", r.status, r.body)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.status != http.StatusUnprocessableEntity || r.errorCode(t) != "INVOICE_ALREADY_PAID" {
		t.Fatalf("second payment = %d %s", r.status, r.body)
	}
	if b := owner.balance(h.ID, acc.ID); b != 80000 {
		t.Fatalf("balance = %d", b)
	}
	assertInvariants(t)
}
