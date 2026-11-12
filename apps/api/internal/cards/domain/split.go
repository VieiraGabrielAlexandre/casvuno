// Package domain holds the pure rules of credit cards: installment split (RN-010) and the
// invoice cycle (RN-011, ADR 0003). No IO, no clock, no database.
package domain

import "github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"

// MaxInstallments is the largest number of installments of one purchase (RN-010, I-32).
const MaxInstallments = 60

// Split divides total cents into n installments: base = total/n and the first total%n
// installments get one extra cent, so the sum is exact and the order is non-increasing
// (RN-010). Every installment is at least one cent (I-48).
func Split(total int64, n int) ([]int64, error) {
	if n < 1 || n > MaxInstallments {
		return nil, kernel.NewError(kernel.ErrValidation, "INVALID_INSTALLMENTS",
			"O número de parcelas deve ficar entre 1 e 60.",
			kernel.FieldError{Field: "installments", Reason: "range"})
	}
	if total < 1 || total > kernel.MaxAbsCents {
		return nil, kernel.NewError(kernel.ErrValidation, "INVALID_AMOUNT",
			"Informe um valor maior que zero e dentro do limite.",
			kernel.FieldError{Field: "amountCents", Reason: "range"})
	}
	if total < int64(n) {
		return nil, kernel.NewError(kernel.ErrValidation, "INSTALLMENT_TOO_SMALL",
			"Cada parcela precisa ter pelo menos R$ 0,01.",
			kernel.FieldError{Field: "installments", Reason: "too_small"})
	}
	base, extra := total/int64(n), total%int64(n)
	parts := make([]int64, n)
	for i := range parts {
		parts[i] = base
		if int64(i) < extra {
			parts[i]++
		}
	}
	return parts, nil
}
