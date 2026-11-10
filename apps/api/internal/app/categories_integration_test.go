//go:build integration

package app

import (
	"net/http"
	"slices"
	"testing"
)

type categoryDTO struct {
	ID        string  `json:"id"`
	ParentID  *string `json:"parentId"`
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	Color     *string `json:"color"`
	IsActive  bool    `json:"isActive"`
	SortOrder int     `json:"sortOrder"`
}

func (c *client) categories(householdID, query string) []categoryDTO {
	c.api.t.Helper()
	var list struct{ Items []categoryDTO }
	c.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+householdID+"/categories"+query, nil).decode(c.api.t, &list)
	return list.Items
}

// firstCategory returns the first active top-level category of a kind (from the defaults).
func (c *client) firstCategory(householdID, kind string) categoryDTO {
	c.api.t.Helper()
	for _, cat := range c.categories(householdID, "?kind="+kind) {
		if cat.ParentID == nil {
			return cat
		}
	}
	c.api.t.Fatalf("no %s category", kind)
	return categoryDTO{}
}

func byName(list []categoryDTO, name string) (categoryDTO, bool) {
	i := slices.IndexFunc(list, func(c categoryDTO) bool { return c.Name == name })
	if i < 0 {
		return categoryDTO{}, false
	}
	return list[i], true
}

func TestNewHouseholdGetsDefaultCategories(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")

	all := owner.categories(h.ID, "")
	food, ok := byName(all, "Alimentação")
	if !ok || food.Kind != "EXPENSE" || food.ParentID != nil {
		t.Fatalf("Alimentação = %+v (found %v)", food, ok)
	}
	market, ok := byName(all, "Mercado")
	if !ok || market.ParentID == nil || *market.ParentID != food.ID {
		t.Fatalf("Mercado must be a child of Alimentação: %+v", market)
	}
	if salary, ok := byName(all, "Salário"); !ok || salary.Kind != "INCOME" {
		t.Fatalf("Salário = %+v", salary)
	}

	for _, c := range owner.categories(h.ID, "?kind=INCOME") {
		if c.Kind != "INCOME" {
			t.Fatalf("kind filter leaked %+v", c)
		}
	}
	owner.expect(http.StatusBadRequest, http.MethodGet, "/api/v1/households/"+h.ID+"/categories?kind=OTHER", nil)

	// Defaults are copied per household: a second household has its own rows.
	other := owner.createHousehold("Casa 2")
	otherFood, _ := byName(owner.categories(other.ID, ""), "Alimentação")
	if otherFood.ID == food.ID {
		t.Fatal("households must not share category rows")
	}
}

func TestCategoryRules(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	base := "/api/v1/households/" + h.ID + "/categories"
	expense := owner.firstCategory(h.ID, "EXPENSE")
	income := owner.firstCategory(h.ID, "INCOME")

	var top categoryDTO
	owner.expect(http.StatusCreated, http.MethodPost, base,
		map[string]any{"name": "  Academia  ", "kind": "EXPENSE", "color": "#1A2b3C", "sortOrder": 5}).decode(t, &top)
	if top.Name != "Academia" || top.ParentID != nil || *top.Color != "#1A2b3C" || !top.IsActive {
		t.Fatalf("top = %+v", top)
	}
	var child categoryDTO
	owner.expect(http.StatusCreated, http.MethodPost, base,
		map[string]any{"name": "Mensalidade", "kind": "EXPENSE", "parentId": top.ID}).decode(t, &child)
	if child.ParentID == nil || *child.ParentID != top.ID {
		t.Fatalf("child = %+v", child)
	}

	for _, tc := range []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"third level", map[string]any{"name": "Neta", "kind": "EXPENSE", "parentId": child.ID}, 422, "MAX_DEPTH"},
		{"kind differs from parent", map[string]any{"name": "Bônus", "kind": "INCOME", "parentId": expense.ID}, 422, "KIND_MISMATCH"},
		{"unknown parent", map[string]any{"name": "X", "kind": "EXPENSE", "parentId": "0190f0a0-0000-7000-8000-000000000009"}, 422, "PARENT_NOT_FOUND"},
		{"duplicate sibling name (case-insensitive)", map[string]any{"name": "academia", "kind": "EXPENSE"}, 409, "CATEGORY_NAME_TAKEN"},
		{"invalid kind", map[string]any{"name": "X", "kind": "OTHER"}, 400, "VALIDATION_ERROR"},
		{"invalid color", map[string]any{"name": "X", "kind": "EXPENSE", "color": "red"}, 400, "VALIDATION_ERROR"},
		{"empty name", map[string]any{"name": "   ", "kind": "EXPENSE"}, 400, "VALIDATION_ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := owner.expect(tc.status, http.MethodPost, base, tc.body).errorCode(t); code != tc.code {
				t.Fatalf("got %s, want %s", code, tc.code)
			}
		})
	}
	// Same name is fine under another kind or another parent.
	owner.expect(http.StatusCreated, http.MethodPost, base, map[string]any{"name": "Academia", "kind": "INCOME"})
	owner.expect(http.StatusCreated, http.MethodPost, base, map[string]any{"name": "Mensalidade", "kind": "INCOME", "parentId": income.ID})

	// PATCH: rename, recolor, clear color; kind cannot be changed (unknown field).
	var renamed categoryDTO
	owner.expect(http.StatusOK, http.MethodPatch, base+"/"+top.ID, map[string]any{"name": "Esportes", "color": ""}).decode(t, &renamed)
	if renamed.Name != "Esportes" || renamed.Color != nil || renamed.SortOrder != 5 {
		t.Fatalf("renamed = %+v", renamed)
	}
	owner.expect(http.StatusBadRequest, http.MethodPatch, base+"/"+top.ID, map[string]any{"kind": "INCOME"})
	owner.expect(http.StatusNotFound, http.MethodPatch, base+"/0190f0a0-0000-7000-8000-000000000009", map[string]any{"name": "x"})
}

func TestArchiveAndRestoreCategories(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	base := "/api/v1/households/" + h.ID + "/categories"

	all := owner.categories(h.ID, "")
	food, _ := byName(all, "Alimentação")
	market, _ := byName(all, "Mercado")

	// Archiving a parent archives its children; DELETE never deletes (RN-022) and is idempotent.
	owner.expect(http.StatusNoContent, http.MethodDelete, base+"/"+food.ID, nil)
	owner.expect(http.StatusNoContent, http.MethodDelete, base+"/"+food.ID, nil)
	if _, ok := byName(owner.categories(h.ID, ""), "Mercado"); ok {
		t.Fatal("archived child must be hidden by default")
	}
	withArchived := owner.categories(h.ID, "?includeArchived=true")
	if m, ok := byName(withArchived, "Mercado"); !ok || m.IsActive {
		t.Fatalf("archived child must still exist as inactive: %+v", m)
	}

	// Cannot create under, or restore a child of, an archived parent.
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, base,
		map[string]any{"name": "Feira", "kind": "EXPENSE", "parentId": food.ID}).errorCode(t); code != "PARENT_ARCHIVED" {
		t.Fatalf("got %s", code)
	}
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPatch, base+"/"+market.ID,
		map[string]any{"isActive": true}).errorCode(t); code != "PARENT_ARCHIVED" {
		t.Fatalf("got %s", code)
	}

	// An archived name can be reused by a new active sibling, but then the old one cannot be
	// restored with the same name.
	var newFood categoryDTO
	owner.expect(http.StatusCreated, http.MethodPost, base, map[string]any{"name": "Alimentação", "kind": "EXPENSE"}).decode(t, &newFood)
	owner.expect(http.StatusConflict, http.MethodPatch, base+"/"+food.ID, map[string]any{"isActive": true})
	owner.expect(http.StatusNoContent, http.MethodDelete, base+"/"+newFood.ID, nil)

	// Restore parent, then child.
	owner.expect(http.StatusOK, http.MethodPatch, base+"/"+food.ID, map[string]any{"isActive": true})
	owner.expect(http.StatusOK, http.MethodPatch, base+"/"+market.ID, map[string]any{"isActive": true})
	if _, ok := byName(owner.categories(h.ID, ""), "Mercado"); !ok {
		t.Fatal("restored child must be listed")
	}
}

func TestMembersCanManageCategories(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	member, _ := api.registerUser("member")
	member.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept", map[string]any{"token": owner.createInvite(h.ID).Token})

	var c categoryDTO
	member.expect(http.StatusCreated, http.MethodPost, "/api/v1/households/"+h.ID+"/categories",
		map[string]any{"name": "Farmácia veterinária", "kind": "EXPENSE"}).decode(t, &c)
	if _, ok := byName(owner.categories(h.ID, ""), "Farmácia veterinária"); !ok {
		t.Fatal("categories are shared by the whole household")
	}
}
