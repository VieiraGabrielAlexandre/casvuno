//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/identity"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/clock"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/config"
)

const testOrigin = "http://casvuno.test"

// Cheap Argon2 parameters keep the suite fast; production parameters are tested in identity.
var fastArgon2 = identity.Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

// testAPI is the full application (real Fx graph, real PostgreSQL) behind httptest.
type testAPI struct {
	t        *testing.T
	server   *httptest.Server
	handler  http.Handler
	clock    *clock.Fake
	pool     *pgxpool.Pool
	identity *identity.Service
}

func newTestAPI(t *testing.T, configure ...func(*config.Config)) *testAPI {
	t.Helper()
	cfg := testConfig(databaseURL)
	cfg.AppOrigin = testOrigin
	cfg.RegistrationEnabled = true
	for _, f := range configure {
		f(&cfg)
	}

	api := &testAPI{t: t, clock: clock.NewFake(time.Now().UTC())}
	a := New(cfg, fx.NopLogger,
		fx.Decorate(func(clock.Clock) clock.Clock { return api.clock }),
		fx.Decorate(func(identity.Argon2Params) identity.Argon2Params { return fastArgon2 }),
		fx.Populate(&api.handler, &api.pool, &api.identity),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatalf("start app: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(context.Background()) })

	api.server = httptest.NewServer(api.handler)
	t.Cleanup(api.server.Close)
	return api
}

// client is one browser: its own cookie jar, always sending the app Origin.
type client struct {
	api  *testAPI
	http *http.Client
}

func (api *testAPI) newClient() *client {
	jar, _ := cookiejar.New(nil)
	return &client{api: api, http: &http.Client{Jar: jar}}
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func (r response) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

func (r response) errorCode(t *testing.T) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	r.decode(t, &env)
	return env.Error.Code
}

// do sends a request (JSON body if body != nil) and validates the response against
// docs/openapi.yaml: undocumented status codes and schema mismatches fail the test.
func (c *client) do(method, path string, body any, headers ...string) response {
	t := c.api.t
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.api.server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", testOrigin)
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i+1] == "" {
			req.Header.Del(headers[i])
		} else {
			req.Header.Set(headers[i], headers[i+1])
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := response{status: resp.StatusCode, header: resp.Header, body: raw}
	validateAgainstOpenAPI(t, method, path, out)
	return out
}

func (c *client) expect(status int, method, path string, body any, headers ...string) response {
	c.api.t.Helper()
	r := c.do(method, path, body, headers...)
	if r.status != status {
		c.api.t.Fatalf("%s %s: status %d, want %d; body %s", method, path, r.status, status, r.body)
	}
	return r
}

// sessionCookie returns the session cookie stored for the API host ("" when none).
func (c *client) sessionCookie() string {
	u, _ := url.Parse(c.api.server.URL)
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == "casvuno_session" {
			return ck.Value
		}
	}
	return ""
}

type userDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type householdDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
	MemberID string `json:"memberId"`
	Role     string `json:"role"`
}

type inviteDTO struct {
	ID    string `json:"id"`
	Token string `json:"token"`
	URL   string `json:"url"`
}

const testPassword = "uma senha bem longa"

// registerUser signs up a fresh user (unique e-mail) and returns the logged-in client.
func (api *testAPI) registerUser(name string) (*client, userDTO) {
	api.t.Helper()
	c := api.newClient()
	var u userDTO
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"name": name, "email": uniqueEmail(name), "password": testPassword,
	}).decode(api.t, &u)
	return c, u
}

func (c *client) createHousehold(name string) householdDTO {
	c.api.t.Helper()
	var h householdDTO
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/households", map[string]any{"name": name}).decode(c.api.t, &h)
	return h
}

func (c *client) createInvite(householdID string) inviteDTO {
	c.api.t.Helper()
	var inv inviteDTO
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/households/"+householdID+"/invites", map[string]any{}).
		decode(c.api.t, &inv)
	return inv
}

func uniqueEmail(prefix string) string {
	return prefix + "-" + uuid.NewString()[:8] + "@example.com"
}

var (
	openapiOnce   sync.Once
	openapiRouter routers.Router
	openapiErr    error
)

func validateAgainstOpenAPI(t *testing.T, method, path string, r response) {
	t.Helper()
	openapiOnce.Do(func() {
		loader := openapi3.NewLoader()
		var doc *openapi3.T
		if doc, openapiErr = loader.LoadFromFile("../../../../docs/openapi.yaml"); openapiErr != nil {
			return
		}
		if openapiErr = doc.Validate(loader.Context); openapiErr != nil {
			return
		}
		openapiRouter, openapiErr = gorillamux.NewRouter(doc)
	})
	if openapiErr != nil {
		t.Fatalf("load docs/openapi.yaml: %v", openapiErr)
	}

	req, _ := http.NewRequestWithContext(context.Background(), method, "http://localhost"+path, nil)
	route, params, err := openapiRouter.FindRoute(req)
	if err != nil {
		if r.status == http.StatusNotFound || r.status == http.StatusMethodNotAllowed {
			return // unknown routes are expected to be absent from the contract
		}
		t.Fatalf("%s %s is not documented in docs/openapi.yaml: %v", method, path, err)
	}
	in := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route},
		Status:                 r.status,
		Header:                 r.header,
		Options: &openapi3filter.Options{
			IncludeResponseStatus:   true,
			AuthenticationFunc:      openapi3filter.NoopAuthenticationFunc,
			SchemaValidationOptions: []openapi3.SchemaValidationOption{openapi3.EnableJSONSchema2020()},
		},
	}
	in.SetBodyBytes(r.body)
	if err := openapi3filter.ValidateResponse(context.Background(), in); err != nil {
		t.Fatalf("%s %s -> %d does not match docs/openapi.yaml: %v\nbody: %s", method, path, r.status, err, r.body)
	}
}
