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
)

type templateDTO struct {
	ID                 string  `json:"id"`
	Label              string  `json:"label"`
	Kind               string  `json:"kind"`
	Description        string  `json:"description"`
	DefaultAmountCents *int64  `json:"defaultAmountCents"`
	CategoryID         string  `json:"categoryId"`
	Scope              string  `json:"scope"`
	PersonMemberID     *string `json:"personMemberId"`
	PaymentMethod      string  `json:"paymentMethod"`
	AccountID          *string `json:"accountId"`
	SortOrder          int     `json:"sortOrder"`
}

// templateBody is a family "Café" expense template paid by Pix (account optional).
func templateBody(categoryID string, accountID *string) map[string]any {
	body := map[string]any{
		"label": "Café", "kind": "EXPENSE", "description": "Café da padaria", "defaultAmountCents": 1250,
		"categoryId": categoryID, "scope": "FAMILY", "paymentMethod": "PIX",
	}
	if accountID != nil {
		body["accountId"] = *accountID
	}
	return body
}

func (c *client) createTemplate(householdID string, body map[string]any) templateDTO {
	c.api.t.Helper()
	var tpl templateDTO
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/households/"+householdID+"/quick-templates", body).decode(c.api.t, &tpl)
	return tpl
}

func (c *client) templates(householdID string) []templateDTO {
	c.api.t.Helper()
	var list struct{ Items []templateDTO }
	c.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+householdID+"/quick-templates", nil).decode(c.api.t, &list)
	return list.Items
}

// entryFromTemplate is what the quick-entry screen sends for a complete template: its
// fields, today's date and a fresh Idempotency-Key (README §9: at most 3 interactions).
func entryFromTemplate(tpl templateDTO, occurredOn string) map[string]any {
	body := map[string]any{
		"kind": tpl.Kind, "scope": tpl.Scope, "description": tpl.Description, "amountCents": *tpl.DefaultAmountCents,
		"categoryId": tpl.CategoryID, "occurredOn": occurredOn,
		"payment": map[string]any{"method": tpl.PaymentMethod, "accountId": *tpl.AccountID, "mode": "IMMEDIATE"},
	}
	if tpl.PersonMemberID != nil {
		body["personMemberId"] = *tpl.PersonMemberID
	}
	return body
}

// Create, list (in order), edit, clear optional fields and delete one's own templates; a
// complete template becomes an entry with the same fields.
func TestQuickTemplateLifecycle(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 10000})
	cats := owner.categories(h.ID, "?kind=EXPENSE")
	cat, other := cats[0], cats[1]

	cafe := owner.createTemplate(h.ID, templateBody(cat.ID, &acc.ID))
	uberBody := templateBody(cat.ID, nil)
	uberBody["label"], uberBody["description"], uberBody["paymentMethod"] = " Uber ", "Corrida", "DEBIT"
	delete(uberBody, "defaultAmountCents")
	uber := owner.createTemplate(h.ID, uberBody)
	if cafe.SortOrder != 0 || uber.SortOrder != 1 || uber.Label != "Uber" || uber.DefaultAmountCents != nil || uber.AccountID != nil ||
		cafe.DefaultAmountCents == nil || *cafe.DefaultAmountCents != 1250 || cafe.AccountID == nil || *cafe.AccountID != acc.ID {
		t.Fatalf("created = %+v / %+v", cafe, uber)
	}
	first := templateBody(cat.ID, nil)
	first["label"], first["sortOrder"] = "Almoço", 0
	owner.createTemplate(h.ID, first)
	// Ordered by sortOrder, then label.
	labels := func() []string {
		var out []string
		for _, tpl := range owner.templates(h.ID) {
			out = append(out, tpl.Label)
		}
		return out
	}
	if got := labels(); !slices.Equal(got, []string{"Almoço", "Café", "Uber"}) {
		t.Fatalf("order = %v", got)
	}

	// A complete template is a valid entry as is.
	tx := owner.createTransaction(h.ID, entryFromTemplate(cafe, "2026-11-10"))
	if tx.AmountCents != 1250 || tx.Description != "Café da padaria" || tx.CategoryID != cat.ID || tx.PaymentMethod != "PIX" {
		t.Fatalf("entry from template = %+v", tx)
	}

	// Edit: change fields, clear the amount and the account.
	var got templateDTO
	path := "/api/v1/households/" + h.ID + "/quick-templates/" + cafe.ID
	owner.expect(http.StatusOK, http.MethodPatch, path, map[string]any{
		"label": "Cafezinho", "defaultAmountCents": 0, "accountId": "", "categoryId": other.ID, "paymentMethod": "CASH", "sortOrder": 5,
	}).decode(t, &got)
	if got.Label != "Cafezinho" || got.DefaultAmountCents != nil || got.AccountID != nil || got.CategoryID != other.ID ||
		got.PaymentMethod != "CASH" || got.SortOrder != 5 || got.Kind != "EXPENSE" || got.Description != "Café da padaria" {
		t.Fatalf("edited = %+v", got)
	}
	owner.expect(http.StatusOK, http.MethodPatch, path, map[string]any{"label": "Cafezinho"}) // no change, no audit
	if got := labels(); !slices.Equal(got, []string{"Almoço", "Uber", "Cafezinho"}) {
		t.Fatalf("order after edit = %v", got)
	}

	// A template keeps working (and stays editable) after its category is archived.
	owner.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/households/"+h.ID+"/categories/"+other.ID, nil)
	owner.expect(http.StatusOK, http.MethodPatch, path, map[string]any{"label": "Café antigo"})

	owner.expect(http.StatusNoContent, http.MethodDelete, path, nil)
	owner.expect(http.StatusNotFound, http.MethodDelete, path, nil)
	owner.expect(http.StatusNotFound, http.MethodPatch, path, map[string]any{"label": "x"})
	if got := labels(); !slices.Equal(got, []string{"Almoço", "Uber"}) {
		t.Fatalf("after delete = %v", got)
	}
	// Deleting a template leaves the entries made from it alone.
	owner.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+h.ID+"/transactions/"+tx.ID, nil)

	if a := api.auditActions(cafe.ID); !slices.Equal(a, []string{"quick_template.created", "quick_template.updated", "quick_template.updated", "quick_template.deleted"}) {
		t.Fatalf("audit = %v", a)
	}
}

// I-46: a template is private to the member who created it, even inside the household.
func TestQuickTemplatesArePrivateToTheMember(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	member, _ := api.registerUser("member")
	member.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept", map[string]any{"token": owner.createInvite(h.ID).Token})
	cat := owner.firstCategory(h.ID, "EXPENSE")
	tpl := owner.createTemplate(h.ID, templateBody(cat.ID, nil))

	if l := member.templates(h.ID); len(l) != 0 {
		t.Fatalf("member sees %+v", l)
	}
	before := countRows(t, api)
	path := "/api/v1/households/" + h.ID + "/quick-templates/" + tpl.ID
	for _, r := range []response{
		member.expect(http.StatusNotFound, http.MethodPatch, path, map[string]any{"label": "Meu"}),
		member.expect(http.StatusNotFound, http.MethodDelete, path, nil),
	} {
		if code := r.errorCode(t); code != "TEMPLATE_NOT_FOUND" {
			t.Fatalf("got %s", code)
		}
	}
	if after := countRows(t, api); !maps.Equal(before, after) {
		t.Fatalf("member changed another member's template: %v -> %v", before, after)
	}
	// Each member has their own list (and their own limit).
	member.createTemplate(h.ID, templateBody(cat.ID, nil))
	if l, m := owner.templates(h.ID), member.templates(h.ID); len(l) != 1 || len(m) != 1 || l[0].ID == m[0].ID {
		t.Fatalf("owner %+v member %+v", l, m)
	}
}

func TestQuickTemplateValidation(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	acc := owner.createAccount(h.ID, nil)
	archivedAcc := owner.createAccount(h.ID, map[string]any{"name": "Arquivada"})
	owner.expect(http.StatusOK, http.MethodPatch, "/api/v1/households/"+h.ID+"/accounts/"+archivedAcc.ID, map[string]any{"isActive": false})
	archivedCat := owner.createCategory(h.ID, map[string]any{"name": "Antiga", "kind": "EXPENSE"})
	owner.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/households/"+h.ID+"/categories/"+archivedCat.ID, nil)
	tpl := owner.createTemplate(h.ID, templateBody(cat.ID, &acc.ID))

	with := func(changes map[string]any) map[string]any {
		body := templateBody(cat.ID, nil)
		for k, v := range changes {
			if v == nil {
				delete(body, k)
			} else {
				body[k] = v
			}
		}
		return body
	}
	before := countRows(t, api)
	for _, tc := range []struct {
		body   map[string]any
		status int
		code   string
	}{
		{with(map[string]any{"label": " "}), 400, "VALIDATION_ERROR"},
		{with(map[string]any{"label": strings.Repeat("x", 61)}), 400, "VALIDATION_ERROR"},
		{with(map[string]any{"kind": "TRANSFER"}), 400, "VALIDATION_ERROR"},
		{with(map[string]any{"description": nil}), 400, "VALIDATION_ERROR"},
		{with(map[string]any{"defaultAmountCents": 0}), 400, "VALIDATION_ERROR"},
		{with(map[string]any{"defaultAmountCents": 1000000000000001}), 400, "VALIDATION_ERROR"},
		{with(map[string]any{"scope": "PERSONAL"}), 400, "VALIDATION_ERROR"},
		{with(map[string]any{"paymentMethod": "CREDIT_CARD"}), 400, "VALIDATION_ERROR"},
		{with(map[string]any{"sortOrder": -1}), 400, "VALIDATION_ERROR"},
		{with(map[string]any{"splitWith": "x"}), 400, "VALIDATION_ERROR"},
		{with(map[string]any{"kind": "INCOME"}), 422, "CATEGORY_KIND_MISMATCH"},
		{with(map[string]any{"categoryId": archivedCat.ID}), 422, "CATEGORY_ARCHIVED"},
		{with(map[string]any{"accountId": archivedAcc.ID}), 422, "ACCOUNT_ARCHIVED"},
		{with(map[string]any{"accountId": uuid.NewString()}), 422, "ACCOUNT_NOT_FOUND"},
		{with(map[string]any{"scope": "PERSONAL", "personMemberId": uuid.NewString()}), 422, "PERSON_NOT_MEMBER"},
	} {
		if code := owner.expect(tc.status, http.MethodPost, "/api/v1/households/"+h.ID+"/quick-templates", tc.body).errorCode(t); code != tc.code {
			t.Fatalf("%v: got %s, want %s", tc.body, code, tc.code)
		}
	}
	path := "/api/v1/households/" + h.ID + "/quick-templates/" + tpl.ID
	for _, tc := range []struct {
		body   map[string]any
		status int
		code   string
	}{
		{map[string]any{"kind": "INCOME"}, 400, "VALIDATION_ERROR"},
		{map[string]any{"label": ""}, 400, "VALIDATION_ERROR"},
		{map[string]any{"defaultAmountCents": -5}, 400, "VALIDATION_ERROR"},
		{map[string]any{"accountId": "nope"}, 400, "VALIDATION_ERROR"},
		{map[string]any{"scope": "PERSONAL"}, 400, "VALIDATION_ERROR"},
		{map[string]any{"sortOrder": 10001}, 400, "VALIDATION_ERROR"},
		{map[string]any{"categoryId": archivedCat.ID}, 422, "CATEGORY_ARCHIVED"},
		{map[string]any{"accountId": archivedAcc.ID}, 422, "ACCOUNT_ARCHIVED"},
	} {
		if code := owner.expect(tc.status, http.MethodPatch, path, tc.body).errorCode(t); code != tc.code {
			t.Fatalf("PATCH %v: got %s, want %s", tc.body, code, tc.code)
		}
	}
	if after := countRows(t, api); !maps.Equal(before, after) {
		t.Fatalf("rejected requests wrote data: %v -> %v", before, after)
	}
}

// The per-member limit holds under concurrent creation.
func TestQuickTemplateLimit(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	cat := owner.firstCategory(h.ID, "EXPENSE")
	ctx := context.Background()
	var memberID string
	if err := api.pool.QueryRow(ctx, "SELECT id FROM household_members WHERE household_id = $1", h.ID).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := api.pool.Exec(ctx, `
		INSERT INTO quick_templates (id, household_id, member_id, label, kind, description, category_id, scope, payment_method, sort_order)
		SELECT uuidv7(), $1, $2, 'F' || g, 'EXPENSE', 'x', $3, 'FAMILY', 'PIX', g FROM generate_series(1, 46) g`,
		h.ID, memberID, cat.ID); err != nil {
		t.Fatal(err)
	}
	const n = 8
	var wg sync.WaitGroup
	statuses := make([]int, n)
	for i := range n {
		wg.Go(func() {
			statuses[i] = owner.do(http.MethodPost, "/api/v1/households/"+h.ID+"/quick-templates", templateBody(cat.ID, nil)).status
		})
	}
	wg.Wait()
	created := 0
	for _, s := range statuses {
		switch s {
		case http.StatusCreated:
			created++
		case http.StatusUnprocessableEntity:
		default:
			t.Fatalf("status %d", s)
		}
	}
	if l := owner.templates(h.ID); created != 4 || len(l) != 50 {
		t.Fatalf("created %d, total %d", created, len(l))
	}
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/households/"+h.ID+"/quick-templates",
		templateBody(cat.ID, nil)).errorCode(t); code != "TEMPLATE_LIMIT" {
		t.Fatalf("got %s", code)
	}
}

// "Repeat last": GET /transactions?createdBy=me&limit=1 returns the caller's own latest
// entry, never another member's.
func TestRepeatLastEntry(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	member, _ := api.registerUser("member")
	var joined householdDTO
	member.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept", map[string]any{"token": owner.createInvite(h.ID).Token}).decode(t, &joined)
	acc := owner.createAccount(h.ID, map[string]any{"openingBalanceCents": 100000})
	cat := owner.firstCategory(h.ID, "EXPENSE")

	last := url.Values{"createdBy": {"me"}, "limit": {"1"}}
	if got, _ := owner.transactions(h.ID, last); len(got) != 0 {
		t.Fatalf("nothing yet: %+v", got)
	}
	owner.createTransaction(h.ID, entryBody("EXPENSE", cat.ID, acc.ID, 100, "2026-11-01"))
	mine := owner.createTransaction(h.ID, entryBody("EXPENSE", cat.ID, acc.ID, 200, "2026-11-05"))
	theirs := member.createTransaction(h.ID, entryBody("EXPENSE", cat.ID, acc.ID, 300, "2026-11-09"))
	voided := owner.createTransaction(h.ID, entryBody("EXPENSE", cat.ID, acc.ID, 400, "2026-11-10"))
	owner.voidTransaction(h.ID, voided.ID)

	if got, next := owner.transactions(h.ID, last); len(got) != 1 || got[0].ID != mine.ID || next == nil {
		t.Fatalf("owner's last = %+v", got)
	}
	if got, _ := member.transactions(h.ID, last); len(got) != 1 || got[0].ID != theirs.ID {
		t.Fatalf("member's last = %+v", got)
	}
	// Any member can filter by author (every member sees every entry, RN-002).
	if got, _ := owner.transactions(h.ID, url.Values{"createdBy": {joined.MemberID}}); len(got) != 1 || got[0].ID != theirs.ID {
		t.Fatalf("by author = %+v", got)
	}
	owner.expect(http.StatusBadRequest, http.MethodGet, "/api/v1/households/"+h.ID+"/transactions?createdBy=you", nil)
}
