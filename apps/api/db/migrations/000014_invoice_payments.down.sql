-- Destroys invoice payments and adjustments: never run in production (ADR 0008). The cash
-- movements of the payments stay in account_movements (append-only).
DROP TABLE IF EXISTS invoice_adjustments;
DROP TABLE IF EXISTS invoice_payments;
DROP FUNCTION IF EXISTS invoice_balance_guard();
