//go:build integration

package app

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/identity"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/config"
)

func TestRegisterLoginMeLogout(t *testing.T) {
	api := newTestAPI(t)
	c := api.newClient()
	email := uniqueEmail("Ana")

	r := c.expect(http.StatusCreated, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"name": "  Ana  ", "email": strings.ToUpper(email), "password": testPassword,
	})
	var u userDTO
	r.decode(t, &u)
	email = strings.ToLower(email)
	if u.Name != "Ana" || u.Email != email {
		t.Fatalf("name/email not normalised: %+v", u)
	}
	assertSessionCookie(t, r, true)

	var me userDTO
	c.expect(http.StatusOK, http.MethodGet, "/api/v1/auth/me", nil).decode(t, &me)
	if me.ID != u.ID {
		t.Fatalf("me = %+v, want %+v", me, u)
	}

	// Logout revokes the session server-side: replaying the old cookie must fail.
	oldToken := c.sessionCookie()
	r = c.expect(http.StatusNoContent, http.MethodPost, "/api/v1/auth/logout", nil)
	if !strings.Contains(r.header.Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("logout must clear the cookie, got %q", r.header.Get("Set-Cookie"))
	}
	replay := api.newClient()
	replay.expect(http.StatusUnauthorized, http.MethodGet, "/api/v1/auth/me", nil, "Cookie", "casvuno_session="+oldToken)

	// Login again issues a different token.
	r = c.expect(http.StatusOK, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": testPassword})
	assertSessionCookie(t, r, true)
	if c.sessionCookie() == oldToken {
		t.Fatal("login must issue a new session token")
	}
	c.expect(http.StatusOK, http.MethodGet, "/api/v1/auth/me", nil)
}

func assertSessionCookie(t *testing.T, r response, wantSet bool) {
	t.Helper()
	sc := r.header.Get("Set-Cookie")
	if !wantSet {
		if sc != "" {
			t.Fatalf("unexpected Set-Cookie %q", sc)
		}
		return
	}
	for _, attr := range []string{"casvuno_session=", "Path=/", "HttpOnly", "SameSite=Lax", "Max-Age="} {
		if !strings.Contains(sc, attr) {
			t.Fatalf("session cookie %q lacks %s", sc, attr)
		}
	}
	if strings.Contains(sc, "Secure") {
		t.Fatalf("cookie over http origin must not be Secure: %q", sc)
	}
}

func TestRegisterValidation(t *testing.T) {
	api := newTestAPI(t)
	c := api.newClient()
	email := uniqueEmail("dup")
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/auth/register", map[string]any{"name": "A", "email": email, "password": testPassword})

	cases := []struct {
		name    string
		body    any
		headers []string
		status  int
		code    string
		field   string
	}{
		{"short password", map[string]any{"name": "B", "email": uniqueEmail("b"), "password": "curta"}, nil, 400, "VALIDATION_ERROR", "password"},
		{"invalid email", map[string]any{"name": "B", "email": "not-an-email", "password": testPassword}, nil, 400, "VALIDATION_ERROR", "email"},
		{"missing name", map[string]any{"email": uniqueEmail("b"), "password": testPassword}, nil, 400, "VALIDATION_ERROR", "name"},
		{"unknown field", map[string]any{"name": "B", "email": uniqueEmail("b"), "password": testPassword, "role": "OWNER"}, nil, 400, "VALIDATION_ERROR", "role"},
		{"duplicate email (case-insensitive)", map[string]any{"name": "B", "email": strings.ToUpper(email), "password": testPassword}, nil, 409, "EMAIL_TAKEN", "email"},
		{"wrong content type", map[string]any{"name": "B"}, []string{"Content-Type", "text/plain"}, 400, "UNSUPPORTED_MEDIA_TYPE", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := api.newClient().expect(tc.status, http.MethodPost, "/api/v1/auth/register", tc.body, tc.headers...)
			var env struct {
				Error struct {
					Code    string `json:"code"`
					Details []struct {
						Field string `json:"field"`
					} `json:"details"`
					RequestID string `json:"requestId"`
				} `json:"error"`
			}
			r.decode(t, &env)
			if env.Error.Code != tc.code || env.Error.RequestID == "" {
				t.Fatalf("error = %+v", env.Error)
			}
			if tc.field != "" && (len(env.Error.Details) == 0 || env.Error.Details[0].Field != tc.field) {
				t.Fatalf("details = %+v, want field %s", env.Error.Details, tc.field)
			}
			assertSessionCookie(t, r, false)
		})
	}
}

func TestLoginDoesNotRevealAccountsAndIsRateLimited(t *testing.T) {
	api := newTestAPI(t)
	_, u := api.registerUser("login")
	c := api.newClient()

	wrongPassword := c.expect(http.StatusUnauthorized, http.MethodPost, "/api/v1/auth/login",
		map[string]any{"email": u.Email, "password": "senha errada 123"})
	unknownEmail := c.expect(http.StatusUnauthorized, http.MethodPost, "/api/v1/auth/login",
		map[string]any{"email": uniqueEmail("ghost"), "password": "senha errada 123"})
	if wrongPassword.errorCode(t) != "INVALID_CREDENTIALS" || unknownEmail.errorCode(t) != "INVALID_CREDENTIALS" {
		t.Fatalf("both failures must be INVALID_CREDENTIALS: %s / %s", wrongPassword.body, unknownEmail.body)
	}

	// 5 attempts per minute per IP: 2 used above, 3 more allowed, the 6th is limited.
	for range 3 {
		c.expect(http.StatusUnauthorized, http.MethodPost, "/api/v1/auth/login",
			map[string]any{"email": u.Email, "password": "senha errada 123"})
	}
	r := c.expect(http.StatusTooManyRequests, http.MethodPost, "/api/v1/auth/login",
		map[string]any{"email": u.Email, "password": testPassword})
	if r.errorCode(t) != "RATE_LIMITED" {
		t.Fatalf("got %s", r.body)
	}
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	api := newTestAPI(t)
	anon := api.newClient()
	hid := "0190f0a0-0000-7000-8000-000000000001"

	for _, rt := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/auth/me"},
		{http.MethodGet, "/api/v1/households"},
		{http.MethodPost, "/api/v1/households"},
		{http.MethodPost, "/api/v1/invites/accept"},
		{http.MethodGet, "/api/v1/households/" + hid},
		{http.MethodPatch, "/api/v1/households/" + hid},
		{http.MethodGet, "/api/v1/households/" + hid + "/members"},
		{http.MethodPost, "/api/v1/households/" + hid + "/invites"},
	} {
		r := anon.expect(http.StatusUnauthorized, rt.method, rt.path, map[string]any{})
		if r.errorCode(t) != "UNAUTHENTICATED" {
			t.Errorf("%s %s: %s", rt.method, rt.path, r.body)
		}
	}

	// A forged cookie is rejected and cleared.
	r := anon.expect(http.StatusUnauthorized, http.MethodGet, "/api/v1/auth/me", nil, "Cookie", "casvuno_session=forged")
	if !strings.Contains(r.header.Get("Set-Cookie"), "Max-Age=0") {
		t.Fatal("invalid session cookie must be cleared")
	}
}

func TestCrossOriginMutationsAreRejected(t *testing.T) {
	api := newTestAPI(t)
	c, _ := api.registerUser("csrf")

	for name, headers := range map[string][]string{
		"foreign origin":         {"Origin", "https://evil.example"},
		"no origin, cross-site":  {"Origin", "", "Sec-Fetch-Site", "cross-site"},
		"no origin, no metadata": {"Origin", ""},
	} {
		t.Run(name, func(t *testing.T) {
			r := c.expect(http.StatusForbidden, http.MethodPost, "/api/v1/households", map[string]any{"name": "X"}, headers...)
			if r.errorCode(t) != "CROSS_ORIGIN_REQUEST" {
				t.Fatalf("got %s", r.body)
			}
		})
	}
	// Same-origin fetch metadata without Origin is accepted; safe methods are never blocked.
	c.expect(http.StatusCreated, http.MethodPost, "/api/v1/households", map[string]any{"name": "Ok"},
		"Origin", "", "Sec-Fetch-Site", "same-origin")
	c.expect(http.StatusOK, http.MethodGet, "/api/v1/households", nil, "Origin", "https://evil.example")
}

func TestSessionSlidingRenewalAndExpiry(t *testing.T) {
	api := newTestAPI(t)
	c, _ := api.registerUser("session")

	// Within the renewal interval: no new cookie.
	api.clock.Advance(time.Minute)
	assertSessionCookie(t, c.expect(http.StatusOK, http.MethodGet, "/api/v1/auth/me", nil), false)

	// After the interval the session slides and the cookie is refreshed (same token).
	token := c.sessionCookie()
	api.clock.Advance(10 * time.Minute)
	assertSessionCookie(t, c.expect(http.StatusOK, http.MethodGet, "/api/v1/auth/me", nil), true)
	if c.sessionCookie() != token {
		t.Fatal("renewal must keep the token")
	}

	// Used every 20 days, the session survives past the idle TTL…
	for range 3 {
		api.clock.Advance(20 * 24 * time.Hour)
		c.expect(http.StatusOK, http.MethodGet, "/api/v1/auth/me", nil)
	}
	// …but never past the absolute lifetime (90 days after login).
	api.clock.Advance(identity.SessionAbsoluteTTL - 60*24*time.Hour)
	c.expect(http.StatusUnauthorized, http.MethodGet, "/api/v1/auth/me", nil, "Cookie", "casvuno_session="+token)

	// Idle expiry: a fresh session unused for longer than the idle TTL is rejected.
	c2, _ := api.registerUser("idle")
	idleToken := c2.sessionCookie()
	api.clock.Advance(identity.SessionIdleTTL + time.Minute)
	c2.expect(http.StatusUnauthorized, http.MethodGet, "/api/v1/auth/me", nil, "Cookie", "casvuno_session="+idleToken)
}

func TestRegistrationDisabledRequiresValidInvite(t *testing.T) {
	// Owner registers while sign-up is open (shared database), then sign-up is closed.
	open := newTestAPI(t)
	owner, _ := open.registerUser("owner")
	h := owner.createHousehold("Casa")
	inv := owner.createInvite(h.ID)

	closed := newTestAPI(t, func(c *config.Config) { c.RegistrationEnabled = false })
	body := func(token string) map[string]any {
		b := map[string]any{"name": "Convidada", "email": uniqueEmail("guest"), "password": testPassword}
		if token != "" {
			b["inviteToken"] = token
		}
		return b
	}
	for _, token := range []string{"", "invalid-token"} {
		r := closed.newClient().expect(http.StatusForbidden, http.MethodPost, "/api/v1/auth/register", body(token))
		if r.errorCode(t) != "REGISTRATION_DISABLED" {
			t.Fatalf("got %s", r.body)
		}
	}

	guest := closed.newClient()
	guest.expect(http.StatusCreated, http.MethodPost, "/api/v1/auth/register", body(inv.Token))
	var joined householdDTO
	guest.expect(http.StatusOK, http.MethodPost, "/api/v1/invites/accept", map[string]any{"token": inv.Token}).decode(t, &joined)
	if joined.ID != h.ID || joined.Role != "MEMBER" {
		t.Fatalf("joined = %+v", joined)
	}
}

func TestOperatorPasswordResetRevokesSessions(t *testing.T) {
	api := newTestAPI(t)
	c, u := api.registerUser("reset")

	if err := api.identity.ResetPassword(context.Background(), u.Email, "uma nova senha forte"); err != nil {
		t.Fatal(err)
	}
	c.expect(http.StatusUnauthorized, http.MethodGet, "/api/v1/auth/me", nil)
	api.newClient().expect(http.StatusUnauthorized, http.MethodPost, "/api/v1/auth/login",
		map[string]any{"email": u.Email, "password": testPassword})
	api.newClient().expect(http.StatusOK, http.MethodPost, "/api/v1/auth/login",
		map[string]any{"email": u.Email, "password": "uma nova senha forte"})
}
