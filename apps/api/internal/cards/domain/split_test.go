package domain

import (
	"errors"
	"slices"
	"testing"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
)

func TestSplit(t *testing.T) {
	cases := []struct {
		total int64
		n     int
		want  []int64
	}{
		{10001, 3, []int64{3334, 3334, 3333}}, // T06: R$ 100,01 em 3x
		{10000, 1, []int64{10000}},
		{10000, 4, []int64{2500, 2500, 2500, 2500}},
		{5, 5, []int64{1, 1, 1, 1, 1}},
		{100, 60, append(slices.Repeat([]int64{2}, 40), slices.Repeat([]int64{1}, 20)...)},
	}
	for _, c := range cases {
		got, err := Split(c.total, c.n)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("Split(%d, %d) = %v, %v; want %v", c.total, c.n, got, err, c.want)
		}
	}
}

func TestSplitRejects(t *testing.T) {
	cases := []struct {
		total int64
		n     int
		code  string
	}{
		{100, 0, "INVALID_INSTALLMENTS"},
		{100, 61, "INVALID_INSTALLMENTS"},
		{0, 1, "INVALID_AMOUNT"},
		{-100, 2, "INVALID_AMOUNT"},
		{kernel.MaxAbsCents + 1, 2, "INVALID_AMOUNT"},
		{2, 3, "INSTALLMENT_TOO_SMALL"},
	}
	for _, c := range cases {
		_, err := Split(c.total, c.n)
		var ke *kernel.Error
		if !errors.As(err, &ke) || ke.Code != c.code || !errors.Is(err, kernel.ErrValidation) {
			t.Errorf("Split(%d, %d) error = %v; want %s", c.total, c.n, err, c.code)
		}
	}
}

func FuzzSplit(f *testing.F) {
	f.Add(int64(10001), 3)
	f.Add(int64(1), 1)
	f.Add(kernel.MaxAbsCents, 60)
	f.Add(int64(59), 60)
	f.Fuzz(func(t *testing.T, total int64, n int) {
		parts, err := Split(total, n)
		valid := n >= 1 && n <= MaxInstallments && total >= int64(n) && total <= kernel.MaxAbsCents
		if !valid {
			if err == nil {
				t.Fatalf("Split(%d, %d) accepted invalid input", total, n)
			}
			return
		}
		if err != nil || len(parts) != n {
			t.Fatalf("Split(%d, %d) = %v, %v", total, n, parts, err)
		}
		var sum int64
		for i, p := range parts {
			sum += p
			if p < 1 || p-parts[n-1] > 1 || (i > 0 && p > parts[i-1]) {
				t.Fatalf("Split(%d, %d) = %v: parts must be >= 1, non-increasing, differ by at most 1", total, n, parts)
			}
		}
		if sum != total {
			t.Fatalf("Split(%d, %d) sums to %d", total, n, sum)
		}
	})
}
