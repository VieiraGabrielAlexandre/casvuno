// Package identity owns users, password hashing and sessions (ADR 0005).
package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/identity/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/clock"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/ratelimit"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/securetoken"
)

// Session lifetime (ADR 0005): the session slides while used and dies after SessionIdleTTL
// without use, or SessionAbsoluteTTL after login, whichever comes first.
const (
	SessionIdleTTL     = 30 * 24 * time.Hour
	SessionAbsoluteTTL = 90 * 24 * time.Hour
	// sessionRenewAfter limits session writes to one every few minutes per session.
	sessionRenewAfter = 5 * time.Minute

	minPasswordLen = 10
	maxPasswordLen = 128
)

// InviteChecker tells whether an invite token is currently valid. It lets invited people sign
// up while public registration is disabled; the households module implements it.
type InviteChecker interface {
	InviteIsValid(ctx context.Context, token string) (bool, error)
}

// User is the public view of a user.
type User struct {
	ID    uuid.UUID
	Name  string
	Email string
}

// Session is a newly issued or renewed session.
type Session struct {
	Token     string
	ExpiresAt time.Time
}

// RegisterInput is the sign-up data.
type RegisterInput struct {
	Name        string
	Email       string
	Password    string
	InviteToken string
}

// Service implements registration, login and session handling.
type Service struct {
	tx                  *postgres.TxManager
	clock               clock.Clock
	hasher              *PasswordHasher
	invites             InviteChecker
	registrationEnabled bool
	loginByIP           *ratelimit.Limiter
	loginByEmail        *ratelimit.Limiter

	dummyOnce sync.Once
	dummyHash string
}

// NewService builds the identity service.
func NewService(tx *postgres.TxManager, clk clock.Clock, hasher *PasswordHasher, invites InviteChecker, registrationEnabled bool) *Service {
	return &Service{
		tx:                  tx,
		clock:               clk,
		hasher:              hasher,
		invites:             invites,
		registrationEnabled: registrationEnabled,
		loginByIP:           ratelimit.New(5, time.Minute),
		loginByEmail:        ratelimit.New(5, time.Minute),
	}
}

var errInvalidCredentials = kernel.NewError(kernel.ErrUnauthenticated, "INVALID_CREDENTIALS", "E-mail ou senha inválidos.")

// Register creates a user and opens a session for them.
func (s *Service) Register(ctx context.Context, in RegisterInput) (User, Session, error) {
	name := strings.TrimSpace(in.Name)
	email := normalizeEmail(in.Email)
	if err := validatePassword(in.Password); err != nil {
		return User{}, Session{}, err
	}
	if name == "" {
		return User{}, Session{}, kernel.NewError(kernel.ErrValidation, "VALIDATION_ERROR", "Dados inválidos.",
			kernel.FieldError{Field: "name", Reason: "required"})
	}
	if !s.registrationEnabled {
		ok := false
		if in.InviteToken != "" {
			var err error
			if ok, err = s.invites.InviteIsValid(ctx, in.InviteToken); err != nil {
				return User{}, Session{}, err
			}
		}
		if !ok {
			return User{}, Session{}, kernel.NewError(kernel.ErrForbidden, "REGISTRATION_DISABLED",
				"O cadastro está fechado. Peça um convite a um membro da família.")
		}
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return User{}, Session{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return User{}, Session{}, err
	}

	var (
		user User
		sess Session
	)
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		row, err := store.New(s.tx.Conn(ctx)).CreateUser(ctx, store.CreateUserParams{
			ID: id, Name: name, Email: email, PasswordHash: hash,
		})
		if isUniqueViolation(err) {
			return kernel.NewError(kernel.ErrConflict, "EMAIL_TAKEN", "Este e-mail já está cadastrado.",
				kernel.FieldError{Field: "email", Reason: "taken"})
		}
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		user = User{ID: row.ID, Name: row.Name, Email: row.Email}
		sess, err = s.createSession(ctx, row.ID)
		return err
	})
	return user, sess, err
}

// Login verifies credentials and opens a new session. clientIP is used only for rate limiting.
func (s *Service) Login(ctx context.Context, email, password, clientIP string) (User, Session, error) {
	email = normalizeEmail(email)
	if !s.loginByIP.Allow(clientIP) || !s.loginByEmail.Allow(email) {
		return User{}, Session{}, kernel.NewError(kernel.ErrRateLimited, "RATE_LIMITED",
			"Muitas tentativas de login. Aguarde um minuto e tente novamente.")
	}
	if len(password) > maxPasswordLen*4 { // bound hashing cost on absurd inputs
		return User{}, Session{}, errInvalidCredentials
	}

	q := store.New(s.tx.Conn(ctx))
	row, err := q.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		// Spend the same hashing time as for a real user so timing does not reveal accounts.
		_, _, _ = s.hasher.Verify(password, s.dummy())
		return User{}, Session{}, errInvalidCredentials
	}
	if err != nil {
		return User{}, Session{}, fmt.Errorf("get user: %w", err)
	}

	match, needsRehash, err := s.hasher.Verify(password, row.PasswordHash)
	if err != nil {
		return User{}, Session{}, fmt.Errorf("verify password: %w", err)
	}
	if !match || !row.IsActive {
		return User{}, Session{}, errInvalidCredentials
	}

	var sess Session
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if needsRehash {
			if err := s.updatePassword(ctx, row.ID, password); err != nil {
				return err
			}
		}
		var err error
		sess, err = s.createSession(ctx, row.ID)
		return err
	})
	return User{ID: row.ID, Name: row.Name, Email: row.Email}, sess, err
}

// Logout revokes the session identified by token. Unknown tokens are ignored.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" || len(token) > securetoken.MaxLen {
		return nil
	}
	return store.New(s.tx.Conn(ctx)).RevokeSession(ctx, store.RevokeSessionParams{
		Now: s.clock.Now(), TokenHash: securetoken.Hash(token),
	})
}

// Authenticate resolves a session token. When the session is renewed (sliding expiration),
// the returned *Session carries the new expiry so the caller can refresh the cookie.
func (s *Service) Authenticate(ctx context.Context, token string) (authctx.User, *Session, error) {
	if token == "" {
		return authctx.User{}, nil, kernel.NewError(kernel.ErrUnauthenticated, "UNAUTHENTICATED", "Faça login para continuar.")
	}
	unauthenticated := kernel.NewError(kernel.ErrUnauthenticated, "UNAUTHENTICATED", "Sua sessão expirou. Faça login novamente.")
	if len(token) > securetoken.MaxLen {
		return authctx.User{}, nil, unauthenticated
	}

	now := s.clock.Now()
	q := store.New(s.tx.Conn(ctx))
	row, err := q.GetActiveSession(ctx, store.GetActiveSessionParams{TokenHash: securetoken.Hash(token), Now: now})
	if errors.Is(err, pgx.ErrNoRows) {
		return authctx.User{}, nil, unauthenticated
	}
	if err != nil {
		return authctx.User{}, nil, fmt.Errorf("get session: %w", err)
	}
	user := authctx.User{ID: row.UserID, Name: row.Name, Email: row.Email}

	if now.Sub(row.LastSeenAt) < sessionRenewAfter {
		return user, nil, nil
	}
	expires := minTime(now.Add(SessionIdleTTL), row.AbsoluteExpiresAt)
	if err := q.TouchSession(ctx, store.TouchSessionParams{ID: row.ID, LastSeenAt: now, ExpiresAt: expires}); err != nil {
		return authctx.User{}, nil, fmt.Errorf("renew session: %w", err)
	}
	return user, &Session{Token: token, ExpiresAt: expires}, nil
}

// ResetPassword sets a new password and revokes every session of the user (operator command, I-12).
func (s *Service) ResetPassword(ctx context.Context, email, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		row, err := q.GetUserByEmail(ctx, normalizeEmail(email))
		if errors.Is(err, pgx.ErrNoRows) {
			return kernel.NewError(kernel.ErrNotFound, "USER_NOT_FOUND", "Usuário não encontrado.")
		}
		if err != nil {
			return fmt.Errorf("get user: %w", err)
		}
		if err := s.updatePassword(ctx, row.ID, password); err != nil {
			return err
		}
		_, err = q.RevokeUserSessions(ctx, store.RevokeUserSessionsParams{Now: s.clock.Now(), UserID: row.ID})
		return err
	})
}

func (s *Service) createSession(ctx context.Context, userID uuid.UUID) (Session, error) {
	token, hash, err := securetoken.New()
	if err != nil {
		return Session{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Session{}, err
	}
	now := s.clock.Now()
	expires := now.Add(SessionIdleTTL)
	err = store.New(s.tx.Conn(ctx)).CreateSession(ctx, store.CreateSessionParams{
		ID: id, UserID: userID, TokenHash: hash,
		ExpiresAt: expires, AbsoluteExpiresAt: now.Add(SessionAbsoluteTTL), LastSeenAt: now,
	})
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return Session{Token: token, ExpiresAt: expires}, nil
}

func (s *Service) updatePassword(ctx context.Context, userID uuid.UUID, password string) error {
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return err
	}
	if _, err := store.New(s.tx.Conn(ctx)).UpdateUserPasswordHash(ctx, store.UpdateUserPasswordHashParams{
		ID: userID, PasswordHash: hash,
	}); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	return nil
}

// dummy returns a valid hash used to equalise login timing for unknown e-mails.
func (s *Service) dummy() string {
	s.dummyOnce.Do(func() {
		s.dummyHash, _ = s.hasher.Hash("casvuno-dummy-password")
	})
	return s.dummyHash
}

func validatePassword(p string) error {
	n := utf8.RuneCountInString(p)
	switch {
	case n < minPasswordLen:
		return kernel.NewError(kernel.ErrValidation, "VALIDATION_ERROR", "A senha deve ter pelo menos 10 caracteres.",
			kernel.FieldError{Field: "password", Reason: fmt.Sprintf("min=%d", minPasswordLen)})
	case n > maxPasswordLen:
		return kernel.NewError(kernel.ErrValidation, "VALIDATION_ERROR", "A senha deve ter no máximo 128 caracteres.",
			kernel.FieldError{Field: "password", Reason: fmt.Sprintf("max=%d", maxPasswordLen)})
	}
	return nil
}

func normalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
