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
