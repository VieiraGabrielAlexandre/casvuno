-- name: ListCards :many
SELECT id, name, holder_member_id, last_four, limit_cents, closing_day, due_day, is_active, created_at, updated_at
FROM credit_cards
WHERE household_id = @household_id AND (@include_archived::boolean OR is_active)
ORDER BY is_active DESC, lower(name), id;

-- name: GetCard :one
SELECT id, name, holder_member_id, last_four, limit_cents, closing_day, due_day, is_active, created_at, updated_at
FROM credit_cards
WHERE household_id = @household_id AND id = @id;

-- Locks the card row: serialises edits and archiving (and, from F4.3, purchases).
-- name: LockCard :one
SELECT id FROM credit_cards WHERE household_id = @household_id AND id = @id FOR UPDATE;

-- name: CountCards :one
SELECT count(*) FROM credit_cards WHERE household_id = @household_id;

-- name: CreateCard :exec
INSERT INTO credit_cards (id, household_id, name, holder_member_id, last_four, limit_cents, closing_day, due_day)
VALUES (@id, @household_id, @name, @holder_member_id, sqlc.narg(last_four), @limit_cents, @closing_day, @due_day);

-- name: UpdateCard :exec
UPDATE credit_cards
SET name = @name, holder_member_id = @holder_member_id, last_four = sqlc.narg(last_four), limit_cents = @limit_cents,
    closing_day = @closing_day, due_day = @due_day, is_active = @is_active, updated_at = now()
WHERE household_id = @household_id AND id = @id;
