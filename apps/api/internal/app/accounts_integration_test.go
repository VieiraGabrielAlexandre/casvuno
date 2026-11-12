//go:build integration

package app

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

type accountDTO struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Ownership           string  `json:"ownership"`
	OwnerMemberID       *string `json:"ownerMemberId"`
	OpeningBalanceCents int64   `json:"openingBalanceCents"`
	OpenedOn            string  `json:"openedOn"`
	BalanceCents        int64   `json:"balanceCents"`
	IsActive            bool    `json:"isActive"`
}

type accountListDTO struct {
	Items             []accountDTO
	TotalBalanceCents int64
}

type movementDTO struct {
	ID          string  `json:"id"`
	Direction   string  `json:"direction"`
	AmountCents int64   `json:"amountCents"`
	PostedOn    string  `json:"postedOn"`
	Kind        string  `json:"kind"`
	ReversalOf  *string `json:"reversalOfId"`
}

// createAccount opens an account; fields override sensible defaults.
func (c *client) createAccount(householdID string, fields map[string]any) accountDTO {
	c.api.t.Helper()
	body := map[string]any{"name": "Conta", "type": "CHECKING", "ownership": "JOINT", "openingBalanceCents": 0, "openedOn": "2026-11-01"}
	for k, v := range fields {
		body[k] = v
	}
	var a accountDTO
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/households/"+householdID+"/accounts", body).decode(c.api.t, &a)
	return a
}

func (c *client) accounts(householdID, query string) accountListDTO {
	c.api.t.Helper()
	var l accountListDTO
	c.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+householdID+"/accounts"+query, nil).decode(c.api.t, &l)
	return l
}

// paidMovementID returns the cash movement of an immediate entry (via its obligation payment).
func (api *testAPI) paidMovementID(transactionID string) string {
	api.t.Helper()
	var id string
	err := api.pool.QueryRow(context.Background(), `
		SELECT p.account_movement_id FROM obligation_payments p
		JOIN obligations o ON o.household_id = p.household_id AND o.id = p.obligation_id
		WHERE o.transaction_id = $1`, transactionID).Scan(&id)
	if err != nil {
		api.t.Fatal(err)
	}
	return id
}

// T21: individual and joint accounts are both part of the family cash, each counted once.
func TestConsolidatedBalanceCountsEveryAccountOnce(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	member, _ := api.registerUser("member")
	var joined householdDTO
	member.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept", map[string]any{"token": owner.createInvite(h.ID).Token}).decode(t, &joined)

	joint := owner.createAccount(h.ID, map[string]any{"name": "Conjunta", "openingBalanceCents": 100000})
	individual := member.createAccount(h.ID, map[string]any{
		"name": "Da Bia", "ownership": "INDIVIDUAL", "ownerMemberId": joined.MemberID, "openingBalanceCents": 50000,
	})
	if individual.OwnerMemberID == nil || *individual.OwnerMemberID != joined.MemberID || joint.OwnerMemberID != nil {
		t.Fatalf("ownership: joint=%+v individual=%+v", joint, individual)
	}
	owner.createAccount(h.ID, map[string]any{"name": "Cheque especial", "openingBalanceCents": -2500})

	// Every member sees every account (RN-002).
	for _, c := range []*client{owner, member} {
		l := c.accounts(h.ID, "")
		if len(l.Items) != 3 || l.TotalBalanceCents != 100000+50000-2500 {
			t.Fatalf("list = %+v", l)
		}
	}
}

// RN-017 / INV-09: balance = opening + Σ IN − Σ OUT, reversals included.
func TestBalanceIsDerivedFromMovements(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	a := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 10000})
	b := owner.createAccount(h.ID, map[string]any{"name": "Outra", "openingBalanceCents": 0})

	expenseCat, incomeCat := owner.firstCategory(h.ID, "EXPENSE"), owner.firstCategory(h.ID, "INCOME")
	owner.createTransaction(h.ID, entryBody("INCOME", incomeCat.ID, a.ID, 5000, "2026-11-02"))
	out := owner.createTransaction(h.ID, entryBody("EXPENSE", expenseCat.ID, a.ID, 1250, "2026-11-03"))
	owner.voidTransaction(h.ID, out.ID) // REVERSAL IN 1250
	owner.transfer(h.ID, a.ID, b.ID, 3000, "2026-11-05")

	var got accountDTO
	owner.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+h.ID+"/accounts/"+a.ID, nil).decode(t, &got)
	if want := int64(10000 + 5000 - 1250 + 1250 - 3000); got.BalanceCents != want {
		t.Fatalf("balance = %d, want %d", got.BalanceCents, want)
	}
	// An internal transfer leaves the consolidated balance unchanged (T04 shape).
	if l := owner.accounts(h.ID, ""); l.TotalBalanceCents != 10000+5000 {
		t.Fatalf("total = %d", l.TotalBalanceCents)
	}

	// Movements are append-only at the database level.
	if _, err := api.pool.Exec(context.Background(), "UPDATE account_movements SET amount_cents = 1 WHERE account_id = $1", a.ID); err == nil {
		t.Fatal("account_movements must reject UPDATE")
	}
	if _, err := api.pool.Exec(context.Background(), "DELETE FROM account_movements WHERE account_id = $1", a.ID); err == nil {
		t.Fatal("account_movements must reject DELETE")
	}
}

func TestStatementPaginationIsStableAndFiltered(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	a := owner.createAccount(h.ID, nil)
	base := "/api/v1/households/" + h.ID + "/accounts/" + a.ID + "/movements"

	income := owner.firstCategory(h.ID, "INCOME")
	days := []string{"2026-11-01", "2026-11-03", "2026-11-03", "2026-11-03", "2026-11-05", "2026-11-07", "2026-11-07"}
	for i, d := range days {
		owner.createTransaction(h.ID, entryBody("INCOME", income.ID, a.ID, int64(100+i), d))
	}

	var all []movementDTO
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("pagination does not terminate")
		}
		q := "?limit=3"
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		var page struct {
			Items      []movementDTO
			NextCursor *string
		}
		owner.expect(http.StatusOK, http.MethodGet, base+q, nil).decode(t, &page)
		all = append(all, page.Items...)
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if len(all) != len(days) {
		t.Fatalf("got %d movements, want %d", len(all), len(days))
	}
	seen := map[string]bool{}
	for i, m := range all {
		if seen[m.ID] {
			t.Fatalf("duplicate %s across pages", m.ID)
		}
		seen[m.ID] = true
		// Newest first; same day → most recently created first (UUIDv7 order).
		if i > 0 && (m.PostedOn > all[i-1].PostedOn || (m.PostedOn == all[i-1].PostedOn && m.ID > all[i-1].ID)) {
			t.Fatalf("order broken at %d: %+v after %+v", i, m, all[i-1])
		}
	}

	var filtered struct{ Items []movementDTO }
	owner.expect(http.StatusOK, http.MethodGet, base+"?from=2026-11-03&to=2026-11-05", nil).decode(t, &filtered)
	if len(filtered.Items) != 4 {
		t.Fatalf("from/to filter returned %d, want 4", len(filtered.Items))
	}
	for _, q := range []string{"?limit=0", "?limit=101", "?cursor=lixo", "?from=2026-13-01", "?from=2026-11-05&to=2026-11-01"} {
		owner.expect(http.StatusBadRequest, http.MethodGet, base+q, nil)
	}
}

func TestAccountValidation(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	base := "/api/v1/households/" + h.ID + "/accounts"
	owner.createAccount(h.ID, map[string]any{"name": "Nubank"})

	member, _ := api.registerUser("member")
	var joined householdDTO
	member.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept", map[string]any{"token": owner.createInvite(h.ID).Token}).decode(t, &joined)
	owner.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/households/"+h.ID+"/members/"+joined.MemberID, nil)

	valid := func(over map[string]any) map[string]any {
		b := map[string]any{"name": "Nova", "type": "CHECKING", "ownership": "JOINT", "openingBalanceCents": 0, "openedOn": "2026-11-01"}
		for k, v := range over {
			b[k] = v
		}
		return b
	}
	for _, tc := range []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"individual without owner", valid(map[string]any{"ownership": "INDIVIDUAL"}), 400, "VALIDATION_ERROR"},
		{"joint with owner", valid(map[string]any{"ownerMemberId": h.MemberID}), 400, "VALIDATION_ERROR"},
		{"inactive member as owner", valid(map[string]any{"ownership": "INDIVIDUAL", "ownerMemberId": joined.MemberID}), 422, "OWNER_NOT_MEMBER"},
		{"invalid type", valid(map[string]any{"type": "CRYPTO"}), 400, "VALIDATION_ERROR"},
		{"invalid ownership", valid(map[string]any{"ownership": "SHARED"}), 400, "VALIDATION_ERROR"},
		{"invalid date", valid(map[string]any{"openedOn": "01/11/2026"}), 400, "VALIDATION_ERROR"},
		{"amount over the limit", valid(map[string]any{"openingBalanceCents": int64(1_000_000_000_000_001)}), 400, "VALIDATION_ERROR"},
		{"float amount", valid(map[string]any{"openingBalanceCents": 12.5}), 400, "VALIDATION_ERROR"},
		{"duplicate name (case-insensitive)", valid(map[string]any{"name": "NUBANK"}), 409, "ACCOUNT_NAME_TAKEN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := owner.expect(tc.status, http.MethodPost, base, tc.body).errorCode(t); code != tc.code {
				t.Fatalf("got %s, want %s", code, tc.code)
			}
		})
	}
}

func TestArchiveRequiresZeroBalanceAndFreezesTheAccount(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	a := owner.createAccount(h.ID, map[string]any{"name": "Poupança", "type": "SAVINGS", "openingBalanceCents": 10000})
	path := "/api/v1/households/" + h.ID + "/accounts/" + a.ID

	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPatch, path, map[string]any{"isActive": false}).
		errorCode(t); code != "ACCOUNT_BALANCE_NOT_ZERO" {
		t.Fatalf("got %s", code)
	}

	// Once the money leaves (here: an expense), the account can be archived.
	owner.createTransaction(h.ID, entryBody("EXPENSE", owner.firstCategory(h.ID, "EXPENSE").ID, a.ID, 10000, "2026-11-05"))
	var archived accountDTO
	owner.expect(http.StatusOK, http.MethodPatch, path, map[string]any{"isActive": false}).decode(t, &archived)
	if archived.IsActive || archived.BalanceCents != 0 {
		t.Fatalf("archived = %+v", archived)
	}

	if len(owner.accounts(h.ID, "").Items) != 0 || len(owner.accounts(h.ID, "?includeArchived=true").Items) != 1 {
		t.Fatal("archived accounts are hidden unless includeArchived=true")
	}
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPatch, path, map[string]any{"openingBalanceCents": 500}).
		errorCode(t); code != "ACCOUNT_ARCHIVED" {
		t.Fatalf("got %s", code)
	}
	// The statement of an archived account stays available (history).
	owner.expect(http.StatusOK, http.MethodGet, path+"/movements", nil)

	var restored accountDTO
	owner.expect(http.StatusOK, http.MethodPatch, path, map[string]any{"isActive": true}).decode(t, &restored)
	if !restored.IsActive {
		t.Fatal("account must be restored")
	}
}

func TestUpdateAccountOwnershipAndOpeningBalance(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	a := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 1000})
	path := "/api/v1/households/" + h.ID + "/accounts/" + a.ID

	owner.expect(http.StatusBadRequest, http.MethodPatch, path, map[string]any{"ownership": "INDIVIDUAL"})
	var individual accountDTO
	owner.expect(http.StatusOK, http.MethodPatch, path, map[string]any{"ownership": "INDIVIDUAL", "ownerMemberId": h.MemberID}).decode(t, &individual)
	if individual.Ownership != "INDIVIDUAL" || individual.OwnerMemberID == nil || *individual.OwnerMemberID != h.MemberID {
		t.Fatalf("individual = %+v", individual)
	}
	var joint accountDTO
	owner.expect(http.StatusOK, http.MethodPatch, path, map[string]any{"ownership": "JOINT"}).decode(t, &joint)
	if joint.OwnerMemberID != nil {
		t.Fatalf("JOINT must clear the owner: %+v", joint)
	}

	// Correcting the opening balance changes the derived balance; archiving in the same
	// request uses the corrected value.
	var corrected accountDTO
	owner.expect(http.StatusOK, http.MethodPatch, path, map[string]any{"openingBalanceCents": 0, "isActive": false, "openedOn": "2026-10-01"}).decode(t, &corrected)
	if corrected.BalanceCents != 0 || corrected.IsActive || corrected.OpenedOn != "2026-10-01" {
		t.Fatalf("corrected = %+v", corrected)
	}
}
