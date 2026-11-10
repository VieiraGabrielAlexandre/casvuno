// Package httpx holds the HTTP conventions shared by every module: strict JSON decoding,
// the error envelope, request IDs and security middleware.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// validate is configured once and only read afterwards (validator.Validate is concurrency safe).
var validate = newValidator()

func newValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}
		return name
	})
	return v
}

// WriteJSON writes v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// DecodeJSON strictly decodes the request body into dst and validates its `validate` tags.
// Unknown fields, trailing data, wrong types and bodies over 1 MiB are rejected.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return kernel.NewError(kernel.ErrValidation, "UNSUPPORTED_MEDIA_TYPE", "Envie o corpo como application/json.")
	}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return kernel.NewError(kernel.ErrValidation, "INVALID_JSON", "O corpo deve conter um único objeto JSON.")
	}
	return validateStruct(dst)
}

func decodeError(err error) error {
	var (
		maxErr  *http.MaxBytesError
		typeErr *json.UnmarshalTypeError
	)
	switch {
	case errors.As(err, &maxErr):
		return kernel.NewError(kernel.ErrValidation, "PAYLOAD_TOO_LARGE", "Corpo da requisição muito grande.")
	case errors.As(err, &typeErr):
		return kernel.NewError(kernel.ErrValidation, "VALIDATION_ERROR", "Dados inválidos.",
			kernel.FieldError{Field: typeErr.Field, Reason: "invalid type"})
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		field := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		return kernel.NewError(kernel.ErrValidation, "VALIDATION_ERROR", "Dados inválidos.",
			kernel.FieldError{Field: field, Reason: "unknown field"})
	default:
		return kernel.NewError(kernel.ErrValidation, "INVALID_JSON", "JSON inválido.")
	}
}

func validateStruct(dst any) error {
	err := validate.Struct(dst)
	if err == nil {
		return nil
	}
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		return err
	}
	fields := make([]kernel.FieldError, 0, len(verrs))
	for _, fe := range verrs {
		reason := fe.Tag()
		if fe.Param() != "" {
			reason += "=" + fe.Param()
		}
		fields = append(fields, kernel.FieldError{Field: fe.Field(), Reason: reason})
	}
	return kernel.NewError(kernel.ErrValidation, "VALIDATION_ERROR", "Dados inválidos.", fields...)
}
