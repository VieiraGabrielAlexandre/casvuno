package ledger

import (
	"testing"

	"github.com/google/uuid"
)

func baseTemplate() QuickTemplate {
	amount := int64(1250)
	acc := uuid.New()
	return QuickTemplate{
		Label: "Café", Kind: KindExpense, Description: "Café", DefaultAmountCents: &amount,
		CategoryID: uuid.New(), Scope: ScopeFamily, PaymentMethod: "PIX", AccountID: &acc, SortOrder: 3,
	}
}

func TestApplyTemplateUpdate(t *testing.T) {
	cur := baseTemplate()
	zero, person := int64(0), uuid.NewString()
	empty, personal, label := "", ScopePersonal, "  Cafezinho "
	next, err := applyTemplateUpdate(cur, TemplateUpdate{
		Label: &label, DefaultAmountCents: &zero, AccountID: &empty, Scope: &personal, PersonMemberID: &person,
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.Label != "Cafezinho" || next.DefaultAmountCents != nil || next.AccountID != nil || next.PersonMemberID == nil ||
		next.Description != cur.Description || next.SortOrder != cur.SortOrder {
		t.Fatalf("next = %+v", next)
	}
	changed := templateChanges(cur, next)
	want := []string{"label", "defaultAmountCents", "scope", "personMemberId", "accountId"}
	if len(changed) != len(want) {
		t.Fatalf("changed = %v, want %v", changed, want)
	}
	for i := range want {
		if changed[i] != want[i] {
			t.Fatalf("changed = %v, want %v", changed, want)
		}
	}
	if c := templateChanges(cur, cur); len(c) != 0 {
		t.Fatalf("no-op changed %v", c)
	}
}

func TestTemplateValidationRules(t *testing.T) {
	neg, big, bad, badID := int64(-1), int64(1_000_000_000_000_001), int32(10001), "x"
	personal, card, blank := ScopePersonal, "CREDIT_CARD", " "
	for name, in := range map[string]TemplateUpdate{
		"negative amount":         {DefaultAmountCents: &neg},
		"amount above the limit":  {DefaultAmountCents: &big},
		"personal without person": {Scope: &personal},
		"card method":             {PaymentMethod: &card},
		"sort order":              {SortOrder: &bad},
		"blank label":             {Label: &blank},
		"invalid account id":      {AccountID: &badID},
	} {
		if _, err := applyTemplateUpdate(baseTemplate(), in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
