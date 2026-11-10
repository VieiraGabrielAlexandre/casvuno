package kernel

import (
	"errors"
	"time"
)

// MaxAbsCents bounds every amount in cents (ADR 0004). It stays below 2^53, so JSON clients
// that read numbers as float64 (JavaScript) still see exact integers. Money is always int64
// cents: never float.
const MaxAbsCents int64 = 1_000_000_000_000_000

// DateLayout is the wire format of calendar dates (README §7).
const DateLayout = "2006-01-02"

var errInvalidDate = errors.New("invalid date")

// ParseDate parses a calendar date (YYYY-MM-DD) into time.Time at UTC midnight, the
// representation used for PostgreSQL DATE columns (ADR 0004).
func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse(DateLayout, s)
	if err != nil || t.Year() < 1900 || t.Year() > 2200 {
		return time.Time{}, errInvalidDate
	}
	return t, nil
}

// FormatDate renders a calendar date as YYYY-MM-DD.
func FormatDate(t time.Time) string { return t.Format(DateLayout) }
