//go:build integration

package idempotency

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/clock"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/httpx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres/pgtest"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()
	url, stop, err := pgtest.Start(ctx)
	defer stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if pool, err = postgres.NewPool(ctx, url); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer pool.Close()
	// Side effects of the test operation; counting rows tells how many times it really ran.
	if _, err := pool.Exec(ctx, `CREATE TABLE test_effects (id bigserial PRIMARY KEY, household_id uuid NOT NULL, note text NOT NULL)`); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}

type env struct {
	t      *testing.T
	server *httptest.Server
	clock  *clock.Fake
}

// newEnv serves POST /things/{id} behind the middleware. The test operation inserts a row
// in test_effects inside the request transaction and answers 201, or 422 when asked to fail.
func newEnv(t *testing.T) *env {
	t.Helper()
	tx := postgres.NewTxManager(pool)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := &env{t: t, clock: clock.NewFake(time.Now())}
	mw := New(tx, e.clock, log)

	operation := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Note    string `json:"note"`
			Fail    bool   `json:"fail,omitempty"`
			SleepMs int    `json:"sleepMs,omitempty"`
		}
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		m, _ := authctx.MembershipFrom(r.Context())
		var id int64
		if err := tx.Conn(r.Context()).QueryRow(r.Context(),
			`INSERT INTO test_effects (household_id, note) VALUES ($1, $2) RETURNING id`, m.HouseholdID, req.Note).Scan(&id); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		time.Sleep(time.Duration(req.SleepMs) * time.Millisecond)
		if req.Fail {
			httpx.WriteError(w, r, log, kernel.NewError(kernel.ErrInvariant, "TEST_FAILURE", "falhou"))
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{"effectId": id, "note": req.Note})
	})

	r := chi.NewRouter()
	r.Use(httpx.WithRequestID, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			m := authctx.Membership{
				HouseholdID: uuid.MustParse(r.Header.Get("X-Test-Household")),
				UserID:      uuid.MustParse(r.Header.Get("X-Test-User")),
				Role:        authctx.RoleMember,
			}
			next.ServeHTTP(w, r.WithContext(authctx.WithMembership(r.Context(), m)))
		})
	})
	r.With(mw.Require).Post("/things/{id}", operation)
	e.server = httptest.NewServer(r)
	t.Cleanup(e.server.Close)
	return e
}

type actor struct{ householdID, userID uuid.UUID }

// newActor creates a real user and household (the key table has foreign keys to both).
func newActor(t *testing.T) actor {
	t.Helper()
	ctx := context.Background()
	a := actor{householdID: uuid.Must(uuid.NewV7()), userID: uuid.Must(uuid.NewV7())}
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, name, email, password_hash) VALUES ($1, 'T', $2, 'x')`,
		a.userID, a.userID.String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO households (id, name, created_by) VALUES ($1, 'Casa', $2)`,
		a.householdID, a.userID); err != nil {
		t.Fatal(err)
	}
	return a
}

type result struct {
	status   int
	body     string
	replayed bool
	code     string
}

func (e *env) post(a actor, path, key, body string) result {
	e.t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, e.server.URL+path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Household", a.householdID.String())
	req.Header.Set("X-Test-User", a.userID.String())
	if key != "" {
		req.Header.Set(Header, key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &env)
	return result{status: resp.StatusCode, body: string(raw), replayed: resp.Header.Get(ReplayedHeader) == "true", code: env.Error.Code}
}

func effects(t *testing.T, a actor, note string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM test_effects WHERE household_id = $1 AND note = $2`, a.householdID, note).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// T09: the same request twice with the same key runs once and replays the first response.
func TestSameKeySameRequestReplays(t *testing.T) {
	e, a := newEnv(t), newActor(t)

	first := e.post(a, "/things/1", "key-t09", `{"note":"t09"}`)
	if first.status != http.StatusCreated || first.replayed {
		t.Fatalf("first = %+v", first)
	}
	// Same JSON with other key order and whitespace is the same request.
	second := e.post(a, "/things/1", "key-t09", `{ "note" : "t09" }`)
	if second.status != http.StatusCreated || !second.replayed || second.body != first.body {
		t.Fatalf("second = %+v, want replay of %+v", second, first)
	}
	if n := effects(t, a, "t09"); n != 1 {
		t.Fatalf("operation ran %d times, want 1", n)
	}
}

// T10: the same key with a different request is rejected and changes nothing.
func TestSameKeyDifferentRequestConflicts(t *testing.T) {
	e, a := newEnv(t), newActor(t)
	e.post(a, "/things/1", "key-t10", `{"note":"t10"}`)

	for name, tc := range map[string]struct{ path, body string }{
		"other body":     {"/things/1", `{"note":"t10-other"}`},
		"other resource": {"/things/2", `{"note":"t10"}`},
	} {
		t.Run(name, func(t *testing.T) {
			r := e.post(a, tc.path, "key-t10", tc.body)
			if r.status != http.StatusConflict || r.code != "IDEMPOTENCY_KEY_REUSED" {
				t.Fatalf("got %+v", r)
			}
		})
	}
	if n := effects(t, a, "t10") + effects(t, a, "t10-other"); n != 1 {
		t.Fatalf("operation ran %d times, want 1", n)
	}
}

func TestKeyIsRequiredAndValidated(t *testing.T) {
	e, a := newEnv(t), newActor(t)
	for key, code := range map[string]string{
		"":                                     "IDEMPOTENCY_KEY_REQUIRED",
		"has space":                            "IDEMPOTENCY_KEY_INVALID",
		string(bytes.Repeat([]byte("k"), 129)): "IDEMPOTENCY_KEY_INVALID",
		"chave-çã":                             "IDEMPOTENCY_KEY_INVALID",
	} {
		if r := e.post(a, "/things/1", key, `{"note":"invalid-key"}`); r.status != http.StatusBadRequest || r.code != code {
			t.Errorf("key %q: got %+v, want %s", key, r, code)
		}
	}
	if n := effects(t, a, "invalid-key"); n != 0 {
		t.Fatalf("operation ran %d times without a valid key", n)
	}
}

// A failed operation rolls back its effects and the key, so the client can retry.
func TestFailureIsNotStoredAndCanBeRetried(t *testing.T) {
	e, a := newEnv(t), newActor(t)

	if r := e.post(a, "/things/1", "key-retry", `{"note":"retry","fail":true}`); r.status != http.StatusUnprocessableEntity || r.replayed {
		t.Fatalf("failure = %+v", r)
	}
	if n := effects(t, a, "retry"); n != 0 {
		t.Fatalf("failed operation left %d effects", n)
	}
	if r := e.post(a, "/things/1", "key-retry", `{"note":"retry"}`); r.status != http.StatusCreated || r.replayed {
		t.Fatalf("retry = %+v", r)
	}
	if n := effects(t, a, "retry"); n != 1 {
		t.Fatalf("operation ran %d times, want 1", n)
	}
}

// Concurrent requests with the same key: exactly one operation, every client gets its response.
func TestConcurrentRequestsWithSameKeyRunOnce(t *testing.T) {
	e, a := newEnv(t), newActor(t)
	const n = 10
	results := make([]result, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = e.post(a, "/things/1", "key-concurrent", `{"note":"concurrent","sleepMs":100}`)
		}()
	}
	close(start)
	wg.Wait()

	replayed := 0
	for _, r := range results {
		if r.status != http.StatusCreated || r.body != results[0].body {
			t.Fatalf("all clients must get the same 201 response: %+v", results)
		}
		if r.replayed {
			replayed++
		}
	}
	if replayed != n-1 {
		t.Fatalf("replayed = %d, want %d", replayed, n-1)
	}
	if got := effects(t, a, "concurrent"); got != 1 {
		t.Fatalf("operation ran %d times, want 1", got)
	}
}

// Keys are scoped by household and user: the same key from another user is independent.
func TestKeysAreScopedPerUserAndHousehold(t *testing.T) {
	e := newEnv(t)
	a, b := newActor(t), newActor(t)
	if r := e.post(a, "/things/1", "shared-key", `{"note":"scoped"}`); r.replayed {
		t.Fatalf("a = %+v", r)
	}
	if r := e.post(b, "/things/1", "shared-key", `{"note":"scoped"}`); r.status != http.StatusCreated || r.replayed {
		t.Fatalf("b must run its own operation: %+v", r)
	}
	if effects(t, a, "scoped") != 1 || effects(t, b, "scoped") != 1 {
		t.Fatal("each actor must have exactly one effect")
	}
}

// After the retention period the key is forgotten and the request runs again.
func TestExpiredKeyRunsAgain(t *testing.T) {
	e, a := newEnv(t), newActor(t)
	e.post(a, "/things/1", "key-expiry", `{"note":"expiry"}`)
	e.clock.Advance(TTL + time.Minute)
	if r := e.post(a, "/things/1", "key-expiry", `{"note":"expiry"}`); r.status != http.StatusCreated || r.replayed {
		t.Fatalf("expired key must run again: %+v", r)
	}
	if n := effects(t, a, "expiry"); n != 2 {
		t.Fatalf("operation ran %d times, want 2", n)
	}
}
