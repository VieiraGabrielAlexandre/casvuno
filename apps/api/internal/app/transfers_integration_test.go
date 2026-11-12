//go:build integration

package app

import (
	"maps"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
)

type transferDTO struct {
	ID              string `json:"id"`
	FromAccountID   string `json:"fromAccountId"`
	FromAccountName string `json:"fromAccountName"`
	ToAccountID     string `json:"toAccountId"`
	AmountCents     int64  `json:"amountCents"`
	PostedOn        string `json:"postedOn"`
	Description     string `json:"description"`
}

// transfer creates a transfer with a fresh Idempotency-Key and returns it.
func (c *client) transfer(householdID, from, to string, cents int64, postedOn string) transferDTO {
	c.api.t.Helper()
	var t transferDTO
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/households/"+householdID+"/transfers",
		map[string]any{"fromAccountId": from, "toAccountId": to, "amountCents": cents, "postedOn": postedOn},
		"Idempotency-Key", uuid.NewString()).decode(c.api.t, &t)
	return t
}

func (c *client) balance(householdID, accountID string) int64 {
	c.api.t.Helper()
	var a accountDTO
	c.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+householdID+"/accounts/"+accountID, nil).decode(c.api.t, &a)
	return a.BalanceCents
}

// T04: a R$ 500 transfer changes both balances and leaves the family cash unchanged.
func TestTransferMovesMoneyWithoutChangingTheTotal(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	checking := owner.createAccount(h.ID, map[string]any{"name": "Corrente", "openingBalanceCents": 200000})
	savings := owner.createAccount(h.ID, map[string]any{"name": "Poupança", "type": "SAVINGS", "openingBalanceCents": 100000})
	totalBefore := owner.accounts(h.ID, "").TotalBalanceCents

	r := owner.expect(http.StatusCreated, http.MethodPost, "/api/v1/households/"+h.ID+"/transfers",
		map[string]any{"fromAccountId": checking.ID, "toAccountId": savings.ID, "amountCents": 50000, "postedOn": "2026-11-10", "description": " Reserva "},
		"Idempotency-Key", "t04")
	var tr transferDTO
	r.decode(t, &tr)
	if tr.FromAccountName != "Corrente" || tr.AmountCents != 50000 || tr.Description != "Reserva" || tr.PostedOn != "2026-11-10" {
		t.Fatalf("transfer = %+v", tr)
	}

	if b := owner.balance(h.ID, checking.ID); b != 150000 {
		t.Fatalf("checking = %d, want 150000", b)
	}
	if b := owner.balance(h.ID, savings.ID); b != 150000 {
		t.Fatalf("savings = %d, want 150000", b)
	}
	if total := owner.accounts(h.ID, "").TotalBalanceCents; total != totalBefore {
		t.Fatalf("family cash changed: %d -> %d", totalBefore, total)
	}

	// Each statement shows its leg, linked to the transfer.
	for acc, dir := range map[string]string{checking.ID: "OUT", savings.ID: "IN"} {
		var page struct {
			Items []struct{ Direction, Kind, TransferID string }
		}
		owner.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+h.ID+"/accounts/"+acc+"/movements", nil).decode(t, &page)
		if len(page.Items) != 1 || page.Items[0].Direction != dir || page.Items[0].Kind != "TRANSFER" || page.Items[0].TransferID != tr.ID {
			t.Fatalf("statement of %s = %+v", acc, page.Items)
		}
	}

	// T09 at the API: a retry with the same key replays and does not move money again.
	replay := owner.expect(http.StatusCreated, http.MethodPost, "/api/v1/households/"+h.ID+"/transfers",
		map[string]any{"fromAccountId": checking.ID, "toAccountId": savings.ID, "amountCents": 50000, "postedOn": "2026-11-10", "description": " Reserva "},
		"Idempotency-Key", "t04")
	if replay.header.Get("Idempotent-Replayed") != "true" || string(replay.body) != string(r.body) {
		t.Fatalf("replay = %s %v", replay.body, replay.header)
	}
	// T10 at the API: same key, different amount.
	if code := owner.expect(http.StatusConflict, http.MethodPost, "/api/v1/households/"+h.ID+"/transfers",
		map[string]any{"fromAccountId": checking.ID, "toAccountId": savings.ID, "amountCents": 1, "postedOn": "2026-11-10"},
		"Idempotency-Key", "t04").errorCode(t); code != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("got %s", code)
	}
	if b := owner.balance(h.ID, checking.ID); b != 150000 {
		t.Fatalf("retries moved money: checking = %d", b)
	}
	assertInvariants(t)
}

func TestTransferValidation(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	a := owner.createAccount(h.ID, map[string]any{"name": "A", "openingBalanceCents": 1000})
	b := owner.createAccount(h.ID, map[string]any{"name": "B"})
	archived := owner.createAccount(h.ID, map[string]any{"name": "Arquivada"})
	owner.expect(http.StatusOK, http.MethodPatch, "/api/v1/households/"+h.ID+"/accounts/"+archived.ID, map[string]any{"isActive": false})
	path := "/api/v1/households/" + h.ID + "/transfers"

	body := func(from, to string, cents any, date string) map[string]any {
		return map[string]any{"fromAccountId": from, "toAccountId": to, "amountCents": cents, "postedOn": date}
	}
	for _, tc := range []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"same account", body(a.ID, a.ID, 100, "2026-11-10"), 422, "SAME_ACCOUNT"},
		{"archived destination", body(a.ID, archived.ID, 100, "2026-11-10"), 422, "ACCOUNT_ARCHIVED"},
		{"archived origin", body(archived.ID, b.ID, 100, "2026-11-10"), 422, "ACCOUNT_ARCHIVED"},
		{"unknown account", body(a.ID, uuid.NewString(), 100, "2026-11-10"), 422, "ACCOUNT_NOT_FOUND"},
		{"zero amount", body(a.ID, b.ID, 0, "2026-11-10"), 400, "VALIDATION_ERROR"},
		{"negative amount", body(a.ID, b.ID, -5, "2026-11-10"), 400, "VALIDATION_ERROR"},
		{"fractional amount", body(a.ID, b.ID, 10.5, "2026-11-10"), 400, "VALIDATION_ERROR"},
		{"invalid date", body(a.ID, b.ID, 100, "2026-02-30"), 400, "VALIDATION_ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := owner.expect(tc.status, http.MethodPost, path, tc.body, "Idempotency-Key", uuid.NewString()).
				errorCode(t); code != tc.code {
				t.Fatalf("got %s, want %s", code, tc.code)
			}
		})
	}
	if code := owner.expect(http.StatusBadRequest, http.MethodPost, path, body(a.ID, b.ID, 100, "2026-11-10")).
		errorCode(t); code != "IDEMPOTENCY_KEY_REQUIRED" {
		t.Fatalf("got %s", code)
	}

	// Overdraft is allowed: Casvuno records what happened, it does not authorise payments.
	owner.transfer(h.ID, b.ID, a.ID, 5000, "2026-11-10")
	if bal := owner.balance(h.ID, b.ID); bal != -5000 {
		t.Fatalf("balance = %d, want -5000", bal)
	}
	if bal := owner.balance(h.ID, a.ID); bal != 6000 {
		t.Fatalf("balance = %d, want 6000", bal)
	}
}

func TestTransferListing(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	a := owner.createAccount(h.ID, map[string]any{"name": "A"})
	b := owner.createAccount(h.ID, map[string]any{"name": "B"})
	c := owner.createAccount(h.ID, map[string]any{"name": "C"})
	owner.transfer(h.ID, a.ID, b.ID, 100, "2026-11-01")
	owner.transfer(h.ID, b.ID, c.ID, 200, "2026-11-03")
	owner.transfer(h.ID, c.ID, a.ID, 300, "2026-11-03")
	base := "/api/v1/households/" + h.ID + "/transfers"

	var first struct {
		Items      []transferDTO
		NextCursor *string
	}
	owner.expect(http.StatusOK, http.MethodGet, base+"?limit=2", nil).decode(t, &first)
	if len(first.Items) != 2 || first.Items[0].AmountCents != 300 || first.Items[1].AmountCents != 200 || first.NextCursor == nil {
		t.Fatalf("first page = %+v", first)
	}
	var second struct {
		Items      []transferDTO
		NextCursor *string
	}
	owner.expect(http.StatusOK, http.MethodGet, base+"?limit=2&cursor="+*first.NextCursor, nil).decode(t, &second)
	if len(second.Items) != 1 || second.Items[0].AmountCents != 100 || second.NextCursor != nil {
		t.Fatalf("second page = %+v", second)
	}

	var forB struct{ Items []transferDTO }
	owner.expect(http.StatusOK, http.MethodGet, base+"?accountId="+b.ID, nil).decode(t, &forB)
	if len(forB.Items) != 2 {
		t.Fatalf("account filter (either side) = %+v", forB.Items)
	}
	var onThird struct{ Items []transferDTO }
	owner.expect(http.StatusOK, http.MethodGet, base+"?from=2026-11-03&to=2026-11-03", nil).decode(t, &onThird)
	if len(onThird.Items) != 2 {
		t.Fatalf("date filter = %+v", onThird.Items)
	}
	owner.expect(http.StatusBadRequest, http.MethodGet, base+"?accountId=nope", nil)
}

// Concurrent transfers in both directions lock accounts in a fixed order: no deadlock, no
// lost update, and the family cash is unchanged.
func TestConcurrentTransfersInOppositeDirections(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	a := owner.createAccount(h.ID, map[string]any{"name": "A", "openingBalanceCents": 100000})
	b := owner.createAccount(h.ID, map[string]any{"name": "B", "openingBalanceCents": 100000})

	const perDirection = 10
	var wg sync.WaitGroup
	statuses := make(chan int, 2*perDirection)
	start := make(chan struct{})
	for i := range 2 * perDirection {
		from, to := a.ID, b.ID
		if i%2 == 1 {
			from, to = b.ID, a.ID
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r := owner.do(http.MethodPost, "/api/v1/households/"+h.ID+"/transfers",
				map[string]any{"fromAccountId": from, "toAccountId": to, "amountCents": 700, "postedOn": "2026-11-10"},
				"Idempotency-Key", uuid.NewString())
			statuses <- r.status
		}()
	}
	close(start)
	wg.Wait()
	close(statuses)
	for s := range statuses {
		if s != http.StatusCreated {
			t.Fatalf("concurrent transfer failed with %d", s)
		}
	}
	// Equal number of transfers each way: balances return to the start.
	if owner.balance(h.ID, a.ID) != 100000 || owner.balance(h.ID, b.ID) != 100000 {
		t.Fatalf("balances drifted: a=%d b=%d", owner.balance(h.ID, a.ID), owner.balance(h.ID, b.ID))
	}
	var page struct{ Items []transferDTO }
	owner.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+h.ID+"/transfers?limit=100", nil).decode(t, &page)
	if len(page.Items) != 2*perDirection {
		t.Fatalf("transfers = %d, want %d", len(page.Items), 2*perDirection)
	}
	assertInvariants(t)
}

// The transfer row and its movements are immutable at the database level (I-37).
func TestTransfersAreImmutable(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	a := owner.createAccount(h.ID, map[string]any{"name": "A"})
	b := owner.createAccount(h.ID, map[string]any{"name": "B"})
	tr := owner.transfer(h.ID, a.ID, b.ID, 100, "2026-11-10")

	if _, err := api.pool.Exec(t.Context(), "UPDATE transfers SET amount_cents = 1 WHERE id = $1", tr.ID); err == nil {
		t.Fatal("transfers must reject UPDATE")
	}
	if _, err := api.pool.Exec(t.Context(), "DELETE FROM transfers WHERE id = $1", tr.ID); err == nil {
		t.Fatal("transfers must reject DELETE")
	}
	// A TRANSFER movement without its transfer is rejected by the schema.
	if _, err := api.pool.Exec(t.Context(), `INSERT INTO account_movements (id, household_id, account_id, direction, amount_cents, posted_on, kind, description)
		VALUES (uuidv7(), $1, $2, 'IN', 1, '2026-11-10', 'TRANSFER', 'x')`, h.ID, a.ID); err == nil {
		t.Fatal("orphan TRANSFER movement must be rejected")
	}
}

// The invariant checker must actually detect broken data (on a separate database, so the
// shared one stays consistent for the end-of-run check).
func TestInvariantCheckerDetectsViolations(t *testing.T) {
	url := createDatabase(t, "invariants_negative")
	if err := postgres.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(t.Context())
	_, err = conn.Exec(t.Context(), `
		INSERT INTO users (id, name, email, password_hash) VALUES ('00000000-0000-7000-8000-000000000001', 'U', 'u@example.com', 'x');
		INSERT INTO households (id, name, created_by) VALUES ('00000000-0000-7000-8000-000000000002', 'H', '00000000-0000-7000-8000-000000000001');
		INSERT INTO household_members (id, household_id, user_id, role, status, joined_at)
		  VALUES ('00000000-0000-7000-8000-000000000003', '00000000-0000-7000-8000-000000000002', '00000000-0000-7000-8000-000000000001', 'OWNER', 'ACTIVE', now());
		INSERT INTO accounts (id, household_id, name, type, ownership, opening_balance_cents, opened_on) VALUES
		  ('00000000-0000-7000-8000-000000000004', '00000000-0000-7000-8000-000000000002', 'A', 'CHECKING', 'JOINT', 0, '2026-11-01'),
		  ('00000000-0000-7000-8000-000000000005', '00000000-0000-7000-8000-000000000002', 'B', 'CHECKING', 'JOINT', 0, '2026-11-01');
		-- INV-08: a transfer with only its OUT leg.
		INSERT INTO transfers (id, household_id, from_account_id, to_account_id, amount_cents, posted_on, description, created_by_member_id)
		  VALUES ('00000000-0000-7000-8000-000000000006', '00000000-0000-7000-8000-000000000002', '00000000-0000-7000-8000-000000000004',
		          '00000000-0000-7000-8000-000000000005', 100, '2026-11-10', 'x', '00000000-0000-7000-8000-000000000003');
		INSERT INTO account_movements (id, household_id, account_id, direction, amount_cents, posted_on, kind, transfer_id, description)
		  VALUES ('00000000-0000-7000-8000-000000000007', '00000000-0000-7000-8000-000000000002', '00000000-0000-7000-8000-000000000004',
		          'OUT', 100, '2026-11-10', 'TRANSFER', '00000000-0000-7000-8000-000000000006', 'x');
		-- INV-18: a "reversal" in the same direction as the original.
		INSERT INTO account_movements (id, household_id, account_id, direction, amount_cents, posted_on, kind, description)
		  VALUES ('00000000-0000-7000-8000-000000000008', '00000000-0000-7000-8000-000000000002', '00000000-0000-7000-8000-000000000004',
		          'OUT', 50, '2026-11-10', 'DIRECT_PAYMENT', 'x');
		INSERT INTO account_movements (id, household_id, account_id, direction, amount_cents, posted_on, kind, reversal_of_id, description)
		  VALUES ('00000000-0000-7000-8000-000000000009', '00000000-0000-7000-8000-000000000002', '00000000-0000-7000-8000-000000000004',
		          'OUT', 50, '2026-11-10', 'REVERSAL', '00000000-0000-7000-8000-000000000008', 'x');
		INSERT INTO categories (id, household_id, name, kind) VALUES
		  ('00000000-0000-7000-8000-00000000000a', '00000000-0000-7000-8000-000000000002', 'C', 'EXPENSE');
		-- INV-03: a transaction without its obligation.
		INSERT INTO transactions (id, household_id, kind, scope, created_by_member_id, description, amount_cents, category_id,
		                          occurred_on, payment_method, account_id)
		  VALUES ('00000000-0000-7000-8000-00000000000b', '00000000-0000-7000-8000-000000000002', 'EXPENSE', 'FAMILY',
		          '00000000-0000-7000-8000-000000000003', 'x', 1000, '00000000-0000-7000-8000-00000000000a', '2026-11-10', 'PIX',
		          '00000000-0000-7000-8000-000000000004');
		INSERT INTO obligations (id, household_id, kind, source, status, amount_cents, due_on, description, category_id, scope,
		                         payment_method, competence_on, created_by_member_id)
		  VALUES ('00000000-0000-7000-8000-00000000000c', '00000000-0000-7000-8000-000000000002', 'PAYABLE', 'MANUAL_FORECAST',
		          'PLANNED', 100, '2026-11-10', 'x', '00000000-0000-7000-8000-00000000000a', 'FAMILY', 'PIX', '2026-11-10',
		          '00000000-0000-7000-8000-000000000003');
		-- INV-04: 150 paid on an obligation of 100.
		INSERT INTO account_movements (id, household_id, account_id, direction, amount_cents, posted_on, kind, description)
		  VALUES ('00000000-0000-7000-8000-00000000000d', '00000000-0000-7000-8000-000000000002', '00000000-0000-7000-8000-000000000004',
		          'OUT', 150, '2026-11-10', 'DIRECT_PAYMENT', 'x');
		INSERT INTO obligation_payments (id, household_id, obligation_id, account_movement_id, amount_cents, paid_on, created_by_member_id)
		  VALUES ('00000000-0000-7000-8000-00000000000e', '00000000-0000-7000-8000-000000000002', '00000000-0000-7000-8000-00000000000c',
		          '00000000-0000-7000-8000-00000000000d', 150, '2026-11-10', '00000000-0000-7000-8000-000000000003');
		-- INV-05: a payable paid by an incoming movement (the payment itself pushes INV-04 further).
		INSERT INTO account_movements (id, household_id, account_id, direction, amount_cents, posted_on, kind, description)
		  VALUES ('00000000-0000-7000-8000-00000000000f', '00000000-0000-7000-8000-000000000002', '00000000-0000-7000-8000-000000000004',
		          'IN', 20, '2026-11-10', 'DIRECT_RECEIPT', 'x');
		INSERT INTO obligation_payments (id, household_id, obligation_id, account_movement_id, amount_cents, paid_on, created_by_member_id)
		  VALUES ('00000000-0000-7000-8000-000000000010', '00000000-0000-7000-8000-000000000002', '00000000-0000-7000-8000-00000000000c',
		          '00000000-0000-7000-8000-00000000000f', 20, '2026-11-10', '00000000-0000-7000-8000-000000000003');
		-- INV-19: a payment reversed while its obligation is still open.
		INSERT INTO account_movements (id, household_id, account_id, direction, amount_cents, posted_on, kind, reversal_of_id, description)
		  VALUES ('00000000-0000-7000-8000-000000000011', '00000000-0000-7000-8000-000000000002', '00000000-0000-7000-8000-000000000004',
		          'IN', 150, '2026-11-10', 'REVERSAL', '00000000-0000-7000-8000-00000000000d', 'x');`)
	if err != nil {
		t.Fatal(err)
	}

	violations, err := invariantViolations(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]int{}
	for _, v := range violations {
		found[v[:6]]++
	}
	// INV-05 twice: the mismatched payment and the orphan DIRECT_PAYMENT planted for INV-18.
	// INV-13: the payments were planted on a PLANNED obligation.
	// INV-19 twice: the planted reversal of a payment, and the INV-18 "reversal" of an orphan movement.
	want := map[string]int{"INV-03": 1, "INV-04": 1, "INV-05": 2, "INV-08": 1, "INV-13": 1, "INV-18": 1, "INV-19": 2}
	if !maps.Equal(found, want) {
		t.Fatalf("violations = %v, want %v", violations, want)
	}
}
