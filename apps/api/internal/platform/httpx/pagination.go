package httpx

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
)

// Pagination limits (README §7: `limit` up to 100, opaque `cursor`).
const (
	DefaultLimit = 25
	MaxLimit     = 100
)

// Page is the standard paginated response: `{"items": [...], "nextCursor": "..."}`.
// NextCursor is null on the last page.
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

// ParseLimit reads ?limit= (default 25, between 1 and 100).
func ParseLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return DefaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > MaxLimit {
		return 0, kernel.NewError(kernel.ErrValidation, "VALIDATION_ERROR", "limit deve estar entre 1 e 100.",
			kernel.FieldError{Field: "limit", Reason: "range=1..100"})
	}
	return n, nil
}

// EncodeCursor turns keyset position data into an opaque cursor (base64url JSON).
func EncodeCursor(position any) string {
	raw, _ := json.Marshal(position)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeCursor parses ?cursor= into position. An empty cursor leaves position untouched.
func DecodeCursor(r *http.Request, position any) error {
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(b) > 512 || json.Unmarshal(b, position) != nil {
		return kernel.NewError(kernel.ErrValidation, "VALIDATION_ERROR", "Cursor de paginação inválido.",
			kernel.FieldError{Field: "cursor", Reason: "invalid"})
	}
	return nil
}
