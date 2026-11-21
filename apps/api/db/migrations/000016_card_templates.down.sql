-- Card templates cannot exist without the column: they are deleted (shortcuts, not history).
ALTER TABLE transactions
    DROP CONSTRAINT IF EXISTS transactions_card_installment_count_check,
    DROP COLUMN IF EXISTS installment_count;
DELETE FROM quick_templates WHERE credit_card_id IS NOT NULL;
ALTER TABLE quick_templates
    DROP CONSTRAINT IF EXISTS quick_templates_income_card_check,
    DROP CONSTRAINT IF EXISTS quick_templates_card_account_check,
    DROP CONSTRAINT IF EXISTS quick_templates_card_method_check,
    DROP CONSTRAINT IF EXISTS quick_templates_credit_card_fk,
    DROP CONSTRAINT IF EXISTS quick_templates_payment_method_check,
    ADD CONSTRAINT quick_templates_payment_method_check
        CHECK (payment_method IN ('PIX', 'DEBIT', 'CASH', 'BANK_TRANSFER', 'BOLETO', 'OTHER')),
    DROP COLUMN IF EXISTS credit_card_id;
