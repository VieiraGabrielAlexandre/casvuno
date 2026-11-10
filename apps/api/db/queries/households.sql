-- name: CreateHousehold :one
INSERT INTO households (id, name, timezone, created_by)
VALUES (@id, @name, @timezone, @created_by)
RETURNING id, name, timezone, currency, created_at;

-- name: GetHousehold :one
SELECT id, name, timezone, currency, created_at
FROM households
WHERE id = @id;

-- Serialises membership changes of one household (e.g. "last owner" checks).
-- name: LockHousehold :exec
SELECT id FROM households WHERE id = @id FOR UPDATE;

-- name: UpdateHousehold :one
UPDATE households
SET name = @name, timezone = @timezone, updated_at = now()
WHERE id = @id
RETURNING id, name, timezone, currency, created_at;

-- name: ListHouseholdsForUser :many
SELECT h.id, h.name, h.timezone, h.currency, h.created_at, m.id AS member_id, m.role
FROM household_members m
JOIN households h ON h.id = m.household_id
WHERE m.user_id = @user_id AND m.status = 'ACTIVE'
ORDER BY h.name, h.id;

-- name: CreateMember :one
INSERT INTO household_members (id, household_id, user_id, role, status, joined_at)
VALUES (@id, @household_id, @user_id, @role, 'ACTIVE', @joined_at)
RETURNING id, role;

-- name: GetActiveMembership :one
SELECT id, role
FROM household_members
WHERE household_id = @household_id AND user_id = @user_id AND status = 'ACTIVE';

-- name: GetMemberByUser :one
SELECT id, role, status
FROM household_members
WHERE household_id = @household_id AND user_id = @user_id
FOR UPDATE;

-- name: GetMember :one
SELECT id, user_id, role, status
FROM household_members
WHERE household_id = @household_id AND id = @id
FOR UPDATE;

-- name: ListMembers :many
SELECT m.id, m.user_id, u.name, u.email, m.role, m.status, m.joined_at
FROM household_members m
JOIN users u ON u.id = m.user_id
WHERE m.household_id = @household_id
ORDER BY m.joined_at, m.id;

-- name: CountActiveOwners :one
SELECT count(*)
FROM household_members
WHERE household_id = @household_id AND role = 'OWNER' AND status = 'ACTIVE';

-- name: DeactivateMember :exec
UPDATE household_members
SET status = 'INACTIVE', deactivated_at = @now::timestamptz, updated_at = now()
WHERE household_id = @household_id AND id = @id AND status = 'ACTIVE';

-- name: ReactivateMember :exec
UPDATE household_members
SET status = 'ACTIVE', role = 'MEMBER', deactivated_at = NULL, joined_at = @now, updated_at = now()
WHERE household_id = @household_id AND id = @id AND status = 'INACTIVE';

-- name: CreateInvite :one
INSERT INTO household_invites (id, household_id, email, token_hash, invited_by_member_id, expires_at)
VALUES (@id, @household_id, @email, @token_hash, @invited_by_member_id, @expires_at)
RETURNING id, email, expires_at, created_at;

-- name: ListPendingInvites :many
SELECT id, email, expires_at, created_at
FROM household_invites
WHERE household_id = @household_id
  AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > @now
ORDER BY created_at DESC, id;

-- name: GetPendingInviteByToken :one
SELECT i.id, i.household_id, h.name AS household_name
FROM household_invites i
JOIN households h ON h.id = i.household_id
WHERE i.token_hash = @token_hash
  AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > @now
FOR UPDATE OF i;

-- name: MarkInviteAccepted :exec
UPDATE household_invites
SET accepted_at = @now::timestamptz, accepted_by_user_id = @user_id::uuid
WHERE id = @id;

-- name: RevokeInvite :execrows
UPDATE household_invites
SET revoked_at = @now::timestamptz
WHERE household_id = @household_id AND id = @id
  AND accepted_at IS NULL AND revoked_at IS NULL;
