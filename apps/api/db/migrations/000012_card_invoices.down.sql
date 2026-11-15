-- Destroys card purchases and invoices: never run in production (ADR 0008).
DROP TABLE IF EXISTS card_installments;
-- Card purchases cannot exist without the card column (account_id becomes NOT NULL again).
DELETE FROM transactions WHERE credit_card_id IS NOT NULL;
DROP INDEX IF EXISTS transactions_household_card_idx;
ALTER TABLE transactions
    DROP CONSTRAINT IF EXISTS transactions_household_id_id_card_key,
    DROP CONSTRAINT IF EXISTS transactions_income_card_check,
    DROP CONSTRAINT IF EXISTS transactions_single_source_check,
    DROP CONSTRAINT IF EXISTS transactions_card_method_check,
    DROP CONSTRAINT IF EXISTS transactions_credit_card_fk,
    DROP CONSTRAINT IF EXISTS transactions_payment_method_check,
    ADD CONSTRAINT transactions_payment_method_check
        CHECK (payment_method IN ('PIX', 'DEBIT', 'CASH', 'BANK_TRANSFER', 'BOLETO', 'OTHER')),
    ALTER COLUMN account_id SET NOT NULL,
    DROP COLUMN IF EXISTS credit_card_id;
DROP TABLE IF EXISTS card_invoices;
