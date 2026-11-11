// Package categories owns income/expense categories: two levels, never deleted (RN-022, I-35).
package categories

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

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/categories/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/kernel"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/audit"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/authctx"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
)

// Kind is the kind of transaction a category classifies.
type Kind string

// Category kinds.
const (
	KindExpense Kind = "EXPENSE"
	KindIncome  Kind = "INCOME"
)

func (k Kind) valid() bool { return k == KindExpense || k == KindIncome }

const (
	// MaxPerHousehold bounds the category list, which is returned without pagination (I-40).
	MaxPerHousehold = 200
	maxNameLen      = 60
	maxIconLen      = 32
	maxSortOrder    = 10000
)

var colorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Category is a household category.
type Category struct {
	ID        uuid.UUID
	ParentID  *uuid.UUID
	Name      string
	Kind      Kind
	Icon      *string
	Color     *string
	IsActive  bool
	SortOrder int
	CreatedAt time.Time
}

// CreateInput is the data for a new category.
type CreateInput struct {
	Name      string
	Kind      Kind
	ParentID  *uuid.UUID
	Icon      *string
	Color     *string
	SortOrder int
}

// UpdateInput holds optional changes. For Icon and Color, an empty string clears the value.
// Kind and parent cannot change: transactions already classified must keep their meaning.
type UpdateInput struct {
	Name      *string
	Icon      *string
	Color     *string
	SortOrder *int
	IsActive  *bool
}

// Service implements category use cases.
type Service struct {
	tx    *postgres.TxManager
	audit *audit.Recorder
}

// NewService builds the categories service.
func NewService(tx *postgres.TxManager, rec *audit.Recorder) *Service {
	return &Service{tx: tx, audit: rec}
}

var errNotFound = kernel.NewError(kernel.ErrNotFound, "CATEGORY_NOT_FOUND", "Categoria não encontrada.")

// SeedDefaults creates the pt-BR starter categories of a new household. It runs inside the
// household-creation transaction (it implements households.CategorySeeder).
func (s *Service) SeedDefaults(ctx context.Context, householdID uuid.UUID) error {
	q := store.New(s.tx.Conn(ctx))
	for i, top := range defaultTree {
		parent, err := s.insert(ctx, q, householdID, nil, top.name, top.kind, i*10)
		if err != nil {
			return fmt.Errorf("seed category %q: %w", top.name, err)
		}
		for j, child := range top.children {
			if _, err := s.insert(ctx, q, householdID, &parent.ID, child, top.kind, j*10); err != nil {
				return fmt.Errorf("seed category %q: %w", child, err)
			}
		}
	}
	return nil
}

func (s *Service) insert(ctx context.Context, q *store.Queries, householdID uuid.UUID, parentID *uuid.UUID, name string, kind Kind, order int) (store.Category, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return store.Category{}, err
	}
	return q.CreateCategory(ctx, store.CreateCategoryParams{
		ID: id, HouseholdID: householdID, ParentID: parentID, Name: name, Kind: string(kind), SortOrder: int32(order), //nolint:gosec // bounded by maxSortOrder
	})
}

// List returns the household categories ordered by kind, sort order and name.
func (s *Service) List(ctx context.Context, m authctx.Membership, kind *Kind, includeArchived bool) ([]Category, error) {
	var k *string
	if kind != nil {
		v := string(*kind)
		k = &v
	}
	rows, err := store.New(s.tx.Conn(ctx)).ListCategories(ctx, store.ListCategoriesParams{
		HouseholdID: m.HouseholdID, Kind: k, IncludeArchived: includeArchived,
	})
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	out := make([]Category, 0, len(rows))
	for _, r := range rows {
		out = append(out, toCategory(r))
	}
	return out, nil
}

// Create adds a category. A child must have an active top-level parent of the same kind.
func (s *Service) Create(ctx context.Context, m authctx.Membership, in CreateInput) (Category, error) {
	name, err := validName(in.Name)
	if err != nil {
		return Category{}, err
	}
	if !in.Kind.valid() {
		return Category{}, validation("kind", "oneof=INCOME EXPENSE", "Tipo de categoria inválido.")
	}
	icon, color, err := validAppearance(in.Icon, in.Color)
	if err != nil {
		return Category{}, err
	}
	if in.SortOrder < 0 || in.SortOrder > maxSortOrder {
		return Category{}, validation("sortOrder", "range=0..10000", "Ordem inválida.")
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Category{}, err
	}

	var c Category
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		n, err := q.CountCategories(ctx, m.HouseholdID)
		if err != nil {
			return fmt.Errorf("count categories: %w", err)
		}
		if n >= MaxPerHousehold {
			return kernel.NewError(kernel.ErrInvariant, "CATEGORY_LIMIT",
				fmt.Sprintf("Limite de %d categorias por família atingido. Arquive ou reutilize categorias.", MaxPerHousehold))
		}
		if in.ParentID != nil {
			if err := checkParent(ctx, q, m.HouseholdID, *in.ParentID, in.Kind); err != nil {
				return err
			}
		}
		row, err := q.CreateCategory(ctx, store.CreateCategoryParams{
			ID: id, HouseholdID: m.HouseholdID, ParentID: in.ParentID, Name: name, Kind: string(in.Kind),
			Icon: icon, Color: color, SortOrder: int32(in.SortOrder), //nolint:gosec // validated range
		})
		if isUniqueViolation(err) {
			return errNameTaken
		}
		if err != nil {
			return fmt.Errorf("create category: %w", err)
		}
		c = toCategory(row)
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "category", EntityID: c.ID, Action: "category.created",
		})
	})
	return c, err
}

// Update renames, restyles, reorders, archives or restores a category. Archiving a parent
// archives its children; a child cannot be restored while its parent is archived.
func (s *Service) Update(ctx context.Context, m authctx.Membership, id uuid.UUID, in UpdateInput) (Category, error) {
	var c Category
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := store.New(s.tx.Conn(ctx))
		cur, err := q.GetCategory(ctx, store.GetCategoryParams{HouseholdID: m.HouseholdID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		}
		if err != nil {
			return fmt.Errorf("get category: %w", err)
		}

		params := store.UpdateCategoryParams{
			HouseholdID: m.HouseholdID, ID: id, ParentID: cur.ParentID, Name: cur.Name,
			Icon: cur.Icon, Color: cur.Color, SortOrder: cur.SortOrder, IsActive: cur.IsActive,
		}
		if in.Name != nil {
			if params.Name, err = validName(*in.Name); err != nil {
				return err
			}
		}
		if in.Icon != nil || in.Color != nil {
			icon, color := cur.Icon, cur.Color
			if in.Icon != nil {
				icon = emptyToNil(*in.Icon)
			}
			if in.Color != nil {
				color = emptyToNil(*in.Color)
			}
			if params.Icon, params.Color, err = validAppearance(icon, color); err != nil {
				return err
			}
		}
		if in.SortOrder != nil {
			if *in.SortOrder < 0 || *in.SortOrder > maxSortOrder {
				return validation("sortOrder", "range=0..10000", "Ordem inválida.")
			}
			params.SortOrder = int32(*in.SortOrder) //nolint:gosec // validated range
		}

		action := "category.updated"
		if in.IsActive != nil && *in.IsActive != cur.IsActive {
			params.IsActive = *in.IsActive
			if params.IsActive {
				action = "category.restored"
				if cur.ParentID != nil {
					parent, err := q.GetCategory(ctx, store.GetCategoryParams{HouseholdID: m.HouseholdID, ID: *cur.ParentID})
					if err != nil {
						return fmt.Errorf("get parent: %w", err)
					}
					if !parent.IsActive {
						return kernel.NewError(kernel.ErrInvariant, "PARENT_ARCHIVED",
							"Restaure primeiro a categoria principal.", kernel.FieldError{Field: "isActive", Reason: "parent_archived"})
					}
				}
			} else {
				action = "category.archived"
				if err := q.ArchiveChildren(ctx, store.ArchiveChildrenParams{HouseholdID: m.HouseholdID, ParentID: id}); err != nil {
					return fmt.Errorf("archive children: %w", err)
				}
			}
		}

		row, err := q.UpdateCategory(ctx, params)
		if isUniqueViolation(err) {
			return errNameTaken
		}
		if err != nil {
			return fmt.Errorf("update category: %w", err)
		}
		c = toCategory(row)
		return s.audit.Record(ctx, audit.Event{
			HouseholdID: m.HouseholdID, ActorUserID: m.UserID,
			EntityType: "category", EntityID: id, Action: action,
		})
	})
	return c, err
}

// Archive hides a category from new entries without deleting it (RN-022). Idempotent.
func (s *Service) Archive(ctx context.Context, m authctx.Membership, id uuid.UUID) error {
	inactive := false
	_, err := s.Update(ctx, m, id, UpdateInput{IsActive: &inactive})
	return err
}

func checkParent(ctx context.Context, q *store.Queries, householdID, parentID uuid.UUID, kind Kind) error {
	parent, err := q.GetCategory(ctx, store.GetCategoryParams{HouseholdID: householdID, ID: parentID})
	if errors.Is(err, pgx.ErrNoRows) {
		// Also covers a parent from another household: it does not exist here (ADR 0006).
		return kernel.NewError(kernel.ErrInvariant, "PARENT_NOT_FOUND", "Categoria principal não encontrada.",
			kernel.FieldError{Field: "parentId", Reason: "not_found"})
	}
	if err != nil {
		return fmt.Errorf("get parent: %w", err)
	}
	switch {
	case parent.ParentID != nil:
		return kernel.NewError(kernel.ErrInvariant, "MAX_DEPTH", "Subcategorias não podem ter subcategorias.",
			kernel.FieldError{Field: "parentId", Reason: "max_depth=2"})
	case Kind(parent.Kind) != kind:
		return kernel.NewError(kernel.ErrInvariant, "KIND_MISMATCH", "A subcategoria deve ter o mesmo tipo da categoria principal.",
			kernel.FieldError{Field: "kind", Reason: "must_match_parent"})
	case !parent.IsActive:
		return kernel.NewError(kernel.ErrInvariant, "PARENT_ARCHIVED", "A categoria principal está arquivada.",
			kernel.FieldError{Field: "parentId", Reason: "archived"})
	}
	return nil
}

var errNameTaken = kernel.NewError(kernel.ErrConflict, "CATEGORY_NAME_TAKEN", "Já existe uma categoria ativa com esse nome neste nível.",
	kernel.FieldError{Field: "name", Reason: "taken"})

func validName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n == 0 || n > maxNameLen {
		return "", validation("name", "length=1..60", "Informe um nome com até 60 caracteres.")
	}
	return name, nil
}

func validAppearance(icon, color *string) (*string, *string, error) {
	if icon != nil {
		if n := utf8.RuneCountInString(*icon); n == 0 || n > maxIconLen {
			return nil, nil, validation("icon", "length=1..32", "Ícone inválido.")
		}
	}
	if color != nil && !colorPattern.MatchString(*color) {
		return nil, nil, validation("color", "hexcolor=#RRGGBB", "Cor inválida. Use o formato #RRGGBB.")
	}
	return icon, color, nil
}

func validation(field, reason, message string) error {
	return kernel.NewError(kernel.ErrValidation, "VALIDATION_ERROR", message, kernel.FieldError{Field: field, Reason: reason})
}

func emptyToNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func toCategory(r store.Category) Category {
	return Category{
		ID: r.ID, ParentID: r.ParentID, Name: r.Name, Kind: Kind(r.Kind), Icon: r.Icon, Color: r.Color,
		IsActive: r.IsActive, SortOrder: int(r.SortOrder), CreatedAt: r.CreatedAt,
	}
}

// LockForEntry returns the kind and active flag of a category that a new financial entry is
// about to reference, holding a share lock until the caller's transaction ends (it must run
// inside TxManager.WithinTx), so the category cannot be archived concurrently. found is
// false for unknown IDs and for categories of other households. It implements the
// consumer-defined ledger.CategoryChecker.
func (s *Service) LockForEntry(ctx context.Context, householdID, id uuid.UUID) (kind string, active, found bool, err error) {
	row, err := store.New(s.tx.Conn(ctx)).LockCategoryForEntry(ctx, store.LockCategoryForEntryParams{HouseholdID: householdID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, fmt.Errorf("lock category: %w", err)
	}
	return row.Kind, row.IsActive, true, nil
}
