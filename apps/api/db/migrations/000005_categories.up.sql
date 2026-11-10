-- Categories (README §6.2, RN-022, I-35): two levels, child kind = parent kind, never deleted
-- (archived with is_active = false) so historical transactions keep their category.
CREATE TABLE categories (
    id           uuid PRIMARY KEY,
    household_id uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    parent_id    uuid,
    name         text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    kind         text        NOT NULL CHECK (kind IN ('INCOME', 'EXPENSE')),
    icon         text CHECK (icon IS NULL OR char_length(icon) BETWEEN 1 AND 32),
    color        text CHECK (color IS NULL OR color ~ '^#[0-9A-Fa-f]{6}$'),
    is_active    boolean     NOT NULL DEFAULT true,
    sort_order   integer     NOT NULL DEFAULT 0 CHECK (sort_order BETWEEN 0 AND 10000),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    -- target of composite foreign keys (transactions, recurring rules, quick templates)
    UNIQUE (household_id, id),
    -- the parent must belong to the same household (MATCH SIMPLE: skipped when parent_id is NULL)
    FOREIGN KEY (household_id, parent_id) REFERENCES categories (household_id, id) ON DELETE RESTRICT,
    CHECK (parent_id IS NULL OR parent_id <> id)
);

-- Names are unique among active siblings (case-insensitive).
CREATE UNIQUE INDEX categories_active_sibling_name_idx
    ON categories (household_id, kind, COALESCE(parent_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name))
    WHERE is_active;

CREATE INDEX categories_household_list_idx ON categories (household_id, kind, sort_order, name);
