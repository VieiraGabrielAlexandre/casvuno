// Package kernel holds the few domain primitives shared by every module.
package kernel

import "errors"

// Error kinds. Use errors.Is(err, kernel.ErrNotFound) to classify; the HTTP layer maps each
// kind to one status code (see platform/httpx).
var (
	ErrValidation      = errors.New("validation failed")
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
	ErrNotFound        = errors.New("not found")
	ErrConflict        = errors.New("conflict")
	ErrInvariant       = errors.New("business rule violated")
	ErrRateLimited     = errors.New("rate limited")
)

// FieldError describes one invalid input field (JSON name and a machine-readable reason).
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// Error is a classified error with a stable code and a user-facing message (pt-BR).
type Error struct {
	Kind    error
	Code    string
	Message string
	Fields  []FieldError
}

// NewError builds a classified error.
func NewError(kind error, code, message string, fields ...FieldError) *Error {
	return &Error{Kind: kind, Code: code, Message: message, Fields: fields}
}

func (e *Error) Error() string { return e.Code + ": " + e.Kind.Error() }

func (e *Error) Unwrap() error { return e.Kind }
