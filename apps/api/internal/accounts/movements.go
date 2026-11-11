package accounts

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/accounts/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
)

// Movement kinds other modules may post. TRANSFER and REVERSAL are written only by this
// module, through transfers and reversals.
const (
	KindDirectPayment = "DIRECT_PAYMENT"
	KindDirectReceipt = "DIRECT_RECEIPT"
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
