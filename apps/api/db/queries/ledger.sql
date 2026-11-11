-- name: CreateTransaction :exec
INSERT INTO transactions (id, household_id, kind, scope, person_member_id, created_by_member_id, description,
                          merchant, notes, amount_cents, category_id, occurred_on, payment_method, account_id)
VALUES (@id, @household_id, @kind, @scope, sqlc.narg(person_member_id), @created_by_member_id, @description,
        sqlc.narg(merchant), sqlc.narg(notes), @amount_cents, @category_id, @occurred_on, @payment_method, @account_id);

-- name: CreateObligation :exec
INSERT INTO obligations (id, household_id, transaction_id, kind, source, status, amount_cents, due_on, description,
                         category_id, scope, person_member_id, payment_method, account_id, competence_on,
                         created_by_member_id)
VALUES (@id, @household_id, sqlc.narg(transaction_id), @kind, @source, @status, @amount_cents, @due_on, @description,
        @category_id, @scope, sqlc.narg(person_member_id), @payment_method, sqlc.narg(account_id), @competence_on,
        @created_by_member_id);

-- name: CreateObligationPayment :exec
INSERT INTO obligation_payments (id, household_id, obligation_id, account_movement_id, amount_cents, paid_on,
                                 created_by_member_id)
VALUES (@id, @household_id, @obligation_id, @account_movement_id, @amount_cents, @paid_on, @created_by_member_id);

-- name: GetTransaction :one
SELECT id, kind, scope, person_member_id, created_by_member_id, description, merchant, notes, amount_cents,
       category_id, occurred_on, payment_method, account_id, status, created_at, updated_at
FROM transactions
WHERE household_id = @household_id AND id = @id;

-- Paid amounts are always summed from obligation_payments (I-02): no cached column.
-- name: GetObligation :one
SELECT o.id, o.transaction_id, o.kind, o.source, o.status, o.amount_cents, o.due_on, o.description, o.category_id,
       o.scope, o.person_member_id, o.payment_method, o.account_id, o.competence_on, o.created_by_member_id,
       o.created_at, o.updated_at,
       COALESCE((SELECT sum(p.amount_cents) FROM obligation_payments p
                 WHERE p.household_id = o.household_id AND p.obligation_id = o.id), 0)::bigint AS paid_cents
FROM obligations o
WHERE o.household_id = @household_id AND o.id = @id;

-- Serialises payments and edits of one obligation (ADR 0007): the caller re-reads it after.
-- name: LockObligation :one
SELECT id FROM obligations WHERE household_id = @household_id AND id = @id FOR UPDATE;

-- Obligations by due date (oldest first), keyset-paginated on (due_on, id).
-- only_open: not cancelled and not fully paid; overdue_before: also due before that date.
-- name: ListObligations :many
SELECT o.id, o.transaction_id, o.kind, o.source, o.status, o.amount_cents, o.due_on, o.description, o.category_id,
       o.scope, o.person_member_id, o.payment_method, o.account_id, o.competence_on, o.created_by_member_id,
       o.created_at, o.updated_at, paid.cents AS paid_cents
FROM obligations o
CROSS JOIN LATERAL (
    SELECT COALESCE(sum(p.amount_cents), 0)::bigint AS cents
    FROM obligation_payments p
    WHERE p.household_id = o.household_id AND p.obligation_id = o.id
) paid
WHERE o.household_id = @household_id
  AND (sqlc.narg(kind)::text IS NULL OR o.kind = sqlc.narg(kind)::text)
  AND (sqlc.narg(from_date)::date IS NULL OR o.due_on >= sqlc.narg(from_date)::date)
  AND (sqlc.narg(to_date)::date IS NULL OR o.due_on <= sqlc.narg(to_date)::date)
  AND (NOT @only_open::boolean OR (o.status <> 'CANCELLED' AND paid.cents < o.amount_cents))
  AND (sqlc.narg(overdue_before)::date IS NULL OR o.due_on < sqlc.narg(overdue_before)::date)
  AND (sqlc.narg(after_due_on)::date IS NULL
       OR (o.due_on, o.id) > (sqlc.narg(after_due_on)::date, sqlc.narg(after_id)::uuid))
ORDER BY o.due_on, o.id
LIMIT @row_limit;

-- name: ListObligationPayments :many
SELECT p.id, p.amount_cents, p.paid_on, m.account_id, p.account_movement_id, p.created_by_member_id, p.created_at
FROM obligation_payments p
JOIN account_movements m ON m.household_id = p.household_id AND m.id = p.account_movement_id
WHERE p.household_id = @household_id AND p.obligation_id = @obligation_id
ORDER BY p.paid_on, p.id;

-- The first payment of a forecast confirms it (I-08).
-- name: ConfirmObligation :exec
UPDATE obligations
SET status = 'CONFIRMED', transaction_id = @transaction_id, updated_at = now()
WHERE household_id = @household_id AND id = @id AND status = 'PLANNED';

-- name: UpdatePlannedObligation :exec
UPDATE obligations
SET amount_cents = @amount_cents, due_on = @due_on, description = @description, updated_at = now()
WHERE household_id = @household_id AND id = @id AND status = 'PLANNED';
