package idempotency

import (
	"bytes"
	"strings"
	"testing"
)

func TestRequestHashIgnoresJSONFormattingButNotContent(t *testing.T) {
	base := requestHash("POST", "/x/1", []byte(`{"a":1,"b":[1,2]}`))
	same := requestHash("POST", "/x/1", []byte(" {\n \"b\": [1, 2], \"a\": 1 }\n"))
	if !bytes.Equal(base, same) {
		t.Fatal("key order and whitespace must not change the hash")
	}
	for name, h := range map[string][]byte{
		"other value":  requestHash("POST", "/x/1", []byte(`{"a":2,"b":[1,2]}`)),
		"array order":  requestHash("POST", "/x/1", []byte(`{"a":1,"b":[2,1]}`)),
		"other path":   requestHash("POST", "/x/2", []byte(`{"a":1,"b":[1,2]}`)),
		"other method": requestHash("PUT", "/x/1", []byte(`{"a":1,"b":[1,2]}`)),
		"big number":   requestHash("POST", "/x/1", []byte(`{"a":10000000000000001,"b":[1,2]}`)),
	} {
		if bytes.Equal(base, h) {
			t.Errorf("%s must change the hash", name)
		}
	}
	// Large integers keep their exact value (no float rounding).
	if bytes.Equal(requestHash("POST", "/", []byte(`{"v":9007199254740993}`)), requestHash("POST", "/", []byte(`{"v":9007199254740992}`))) {
		t.Error("integers beyond float precision must hash differently")
	}
}

func TestValidKey(t *testing.T) {
	for _, k := range []string{"a", "6c5d4f2e-0b9a-4c1d-8e7f-123456789abc", strings.Repeat("x", 128), "~!@#$%^&*()_+"} {
		if !validKey(k) {
			t.Errorf("%q should be valid", k)
		}
	}
	for _, k := range []string{strings.Repeat("x", 129), "has space", "tab\t", "ç", "\x00"} {
		if validKey(k) {
			t.Errorf("%q should be invalid", k)
		}
	}
}
