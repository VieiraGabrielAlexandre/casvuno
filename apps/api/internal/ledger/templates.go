package ledger

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/ledger/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/audit"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
)

const (
	// MaxTemplatesPerMember bounds the (unpaginated) list of a member's quick templates.
	MaxTemplatesPerMember = 50
	maxLabelLen           = 60
	maxSortOrder          = 10000
)

// QuickTemplate ("favorito") pre-fills an entry: everything but the date, and optionally the
// amount. It belongs to one member of the household and is invisible to the others (I-46).
type QuickTemplate struct {
	ID                 uuid.UUID
	Label              string
	Kind               string
	Description        string
	DefaultAmountCents *int64
	CategoryID         uuid.UUID
	Scope              string
	PersonMemberID     *uuid.UUID
	PaymentMethod      string
	AccountID          *uuid.UUID
	SortOrder          int32
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// TemplateInput is a new quick template. SortOrder nil puts it last.
type TemplateInput struct {
	Label              string
	Kind               string
	Description        string
	DefaultAmountCents *int64
	CategoryID         uuid.UUID
	Scope              string
	PersonMemberID     *uuid.UUID
	PaymentMethod      string
	AccountID          *uuid.UUID
	SortOrder          *int32
}

// TemplateUpdate edits a quick template; nil fields stay. The kind never changes. An empty
// PersonMemberID or AccountID clears it; DefaultAmountCents 0 clears the amount.
type TemplateUpdate struct {
	Label              *string
	Description        *string
	DefaultAmountCents *int64
	CategoryID         *uuid.UUID
	Scope              *string
	PersonMemberID     *string
	PaymentMethod      *string
	AccountID          *string
	SortOrder          *int32
}

var errTemplateNotFound = kernel.NewError(kernel.ErrNotFound, "TEMPLATE_NOT_FOUND", "Favorito não encontrado.")

// ListTemplates returns the member's own quick templates, in their order.
func (s *Service) ListTemplates(ctx context.Context, m authctx.Membership) ([]QuickTemplate, error) {
	rows, err := store.New(s.tx.Conn(ctx)).ListQuickTemplates(ctx, store.ListQuickTemplatesParams{
		HouseholdID: m.HouseholdID, MemberID: m.MemberID,
	})
	if err != nil {
		return nil, fmt.Errorf("list quick templates: %w", err)
	}
	out := make([]QuickTemplate, 0, len(rows))
	for _, r := range rows {
		out = append(out, QuickTemplate(r))
	}
	return out, nil
}

// CreateTemplate stores a quick template for the member. Its references must be usable now
// (active category of the same kind, active account, active member); they are checked again
// when an entry is created from it.
func (s *Service) CreateTemplate(ctx context.Context, m authctx.Membership, in TemplateInput) (QuickTemplate, error) {
	t := QuickTemplate{
		Kind: in.Kind, DefaultAmountCents: in.DefaultAmountCents, CategoryID: in.CategoryID, Scope: in.Scope,
		PersonMemberID: in.PersonMemberID, PaymentMethod: in.PaymentMethod, AccountID: in.AccountID,
	}
	if in.Kind != KindExpense && in.Kind != KindIncome {
		return QuickTemplate{}, validation("kind", "oneof=EXPENSE INCOME", "Tipo de lançamento inválido.")
	}
	var err error
	if t.Label, err = validLabel(in.Label); err != nil {
		return QuickTemplate{}, err
	}
	if t.Description, err = validDescription(in.Description); err != nil {
		return QuickTemplate{}, err
	}
	if err := validTemplate(t, in.SortOrder); err != nil {
		return QuickTemplate{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return QuickTemplate{}, err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		if err := q.LockMemberTemplates(ctx, "quick_templates:"+m.MemberID.String()); err != nil {
			return fmt.Errorf("lock member templates: %w", err)
		}
		stats, err := q.QuickTemplateStats(ctx, store.QuickTemplateStatsParams{HouseholdID: m.HouseholdID, MemberID: m.MemberID})
		if err != nil {
			return fmt.Errorf("count quick templates: %w", err)
		}
		if stats.Total >= MaxTemplatesPerMember {
			return kernel.NewError(kernel.ErrInvariant, "TEMPLATE_LIMIT",
				fmt.Sprintf("Limite de %d favoritos atingido. Exclua algum para criar outro.", MaxTemplatesPerMember))
		}
		t.SortOrder = min(stats.NextSortOrder, maxSortOrder)
		if in.SortOrder != nil {
			t.SortOrder = *in.SortOrder
		}
		if err := s.checkTemplateRefs(ctx, m.HouseholdID, t, true, true, true); err != nil {
			return err
		}
		if err := q.CreateQuickTemplate(ctx, store.CreateQuickTemplateParams{
			ID: id, HouseholdID: m.HouseholdID, MemberID: m.MemberID, Label: t.Label, Kind: t.Kind,
			Description: t.Description, DefaultAmountCents: t.DefaultAmountCents, CategoryID: t.CategoryID, Scope: t.Scope,
			PersonMemberID: t.PersonMemberID, PaymentMethod: t.PaymentMethod, AccountID: t.AccountID, SortOrder: t.SortOrder,
		}); err != nil {
			return fmt.Errorf("create quick template: %w", err)
		}
		return s.recordTemplate(ctx, m, id, "quick_template.created", nil)
	})
	if err != nil {
		return QuickTemplate{}, err
	}
	return s.template(ctx, m, id)
}

// UpdateTemplate edits one of the member's quick templates. Only the references that change
// are checked, so a template keeps working labels even if its category was archived since.
func (s *Service) UpdateTemplate(ctx context.Context, m authctx.Membership, id uuid.UUID, in TemplateUpdate) (QuickTemplate, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		if _, err := q.LockQuickTemplate(ctx, store.LockQuickTemplateParams{HouseholdID: m.HouseholdID, MemberID: m.MemberID, ID: id}); errors.Is(err, pgx.ErrNoRows) {
			return errTemplateNotFound
		} else if err != nil {
			return fmt.Errorf("lock quick template: %w", err)
		}
		cur, err := s.template(ctx, m, id)
		if err != nil {
			return err
		}
		next, err := applyTemplateUpdate(cur, in)
		if err != nil {
			return err
		}
		changed := templateChanges(cur, next)
		if len(changed) == 0 {
			return nil
		}
		if err := s.checkTemplateRefs(ctx, m.HouseholdID, next,
			next.CategoryID != cur.CategoryID, !equalID(next.PersonMemberID, cur.PersonMemberID), !equalID(next.AccountID, cur.AccountID)); err != nil {
			return err
		}
		if err := q.UpdateQuickTemplate(ctx, store.UpdateQuickTemplateParams{
			HouseholdID: m.HouseholdID, MemberID: m.MemberID, ID: id, Label: next.Label, Description: next.Description,
			DefaultAmountCents: next.DefaultAmountCents, CategoryID: next.CategoryID, Scope: next.Scope,
			PersonMemberID: next.PersonMemberID, PaymentMethod: next.PaymentMethod, AccountID: next.AccountID, SortOrder: next.SortOrder,
		}); err != nil {
			return fmt.Errorf("update quick template: %w", err)
		}
		return s.recordTemplate(ctx, m, id, "quick_template.updated", map[string]any{"fields": changed})
	})
	if err != nil {
		return QuickTemplate{}, err
	}
	return s.template(ctx, m, id)
}

// DeleteTemplate removes one of the member's quick templates (a shortcut, not history).
func (s *Service) DeleteTemplate(ctx context.Context, m authctx.Membership, id uuid.UUID) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		n, err := store.New(s.tx.Conn(ctx)).DeleteQuickTemplate(ctx, store.DeleteQuickTemplateParams{
			HouseholdID: m.HouseholdID, MemberID: m.MemberID, ID: id,
		})
		if err != nil {
			return fmt.Errorf("delete quick template: %w", err)
		}
		if n == 0 {
			return errTemplateNotFound
		}
		return s.recordTemplate(ctx, m, id, "quick_template.deleted", nil)
	})
}

func (s *Service) template(ctx context.Context, m authctx.Membership, id uuid.UUID) (QuickTemplate, error) {
	row, err := store.New(s.tx.Conn(ctx)).GetQuickTemplate(ctx, store.GetQuickTemplateParams{
		HouseholdID: m.HouseholdID, MemberID: m.MemberID, ID: id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return QuickTemplate{}, errTemplateNotFound
	}
	if err != nil {
		return QuickTemplate{}, fmt.Errorf("get quick template: %w", err)
	}
	return QuickTemplate(row), nil
}

func (s *Service) recordTemplate(ctx context.Context, m authctx.Membership, id uuid.UUID, action string, meta map[string]any) error {
	return s.audit.Record(ctx, audit.Event{
		HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
		EntityType: "quick_template", EntityID: id, Action: action, Metadata: meta,
	})
}

// checkTemplateRefs checks the selected references against the household (ADR 0006).
func (s *Service) checkTemplateRefs(ctx context.Context, householdID uuid.UUID, t QuickTemplate, category, person, account bool) error {
	ref := CreateInput{Kind: t.Kind, CategoryID: t.CategoryID}
	if person {
		ref.PersonMemberID = t.PersonMemberID
	}
	if category || ref.PersonMemberID != nil {
		if err := s.checkRefs(ctx, householdID, ref, category); err != nil {
			return err
		}
	}
	if account && t.AccountID != nil {
		return s.cash.RequireActiveAccount(ctx, householdID, *t.AccountID, "accountId")
	}
	return nil
}

func applyTemplateUpdate(cur QuickTemplate, in TemplateUpdate) (QuickTemplate, error) {
	next := cur
	var err error
	if in.Label != nil {
		if next.Label, err = validLabel(*in.Label); err != nil {
			return cur, err
		}
	}
	if in.Description != nil {
		if next.Description, err = validDescription(*in.Description); err != nil {
			return cur, err
		}
	}
	if in.DefaultAmountCents != nil {
		next.DefaultAmountCents = nil
		if *in.DefaultAmountCents != 0 {
			next.DefaultAmountCents = in.DefaultAmountCents
		}
	}
	if in.CategoryID != nil {
		next.CategoryID = *in.CategoryID
	}
	if in.Scope != nil {
		next.Scope = *in.Scope
	}
	if in.PaymentMethod != nil {
		next.PaymentMethod = *in.PaymentMethod
	}
	if in.PersonMemberID != nil {
		if next.PersonMemberID, err = clearableID("personMemberId", *in.PersonMemberID); err != nil {
			return cur, err
		}
	}
	if in.AccountID != nil {
		if next.AccountID, err = clearableID("accountId", *in.AccountID); err != nil {
			return cur, err
		}
	}
	if in.SortOrder != nil {
		next.SortOrder = *in.SortOrder
	}
	return next, validTemplate(next, in.SortOrder)
}

// validTemplate checks the shape shared by create and update.
func validTemplate(t QuickTemplate, sortOrder *int32) error {
	if t.DefaultAmountCents != nil {
		if err := validAmount("defaultAmountCents", *t.DefaultAmountCents); err != nil {
			return err
		}
	}
	switch t.Scope {
	case ScopeFamily:
	case ScopePersonal:
		if t.PersonMemberID == nil {
			return validation("personMemberId", "required_for_personal", "Informe a pessoa do lançamento pessoal.")
		}
	default:
		return validation("scope", "oneof=PERSONAL FAMILY", "Escopo inválido.")
	}
	if !accountMethods[t.PaymentMethod] {
		return validation("paymentMethod", "oneof=PIX DEBIT CASH BANK_TRANSFER BOLETO OTHER", "Meio de pagamento inválido.")
	}
	if sortOrder != nil && (*sortOrder < 0 || *sortOrder > maxSortOrder) {
		return validation("sortOrder", fmt.Sprintf("range=0..%d", maxSortOrder), "Ordem inválida.")
	}
	return nil
}

func templateChanges(cur, next QuickTemplate) []string {
	changed := []string{}
	for _, c := range []struct {
		field string
		same  bool
	}{
		{"label", cur.Label == next.Label},
		{"description", cur.Description == next.Description},
		{"defaultAmountCents", equalAmount(cur.DefaultAmountCents, next.DefaultAmountCents)},
		{"categoryId", cur.CategoryID == next.CategoryID},
		{"scope", cur.Scope == next.Scope},
		{"personMemberId", equalID(cur.PersonMemberID, next.PersonMemberID)},
		{"paymentMethod", cur.PaymentMethod == next.PaymentMethod},
		{"accountId", equalID(cur.AccountID, next.AccountID)},
		{"sortOrder", cur.SortOrder == next.SortOrder},
	} {
		if !c.same {
			changed = append(changed, c.field)
		}
	}
	return changed
}

func validLabel(l string) (string, error) {
	l = strings.TrimSpace(l)
	if n := utf8.RuneCountInString(l); n == 0 || n > maxLabelLen {
		return "", validation("label", fmt.Sprintf("length=1..%d", maxLabelLen), fmt.Sprintf("Informe um nome com até %d caracteres.", maxLabelLen))
	}
	return l, nil
}

// clearableID parses an optional ID where "" means "clear".
func clearableID(field, raw string) (*uuid.UUID, error) {
	if raw == "" {
		return nil, nil
	}
	return parseOptionalID(field, raw)
}

func equalAmount(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
