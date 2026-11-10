package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func reqWithQuery(q string) *http.Request {
	return httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x?"+q, nil)
}

func TestParseLimit(t *testing.T) {
	for q, want := range map[string]int{"": DefaultLimit, "limit=1": 1, "limit=100": 100} {
		if got, err := ParseLimit(reqWithQuery(q)); err != nil || got != want {
			t.Errorf("%q: got %d %v, want %d", q, got, err, want)
		}
	}
	for _, q := range []string{"limit=0", "limit=101", "limit=-1", "limit=abc"} {
		if _, err := ParseLimit(reqWithQuery(q)); err == nil {
			t.Errorf("%q must be rejected", q)
		}
	}
}

func TestCursorRoundTrip(t *testing.T) {
	type pos struct {
		PostedOn string `json:"p"`
		ID       string `json:"i"`
	}
	c := EncodeCursor(pos{PostedOn: "2026-11-10", ID: "abc"})
	var got pos
	if err := DecodeCursor(reqWithQuery("cursor="+c), &got); err != nil || got.PostedOn != "2026-11-10" || got.ID != "abc" {
		t.Fatalf("got %+v %v", got, err)
	}
	for _, bad := range []string{"cursor=***", "cursor=bm90LWpzb24"} {
		if err := DecodeCursor(reqWithQuery(bad), &got); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}
