-- name: CreateUser :one
INSERT INTO users (id, name, email, password_hash)
VALUES (@id, @name, @email, @password_hash)
RETURNING id, name, email, created_at;

-- name: GetUserByEmail :one
SELECT id, name, email, password_hash, is_active
FROM users
WHERE email = @email;

-- name: UpdateUserPasswordHash :execrows
UPDATE users
SET password_hash = @password_hash, updated_at = now()
WHERE id = @id;

-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, token_hash, expires_at, absolute_expires_at, last_seen_at)
VALUES (@id, @user_id, @token_hash, @expires_at, @absolute_expires_at, @last_seen_at);

-- name: GetActiveSession :one
SELECT s.id, s.user_id, s.expires_at, s.absolute_expires_at, s.last_seen_at, u.name, u.email
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = @token_hash
  AND s.revoked_at IS NULL
  AND s.expires_at > @now
  AND u.is_active;

-- name: TouchSession :exec
UPDATE sessions
SET last_seen_at = @last_seen_at, expires_at = @expires_at, updated_at = now()
WHERE id = @id AND revoked_at IS NULL;

-- name: RevokeSession :exec
UPDATE sessions
SET revoked_at = @now::timestamptz, updated_at = now()
WHERE token_hash = @token_hash AND revoked_at IS NULL;

-- name: RevokeUserSessions :execrows
UPDATE sessions
SET revoked_at = @now::timestamptz, updated_at = now()
WHERE user_id = @user_id AND revoked_at IS NULL;
