-- Invoice payments and adjustments (F4.5, README §6.5, RN-012/013, ADR 0003).
-- invoice total = Σ ACTIVE installments + Σ adjustments; paid = Σ payments;
-- outstanding = total − paid, never negative (INV-06). Both tables are history: a mistake
-- is corrected by a new row (an adjustment), never by editing or deleting one.

-- A payment is the record that justifies one OUT/INVOICE_PAYMENT cash movement (INV-07).
-- Paying an invoice is never an expense: consumption stays in the purchases (RN-014).
CREATE TABLE invoice_payments (
    id                   uuid PRIMARY KEY,
    household_id         uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    card_invoice_id      uuid        NOT NULL,
    account_movement_id  uuid        NOT NULL UNIQUE,
    amount_cents         bigint      NOT NULL CHECK (amount_cents > 0 AND amount_cents <= 1000000000000000),
    paid_on              date        NOT NULL,
    created_by_member_id uuid        NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, card_invoice_id) REFERENCES card_invoices (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, account_movement_id) REFERENCES account_movements (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, created_by_member_id) REFERENCES household_members (household_id, id) ON DELETE RESTRICT
);

CREATE INDEX invoice_payments_invoice_idx ON invoice_payments (household_id, card_invoice_id);

CREATE TRIGGER invoice_payments_append_only
    BEFORE UPDATE OR DELETE ON invoice_payments
    FOR EACH ROW EXECUTE FUNCTION forbid_update_delete();

-- A signed correction of the invoice balance (fee, interest, refund, statement difference),
-- explained by a reason. It never touches the installments (RN-012).
CREATE TABLE invoice_adjustments (
    id                   uuid PRIMARY KEY,
    household_id         uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    card_invoice_id      uuid        NOT NULL,
    amount_cents         bigint      NOT NULL CHECK (amount_cents <> 0 AND amount_cents BETWEEN -1000000000000000 AND 1000000000000000),
    reason               text        NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 200),
    created_by_member_id uuid        NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, card_invoice_id) REFERENCES card_invoices (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, created_by_member_id) REFERENCES household_members (household_id, id) ON DELETE RESTRICT
);

CREATE INDEX invoice_adjustments_invoice_idx ON invoice_adjustments (household_id, card_invoice_id);

CREATE TRIGGER invoice_adjustments_append_only
    BEFORE UPDATE OR DELETE ON invoice_adjustments
    FOR EACH ROW EXECUTE FUNCTION forbid_update_delete();

-- INV-06 in the database: after a payment or an adjustment the invoice still has
-- 0 <= paid <= total. The service locks the invoice (FOR UPDATE) before inserting; the
-- trigger takes the same lock, so even a bug that skipped it cannot overpay concurrently.
CREATE FUNCTION invoice_balance_guard() RETURNS trigger
    LANGUAGE plpgsql AS
$$
DECLARE
    total_cents bigint;
    paid_cents  bigint;
BEGIN
    PERFORM 1 FROM card_invoices WHERE household_id = NEW.household_id AND id = NEW.card_invoice_id FOR UPDATE;
    SELECT (SELECT COALESCE(sum(amount_cents), 0) FROM card_installments
            WHERE household_id = NEW.household_id AND card_invoice_id = NEW.card_invoice_id AND status = 'ACTIVE')
         + (SELECT COALESCE(sum(amount_cents), 0) FROM invoice_adjustments
            WHERE household_id = NEW.household_id AND card_invoice_id = NEW.card_invoice_id)
      INTO total_cents;
    SELECT COALESCE(sum(amount_cents), 0) INTO paid_cents FROM invoice_payments
    WHERE household_id = NEW.household_id AND card_invoice_id = NEW.card_invoice_id;
    IF paid_cents > total_cents THEN
        RAISE EXCEPTION 'invoice % would be paid beyond its total', NEW.card_invoice_id USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER invoice_payments_balance_guard
    AFTER INSERT ON invoice_payments
    FOR EACH ROW EXECUTE FUNCTION invoice_balance_guard();

CREATE TRIGGER invoice_adjustments_balance_guard
    AFTER INSERT ON invoice_adjustments
    FOR EACH ROW EXECUTE FUNCTION invoice_balance_guard();
