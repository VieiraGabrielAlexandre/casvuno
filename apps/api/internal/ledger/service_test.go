package ledger

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
)

func validInput() CreateInput {
	acc := uuid.New()
	return CreateInput{
		Kind: KindExpense, Scope: ScopeFamily, Description: "Mercado", AmountCents: 30000,
		CategoryID: uuid.New(), OccurredOn: time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC),
		Payment: Payment{Method: "PIX", AccountID: &acc, Mode: ModeImmediate},
	}
}

func ptr(s string) *string { return &s }

func TestNormalizeTrimsTexts(t *testing.T) {
	in := validInput()
	in.Description, in.Merchant, in.Notes = "  Mercado  ", ptr(" Feira "), ptr("   ")
	out, err := normalize(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Description != "Mercado" || out.Merchant == nil || *out.Merchant != "Feira" || out.Notes != nil {
		t.Fatalf("got %q %v %v", out.Description, out.Merchant, out.Notes)
	}
}

func TestNormalizeRejects(t *testing.T) {
	person := uuid.New()
	for _, tc := range []struct {
		name  string
		edit  func(*CreateInput)
		field string
	}{
		{"unknown kind", func(in *CreateInput) { in.Kind = "TRANSFER" }, "kind"},
		{"unknown scope", func(in *CreateInput) { in.Scope = "SHARED" }, "scope"},
		{"personal without person", func(in *CreateInput) { in.Scope = ScopePersonal }, "personMemberId"},
		{"zero amount", func(in *CreateInput) { in.AmountCents = 0 }, "amountCents"},
		{"negative amount", func(in *CreateInput) { in.AmountCents = -1 }, "amountCents"},
		{"amount over the limit", func(in *CreateInput) { in.AmountCents = kernel.MaxAbsCents + 1 }, "amountCents"},
		{"blank description", func(in *CreateInput) { in.Description = " \t " }, "description"},
		{"201-character description", func(in *CreateInput) { in.Description = strings.Repeat("ç", 201) }, "description"},
		{"long merchant", func(in *CreateInput) { in.Merchant = ptr(strings.Repeat("a", 121)) }, "merchant"},
		{"long notes", func(in *CreateInput) { in.Notes = ptr(strings.Repeat("a", 1001)) }, "notes"},
		{"planned mode (F3.2)", func(in *CreateInput) { in.Payment.Mode = "PLANNED" }, "payment.mode"},
		{"credit card (F4.3)", func(in *CreateInput) { in.Payment.Method = "CREDIT_CARD" }, "payment.method"},
		{"missing account", func(in *CreateInput) { in.Payment.AccountID = nil }, "payment.accountId"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput()
			tc.edit(&in)
			_, err := normalize(in)
			var kerr *kernel.Error
			if !errors.As(err, &kerr) || !errors.Is(err, kernel.ErrValidation) {
				t.Fatalf("err = %v, want a validation error", err)
			}
			if len(kerr.Fields) != 1 || kerr.Fields[0].Field != tc.field {
				t.Fatalf("fields = %+v, want %s", kerr.Fields, tc.field)
			}
		})
	}

	// Boundaries that must pass: 200 characters, the maximum amount, a personal entry with its person.
	in := validInput()
	in.Description, in.AmountCents = strings.Repeat("ç", 200), kernel.MaxAbsCents
	in.Scope, in.PersonMemberID = ScopePersonal, &person
	if _, err := normalize(in); err != nil {
		t.Fatalf("boundaries rejected: %v", err)
	}
}

func TestObligationKindMirrorsTheEntry(t *testing.T) {
	if obligationKind(KindExpense) != "PAYABLE" || obligationKind(KindIncome) != "RECEIVABLE" {
		t.Fatal("expense must be payable and income receivable")
	}
}
