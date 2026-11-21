package domain

import (
	"testing"
	"time"
)

func TestPaymentStatus(t *testing.T) {
	cases := []struct {
		total, paid int64
		want        string
	}{
		{20000, 0, PaymentUnpaid},
		{20000, 5000, PaymentPartiallyPaid}, // T08: R$ 50 of R$ 200
		{20000, 19999, PaymentPartiallyPaid},
		{20000, 20000, PaymentPaid},
		{0, 0, PaymentPaid}, // empty invoice: nothing to pay
	}
	for _, c := range cases {
		if got := PaymentStatus(c.total, c.paid); got != c.want {
			t.Errorf("PaymentStatus(%d, %d) = %s, want %s", c.total, c.paid, got, c.want)
		}
	}
}

func TestClosable(t *testing.T) {
	closing := time.Date(2026, 12, 3, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		today time.Time
		want  bool
	}{
		{closing.AddDate(0, 0, -1), false},
		{closing, true}, // the closing day itself
		{closing.AddDate(0, 0, 1), true},
	} {
		if got := Closable(closing, c.today); got != c.want {
			t.Errorf("Closable(%s) = %v, want %v", c.today.Format(time.DateOnly), got, c.want)
		}
	}
}
