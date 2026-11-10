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
func householdRoutes(memberID, inviteID, categoryID string) []struct {
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
		{http.MethodDelete, "/members/" + memberID, nil},
		{http.MethodPost, "/invites", map[string]any{}},
		{http.MethodGet, "/invites", nil},
		{http.MethodDelete, "/invites/" + inviteID, nil},
		{http.MethodGet, "/categories", nil},
		{http.MethodPost, "/categories", map[string]any{"name": "Invasão", "kind": "EXPENSE"}},
		{http.MethodPatch, "/categories/" + categoryID, map[string]any{"name": "Invadida"}},
		{http.MethodDelete, "/categories/" + categoryID, nil},
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

	ownerB, _ := api.registerUser("ownerB")
	b := ownerB.createHousehold("Casa B")

	before := countRows(t, api)
	for _, rt := range householdRoutes(a.MemberID, invA.ID, catA.ID) {
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
	for _, rt := range householdRoutes("{memberId}", "{inviteId}", "{categoryId}") {
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
	for _, table := range []string{"households", "household_members", "household_invites", "audit_events", "categories"} {
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
