// Package households owns households, memberships (OWNER/MEMBER) and invites (ADR 0006).
package households

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/households/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/audit"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/clock"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/securetoken"
)

const (
	// InviteTTL is how long an invite link stays valid.
	InviteTTL       = 7 * 24 * time.Hour
	defaultTimezone = "America/Sao_Paulo"
	maxNameLen      = 100
)

// Household is a household as seen by one member.
type Household struct {
	ID        uuid.UUID
	Name      string
	Timezone  string
	Currency  string
	CreatedAt time.Time
	MemberID  uuid.UUID
	Role      authctx.Role
}

// Member is a household member.
type Member struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	Name     string
	Email    string
	Role     authctx.Role
	Status   string
	JoinedAt time.Time
}

// Invite is a pending invite (its token is never readable again after creation).
type Invite struct {
	ID        uuid.UUID
	Email     *string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// CreatedInvite is returned once, at creation, with the secret token and the link to share.
type CreatedInvite struct {
	Invite
	Token string
	URL   string
}

// UpdateInput holds optional household changes.
type UpdateInput struct {
	Name     *string
	Timezone *string
}

// CategorySeeder creates the starter categories of a new household inside the creation
// transaction. The categories module implements it.
type CategorySeeder interface {
	SeedDefaults(ctx context.Context, householdID uuid.UUID) error
}

// Service implements household use cases.
type Service struct {
	tx         *postgres.TxManager
	clock      clock.Clock
	audit      *audit.Recorder
	categories CategorySeeder
	appOrigin  string
}

// NewService builds the households service. appOrigin is used to build invite links.
func NewService(tx *postgres.TxManager, clk clock.Clock, rec *audit.Recorder, categories CategorySeeder, appOrigin string) *Service {
	return &Service{tx: tx, clock: clk, audit: rec, categories: categories, appOrigin: appOrigin}
}

var errHouseholdNotFound = kernel.NewError(kernel.ErrNotFound, "HOUSEHOLD_NOT_FOUND", "Família não encontrada.")

// Create creates a household with the caller as its first OWNER.
func (s *Service) Create(ctx context.Context, user authctx.User, name, timezone string) (Household, error) {
	name, err := validName(name)
	if err != nil {
		return Household{}, err
	}
	if timezone == "" {
		timezone = defaultTimezone
	}
	if err := validTimezone(timezone); err != nil {
		return Household{}, err
	}
	householdID, err := uuid.NewV7()
	if err != nil {
		return Household{}, err
	}
	memberID, err := uuid.NewV7()
	if err != nil {
		return Household{}, err
	}

	var h Household
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		row, err := q.CreateHousehold(ctx, store.CreateHouseholdParams{
			ID: householdID, Name: name, Timezone: timezone, CreatedBy: user.ID,
		})
		if err != nil {
			return fmt.Errorf("create household: %w", err)
		}
		if _, err := q.CreateMember(ctx, store.CreateMemberParams{
			ID: memberID, HouseholdID: householdID, UserID: user.ID,
			Role: string(authctx.RoleOwner), JoinedAt: s.clock.Now(),
		}); err != nil {
			return fmt.Errorf("create owner membership: %w", err)
		}
		if err := s.categories.SeedDefaults(ctx, householdID); err != nil {
			return fmt.Errorf("seed default categories: %w", err)
		}
		h = Household{
			ID: row.ID, Name: row.Name, Timezone: row.Timezone, Currency: row.Currency,
			CreatedAt: row.CreatedAt, MemberID: memberID, Role: authctx.RoleOwner,
		}
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: householdID, ActorUserID: user.ID,
			EntityType: "household", EntityID: householdID, Action: "household.created",
		})
	})
	return h, err
}

// ListForUser returns the households where the user is an active member.
func (s *Service) ListForUser(ctx context.Context, userID uuid.UUID) ([]Household, error) {
	rows, err := store.New(s.tx.Conn(ctx)).ListHouseholdsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list households: %w", err)
	}
	out := make([]Household, 0, len(rows))
	for _, r := range rows {
		out = append(out, Household{
			ID: r.ID, Name: r.Name, Timezone: r.Timezone, Currency: r.Currency,
			CreatedAt: r.CreatedAt, MemberID: r.MemberID, Role: authctx.Role(r.Role),
		})
	}
	return out, nil
}

// Membership returns the caller's active membership in a household, or ErrNotFound
// (non-members must not learn that the household exists).
func (s *Service) Membership(ctx context.Context, householdID, userID uuid.UUID) (authctx.Membership, error) {
	row, err := store.New(s.tx.Conn(ctx)).GetActiveMembership(ctx, store.GetActiveMembershipParams{
		HouseholdID: householdID, UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return authctx.Membership{}, errHouseholdNotFound
	}
	if err != nil {
		return authctx.Membership{}, fmt.Errorf("get membership: %w", err)
	}
	return authctx.Membership{
		HouseholdID: householdID, MemberID: row.ID, UserID: userID, Role: authctx.Role(row.Role), Timezone: row.Timezone,
	}, nil
}

// Get returns the household of membership m.
func (s *Service) Get(ctx context.Context, m authctx.Membership) (Household, error) {
	row, err := store.New(s.tx.Conn(ctx)).GetHousehold(ctx, m.HouseholdID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Household{}, errHouseholdNotFound
	}
	if err != nil {
		return Household{}, fmt.Errorf("get household: %w", err)
	}
	return Household{
		ID: row.ID, Name: row.Name, Timezone: row.Timezone, Currency: row.Currency,
		CreatedAt: row.CreatedAt, MemberID: m.MemberID, Role: m.Role,
	}, nil
}

// Update changes the name and/or timezone (OWNER only; enforced by RequireOwner).
func (s *Service) Update(ctx context.Context, m authctx.Membership, in UpdateInput) (Household, error) {
	current, err := s.Get(ctx, m)
	if err != nil {
		return Household{}, err
	}
	name, timezone := current.Name, current.Timezone
	changed := map[string]any{}
	if in.Name != nil {
		if name, err = validName(*in.Name); err != nil {
			return Household{}, err
		}
		changed["name"] = true
	}
	if in.Timezone != nil {
		if err := validTimezone(*in.Timezone); err != nil {
			return Household{}, err
		}
		timezone = *in.Timezone
		changed["timezone"] = timezone
	}

	var h Household
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		row, err := store.New(s.tx.Conn(ctx)).UpdateHousehold(ctx, store.UpdateHouseholdParams{
			ID: m.HouseholdID, Name: name, Timezone: timezone,
		})
		if err != nil {
			return fmt.Errorf("update household: %w", err)
		}
		h = Household{
			ID: row.ID, Name: row.Name, Timezone: row.Timezone, Currency: row.Currency,
			CreatedAt: row.CreatedAt, MemberID: m.MemberID, Role: m.Role,
		}
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "household", EntityID: m.HouseholdID, Action: "household.updated", Metadata: changed,
		})
	})
	return h, err
}

// Members lists all members, active and inactive (history is preserved, RN-023).
func (s *Service) Members(ctx context.Context, m authctx.Membership) ([]Member, error) {
	rows, err := store.New(s.tx.Conn(ctx)).ListMembers(ctx, m.HouseholdID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	out := make([]Member, 0, len(rows))
	for _, r := range rows {
		out = append(out, Member{
			ID: r.ID, UserID: r.UserID, Name: r.Name, Email: r.Email,
			Role: authctx.Role(r.Role), Status: r.Status, JoinedAt: r.JoinedAt,
		})
	}
	return out, nil
}

// DeactivateMember removes a member's access without deleting history. A household always
// keeps at least one active OWNER (INV-17).
func (s *Service) DeactivateMember(ctx context.Context, m authctx.Membership, memberID uuid.UUID) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		if err := q.LockHousehold(ctx, m.HouseholdID); err != nil {
			return fmt.Errorf("lock household: %w", err)
		}
		target, err := q.GetMember(ctx, store.GetMemberParams{HouseholdID: m.HouseholdID, ID: memberID})
		if errors.Is(err, pgx.ErrNoRows) {
			return kernel.NewError(kernel.ErrNotFound, "MEMBER_NOT_FOUND", "Membro não encontrado.")
		}
		if err != nil {
			return fmt.Errorf("get member: %w", err)
		}
		if target.Status != "ACTIVE" {
			return nil // already inactive: idempotent
		}
		if target.Role == string(authctx.RoleOwner) {
			owners, err := q.CountActiveOwners(ctx, m.HouseholdID)
			if err != nil {
				return fmt.Errorf("count owners: %w", err)
			}
			if owners <= 1 {
				return kernel.NewError(kernel.ErrInvariant, "LAST_OWNER",
					"A família precisa de pelo menos um responsável (OWNER) ativo.")
			}
		}
		if err := q.DeactivateMember(ctx, store.DeactivateMemberParams{
			HouseholdID: m.HouseholdID, ID: memberID, Now: s.clock.Now(),
		}); err != nil {
			return fmt.Errorf("deactivate member: %w", err)
		}
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "household_member", EntityID: memberID, Action: "member.deactivated",
		})
	})
}

// CreateInvite issues a single-use invite link (I-12). The e-mail is informative only.
func (s *Service) CreateInvite(ctx context.Context, m authctx.Membership, email *string) (CreatedInvite, error) {
	if email != nil {
		e := strings.ToLower(strings.TrimSpace(*email))
		email = &e
		if e == "" {
			email = nil
		}
	}
	token, hash, err := securetoken.New()
	if err != nil {
		return CreatedInvite{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return CreatedInvite{}, err
	}

	var inv CreatedInvite
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		row, err := store.New(s.tx.Conn(ctx)).CreateInvite(ctx, store.CreateInviteParams{
			ID: id, HouseholdID: m.HouseholdID, Email: email, TokenHash: hash,
			InvitedByMemberID: m.MemberID, ExpiresAt: s.clock.Now().Add(InviteTTL),
		})
		if err != nil {
			return fmt.Errorf("create invite: %w", err)
		}
		inv = CreatedInvite{
			Invite: Invite{ID: row.ID, Email: row.Email, ExpiresAt: row.ExpiresAt, CreatedAt: row.CreatedAt},
			Token:  token,
			URL:    s.appOrigin + "/invite?token=" + url.QueryEscape(token),
		}
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "household_invite", EntityID: row.ID, Action: "invite.created",
			Metadata: map[string]any{"hasEmail": email != nil},
		})
	})
	return inv, err
}

// ListInvites returns pending, unexpired invites.
func (s *Service) ListInvites(ctx context.Context, m authctx.Membership) ([]Invite, error) {
	rows, err := store.New(s.tx.Conn(ctx)).ListPendingInvites(ctx, store.ListPendingInvitesParams{
		HouseholdID: m.HouseholdID, Now: s.clock.Now(),
	})
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	out := make([]Invite, 0, len(rows))
	for _, r := range rows {
		out = append(out, Invite{ID: r.ID, Email: r.Email, ExpiresAt: r.ExpiresAt, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

// RevokeInvite cancels a pending invite of this household.
func (s *Service) RevokeInvite(ctx context.Context, m authctx.Membership, inviteID uuid.UUID) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		n, err := store.New(s.tx.Conn(ctx)).RevokeInvite(ctx, store.RevokeInviteParams{
			HouseholdID: m.HouseholdID, ID: inviteID, Now: s.clock.Now(),
		})
		if err != nil {
			return fmt.Errorf("revoke invite: %w", err)
		}
		if n == 0 {
			return kernel.NewError(kernel.ErrNotFound, "INVITE_NOT_FOUND", "Convite não encontrado.")
		}
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "household_invite", EntityID: inviteID, Action: "invite.revoked",
		})
	})
}

var errInviteInvalid = kernel.NewError(kernel.ErrNotFound, "INVITE_INVALID", "Convite inválido, expirado ou já utilizado.")

// AcceptInvite makes the user a MEMBER of the invite's household and consumes the invite.
// A previously deactivated member is reactivated as MEMBER.
func (s *Service) AcceptInvite(ctx context.Context, user authctx.User, token string) (Household, error) {
	if token == "" || len(token) > securetoken.MaxLen {
		return Household{}, errInviteInvalid
	}
	var m authctx.Membership
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		now := s.clock.Now()
		inv, err := q.GetPendingInviteByToken(ctx, store.GetPendingInviteByTokenParams{
			TokenHash: securetoken.Hash(token), Now: now,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return errInviteInvalid
		}
		if err != nil {
			return fmt.Errorf("get invite: %w", err)
		}
		if err := q.LockHousehold(ctx, inv.HouseholdID); err != nil {
			return fmt.Errorf("lock household: %w", err)
		}

		existing, err := q.GetMemberByUser(ctx, store.GetMemberByUserParams{HouseholdID: inv.HouseholdID, UserID: user.ID})
		switch {
		case err == nil && existing.Status == "ACTIVE":
			return kernel.NewError(kernel.ErrConflict, "ALREADY_MEMBER", "Você já é membro desta família.")
		case err == nil:
			if err := q.ReactivateMember(ctx, store.ReactivateMemberParams{
				HouseholdID: inv.HouseholdID, ID: existing.ID, Now: now,
			}); err != nil {
				return fmt.Errorf("reactivate member: %w", err)
			}
			m = authctx.Membership{HouseholdID: inv.HouseholdID, MemberID: existing.ID, UserID: user.ID, Role: authctx.RoleMember}
		case errors.Is(err, pgx.ErrNoRows):
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			if _, err := q.CreateMember(ctx, store.CreateMemberParams{
				ID: id, HouseholdID: inv.HouseholdID, UserID: user.ID, Role: string(authctx.RoleMember), JoinedAt: now,
			}); err != nil {
				return fmt.Errorf("create member: %w", err)
			}
			m = authctx.Membership{HouseholdID: inv.HouseholdID, MemberID: id, UserID: user.ID, Role: authctx.RoleMember}
		default:
			return fmt.Errorf("get member: %w", err)
		}

		if err := q.MarkInviteAccepted(ctx, store.MarkInviteAcceptedParams{ID: inv.ID, UserID: user.ID, Now: now}); err != nil {
			return fmt.Errorf("accept invite: %w", err)
		}
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: inv.HouseholdID, ActorUserID: user.ID,
			EntityType: "household_invite", EntityID: inv.ID, Action: "invite.accepted",
			Metadata: map[string]any{"memberId": m.MemberID.String()},
		})
	})
	if err != nil {
		return Household{}, err
	}
	return s.Get(ctx, m)
}

// InviteIsValid reports whether token belongs to a pending, unexpired invite. It implements
// identity.InviteChecker (registration while sign-up is closed).
func (s *Service) InviteIsValid(ctx context.Context, token string) (bool, error) {
	if token == "" || len(token) > securetoken.MaxLen {
		return false, nil
	}
	_, err := store.New(s.tx.Conn(ctx)).GetPendingInviteByToken(ctx, store.GetPendingInviteByTokenParams{
		TokenHash: securetoken.Hash(token), Now: s.clock.Now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func validName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n == 0 || n > maxNameLen {
		return "", kernel.NewError(kernel.ErrValidation, "VALIDATION_ERROR", "Informe um nome com até 100 caracteres.",
			kernel.FieldError{Field: "name", Reason: "length=1..100"})
	}
	return name, nil
}

func validTimezone(tz string) error {
	if tz == "" || tz == "Local" || len(tz) > 64 {
		return invalidTimezone()
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return invalidTimezone()
	}
	return nil
}

func invalidTimezone() error {
	return kernel.NewError(kernel.ErrValidation, "VALIDATION_ERROR", "Fuso horário inválido.",
		kernel.FieldError{Field: "timezone", Reason: "iana_timezone"})
}

// IsActiveMember reports whether memberID is an active member of householdID. It implements
// accounts.MemberChecker (owner of INDIVIDUAL accounts).
func (s *Service) IsActiveMember(ctx context.Context, householdID, memberID uuid.UUID) (bool, error) {
	ok, err := store.New(s.tx.Conn(ctx)).IsActiveMember(ctx, store.IsActiveMemberParams{
		HouseholdID: householdID, MemberID: memberID,
	})
	if err != nil {
		return false, fmt.Errorf("check member: %w", err)
	}
	return ok, nil
}
