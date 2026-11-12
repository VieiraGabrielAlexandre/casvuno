package accounts

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/accounts/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
)

// Movement kinds other modules may post. TRANSFER and REVERSAL are written only by this
// module, through transfers and reversals.
const (
	KindDirectPayment = "DIRECT_PAYMENT"
	KindDirectReceipt = "DIRECT_RECEIPT"

	kindReversal = "REVERSAL"
)

// Directions of a cash movement.
const (
	DirectionIn  = "IN"
	DirectionOut = "OUT"
)

// PostInput is a cash movement requested by another module (e.g. the ledger paying an
// obligation). Field names the request field that carried the account, for error details.
type PostInput struct {
	AccountID   uuid.UUID
	Direction   string
	Kind        string
	AmountCents int64
	PostedOn    time.Time
	Description string
	Field       string
}

var postableKinds = map[string]string{KindDirectPayment: DirectionOut, KindDirectReceipt: DirectionIn}

// PostMovement records one cash movement on an active account of the household and returns
// its ID. It must run inside the caller's transaction (TxManager.WithinTx), so the movement
// and the record that justifies it (an obligation payment) commit together. The account is
// locked, so it cannot be archived concurrently. Balances may go negative (I-27).
func (s *Service) PostMovement(ctx context.Context, householdID uuid.UUID, in PostInput) (uuid.UUID, error) {
	if dir, ok := postableKinds[in.Kind]; !ok || dir != in.Direction {
		return uuid.Nil, fmt.Errorf("post movement: kind %q with direction %q is not postable", in.Kind, in.Direction)
	}
	if in.AmountCents <= 0 || in.AmountCents > kernel.MaxAbsCents {
		return uuid.Nil, fmt.Errorf("post movement: amount out of range")
	}
	description := strings.TrimSpace(in.Description)
	if n := utf8.RuneCountInString(description); n == 0 || n > maxDescriptionLen {
		return uuid.Nil, fmt.Errorf("post movement: invalid description length %d", n)
	}
	field := in.Field
	if field == "" {
		field = "accountId"
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		if err := lockActiveAccount(ctx, q, householdID, in.AccountID, field); err != nil {
			return err
		}
		if err := q.CreateMovement(ctx, store.CreateMovementParams{
			ID: id, HouseholdID: householdID, AccountID: in.AccountID, Direction: in.Direction,
			AmountCents: in.AmountCents, PostedOn: in.PostedOn, Kind: in.Kind, Description: description,
		}); err != nil {
			return fmt.Errorf("create movement: %w", err)
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// RequireActive locks an account of the household (until the caller's transaction ends) and
// requires it to be active, without moving money: e.g. a forecast that names the account it
// will be paid from. field names the request field in errors.
func (s *Service) RequireActive(ctx context.Context, householdID, accountID uuid.UUID, field string) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		return lockActiveAccount(ctx, store.New(s.tx.Conn(ctx)), householdID, accountID, field)
	})
}

// ReverseMovements posts one REVERSAL for each given movement (INV-18): same account and
// amount, opposite direction, dated on (or on the original date, if later: a reversal never
// predates what it undoes). Only direct payments and receipts are reversible here (transfers
// are immutable, I-37). It must run inside the caller's transaction. The accounts are locked
// in a fixed order (by ID, as transfers do) and must be active: money cannot move through an
// archived account, so its owner restores it first.
func (s *Service) ReverseMovements(ctx context.Context, householdID uuid.UUID, movementIDs []uuid.UUID, on time.Time, description string) error {
	description = strings.TrimSpace(description)
	if n := utf8.RuneCountInString(description); n == 0 || n > maxDescriptionLen {
		return fmt.Errorf("reverse movements: invalid description length %d", n)
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		originals := make([]store.GetMovementForReversalRow, 0, len(movementIDs))
		for _, id := range movementIDs {
			mv, err := q.GetMovementForReversal(ctx, store.GetMovementForReversalParams{HouseholdID: householdID, ID: id})
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("reverse movements: movement %s not found", id)
			}
			if err != nil {
				return fmt.Errorf("get movement: %w", err)
			}
			if _, ok := postableKinds[mv.Kind]; !ok {
				return fmt.Errorf("reverse movements: kind %q is not reversible", mv.Kind)
			}
			if mv.Reversed {
				return kernel.NewError(kernel.ErrConflict, "MOVEMENT_ALREADY_REVERSED", "Este movimento já foi estornado.")
			}
			originals = append(originals, mv)
		}

		accountIDs := make([]uuid.UUID, 0, len(originals))
		for _, mv := range originals {
			accountIDs = append(accountIDs, mv.AccountID)
		}
		slices.SortFunc(accountIDs, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		for _, id := range slices.Compact(accountIDs) {
			acc, err := q.LockAccount(ctx, store.LockAccountParams{HouseholdID: householdID, ID: id})
			if err != nil {
				return fmt.Errorf("lock account: %w", err)
			}
			if !acc.IsActive {
				return kernel.NewError(kernel.ErrInvariant, "ACCOUNT_ARCHIVED",
					"O dinheiro voltaria para uma conta arquivada. Restaure a conta antes de cancelar.")
			}
		}

		for _, mv := range originals {
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			direction := DirectionIn
			if mv.Direction == DirectionIn {
				direction = DirectionOut
			}
			postedOn := on
			if mv.PostedOn.After(on) {
				postedOn = mv.PostedOn
			}
			original := mv.ID
			if err := q.CreateMovement(ctx, store.CreateMovementParams{
				ID: id, HouseholdID: householdID, AccountID: mv.AccountID, Direction: direction,
				AmountCents: mv.AmountCents, PostedOn: postedOn, Kind: kindReversal, ReversalOfID: &original,
				Description: description,
			}); err != nil {
				return fmt.Errorf("create reversal: %w", err)
			}
		}
		return nil
	})
}
