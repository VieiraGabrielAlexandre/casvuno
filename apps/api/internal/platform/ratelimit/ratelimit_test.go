package ratelimit

import (
	"testing"
	"time"
)

func TestAllowIsPerKeyAndBounded(t *testing.T) {
	l := New(3, time.Hour)
	for i := range 3 {
		if !l.Allow("a") {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}
	if l.Allow("a") {
		t.Fatal("4th attempt within the period must be rejected")
	}
	if !l.Allow("b") {
		t.Fatal("other keys must have their own budget")
	}
}
