-- Destroys financial history: never run in production (ADR 0008).
DROP INDEX IF EXISTS account_movements_transfer_idx;
ALTER TABLE account_movements
    DROP CONSTRAINT IF EXISTS account_movements_transfer_kind_chk,
    DROP CONSTRAINT IF EXISTS account_movements_transfer_fk,
    DROP COLUMN IF EXISTS transfer_id;
DROP TABLE IF EXISTS transfers;
