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
