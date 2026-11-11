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
WHERE r.direction = o.direction OR r.amount_cents <> o.amount_cents OR r.account_id <> o.account_id;
