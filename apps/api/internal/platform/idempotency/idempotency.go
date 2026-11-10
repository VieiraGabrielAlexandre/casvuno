// Package idempotency makes critical financial mutations safe to retry (ADR 0007).
//
// The key row is written in the same database transaction as the operation:
//   - first request: the handler runs inside the transaction; a 2xx response is stored with
//     the key and committed, any other response rolls everything back (the key included), so
//     the client may retry with the same key;
//   - same key and same request: the stored response is replayed (header Idempotent-Replayed);
//   - same key and a different request: 409 IDEMPOTENCY_KEY_REUSED;
//   - concurrent requests with the same key: PostgreSQL makes the second INSERT wait for the
//     first transaction, so exactly one operation runs and the other replays it.
package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/clock"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/httpx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/idempotency/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
)

const (
	// Header is the request header carrying the client-generated key.
	Header = "Idempotency-Key"
	// ReplayedHeader marks a response served from a previous execution.
	ReplayedHeader = "Idempotent-Replayed"
	// TTL is how long a key is remembered (ADR 0007).
	TTL = 7 * 24 * time.Hour

	maxKeyLen    = 128
	maxBodyBytes = 1 << 20
)

// Middleware enforces idempotency on the routes it wraps. Those routes must be household
// scoped (mounted behind households.RequireMember): keys are per household and user.
type Middleware struct {
	tx    *postgres.TxManager
	clock clock.Clock
	log   *slog.Logger
}

// New builds the middleware.
func New(tx *postgres.TxManager, clk clock.Clock, log *slog.Logger) *Middleware {
	return &Middleware{tx: tx, clock: clk, log: log}
}

var (
	errKeyRequired = kernel.NewError(kernel.ErrValidation, "IDEMPOTENCY_KEY_REQUIRED",
		"Envie o cabeçalho Idempotency-Key para esta operação.")
	errKeyInvalid = kernel.NewError(kernel.ErrValidation, "IDEMPOTENCY_KEY_INVALID",
		"Idempotency-Key deve ter de 1 a 128 caracteres ASCII visíveis.")
	errKeyReused = kernel.NewError(kernel.ErrConflict, "IDEMPOTENCY_KEY_REUSED",
		"Esta Idempotency-Key já foi usada com outra requisição.")
	errPayloadTooLarge = kernel.NewError(kernel.ErrValidation, "PAYLOAD_TOO_LARGE", "Corpo da requisição muito grande.")

	// errNotCompleted rolls back the transaction when the handler did not succeed.
	errNotCompleted = errors.New("idempotency: handler did not succeed")
)

// Require wraps a state-changing household route.
func (m *Middleware) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		membership, ok := authctx.MembershipFrom(r.Context())
		if !ok {
			httpx.WriteError(w, r, m.log, errors.New("idempotency: route is not household scoped"))
			return
		}
		key := r.Header.Get(Header)
		if key == "" {
			httpx.WriteError(w, r, m.log, errKeyRequired)
			return
		}
		if !validKey(key) {
			httpx.WriteError(w, r, m.log, errKeyInvalid)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		if err != nil {
			httpx.WriteError(w, r, m.log, errPayloadTooLarge)
			return
		}

		id := identity{
			householdID: membership.HouseholdID,
			userID:      membership.UserID,
			endpoint:    r.Method + " " + routePattern(r),
			key:         key,
			hash:        requestHash(r.Method, r.URL.Path, body),
		}

		var (
			replay *store.GetKeyRow
			result *recorder
		)
		err = m.tx.WithinTx(r.Context(), func(ctx context.Context) error {
			rowID, stored, err := m.claim(ctx, id)
			if err != nil || stored != nil {
				replay = stored
				return err
			}
			result = newRecorder()
			req := r.WithContext(ctx)
			req.Body = io.NopCloser(bytes.NewReader(body))
			next.ServeHTTP(result, req)
			if result.status < 200 || result.status > 299 {
				return errNotCompleted
			}
			return m.complete(ctx, rowID, result)
		})

		switch {
		case err != nil && !errors.Is(err, errNotCompleted):
			httpx.WriteError(w, r, m.log, err)
		case replay != nil:
			writeReplay(w, replay)
		default:
			result.flush(w)
		}
	})
}

type identity struct {
	householdID uuid.UUID
	userID      uuid.UUID
	endpoint    string
	key         string
	hash        []byte
}

// claim inserts the key and returns its row ID, or returns the stored row to replay.
// Expired keys are replaced.
func (m *Middleware) claim(ctx context.Context, id identity) (uuid.UUID, *store.GetKeyRow, error) {
	q := store.New(m.tx.Conn(ctx))
	now := m.clock.Now()
	insert := func() (uuid.UUID, error) {
		rowID, err := uuid.NewV7()
		if err != nil {
			return uuid.Nil, err
		}
		return q.InsertKey(ctx, store.InsertKeyParams{
			ID: rowID, HouseholdID: id.householdID, UserID: id.userID, Endpoint: id.endpoint,
			Key: id.key, RequestHash: id.hash, ExpiresAt: now.Add(TTL),
		})
	}

	rowID, err := insert()
	if err == nil {
		return rowID, nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil, fmt.Errorf("insert idempotency key: %w", err)
	}

	// Conflict: another transaction committed this key (the INSERT waited for it).
	row, err := q.GetKey(ctx, store.GetKeyParams{
		HouseholdID: id.householdID, UserID: id.userID, Endpoint: id.endpoint, Key: id.key,
	})
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("get idempotency key: %w", err)
	}
	if !row.ExpiresAt.After(now) {
		if err := q.DeleteKey(ctx, row.ID); err != nil {
			return uuid.Nil, nil, fmt.Errorf("delete expired idempotency key: %w", err)
		}
		if rowID, err = insert(); err != nil {
			return uuid.Nil, nil, fmt.Errorf("reinsert idempotency key: %w", err)
		}
		return rowID, nil, nil
	}
	if !bytes.Equal(row.RequestHash, id.hash) {
		return uuid.Nil, nil, errKeyReused
	}
	if row.ResponseStatus == nil {
		// Unreachable while rows are only committed together with their response.
		return uuid.Nil, nil, errors.New("idempotency: stored key has no response")
	}
	return uuid.Nil, &row, nil
}

func (m *Middleware) complete(ctx context.Context, rowID uuid.UUID, rec *recorder) error {
	var body []byte
	if rec.body.Len() > 0 {
		if !json.Valid(rec.body.Bytes()) {
			return errors.New("idempotency: response body is not JSON")
		}
		body = rec.body.Bytes()
	}
	if err := store.New(m.tx.Conn(ctx)).CompleteKey(ctx, store.CompleteKeyParams{
		ID: rowID, ResponseStatus: int16(rec.status), ResponseBody: body, //nolint:gosec // status is 2xx
	}); err != nil {
		return fmt.Errorf("complete idempotency key: %w", err)
	}
	return nil
}

func writeReplay(w http.ResponseWriter, row *store.GetKeyRow) {
	w.Header().Set(ReplayedHeader, "true")
	if len(row.ResponseBody) > 0 {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(int(*row.ResponseStatus))
	_, _ = w.Write(row.ResponseBody)
}

// requestHash identifies "the same request": method, concrete path (so the same key on
// another resource is a conflict) and the JSON body in canonical form (key order and
// whitespace do not matter). Non-JSON bodies are hashed as is.
func requestHash(method, path string, body []byte) []byte {
	canonical := body
	var v any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if len(bytes.TrimSpace(body)) > 0 && dec.Decode(&v) == nil {
		if b, err := json.Marshal(v); err == nil {
			canonical = b
		}
	}
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s %s\n", method, path)
	_, _ = h.Write(canonical)
	return h.Sum(nil)
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
		return rc.RoutePattern()
	}
	return r.URL.Path
}

func validKey(k string) bool {
	if len(k) > maxKeyLen {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < 0x21 || k[i] > 0x7e {
			return false
		}
	}
	return true
}

// recorder buffers the handler response until the transaction outcome is known.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newRecorder() *recorder { return &recorder{header: http.Header{}} }

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(b)
}

func (r *recorder) flush(w http.ResponseWriter) {
	for k, v := range r.header {
		w.Header()[k] = v
	}
	status := r.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(r.body.Bytes())
}
