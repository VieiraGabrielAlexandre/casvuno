-- Cards with what is left to pay on all their invoices, future ones included (RN-018,
-- I-17): the base of the estimated available limit.
-- name: ListCards :many
SELECT c.id, c.name, c.holder_member_id, c.last_four, c.limit_cents, c.closing_day, c.due_day, c.is_active,
       c.created_at, c.updated_at,
       ((SELECT COALESCE(sum(i.amount_cents), 0) FROM card_installments i
         WHERE i.household_id = c.household_id AND i.credit_card_id = c.id AND i.status = 'ACTIVE')
      + (SELECT COALESCE(sum(a.amount_cents), 0) FROM invoice_adjustments a
         JOIN card_invoices v ON v.household_id = a.household_id AND v.id = a.card_invoice_id
         WHERE a.household_id = c.household_id AND v.credit_card_id = c.id)
      - (SELECT COALESCE(sum(p.amount_cents), 0) FROM invoice_payments p
         JOIN card_invoices v ON v.household_id = p.household_id AND v.id = p.card_invoice_id
         WHERE p.household_id = c.household_id AND v.credit_card_id = c.id))::bigint AS outstanding_cents
FROM credit_cards c
WHERE c.household_id = @household_id AND (@include_archived::boolean OR c.is_active)
ORDER BY c.is_active DESC, lower(c.name), c.id;

-- name: GetCard :one
SELECT c.id, c.name, c.holder_member_id, c.last_four, c.limit_cents, c.closing_day, c.due_day, c.is_active,
       c.created_at, c.updated_at,
       ((SELECT COALESCE(sum(i.amount_cents), 0) FROM card_installments i
         WHERE i.household_id = c.household_id AND i.credit_card_id = c.id AND i.status = 'ACTIVE')
      + (SELECT COALESCE(sum(a.amount_cents), 0) FROM invoice_adjustments a
         JOIN card_invoices v ON v.household_id = a.household_id AND v.id = a.card_invoice_id
         WHERE a.household_id = c.household_id AND v.credit_card_id = c.id)
      - (SELECT COALESCE(sum(p.amount_cents), 0) FROM invoice_payments p
         JOIN card_invoices v ON v.household_id = p.household_id AND v.id = p.card_invoice_id
         WHERE p.household_id = c.household_id AND v.credit_card_id = c.id))::bigint AS outstanding_cents
FROM credit_cards c
WHERE c.household_id = @household_id AND c.id = @id;

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

-- Invoice totals (INV-06): total = ACTIVE installments + adjustments; paid = payments.
-- name: ListInvoices :many
SELECT v.id, v.credit_card_id, v.cycle_start_on, v.closing_on, v.due_on, v.status, v.closed_base_amount_cents,
       v.closed_at, v.closed_by_member_id, v.created_at, v.updated_at,
       totals.installments_cents, totals.installments_count, adj.cents AS adjustments_cents, paid.cents AS paid_cents
FROM card_invoices v
CROSS JOIN LATERAL (
    SELECT COALESCE(sum(i.amount_cents), 0)::bigint AS installments_cents, count(*)::int AS installments_count
    FROM card_installments i
    WHERE i.household_id = v.household_id AND i.card_invoice_id = v.id AND i.status = 'ACTIVE'
) totals
CROSS JOIN LATERAL (
    SELECT COALESCE(sum(a.amount_cents), 0)::bigint AS cents
    FROM invoice_adjustments a
    WHERE a.household_id = v.household_id AND a.card_invoice_id = v.id
) adj
CROSS JOIN LATERAL (
    SELECT COALESCE(sum(p.amount_cents), 0)::bigint AS cents
    FROM invoice_payments p
    WHERE p.household_id = v.household_id AND p.card_invoice_id = v.id
) paid
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
       totals.installments_cents, totals.installments_count, adj.cents AS adjustments_cents, paid.cents AS paid_cents
FROM card_invoices v
CROSS JOIN LATERAL (
    SELECT COALESCE(sum(i.amount_cents), 0)::bigint AS installments_cents, count(*)::int AS installments_count
    FROM card_installments i
    WHERE i.household_id = v.household_id AND i.card_invoice_id = v.id AND i.status = 'ACTIVE'
) totals
CROSS JOIN LATERAL (
    SELECT COALESCE(sum(a.amount_cents), 0)::bigint AS cents
    FROM invoice_adjustments a
    WHERE a.household_id = v.household_id AND a.card_invoice_id = v.id
) adj
CROSS JOIN LATERAL (
    SELECT COALESCE(sum(p.amount_cents), 0)::bigint AS cents
    FROM invoice_payments p
    WHERE p.household_id = v.household_id AND p.card_invoice_id = v.id
) paid
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

-- Locks the invoice (ADR 0007) to close it, pay it or adjust it; purchases lock it too before
-- placing installments, so its total never changes under one of these operations.
-- name: LockInvoice :one
SELECT id, credit_card_id, status, closing_on, due_on
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

-- name: ListInvoicePayments :many
SELECT p.id, p.amount_cents, p.paid_on, m.account_id, p.account_movement_id, p.created_by_member_id, p.created_at
FROM invoice_payments p
JOIN account_movements m ON m.household_id = p.household_id AND m.id = p.account_movement_id
WHERE p.household_id = @household_id AND p.card_invoice_id = @card_invoice_id
ORDER BY p.paid_on, p.id;

-- name: ListInvoiceAdjustments :many
SELECT id, amount_cents, reason, created_by_member_id, created_at
FROM invoice_adjustments
WHERE household_id = @household_id AND card_invoice_id = @card_invoice_id
ORDER BY created_at, id;

-- name: CreateInvoicePayment :exec
INSERT INTO invoice_payments (id, household_id, card_invoice_id, account_movement_id, amount_cents, paid_on, created_by_member_id)
VALUES (@id, @household_id, @card_invoice_id, @account_movement_id, @amount_cents, @paid_on, @created_by_member_id);

-- name: CreateInvoiceAdjustment :exec
INSERT INTO invoice_adjustments (id, household_id, card_invoice_id, amount_cents, reason, created_by_member_id)
VALUES (@id, @household_id, @card_invoice_id, @amount_cents, @reason, @created_by_member_id);

-- Shares the card lock (adjustments): archiving (FOR UPDATE) waits, so a card is never
-- archived while an adjustment adds to its balance.
-- name: ShareCard :one
SELECT name, is_active FROM credit_cards WHERE household_id = @household_id AND id = @id FOR SHARE;

-- What is left to pay on all the card's invoices (each one is >= 0, INV-06): archiving
-- requires zero (I-49).
-- name: CardOutstandingCents :one
SELECT ((SELECT COALESCE(sum(i.amount_cents), 0) FROM card_installments i
         WHERE i.household_id = @household_id AND i.credit_card_id = @credit_card_id AND i.status = 'ACTIVE')
      + (SELECT COALESCE(sum(a.amount_cents), 0) FROM invoice_adjustments a
         JOIN card_invoices v ON v.household_id = a.household_id AND v.id = a.card_invoice_id
         WHERE a.household_id = @household_id AND v.credit_card_id = @credit_card_id)
      - (SELECT COALESCE(sum(p.amount_cents), 0) FROM invoice_payments p
         JOIN card_invoices v ON v.household_id = p.household_id AND v.id = p.card_invoice_id
         WHERE p.household_id = @household_id AND v.credit_card_id = @credit_card_id))::bigint;

-- The installments of one purchase, with their invoices (void, F4.6).
-- name: ListPurchaseInstallments :many
SELECT i.id, i.card_invoice_id, i.amount_cents, i.status
FROM card_installments i
WHERE i.household_id = @household_id AND i.transaction_id = @transaction_id
ORDER BY i.installment_number;

-- Locks the invoices of one purchase (oldest first, as purchases do) before its installments
-- are cancelled, so no close, payment or adjustment runs on them meanwhile.
-- name: LockPurchaseInvoices :exec
SELECT v.id
FROM card_invoices v
WHERE v.household_id = @household_id
  AND v.id IN (SELECT i.card_invoice_id FROM card_installments i
               WHERE i.household_id = @household_id AND i.transaction_id = @transaction_id)
ORDER BY v.cycle_start_on
FOR UPDATE;

-- name: CancelPurchaseInstallments :exec
UPDATE card_installments
SET status = 'CANCELLED', updated_at = now()
WHERE household_id = @household_id AND transaction_id = @transaction_id AND status = 'ACTIVE';
