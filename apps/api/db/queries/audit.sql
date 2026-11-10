-- name: InsertAuditEvent :exec
INSERT INTO audit_events (id, household_id, actor_user_id, entity_type, entity_id, action, metadata)
VALUES (@id, @household_id, @actor_user_id, @entity_type, @entity_id, @action, @metadata);
