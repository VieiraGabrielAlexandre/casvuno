//go:build integration

package app

import (
	"context"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
)

func (c *client) voidTransaction(householdID, transactionID string) transactionDTO {
	c.api.t.Helper()
	var tx transactionDTO
	c.expect(http.StatusOK, http.MethodPost, "/api/v1/households/"+householdID+"/transactions/"+transactionID+"/void", nil,
		"Idempotency-Key", uuid.NewString()).decode(c.api.t, &tx)
	return tx
}

func (c *client) transactions(householdID string, query url.Values) ([]transactionDTO, *string) {
	c.api.t.Helper()
	var page struct {
		Items      []transactionDTO
		NextCursor *string
	}
	c.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+householdID+"/transactions?"+query.Encode(), nil).decode(c.api.t, &page)
	return page.Items, page.NextCursor
}

func (c *client) movements(householdID, accountID string) []movementDTO {
	c.api.t.Helper()
	var page struct{ Items []movementDTO }
	c.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+householdID+"/accounts/"+accountID+"/movements", nil).decode(c.api.t, &page)
	return page.Items
}

func (api *testAPI) obligationOf(transactionID string) obligationDTO {
	api.t.Helper()
	var o obligationDTO
	if err := api.pool.QueryRow(context.Background(),
		"SELECT id, status, description FROM obligations WHERE transaction_id = $1", transactionID).Scan(&o.ID, &o.Status, &o.Description); err != nil {
		api.t.Fatal(err)
	}
	return o
}

func (api *testAPI) reversalsOf(movementID string) int {
	api.t.Helper()
	var n int
	if err := api.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM account_movements WHERE reversal_of_id = $1", movementID).Scan(&n); err != nil {
		api.t.Fatal(err)
	}
	return n
}

func (api *testAPI) auditActions(entityID string) []string {
	api.t.Helper()
	rows, err := api.pool.Query(context.Background(), "SELECT action FROM audit_events WHERE entity_id = $1 ORDER BY created_at, id", entityID)
	if err != nil {
		api.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			api.t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

// RN-019 / I-18 / INV-19: voiding an immediate expense removes the consumption, cancels the
// obligation and gives the money back with a mirrored REVERSAL; the payment stays as history.
// Repeating the void (same or another key) changes nothing.
func TestVoidImmediateExpense(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 10000})
	cat := owner.firstCategory(h.ID, "EXPENSE")
	tx := owner.createTransaction(h.ID, entryBody("EXPENSE", cat.ID, acc.ID, 1250, "2026-11-03"))
	paid := api.paidMovementID(tx.ID)

	path := "/api/v1/households/" + h.ID + "/transactions/" + tx.ID + "/void"
	first := owner.expect(http.StatusOK, http.MethodPost, path, nil, "Idempotency-Key", "void-1")
	var voided transactionDTO
	first.decode(t, &voided)
	if voided.Status != "VOIDED" || voided.AmountCents != 1250 {
		t.Fatalf("voided = %+v", voided)
	}
	if c := api.consumption(h.ID); c != 0 {
		t.Fatalf("consumption after void = %d", c)
	}
	if b := owner.balance(h.ID, acc.ID); b != 10000 {
		t.Fatalf("balance after void = %d", b)
	}
	if o := api.obligationOf(tx.ID); o.Status != "CANCELLED" {
		t.Fatalf("obligation = %+v", o)
	}
	// The reversal mirrors the payment, dated today in the household (the payment is older).
	today := kernel.FormatDate(kernel.Today(api.clock.Now(), h.Timezone))
	mv := owner.movements(h.ID, acc.ID)
	if len(mv) != 2 {
		t.Fatalf("movements = %+v", mv)
	}
	i := slices.IndexFunc(mv, func(m movementDTO) bool { return m.Kind == "REVERSAL" })
	if i < 0 || mv[i].Direction != "IN" || mv[i].AmountCents != 1250 || mv[i].ReversalOf == nil || *mv[i].ReversalOf != paid || mv[i].PostedOn != today {
		t.Fatalf("reversal = %+v (today %s, paid %s)", mv, today, paid)
	}
	var payments int
	if err := api.pool.QueryRow(context.Background(), "SELECT count(*) FROM obligation_payments p JOIN obligations o ON o.id = p.obligation_id WHERE o.transaction_id = $1", tx.ID).Scan(&payments); err != nil {
		t.Fatal(err)
	}
	if payments != 1 {
		t.Fatalf("payments must stay as history: %d", payments)
	}

	// Same key: replayed bytes. Another key: 200, nothing new.
	again := owner.expect(http.StatusOK, http.MethodPost, path, nil, "Idempotency-Key", "void-1")
	if again.header.Get("Idempotent-Replayed") != "true" || string(again.body) != string(first.body) {
		t.Fatalf("replay = %s %v", again.body, again.header)
	}
	before := countRows(t, api)
	if v := owner.voidTransaction(h.ID, tx.ID); v.Status != "VOIDED" {
		t.Fatalf("second void = %+v", v)
	}
	if after := countRows(t, api); after["account_movements"] != before["account_movements"] || after["audit_events"] != before["audit_events"] {
		t.Fatalf("second void wrote data: %v -> %v", before, after)
	}
	if n := api.reversalsOf(paid); n != 1 {
		t.Fatalf("reversals = %d", n)
	}
	if a := api.auditActions(tx.ID); !slices.Equal(a, []string{"transaction.created", "transaction.voided"}) {
		t.Fatalf("audit = %v", a)
	}
	// A voided entry is history: no edits.
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPatch, "/api/v1/households/"+h.ID+"/transactions/"+tx.ID,
		map[string]any{"description": "x"}).errorCode(t); code != "TRANSACTION_VOIDED" {
		t.Fatalf("got %s", code)
	}
	owner.expect(http.StatusNotFound, http.MethodPost, "/api/v1/households/"+h.ID+"/transactions/"+uuid.NewString()+"/void", nil,
		"Idempotency-Key", uuid.NewString())
	assertInvariants(t)
}

// Voiding a confirmed forecast paid in parts from two accounts reverses every payment on its
// own account; the obligation leaves the commitments and accepts no more payments. Voiding an
// income takes the money back out. A payment dated in the future keeps its date on the reversal.
func TestVoidReversesEveryPayment(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	a := owner.createAccount(h.ID, map[string]any{"name": "A", "openingBalanceCents": 100000})
	b := owner.createAccount(h.ID, map[string]any{"name": "B", "openingBalanceCents": 100000})
	cat := owner.firstCategory(h.ID, "EXPENSE")

	o := owner.createForecast(h.ID, forecastBody("EXPENSE", cat.ID, &a.ID, 65000, "2026-12-01", "2026-12-10"))
	owner.pay(h.ID, o.ID, map[string]any{"amountCents": 20000, "paidOn": "2026-11-05"})
	d := owner.pay(h.ID, o.ID, map[string]any{"amountCents": 10000, "paidOn": "2099-01-02", "accountId": b.ID})
	var full obligationDTO
	owner.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+h.ID+"/obligations/"+o.ID, nil).decode(t, &struct {
		Obligation *obligationDTO `json:"obligation"`
	}{&full})
	if full.TransactionID == nil || len(d.Payments) != 2 {
		t.Fatalf("forecast not confirmed: %+v", full)
	}

	owner.voidTransaction(h.ID, *full.TransactionID)
	if ba, bb := owner.balance(h.ID, a.ID), owner.balance(h.ID, b.ID); ba != 100000 || bb != 100000 {
		t.Fatalf("balances after void = %d, %d", ba, bb)
	}
	if c := api.consumption(h.ID); c != 0 {
		t.Fatalf("consumption = %d", c)
	}
	var got obligationDetailDTO
	owner.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+h.ID+"/obligations/"+o.ID, nil).decode(t, &got)
	if got.Obligation.Status != "CANCELLED" || got.Obligation.OutstandingCents != 0 || got.Obligation.PaidCents != 30000 || len(got.Payments) != 2 {
		t.Fatalf("obligation after void = %+v", got)
	}
	if open := owner.obligations(h.ID, ""); len(open) != 0 {
		t.Fatalf("open after void = %+v", open)
	}
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/households/"+h.ID+"/obligations/"+o.ID+"/payments",
		map[string]any{"amountCents": 100, "paidOn": "2026-11-20"}, "Idempotency-Key", uuid.NewString()).errorCode(t); code != "OBLIGATION_CANCELLED" {
		t.Fatalf("got %s", code)
	}
	// The future payment's reversal is not dated before it.
	for _, m := range owner.movements(h.ID, b.ID) {
		if m.Kind == "REVERSAL" && (m.PostedOn != "2099-01-02" || m.Direction != "IN") {
			t.Fatalf("reversal on B = %+v", m)
		}
	}

	income := owner.createTransaction(h.ID, entryBody("INCOME", owner.firstCategory(h.ID, "INCOME").ID, a.ID, 5000, "2026-11-04"))
	owner.voidTransaction(h.ID, income.ID)
	if ba := owner.balance(h.ID, a.ID); ba != 100000 {
		t.Fatalf("balance after income void = %d", ba)
	}
	mv := owner.movements(h.ID, a.ID)
	if i := slices.IndexFunc(mv, func(m movementDTO) bool {
		return m.Kind == "REVERSAL" && m.ReversalOf != nil && *m.ReversalOf == api.paidMovementID(income.ID)
	}); i < 0 || mv[i].Direction != "OUT" {
		t.Fatalf("income reversal = %+v", mv)
	}
	assertInvariants(t)
}

// Money cannot flow back into an archived account: the void is rejected and nothing changes.
func TestVoidRejectsArchivedAccount(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	acc := owner.createAccount(h.ID, nil)
	owner.createTransaction(h.ID, entryBody("INCOME", owner.firstCategory(h.ID, "INCOME").ID, acc.ID, 500, "2026-11-02"))
	out := owner.createTransaction(h.ID, entryBody("EXPENSE", owner.firstCategory(h.ID, "EXPENSE").ID, acc.ID, 500, "2026-11-03"))
	owner.expect(http.StatusOK, http.MethodPatch, "/api/v1/households/"+h.ID+"/accounts/"+acc.ID, map[string]any{"isActive": false})

	before := countRows(t, api)
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/households/"+h.ID+"/transactions/"+out.ID+"/void", nil,
		"Idempotency-Key", uuid.NewString()).errorCode(t); code != "ACCOUNT_ARCHIVED" {
		t.Fatalf("got %s", code)
	}
	if after := countRows(t, api); !maps.Equal(before, after) {
		t.Fatalf("rejected void wrote data: %v -> %v", before, after)
	}
	// After restoring the account the void goes through.
	owner.expect(http.StatusOK, http.MethodPatch, "/api/v1/households/"+h.ID+"/accounts/"+acc.ID, map[string]any{"isActive": true})
	owner.voidTransaction(h.ID, out.ID)
	if b := owner.balance(h.ID, acc.ID); b != 500 {
		t.Fatalf("balance = %d", b)
	}
	assertInvariants(t)
}

// ADR 0007: simultaneous voids of one transaction (different keys) reverse it exactly once.
func TestConcurrentVoidsReverseOnce(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 10000})
	tx := owner.createTransaction(h.ID, expenseBody(owner.firstCategory(h.ID, "EXPENSE").ID, acc.ID, 3000))

	const n = 10
	var wg sync.WaitGroup
	statuses := make([]int, n)
	for i := range n {
		wg.Go(func() {
			r := owner.do(http.MethodPost, "/api/v1/households/"+h.ID+"/transactions/"+tx.ID+"/void", nil, "Idempotency-Key", uuid.NewString())
			statuses[i] = r.status
		})
	}
	wg.Wait()
	for i, s := range statuses {
		if s != http.StatusOK {
			t.Fatalf("void %d: status %d", i, s)
		}
	}
	if r := api.reversalsOf(api.paidMovementID(tx.ID)); r != 1 {
		t.Fatalf("reversals = %d", r)
	}
	if b := owner.balance(h.ID, acc.ID); b != 10000 {
		t.Fatalf("balance = %d", b)
	}
	assertInvariants(t)
}

// A forecast entered by mistake is cancelled while unpaid; once paid, it is undone by
// voiding its transaction.
func TestCancelForecast(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 10000})
	cat := owner.firstCategory(h.ID, "EXPENSE")
	o := owner.createForecast(h.ID, forecastBody("EXPENSE", cat.ID, &acc.ID, 7000, "2026-11-01", "2026-11-20"))
	base := "/api/v1/households/" + h.ID + "/obligations/" + o.ID

	var d obligationDetailDTO
	owner.expect(http.StatusOK, http.MethodPost, base+"/cancel", nil).decode(t, &d)
	if d.Obligation.Status != "CANCELLED" || d.Obligation.OutstandingCents != 0 || d.Obligation.TransactionID != nil {
		t.Fatalf("cancelled = %+v", d.Obligation)
	}
	if open := owner.obligations(h.ID, ""); len(open) != 0 {
		t.Fatalf("open = %+v", open)
	}
	if all := owner.obligations(h.ID, "?view=all"); len(all) != 1 || all[0].Status != "CANCELLED" {
		t.Fatalf("all = %+v", all)
	}
	// Cancelling again changes nothing; a cancelled forecast takes no payment or edit.
	before := countRows(t, api)
	owner.expect(http.StatusOK, http.MethodPost, base+"/cancel", nil)
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, base+"/payments",
		map[string]any{"amountCents": 100, "paidOn": "2026-11-20"}, "Idempotency-Key", uuid.NewString()).errorCode(t); code != "OBLIGATION_CANCELLED" {
		t.Fatalf("got %s", code)
	}
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPatch, base, map[string]any{"amountCents": 1}).errorCode(t); code != "OBLIGATION_NOT_PLANNED" {
		t.Fatalf("got %s", code)
	}
	if after := countRows(t, api); !maps.Equal(before, after) {
		t.Fatalf("no-op cancel and rejected calls wrote data: %v -> %v", before, after)
	}
	if a := api.auditActions(o.ID); !slices.Equal(a, []string{"obligation.created", "obligation.cancelled"}) {
		t.Fatalf("audit = %v", a)
	}
	if b := owner.balance(h.ID, acc.ID); b != 10000 {
		t.Fatalf("balance = %d", b)
	}

	paid := owner.createForecast(h.ID, forecastBody("EXPENSE", cat.ID, &acc.ID, 7000, "2026-11-01", "2026-11-20"))
	owner.pay(h.ID, paid.ID, map[string]any{"amountCents": 100, "paidOn": "2026-11-10"})
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/households/"+h.ID+"/obligations/"+paid.ID+"/cancel", nil).errorCode(t); code != "OBLIGATION_NOT_PLANNED" {
		t.Fatalf("got %s", code)
	}
	owner.expect(http.StatusNotFound, http.MethodPost, "/api/v1/households/"+h.ID+"/obligations/"+uuid.NewString()+"/cancel", nil)
	assertInvariants(t)
}

// I-18: only non-structural fields are editable; the obligation keeps the same classification.
func TestUpdateTransaction(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	member, _ := api.registerUser("member")
	var joined householdDTO
	member.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept", map[string]any{"token": owner.createInvite(h.ID).Token}).decode(t, &joined)
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 10000})
	cats := owner.categories(h.ID, "?kind=EXPENSE")
	cat, other := cats[0], cats[1]
	body := entryBody("EXPENSE", cat.ID, acc.ID, 1250, "2026-11-03")
	body["merchant"], body["notes"] = "Padaria", "pão"
	tx := owner.createTransaction(h.ID, body)
	path := "/api/v1/households/" + h.ID + "/transactions/" + tx.ID

	var got transactionDTO
	member.expect(http.StatusOK, http.MethodPatch, path, map[string]any{
		"description": " Café da manhã ", "merchant": "", "notes": "com leite", "categoryId": other.ID,
		"scope": "PERSONAL", "personMemberId": joined.MemberID,
	}).decode(t, &got)
	if got.Description != "Café da manhã" || got.Merchant != nil || got.Notes == nil || *got.Notes != "com leite" ||
		got.CategoryID != other.ID || got.Scope != "PERSONAL" || got.PersonMemberID == nil || *got.PersonMemberID != joined.MemberID ||
		got.AmountCents != 1250 || got.OccurredOn != "2026-11-03" || got.Status != "ACTIVE" {
		t.Fatalf("updated = %+v", got)
	}
	if o := api.obligationOf(tx.ID); o.Description != "Café da manhã" || o.Status != "CONFIRMED" {
		t.Fatalf("obligation = %+v", o)
	}
	var oblCategory, oblScope string
	if err := api.pool.QueryRow(context.Background(), "SELECT category_id, scope FROM obligations WHERE transaction_id = $1", tx.ID).Scan(&oblCategory, &oblScope); err != nil {
		t.Fatal(err)
	}
	if oblCategory != other.ID || oblScope != "PERSONAL" {
		t.Fatalf("obligation classification = %s %s", oblCategory, oblScope)
	}
	if b := owner.balance(h.ID, acc.ID); b != 10000-1250 {
		t.Fatalf("an edit moves no cash: %d", b)
	}

	// Back to family, clearing the person.
	owner.expect(http.StatusOK, http.MethodPatch, path, map[string]any{"scope": "FAMILY", "personMemberId": ""}).decode(t, &got)
	if got.Scope != "FAMILY" || got.PersonMemberID != nil {
		t.Fatalf("family = %+v", got)
	}
	// Nothing changes: no audit event.
	owner.expect(http.StatusOK, http.MethodPatch, path, map[string]any{"description": "Café da manhã"})
	if a := api.auditActions(tx.ID); !slices.Equal(a, []string{"transaction.created", "transaction.updated", "transaction.updated"}) {
		t.Fatalf("audit = %v", a)
	}

	archived := owner.createCategory(h.ID, map[string]any{"name": "Antiga", "kind": "EXPENSE"})
	owner.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/households/"+h.ID+"/categories/"+archived.ID, nil)
	before := countRows(t, api)
	for _, tc := range []struct {
		body   map[string]any
		status int
		code   string
	}{
		{map[string]any{"amountCents": 1}, 400, "VALIDATION_ERROR"},
		{map[string]any{"occurredOn": "2026-11-01"}, 400, "VALIDATION_ERROR"},
		{map[string]any{"description": " "}, 400, "VALIDATION_ERROR"},
		{map[string]any{"merchant": strings.Repeat("x", 121)}, 400, "VALIDATION_ERROR"},
		{map[string]any{"scope": "SPLIT"}, 400, "VALIDATION_ERROR"},
		{map[string]any{"scope": "PERSONAL"}, 400, "VALIDATION_ERROR"},
		{map[string]any{"personMemberId": "nope"}, 400, "VALIDATION_ERROR"},
		{map[string]any{"categoryId": "nope"}, 400, "VALIDATION_ERROR"},
		{map[string]any{"categoryId": owner.firstCategory(h.ID, "INCOME").ID}, 422, "CATEGORY_KIND_MISMATCH"},
		{map[string]any{"categoryId": archived.ID}, 422, "CATEGORY_ARCHIVED"},
		{map[string]any{"categoryId": uuid.NewString()}, 422, "CATEGORY_NOT_FOUND"},
		{map[string]any{"personMemberId": uuid.NewString()}, 422, "PERSON_NOT_MEMBER"},
	} {
		if code := owner.expect(tc.status, http.MethodPatch, path, tc.body).errorCode(t); code != tc.code {
			t.Fatalf("%v: got %s, want %s", tc.body, code, tc.code)
		}
	}
	if after := countRows(t, api); !maps.Equal(before, after) {
		t.Fatalf("rejected edits wrote data: %v -> %v", before, after)
	}
	owner.expect(http.StatusNotFound, http.MethodPatch, "/api/v1/households/"+h.ID+"/transactions/"+uuid.NewString(), map[string]any{"description": "x"})
	assertInvariants(t)
}

// RN-025: filters combine, pagination is stable (occurredOn desc, id desc) and voided
// entries are hidden unless asked for.
func TestTransactionListing(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	member, _ := api.registerUser("member")
	var joined householdDTO
	member.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept", map[string]any{"token": owner.createInvite(h.ID).Token}).decode(t, &joined)
	a := owner.createAccount(h.ID, map[string]any{"name": "A", "openingBalanceCents": 100000})
	b := owner.createAccount(h.ID, map[string]any{"name": "B", "openingBalanceCents": 100000})
	food := owner.firstCategory(h.ID, "EXPENSE")
	sub := owner.createCategory(h.ID, map[string]any{"name": "Padaria", "kind": "EXPENSE", "parentId": food.ID})
	otherCat := owner.categories(h.ID, "?kind=EXPENSE")[1]
	if otherCat.ID == food.ID || otherCat.ID == sub.ID {
		t.Fatalf("unexpected category order")
	}
	income := owner.firstCategory(h.ID, "INCOME")

	mk := func(kind, cat, acc string, cents int64, on string, extra map[string]any) transactionDTO {
		body := entryBody(kind, cat, acc, cents, on)
		for k, v := range extra {
			body[k] = v
		}
		return owner.createTransaction(h.ID, body)
	}
	t1 := mk("EXPENSE", food.ID, a.ID, 100, "2026-11-01", map[string]any{"description": "Mercado do mês"})
	t2 := mk("EXPENSE", sub.ID, a.ID, 200, "2026-11-02", map[string]any{"merchant": "PADARIA Central"})
	t3 := mk("EXPENSE", otherCat.ID, b.ID, 300, "2026-11-02", map[string]any{"scope": "PERSONAL", "personMemberId": joined.MemberID,
		"payment": map[string]any{"method": "CASH", "accountId": b.ID, "mode": "IMMEDIATE"}})
	t4 := mk("INCOME", income.ID, a.ID, 400, "2026-11-03", map[string]any{"notes": "bônus de natal"})
	t5 := mk("EXPENSE", food.ID, b.ID, 500, "2026-11-04", nil)
	owner.voidTransaction(h.ID, t5.ID)

	ids := func(list []transactionDTO) []string {
		out := make([]string, 0, len(list))
		for _, tx := range list {
			out = append(out, tx.ID)
		}
		return out
	}
	// Same date: newer ID first (UUIDv7), so t3 precedes t2.
	newestFirst := []string{t4.ID, t3.ID, t2.ID, t1.ID}
	for _, tc := range []struct {
		query url.Values
		want  []string
	}{
		{url.Values{}, newestFirst},
		{url.Values{"status": {"ALL"}}, append([]string{t5.ID}, newestFirst...)},
		{url.Values{"status": {"VOIDED"}}, []string{t5.ID}},
		{url.Values{"kind": {"INCOME"}}, []string{t4.ID}},
		{url.Values{"from": {"2026-11-02"}, "to": {"2026-11-02"}}, []string{t3.ID, t2.ID}},
		{url.Values{"categoryId": {food.ID}}, []string{t2.ID, t1.ID}}, // includes the subcategory
		{url.Values{"categoryId": {sub.ID}}, []string{t2.ID}},
		{url.Values{"accountId": {b.ID}}, []string{t3.ID}},
		{url.Values{"personMemberId": {joined.MemberID}}, []string{t3.ID}},
		{url.Values{"scope": {"FAMILY"}, "kind": {"EXPENSE"}}, []string{t2.ID, t1.ID}},
		{url.Values{"paymentMethod": {"CASH"}}, []string{t3.ID}},
		{url.Values{"q": {"padaria"}}, []string{t2.ID}},
		{url.Values{"q": {"MERCADO"}}, []string{t1.ID}},
		{url.Values{"q": {"Natal"}}, []string{t4.ID}},
		{url.Values{"q": {"100%"}}, nil},
		{url.Values{"q": {"   "}}, newestFirst},
	} {
		got, _ := owner.transactions(h.ID, tc.query)
		if !slices.Equal(ids(got), tc.want) {
			t.Fatalf("%v: got %v, want %v", tc.query, ids(got), tc.want)
		}
	}

	// Keyset pagination walks every row once, in order.
	var walked []string
	q := url.Values{"limit": {"2"}, "status": {"ALL"}}
	for range 5 {
		page, next := owner.transactions(h.ID, q)
		walked = append(walked, ids(page)...)
		if next == nil {
			break
		}
		q.Set("cursor", *next)
	}
	if want := append([]string{t5.ID}, newestFirst...); !slices.Equal(walked, want) {
		t.Fatalf("walked = %v, want %v", walked, want)
	}

	for _, query := range []string{"status=GONE", "kind=X", "scope=SPLIT", "paymentMethod=CREDIT_CARD", "categoryId=x",
		"from=2026-11-05&to=2026-11-01", "from=05/11/2026", "limit=0", "cursor=bad", "q=" + strings.Repeat("x", 101)} {
		owner.expect(http.StatusBadRequest, http.MethodGet, "/api/v1/households/"+h.ID+"/transactions?"+query, nil)
	}
	// Every member sees every entry (RN-002).
	if got, _ := member.transactions(h.ID, url.Values{}); len(got) != 4 {
		t.Fatalf("member sees %d", len(got))
	}
}

func (c *client) createCategory(householdID string, body map[string]any) categoryDTO {
	c.api.t.Helper()
	var cat categoryDTO
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/households/"+householdID+"/categories", body).decode(c.api.t, &cat)
	return cat
}
