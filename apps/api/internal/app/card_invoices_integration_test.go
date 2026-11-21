//go:build integration

package app

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type invoiceDTO struct {
	ID                    string  `json:"id"`
	CreditCardID          string  `json:"creditCardId"`
	CycleStartOn          string  `json:"cycleStartOn"`
	ClosingOn             string  `json:"closingOn"`
	DueOn                 string  `json:"dueOn"`
	ReferenceMonth        string  `json:"referenceMonth"`
	Status                string  `json:"status"`
	PaymentStatus         string  `json:"paymentStatus"`
	ClosingPending        bool    `json:"closingPending"`
	InstallmentsCount     int     `json:"installmentsCount"`
	InstallmentsCents     int64   `json:"installmentsCents"`
	AdjustmentsCents      int64   `json:"adjustmentsCents"`
	TotalCents            int64   `json:"totalCents"`
	PaidCents             int64   `json:"paidCents"`
	OutstandingCents      int64   `json:"outstandingCents"`
	ClosedBaseAmountCents *int64  `json:"closedBaseAmountCents"`
	ClosedAt              *string `json:"closedAt"`
	ClosedByMemberID      *string `json:"closedByMemberId"`
}

type invoiceLineDTO struct {
	ID                  string  `json:"id"`
	TransactionID       string  `json:"transactionId"`
	Number              int     `json:"number"`
	Total               int     `json:"total"`
	AmountCents         int64   `json:"amountCents"`
	Status              string  `json:"status"`
	OccurredOn          string  `json:"occurredOn"`
	Description         string  `json:"description"`
	Merchant            *string `json:"merchant"`
	PurchaseAmountCents int64   `json:"purchaseAmountCents"`
}

type invoicePaymentDTO struct {
	ID          string `json:"id"`
	AmountCents int64  `json:"amountCents"`
	PaidOn      string `json:"paidOn"`
	AccountID   string `json:"accountId"`
	MovementID  string `json:"movementId"`
}

type invoiceAdjustmentDTO struct {
	ID          string `json:"id"`
	AmountCents int64  `json:"amountCents"`
	Reason      string `json:"reason"`
}

type invoiceDetailDTO struct {
	Invoice      invoiceDTO             `json:"invoice"`
	Installments []invoiceLineDTO       `json:"installments"`
	Payments     []invoicePaymentDTO    `json:"payments"`
	Adjustments  []invoiceAdjustmentDTO `json:"adjustments"`
}

// setToday moves the fake clock to noon UTC of date (morning in America/Sao_Paulo, the
// households' default timezone).
func (api *testAPI) setToday(date string) {
	api.t.Helper()
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		api.t.Fatal(err)
	}
	api.clock.Set(d.Add(12 * time.Hour))
}

// cardInvoices lists one page of a card's invoices through the API.
func (c *client) cardInvoices(householdID, cardID, query string) ([]invoiceDTO, *string) {
	c.api.t.Helper()
	var page struct {
		Items      []invoiceDTO `json:"items"`
		NextCursor *string      `json:"nextCursor"`
	}
	c.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+householdID+"/cards/"+cardID+"/invoices"+query, nil).decode(c.api.t, &page)
	return page.Items, page.NextCursor
}

func (c *client) invoice(householdID, invoiceID string) invoiceDetailDTO {
	c.api.t.Helper()
	var d invoiceDetailDTO
	c.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+householdID+"/invoices/"+invoiceID, nil).decode(c.api.t, &d)
	return d
}

// closeInvoice closes an invoice with a fresh Idempotency-Key.
func (c *client) closeInvoice(householdID, invoiceID string) invoiceDetailDTO {
	c.api.t.Helper()
	var d invoiceDetailDTO
	c.expect(http.StatusOK, http.MethodPost, "/api/v1/households/"+householdID+"/invoices/"+invoiceID+"/close", nil,
		"Idempotency-Key", uuid.NewString()).decode(c.api.t, &d)
	return d
}

func dueDates(l []invoiceDTO) []string {
	out := make([]string, 0, len(l))
	for _, i := range l {
		out = append(out, i.DueOn)
	}
	return out
}

// F4.4: a card's invoices by due date, with totals derived from their ACTIVE installments
// (commitment, never summed again outside the invoice), paginated and filtered by due date.
func TestListCardInvoices(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil)) // closes on the 3rd, due on the 10th
	owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 120000, 6, "2026-11-10"))
	owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 10001, 3, "2026-12-02"))

	list, next := owner.cardInvoices(h.ID, card.ID, "")
	want := []string{"2026-12-10", "2027-01-10", "2027-02-10", "2027-03-10", "2027-04-10", "2027-05-10"}
	if got := dueDates(list); !slices.Equal(got, want) || next != nil {
		t.Fatalf("due dates = %v (next %v), want %v", got, next, want)
	}
	first := list[0]
	if first.CreditCardID != card.ID || first.CycleStartOn != "2026-11-03" || first.ClosingOn != "2026-12-03" ||
		first.ReferenceMonth != "2026-12" || first.Status != "OPEN" || first.PaymentStatus != "UNPAID" || first.ClosingPending ||
		first.InstallmentsCount != 2 || first.InstallmentsCents != 23334 || first.TotalCents != 23334 ||
		first.PaidCents != 0 || first.OutstandingCents != 23334 || first.ClosedBaseAmountCents != nil || first.ClosedAt != nil {
		t.Fatalf("first invoice = %+v", first)
	}
	if last := list[5]; last.TotalCents != 20000 || last.InstallmentsCount != 1 || last.ReferenceMonth != "2027-05" {
		t.Fatalf("last invoice = %+v", last)
	}
	// The invoices add up to the purchases: R$ 1.200 + R$ 100,01.
	var sum int64
	for _, i := range list {
		sum += i.TotalCents
	}
	if sum != 130001 {
		t.Fatalf("invoices total %d", sum)
	}

	// Keyset pagination by due date, and the due-date filter (reference month).
	page, next := owner.cardInvoices(h.ID, card.ID, "?limit=4")
	if len(page) != 4 || next == nil {
		t.Fatalf("page 1 = %d items, next %v", len(page), next)
	}
	page2, next2 := owner.cardInvoices(h.ID, card.ID, "?limit=4&cursor="+*next)
	if got := dueDates(append(page, page2...)); !slices.Equal(got, want) || next2 != nil {
		t.Fatalf("pages = %v (next %v)", got, next2)
	}
	if got, _ := owner.cardInvoices(h.ID, card.ID, "?from=2027-01-01&to=2027-02-28"); !slices.Equal(dueDates(got), want[1:3]) {
		t.Fatalf("filtered = %v", dueDates(got))
	}

	// An empty cycle between two purchases is listed with total zero (nothing to pay).
	other := owner.createCard(h.ID, cardBody(h.MemberID, map[string]any{"name": "Outro"}))
	owner.purchase(h.ID, purchaseBody(cat.ID, other.ID, 1000, 1, "2026-11-10"))
	owner.purchase(h.ID, purchaseBody(cat.ID, other.ID, 1000, 1, "2027-01-10"))
	gap, _ := owner.cardInvoices(h.ID, other.ID, "")
	if len(gap) != 3 || gap[1].TotalCents != 0 || gap[1].InstallmentsCount != 0 || gap[1].PaymentStatus != "PAID" {
		t.Fatalf("invoices with a gap = %+v", gap)
	}

	base := "/api/v1/households/" + h.ID + "/cards/"
	for _, tc := range []struct {
		path   string
		status int
		code   string
	}{
		{card.ID + "/invoices?from=2027-02-01&to=2027-01-01", 400, "VALIDATION_ERROR"},
		{card.ID + "/invoices?from=01/02/2027", 400, "VALIDATION_ERROR"},
		{card.ID + "/invoices?cursor=xyz", 400, "VALIDATION_ERROR"},
		{card.ID + "/invoices?limit=101", 400, "VALIDATION_ERROR"},
		{uuid.NewString() + "/invoices", 404, "CARD_NOT_FOUND"},
		{"not-a-uuid/invoices", 404, "CARD_NOT_FOUND"},
	} {
		if code := owner.expect(tc.status, http.MethodGet, base+tc.path, nil).errorCode(t); code != tc.code {
			t.Errorf("%s: got %s, want %s", tc.path, code, tc.code)
		}
	}
}

// F4.4: invoice detail lists its lines with the purchase each one belongs to.
func TestInvoiceDetail(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	tv := purchaseBody(cat.ID, card.ID, 120000, 6, "2026-11-10")
	tv["description"], tv["merchant"] = "Televisão", "Loja"
	p1 := owner.purchase(h.ID, tv)
	p2 := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 10001, 3, "2026-11-05"))

	d := owner.invoice(h.ID, p1.Installments[0].InvoiceID)
	if d.Invoice.ID != p1.Installments[0].InvoiceID || d.Invoice.TotalCents != 23334 || len(d.Installments) != 2 {
		t.Fatalf("detail = %+v", d)
	}
	// By purchase date: the 3x purchase of 05/11 first, then the TV.
	a, b := d.Installments[0], d.Installments[1]
	if a.TransactionID != p2.Transaction.ID || a.Number != 1 || a.Total != 3 || a.AmountCents != 3334 || a.Status != "ACTIVE" ||
		a.OccurredOn != "2026-11-05" || a.PurchaseAmountCents != 10001 {
		t.Fatalf("first line = %+v", a)
	}
	if b.TransactionID != p1.Transaction.ID || b.Number != 1 || b.Total != 6 || b.AmountCents != 20000 ||
		b.Description != "Televisão" || b.Merchant == nil || *b.Merchant != "Loja" || b.PurchaseAmountCents != 120000 {
		t.Fatalf("second line = %+v", b)
	}
	// The sixth installment of the TV is alone on its invoice.
	if last := owner.invoice(h.ID, p1.Installments[5].InvoiceID); len(last.Installments) != 1 || last.Installments[0].Number != 6 {
		t.Fatalf("last invoice = %+v", last)
	}

	base := "/api/v1/households/" + h.ID + "/invoices/"
	for _, path := range []string{uuid.NewString(), "not-a-uuid"} {
		if code := owner.expect(http.StatusNotFound, http.MethodGet, base+path, nil).errorCode(t); code != "INVOICE_NOT_FOUND" {
			t.Errorf("%s: got %s", path, code)
		}
	}
}

// F4.4 / I-15: closing is manual, from the closing date on (household's date), and freezes
// the base at the sum of the ACTIVE installments (INV-15). Closing again changes nothing; a
// late purchase skips the closed invoice (allocationShifted).
func TestCloseInvoice(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	member, _ := api.registerUser("member")
	var joined householdDTO
	member.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept",
		map[string]any{"token": owner.createInvite(h.ID).Token}).decode(t, &joined)
	owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 120000, 6, "2026-11-10"))
	owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 10001, 3, "2026-12-02"))
	invoices, _ := owner.cardInvoices(h.ID, card.ID, "")
	first := invoices[0]
	path := "/api/v1/households/" + h.ID + "/invoices/" + first.ID + "/close"

	// Before the closing date (in the household's timezone): 422, nothing changes. At 23:30
	// in São Paulo on 02/12 it is already 03/12 in UTC, and still not closable.
	api.clock.Set(time.Date(2026, 12, 3, 2, 30, 0, 0, time.UTC))
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, path, nil,
		"Idempotency-Key", uuid.NewString()).errorCode(t); code != "INVOICE_NOT_CLOSABLE_YET" {
		t.Fatalf("got %s", code)
	}
	if code := owner.expect(http.StatusBadRequest, http.MethodPost, path, nil).errorCode(t); code != "IDEMPOTENCY_KEY_REQUIRED" {
		t.Fatalf("got %s", code)
	}
	if d := owner.invoice(h.ID, first.ID); d.Invoice.Status != "OPEN" || d.Invoice.ClosingPending {
		t.Fatalf("invoice = %+v", d.Invoice)
	}

	// On the closing date the UI flags "closing pending" (no auto-close); any member may close.
	api.setToday("2026-12-03")
	if l, _ := owner.cardInvoices(h.ID, card.ID, "?limit=2"); !l[0].ClosingPending || l[1].ClosingPending {
		t.Fatalf("closingPending = %v %v", l[0].ClosingPending, l[1].ClosingPending)
	}
	key := uuid.NewString()
	var closed invoiceDetailDTO
	member.expect(http.StatusOK, http.MethodPost, path, nil, "Idempotency-Key", key).decode(t, &closed)
	inv := closed.Invoice
	if inv.Status != "CLOSED" || inv.ClosedBaseAmountCents == nil || *inv.ClosedBaseAmountCents != 23334 ||
		inv.ClosedAt == nil || inv.ClosedByMemberID == nil || *inv.ClosedByMemberID != joined.MemberID ||
		inv.ClosingPending || inv.TotalCents != 23334 || inv.OutstandingCents != 23334 || inv.PaymentStatus != "UNPAID" ||
		len(closed.Installments) != 2 {
		t.Fatalf("closed = %+v", closed)
	}

	// Same key: replay. New key: the invoice is already closed, nothing changes.
	r := member.expect(http.StatusOK, http.MethodPost, path, nil, "Idempotency-Key", key)
	if r.header.Get("Idempotent-Replayed") != "true" {
		t.Fatal("not replayed")
	}
	again := owner.closeInvoice(h.ID, first.ID)
	if again.Invoice.Status != "CLOSED" || *again.Invoice.ClosedByMemberID != joined.MemberID || *again.Invoice.ClosedAt != *inv.ClosedAt {
		t.Fatalf("closed again = %+v", again.Invoice)
	}
	if a := api.auditActions(first.ID); !slices.Equal(a, []string{"card_invoice.closed"}) {
		t.Fatalf("audit = %v", a)
	}

	// A late purchase that belonged to the closed cycle goes to the next open invoice.
	p := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 5000, 1, "2026-11-20"))
	if !p.AllocationShifted || p.Installments[0].InvoiceID != invoices[1].ID {
		t.Fatalf("late purchase = %+v", p)
	}
	if d := owner.invoice(h.ID, first.ID); d.Invoice.TotalCents != 23334 || len(d.Installments) != 2 {
		t.Fatalf("closed invoice changed: %+v", d.Invoice)
	}
	// The next invoice is not closable yet.
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/households/"+h.ID+"/invoices/"+invoices[1].ID+"/close",
		nil, "Idempotency-Key", uuid.NewString()).errorCode(t); code != "INVOICE_NOT_CLOSABLE_YET" {
		t.Fatalf("got %s", code)
	}
	// An empty invoice (the cycle closing on 2027-01-03) closes with base zero. The clock moves
	// in steps shorter than the session's idle lifetime (ADR 0005).
	other := owner.createCard(h.ID, cardBody(h.MemberID, map[string]any{"name": "Outro"}))
	owner.purchase(h.ID, purchaseBody(cat.ID, other.ID, 1000, 1, "2026-11-10"))
	owner.purchase(h.ID, purchaseBody(cat.ID, other.ID, 1000, 1, "2027-01-10"))
	api.setToday("2026-12-20")
	owner.cardInvoices(h.ID, other.ID, "")
	api.setToday("2027-01-03")
	gap, _ := owner.cardInvoices(h.ID, other.ID, "")
	if d := owner.closeInvoice(h.ID, gap[1].ID); *d.Invoice.ClosedBaseAmountCents != 0 {
		t.Fatalf("empty invoice = %+v", d.Invoice)
	}
	assertInvariants(t)
}

// ADR 0007: closing and purchases lock the invoice, so a purchase racing the close either
// lands before it (in the frozen base) or is shifted to the next invoice; never in between.
// Concurrent closes record one close.
func TestConcurrentCloseAndPurchases(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 1000, 2, "2026-11-10"))
	first := api.invoices(card.ID)[0].ID
	api.setToday("2026-12-03")

	const purchases, closes = 10, 5
	var wg sync.WaitGroup
	statuses := make(chan int, purchases+closes)
	start := make(chan struct{})
	for i := range purchases + closes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if i < purchases {
				statuses <- owner.do(http.MethodPost, "/api/v1/households/"+h.ID+"/transactions",
					purchaseBody(cat.ID, card.ID, 700, 1, "2026-12-01"), "Idempotency-Key", uuid.NewString()).status
				return
			}
			statuses <- owner.do(http.MethodPost, "/api/v1/households/"+h.ID+"/invoices/"+first+"/close", nil,
				"Idempotency-Key", uuid.NewString()).status
		}()
	}
	close(start)
	wg.Wait()
	close(statuses)
	for s := range statuses {
		if s != http.StatusCreated && s != http.StatusOK {
			t.Fatalf("request failed with %d", s)
		}
	}
	d := owner.invoice(h.ID, first)
	before := len(d.Installments) - 1 // purchases that landed before the close
	if d.Invoice.Status != "CLOSED" || *d.Invoice.ClosedBaseAmountCents != d.Invoice.TotalCents ||
		d.Invoice.TotalCents != 500+int64(before)*700 {
		t.Fatalf("closed invoice = %+v (%d lines)", d.Invoice, len(d.Installments))
	}
	// Every purchase landed exactly once: the rest went to the next invoice.
	next := owner.invoice(h.ID, api.invoices(card.ID)[1].ID)
	if got := before + len(next.Installments) - 1; got != purchases {
		t.Fatalf("%d purchases placed, want %d", got, purchases)
	}
	if a := api.auditActions(first); !slices.Equal(a, []string{"card_invoice.closed"}) {
		t.Fatalf("audit = %v", a)
	}
	assertInvariants(t)
}

// INV-15 in the database: a closed invoice is frozen even if a bug skipped the service.
func TestClosedInvoiceIsFrozenInTheDatabase(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	p := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 3000, 2, "2026-11-10"))
	closedID, openID := p.Installments[0].InvoiceID, p.Installments[1].InvoiceID
	api.setToday("2026-12-03")
	owner.closeInvoice(h.ID, closedID)
	ctx := context.Background()

	for name, sql := range map[string]string{
		"installment into a closed invoice": `INSERT INTO card_installments (id, household_id, transaction_id, credit_card_id,
			card_invoice_id, installment_number, installment_total, amount_cents)
			VALUES (gen_random_uuid(), $1, '` + p.Transaction.ID + `', '` + card.ID + `', '` + closedID + `', 3, 3, 1)`,
		"installment of a closed invoice cancelled": `UPDATE card_installments SET status = 'CANCELLED'
			WHERE household_id = $1 AND card_invoice_id = '` + closedID + `'`,
		"installment moved out of a closed invoice": `UPDATE card_installments SET card_invoice_id = '` + openID + `'
			WHERE household_id = $1 AND card_invoice_id = '` + closedID + `'`,
		"installment moved into a closed invoice": `UPDATE card_installments SET card_invoice_id = '` + closedID + `'
			WHERE household_id = $1 AND card_invoice_id = '` + openID + `'`,
		"installment deleted":     `DELETE FROM card_installments WHERE household_id = $1 AND card_invoice_id = '` + openID + `'`,
		"closed invoice reopened": `UPDATE card_invoices SET status = 'OPEN', closed_base_amount_cents = NULL, closed_at = NULL, closed_by_member_id = NULL WHERE household_id = $1 AND id = '` + closedID + `'`,
		"closed base changed":     `UPDATE card_invoices SET closed_base_amount_cents = 1 WHERE household_id = $1 AND id = '` + closedID + `'`,
		"base other than the installments": `UPDATE card_invoices SET status = 'CLOSED', closed_base_amount_cents = 1, closed_at = now(),
			closed_by_member_id = '` + h.MemberID + `' WHERE household_id = $1 AND id = '` + openID + `'`,
		"open invoice dates changed": `UPDATE card_invoices SET due_on = due_on + 1 WHERE household_id = $1 AND id = '` + openID + `'`,
		"invoice deleted":            `DELETE FROM card_invoices WHERE household_id = $1 AND id = '` + openID + `'`,
	} {
		if _, err := api.pool.Exec(ctx, sql, h.ID); err == nil {
			t.Errorf("%s: the database must reject it", name)
		}
	}
	assertInvariants(t)
}

// ADR 0007: a close waits for a purchase that already locked the invoice (as
// PlaceInstallments does) and freezes a base that includes it (INV-15).
func TestCloseWaitsForAPurchaseInProgress(t *testing.T) {
	api := newTestAPI(t)
	api.setToday("2026-11-10")
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	invoiceID := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 1000, 1, "2026-11-10")).Installments[0].InvoiceID
	api.setToday("2026-12-03")

	ctx := context.Background()
	tx, err := api.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txID := uuid.NewString()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{"SELECT 1 FROM credit_cards WHERE id = $1 FOR UPDATE", []any{card.ID}},
		{"SELECT 1 FROM card_invoices WHERE id = $1 FOR UPDATE", []any{invoiceID}},
		{`INSERT INTO transactions (id, household_id, kind, scope, created_by_member_id, description, amount_cents,
		      category_id, occurred_on, payment_method, credit_card_id)
		  VALUES ($1, $2, 'EXPENSE', 'FAMILY', $3, 'Em andamento', 700, $4, '2026-12-01', 'CREDIT_CARD', $5)`,
			[]any{txID, h.ID, h.MemberID, cat.ID, card.ID}},
		{`INSERT INTO card_installments (id, household_id, transaction_id, credit_card_id, card_invoice_id,
		      installment_number, installment_total, amount_cents)
		  VALUES (gen_random_uuid(), $1, $2, $3, $4, 1, 1, 700)`, []any{h.ID, txID, card.ID, invoiceID}},
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
		r = owner.do(http.MethodPost, "/api/v1/households/"+h.ID+"/invoices/"+invoiceID+"/close", nil,
			"Idempotency-Key", uuid.NewString())
	}()
	select {
	case r := <-done:
		t.Fatalf("close did not wait for the purchase: %d %s", r.status, r.body)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.status != http.StatusOK {
		t.Fatalf("close = %d %s", r.status, r.body)
	}
	var d invoiceDetailDTO
	r.decode(t, &d)
	if *d.Invoice.ClosedBaseAmountCents != 1700 || len(d.Installments) != 2 {
		t.Fatalf("closed = %+v", d.Invoice)
	}
	assertInvariants(t)
}
