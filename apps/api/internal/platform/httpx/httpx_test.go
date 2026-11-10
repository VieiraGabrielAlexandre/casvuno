package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

type envelope struct {
	Error struct {
		Code      string              `json:"code"`
		Message   string              `json:"message"`
		Details   []kernel.FieldError `json:"details"`
		RequestID string              `json:"requestId"`
	} `json:"error"`
}

func serve(h http.Handler, req *http.Request) (*httptest.ResponseRecorder, envelope) {
	rec := httptest.NewRecorder()
	WithRequestID(h).ServeHTTP(rec, req)
	var env envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	return rec, env
}

func get() *http.Request {
	return httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
}

func TestWriteErrorMapsKindsAndHidesInternalErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{kernel.NewError(kernel.ErrValidation, "VALIDATION_ERROR", "x", kernel.FieldError{Field: "name", Reason: "required"}), 400, "VALIDATION_ERROR"},
		{fmt.Errorf("wrapped: %w", kernel.NewError(kernel.ErrNotFound, "HOUSEHOLD_NOT_FOUND", "x")), 404, "HOUSEHOLD_NOT_FOUND"},
		{kernel.ErrUnauthenticated, 401, "UNAUTHENTICATED"},
		{kernel.NewError(kernel.ErrInvariant, "LAST_OWNER", "x"), 422, "LAST_OWNER"},
		{errors.New("pq: password=secret leaked"), 500, "INTERNAL_ERROR"},
	} {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { WriteError(w, r, discard, tc.err) })
		rec, env := serve(h, get())
		if rec.Code != tc.status || env.Error.Code != tc.code || env.Error.RequestID == "" {
			t.Errorf("%v: got %d %+v", tc.err, rec.Code, env)
		}
		if strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("internal error details leaked: %s", rec.Body)
		}
	}
}

func TestRecovererReturnsEnvelope(t *testing.T) {
	h := Recoverer(discard)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec, env := serve(h, get())
	if rec.Code != 500 || env.Error.Code != "INTERNAL_ERROR" || strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestRequestIDAcceptsOnlyWellFormedIDs(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, RequestID(r.Context())) })
	req := get()
	req.Header.Set("X-Request-Id", "abc-123")
	rec, _ := serve(h, req)
	if rec.Body.String() != "abc-123" || rec.Header().Get("X-Request-Id") != "abc-123" {
		t.Fatalf("valid id not kept: %q", rec.Body)
	}
	req = get()
	req.Header.Set("X-Request-Id", "bad id\nwith newline")
	rec, _ = serve(h, req)
	if strings.Contains(rec.Body.String(), "bad") || rec.Body.Len() == 0 {
		t.Fatalf("invalid id must be replaced: %q", rec.Body)
	}
}

func TestDecodeJSONIsStrict(t *testing.T) {
	type payload struct {
		Name string `json:"name" validate:"required,max=5"`
	}
	for _, tc := range []struct {
		body, contentType, code, field string
	}{
		{`{"name":"ok"}`, "application/json; charset=utf-8", "", ""},
		{`{"name":"ok"}`, "text/plain", "UNSUPPORTED_MEDIA_TYPE", ""},
		{`{"name":"ok","extra":1}`, "application/json", "VALIDATION_ERROR", "extra"},
		{`{"name":1}`, "application/json", "VALIDATION_ERROR", "name"},
		{`{"name":"toolong"}`, "application/json", "VALIDATION_ERROR", "name"},
		{`{}`, "application/json", "VALIDATION_ERROR", "name"},
		{`{"name":"ok"}{"name":"x"}`, "application/json", "INVALID_JSON", ""},
		{`{"name":`, "application/json", "INVALID_JSON", ""},
		{`{"name":"` + strings.Repeat("a", maxBodyBytes) + `"}`, "application/json", "PAYLOAD_TOO_LARGE", ""},
	} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", tc.contentType)
		var p payload
		err := DecodeJSON(httptest.NewRecorder(), req, &p)

		var ke *kernel.Error
		switch {
		case tc.code == "" && err != nil:
			t.Errorf("%.40s: unexpected error %v", tc.body, err)
		case tc.code != "" && (!errors.As(err, &ke) || ke.Code != tc.code):
			t.Errorf("%.40s: got %v, want %s", tc.body, err, tc.code)
		case tc.field != "" && (len(ke.Fields) == 0 || ke.Fields[0].Field != tc.field):
			t.Errorf("%.40s: fields %+v, want %s", tc.body, ke.Fields, tc.field)
		}
	}
}
