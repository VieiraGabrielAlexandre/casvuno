-- Financial invariants that SQL can verify (docs/domain-model.md §5, marked ✔︎SQL).
-- Returns one row per violation; an empty result means the database is consistent.
-- Used by the integration tests (after every package run) and by `make check-invariants`.
-- Add a query here whenever a feature introduces a verifiable invariant.

-- INV-08: each transfer has exactly two TRANSFER legs: OUT on the origin and IN on the
-- destination, both with the transfer amount and date.
SELECT 'INV-08' AS invariant, t.id::text AS ref, 'transfer legs do not match the transfer' AS detail
FROM transfers t
WHERE (SELECT count(*) FROM account_movements m WHERE m.transfer_id = t.id) <> 2
   OR NOT EXISTS (
        SELECT 1 FROM account_movements m
        WHERE m.transfer_id = t.id AND m.direction = 'OUT' AND m.account_id = t.from_account_id
          AND m.amount_cents = t.amount_cents AND m.posted_on = t.posted_on)
   OR NOT EXISTS (
        SELECT 1 FROM account_movements m
        WHERE m.transfer_id = t.id AND m.direction = 'IN' AND m.account_id = t.to_account_id
          AND m.amount_cents = t.amount_cents AND m.posted_on = t.posted_on)

UNION ALL

-- INV-18: a reversal mirrors its original: same account and amount, opposite direction.
SELECT 'INV-18', r.id::text, 'reversal does not mirror the original movement'
FROM account_movements r
JOIN account_movements o ON o.household_id = r.household_id AND o.id = r.reversal_of_id
WHERE r.direction = o.direction OR r.amount_cents <> o.amount_cents OR r.account_id <> o.account_id

UNION ALL

-- INV-03 (outside the card): each transaction has exactly one obligation, of the matching
-- kind and amount, and that obligation is not a forecast. (F4.3: card purchases have none.)
SELECT 'INV-03', t.id::text, 'transaction without exactly one matching obligation'
FROM transactions t
WHERE (SELECT count(*) FROM obligations o WHERE o.household_id = t.household_id AND o.transaction_id = t.id) <> 1
   OR NOT EXISTS (
        SELECT 1 FROM obligations o
        WHERE o.household_id = t.household_id AND o.transaction_id = t.id
          AND o.amount_cents = t.amount_cents AND o.status <> 'PLANNED'
          AND o.kind = CASE t.kind WHEN 'EXPENSE' THEN 'PAYABLE' ELSE 'RECEIVABLE' END)

UNION ALL

-- INV-04: payments never exceed the obligation.
SELECT 'INV-04', o.id::text, 'payments exceed the obligation amount'
FROM obligations o
JOIN obligation_payments p ON p.household_id = o.household_id AND p.obligation_id = o.id
GROUP BY o.id, o.amount_cents
HAVING sum(p.amount_cents) > o.amount_cents

UNION ALL

-- INV-05: each obligation payment points to one movement of the same amount and date:
-- PAYABLE -> OUT/DIRECT_PAYMENT, RECEIVABLE -> IN/DIRECT_RECEIPT.
SELECT 'INV-05', p.id::text, 'payment movement does not match the obligation'
FROM obligation_payments p
JOIN obligations o ON o.household_id = p.household_id AND o.id = p.obligation_id
JOIN account_movements m ON m.household_id = p.household_id AND m.id = p.account_movement_id
WHERE m.amount_cents <> p.amount_cents OR m.posted_on <> p.paid_on
   OR (o.kind = 'PAYABLE' AND (m.direction, m.kind) <> ('OUT', 'DIRECT_PAYMENT'))
   OR (o.kind = 'RECEIVABLE' AND (m.direction, m.kind) <> ('IN', 'DIRECT_RECEIPT'))

UNION ALL

-- INV-05 (converse): direct payments and receipts only exist as the cash side of an
-- obligation payment (no orphan cash).
SELECT 'INV-05', m.id::text, 'direct movement without an obligation payment'
FROM account_movements m
WHERE m.kind IN ('DIRECT_PAYMENT', 'DIRECT_RECEIPT')
  AND NOT EXISTS (SELECT 1 FROM obligation_payments p WHERE p.household_id = m.household_id AND p.account_movement_id = m.id)

UNION ALL

-- INV-13: a forecast (PLANNED) has no transaction and no payments: its first payment confirms it.
SELECT 'INV-13', o.id::text, 'planned obligation with payments'
FROM obligations o
WHERE o.status = 'PLANNED'
  AND EXISTS (SELECT 1 FROM obligation_payments p WHERE p.household_id = o.household_id AND p.obligation_id = o.id)

UNION ALL

-- INV-19: a voided transaction has its obligation cancelled (and a cancelled obligation that
-- was confirmed belongs to a voided transaction).
SELECT 'INV-19', t.id::text, 'voided transaction and cancelled obligation out of step'
FROM transactions t
JOIN obligations o ON o.household_id = t.household_id AND o.transaction_id = t.id
WHERE (t.status = 'VOIDED') <> (o.status = 'CANCELLED')

UNION ALL

-- INV-19: every payment of a cancelled obligation has its movement reversed.
SELECT 'INV-19', p.id::text, 'payment of a cancelled obligation not reversed'
FROM obligation_payments p
JOIN obligations o ON o.household_id = p.household_id AND o.id = p.obligation_id
WHERE o.status = 'CANCELLED'
  AND NOT EXISTS (SELECT 1 FROM account_movements r
                  WHERE r.household_id = p.household_id AND r.reversal_of_id = p.account_movement_id)

UNION ALL

-- INV-19 (converse): a direct payment or receipt is reversed only by cancelling its obligation.
SELECT 'INV-19', r.id::text, 'reversal of a movement whose obligation is not cancelled'
FROM account_movements r
JOIN account_movements m ON m.household_id = r.household_id AND m.id = r.reversal_of_id
WHERE m.kind IN ('DIRECT_PAYMENT', 'DIRECT_RECEIPT')
  AND NOT EXISTS (SELECT 1 FROM obligation_payments p
                  JOIN obligations o ON o.household_id = p.household_id AND o.id = p.obligation_id
                  WHERE p.household_id = m.household_id AND p.account_movement_id = m.id AND o.status = 'CANCELLED');
