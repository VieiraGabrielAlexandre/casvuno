-- On a concurrent insert with the same key, PostgreSQL makes this statement wait until the
-- first transaction ends; ON CONFLICT then returns no row (pgx.ErrNoRows).
-- name: InsertKey :one
INSERT INTO idempotency_keys (id, household_id, user_id, endpoint, key, request_hash, expires_at)
VALUES (@id, @household_id, @user_id, @endpoint, @key, @request_hash, @expires_at)
ON CONFLICT (household_id, user_id, endpoint, key) DO NOTHING
RETURNING id;

-- name: GetKey :one
SELECT id, request_hash, response_status, response_body, expires_at
FROM idempotency_keys
WHERE household_id = @household_id AND user_id = @user_id AND endpoint = @endpoint AND key = @key
FOR UPDATE;

-- name: CompleteKey :exec
UPDATE idempotency_keys
SET response_status = @response_status::smallint, response_body = @response_body, updated_at = now()
WHERE id = @id;

-- name: DeleteKey :exec
DELETE FROM idempotency_keys WHERE id = @id;
