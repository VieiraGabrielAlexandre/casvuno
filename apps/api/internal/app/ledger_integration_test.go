//go:build integration

package app

import (
	"context"
	"maps"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type transactionDTO struct {
	ID                string  `json:"id"`
	Kind              string  `json:"kind"`
	Scope             string  `json:"scope"`
	PersonMemberID    *string `json:"personMemberId"`
	CreatedByMemberID string  `json:"createdByMemberId"`
	Description       string  `json:"description"`
	Merchant          *string `json:"merchant"`
	Notes             *string `json:"notes"`
	AmountCents       int64   `json:"amountCents"`
	CategoryID        string  `json:"categoryId"`
	OccurredOn        string  `json:"occurredOn"`
	PaymentMethod     string  `json:"paymentMethod"`
	AccountID         string  `json:"accountId"`
	Status            string  `json:"status"`
}

// entryBody is an immediate family entry paid or received now by Pix.
func entryBody(kind, categoryID, accountID string, cents int64, occurredOn string) map[string]any {
	return map[string]any{
		"kind": kind, "scope": "FAMILY", "description": "Lançamento", "amountCents": cents,
		"categoryId": categoryID, "occurredOn": occurredOn,
		"payment": map[string]any{"method": "PIX", "accountId": accountID, "mode": "IMMEDIATE"},
	}
}

func expenseBody(categoryID, accountID string, cents int64) map[string]any {
	return entryBody("EXPENSE", categoryID, accountID, cents, "2026-11-10")
}

// entryDTO is the response of POST /transactions.
type entryDTO struct {
	Transaction *transactionDTO `json:"transaction"`
	Obligation  obligationDTO   `json:"obligation"`
}

// createTransaction records an immediate entry with a fresh Idempotency-Key and returns it.
func (c *client) createTransaction(householdID string, body map[string]any) transactionDTO {
	c.api.t.Helper()
	var e entryDTO
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/households/"+householdID+"/transactions", body,
		"Idempotency-Key", uuid.NewString()).decode(c.api.t, &e)
	if e.Transaction == nil {
		c.api.t.Fatal("an immediate entry must return its transaction")
	}
	return *e.Transaction
}

// consumption is the family consumption of the period (INV-10), computed the way the
// reports (F6) will: active expense transactions, never movements or obligations.
func (api *testAPI) consumption(householdID string) int64 {
	api.t.Helper()
	var total int64
	if err := api.pool.QueryRow(context.Background(), `
		SELECT COALESCE(sum(amount_cents), 0) FROM transactions
		WHERE household_id = $1 AND kind = 'EXPENSE' AND status = 'ACTIVE'`, householdID).Scan(&total); err != nil {
		api.t.Fatal(err)
	}
	return total
}

// T01: a R$ 12,50 coffee by Pix is one expense, one outgoing movement and -R$ 12,50.
func TestImmediateExpense(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	acc := owner.createAccount(h.ID, map[string]any{"name": "Corrente"})
	cat := owner.firstCategory(h.ID, "EXPENSE")
	path := "/api/v1/households/" + h.ID + "/transactions"
	body := map[string]any{
		"kind": "EXPENSE", "scope": "PERSONAL", "personMemberId": h.MemberID, "description": " Café ",
		"merchant": "Padaria", "notes": "  ", "amountCents": 1250, "categoryId": cat.ID, "occurredOn": "2026-11-10",
		"payment": map[string]any{"method": "PIX", "accountId": acc.ID, "mode": "IMMEDIATE"},
	}

	r := owner.expect(http.StatusCreated, http.MethodPost, path, body, "Idempotency-Key", "t01")
	var e entryDTO
	r.decode(t, &e)
	if e.Transaction == nil || e.Obligation.Status != "CONFIRMED" || e.Obligation.PaymentStatus != "PAID" ||
		e.Obligation.TransactionID == nil || *e.Obligation.TransactionID != e.Transaction.ID {
		t.Fatalf("entry = %s", r.body)
	}
	tx := *e.Transaction
	if tx.Kind != "EXPENSE" || tx.Scope != "PERSONAL" || tx.PersonMemberID == nil || *tx.PersonMemberID != h.MemberID ||
		tx.Description != "Café" || tx.Merchant == nil || *tx.Merchant != "Padaria" || tx.AmountCents != 1250 ||
		tx.OccurredOn != "2026-11-10" || tx.PaymentMethod != "PIX" || tx.AccountID != acc.ID || tx.Status != "ACTIVE" ||
		tx.CreatedByMemberID != h.MemberID {
		t.Fatalf("transaction = %s", r.body)
	}
	if !strings.Contains(string(r.body), `"notes":null`) {
		t.Fatalf("blank notes must be stored as null: %s", r.body)
	}
	var got transactionDTO
	owner.expect(http.StatusOK, http.MethodGet, path+"/"+tx.ID, nil).decode(t, &got)
	if !reflect.DeepEqual(got, tx) {
		t.Fatalf("GET = %+v, want %+v", got, tx)
	}

	if b := owner.balance(h.ID, acc.ID); b != -1250 {
		t.Fatalf("balance = %d, want -1250", b)
	}
	var page struct {
		Items []struct {
			Direction, Kind, Description string
			AmountCents                  int64
		}
	}
	owner.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+h.ID+"/accounts/"+acc.ID+"/movements", nil).decode(t, &page)
	if len(page.Items) != 1 || page.Items[0].Direction != "OUT" || page.Items[0].Kind != "DIRECT_PAYMENT" ||
		page.Items[0].AmountCents != 1250 || page.Items[0].Description != "Café" {
		t.Fatalf("statement = %+v", page.Items)
	}
	if c := api.consumption(h.ID); c != 1250 {
		t.Fatalf("consumption = %d", c)
	}

	// One confirmed DIRECT obligation, fully paid by one payment (ADR 0002 §3).
	var status, source, kind string
	var paid int64
	if err := api.pool.QueryRow(context.Background(), `
		SELECT o.status, o.source, o.kind, (SELECT sum(amount_cents) FROM obligation_payments p WHERE p.obligation_id = o.id)
		FROM obligations o WHERE o.transaction_id = $1`, tx.ID).Scan(&status, &source, &kind, &paid); err != nil {
		t.Fatal(err)
	}
	if status != "CONFIRMED" || source != "DIRECT" || kind != "PAYABLE" || paid != 1250 {
		t.Fatalf("obligation = %s %s %s paid %d", status, source, kind, paid)
	}
	var meta string
	if err := api.pool.QueryRow(context.Background(),
		"SELECT metadata::text FROM audit_events WHERE entity_id = $1 AND action = 'transaction.created'", tx.ID).Scan(&meta); err != nil {
		t.Fatalf("audit event: %v", err)
	}
	if strings.Contains(meta, "1250") || strings.Contains(meta, "Café") {
		t.Fatalf("audit metadata must not carry amounts or descriptions: %s", meta)
	}

	// T09/T10: a retry replays without recording twice; the same key with another body is 409.
	before := countRows(t, api)
	replay := owner.expect(http.StatusCreated, http.MethodPost, path, body, "Idempotency-Key", "t01")
	if replay.header.Get("Idempotent-Replayed") != "true" || string(replay.body) != string(r.body) {
		t.Fatalf("replay = %s %v", replay.body, replay.header)
	}
	body["amountCents"] = 1300
	if code := owner.expect(http.StatusConflict, http.MethodPost, path, body, "Idempotency-Key", "t01").errorCode(t); code != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("got %s", code)
	}
	if after := countRows(t, api); !maps.Equal(before, after) {
		t.Fatalf("retries wrote data: before %v after %v", before, after)
	}
	assertInvariants(t)
}

// Income received now: one transaction, a RECEIVABLE obligation and an incoming movement.
func TestImmediateIncome(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 1000})

	body := entryBody("INCOME", owner.firstCategory(h.ID, "INCOME").ID, acc.ID, 500000, "2026-11-05")
	body["payment"] = map[string]any{"method": "BANK_TRANSFER", "accountId": acc.ID, "mode": "IMMEDIATE"}
	tx := owner.createTransaction(h.ID, body)
	if tx.Kind != "INCOME" || tx.PaymentMethod != "BANK_TRANSFER" {
		t.Fatalf("transaction = %+v", tx)
	}
	if b := owner.balance(h.ID, acc.ID); b != 501000 {
		t.Fatalf("balance = %d", b)
	}
	var direction, kind, obligation string
	if err := api.pool.QueryRow(context.Background(), `
		SELECT m.direction, m.kind, o.kind FROM obligations o
		JOIN obligation_payments p ON p.obligation_id = o.id
		JOIN account_movements m ON m.id = p.account_movement_id
		WHERE o.transaction_id = $1`, tx.ID).Scan(&direction, &kind, &obligation); err != nil {
		t.Fatal(err)
	}
	if direction != "IN" || kind != "DIRECT_RECEIPT" || obligation != "RECEIVABLE" {
		t.Fatalf("got %s %s %s", direction, kind, obligation)
	}
	if c := api.consumption(h.ID); c != 0 {
		t.Fatalf("income is not consumption: %d", c)
	}
	assertInvariants(t)
}

// T02 and T03: the whole amount is family consumption once, whoever paid, consumed or owns
// the account. There is no split between members (RN-003); every member sees every entry.
func TestEntriesAreNeverSplitBetweenMembers(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	member, _ := api.registerUser("member")
	var joined householdDTO
	member.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept", map[string]any{"token": owner.createInvite(h.ID).Token}).decode(t, &joined)
	ownersAccount := owner.createAccount(h.ID, map[string]any{"name": "Da Ana", "ownership": "INDIVIDUAL", "ownerMemberId": h.MemberID})
	cat := owner.firstCategory(h.ID, "EXPENSE")

	// T02: the member's personal lunch, paid from the owner's individual account.
	lunch := expenseBody(cat.ID, ownersAccount.ID, 8000)
	lunch["scope"], lunch["personMemberId"], lunch["description"] = "PERSONAL", joined.MemberID, "Almoço"
	l := member.createTransaction(h.ID, lunch)
	// T03: family groceries entered by the owner.
	g := owner.createTransaction(h.ID, expenseBody(cat.ID, ownersAccount.ID, 30000))

	if l.AmountCents != 8000 || g.AmountCents != 30000 {
		t.Fatalf("amounts must be stored whole: %d %d", l.AmountCents, g.AmountCents)
	}
	if c := api.consumption(h.ID); c != 38000 {
		t.Fatalf("family consumption = %d, want 38000", c)
	}
	// RN-002: PERSONAL is a filter, not an access rule.
	owner.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+h.ID+"/transactions/"+l.ID, nil)
	member.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+h.ID+"/transactions/"+g.ID, nil)
	if b := owner.balance(h.ID, ownersAccount.ID); b != -38000 {
		t.Fatalf("balance = %d", b)
	}
	assertInvariants(t)
}

func TestTransactionValidation(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	acc := owner.createAccount(h.ID, nil)
	archivedAcc := owner.createAccount(h.ID, map[string]any{"name": "Arquivada"})
	owner.expect(http.StatusOK, http.MethodPatch, "/api/v1/households/"+h.ID+"/accounts/"+archivedAcc.ID, map[string]any{"isActive": false})
	expense, income := owner.firstCategory(h.ID, "EXPENSE"), owner.firstCategory(h.ID, "INCOME")
	archivedCat := owner.firstCategory(h.ID, "EXPENSE")
	for _, c := range owner.categories(h.ID, "") {
		if c.Kind == "EXPENSE" && c.ID != expense.ID && c.ParentID == nil {
			archivedCat = c
			break
		}
	}
	owner.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/households/"+h.ID+"/categories/"+archivedCat.ID, nil)
	path := "/api/v1/households/" + h.ID + "/transactions"

	with := func(changes map[string]any) map[string]any {
		b := expenseBody(expense.ID, acc.ID, 1000)
		for k, v := range changes {
			b[k] = v
		}
		return b
	}
	payment := func(method, accountID, mode string) map[string]any {
		p := map[string]any{"method": method, "mode": mode}
		if accountID != "" {
			p["accountId"] = accountID
		}
		return p
	}
	before := countRows(t, api)
	for _, tc := range []struct {
		name   string
		body   map[string]any
		status int
		code   string
		field  string
	}{
		{"income category on an expense", with(map[string]any{"categoryId": income.ID}), 422, "CATEGORY_KIND_MISMATCH", "categoryId"},
		{"expense category on an income", with(map[string]any{"kind": "INCOME"}), 422, "CATEGORY_KIND_MISMATCH", "categoryId"},
		{"archived category", with(map[string]any{"categoryId": archivedCat.ID}), 422, "CATEGORY_ARCHIVED", "categoryId"},
		{"unknown category", with(map[string]any{"categoryId": uuid.NewString()}), 422, "CATEGORY_NOT_FOUND", "categoryId"},
		{"archived account", with(map[string]any{"payment": payment("PIX", archivedAcc.ID, "IMMEDIATE")}), 422, "ACCOUNT_ARCHIVED", "payment.accountId"},
		{"unknown account", with(map[string]any{"payment": payment("PIX", uuid.NewString(), "IMMEDIATE")}), 422, "ACCOUNT_NOT_FOUND", "payment.accountId"},
		{"missing account", with(map[string]any{"payment": payment("PIX", "", "IMMEDIATE")}), 400, "VALIDATION_ERROR", "payment.accountId"},
		{"credit card not yet supported", with(map[string]any{"payment": payment("CREDIT_CARD", acc.ID, "IMMEDIATE")}), 400, "VALIDATION_ERROR", "payment.method"},
		{"unknown mode", with(map[string]any{"payment": payment("PIX", acc.ID, "LATER")}), 400, "VALIDATION_ERROR", "payment.mode"},
		{"planned without due date", with(map[string]any{"payment": payment("PIX", acc.ID, "PLANNED")}), 400, "VALIDATION_ERROR", "payment.dueOn"},
		{"immediate with due date", with(map[string]any{"payment": map[string]any{"method": "PIX", "accountId": acc.ID, "mode": "IMMEDIATE", "dueOn": "2026-11-20"}}), 400, "VALIDATION_ERROR", "payment.dueOn"},
		{"planned with merchant", with(map[string]any{"merchant": "Feira", "payment": map[string]any{"method": "PIX", "mode": "PLANNED", "dueOn": "2026-11-20"}}), 400, "VALIDATION_ERROR", "merchant"},
		{"planned on an archived account", with(map[string]any{"payment": map[string]any{"method": "PIX", "accountId": archivedAcc.ID, "mode": "PLANNED", "dueOn": "2026-11-20"}}), 422, "ACCOUNT_ARCHIVED", "payment.accountId"},
		{"personal without person", with(map[string]any{"scope": "PERSONAL"}), 400, "VALIDATION_ERROR", "personMemberId"},
		{"unknown person", with(map[string]any{"personMemberId": uuid.NewString()}), 422, "PERSON_NOT_MEMBER", "personMemberId"},
		{"invalid scope", with(map[string]any{"scope": "SHARED"}), 400, "VALIDATION_ERROR", "scope"},
		{"zero amount", with(map[string]any{"amountCents": 0}), 400, "VALIDATION_ERROR", "amountCents"},
		{"negative amount", with(map[string]any{"amountCents": -100}), 400, "VALIDATION_ERROR", "amountCents"},
		{"fractional amount", with(map[string]any{"amountCents": 10.5}), 400, "VALIDATION_ERROR", "amountCents"},
		{"amount over the limit", with(map[string]any{"amountCents": 1000000000000001}), 400, "VALIDATION_ERROR", "amountCents"},
		{"blank description", with(map[string]any{"description": "   "}), 400, "VALIDATION_ERROR", "description"},
		{"long description", with(map[string]any{"description": strings.Repeat("é", 201)}), 400, "VALIDATION_ERROR", "description"},
		{"invalid date", with(map[string]any{"occurredOn": "2026-02-30"}), 400, "VALIDATION_ERROR", "occurredOn"},
		{"split field", with(map[string]any{"shares": []any{}}), 400, "VALIDATION_ERROR", "shares"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := owner.expect(tc.status, http.MethodPost, path, tc.body, "Idempotency-Key", uuid.NewString())
			if code := r.errorCode(t); code != tc.code {
				t.Fatalf("got %s, want %s (%s)", code, tc.code, r.body)
			}
			if !strings.Contains(string(r.body), `"field":"`+tc.field+`"`) {
				t.Fatalf("error must point at %s: %s", tc.field, r.body)
			}
		})
	}
	if code := owner.expect(http.StatusBadRequest, http.MethodPost, path, with(nil)).errorCode(t); code != "IDEMPOTENCY_KEY_REQUIRED" {
		t.Fatalf("got %s", code)
	}
	// Rejected entries write nothing: no transaction, obligation, payment or movement.
	if after := countRows(t, api); !maps.Equal(before, after) {
		t.Fatalf("rejected requests wrote data: before %v after %v", before, after)
	}
	owner.expect(http.StatusNotFound, http.MethodGet, path+"/"+uuid.NewString(), nil)
	owner.expect(http.StatusNotFound, http.MethodGet, path+"/not-a-uuid", nil)
}

// The database enforces what the service checks, so a bug cannot corrupt the ledger.
func TestLedgerConstraints(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	acc := owner.createAccount(h.ID, nil)
	tx := owner.createTransaction(h.ID, expenseBody(owner.firstCategory(h.ID, "EXPENSE").ID, acc.ID, 1000))
	ctx := context.Background()

	for name, sql := range map[string]string{
		"payment update": "UPDATE obligation_payments SET amount_cents = 1 WHERE household_id = $1",
		"payment delete": "DELETE FROM obligation_payments WHERE household_id = $1",
		"income category on an expense": `
			UPDATE transactions SET category_id = (SELECT id FROM categories WHERE household_id = $1 AND kind = 'INCOME' LIMIT 1)
			WHERE household_id = $1`,
		"confirmed obligation without transaction": "UPDATE obligations SET transaction_id = NULL WHERE household_id = $1",
		"personal without person":                  "UPDATE transactions SET scope = 'PERSONAL', person_member_id = NULL WHERE household_id = $1",
		"transaction delete":                       "DELETE FROM transactions WHERE household_id = $1",
	} {
		if _, err := api.pool.Exec(ctx, sql, h.ID); err == nil {
			t.Errorf("%s: the database must reject it", name)
		}
	}
	var n int
	if err := api.pool.QueryRow(ctx, "SELECT count(*) FROM transactions WHERE id = $1", tx.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("transaction lost: %d %v", n, err)
	}
	assertInvariants(t)
}

// Concurrent entries on the same account are all recorded and the balance is exact (RN-020).
func TestConcurrentEntriesOnOneAccount(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 100000})
	cat := owner.firstCategory(h.ID, "EXPENSE")

	const n = 20
	var wg sync.WaitGroup
	statuses := make(chan int, n)
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r := owner.do(http.MethodPost, "/api/v1/households/"+h.ID+"/transactions", expenseBody(cat.ID, acc.ID, 1234),
				"Idempotency-Key", uuid.NewString())
			statuses <- r.status
		}()
	}
	close(start)
	wg.Wait()
	close(statuses)
	for s := range statuses {
		if s != http.StatusCreated {
			t.Fatalf("concurrent entry failed with %d", s)
		}
	}
	if b := owner.balance(h.ID, acc.ID); b != 100000-n*1234 {
		t.Fatalf("balance = %d, want %d", b, 100000-n*1234)
	}
	if c := api.consumption(h.ID); c != n*1234 {
		t.Fatalf("consumption = %d", c)
	}
	assertInvariants(t)
}
