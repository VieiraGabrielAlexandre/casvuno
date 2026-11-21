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

-- Locks the card for a purchase (ADR 0007): serialises the find-or-create of its invoices.
-- name: LockCardForPurchase :one
SELECT closing_day, due_day, is_active
FROM credit_cards
WHERE household_id = @household_id AND id = @id
FOR UPDATE;

-- Locks the card's OPEN invoices (oldest first) so none of them is closed while a purchase
-- places installments on it. An invoice closed meanwhile is re-checked and left out.
-- name: LockOpenInvoices :exec
SELECT id
FROM card_invoices
WHERE household_id = @household_id AND credit_card_id = @credit_card_id AND status = 'OPEN'
ORDER BY cycle_start_on
FOR UPDATE;

-- name: ListCardInvoices :many
SELECT id, cycle_start_on, closing_on, due_on, status
FROM card_invoices
WHERE household_id = @household_id AND credit_card_id = @credit_card_id
ORDER BY cycle_start_on;

-- Find-or-create (ADR 0003): the card lock already serialises purchases, so a conflict only
-- means the invoice exists; the caller reads it back and checks its dates.
-- name: CreateInvoice :exec
INSERT INTO card_invoices (id, household_id, credit_card_id, cycle_start_on, closing_on, due_on)
VALUES (@id, @household_id, @credit_card_id, @cycle_start_on, @closing_on, @due_on)
ON CONFLICT (credit_card_id, cycle_start_on) DO NOTHING;

-- name: GetInvoiceByStart :one
SELECT id, cycle_start_on, closing_on, due_on, status
FROM card_invoices
WHERE household_id = @household_id AND credit_card_id = @credit_card_id AND cycle_start_on = @cycle_start_on;

-- name: CreateInstallment :exec
INSERT INTO card_installments (id, household_id, transaction_id, credit_card_id, card_invoice_id, installment_number,
                               installment_total, amount_cents)
VALUES (@id, @household_id, @transaction_id, @credit_card_id, @card_invoice_id, @installment_number,
        @installment_total, @amount_cents);

-- Invoice totals (F4.4): only ACTIVE installments count (INV-06). Payments and adjustments
-- join this sum in F4.5.
-- name: ListInvoices :many
SELECT v.id, v.credit_card_id, v.cycle_start_on, v.closing_on, v.due_on, v.status, v.closed_base_amount_cents,
       v.closed_at, v.closed_by_member_id, v.created_at, v.updated_at,
       totals.installments_cents, totals.installments_count
FROM card_invoices v
CROSS JOIN LATERAL (
    SELECT COALESCE(sum(i.amount_cents), 0)::bigint AS installments_cents, count(*)::int AS installments_count
    FROM card_installments i
    WHERE i.household_id = v.household_id AND i.card_invoice_id = v.id AND i.status = 'ACTIVE'
) totals
WHERE v.household_id = @household_id AND v.credit_card_id = @credit_card_id
  AND (sqlc.narg(from_date)::date IS NULL OR v.due_on >= sqlc.narg(from_date)::date)
  AND (sqlc.narg(to_date)::date IS NULL OR v.due_on <= sqlc.narg(to_date)::date)
  AND (sqlc.narg(after_due_on)::date IS NULL
       OR (v.due_on, v.id) > (sqlc.narg(after_due_on)::date, sqlc.narg(after_id)::uuid))
ORDER BY v.due_on, v.id
LIMIT @row_limit;

-- name: GetInvoice :one
SELECT v.id, v.credit_card_id, v.cycle_start_on, v.closing_on, v.due_on, v.status, v.closed_base_amount_cents,
       v.closed_at, v.closed_by_member_id, v.created_at, v.updated_at,
       totals.installments_cents, totals.installments_count
FROM card_invoices v
CROSS JOIN LATERAL (
    SELECT COALESCE(sum(i.amount_cents), 0)::bigint AS installments_cents, count(*)::int AS installments_count
    FROM card_installments i
    WHERE i.household_id = v.household_id AND i.card_invoice_id = v.id AND i.status = 'ACTIVE'
) totals
WHERE v.household_id = @household_id AND v.id = @id;

-- The installments of one invoice with their purchase (read-only join on the ledger's
-- transactions, which the installments already reference by FK; I-51).
-- name: ListInvoiceInstallments :many
SELECT i.id, i.transaction_id, i.installment_number, i.installment_total, i.amount_cents, i.status,
       t.occurred_on, t.description, t.merchant, t.category_id, t.scope, t.person_member_id,
       t.amount_cents AS purchase_amount_cents
FROM card_installments i
JOIN transactions t ON t.household_id = i.household_id AND t.id = i.transaction_id
WHERE i.household_id = @household_id AND i.card_invoice_id = @card_invoice_id
ORDER BY t.occurred_on, t.id, i.installment_number;

-- Locks the invoice for closing (ADR 0007): purchases lock it too before placing installments.
-- name: LockInvoice :one
SELECT id, status, closing_on
FROM card_invoices
WHERE household_id = @household_id AND id = @id
FOR UPDATE;

-- name: SumActiveInstallments :one
SELECT COALESCE(sum(amount_cents), 0)::bigint
FROM card_installments
WHERE household_id = @household_id AND card_invoice_id = @card_invoice_id AND status = 'ACTIVE';

-- name: CloseInvoice :exec
UPDATE card_invoices
SET status = 'CLOSED', closed_base_amount_cents = @closed_base_amount_cents, closed_at = now(),
    closed_by_member_id = @closed_by_member_id, updated_at = now()
WHERE household_id = @household_id AND id = @id AND status = 'OPEN';
