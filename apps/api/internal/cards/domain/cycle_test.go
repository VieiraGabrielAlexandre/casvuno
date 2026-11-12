package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
)

func day(s string) time.Time {
	d, err := kernel.ParseDate(s)
	if err != nil {
		panic(err) // test helper: literals only
	}
	return d
}

func period(start, closing, due string) Period {
	return Period{StartOn: day(start), ClosingOn: day(closing), DueOn: day(due)}
}

func cycle(t *testing.T, closingDay, dueDay int) Cycle {
	t.Helper()
	c, err := NewCycle(closingDay, dueDay)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewCycleValidatesDays(t *testing.T) {
	for _, d := range [][2]int{{0, 10}, {32, 10}, {10, 0}, {10, 32}} {
		if _, err := NewCycle(d[0], d[1]); !errors.Is(err, kernel.ErrValidation) {
			t.Errorf("NewCycle(%d, %d) = %v; want validation error", d[0], d[1], err)
		}
	}
	if _, err := NewCycle(31, 1); err != nil {
		t.Fatal(err)
	}
}

// The ADR 0003 table: every case is (closing day, due day, purchase date) → invoice.
func TestPeriodFor(t *testing.T) {
	cases := []struct {
		name         string
		closing, due int
		purchase     string
		want         Period
	}{
		{"closing 31 in February, non-leap", 31, 10, "2027-02-10", period("2027-01-31", "2027-02-28", "2027-03-10")},
		{"closing 31 in February, leap", 31, 10, "2028-02-10", period("2028-01-31", "2028-02-29", "2028-03-10")},
		{"closing 31 in April", 31, 10, "2027-04-15", period("2027-03-31", "2027-04-30", "2027-05-10")},
		{"closing 31 on Dec 31 goes to January", 31, 10, "2026-12-31", period("2026-12-31", "2027-01-31", "2027-02-10")},
		{"due 31 after closing 31 in February", 31, 31, "2027-02-01", period("2027-01-31", "2027-02-28", "2027-03-31")},
		{"closing == due: due next month", 10, 10, "2026-11-05", period("2026-10-10", "2026-11-10", "2026-12-10")},
		{"due < closing: due next month", 25, 5, "2026-11-01", period("2026-10-25", "2026-11-25", "2026-12-05")},
		{"due > closing: same month", 3, 10, "2026-11-01", period("2026-10-03", "2026-11-03", "2026-11-10")},
		{"due > closing, Dec closing: due in December", 3, 10, "2026-12-02", period("2026-11-03", "2026-12-03", "2026-12-10")},
		{"due < closing in December: due in January", 25, 5, "2026-12-20", period("2026-11-25", "2026-12-25", "2027-01-05")},
		{"purchase on closing − 1", 10, 20, "2026-11-09", period("2026-10-10", "2026-11-10", "2026-11-20")},
		{"T14: purchase on the closing day goes to the next invoice", 10, 20, "2026-11-10", period("2026-11-10", "2026-12-10", "2026-12-20")},
		{"purchase on closing + 1", 10, 20, "2026-11-11", period("2026-11-10", "2026-12-10", "2026-12-20")},
		{"T13: Feb 28 of a leap year, closing 29", 29, 5, "2028-02-28", period("2028-01-29", "2028-02-29", "2028-03-05")},
		{"T13: Feb 29 of a leap year, closing 29", 29, 5, "2028-02-29", period("2028-02-29", "2028-03-29", "2028-04-05")},
		{"T13: Feb 28 of a leap year, closing 28", 28, 5, "2028-02-28", period("2028-02-28", "2028-03-28", "2028-04-05")},
		{"T13: Feb 29 of a leap year, closing 31", 31, 10, "2028-02-29", period("2028-02-29", "2028-03-31", "2028-04-10")},
		{"T13: Feb 28 of a non-leap year, closing 30", 30, 10, "2027-02-28", period("2027-02-28", "2027-03-30", "2027-04-10")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cycle(t, c.closing, c.due).PeriodFor(day(c.purchase))
			if got != c.want {
				t.Fatalf("got %s→%s due %s; want %s→%s due %s",
					kernel.FormatDate(got.StartOn), kernel.FormatDate(got.ClosingOn), kernel.FormatDate(got.DueOn),
					kernel.FormatDate(c.want.StartOn), kernel.FormatDate(c.want.ClosingOn), kernel.FormatDate(c.want.DueOn))
			}
		})
	}
}

func TestAllocateTwelveInstallmentsAcrossTheYear(t *testing.T) {
	c := cycle(t, 10, 20)
	a, err := c.Allocate(nil, day("2026-11-15"), 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Periods) != 12 || len(a.Create) != 12 || a.Shifted {
		t.Fatalf("got %d periods, %d to create, shifted %v", len(a.Periods), len(a.Create), a.Shifted)
	}
	if a.Periods[0] != period("2026-11-10", "2026-12-10", "2026-12-20") ||
		a.Periods[1] != period("2026-12-10", "2027-01-10", "2027-01-20") ||
		a.Periods[11] != period("2027-10-10", "2027-11-10", "2027-11-20") {
		t.Fatalf("unexpected periods %v", a.Periods)
	}
	for i := 1; i < 12; i++ {
		if !a.Periods[i].StartOn.Equal(a.Periods[i-1].ClosingOn) {
			t.Fatalf("installment %d is not chained", i+1)
		}
	}
}

func TestAllocateReusesExistingInvoicesAndFillsTheChain(t *testing.T) {
	c := cycle(t, 10, 20)
	existing := []Invoice{
		{Period: period("2026-10-10", "2026-11-10", "2026-11-20")},
		{Period: period("2026-11-10", "2026-12-10", "2026-12-20")},
	}
	a, err := c.Allocate(existing, day("2026-11-10"), 1)
	if err != nil || len(a.Create) != 0 || a.Periods[0] != existing[1].Period {
		t.Fatalf("got %+v, %v", a, err)
	}
	// A purchase three cycles after the last invoice creates the skipped ones too.
	a, err = c.Allocate(existing, day("2027-02-15"), 2)
	if err != nil {
		t.Fatal(err)
	}
	wantCreate := []Period{
		period("2026-12-10", "2027-01-10", "2027-01-20"),
		period("2027-01-10", "2027-02-10", "2027-02-20"),
		period("2027-02-10", "2027-03-10", "2027-03-20"),
		period("2027-03-10", "2027-04-10", "2027-04-20"),
	}
	if len(a.Create) != len(wantCreate) || a.Periods[0] != wantCreate[2] || a.Periods[1] != wantCreate[3] {
		t.Fatalf("got %+v", a)
	}
	for i := range wantCreate {
		if a.Create[i] != wantCreate[i] {
			t.Fatalf("create[%d] = %v; want %v", i, a.Create[i], wantCreate[i])
		}
	}
}

func TestAllocateShiftsPastClosedInvoices(t *testing.T) {
	c := cycle(t, 10, 20)
	existing := []Invoice{
		{Period: period("2026-10-10", "2026-11-10", "2026-11-20"), Closed: true},
		{Period: period("2026-11-10", "2026-12-10", "2026-12-20")},
	}
	// Late purchase dated inside the CLOSED invoice: installment 1 goes to the next open one.
	a, err := c.Allocate(existing, day("2026-11-05"), 3)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Shifted || a.Periods[0] != existing[1].Period ||
		a.Periods[1] != period("2026-12-10", "2027-01-10", "2027-01-20") ||
		a.Periods[2] != period("2027-01-10", "2027-02-10", "2027-02-20") || len(a.Create) != 2 {
		t.Fatalf("got %+v", a)
	}
	// An open invoice followed by a CLOSED one: the closed one is skipped as well.
	existing[0].Closed, existing[1].Closed = false, true
	a, err = c.Allocate(existing, day("2026-11-05"), 2)
	if err != nil || !a.Shifted || a.Periods[0] != existing[0].Period ||
		a.Periods[1] != period("2026-12-10", "2027-01-10", "2027-01-20") {
		t.Fatalf("got %+v, %v", a, err)
	}
	// No CLOSED invoice on the way: not shifted.
	if a, err = c.Allocate(existing, day("2026-10-20"), 1); err != nil || a.Shifted {
		t.Fatalf("got %+v, %v", a, err)
	}
}

// Changing the card's days affects only invoices not created yet (ADR 0003).
func TestAllocateAfterClosingDayChange(t *testing.T) {
	existing := []Invoice{
		{Period: period("2026-10-10", "2026-11-10", "2026-11-20")},
		{Period: period("2026-11-10", "2026-12-10", "2026-12-20")},
	}
	c := cycle(t, 25, 5) // was closing 10 / due 20

	// Existing invoices keep their dates.
	a, err := c.Allocate(existing, day("2026-12-01"), 1)
	if err != nil || a.Periods[0] != existing[1].Period {
		t.Fatalf("got %+v, %v", a, err)
	}
	// The next one chains on the last closing (a short transition cycle), then the new rule.
	a, err = c.Allocate(existing, day("2026-12-15"), 2)
	if err != nil {
		t.Fatal(err)
	}
	if a.Periods[0] != period("2026-12-10", "2026-12-25", "2027-01-05") ||
		a.Periods[1] != period("2026-12-25", "2027-01-25", "2027-02-05") {
		t.Fatalf("got %+v", a.Periods)
	}
	// Before the first invoice the current rule decides; overlapping it is rejected.
	if _, err := c.Allocate(existing, day("2026-10-01"), 1); !isCode(err, "CARD_CYCLE_OVERLAP") {
		t.Fatalf("overlap accepted: %v", err)
	}
	a, err = c.Allocate(existing, day("2026-09-01"), 1)
	if err != nil || a.Periods[0] != period("2026-08-25", "2026-09-25", "2026-10-05") {
		t.Fatalf("got %+v, %v", a, err)
	}
	// ...but its second installment would overlap the first existing invoice.
	if _, err := c.Allocate(existing, day("2026-09-01"), 2); !isCode(err, "CARD_CYCLE_OVERLAP") {
		t.Fatalf("overlap accepted: %v", err)
	}
}

func TestAllocateRejects(t *testing.T) {
	c := cycle(t, 10, 20)
	if _, err := c.Allocate(nil, day("2026-11-01"), 0); !isCode(err, "INVALID_INSTALLMENTS") {
		t.Errorf("n=0: %v", err)
	}
	if _, err := c.Allocate(nil, day("2026-11-01"), 61); !isCode(err, "INVALID_INSTALLMENTS") {
		t.Errorf("n=61: %v", err)
	}
	broken := []Invoice{
		{Period: period("2026-10-10", "2026-11-10", "2026-11-20")},
		{Period: period("2026-11-01", "2026-12-10", "2026-12-20")}, // overlaps the previous one
	}
	if _, err := c.Allocate(broken, day("2026-11-01"), 1); !isCode(err, "CARD_INVOICES_INCONSISTENT") {
		t.Errorf("broken chain: %v", err)
	}
}

func isCode(err error, code string) bool {
	var ke *kernel.Error
	return errors.As(err, &ke) && ke.Code == code
}

func FuzzCycle(f *testing.F) {
	f.Add(31, 10, 0, 6)
	f.Add(29, 29, 365*2+58, 12)
	f.Add(1, 31, 40000, 60)
	f.Fuzz(func(t *testing.T, closingDay, dueDay, offset, n int) {
		c, err := NewCycle(closingDay, dueDay)
		if err != nil || offset < 0 || offset > 80000 || n < 1 || n > MaxInstallments {
			return
		}
		d := day("1990-01-01").AddDate(0, 0, offset)
		p := c.PeriodFor(d)
		if !p.Contains(d) || !p.DueOn.After(p.ClosingOn) {
			t.Fatalf("PeriodFor(%s) = %+v", kernel.FormatDate(d), p)
		}
		if days := p.ClosingOn.Sub(p.StartOn).Hours() / 24; days < 28 || days > 31 {
			t.Fatalf("cycle of %v days: %+v", days, p)
		}
		if days := p.DueOn.Sub(p.ClosingOn).Hours() / 24; days > 31 {
			t.Fatalf("due %v days after closing: %+v", days, p)
		}
		if next := c.Next(p); next != c.PeriodFor(p.ClosingOn) {
			t.Fatalf("Next(%+v) = %+v; want %+v", p, next, c.PeriodFor(p.ClosingOn))
		}
		a, err := c.Allocate(nil, d, n)
		if err != nil || len(a.Periods) != n || len(a.Create) != n || a.Periods[0] != p {
			t.Fatalf("Allocate = %+v, %v", a, err)
		}
		for i := 1; i < n; i++ {
			if !a.Periods[i].StartOn.Equal(a.Periods[i-1].ClosingOn) {
				t.Fatalf("installment %d not chained: %+v", i+1, a.Periods)
			}
		}
	})
}
