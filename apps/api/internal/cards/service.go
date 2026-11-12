// Package cards owns credit cards, their invoice cycle and, from F4.3 on, invoices and
// installments (ADR 0003). The card never stores the full number, the CVV or credentials.
package cards

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/cards/domain"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/cards/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/audit"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
)

const (
	// MaxPerHousehold bounds the card list, which is returned without pagination (I-49).
	MaxPerHousehold = 50
	maxNameLen      = 60
)

var lastFourPattern = regexp.MustCompile(`^[0-9]{4}$`)

// MemberChecker tells whether a member is active in a household. The households module
// implements it; cards needs it to validate the card holder.
type MemberChecker interface {
	IsActiveMember(ctx context.Context, householdID, memberID uuid.UUID) (bool, error)
}

// Card is a credit card of the household.
type Card struct {
	ID             uuid.UUID
	Name           string
	HolderMemberID uuid.UUID
	LastFour       *string
	LimitCents     int64
	ClosingDay     int
	DueDay         int
	IsActive       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreateInput is the data of a new card.
type CreateInput struct {
	Name           string
	HolderMemberID uuid.UUID
	LastFour       *string
	LimitCents     int64
	ClosingDay     int
	DueDay         int
}

// UpdateInput holds optional changes. LastFour "" clears the digits. New closing or due days
// only affect invoices not created yet (ADR 0003).
type UpdateInput struct {
	Name           *string
	HolderMemberID *uuid.UUID
	LastFour       *string
	LimitCents     *int64
	ClosingDay     *int
	DueDay         *int
	IsActive       *bool
}

// Service implements card use cases.
type Service struct {
	tx      *postgres.TxManager
	audit   *audit.Recorder
	members MemberChecker
}

// NewService builds the cards service.
func NewService(tx *postgres.TxManager, rec *audit.Recorder, members MemberChecker) *Service {
	return &Service{tx: tx, audit: rec, members: members}
}

var errNotFound = kernel.NewError(kernel.ErrNotFound, "CARD_NOT_FOUND", "Cartão não encontrado.")

var errNameTaken = kernel.NewError(kernel.ErrConflict, "CARD_NAME_TAKEN", "Já existe um cartão ativo com esse nome.",
	kernel.FieldError{Field: "name", Reason: "taken"})

// List returns the household's cards, active first, by name.
func (s *Service) List(ctx context.Context, m authctx.Membership, includeArchived bool) ([]Card, error) {
	rows, err := store.New(s.tx.Conn(ctx)).ListCards(ctx, store.ListCardsParams{
		HouseholdID: m.HouseholdID, IncludeArchived: includeArchived,
	})
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}
	out := make([]Card, 0, len(rows))
	for _, r := range rows {
		out = append(out, toCard(store.GetCardRow(r)))
	}
	return out, nil
}

// Get returns one card.
func (s *Service) Get(ctx context.Context, m authctx.Membership, id uuid.UUID) (Card, error) {
	row, err := store.New(s.tx.Conn(ctx)).GetCard(ctx, store.GetCardParams{HouseholdID: m.HouseholdID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Card{}, errNotFound
	}
	if err != nil {
		return Card{}, fmt.Errorf("get card: %w", err)
	}
	return toCard(row), nil
}

// Create registers a card held by an active member of the household.
func (s *Service) Create(ctx context.Context, m authctx.Membership, in CreateInput) (Card, error) {
	name, err := validName(in.Name)
	if err != nil {
		return Card{}, err
	}
	lastFour, err := validLastFour(in.LastFour)
	if err != nil {
		return Card{}, err
	}
	if err := validLimit(in.LimitCents); err != nil {
		return Card{}, err
	}
	if _, err := domain.NewCycle(in.ClosingDay, in.DueDay); err != nil {
		return Card{}, err
	}
	if err := s.checkHolder(ctx, m.HouseholdID, in.HolderMemberID); err != nil {
		return Card{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Card{}, err
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		n, err := q.CountCards(ctx, m.HouseholdID)
		if err != nil {
			return fmt.Errorf("count cards: %w", err)
		}
		if n >= MaxPerHousehold {
			return kernel.NewError(kernel.ErrInvariant, "CARD_LIMIT",
				fmt.Sprintf("Limite de %d cartões por família atingido.", MaxPerHousehold))
		}
		err = q.CreateCard(ctx, store.CreateCardParams{
			ID: id, HouseholdID: m.HouseholdID, Name: name, HolderMemberID: in.HolderMemberID, LastFour: lastFour,
			LimitCents: in.LimitCents, ClosingDay: int16(in.ClosingDay), DueDay: int16(in.DueDay), //nolint:gosec // validated 1..31
		})
		if isUniqueViolation(err) {
			return errNameTaken
		}
		if err != nil {
			return fmt.Errorf("create card: %w", err)
		}
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "credit_card", EntityID: id, Action: "credit_card.created",
		})
	})
	if err != nil {
		return Card{}, err
	}
	return s.Get(ctx, m, id)
}

// Update edits a card. An archived card only accepts being restored.
//
// Archiving will require no open invoice balance once invoices exist (F4.4/F4.5); until then
// a card has no invoices and can always be archived (I-49).
func (s *Service) Update(ctx context.Context, m authctx.Membership, id uuid.UUID, in UpdateInput) (Card, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		if _, err := q.LockCard(ctx, store.LockCardParams{HouseholdID: m.HouseholdID, ID: id}); errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		} else if err != nil {
			return fmt.Errorf("lock card: %w", err)
		}
		cur, err := s.Get(ctx, m, id)
		if err != nil {
			return err
		}

		restoring := in.IsActive != nil && *in.IsActive && !cur.IsActive
		if !cur.IsActive && !restoring {
			return kernel.NewError(kernel.ErrInvariant, "CARD_ARCHIVED", "Restaure o cartão antes de editá-lo.")
		}

		next := cur
		changed := []string{}
		if in.Name != nil {
			if next.Name, err = validName(*in.Name); err != nil {
				return err
			}
			changed = append(changed, "name")
		}
		if in.HolderMemberID != nil {
			if err := s.checkHolder(ctx, m.HouseholdID, *in.HolderMemberID); err != nil {
				return err
			}
			next.HolderMemberID = *in.HolderMemberID
			changed = append(changed, "holderMemberId")
		}
		if in.LastFour != nil {
			next.LastFour = nil
			if *in.LastFour != "" {
				if next.LastFour, err = validLastFour(in.LastFour); err != nil {
					return err
				}
			}
			changed = append(changed, "lastFour")
		}
		if in.LimitCents != nil {
			if err := validLimit(*in.LimitCents); err != nil {
				return err
			}
			next.LimitCents = *in.LimitCents
			changed = append(changed, "limitCents")
		}
		if in.ClosingDay != nil {
			next.ClosingDay = *in.ClosingDay
			changed = append(changed, "closingDay")
		}
		if in.DueDay != nil {
			next.DueDay = *in.DueDay
			changed = append(changed, "dueDay")
		}
		if _, err := domain.NewCycle(next.ClosingDay, next.DueDay); err != nil {
			return err
		}

		action := "credit_card.updated"
		if in.IsActive != nil && *in.IsActive != cur.IsActive {
			next.IsActive = *in.IsActive
			action = "credit_card.restored"
			if !next.IsActive {
				action = "credit_card.archived"
			}
		}

		err = q.UpdateCard(ctx, store.UpdateCardParams{
			HouseholdID: m.HouseholdID, ID: id, Name: next.Name, HolderMemberID: next.HolderMemberID,
			LastFour: next.LastFour, LimitCents: next.LimitCents,
			ClosingDay: int16(next.ClosingDay), DueDay: int16(next.DueDay), //nolint:gosec // validated 1..31
			IsActive: next.IsActive,
		})
		if isUniqueViolation(err) {
			return errNameTaken
		}
		if err != nil {
			return fmt.Errorf("update card: %w", err)
		}
		// Field names only: never the digits or the limit (ADR 0012).
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "credit_card", EntityID: id, Action: action,
			Metadata: map[string]any{"fields": changed},
		})
	})
	if err != nil {
		return Card{}, err
	}
	return s.Get(ctx, m, id)
}

func (s *Service) checkHolder(ctx context.Context, householdID, memberID uuid.UUID) error {
	ok, err := s.members.IsActiveMember(ctx, householdID, memberID)
	if err != nil {
		return err
	}
	if !ok {
		// Also covers members of other households (ADR 0006).
		return kernel.NewError(kernel.ErrInvariant, "HOLDER_NOT_MEMBER", "O titular deve ser um membro ativo da família.",
			kernel.FieldError{Field: "holderMemberId", Reason: "not_active_member"})
	}
	return nil
}

func toCard(r store.GetCardRow) Card {
	return Card{
		ID: r.ID, Name: r.Name, HolderMemberID: r.HolderMemberID, LastFour: r.LastFour, LimitCents: r.LimitCents,
		ClosingDay: int(r.ClosingDay), DueDay: int(r.DueDay), IsActive: r.IsActive,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func validName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n == 0 || n > maxNameLen {
		return "", validation("name", "length=1..60", "Informe um nome com até 60 caracteres.")
	}
	return name, nil
}

// validLastFour accepts nil (not informed) or exactly four digits. Anything longer is
// rejected rather than truncated: the full card number must never reach the database.
func validLastFour(s *string) (*string, error) {
	if s == nil {
		return nil, nil
	}
	if !lastFourPattern.MatchString(*s) {
		return nil, validation("lastFour", "digits=4", "Informe só os 4 últimos dígitos do cartão.")
	}
	v := *s
	return &v, nil
}

func validLimit(cents int64) error {
	if cents < 1 || cents > kernel.MaxAbsCents {
		return validation("limitCents", "range", "Informe um limite maior que zero.")
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
