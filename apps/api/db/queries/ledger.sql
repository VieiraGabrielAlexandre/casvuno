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

-- Transactions newest first, keyset-paginated on (occurred_on, id) (RN-025). Filters are
-- optional; category_id also matches its subcategories; text searches description, merchant
-- and notes (case-insensitive substring); status NULL means any status; created_by_member_id
-- serves "repeat last" (createdBy=me).
-- name: ListTransactions :many
SELECT t.id, t.kind, t.scope, t.person_member_id, t.created_by_member_id, t.description, t.merchant, t.notes,
       t.amount_cents, t.category_id, t.occurred_on, t.payment_method, t.account_id, t.status, t.created_at,
       t.updated_at
FROM transactions t
WHERE t.household_id = @household_id
  AND (sqlc.narg(status)::text IS NULL OR t.status = sqlc.narg(status)::text)
  AND (sqlc.narg(kind)::text IS NULL OR t.kind = sqlc.narg(kind)::text)
  AND (sqlc.narg(from_date)::date IS NULL OR t.occurred_on >= sqlc.narg(from_date)::date)
  AND (sqlc.narg(to_date)::date IS NULL OR t.occurred_on <= sqlc.narg(to_date)::date)
  AND (sqlc.narg(category_id)::uuid IS NULL
       OR t.category_id = sqlc.narg(category_id)::uuid
       OR t.category_id IN (SELECT c.id FROM categories c
                            WHERE c.household_id = t.household_id AND c.parent_id = sqlc.narg(category_id)::uuid))
  AND (sqlc.narg(account_id)::uuid IS NULL OR t.account_id = sqlc.narg(account_id)::uuid)
  AND (sqlc.narg(created_by_member_id)::uuid IS NULL OR t.created_by_member_id = sqlc.narg(created_by_member_id)::uuid)
  AND (sqlc.narg(person_member_id)::uuid IS NULL OR t.person_member_id = sqlc.narg(person_member_id)::uuid)
  AND (sqlc.narg(scope)::text IS NULL OR t.scope = sqlc.narg(scope)::text)
  AND (sqlc.narg(payment_method)::text IS NULL OR t.payment_method = sqlc.narg(payment_method)::text)
  AND (sqlc.narg(text)::text IS NULL
       OR strpos(lower(t.description || ' ' || COALESCE(t.merchant, '') || ' ' || COALESCE(t.notes, '')),
                 lower(sqlc.narg(text)::text)) > 0)
  AND (sqlc.narg(after_occurred_on)::date IS NULL
       OR (t.occurred_on, t.id) < (sqlc.narg(after_occurred_on)::date, sqlc.narg(after_id)::uuid))
ORDER BY t.occurred_on DESC, t.id DESC
LIMIT @row_limit;

-- Serialises edits and the void of one transaction (ADR 0007).
-- name: LockTransaction :one
SELECT id FROM transactions WHERE household_id = @household_id AND id = @id FOR UPDATE;

-- name: GetObligationIDByTransaction :one
SELECT id FROM obligations WHERE household_id = @household_id AND transaction_id = @transaction_id;

-- Non-structural fields only (I-18): amount, date, method and account change by void + new entry.
-- name: UpdateTransaction :exec
UPDATE transactions
SET description = @description, merchant = sqlc.narg(merchant), notes = sqlc.narg(notes), category_id = @category_id,
    scope = @scope, person_member_id = sqlc.narg(person_member_id), updated_at = now()
WHERE household_id = @household_id AND id = @id AND status = 'ACTIVE';

-- Keeps the confirmed obligation's copy of the classification in step with its transaction.
-- name: UpdateObligationClassification :exec
UPDATE obligations
SET description = @description, category_id = @category_id, scope = @scope,
    person_member_id = sqlc.narg(person_member_id), updated_at = now()
WHERE household_id = @household_id AND id = @id;

-- name: VoidTransaction :exec
UPDATE transactions SET status = 'VOIDED', updated_at = now()
WHERE household_id = @household_id AND id = @id AND status = 'ACTIVE';

-- name: CancelObligation :exec
UPDATE obligations SET status = 'CANCELLED', updated_at = now()
WHERE household_id = @household_id AND id = @id AND status <> 'CANCELLED';

-- Quick templates are private to their member (I-46): every query filters by member too.
-- name: ListQuickTemplates :many
SELECT id, label, kind, description, default_amount_cents, category_id, scope, person_member_id, payment_method,
       account_id, sort_order, created_at, updated_at
FROM quick_templates
WHERE household_id = @household_id AND member_id = @member_id
ORDER BY sort_order, lower(label), id;

-- name: GetQuickTemplate :one
SELECT id, label, kind, description, default_amount_cents, category_id, scope, person_member_id, payment_method,
       account_id, sort_order, created_at, updated_at
FROM quick_templates
WHERE household_id = @household_id AND member_id = @member_id AND id = @id;

-- name: LockQuickTemplate :one
SELECT id FROM quick_templates
WHERE household_id = @household_id AND member_id = @member_id AND id = @id
FOR UPDATE;

-- Serialises template creation per member, so the per-member limit holds under concurrency.
-- name: LockMemberTemplates :exec
SELECT pg_advisory_xact_lock(hashtextextended(@lock_key::text, 0));

-- name: QuickTemplateStats :one
SELECT count(*) AS total, COALESCE(max(sort_order) + 1, 0)::integer AS next_sort_order
FROM quick_templates
WHERE household_id = @household_id AND member_id = @member_id;

-- name: CreateQuickTemplate :exec
INSERT INTO quick_templates (id, household_id, member_id, label, kind, description, default_amount_cents, category_id,
                             scope, person_member_id, payment_method, account_id, sort_order)
VALUES (@id, @household_id, @member_id, @label, @kind, @description, sqlc.narg(default_amount_cents), @category_id,
        @scope, sqlc.narg(person_member_id), @payment_method, sqlc.narg(account_id), @sort_order);

-- name: UpdateQuickTemplate :exec
UPDATE quick_templates
SET label = @label, description = @description, default_amount_cents = sqlc.narg(default_amount_cents),
    category_id = @category_id, scope = @scope, person_member_id = sqlc.narg(person_member_id),
    payment_method = @payment_method, account_id = sqlc.narg(account_id), sort_order = @sort_order, updated_at = now()
WHERE household_id = @household_id AND member_id = @member_id AND id = @id;

-- name: DeleteQuickTemplate :execrows
DELETE FROM quick_templates WHERE household_id = @household_id AND member_id = @member_id AND id = @id;
