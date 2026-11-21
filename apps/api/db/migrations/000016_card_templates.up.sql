-- F4.7 (I-50): a quick template may be a card purchase, and a card purchase keeps its number
-- of installments on the transaction (ledger data), so "Corrigir" and "Repetir último" can
-- pre-fill it without reading the cards module's tables.

ALTER TABLE quick_templates
    ADD COLUMN credit_card_id uuid,
    DROP CONSTRAINT quick_templates_payment_method_check,
    ADD CONSTRAINT quick_templates_payment_method_check
        CHECK (payment_method IN ('PIX', 'DEBIT', 'CASH', 'BANK_TRANSFER', 'BOLETO', 'CREDIT_CARD', 'OTHER')),
    ADD CONSTRAINT quick_templates_credit_card_fk
        FOREIGN KEY (household_id, credit_card_id) REFERENCES credit_cards (household_id, id) ON DELETE RESTRICT,
    -- the card replaces the account, as in transactions (I-07); income never goes to a card
    ADD CONSTRAINT quick_templates_card_method_check CHECK ((credit_card_id IS NOT NULL) = (payment_method = 'CREDIT_CARD')),
    ADD CONSTRAINT quick_templates_card_account_check CHECK (credit_card_id IS NULL OR account_id IS NULL),
    ADD CONSTRAINT quick_templates_income_card_check CHECK (kind = 'EXPENSE' OR credit_card_id IS NULL);

ALTER TABLE transactions ADD COLUMN installment_count smallint CHECK (installment_count BETWEEN 1 AND 60);

UPDATE transactions t
SET installment_count = (SELECT max(i.installment_total) FROM card_installments i
                         WHERE i.household_id = t.household_id AND i.transaction_id = t.id)
WHERE t.credit_card_id IS NOT NULL;

ALTER TABLE transactions
    ADD CONSTRAINT transactions_card_installment_count_check CHECK ((credit_card_id IS NULL) = (installment_count IS NULL));
