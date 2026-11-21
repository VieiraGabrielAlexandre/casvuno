package domain

import "time"

// Derived payment situation of an invoice (I-03): it is never stored, only computed from
// the balance, so it cannot drift from the payments (F4.5).
const (
	PaymentUnpaid        = "UNPAID"
	PaymentPartiallyPaid = "PARTIALLY_PAID"
	PaymentPaid          = "PAID"
)

// PaymentStatus derives the situation from the invoice total and what was paid: nothing
// left to pay (including an empty invoice) is PAID, some payment with a balance left is
// PARTIALLY_PAID, otherwise UNPAID (I-51).
func PaymentStatus(totalCents, paidCents int64) string {
	switch {
	case paidCents >= totalCents:
		return PaymentPaid
	case paidCents > 0:
		return PaymentPartiallyPaid
	default:
		return PaymentUnpaid
	}
}

// Closable tells whether an OPEN invoice may be closed on today (the household's date):
// closing is manual and allowed from the closing date on (I-15, ADR 0003).
func Closable(closingOn, today time.Time) bool {
	return !today.Before(closingOn)
}
