package domain

import (
	"time"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
)

// Cycle is the card's billing rule: the invoice closes on ClosingDay and is due on DueDay,
// both 1..31 and normalized to the last day of short months (ADR 0003).
type Cycle struct {
	ClosingDay int
	DueDay     int
}

// NewCycle validates the closing and due days.
func NewCycle(closingDay, dueDay int) (Cycle, error) {
	var fields []kernel.FieldError
	if closingDay < 1 || closingDay > 31 {
		fields = append(fields, kernel.FieldError{Field: "closingDay", Reason: "range"})
	}
	if dueDay < 1 || dueDay > 31 {
		fields = append(fields, kernel.FieldError{Field: "dueDay", Reason: "range"})
	}
	if fields != nil {
		return Cycle{}, kernel.NewError(kernel.ErrValidation, "INVALID_CARD_CYCLE",
			"Os dias de fechamento e vencimento devem ficar entre 1 e 31.", fields...)
	}
	return Cycle{ClosingDay: closingDay, DueDay: dueDay}, nil
}

// Period holds the concrete dates of one invoice. Purchases on [StartOn, ClosingOn) belong
// to it. Dates are calendar dates at UTC midnight (kernel.ParseDate).
type Period struct {
	StartOn   time.Time
	ClosingOn time.Time
	DueOn     time.Time
}

// Contains reports whether a purchase on date d belongs to the period.
func (p Period) Contains(d time.Time) bool {
	return !d.Before(p.StartOn) && d.Before(p.ClosingOn)
}

func (p Period) overlaps(o Period) bool {
	return p.StartOn.Before(o.ClosingOn) && o.StartOn.Before(p.ClosingOn)
}

// Invoice is a persisted period and whether it is CLOSED (its base amount is frozen).
type Invoice struct {
	Period
	Closed bool
}

// Allocation places the installments of one purchase.
type Allocation struct {
	// Periods has one entry per installment, in order.
	Periods []Period
	// Create lists the periods that do not exist yet, ascending, including the ones skipped
	// between the last existing invoice and the target, so the chain stays contiguous.
	Create []Period
	// Shifted is true when a CLOSED invoice was skipped (I-15: allocationShifted).
	Shifted bool
}

var errOverlap = kernel.NewError(kernel.ErrInvariant, "CARD_CYCLE_OVERLAP",
	"A data cai num ciclo que se sobrepõe a faturas existentes deste cartão.",
	kernel.FieldError{Field: "occurredOn", Reason: "cycle_overlap"})

var errBrokenChain = kernel.NewError(kernel.ErrInvariant, "CARD_INVOICES_INCONSISTENT",
	"As faturas deste cartão estão inconsistentes.")

// norm clamps day to the last day of the month (ADR 0003).
func norm(day int, year int, month time.Month) time.Time {
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return time.Date(year, month, min(day, last), 0, 0, 0, 0, time.UTC)
}

// closingAfter is the first closing date strictly after d under the current rule.
func (c Cycle) closingAfter(d time.Time) time.Time {
	if closing := norm(c.ClosingDay, d.Year(), d.Month()); closing.After(d) {
		return closing
	}
	return norm(c.ClosingDay, d.Year(), d.Month()+1)
}

// dueFor is the first of norm(DueDay, M) and norm(DueDay, M+1) strictly after closing, M
// being the closing month. DueDay == ClosingDay therefore falls in the next month.
func (c Cycle) dueFor(closing time.Time) time.Time {
	if due := norm(c.DueDay, closing.Year(), closing.Month()); due.After(closing) {
		return due
	}
	return norm(c.DueDay, closing.Year(), closing.Month()+1)
}

// PeriodFor computes, by the current rule alone, the invoice of a purchase on date d: the
// one with the smallest closing date after d, starting at the previous month's closing.
// A purchase on the closing day goes to the next invoice (RN-011).
func (c Cycle) PeriodFor(d time.Time) Period {
	closing := c.closingAfter(d)
	return Period{
		StartOn:   norm(c.ClosingDay, closing.Year(), closing.Month()-1),
		ClosingOn: closing,
		DueOn:     c.dueFor(closing),
	}
}

// Next is the invoice that follows p, chained on p's closing date. After a change of the
// card's days the first chained period may be shorter or longer than a month (ADR 0003:
// changes only affect invoices not created yet).
func (c Cycle) Next(p Period) Period {
	closing := c.closingAfter(p.ClosingOn)
	return Period{StartOn: p.ClosingOn, ClosingOn: closing, DueOn: c.dueFor(closing)}
}

// Allocate places n installments of a purchase on date d (ADR 0003). existing are the
// card's persisted invoices in ascending order. The first installment goes to the invoice
// that contains d (an existing one, else the chain continued from the last existing invoice
// before d, else the current rule when d precedes every invoice); installment k goes to the
// (k−1)-th following invoice. CLOSED invoices are skipped and set Shifted (I-15). A computed
// period that would overlap an existing invoice is rejected (CARD_CYCLE_OVERLAP).
func (c Cycle) Allocate(existing []Invoice, d time.Time, n int) (Allocation, error) {
	if n < 1 || n > MaxInstallments {
		return Allocation{}, kernel.NewError(kernel.ErrValidation, "INVALID_INSTALLMENTS",
			"O número de parcelas deve ficar entre 1 e 60.",
			kernel.FieldError{Field: "installments", Reason: "range"})
	}
	var prevClosing time.Time
	for _, e := range existing {
		if !e.StartOn.Before(e.ClosingOn) || !e.DueOn.After(e.ClosingOn) || prevClosing.After(e.StartOn) {
			return Allocation{}, errBrokenChain
		}
		prevClosing = e.ClosingOn
	}
	w := walker{cycle: c, existing: existing}
	if err := w.locate(d); err != nil {
		return Allocation{}, err
	}
	var out Allocation
	for {
		if w.closed() {
			out.Shifted = true
		} else {
			out.Periods = append(out.Periods, w.cur)
			if len(out.Periods) == n {
				break
			}
		}
		if err := w.advance(); err != nil {
			return Allocation{}, err
		}
	}
	out.Create = w.created
	return out, nil
}

// walker moves along the invoice chain, reusing existing invoices and recording the
// periods that must be created.
type walker struct {
	cycle    Cycle
	existing []Invoice
	cur      Period
	idx      int // index of cur in existing, or -1 when cur is new
	created  []Period
}

func (w *walker) closed() bool { return w.idx >= 0 && w.existing[w.idx].Closed }

func (w *walker) locate(d time.Time) error {
	prev := -1
	for i, e := range w.existing {
		if e.Contains(d) {
			w.cur, w.idx = e.Period, i
			return nil
		}
		if !e.ClosingOn.After(d) {
			prev = i
		}
	}
	if prev < 0 {
		// Before every existing invoice (or none yet): the current rule decides.
		return w.add(w.cycle.PeriodFor(d))
	}
	w.cur, w.idx = w.existing[prev].Period, prev
	for !w.cur.Contains(d) {
		if err := w.advance(); err != nil {
			return err
		}
	}
	return nil
}

// advance moves to the invoice that starts on the current closing date: the existing one
// if any, else the next period computed by the current rule.
func (w *walker) advance() error {
	for i, e := range w.existing {
		if e.StartOn.Equal(w.cur.ClosingOn) {
			w.cur, w.idx = e.Period, i
			return nil
		}
	}
	return w.add(w.cycle.Next(w.cur))
}

// maxWalk bounds the periods one allocation may create. Dates are limited to 1900..2200
// (kernel.ParseDate), about 3,600 monthly cycles; the bound only guards against a bug that
// would stop the chain from advancing.
const maxWalk = 4000

func (w *walker) add(p Period) error {
	if !p.StartOn.Before(p.ClosingOn) || !p.DueOn.After(p.ClosingOn) || len(w.created) >= maxWalk {
		return errBrokenChain
	}
	for _, e := range w.existing {
		if p.overlaps(e.Period) {
			return errOverlap
		}
	}
	w.cur, w.idx = p, -1
	w.created = append(w.created, p)
	return nil
}
