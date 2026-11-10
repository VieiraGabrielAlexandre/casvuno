-- Destroys financial history: never run in production (ADR 0008).
DROP TABLE IF EXISTS account_movements;
DROP TABLE IF EXISTS accounts;
DROP FUNCTION IF EXISTS forbid_update_delete();
