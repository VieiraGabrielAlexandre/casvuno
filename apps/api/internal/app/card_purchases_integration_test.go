//go:build integration

package app

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type installmentDTO struct {
	Number      int    `json:"number"`
	AmountCents int64  `json:"amountCents"`
	InvoiceID   string `json:"invoiceId"`
	ClosingOn   string `json:"closingOn"`
	DueOn       string `json:"dueOn"`
}

// purchaseDTO is the response of POST /transactions for a card purchase.
type purchaseDTO struct {
	Transaction       *transactionDTO  `json:"transaction"`
	Obligation        *obligationDTO   `json:"obligation"`
	Installments      []installmentDTO `json:"installments"`
	AllocationShifted bool             `json:"allocationShifted"`
}

// purchaseBody is a family expense on the card in n installments.
func purchaseBody(categoryID, cardID string, cents int64, n int, occurredOn string) map[string]any {
	return map[string]any{
		"kind": "EXPENSE", "scope": "FAMILY", "description": "Compra", "amountCents": cents,
		"categoryId": categoryID, "occurredOn": occurredOn,
		"payment": map[string]any{"method": "CREDIT_CARD", "cardId": cardID, "installments": n},
	}
}

// purchase records a card purchase with a fresh Idempotency-Key.
func (c *client) purchase(householdID string, body map[string]any) purchaseDTO {
	c.api.t.Helper()
	var p purchaseDTO
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/households/"+householdID+"/transactions", body,
		"Idempotency-Key", uuid.NewString()).decode(c.api.t, &p)
	if p.Transaction == nil || p.Obligation != nil {
		c.api.t.Fatalf("a card purchase returns its transaction and no obligation: %+v", p)
	}
	return p
}

type invoiceRow struct {
	ID, Start, Closing, Due, Status string
}

// invoices returns a card's invoices in cycle order (read from the database until the
// invoice routes exist, F4.4).
func (api *testAPI) invoices(cardID string) []invoiceRow {
	api.t.Helper()
	rows, err := api.pool.Query(context.Background(), `
		SELECT id::text, cycle_start_on::text, closing_on::text, due_on::text, status
		FROM card_invoices WHERE credit_card_id = $1 ORDER BY cycle_start_on`, cardID)
	if err != nil {
		api.t.Fatal(err)
	}
	defer rows.Close()
	var out []invoiceRow
	for rows.Next() {
		var r invoiceRow
		if err := rows.Scan(&r.ID, &r.Start, &r.Closing, &r.Due, &r.Status); err != nil {
			api.t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// invoiceTotal sums the active installments of one invoice.
func (api *testAPI) invoiceTotal(invoiceID string) int64 {
	api.t.Helper()
	var total int64
	if err := api.pool.QueryRow(context.Background(), `
		SELECT COALESCE(sum(amount_cents), 0) FROM card_installments WHERE card_invoice_id = $1 AND status = 'ACTIVE'`,
		invoiceID).Scan(&total); err != nil {
		api.t.Fatal(err)
	}
	return total
}

func closings(l []installmentDTO) []string {
	out := make([]string, 0, len(l))
	for _, i := range l {
		out = append(out, i.ClosingOn)
	}
	return out
}

func (api *testAPI) count(query string, args ...any) int {
	api.t.Helper()
	var n int
	if err := api.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		api.t.Fatal(err)
	}
	return n
}

// T05: R$ 1.200 in 6x is one consumption of R$ 1.200 and six installments of R$ 200 on six
// consecutive invoices; no obligation, no cash (RN-009, INV-02/03).
func TestCardPurchaseInInstallments(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 100000})
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil)) // closes on the 3rd, due on the 10th
	base := "/api/v1/households/" + h.ID + "/transactions"
	movements := api.count("SELECT count(*) FROM account_movements WHERE household_id = $1", h.ID)

	body := purchaseBody(cat.ID, card.ID, 120000, 6, "2026-11-10")
	body["merchant"] = "Loja"
	p := owner.purchase(h.ID, body)
	tx := p.Transaction
	if tx.PaymentMethod != "CREDIT_CARD" || tx.CreditCardID == nil || *tx.CreditCardID != card.ID || tx.AccountID != "" ||
		tx.AmountCents != 120000 || tx.Merchant == nil || p.AllocationShifted {
		t.Fatalf("purchase = %+v %+v", tx, p)
	}
	want := []string{"2026-12-03", "2027-01-03", "2027-02-03", "2027-03-03", "2027-04-03", "2027-05-03"}
	if got := closings(p.Installments); !slices.Equal(got, want) {
		t.Fatalf("closings = %v, want %v", got, want)
	}
	for i, inst := range p.Installments {
		if inst.Number != i+1 || inst.AmountCents != 20000 {
			t.Fatalf("installment %d = %+v", i+1, inst)
		}
	}
	if p.Installments[0].DueOn != "2026-12-10" || p.Installments[5].DueOn != "2027-05-10" {
		t.Fatalf("due dates = %+v", p.Installments)
	}
	invoices := api.invoices(card.ID)
	if len(invoices) != 6 || invoices[0].Start != "2026-11-03" || invoices[0].Status != "OPEN" {
		t.Fatalf("invoices = %+v", invoices)
	}
	// Consumption once, by the purchase date; the commitment goes through the invoices; cash untouched.
	if c := api.consumption(h.ID); c != 120000 {
		t.Fatalf("consumption = %d", c)
	}
	if n := api.count("SELECT count(*) FROM obligations WHERE transaction_id = $1", tx.ID); n != 0 {
		t.Fatalf("card purchase created %d obligations", n)
	}
	if n := api.count("SELECT count(*) FROM account_movements WHERE household_id = $1", h.ID); n != movements {
		t.Fatalf("card purchase moved cash: %d movements", n-movements)
	}
	if b := owner.balance(h.ID, acc.ID); b != 100000 {
		t.Fatalf("balance = %d", b)
	}

	// T06 on the card: R$ 100,01 in 3x = 33,34 + 33,34 + 33,33, on the invoices that exist.
	p = owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 10001, 3, "2026-12-02"))
	if got := closings(p.Installments); !slices.Equal(got, want[:3]) {
		t.Fatalf("closings = %v", got)
	}
	if p.Installments[0].AmountCents != 3334 || p.Installments[1].AmountCents != 3334 || p.Installments[2].AmountCents != 3333 ||
		p.Installments[0].InvoiceID != invoices[0].ID {
		t.Fatalf("installments = %+v", p.Installments)
	}
	// T14: a purchase on the closing day goes to the next invoice; installments default to 1.
	onClosing := purchaseBody(cat.ID, card.ID, 5000, 1, "2026-12-03")
	delete(onClosing["payment"].(map[string]any), "installments")
	p = owner.purchase(h.ID, onClosing)
	if len(p.Installments) != 1 || p.Installments[0].ClosingOn != "2027-01-03" || p.Installments[0].InvoiceID != invoices[1].ID {
		t.Fatalf("closing-day purchase = %+v", p.Installments)
	}
	if n := len(api.invoices(card.ID)); n != 6 {
		t.Fatalf("invoices = %d, want 6 (reused)", n)
	}
	if got := api.invoiceTotal(invoices[0].ID); got != 20000+3334 {
		t.Fatalf("first invoice total = %d", got)
	}
	if got := api.invoiceTotal(invoices[1].ID); got != 20000+3334+5000 {
		t.Fatalf("second invoice total = %d", got)
	}

	// Read back, filter by card or method, and edit the classification (no obligation to sync).
	var got transactionDTO
	owner.expect(http.StatusOK, http.MethodGet, base+"/"+tx.ID, nil).decode(t, &got)
	if got.CreditCardID == nil || got.AccountID != "" {
		t.Fatalf("get = %+v", got)
	}
	for _, query := range []string{"?creditCardId=" + card.ID, "?paymentMethod=CREDIT_CARD"} {
		var page struct{ Items []transactionDTO }
		owner.expect(http.StatusOK, http.MethodGet, base+query, nil).decode(t, &page)
		if len(page.Items) != 3 {
			t.Fatalf("%s: %d items", query, len(page.Items))
		}
	}
	var none struct{ Items []transactionDTO }
	owner.expect(http.StatusOK, http.MethodGet, base+"?accountId="+acc.ID, nil).decode(t, &none)
	if len(none.Items) != 0 {
		t.Fatalf("account filter lists card purchases: %+v", none.Items)
	}
	owner.expect(http.StatusOK, http.MethodPatch, base+"/"+tx.ID, map[string]any{"description": "Geladeira", "notes": "10 anos"}).decode(t, &got)
	if got.Description != "Geladeira" {
		t.Fatalf("updated = %+v", got)
	}
	// Voiding a card purchase arrives with F4.6 (RN-019); nothing changes meanwhile.
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, base+"/"+tx.ID+"/void", nil,
		"Idempotency-Key", uuid.NewString()).errorCode(t); code != "CARD_PURCHASE_VOID_UNAVAILABLE" {
		t.Fatalf("got %s", code)
	}
	if c := api.consumption(h.ID); c != 120000+10001+5000 {
		t.Fatalf("consumption = %d", c)
	}

	// Audit: the purchase is recorded without amount, description or merchant (ADR 0012).
	if a := api.auditActions(tx.ID); !slices.Equal(a, []string{"transaction.created", "transaction.updated"}) {
		t.Fatalf("audit = %v", a)
	}
	var meta string
	if err := api.pool.QueryRow(context.Background(),
		"SELECT metadata::text FROM audit_events WHERE entity_id = $1 AND action = 'transaction.created'", tx.ID).Scan(&meta); err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"120000", "Compra", "Loja"} {
		if strings.Contains(meta, leak) {
			t.Fatalf("audit metadata leaks %q: %s", leak, meta)
		}
	}
	assertInvariants(t)
}

// The same Idempotency-Key replays the purchase instead of placing its installments twice.
func TestCardPurchaseIsIdempotent(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	key := uuid.NewString()
	path := "/api/v1/households/" + h.ID + "/transactions"
	body := purchaseBody(cat.ID, card.ID, 30000, 3, "2026-11-10")

	var first, second purchaseDTO
	owner.expect(http.StatusCreated, http.MethodPost, path, body, "Idempotency-Key", key).decode(t, &first)
	r := owner.expect(http.StatusCreated, http.MethodPost, path, body, "Idempotency-Key", key)
	r.decode(t, &second)
	if r.header.Get("Idempotent-Replayed") != "true" || second.Transaction.ID != first.Transaction.ID {
		t.Fatalf("replay = %+v (%s)", second, r.header.Get("Idempotent-Replayed"))
	}
	if n := api.count("SELECT count(*) FROM card_installments WHERE household_id = $1", h.ID); n != 3 {
		t.Fatalf("installments = %d, want 3", n)
	}
	assertInvariants(t)
}

// Card purchases are validated before anything is written (RN-010, I-48, RN-021).
func TestCardPurchaseValidation(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	income := owner.firstCategory(h.ID, "INCOME")
	acc := owner.createAccount(h.ID, nil)
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	archived := owner.createCard(h.ID, cardBody(h.MemberID, map[string]any{"name": "Velho"}))
	owner.expect(http.StatusOK, http.MethodPatch, "/api/v1/households/"+h.ID+"/cards/"+archived.ID, map[string]any{"isActive": false})
	path := "/api/v1/households/" + h.ID + "/transactions"

	with := func(edit func(body, payment map[string]any)) map[string]any {
		body := purchaseBody(cat.ID, card.ID, 30000, 3, "2026-11-10")
		edit(body, body["payment"].(map[string]any))
		return body
	}
	for _, tc := range []struct {
		name   string
		body   map[string]any
		status int
		code   string
		field  string
	}{
		{"income on the card", with(func(b, _ map[string]any) { b["kind"], b["categoryId"] = "INCOME", income.ID }), 400, "VALIDATION_ERROR", "payment.method"},
		{"no card", with(func(_, p map[string]any) { delete(p, "cardId") }), 400, "VALIDATION_ERROR", "payment.cardId"},
		{"card and account", with(func(_, p map[string]any) { p["accountId"] = acc.ID }), 400, "VALIDATION_ERROR", "payment.accountId"},
		{"due date", with(func(_, p map[string]any) { p["dueOn"] = "2026-12-10" }), 400, "VALIDATION_ERROR", "payment.dueOn"},
		{"planned", with(func(_, p map[string]any) { p["mode"] = "PLANNED" }), 400, "VALIDATION_ERROR", "payment.mode"},
		{"61 installments", with(func(_, p map[string]any) { p["installments"] = 61 }), 400, "INVALID_INSTALLMENTS", "payment.installments"},
		{"under one cent", with(func(b, p map[string]any) { b["amountCents"], p["installments"] = 5, 6 }), 400, "INSTALLMENT_TOO_SMALL", "payment.installments"},
		{"installments without card", map[string]any{
			"kind": "EXPENSE", "scope": "FAMILY", "description": "x", "amountCents": 100, "categoryId": cat.ID, "occurredOn": "2026-11-10",
			"payment": map[string]any{"method": "PIX", "accountId": acc.ID, "mode": "IMMEDIATE", "installments": 2},
		}, 400, "VALIDATION_ERROR", "payment.installments"},
		{"unknown card", with(func(_, p map[string]any) { p["cardId"] = uuid.NewString() }), 422, "CARD_NOT_FOUND", "payment.cardId"},
		{"archived card", with(func(_, p map[string]any) { p["cardId"] = archived.ID }), 422, "CARD_ARCHIVED", "payment.cardId"},
	} {
		r := owner.expect(tc.status, http.MethodPost, path, tc.body, "Idempotency-Key", uuid.NewString())
		if code := r.errorCode(t); code != tc.code || !slices.Contains(r.errorFields(t), tc.field) {
			t.Errorf("%s: %s", tc.name, r.body)
		}
	}
	if n := api.count("SELECT count(*) FROM transactions WHERE household_id = $1", h.ID); n != 0 {
		t.Fatalf("%d transactions written by rejected requests", n)
	}
	if n := api.count("SELECT count(*) FROM card_invoices WHERE household_id = $1", h.ID); n != 0 {
		t.Fatalf("%d invoices written by rejected requests", n)
	}
}

// A purchase far from the card's last invoice would write one invoice per empty cycle in
// between; past MaxNewInvoices it is rejected as a mistyped date (I-50).
func TestCardPurchaseTooFarFromTheLastInvoice(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 1000, 1, "2026-11-10"))
	// Five idle years later is still accepted (60 empty cycles + 60 installments = 120)...
	owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 6000, 60, "2031-11-10"))
	// ...but a year typo is not.
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/households/"+h.ID+"/transactions",
		purchaseBody(cat.ID, card.ID, 1000, 1, "2100-11-10"), "Idempotency-Key", uuid.NewString()).errorCode(t); code != "CARD_PURCHASE_TOO_FAR" {
		t.Fatalf("got %s", code)
	}
	assertInvariants(t)
}

// ADR 0003 / I-15: a late purchase whose cycle is already CLOSED goes to the next invoice not
// closed, the others follow, and the response says allocationShifted.
func TestRetroactivePurchaseSkipsAClosedInvoice(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 30000, 3, "2026-11-10"))
	invoices := api.invoices(card.ID)

	// Closing arrives with F4.4: freeze the first invoice directly, as the close route will.
	if _, err := api.pool.Exec(context.Background(), `
		UPDATE card_invoices SET status = 'CLOSED', closed_base_amount_cents = 10000, closed_at = now(),
		       closed_by_member_id = $2, updated_at = now()
		WHERE id = $1`, invoices[0].ID, h.MemberID); err != nil {
		t.Fatal(err)
	}
	p := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 4000, 2, "2026-11-20"))
	if !p.AllocationShifted {
		t.Fatal("allocationShifted = false")
	}
	if got := closings(p.Installments); !slices.Equal(got, []string{"2027-01-03", "2027-02-03"}) {
		t.Fatalf("closings = %v", got)
	}
	if got := api.invoiceTotal(invoices[0].ID); got != 10000 {
		t.Fatalf("closed invoice changed: %d", got)
	}
	// A purchase that does not touch the closed invoice is not shifted.
	if p := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 1000, 1, "2026-12-10")); p.AllocationShifted {
		t.Fatal("not shifted, but allocationShifted = true")
	}
	if a := api.auditActions(p.Transaction.ID); len(a) != 1 {
		t.Fatalf("audit = %v", a)
	}
	assertInvariants(t)
}

// ADR 0003: changing the card's days only affects invoices not created yet; the next one is
// chained on the last existing closing date, with the empty cycles in between (I-48).
func TestCardPurchaseAfterChangingTheClosingDay(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 1000, 1, "2026-11-10")) // invoice 11-03 → 12-03
	owner.expect(http.StatusOK, http.MethodPatch, "/api/v1/households/"+h.ID+"/cards/"+card.ID,
		map[string]any{"closingDay": 20, "dueDay": 28})

	p := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 1000, 1, "2027-01-10"))
	if got := closings(p.Installments); !slices.Equal(got, []string{"2027-01-20"}) {
		t.Fatalf("closings = %v", got)
	}
	var got []string
	for _, inv := range api.invoices(card.ID) {
		got = append(got, inv.Start+"→"+inv.Closing+"/"+inv.Due)
	}
	want := []string{"2026-11-03→2026-12-03/2026-12-10", "2026-12-03→2026-12-20/2026-12-28", "2026-12-20→2027-01-20/2027-01-28"}
	if !slices.Equal(got, want) {
		t.Fatalf("invoices = %v, want %v", got, want)
	}
	// The existing invoice keeps its dates: a purchase inside it still lands there.
	p = owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 1000, 1, "2026-11-25"))
	if p.Installments[0].ClosingOn != "2026-12-03" {
		t.Fatalf("closing = %s", p.Installments[0].ClosingOn)
	}
	assertInvariants(t)
}

// Concurrent purchases on one card create each invoice once (card lock, ADR 0007) and every
// installment lands (RN-020).
func TestConcurrentPurchasesOnOneCard(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))

	const n = 20
	var wg sync.WaitGroup
	statuses := make(chan int, n)
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r := owner.do(http.MethodPost, "/api/v1/households/"+h.ID+"/transactions",
				purchaseBody(cat.ID, card.ID, 3000, 3, "2026-11-10"), "Idempotency-Key", uuid.NewString())
			statuses <- r.status
		}()
	}
	close(start)
	wg.Wait()
	close(statuses)
	for s := range statuses {
		if s != http.StatusCreated {
			t.Fatalf("concurrent purchase failed with %d", s)
		}
	}
	invoices := api.invoices(card.ID)
	if len(invoices) != 3 {
		t.Fatalf("invoices = %d, want 3", len(invoices))
	}
	for _, inv := range invoices {
		if got := api.invoiceTotal(inv.ID); got != n*1000 {
			t.Fatalf("invoice %s total = %d", inv.Closing, got)
		}
	}
	assertInvariants(t)
}

// The database refuses installments that do not belong to the purchase's card, and card
// columns that contradict the method (I-07), even if a bug skipped the service checks.
func TestCardPurchaseConstraints(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	acc := owner.createAccount(h.ID, nil)
	card := owner.createCard(h.ID, cardBody(h.MemberID, nil))
	other := owner.createCard(h.ID, cardBody(h.MemberID, map[string]any{"name": "Outro"}))
	p := owner.purchase(h.ID, purchaseBody(cat.ID, card.ID, 3000, 1, "2026-11-10"))
	owner.purchase(h.ID, purchaseBody(cat.ID, other.ID, 3000, 1, "2026-11-10"))
	otherInvoice := api.invoices(other.ID)[0].ID
	ctx := context.Background()

	for name, sql := range map[string]string{
		"installment on another card's invoice": `INSERT INTO card_installments (id, household_id, transaction_id, credit_card_id,
			card_invoice_id, installment_number, installment_total, amount_cents)
			VALUES (gen_random_uuid(), $1, '` + p.Transaction.ID + `', '` + card.ID + `', '` + otherInvoice + `', 2, 2, 1)`,
		"installment claiming another card": `INSERT INTO card_installments (id, household_id, transaction_id, credit_card_id,
			card_invoice_id, installment_number, installment_total, amount_cents)
			VALUES (gen_random_uuid(), $1, '` + p.Transaction.ID + `', '` + other.ID + `', '` + otherInvoice + `', 2, 2, 1)`,
		"card purchase with an account": `UPDATE transactions SET account_id = '` + acc.ID + `' WHERE household_id = $1 AND id = '` + p.Transaction.ID + `'`,
		"card method without card": `UPDATE transactions SET credit_card_id = NULL, account_id = '` + acc.ID + `'
			WHERE household_id = $1 AND id = '` + p.Transaction.ID + `'`,
		"income on the card": `UPDATE transactions SET kind = 'INCOME' WHERE household_id = $1 AND id = '` + p.Transaction.ID + `'`,
		"invoice closing before it starts": `INSERT INTO card_invoices (id, household_id, credit_card_id, cycle_start_on, closing_on, due_on)
			VALUES (gen_random_uuid(), $1, '` + card.ID + `', '2030-01-10', '2030-01-10', '2030-01-20')`,
		"closed invoice without base": `UPDATE card_invoices SET status = 'CLOSED' WHERE household_id = $1`,
	} {
		if _, err := api.pool.Exec(ctx, sql, h.ID); err == nil {
			t.Errorf("%s: the database must reject it", name)
		}
	}
	assertInvariants(t)
}
