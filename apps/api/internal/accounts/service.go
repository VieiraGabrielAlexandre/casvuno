// Package accounts owns financial accounts and their cash movements (ADR 0002). Balances are
// always derived from movements (INV-09, RN-017); movements are append-only.
package accounts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/accounts/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/audit"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
)

// Account types and ownership values (README §6.2).
const (
	OwnershipIndividual = "INDIVIDUAL"
	OwnershipJoint      = "JOINT"
)

var accountTypes = map[string]bool{"CHECKING": true, "SAVINGS": true, "CASH": true, "OTHER": true}

const (
	// MaxPerHousehold bounds the account list, which is returned without pagination (I-42).
	MaxPerHousehold = 50
	maxNameLen      = 60
)

// MemberChecker tells whether a member is active in a household. The households module
// implements it; accounts needs it to validate the owner of INDIVIDUAL accounts.
type MemberChecker interface {
	IsActiveMember(ctx context.Context, householdID, memberID uuid.UUID) (bool, error)
}

// Account is an account with its derived balance.
type Account struct {
	ID                  uuid.UUID
	Name                string
	Type                string
	Ownership           string
	OwnerMemberID       *uuid.UUID
	OpeningBalanceCents int64
	OpenedOn            time.Time
	IsActive            bool
	CreatedAt           time.Time
	BalanceCents        int64
}

// Movement is one immutable cash movement of a statement.
type Movement struct {
	ID           uuid.UUID
	Direction    string
	AmountCents  int64
	PostedOn     time.Time
	Kind         string
	TransferID   *uuid.UUID
	ReversalOfID *uuid.UUID
	Description  string
	CreatedAt    time.Time
}

// CreateInput is the data of a new account.
type CreateInput struct {
	Name                string
	Type                string
	Ownership           string
	OwnerMemberID       *uuid.UUID
	OpeningBalanceCents int64
	OpenedOn            time.Time
}

// UpdateInput holds optional changes. Setting Ownership to JOINT clears the owner; an
// INDIVIDUAL account needs OwnerMemberID (given now or already set).
type UpdateInput struct {
	Name                *string
	Type                *string
	Ownership           *string
	OwnerMemberID       *uuid.UUID
	OpeningBalanceCents *int64
	OpenedOn            *time.Time
	IsActive            *bool
}

// StatementPosition is the keyset position of the last movement of a page.
type StatementPosition struct {
	PostedOn time.Time
	ID       uuid.UUID
}

// StatementQuery filters and paginates a statement.
type StatementQuery struct {
	From, To *time.Time
	After    *StatementPosition
	Limit    int
}

// Service implements account use cases.
type Service struct {
	tx      *postgres.TxManager
	audit   *audit.Recorder
	members MemberChecker
}

// NewService builds the accounts service.
func NewService(tx *postgres.TxManager, rec *audit.Recorder, members MemberChecker) *Service {
	return &Service{tx: tx, audit: rec, members: members}
}

var errNotFound = kernel.NewError(kernel.ErrNotFound, "ACCOUNT_NOT_FOUND", "Conta não encontrada.")

// List returns the accounts with balances and the consolidated balance of the household:
// every account counted once, individual and joint alike (T21).
func (s *Service) List(ctx context.Context, m authctx.Membership, includeArchived bool) ([]Account, int64, error) {
	rows, err := store.New(s.tx.Conn(ctx)).ListAccounts(ctx, store.ListAccountsParams{
		HouseholdID: m.HouseholdID, IncludeArchived: includeArchived,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list accounts: %w", err)
	}
	out := make([]Account, 0, len(rows))
	var total int64
	for _, r := range rows {
		a := Account(r)
		total += a.BalanceCents
		out = append(out, a)
	}
	return out, total, nil
}

// Get returns one account with its balance.
func (s *Service) Get(ctx context.Context, m authctx.Membership, id uuid.UUID) (Account, error) {
	row, err := store.New(s.tx.Conn(ctx)).GetAccount(ctx, store.GetAccountParams{HouseholdID: m.HouseholdID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, errNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("get account: %w", err)
	}
	return Account(row), nil
}

// Create opens an account. INDIVIDUAL accounts need an active member as owner; JOINT
// accounts have none. Both count in the family balance (README §4).
func (s *Service) Create(ctx context.Context, m authctx.Membership, in CreateInput) (Account, error) {
	name, err := validName(in.Name)
	if err != nil {
		return Account{}, err
	}
	if !accountTypes[in.Type] {
		return Account{}, validation("type", "oneof=CHECKING SAVINGS CASH OTHER", "Tipo de conta inválido.")
	}
	if err := validAmount("openingBalanceCents", in.OpeningBalanceCents); err != nil {
		return Account{}, err
	}
	if err := s.checkOwnership(ctx, m.HouseholdID, in.Ownership, in.OwnerMemberID); err != nil {
		return Account{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Account{}, err
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		n, err := q.CountAccounts(ctx, m.HouseholdID)
		if err != nil {
			return fmt.Errorf("count accounts: %w", err)
		}
		if n >= MaxPerHousehold {
			return kernel.NewError(kernel.ErrInvariant, "ACCOUNT_LIMIT",
				fmt.Sprintf("Limite de %d contas por família atingido.", MaxPerHousehold))
		}
		err = q.CreateAccount(ctx, store.CreateAccountParams{
			ID: id, HouseholdID: m.HouseholdID, Name: name, Type: in.Type, Ownership: in.Ownership,
			OwnerMemberID: ownerFor(in.Ownership, in.OwnerMemberID), OpeningBalanceCents: in.OpeningBalanceCents, OpenedOn: in.OpenedOn,
		})
		if isUniqueViolation(err) {
			return errNameTaken
		}
		if err != nil {
			return fmt.Errorf("create account: %w", err)
		}
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "account", EntityID: id, Action: "account.created",
		})
	})
	if err != nil {
		return Account{}, err
	}
	return s.Get(ctx, m, id)
}

// Update edits an account. An archived account only accepts being restored, and an
// account can only be archived with a zero balance (money must not disappear from totals).
func (s *Service) Update(ctx context.Context, m authctx.Membership, id uuid.UUID, in UpdateInput) (Account, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		if _, err := q.LockAccount(ctx, store.LockAccountParams{HouseholdID: m.HouseholdID, ID: id}); errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		} else if err != nil {
			return fmt.Errorf("lock account: %w", err)
		}
		cur, err := s.Get(ctx, m, id) // balance computed after the lock
		if err != nil {
			return err
		}

		restoring := in.IsActive != nil && *in.IsActive && !cur.IsActive
		if !cur.IsActive && !restoring {
			return kernel.NewError(kernel.ErrInvariant, "ACCOUNT_ARCHIVED", "Restaure a conta antes de editá-la.")
		}

		next := store.UpdateAccountParams{
			HouseholdID: m.HouseholdID, ID: id, Name: cur.Name, Type: cur.Type, Ownership: cur.Ownership,
			OwnerMemberID: cur.OwnerMemberID, OpeningBalanceCents: cur.OpeningBalanceCents, OpenedOn: cur.OpenedOn,
			IsActive: cur.IsActive,
		}
		changed := []string{}
		if in.Name != nil {
			if next.Name, err = validName(*in.Name); err != nil {
				return err
			}
			changed = append(changed, "name")
		}
		if in.Type != nil {
			if !accountTypes[*in.Type] {
				return validation("type", "oneof=CHECKING SAVINGS CASH OTHER", "Tipo de conta inválido.")
			}
			next.Type = *in.Type
			changed = append(changed, "type")
		}
		if in.Ownership != nil || in.OwnerMemberID != nil {
			ownership, owner := cur.Ownership, cur.OwnerMemberID
			if in.Ownership != nil {
				ownership = *in.Ownership
				if ownership == OwnershipJoint {
					owner = nil
				}
			}
			if in.OwnerMemberID != nil {
				owner = in.OwnerMemberID
			}
			if err := s.checkOwnership(ctx, m.HouseholdID, ownership, owner); err != nil {
				return err
			}
			next.Ownership, next.OwnerMemberID = ownership, owner
			changed = append(changed, "ownership")
		}
		if in.OpeningBalanceCents != nil {
			if err := validAmount("openingBalanceCents", *in.OpeningBalanceCents); err != nil {
				return err
			}
			next.OpeningBalanceCents = *in.OpeningBalanceCents
			changed = append(changed, "openingBalanceCents")
		}
		if in.OpenedOn != nil {
			next.OpenedOn = *in.OpenedOn
			changed = append(changed, "openedOn")
		}

		action := "account.updated"
		if in.IsActive != nil && *in.IsActive != cur.IsActive {
			next.IsActive = *in.IsActive
			action = "account.restored"
			if !next.IsActive {
				action = "account.archived"
				// Uses the balance after this request's changes (e.g. a corrected opening balance).
				balance := cur.BalanceCents - cur.OpeningBalanceCents + next.OpeningBalanceCents
				if balance != 0 {
					return kernel.NewError(kernel.ErrInvariant, "ACCOUNT_BALANCE_NOT_ZERO",
						"Só é possível arquivar uma conta com saldo zero. Transfira o saldo antes.")
				}
			}
		}

		err = q.UpdateAccount(ctx, next)
		if isUniqueViolation(err) {
			return errNameTaken
		}
		if err != nil {
			return fmt.Errorf("update account: %w", err)
		}
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "account", EntityID: id, Action: action,
			Metadata: map[string]any{"fields": changed},
		})
	})
	if err != nil {
		return Account{}, err
	}
	return s.Get(ctx, m, id)
}

// Statement returns movements newest first (posted_on DESC, id DESC), keyset-paginated, and
// the position to continue from (nil on the last page).
func (s *Service) Statement(ctx context.Context, m authctx.Membership, accountID uuid.UUID, q StatementQuery) ([]Movement, *StatementPosition, error) {
	if _, err := s.Get(ctx, m, accountID); err != nil {
		return nil, nil, err
	}
	params := store.ListMovementsParams{
		HouseholdID: m.HouseholdID, AccountID: accountID, FromDate: q.From, ToDate: q.To,
		RowLimit: int32(q.Limit + 1), //nolint:gosec // limit validated (<= 100)
	}
	if q.After != nil {
		params.AfterPostedOn, params.AfterID = &q.After.PostedOn, &q.After.ID
	}
	rows, err := store.New(s.tx.Conn(ctx)).ListMovements(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list movements: %w", err)
	}

	var next *StatementPosition
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		last := rows[len(rows)-1]
		next = &StatementPosition{PostedOn: last.PostedOn, ID: last.ID}
	}
	out := make([]Movement, 0, len(rows))
	for _, r := range rows {
		out = append(out, Movement(r))
	}
	return out, next, nil
}

func (s *Service) checkOwnership(ctx context.Context, householdID uuid.UUID, ownership string, owner *uuid.UUID) error {
	switch ownership {
	case OwnershipJoint:
		if owner != nil {
			return validation("ownerMemberId", "must_be_null_for_joint", "Contas conjuntas não têm titular individual.")
		}
		return nil
	case OwnershipIndividual:
		if owner == nil {
			return validation("ownerMemberId", "required_for_individual", "Informe o titular da conta individual.")
		}
		ok, err := s.members.IsActiveMember(ctx, householdID, *owner)
		if err != nil {
			return err
		}
		if !ok {
			// Also covers members of other households (ADR 0006).
			return kernel.NewError(kernel.ErrInvariant, "OWNER_NOT_MEMBER", "O titular deve ser um membro ativo da família.",
				kernel.FieldError{Field: "ownerMemberId", Reason: "not_active_member"})
		}
		return nil
	default:
		return validation("ownership", "oneof=INDIVIDUAL JOINT", "Titularidade inválida.")
	}
}

func ownerFor(ownership string, owner *uuid.UUID) *uuid.UUID {
	if ownership == OwnershipJoint {
		return nil
	}
	return owner
}

var errNameTaken = kernel.NewError(kernel.ErrConflict, "ACCOUNT_NAME_TAKEN", "Já existe uma conta ativa com esse nome.",
	kernel.FieldError{Field: "name", Reason: "taken"})

func validName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n == 0 || n > maxNameLen {
		return "", validation("name", "length=1..60", "Informe um nome com até 60 caracteres.")
	}
	return name, nil
}

func validAmount(field string, cents int64) error {
	if cents < -kernel.MaxAbsCents || cents > kernel.MaxAbsCents {
		return validation(field, "range", "Valor fora do limite permitido.")
	}
	return nil
}

func validation(field, reason, message string) error {
	return kernel.NewError(kernel.ErrValidation, "VALIDATION_ERROR", message, kernel.FieldError{Field: field, Reason: reason})
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
