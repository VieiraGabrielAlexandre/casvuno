//go:build integration

package app

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestHouseholdLifecycle(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")

	h := owner.createHousehold("  Casa Vieira  ")
	if h.Name != "Casa Vieira" || h.Timezone != "America/Sao_Paulo" || h.Role != "OWNER" {
		t.Fatalf("created = %+v", h)
	}

	r := owner.expect(http.StatusBadRequest, http.MethodPost, "/api/v1/households",
		map[string]any{"name": "X", "timezone": "Mars/Olympus"})
	if r.errorCode(t) != "VALIDATION_ERROR" {
		t.Fatalf("got %s", r.body)
	}

	var list struct{ Items []householdDTO }
	owner.expect(http.StatusOK, http.MethodGet, "/api/v1/households", nil).decode(t, &list)
	if len(list.Items) != 1 || list.Items[0].ID != h.ID {
		t.Fatalf("list = %+v", list)
	}

	var updated householdDTO
	owner.expect(http.StatusOK, http.MethodPatch, "/api/v1/households/"+h.ID,
		map[string]any{"name": "Casa Nova", "timezone": "America/Manaus"}).decode(t, &updated)
	if updated.Name != "Casa Nova" || updated.Timezone != "America/Manaus" {
		t.Fatalf("updated = %+v", updated)
	}

	var members struct {
		Items []struct{ Role, Status string }
	}
	owner.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+h.ID+"/members", nil).decode(t, &members)
	if len(members.Items) != 1 || members.Items[0].Role != "OWNER" || members.Items[0].Status != "ACTIVE" {
		t.Fatalf("members = %+v", members)
	}

	owner.expect(http.StatusNotFound, http.MethodGet, "/api/v1/households/not-a-uuid", nil)
}

func TestInviteFlowAndRoles(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	base := "/api/v1/households/" + h.ID

	inv := owner.createInvite(h.ID)
	if inv.Token == "" || inv.URL != testOrigin+"/invite?token="+inv.Token {
		t.Fatalf("invite = %+v", inv)
	}
	var pending struct{ Items []map[string]any }
	owner.expect(http.StatusOK, http.MethodGet, base+"/invites", nil).decode(t, &pending)
	if len(pending.Items) != 1 || pending.Items[0]["token"] != nil {
		t.Fatalf("pending invites must not expose tokens: %+v", pending)
	}

	member, _ := api.registerUser("member")
	var joined householdDTO
	member.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept", map[string]any{"token": inv.Token}).decode(t, &joined)
	if joined.ID != h.ID || joined.Role != "MEMBER" {
		t.Fatalf("joined = %+v", joined)
	}

	// Invites are single use.
	other, _ := api.registerUser("other")
	r := other.expect(http.StatusNotFound, http.MethodPost, "/api/v1/invites/accept", map[string]any{"token": inv.Token})
	if r.errorCode(t) != "INVITE_INVALID" {
		t.Fatalf("got %s", r.body)
	}
	// Already a member.
	again := owner.createInvite(h.ID)
	if code := member.expect(http.StatusConflict, http.MethodPost, "/api/v1/invites/accept",
		map[string]any{"token": again.Token}).errorCode(t); code != "ALREADY_MEMBER" {
		t.Fatalf("got %s", code)
	}

	// MEMBER sees the household (RN-002) but cannot administer it.
	member.expect(http.StatusOK, http.MethodGet, base, nil)
	member.expect(http.StatusOK, http.MethodGet, base+"/members", nil)
	for _, rt := range []struct{ method, path string }{
		{http.MethodPatch, base},
		{http.MethodPost, base + "/invites"},
		{http.MethodGet, base + "/invites"},
		{http.MethodDelete, base + "/invites/" + again.ID},
		{http.MethodDelete, base + "/members/" + joined.MemberID},
	} {
		if code := member.expect(http.StatusForbidden, rt.method, rt.path, map[string]any{}).errorCode(t); code != "OWNER_REQUIRED" {
			t.Errorf("%s %s: %s", rt.method, rt.path, code)
		}
	}

	// Revoked invites cannot be used.
	owner.expect(http.StatusNoContent, http.MethodDelete, base+"/invites/"+again.ID, nil)
	owner.expect(http.StatusNotFound, http.MethodDelete, base+"/invites/"+again.ID, nil)
	other.expect(http.StatusNotFound, http.MethodPost, "/api/v1/invites/accept", map[string]any{"token": again.Token})
}

func TestDeactivateMemberPreservesHistoryAndKeepsAnOwner(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	base := "/api/v1/households/" + h.ID

	member, _ := api.registerUser("member")
	var joined householdDTO
	member.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept",
		map[string]any{"token": owner.createInvite(h.ID).Token}).decode(t, &joined)

	// The only OWNER cannot leave the household without an owner (INV-17).
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodDelete, base+"/members/"+h.MemberID, nil).
		errorCode(t); code != "LAST_OWNER" {
		t.Fatalf("got %s", code)
	}

	owner.expect(http.StatusNoContent, http.MethodDelete, base+"/members/"+joined.MemberID, nil)
	owner.expect(http.StatusNoContent, http.MethodDelete, base+"/members/"+joined.MemberID, nil) // idempotent

	// T16: no future access, history (the member row) preserved as INACTIVE.
	member.expect(http.StatusNotFound, http.MethodGet, base, nil)
	var list struct{ Items []householdDTO }
	member.expect(http.StatusOK, http.MethodGet, "/api/v1/households", nil).decode(t, &list)
	if len(list.Items) != 0 {
		t.Fatalf("deactivated member still lists the household: %+v", list)
	}
	var members struct {
		Items []struct{ ID, Status string }
	}
	owner.expect(http.StatusOK, http.MethodGet, base+"/members", nil).decode(t, &members)
	if len(members.Items) != 2 || members.Items[1].Status != "INACTIVE" {
		t.Fatalf("members = %+v", members)
	}

	// A new invite reactivates the same membership as MEMBER.
	var rejoined householdDTO
	member.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept",
		map[string]any{"token": owner.createInvite(h.ID).Token}).decode(t, &rejoined)
	if rejoined.MemberID != joined.MemberID || rejoined.Role != "MEMBER" {
		t.Fatalf("rejoined = %+v, want member %s", rejoined, joined.MemberID)
	}
}

// householdRoutes lists every route under /households/{householdId} with a request that
// exercises it. TestEveryHouseholdRouteIsCoveredByIsolationTests keeps it complete.
// routeIDs are resource IDs used to build household-scoped paths.
type routeIDs struct{ member, invite, category, account, transaction, obligation string }

func householdRoutes(ids routeIDs) []struct {
	method, path string
	body         any
} {
	return []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/", nil},
		{http.MethodPatch, "/", map[string]any{"name": "Invadida"}},
		{http.MethodGet, "/members", nil},
		{http.MethodDelete, "/members/" + ids.member, nil},
		{http.MethodPost, "/invites", map[string]any{}},
		{http.MethodGet, "/invites", nil},
		{http.MethodDelete, "/invites/" + ids.invite, nil},
		{http.MethodGet, "/categories", nil},
		{http.MethodPost, "/categories", map[string]any{"name": "Invasão", "kind": "EXPENSE"}},
		{http.MethodPatch, "/categories/" + ids.category, map[string]any{"name": "Invadida"}},
		{http.MethodDelete, "/categories/" + ids.category, nil},
		{http.MethodGet, "/accounts", nil},
		{http.MethodPost, "/accounts", map[string]any{"name": "Invasão", "type": "CHECKING", "ownership": "JOINT", "openingBalanceCents": 0, "openedOn": "2026-11-10"}},
		{http.MethodGet, "/accounts/" + ids.account, nil},
		{http.MethodPatch, "/accounts/" + ids.account, map[string]any{"name": "Invadida"}},
		{http.MethodGet, "/accounts/" + ids.account + "/movements", nil},
		{http.MethodGet, "/transfers", nil},
		{http.MethodPost, "/transfers", map[string]any{"fromAccountId": ids.account, "toAccountId": ids.account, "amountCents": 1, "postedOn": "2026-11-10"}},
		{http.MethodPost, "/transactions", expenseBody(ids.category, ids.account, 1)},
		{http.MethodGet, "/transactions/" + ids.transaction, nil},
		{http.MethodGet, "/obligations", nil},
		{http.MethodGet, "/obligations/" + ids.obligation, nil},
		{http.MethodPatch, "/obligations/" + ids.obligation, map[string]any{"amountCents": 1}},
		{http.MethodPost, "/obligations/" + ids.obligation + "/payments", map[string]any{"amountCents": 1, "paidOn": "2026-11-10"}},
	}
}

// TestHouseholdIsolation: a user of household B gets 404 on every route of household A,
// cannot reach A's resources through B's own routes, and nothing changes in the database (T15).
func TestHouseholdIsolation(t *testing.T) {
	api := newTestAPI(t)
	ownerA, _ := api.registerUser("ownerA")
	a := ownerA.createHousehold("Casa A")
	invA := ownerA.createInvite(a.ID)
	catA := ownerA.firstCategory(a.ID, "EXPENSE")
	accA := ownerA.createAccount(a.ID, map[string]any{"name": "Conta A", "ownership": "JOINT", "openingBalanceCents": 1000})
	txA := ownerA.createTransaction(a.ID, expenseBody(catA.ID, accA.ID, 500))
	forecastA := ownerA.createForecast(a.ID, forecastBody("EXPENSE", catA.ID, &accA.ID, 700, "2026-11-10", "2026-11-20"))

	ownerB, _ := api.registerUser("ownerB")
	b := ownerB.createHousehold("Casa B")
	accB := ownerB.createAccount(b.ID, map[string]any{"name": "Conta B"})
	catB := ownerB.firstCategory(b.ID, "EXPENSE")

	before := countRows(t, api)
	for _, rt := range householdRoutes(routeIDs{member: a.MemberID, invite: invA.ID, category: catA.ID, account: accA.ID, transaction: txA.ID, obligation: forecastA.ID}) {
		path := strings.TrimSuffix("/api/v1/households/"+a.ID+rt.path, "/")
		r := ownerB.expect(http.StatusNotFound, rt.method, path, rt.body)
		if r.errorCode(t) != "HOUSEHOLD_NOT_FOUND" {
			t.Errorf("%s %s: %s", rt.method, path, r.body)
		}
	}
	// A's member and invite IDs addressed through B's household: not found in B.
	ownerB.expect(http.StatusNotFound, http.MethodDelete, "/api/v1/households/"+b.ID+"/members/"+a.MemberID, nil)
	ownerB.expect(http.StatusNotFound, http.MethodDelete, "/api/v1/households/"+b.ID+"/invites/"+invA.ID, nil)
	ownerB.expect(http.StatusNotFound, http.MethodPatch, "/api/v1/households/"+b.ID+"/categories/"+catA.ID, map[string]any{"name": "x"})
	ownerB.expect(http.StatusNotFound, http.MethodDelete, "/api/v1/households/"+b.ID+"/categories/"+catA.ID, nil)
	ownerB.expect(http.StatusNotFound, http.MethodGet, "/api/v1/households/"+b.ID+"/accounts/"+accA.ID, nil)
	ownerB.expect(http.StatusNotFound, http.MethodGet, "/api/v1/households/"+b.ID+"/accounts/"+accA.ID+"/movements", nil)
	ownerB.expect(http.StatusNotFound, http.MethodPatch, "/api/v1/households/"+b.ID+"/accounts/"+accA.ID, map[string]any{"isActive": false})
	// A's accounts used in a transfer inside B: rejected as if they did not exist.
	for _, body := range []map[string]any{
		{"fromAccountId": accA.ID, "toAccountId": accB.ID, "amountCents": 100, "postedOn": "2026-11-10"},
		{"fromAccountId": accB.ID, "toAccountId": accA.ID, "amountCents": 100, "postedOn": "2026-11-10"},
	} {
		if code := ownerB.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/households/"+b.ID+"/transfers", body,
			"Idempotency-Key", uuid.NewString()).errorCode(t); code != "ACCOUNT_NOT_FOUND" {
			t.Fatalf("got %s", code)
		}
	}
	// A's transaction read through B, and A's category, account or member used in an entry in B:
	// rejected as if they did not exist (T15, RN-021).
	ownerB.expect(http.StatusNotFound, http.MethodGet, "/api/v1/households/"+b.ID+"/transactions/"+txA.ID, nil)
	personal := expenseBody(catB.ID, accB.ID, 100)
	personal["scope"], personal["personMemberId"] = "PERSONAL", a.MemberID
	for _, tc := range []struct {
		body map[string]any
		code string
	}{
		{expenseBody(catA.ID, accB.ID, 100), "CATEGORY_NOT_FOUND"},
		{expenseBody(catB.ID, accA.ID, 100), "ACCOUNT_NOT_FOUND"},
		{personal, "PERSON_NOT_MEMBER"},
	} {
		if code := ownerB.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/households/"+b.ID+"/transactions", tc.body,
			"Idempotency-Key", uuid.NewString()).errorCode(t); code != tc.code {
			t.Fatalf("got %s, want %s", code, tc.code)
		}
	}
	// A's obligation addressed through B, and B's forecast paid from A's account.
	ownerB.expect(http.StatusNotFound, http.MethodGet, "/api/v1/households/"+b.ID+"/obligations/"+forecastA.ID, nil)
	ownerB.expect(http.StatusNotFound, http.MethodPatch, "/api/v1/households/"+b.ID+"/obligations/"+forecastA.ID, map[string]any{"amountCents": 1})
	ownerB.expect(http.StatusNotFound, http.MethodPost, "/api/v1/households/"+b.ID+"/obligations/"+forecastA.ID+"/payments",
		map[string]any{"amountCents": 1, "paidOn": "2026-11-10"}, "Idempotency-Key", uuid.NewString())
	if after := countRows(t, api); !maps.Equal(before, after) {
		t.Fatalf("cross-household requests changed data: before %v after %v", before, after)
	}
	forecastB := ownerB.createForecast(b.ID, forecastBody("EXPENSE", catB.ID, nil, 300, "2026-11-10", "2026-11-20"))
	before = countRows(t, api) // B's own forecast is a legitimate write
	if code := ownerB.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/households/"+b.ID+"/obligations/"+forecastB.ID+"/payments",
		map[string]any{"amountCents": 300, "paidOn": "2026-11-10", "accountId": accA.ID}, "Idempotency-Key", uuid.NewString()).errorCode(t); code != "ACCOUNT_NOT_FOUND" {
		t.Fatalf("got %s", code)
	}
	var listB struct{ Items []obligationDTO }
	ownerB.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+b.ID+"/obligations?view=all", nil).decode(t, &listB)
	if len(listB.Items) != 1 || listB.Items[0].ID != forecastB.ID {
		t.Fatalf("B lists %+v", listB.Items)
	}
	// A's member as owner of an account in B: rejected as if it did not exist (field ownerMemberId).
	if code := ownerB.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/households/"+b.ID+"/accounts",
		map[string]any{"name": "X", "type": "CHECKING", "ownership": "INDIVIDUAL", "ownerMemberId": a.MemberID,
			"openingBalanceCents": 0, "openedOn": "2026-11-10"}).errorCode(t); code != "OWNER_NOT_MEMBER" {
		t.Fatalf("got %s", code)
	}
	// A's category as parent of a category in B: rejected as if it did not exist (field parentId).
	if code := ownerB.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/households/"+b.ID+"/categories",
		map[string]any{"name": "Filha", "kind": "EXPENSE", "parentId": catA.ID}).errorCode(t); code != "PARENT_NOT_FOUND" {
		t.Fatalf("got %s", code)
	}

	if after := countRows(t, api); !maps.Equal(before, after) {
		t.Fatalf("cross-household requests changed data: before %v after %v", before, after)
	}

	// B's listing never contains A.
	var list struct{ Items []householdDTO }
	ownerB.expect(http.StatusOK, http.MethodGet, "/api/v1/households", nil).decode(t, &list)
	if len(list.Items) != 1 || list.Items[0].ID != b.ID {
		t.Fatalf("B lists %+v", list)
	}
	// A's invite remains usable by its intended recipient.
	guest, _ := api.registerUser("guest")
	guest.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept", map[string]any{"token": invA.Token})
}

// TestEveryHouseholdRouteIsCoveredByIsolationTests fails when a new household-scoped route is
// added without being listed in householdRoutes (ADR 0006).
func TestEveryHouseholdRouteIsCoveredByIsolationTests(t *testing.T) {
	api := newTestAPI(t)
	covered := map[string]bool{}
	for _, rt := range householdRoutes(routeIDs{"{memberId}", "{inviteId}", "{categoryId}", "{accountId}", "{transactionId}", "{obligationId}"}) {
		covered[rt.method+" /api/v1/households/{householdId}"+rt.path] = true
	}

	var missing []string
	err := chi.Walk(api.handler.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.Contains(route, "{householdId}") && !covered[method+" "+route] {
			missing = append(missing, method+" "+route)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(missing)
	if len(missing) > 0 {
		t.Fatalf("household routes without isolation coverage: %v", missing)
	}
}

func TestAuditTrail(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	member, _ := api.registerUser("member")
	var joined householdDTO
	member.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept",
		map[string]any{"token": owner.createInvite(h.ID).Token}).decode(t, &joined)
	owner.expect(http.StatusOK, http.MethodPatch, "/api/v1/households/"+h.ID, map[string]any{"name": "Casa 2"})
	owner.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/households/"+h.ID+"/members/"+joined.MemberID, nil)

	rows, err := api.pool.Query(context.Background(),
		"SELECT action, metadata::text FROM audit_events WHERE household_id = $1 ORDER BY created_at, id", h.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action, meta string
		if err := rows.Scan(&action, &meta); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(meta, "token") {
			t.Fatalf("audit metadata must not contain tokens: %s", meta)
		}
		actions = append(actions, action)
	}
	want := []string{"household.created", "invite.created", "invite.accepted", "household.updated", "member.deactivated"}
	if !slices.Equal(actions, want) {
		t.Fatalf("audit actions = %v, want %v", actions, want)
	}

	// Append-only at the database level.
	if _, err := api.pool.Exec(context.Background(), "DELETE FROM audit_events WHERE household_id = $1", h.ID); err == nil {
		t.Fatal("audit_events must reject DELETE")
	}
}

func countRows(t *testing.T, api *testAPI) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, table := range []string{"households", "household_members", "household_invites", "audit_events", "categories", "accounts", "account_movements", "transfers", "transactions", "obligations", "obligation_payments"} {
		var n int64
		if err := api.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[table] = n
	}
	var revoked int64
	if err := api.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM household_invites WHERE revoked_at IS NOT NULL").Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	out["revoked_invites"] = revoked
	var archived int64
	if err := api.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM categories WHERE NOT is_active").Scan(&archived); err != nil {
		t.Fatal(err)
	}
	out["archived_categories"] = archived
	return out
}
