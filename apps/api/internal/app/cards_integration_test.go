//go:build integration

package app

import (
	"context"
	"net/http"
	"slices"
	"testing"
)

type cardDTO struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	HolderMemberID string  `json:"holderMemberId"`
	LastFour       *string `json:"lastFour"`
	LimitCents     int64   `json:"limitCents"`
	ClosingDay     int     `json:"closingDay"`
	DueDay         int     `json:"dueDay"`
	IsActive       bool    `json:"isActive"`
}

// cardBody is a card held by holderMemberID; fields override the defaults.
func cardBody(holderMemberID string, fields map[string]any) map[string]any {
	body := map[string]any{"name": "Nubank", "holderMemberId": holderMemberID, "limitCents": 500000, "closingDay": 3, "dueDay": 10}
	for k, v := range fields {
		body[k] = v
	}
	return body
}

func (c *client) createCard(householdID string, body map[string]any) cardDTO {
	c.api.t.Helper()
	var card cardDTO
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/households/"+householdID+"/cards", body).decode(c.api.t, &card)
	return card
}

func (c *client) cards(householdID, query string) []cardDTO {
	c.api.t.Helper()
	var l struct{ Items []cardDTO }
	c.expect(http.StatusOK, http.MethodGet, "/api/v1/households/"+householdID+"/cards"+query, nil).decode(c.api.t, &l)
	return l.Items
}

func TestCardsCRUD(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	member, _ := api.registerUser("member")
	var joined householdDTO
	member.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept", map[string]any{"token": owner.createInvite(h.ID).Token}).decode(t, &joined)
	base := "/api/v1/households/" + h.ID + "/cards"

	nubank := owner.createCard(h.ID, cardBody(h.MemberID, map[string]any{"lastFour": "1234"}))
	if nubank.Name != "Nubank" || nubank.LastFour == nil || *nubank.LastFour != "1234" || nubank.LimitCents != 500000 ||
		nubank.ClosingDay != 3 || nubank.DueDay != 10 || !nubank.IsActive || nubank.HolderMemberID != h.MemberID {
		t.Fatalf("created %+v", nubank)
	}
	// Any member registers cards, held by any active member; the digits are optional.
	inter := member.createCard(h.ID, cardBody(joined.MemberID, map[string]any{"name": "Inter", "closingDay": 31, "dueDay": 31}))
	if inter.LastFour != nil {
		t.Fatalf("lastFour = %v", *inter.LastFour)
	}
	// Every member sees every card (RN-002), active first by name.
	for _, c := range []*client{owner, member} {
		if l := c.cards(h.ID, ""); len(l) != 2 || l[0].ID != inter.ID || l[1].ID != nubank.ID {
			t.Fatalf("list = %+v", l)
		}
	}
	var got cardDTO
	member.expect(http.StatusOK, http.MethodGet, base+"/"+nubank.ID, nil).decode(t, &got)
	if got.ID != nubank.ID {
		t.Fatalf("get = %+v", got)
	}

	// Validation: days 1..31, limit > 0, exactly four digits (never the full number).
	for _, tc := range []struct {
		fields map[string]any
		field  string
	}{
		{map[string]any{"closingDay": 0}, "closingDay"},
		{map[string]any{"closingDay": 32}, "closingDay"},
		{map[string]any{"dueDay": 0}, "dueDay"},
		{map[string]any{"dueDay": 32}, "dueDay"},
		{map[string]any{"limitCents": 0}, "limitCents"},
		{map[string]any{"limitCents": -1}, "limitCents"},
		{map[string]any{"lastFour": "5502093412341234"}, "lastFour"},
		{map[string]any{"lastFour": "12a4"}, "lastFour"},
		{map[string]any{"lastFour": ""}, "lastFour"},
		{map[string]any{"name": "  "}, "name"},
	} {
		r := owner.expect(http.StatusBadRequest, http.MethodPost, base, cardBody(h.MemberID, tc.fields))
		if !slices.Contains(r.errorFields(t), tc.field) {
			t.Errorf("%v: %s", tc.fields, r.body)
		}
	}
	// Unknown fields (e.g. a CVV) are rejected by the strict decoder.
	owner.expect(http.StatusBadRequest, http.MethodPost, base, cardBody(h.MemberID, map[string]any{"name": "X", "cvv": "123"}))
	// Holder must be an active member.
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, base,
		cardBody("00000000-0000-4000-8000-000000000000", map[string]any{"name": "X"})).errorCode(t); code != "HOLDER_NOT_MEMBER" {
		t.Fatalf("got %s", code)
	}
	// Active names are unique (case-insensitive).
	if code := owner.expect(http.StatusConflict, http.MethodPost, base, cardBody(h.MemberID, map[string]any{"name": "NUBANK"})).errorCode(t); code != "CARD_NAME_TAKEN" {
		t.Fatalf("got %s", code)
	}

	// Edit: changed fields only; "" clears the digits; new days are accepted (future invoices).
	member.expect(http.StatusOK, http.MethodPatch, base+"/"+nubank.ID, map[string]any{
		"name": "Nubank Ana", "lastFour": "", "limitCents": 800000, "closingDay": 25, "dueDay": 5, "holderMemberId": joined.MemberID,
	}).decode(t, &got)
	if got.Name != "Nubank Ana" || got.LastFour != nil || got.LimitCents != 800000 || got.ClosingDay != 25 || got.DueDay != 5 ||
		got.HolderMemberID != joined.MemberID {
		t.Fatalf("updated %+v", got)
	}
	owner.expect(http.StatusBadRequest, http.MethodPatch, base+"/"+nubank.ID, map[string]any{"dueDay": 40})
	owner.expect(http.StatusBadRequest, http.MethodPatch, base+"/"+nubank.ID, map[string]any{"lastFour": "123"})
	if code := owner.expect(http.StatusConflict, http.MethodPatch, base+"/"+nubank.ID, map[string]any{"name": "inter"}).errorCode(t); code != "CARD_NAME_TAKEN" {
		t.Fatalf("got %s", code)
	}

	// Archive: hidden from the default list; only restoring is accepted afterwards.
	owner.expect(http.StatusOK, http.MethodPatch, base+"/"+inter.ID, map[string]any{"isActive": false}).decode(t, &got)
	if got.IsActive {
		t.Fatal("not archived")
	}
	if l := owner.cards(h.ID, ""); len(l) != 1 || l[0].ID != nubank.ID {
		t.Fatalf("list = %+v", l)
	}
	if l := owner.cards(h.ID, "?includeArchived=true"); len(l) != 2 || l[1].ID != inter.ID {
		t.Fatalf("list with archived = %+v", l)
	}
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPatch, base+"/"+inter.ID, map[string]any{"name": "Y"}).errorCode(t); code != "CARD_ARCHIVED" {
		t.Fatalf("got %s", code)
	}
	// An archived name can be reused; restoring then collides with the new active card.
	owner.createCard(h.ID, cardBody(h.MemberID, map[string]any{"name": "Inter"}))
	if code := owner.expect(http.StatusConflict, http.MethodPatch, base+"/"+inter.ID, map[string]any{"isActive": true}).errorCode(t); code != "CARD_NAME_TAKEN" {
		t.Fatalf("got %s", code)
	}
	owner.expect(http.StatusOK, http.MethodPatch, base+"/"+inter.ID, map[string]any{"isActive": true, "name": "Inter antigo"}).decode(t, &got)
	if !got.IsActive || got.Name != "Inter antigo" {
		t.Fatalf("restored %+v", got)
	}

	// A member who left can no longer become holder; existing cards keep their holder.
	owner.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/households/"+h.ID+"/members/"+joined.MemberID, nil)
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPatch, base+"/"+inter.ID,
		map[string]any{"holderMemberId": joined.MemberID}).errorCode(t); code != "HOLDER_NOT_MEMBER" {
		t.Fatalf("got %s", code)
	}
	owner.expect(http.StatusOK, http.MethodPatch, base+"/"+nubank.ID, map[string]any{"limitCents": 900000})

	if a := api.auditActions(inter.ID); !slices.Equal(a, []string{
		"credit_card.created", "credit_card.archived", "credit_card.restored",
	}) {
		t.Fatalf("audit = %v", a)
	}
	// The audit trail names the changed fields, never the digits or the limit.
	var leaked int
	if err := api.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE entity_type = 'credit_card' AND (metadata::text LIKE '%1234%' OR metadata::text LIKE '%800000%')`,
	).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatalf("audit metadata leaked card data: %d %v", leaked, err)
	}
	assertInvariants(t)
}

// The household card list is bounded (no pagination).
func TestCardLimitPerHousehold(t *testing.T) {
	api := newTestAPI(t)
	owner, _ := api.registerUser("owner")
	h := owner.createHousehold("Casa")
	for i := range 50 {
		owner.createCard(h.ID, cardBody(h.MemberID, map[string]any{"name": "Cartão " + string(rune('A'+i/26)) + string(rune('a'+i%26))}))
	}
	if code := owner.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/households/"+h.ID+"/cards",
		cardBody(h.MemberID, map[string]any{"name": "Excedente"})).errorCode(t); code != "CARD_LIMIT" {
		t.Fatalf("got %s", code)
	}
}

// errorFields returns the fields named in an error envelope's details.
func (r response) errorFields(t *testing.T) []string {
	t.Helper()
	var env struct {
		Error struct {
			Details []struct {
				Field string `json:"field"`
			} `json:"details"`
		} `json:"error"`
	}
	r.decode(t, &env)
	out := make([]string, 0, len(env.Error.Details))
	for _, d := range env.Error.Details {
		out = append(out, d.Field)
	}
	return out
}
