-- Balance per account (INV-09): opening + Σ IN − Σ OUT over all movements (reversals included).
-- name: ListAccounts :many
SELECT a.id, a.name, a.type, a.ownership, a.owner_member_id, a.opening_balance_cents, a.opened_on,
       a.is_active, a.created_at,
       (a.opening_balance_cents + COALESCE(m.net, 0))::bigint AS balance_cents
FROM accounts a
LEFT JOIN (
    SELECT mv.account_id, SUM(CASE WHEN mv.direction = 'IN' THEN mv.amount_cents ELSE -mv.amount_cents END) AS net
    FROM account_movements mv
    WHERE mv.household_id = @household_id
    GROUP BY mv.account_id
) m ON m.account_id = a.id
WHERE a.household_id = @household_id AND (@include_archived::boolean OR a.is_active)
ORDER BY a.is_active DESC, lower(a.name), a.id;

-- name: GetAccount :one
SELECT a.id, a.name, a.type, a.ownership, a.owner_member_id, a.opening_balance_cents, a.opened_on,
       a.is_active, a.created_at,
       (a.opening_balance_cents + COALESCE((
           SELECT SUM(CASE WHEN mv.direction = 'IN' THEN mv.amount_cents ELSE -mv.amount_cents END)
           FROM account_movements mv
           WHERE mv.household_id = a.household_id AND mv.account_id = a.id
       ), 0))::bigint AS balance_cents
FROM accounts a
WHERE a.household_id = @household_id AND a.id = @id;

-- Locks the account row: serialises archiving against concurrent changes.
-- name: LockAccount :one
SELECT id, is_active FROM accounts WHERE household_id = @household_id AND id = @id FOR UPDATE;

-- name: CountAccounts :one
SELECT count(*) FROM accounts WHERE household_id = @household_id;

-- name: CreateAccount :exec
INSERT INTO accounts (id, household_id, name, type, ownership, owner_member_id, opening_balance_cents, opened_on)
VALUES (@id, @household_id, @name, @type, @ownership, sqlc.narg(owner_member_id), @opening_balance_cents, @opened_on);

-- name: UpdateAccount :exec
UPDATE accounts
SET name = @name, type = @type, ownership = @ownership, owner_member_id = sqlc.narg(owner_member_id),
    opening_balance_cents = @opening_balance_cents, opened_on = @opened_on, is_active = @is_active,
    updated_at = now()
WHERE household_id = @household_id AND id = @id;

-- Statement, newest first, keyset-paginated on (posted_on, id).
-- name: ListMovements :many
SELECT id, direction, amount_cents, posted_on, kind, transfer_id, reversal_of_id, description, created_at
FROM account_movements
WHERE household_id = @household_id AND account_id = @account_id
  AND (sqlc.narg(from_date)::date IS NULL OR posted_on >= sqlc.narg(from_date)::date)
  AND (sqlc.narg(to_date)::date IS NULL OR posted_on <= sqlc.narg(to_date)::date)
  AND (sqlc.narg(after_posted_on)::date IS NULL
       OR (posted_on, id) < (sqlc.narg(after_posted_on)::date, sqlc.narg(after_id)::uuid))
ORDER BY posted_on DESC, id DESC
LIMIT @row_limit;

-- name: CreateTransfer :exec
INSERT INTO transfers (id, household_id, from_account_id, to_account_id, amount_cents, posted_on, description, created_by_member_id)
VALUES (@id, @household_id, @from_account_id, @to_account_id, @amount_cents, @posted_on, @description, @created_by_member_id);

-- name: CreateMovement :exec
INSERT INTO account_movements (id, household_id, account_id, direction, amount_cents, posted_on, kind, transfer_id, reversal_of_id, description)
VALUES (@id, @household_id, @account_id, @direction, @amount_cents, @posted_on, @kind,
        sqlc.narg(transfer_id), sqlc.narg(reversal_of_id), @description);

-- Transfers newest first, keyset-paginated on (posted_on, id); optional account filter
-- matches either side.
-- name: ListTransfers :many
SELECT t.id, t.from_account_id, fa.name AS from_account_name, t.to_account_id, ta.name AS to_account_name,
       t.amount_cents, t.posted_on, t.description, t.created_by_member_id, t.created_at
FROM transfers t
JOIN accounts fa ON fa.household_id = t.household_id AND fa.id = t.from_account_id
JOIN accounts ta ON ta.household_id = t.household_id AND ta.id = t.to_account_id
WHERE t.household_id = @household_id
  AND (sqlc.narg(account_id)::uuid IS NULL
       OR t.from_account_id = sqlc.narg(account_id)::uuid OR t.to_account_id = sqlc.narg(account_id)::uuid)
  AND (sqlc.narg(from_date)::date IS NULL OR t.posted_on >= sqlc.narg(from_date)::date)
  AND (sqlc.narg(to_date)::date IS NULL OR t.posted_on <= sqlc.narg(to_date)::date)
  AND (sqlc.narg(after_posted_on)::date IS NULL
       OR (t.posted_on, t.id) < (sqlc.narg(after_posted_on)::date, sqlc.narg(after_id)::uuid))
ORDER BY t.posted_on DESC, t.id DESC
LIMIT @row_limit;

-- name: GetTransfer :one
SELECT t.id, t.from_account_id, fa.name AS from_account_name, t.to_account_id, ta.name AS to_account_name,
       t.amount_cents, t.posted_on, t.description, t.created_by_member_id, t.created_at
FROM transfers t
JOIN accounts fa ON fa.household_id = t.household_id AND fa.id = t.from_account_id
JOIN accounts ta ON ta.household_id = t.household_id AND ta.id = t.to_account_id
WHERE t.household_id = @household_id AND t.id = @id;

-- A movement about to be reversed, with whether it already was (INV-18).
-- name: GetMovementForReversal :one
SELECT m.id, m.account_id, m.direction, m.amount_cents, m.posted_on, m.kind, m.description,
       EXISTS (SELECT 1 FROM account_movements r
               WHERE r.household_id = m.household_id AND r.reversal_of_id = m.id) AS reversed
FROM account_movements m
WHERE m.household_id = @household_id AND m.id = @id;
