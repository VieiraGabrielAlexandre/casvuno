-- INV-15 (F4.4): closing an invoice freezes it. The service already locks the invoice and
-- computes the base; these triggers make the database refuse, even if a bug skipped those
-- checks, a base that differs from the active installments, any change to a CLOSED invoice
-- (V1 never reopens one) and any installment entering or leaving a CLOSED invoice. Later
-- corrections are invoice adjustments (F4.5, RN-012).

CREATE FUNCTION card_invoices_guard() RETURNS trigger
    LANGUAGE plpgsql AS
$$
DECLARE
    active_cents bigint;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'card_invoices rows are never deleted' USING ERRCODE = 'check_violation';
    END IF;
    IF OLD.status = 'CLOSED' THEN
        RAISE EXCEPTION 'invoice % is closed', OLD.id USING ERRCODE = 'check_violation';
    END IF;
    -- concrete dates and card never change once the invoice exists (ADR 0003)
    IF (NEW.household_id, NEW.credit_card_id, NEW.cycle_start_on, NEW.closing_on, NEW.due_on)
        IS DISTINCT FROM (OLD.household_id, OLD.credit_card_id, OLD.cycle_start_on, OLD.closing_on, OLD.due_on) THEN
        RAISE EXCEPTION 'invoice % keeps its card and dates', OLD.id USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.status = 'CLOSED' THEN
        SELECT COALESCE(sum(amount_cents), 0) INTO active_cents
        FROM card_installments
        WHERE household_id = NEW.household_id AND card_invoice_id = NEW.id AND status = 'ACTIVE';
        IF NEW.closed_base_amount_cents IS DISTINCT FROM active_cents THEN
            RAISE EXCEPTION 'invoice % closed with a base other than its active installments', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER card_invoices_guard
    BEFORE UPDATE OR DELETE ON card_invoices
    FOR EACH ROW EXECUTE FUNCTION card_invoices_guard();

-- FOR SHARE waits for a close in progress (which holds FOR UPDATE) and then sees CLOSED.
CREATE FUNCTION card_installments_guard() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'card_installments rows are never deleted' USING ERRCODE = 'check_violation';
    END IF;
    IF TG_OP = 'UPDATE' THEN
        PERFORM 1 FROM card_invoices
        WHERE household_id = OLD.household_id AND id = OLD.card_invoice_id AND status = 'CLOSED'
        FOR SHARE;
        IF FOUND THEN
            RAISE EXCEPTION 'installment % is on a closed invoice', OLD.id USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    PERFORM 1 FROM card_invoices
    WHERE household_id = NEW.household_id AND id = NEW.card_invoice_id AND status = 'CLOSED'
    FOR SHARE;
    IF FOUND THEN
        RAISE EXCEPTION 'invoice % is closed', NEW.card_invoice_id USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER card_installments_guard
    BEFORE INSERT OR UPDATE OR DELETE ON card_installments
    FOR EACH ROW EXECUTE FUNCTION card_installments_guard();
