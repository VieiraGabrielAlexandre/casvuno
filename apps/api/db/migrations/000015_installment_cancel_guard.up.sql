-- F4.6: voiding a card purchase cancels its installments (ACTIVE → CANCELLED). Cancelling
-- lowers the invoice total, so it must keep paid <= total (INV-06), like payments and
-- adjustments (000014). Installments of CLOSED invoices are already frozen (000013).
CREATE TRIGGER card_installments_balance_guard
    AFTER UPDATE ON card_installments
    FOR EACH ROW EXECUTE FUNCTION invoice_balance_guard();
