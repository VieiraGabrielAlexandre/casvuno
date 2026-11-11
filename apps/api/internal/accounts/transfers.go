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

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/accounts/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/audit"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
)

const (
	defaultTransferDescription = "Transferência"
	maxDescriptionLen          = 200
)

// Transfer is an internal transfer between two accounts of the same household.
type Transfer struct {
	ID                uuid.UUID
	FromAccountID     uuid.UUID
	FromAccountName   string
	ToAccountID       uuid.UUID
	ToAccountName     string
	AmountCents       int64
	PostedOn          time.Time
	Description       string
	CreatedByMemberID uuid.UUID
	CreatedAt         time.Time
}

// TransferInput is a transfer request.
type TransferInput struct {
	FromAccountID uuid.UUID
	ToAccountID   uuid.UUID
	AmountCents   int64
	PostedOn      time.Time
	Description   string
}

// TransferQuery filters and paginates the transfer history.
type TransferQuery struct {
	AccountID *uuid.UUID
	From, To  *time.Time
	After     *StatementPosition
	Limit     int
}

// CreateTransfer moves money between two active accounts of the household (RN-007): one
// transfer row and two TRANSFER movements (OUT on the origin, IN on the destination) in a
// single transaction, so the household cash total never changes (T04). It is not income or
// expense. Balances may go negative: Casvuno records what happened, it does not authorise it.
func (s *Service) CreateTransfer(ctx context.Context, m authctx.Membership, in TransferInput) (Transfer, error) {
	if in.FromAccountID == in.ToAccountID {
		return Transfer{}, kernel.NewError(kernel.ErrInvariant, "SAME_ACCOUNT", "Escolha contas de origem e destino diferentes.",
			kernel.FieldError{Field: "toAccountId", Reason: "must_differ_from_origin"})
	}
	if in.AmountCents <= 0 || in.AmountCents > kernel.MaxAbsCents {
		return Transfer{}, validation("amountCents", "range=1..1e15", "Informe um valor maior que zero.")
	}
	description := strings.TrimSpace(in.Description)
	if description == "" {
		description = defaultTransferDescription
	}
	if utf8.RuneCountInString(description) > maxDescriptionLen {
		return Transfer{}, validation("description", "max=200", "A descrição deve ter no máximo 200 caracteres.")
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Transfer{}, err
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		if err := lockActiveAccounts(ctx, q, m.HouseholdID, in.FromAccountID, in.ToAccountID); err != nil {
			return err
		}
		if err := q.CreateTransfer(ctx, store.CreateTransferParams{
			ID: id, HouseholdID: m.HouseholdID, FromAccountID: in.FromAccountID, ToAccountID: in.ToAccountID,
			AmountCents: in.AmountCents, PostedOn: in.PostedOn, Description: description, CreatedByMemberID: m.MemberID,
		}); err != nil {
			return fmt.Errorf("create transfer: %w", err)
		}
		for _, leg := range []struct {
			account   uuid.UUID
			direction string
		}{{in.FromAccountID, "OUT"}, {in.ToAccountID, "IN"}} {
			legID, err := uuid.NewV7()
			if err != nil {
				return err
			}
			if err := q.CreateMovement(ctx, store.CreateMovementParams{
				ID: legID, HouseholdID: m.HouseholdID, AccountID: leg.account, Direction: leg.direction,
				AmountCents: in.AmountCents, PostedOn: in.PostedOn, Kind: "TRANSFER", TransferID: &id, Description: description,
			}); err != nil {
				return fmt.Errorf("create transfer movement: %w", err)
			}
		}
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "transfer", EntityID: id, Action: "transfer.created",
		})
	})
	if err != nil {
		return Transfer{}, err
	}

	row, err := store.New(s.tx.Conn(ctx)).GetTransfer(ctx, store.GetTransferParams{HouseholdID: m.HouseholdID, ID: id})
	if err != nil {
		return Transfer{}, fmt.Errorf("get transfer: %w", err)
	}
	return Transfer(row), nil
}

// lockActiveAccounts locks both accounts in a fixed order (by ID) so concurrent transfers in
// opposite directions cannot deadlock, and so an account cannot be archived meanwhile.
func lockActiveAccounts(ctx context.Context, q *store.Queries, householdID, from, to uuid.UUID) error {
	type side struct {
		id    uuid.UUID
		field string
	}
	sides := []side{{from, "fromAccountId"}, {to, "toAccountId"}}
	if strings.Compare(to.String(), from.String()) < 0 {
		sides[0], sides[1] = sides[1], sides[0]
	}
	for _, sd := range sides {
		acc, err := q.LockAccount(ctx, store.LockAccountParams{HouseholdID: householdID, ID: sd.id})
		if errors.Is(err, pgx.ErrNoRows) {
			// Also covers accounts of other households (ADR 0006): 422 because the ID came in the body.
			return kernel.NewError(kernel.ErrInvariant, "ACCOUNT_NOT_FOUND", "Conta não encontrada.",
				kernel.FieldError{Field: sd.field, Reason: "not_found"})
		}
		if err != nil {
			return fmt.Errorf("lock account: %w", err)
		}
		if !acc.IsActive {
			return kernel.NewError(kernel.ErrInvariant, "ACCOUNT_ARCHIVED", "Não é possível movimentar uma conta arquivada.",
				kernel.FieldError{Field: sd.field, Reason: "archived"})
		}
	}
	return nil
}

// ListTransfers returns transfers newest first, keyset-paginated, and the next position.
func (s *Service) ListTransfers(ctx context.Context, m authctx.Membership, q TransferQuery) ([]Transfer, *StatementPosition, error) {
	params := store.ListTransfersParams{
		HouseholdID: m.HouseholdID, AccountID: q.AccountID, FromDate: q.From, ToDate: q.To,
		RowLimit: int32(q.Limit + 1), //nolint:gosec // limit validated (<= 100)
	}
	if q.After != nil {
		params.AfterPostedOn, params.AfterID = &q.After.PostedOn, &q.After.ID
	}
	rows, err := store.New(s.tx.Conn(ctx)).ListTransfers(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list transfers: %w", err)
	}
	var next *StatementPosition
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		last := rows[len(rows)-1]
		next = &StatementPosition{PostedOn: last.PostedOn, ID: last.ID}
	}
	out := make([]Transfer, 0, len(rows))
	for _, r := range rows {
		out = append(out, Transfer(r))
	}
	return out, next, nil
}
