package httpx

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
)

type errorEnvelope struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code      string              `json:"code"`
	Message   string              `json:"message"`
	Details   []kernel.FieldError `json:"details,omitempty"`
	RequestID string              `json:"requestId"`
}

type kindMapping struct {
	kind    error
	status  int
	code    string
	message string
}

var kinds = []kindMapping{
	{kernel.ErrValidation, http.StatusBadRequest, "VALIDATION_ERROR", "Dados inválidos."},
	{kernel.ErrUnauthenticated, http.StatusUnauthorized, "UNAUTHENTICATED", "Faça login para continuar."},
	{kernel.ErrForbidden, http.StatusForbidden, "FORBIDDEN", "Você não tem permissão para esta ação."},
	{kernel.ErrNotFound, http.StatusNotFound, "NOT_FOUND", "Recurso não encontrado."},
	{kernel.ErrConflict, http.StatusConflict, "CONFLICT", "Conflito com o estado atual."},
	{kernel.ErrInvariant, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION", "Operação não permitida."},
	{kernel.ErrRateLimited, http.StatusTooManyRequests, "RATE_LIMITED", "Muitas tentativas. Aguarde e tente novamente."},
}

// WriteError writes err in the standard envelope (README §7). Unclassified errors become
// 500 and are logged; their details never reach the client.
func WriteError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	payload := errorPayload{RequestID: RequestID(r.Context())}
	status := http.StatusInternalServerError

	for _, k := range kinds {
		if errors.Is(err, k.kind) {
			status, payload.Code, payload.Message = k.status, k.code, k.message
			break
		}
	}

	var ke *kernel.Error
	if errors.As(err, &ke) {
		payload.Code, payload.Details = ke.Code, ke.Fields
		if ke.Message != "" {
			payload.Message = ke.Message
		}
	}

	if status == http.StatusInternalServerError {
		log.ErrorContext(r.Context(), "unhandled error",
			slog.String("request_id", payload.RequestID), slog.Any("error", err))
		payload.Code, payload.Message = "INTERNAL_ERROR", "Erro interno. Tente novamente."
	}

	WriteJSON(w, status, errorEnvelope{Error: payload})
}
