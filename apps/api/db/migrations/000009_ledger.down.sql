-- Destroys financial history: never run in production (ADR 0008).
DROP TABLE IF EXISTS obligation_payments;
DROP TABLE IF EXISTS obligations;
DROP TABLE IF EXISTS transactions;
ALTER TABLE categories DROP CONSTRAINT IF EXISTS categories_household_id_id_kind_key;
