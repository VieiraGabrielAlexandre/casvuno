package kernel

import (
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	d, err := ParseDate("2028-02-29")
	if err != nil || d != time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC) || FormatDate(d) != "2028-02-29" {
		t.Fatalf("got %v %v", d, err)
	}
	for _, bad := range []string{"", "2027-02-29", "2026-13-01", "10/11/2026", "2026-11-10T00:00:00Z", "1899-12-31", "2201-01-01"} {
		if _, err := ParseDate(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestMaxAbsCentsIsExactInJSONNumbers(t *testing.T) {
	const maxSafeInteger = 1<<53 - 1 // largest integer a float64 (JavaScript number) represents exactly
	if MaxAbsCents > maxSafeInteger {
		t.Fatal("MaxAbsCents must stay below 2^53")
	}
}

func TestTodayUsesTheHouseholdTimezone(t *testing.T) {
	// 02:30 UTC on Nov 11 is still Nov 10 in São Paulo (UTC−3).
	now := time.Date(2026, 11, 11, 2, 30, 0, 0, time.UTC)
	if got := FormatDate(Today(now, "America/Sao_Paulo")); got != "2026-11-10" {
		t.Fatalf("São Paulo: %s", got)
	}
	if got := FormatDate(Today(now, "UTC")); got != "2026-11-11" {
		t.Fatalf("UTC: %s", got)
	}
	if got := Today(now, "Not/AZone"); got.Location() != time.UTC || FormatDate(got) != "2026-11-11" {
		t.Fatalf("invalid zone must fall back to UTC: %v", got)
	}
}
