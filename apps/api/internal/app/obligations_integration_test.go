//go:build integration

package app

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
)

type obligationDTO struct {
	ID               string  `json:"id"`
	TransactionID    *string `json:"transactionId"`
	Kind             string  `json:"kind"`
	Source           string  `json:"source"`
	Status           string  `json:"status"`
	PaymentStatus    string  `json:"paymentStatus"`
	IsOverdue        bool    `json:"isOverdue"`
	AmountCents      int64   `json:"amountCents"`
	PaidCents        int64   `json:"paidCents"`
	OutstandingCents int64   `json:"outstandingCents"`
	DueOn            string  `json:"dueOn"`
	Description      string  `json:"description"`
	AccountID        *string `json:"accountId"`
	CompetenceOn     string  `json:"competenceOn"`
}

type obligationDetailDTO struct {
	Obligation obligationDTO `json:"obligation"`
	Payments   []struct {
		AmountCents int64  `json:"amountCents"`
		PaidOn      string `json:"paidOn"`
		AccountID   string `json:"accountId"`
	} `json:"payments"`
}

// forecastBody is a PLANNED family entry; accountID may be nil (chosen when paying).
func forecastBody(kind, categoryID string, accountID *string, cents int64, occurredOn, dueOn string) map[string]any {
	payment := map[string]any{"method": "BOLETO", "mode": "PLANNED", "dueOn": dueOn}
	if accountID != nil {
		payment["accountId"] = *accountID
	}
	return map[string]any{
		"kind": kind, "scope": "FAMILY", "description": "Previsto", "amountCents": cents,
		"categoryId": categoryID, "occurredOn": occurredOn, "payment": payment,
	}
}

// createForecast records a PLANNED entry and returns its obligation.
func (c *client) createForecast(householdID string, body map[string]any) obligationDTO {
	c.api.t.Helper()
	var e entryDTO
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/households/"+householdID+"/transactions", body,
		"Idempotency-Key", uuid.NewString()).decode(c.api.t, &e)
	if e.Transaction != nil {
		c.api.t.Fatal("a forecast must not create a transaction")
	}
	return e.Obligation
}

func (c *client) pay(householdID, obligationID string, body map[string]any) obligationDetailDTO {
	c.api.t.Helper()
	var d obligationDetailDTO
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/households/"+householdID+"/obligations/"+obligationID+"/payments", body,
		"Idempotency-Key", uuid.NewString()).decode(c.api.t, &d)
	return d
}

func (c *client) obligations(householdID, query string) []obligationDTO {
	c.api.t.Helper()
	var page struct{ Items []obligationDTO }
	c.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+householdID+"/obligations"+query, nil).decode(c.api.t, &page)
	return page.Items
}

func (api *testAPI) transactionsOf(obligationID string) int {
	api.t.Helper()
	var n int
	if err := api.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM transactions t JOIN obligations o ON o.transaction_id = t.id WHERE o.id = $1`, obligationID).Scan(&n); err != nil {
		api.t.Fatal(err)
	}
	return n
}

// RN-008 / I-08: a planned expense is a commitment, not consumption and not cash, until it
// is paid; the first (partial) payment confirms it with the whole amount, once.
func TestPlannedExpenseBecomesConsumptionOnFirstPayment(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 100000})
	cat := owner.firstCategory(h.ID, "EXPENSE")

	o := owner.createForecast(h.ID, forecastBody("EXPENSE", cat.ID, &acc.ID, 65000, "2026-12-01", "2026-12-10"))
	if o.Status != "PLANNED" || o.Source != "MANUAL_FORECAST" || o.Kind != "PAYABLE" || o.TransactionID != nil ||
		o.PaymentStatus != "UNPAID" || o.OutstandingCents != 65000 || o.DueOn != "2026-12-10" || o.CompetenceOn != "2026-12-01" {
		t.Fatalf("forecast = %+v", o)
	}
	if c := api.consumption(h.ID); c != 0 {
		t.Fatalf("a forecast is not consumption: %d", c)
	}
	if b := owner.balance(h.ID, acc.ID); b != 100000 {
		t.Fatalf("a forecast moves no cash: %d", b)
	}
	if open := owner.obligations(h.ID, ""); len(open) != 1 || open[0].ID != o.ID {
		t.Fatalf("open = %+v", open)
	}
	assertInvariants(t)

	// First payment (partial): confirms the forecast with the whole amount at the competence date.
	path := "/api/v1/households/" + h.ID + "/obligations/" + o.ID + "/payments"
	first := owner.expect(http.StatusCreated, http.MethodPost, path, map[string]any{"amountCents": 20000, "paidOn": "2026-12-08"},
		"Idempotency-Key", "first")
	var d obligationDetailDTO
	first.decode(t, &d)
	if d.Obligation.Status != "CONFIRMED" || d.Obligation.PaymentStatus != "PARTIALLY_PAID" || d.Obligation.PaidCents != 20000 ||
		d.Obligation.OutstandingCents != 45000 || d.Obligation.TransactionID == nil || len(d.Payments) != 1 ||
		d.Payments[0].AccountID != acc.ID {
		t.Fatalf("after first payment = %s", first.body)
	}
	var tx transactionDTO
	owner.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+h.ID+"/transactions/"+*d.Obligation.TransactionID, nil).decode(t, &tx)
	if tx.Kind != "EXPENSE" || tx.AmountCents != 65000 || tx.OccurredOn != "2026-12-01" || tx.PaymentMethod != "BOLETO" || tx.Description != "Previsto" {
		t.Fatalf("confirmed transaction = %+v", tx)
	}
	if c := api.consumption(h.ID); c != 65000 {
		t.Fatalf("consumption = %d, want the whole amount", c)
	}
	if b := owner.balance(h.ID, acc.ID); b != 80000 {
		t.Fatalf("balance = %d", b)
	}

	// Replay of the first payment: same response, nothing recorded again (T09).
	before := countRows(t, api)
	replay := owner.expect(http.StatusCreated, http.MethodPost, path, map[string]any{"amountCents": 20000, "paidOn": "2026-12-08"},
		"Idempotency-Key", "first")
	if replay.header.Get("Idempotent-Replayed") != "true" || string(replay.body) != string(first.body) {
		t.Fatalf("replay = %s", replay.body)
	}

	// Paying the rest settles it; consumption does not grow (one transaction per obligation).
	d = owner.pay(h.ID, o.ID, map[string]any{"amountCents": 45000, "paidOn": "2026-12-10"})
	if d.Obligation.PaymentStatus != "PAID" || d.Obligation.OutstandingCents != 0 || len(d.Payments) != 2 {
		t.Fatalf("after settling = %+v", d)
	}
	if c := api.consumption(h.ID); c != 65000 || api.transactionsOf(o.ID) != 1 {
		t.Fatalf("consumption = %d, transactions = %d", c, api.transactionsOf(o.ID))
	}
	if b := owner.balance(h.ID, acc.ID); b != 35000 {
		t.Fatalf("balance = %d", b)
	}
	after := countRows(t, api)
	if after["obligation_payments"] != before["obligation_payments"]+1 || after["transactions"] != before["transactions"] {
		t.Fatalf("rows: before %v after %v", before, after)
	}
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, path, map[string]any{"amountCents": 1, "paidOn": "2026-12-10"},
		"Idempotency-Key", uuid.NewString()).errorCode(t); code != "OBLIGATION_ALREADY_PAID" {
		t.Fatalf("got %s", code)
	}
	if open := owner.obligations(h.ID, ""); len(open) != 0 {
		t.Fatalf("a paid obligation is not open: %+v", open)
	}
	if all := owner.obligations(h.ID, "?view=all"); len(all) != 1 {
		t.Fatalf("view=all = %+v", all)
	}
	assertInvariants(t)
}

// T22: a planned income, then received: the forecast is replaced by the realised income,
// never duplicated. Without an account on the forecast, the payment must name one.
func TestPlannedIncomeReceived(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	acc := owner.createAccount(h.ID, nil)
	o := owner.createForecast(h.ID, forecastBody("INCOME", owner.firstCategory(h.ID, "INCOME").ID, nil, 500000, "2026-11-05", "2026-11-05"))
	if o.Kind != "RECEIVABLE" || o.AccountID != nil {
		t.Fatalf("forecast = %+v", o)
	}
	path := "/api/v1/households/" + h.ID + "/obligations/" + o.ID + "/payments"
	if code := owner.expect(http.StatusBadRequest, http.MethodPost, path, map[string]any{"amountCents": 500000, "paidOn": "2026-11-05"},
		"Idempotency-Key", uuid.NewString()).errorCode(t); code != "VALIDATION_ERROR" {
		t.Fatalf("got %s", code)
	}

	d := owner.pay(h.ID, o.ID, map[string]any{"amountCents": 500000, "paidOn": "2026-11-06", "accountId": acc.ID})
	if d.Obligation.Status != "CONFIRMED" || d.Obligation.PaymentStatus != "PAID" || api.transactionsOf(o.ID) != 1 {
		t.Fatalf("received = %+v", d)
	}
	var tx transactionDTO
	owner.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+h.ID+"/transactions/"+*d.Obligation.TransactionID, nil).decode(t, &tx)
	if tx.Kind != "INCOME" || tx.AccountID != acc.ID || tx.AmountCents != 500000 {
		t.Fatalf("income = %+v", tx)
	}
	if b := owner.balance(h.ID, acc.ID); b != 500000 {
		t.Fatalf("balance = %d", b)
	}
	if c := api.consumption(h.ID); c != 0 {
		t.Fatalf("income is not consumption: %d", c)
	}
	assertInvariants(t)
}

func TestObligationListingOverdueAndPagination(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	acc := owner.createAccount(h.ID, nil)
	exp, inc := owner.firstCategory(h.ID, "EXPENSE"), owner.firstCategory(h.ID, "INCOME")
	today := kernel.Today(api.clock.Now(), "America/Sao_Paulo")
	day := func(offset int) string { return kernel.FormatDate(today.AddDate(0, 0, offset)) }

	late := owner.createForecast(h.ID, forecastBody("EXPENSE", exp.ID, &acc.ID, 100, day(-10), day(-1)))
	dueToday := owner.createForecast(h.ID, forecastBody("EXPENSE", exp.ID, &acc.ID, 200, day(-10), day(0)))
	future := owner.createForecast(h.ID, forecastBody("INCOME", inc.ID, &acc.ID, 300, day(0), day(5)))
	paidLate := owner.createForecast(h.ID, forecastBody("EXPENSE", exp.ID, &acc.ID, 400, day(-20), day(-15)))
	owner.pay(h.ID, paidLate.ID, map[string]any{"amountCents": 400, "paidOn": day(-15)})
	partlyLate := owner.createForecast(h.ID, forecastBody("EXPENSE", exp.ID, &acc.ID, 500, day(-20), day(-3)))
	owner.pay(h.ID, partlyLate.ID, map[string]any{"amountCents": 100, "paidOn": day(-3)})

	ids := func(list []obligationDTO) []string {
		out := make([]string, 0, len(list))
		for _, o := range list {
			out = append(out, o.ID)
		}
		return out
	}
	equal := func(got []obligationDTO, want ...string) bool {
		g := ids(got)
		if len(g) != len(want) {
			return false
		}
		for i := range g {
			if g[i] != want[i] {
				return false
			}
		}
		return true
	}

	open := owner.obligations(h.ID, "")
	if !equal(open, partlyLate.ID, late.ID, dueToday.ID, future.ID) {
		t.Fatalf("open (by due date) = %v", ids(open))
	}
	for _, o := range open {
		if want := o.ID == late.ID || o.ID == partlyLate.ID; o.IsOverdue != want {
			t.Fatalf("%s isOverdue = %v (due %s, today %s)", o.ID, o.IsOverdue, o.DueOn, day(0))
		}
	}
	if got := owner.obligations(h.ID, "?view=overdue"); !equal(got, partlyLate.ID, late.ID) {
		t.Fatalf("overdue = %v", ids(got))
	}
	if got := owner.obligations(h.ID, "?view=all"); !equal(got, paidLate.ID, partlyLate.ID, late.ID, dueToday.ID, future.ID) {
		t.Fatalf("all = %v", ids(got))
	}
	if got := owner.obligations(h.ID, "?kind=RECEIVABLE"); !equal(got, future.ID) {
		t.Fatalf("receivable = %v", ids(got))
	}
	if got := owner.obligations(h.ID, "?view=all&from="+day(-3)+"&to="+day(0)); !equal(got, partlyLate.ID, late.ID, dueToday.ID) {
		t.Fatalf("period = %v", ids(got))
	}

	// Keyset pagination walks the same order without gaps or repeats.
	var walked []string
	cursor := ""
	for range 10 {
		var page struct {
			Items      []obligationDTO
			NextCursor *string
		}
		owner.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+h.ID+"/obligations?view=all&limit=2"+cursor, nil).decode(t, &page)
		walked = append(walked, ids(page.Items)...)
		if page.NextCursor == nil {
			break
		}
		cursor = "&cursor=" + *page.NextCursor
	}
	if len(walked) != 5 || walked[0] != paidLate.ID || walked[4] != future.ID {
		t.Fatalf("walked = %v", walked)
	}

	for _, q := range []string{"?view=late", "?kind=EXPENSE", "?from=2026-13-01", "?from=2026-11-10&to=2026-11-01", "?cursor=@@", "?limit=0"} {
		owner.expect(http.StatusBadRequest, http.MethodGet, "/api/v1/households/"+h.ID+"/obligations"+q, nil)
	}

	// Overdue follows the household's date, not UTC: move the clock past midnight in São Paulo.
	api.clock.Advance(24 * time.Hour)
	if got := owner.obligations(h.ID, "?view=overdue"); !equal(got, partlyLate.ID, late.ID, dueToday.ID) {
		t.Fatalf("overdue tomorrow = %v", ids(got))
	}
}

func TestPaymentAndForecastRules(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	acc := owner.createAccount(h.ID, nil)
	archived := owner.createAccount(h.ID, map[string]any{"name": "Arquivada"})
	owner.expect(http.StatusOK, http.MethodPatch, "/api/v1/households/"+h.ID+"/accounts/"+archived.ID, map[string]any{"isActive": false})
	o := owner.createForecast(h.ID, forecastBody("EXPENSE", owner.firstCategory(h.ID, "EXPENSE").ID, &acc.ID, 10000, "2026-11-01", "2026-11-10"))
	base := "/api/v1/households/" + h.ID + "/obligations/" + o.ID

	// A forecast can be edited while unpaid (I-16).
	var d obligationDetailDTO
	owner.expect(http.StatusOK, http.MethodPatch, base, map[string]any{"amountCents": 12000, "dueOn": "2026-11-15", "description": " Condomínio "}).decode(t, &d)
	if d.Obligation.AmountCents != 12000 || d.Obligation.DueOn != "2026-11-15" || d.Obligation.Description != "Condomínio" || d.Obligation.Status != "PLANNED" {
		t.Fatalf("edited = %+v", d.Obligation)
	}
	for _, body := range []map[string]any{{"amountCents": 0}, {"description": " "}, {"dueOn": "2026-02-30"}, {"status": "PAID"}} {
		owner.expect(http.StatusBadRequest, http.MethodPatch, base, body)
	}

	pay := func(status int, body map[string]any) string {
		return owner.expect(status, http.MethodPost, base+"/payments", body, "Idempotency-Key", uuid.NewString()).errorCode(t)
	}
	before := countRows(t, api)
	for _, tc := range []struct {
		body   map[string]any
		status int
		code   string
	}{
		{map[string]any{"amountCents": 12001, "paidOn": "2026-11-15"}, 422, "AMOUNT_EXCEEDS_OUTSTANDING"},
		{map[string]any{"amountCents": 0, "paidOn": "2026-11-15"}, 400, "VALIDATION_ERROR"},
		{map[string]any{"amountCents": 100, "paidOn": "15/11/2026"}, 400, "VALIDATION_ERROR"},
		{map[string]any{"amountCents": 100, "paidOn": "2026-11-15", "accountId": archived.ID}, 422, "ACCOUNT_ARCHIVED"},
		{map[string]any{"amountCents": 100, "paidOn": "2026-11-15", "accountId": uuid.NewString()}, 422, "ACCOUNT_NOT_FOUND"},
	} {
		if code := pay(tc.status, tc.body); code != tc.code {
			t.Fatalf("%v: got %s, want %s", tc.body, code, tc.code)
		}
	}
	if after := countRows(t, api); after["obligation_payments"] != before["obligation_payments"] || after["transactions"] != before["transactions"] {
		t.Fatalf("rejected payments wrote data: %v -> %v", before, after)
	}
	owner.expect(http.StatusNotFound, http.MethodPost, "/api/v1/households/"+h.ID+"/obligations/"+uuid.NewString()+"/payments",
		map[string]any{"amountCents": 1, "paidOn": "2026-11-15"}, "Idempotency-Key", uuid.NewString())

	// Once paid, it is history: no more edits (void + new entry, F3.3).
	owner.pay(h.ID, o.ID, map[string]any{"amountCents": 2000, "paidOn": "2026-11-15"})
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPatch, base, map[string]any{"amountCents": 5000}).errorCode(t); code != "OBLIGATION_NOT_PLANNED" {
		t.Fatalf("got %s", code)
	}
	// Immediate entries are confirmed from birth: not editable either.
	immediate := owner.createTransaction(h.ID, expenseBody(owner.firstCategory(h.ID, "EXPENSE").ID, acc.ID, 100))
	var oblID string
	if err := api.pool.QueryRow(context.Background(), "SELECT id FROM obligations WHERE transaction_id = $1", immediate.ID).Scan(&oblID); err != nil {
		t.Fatal(err)
	}
	owner.expect(http.StatusUnprocessableEntity, http.MethodPatch, "/api/v1/households/"+h.ID+"/obligations/"+oblID, map[string]any{"amountCents": 5})

	// Audit trail of the obligation, without amounts.
	rows, err := api.pool.Query(context.Background(),
		"SELECT action FROM audit_events WHERE entity_id = $1 ORDER BY created_at, id", o.ID)
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, a)
	}
	rows.Close()
	if len(actions) != 3 || actions[0] != "obligation.created" || actions[1] != "obligation.updated" || actions[2] != "obligation.paid" {
		t.Fatalf("audit = %v", actions)
	}
	assertInvariants(t)
}

// T17 for obligations: simultaneous payments of the whole amount (different keys) never pay
// more than the outstanding amount, and the forecast is confirmed exactly once.
func TestConcurrentPaymentsNeverExceedTheOutstanding(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	acc := owner.createAccount(h.ID, nil)
	o := owner.createForecast(h.ID, forecastBody("EXPENSE", owner.firstCategory(h.ID, "EXPENSE").ID, &acc.ID, 10000, "2026-11-01", "2026-11-10"))

	const n = 10
	var wg sync.WaitGroup
	results := make(chan response, n)
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- owner.do(http.MethodPost, "/api/v1/households/"+h.ID+"/obligations/"+o.ID+"/payments",
				map[string]any{"amountCents": 10000, "paidOn": "2026-11-10"}, "Idempotency-Key", uuid.NewString())
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	created := 0
	for r := range results {
		switch {
		case r.status == http.StatusCreated:
			created++
		case r.status == http.StatusUnprocessableEntity && r.errorCode(t) == "OBLIGATION_ALREADY_PAID":
		default:
			t.Fatalf("unexpected %d %s", r.status, r.body)
		}
	}
	if created != 1 {
		t.Fatalf("%d payments accepted, want exactly 1", created)
	}
	if b := owner.balance(h.ID, acc.ID); b != -10000 {
		t.Fatalf("balance = %d", b)
	}
	if api.transactionsOf(o.ID) != 1 || api.consumption(h.ID) != 10000 {
		t.Fatalf("transactions = %d, consumption = %d", api.transactionsOf(o.ID), api.consumption(h.ID))
	}
	assertInvariants(t)
}
