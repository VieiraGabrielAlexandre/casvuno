-- name: ListCategories :many
SELECT *
FROM categories
WHERE household_id = @household_id
  AND (sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind)::text)
  AND (@include_archived::boolean OR is_active)
ORDER BY kind, sort_order, name, id;

-- name: GetCategory :one
SELECT *
FROM categories
WHERE household_id = @household_id AND id = @id
FOR UPDATE;

-- name: CountCategories :one
SELECT count(*) FROM categories WHERE household_id = @household_id;

-- name: CreateCategory :one
INSERT INTO categories (id, household_id, parent_id, name, kind, icon, color, sort_order)
VALUES (@id, @household_id, sqlc.narg(parent_id), @name, @kind, sqlc.narg(icon), sqlc.narg(color), @sort_order)
RETURNING *;

-- name: UpdateCategory :one
UPDATE categories
SET parent_id = sqlc.narg(parent_id), name = @name, icon = sqlc.narg(icon), color = sqlc.narg(color),
    sort_order = @sort_order, is_active = @is_active, updated_at = now()
WHERE household_id = @household_id AND id = @id
RETURNING *;

-- Archiving a parent archives its children too (they cannot outlive it as active).
-- name: ArchiveChildren :exec
UPDATE categories
SET is_active = false, updated_at = now()
WHERE household_id = @household_id AND parent_id = @parent_id::uuid AND is_active;

-- Category of a new financial entry. FOR SHARE: concurrent entries may use it, but it cannot
-- be archived (UPDATE) until the entry commits.
-- name: LockCategoryForEntry :one
SELECT kind, is_active
FROM categories
WHERE household_id = @household_id AND id = @id
FOR SHARE;
