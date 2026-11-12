package ledger

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

func planned(in *CreateInput) {
	due := in.OccurredOn.AddDate(0, 0, 10)
	in.Payment.Mode, in.Payment.DueOn = ModePlanned, &due
}

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
		{"unknown mode", func(in *CreateInput) { in.Payment.Mode = "LATER" }, "payment.mode"},
		{"immediate with due date", func(in *CreateInput) { d := in.OccurredOn; in.Payment.DueOn = &d }, "payment.dueOn"},
		{"planned without due date", func(in *CreateInput) { in.Payment.Mode = ModePlanned }, "payment.dueOn"},
		{"planned with merchant", func(in *CreateInput) { planned(in); in.Merchant = ptr("Feira") }, "merchant"},
		{"planned with notes", func(in *CreateInput) { planned(in); in.Notes = ptr("x") }, "notes"},
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

func TestNormalizeAcceptsAForecastWithoutAccount(t *testing.T) {
	in := validInput()
	planned(&in)
	in.Payment.AccountID = nil
	if _, err := normalize(in); err != nil {
		t.Fatalf("forecast without account rejected: %v", err)
	}
}

func TestDerivedPaymentSituation(t *testing.T) {
	today := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	o := Obligation{Status: StatusPlanned, AmountCents: 1000, DueOn: today.AddDate(0, 0, -1)}
	if o.PaymentStatus() != PaymentUnpaid || o.OutstandingCents() != 1000 || !o.IsOverdue(today) {
		t.Fatalf("unpaid: %s %d %v", o.PaymentStatus(), o.OutstandingCents(), o.IsOverdue(today))
	}
	o.Status, o.PaidCents = StatusConfirmed, 400
	if o.PaymentStatus() != PaymentPartiallyPaid || o.OutstandingCents() != 600 || !o.IsOverdue(today) {
		t.Fatal("partially paid and still overdue")
	}
	o.PaidCents = 1000
	if o.PaymentStatus() != PaymentPaid || o.OutstandingCents() != 0 || o.IsOverdue(today) {
		t.Fatal("paid is never overdue")
	}
	due := Obligation{Status: StatusPlanned, AmountCents: 1000, DueOn: today}
	if due.IsOverdue(today) {
		t.Fatal("due today is not overdue yet")
	}
	cancelled := Obligation{Status: StatusCancelled, AmountCents: 1000, DueOn: today.AddDate(0, -1, 0)}
	if cancelled.OutstandingCents() != 0 || cancelled.IsOverdue(today) {
		t.Fatal("cancelled is neither outstanding nor overdue")
	}
}

func TestObligationKindMirrorsTheEntry(t *testing.T) {
	if obligationKind(KindExpense) != "PAYABLE" || obligationKind(KindIncome) != "RECEIVABLE" {
		t.Fatal("expense must be payable and income receivable")
	}
	if transactionKind("PAYABLE") != KindExpense || transactionKind("RECEIVABLE") != KindIncome {
		t.Fatal("a confirmed forecast keeps its kind")
	}
}

// The reversal is named after the entry and always fits the movement description limit,
// cutting by characters (never in the middle of a multi-byte rune).
func TestReversalDescription(t *testing.T) {
	if got := reversalDescription("Café"); got != "Estorno: Café" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("é", maxDescriptionLen)
	got := reversalDescription(long)
	if n := utf8.RuneCountInString(got); n != maxDescriptionLen || !utf8.ValidString(got) || !strings.HasPrefix(got, "Estorno: ") {
		t.Fatalf("len %d, valid %v: %q", n, utf8.ValidString(got), got)
	}
}
