import type {
  CreateTransactionRequest,
  QuickTemplate,
  Scope,
  Transaction,
  TransactionKind,
  TransactionPaymentMethod,
} from "@/lib/api/client";

export type EntryMode = "IMMEDIATE" | "PLANNED";

/** Quick-entry form state. Amounts are integer cents; dates are "YYYY-MM-DD". */
export type EntryValues = {
  kind: TransactionKind;
  amountCents: number;
  description: string;
  categoryId: string;
  accountId: string;
  /** CREDIT_CARD: a purchase on `cardId` in `installments` (F4.7); else paid from an account. */
  method: TransactionPaymentMethod;
  cardId: string;
  installments: number;
  mode: EntryMode;
  occurredOn: string;
  dueOn: string;
  scope: Scope;
  personMemberId: string;
  merchant: string;
  notes: string;
};

/** A blank entry for today: expense, paid now by Pix, for the family. */
export function emptyEntry(
  today: string,
  defaults: Partial<EntryValues> = {},
): EntryValues {
  return {
    kind: "EXPENSE",
    amountCents: 0,
    description: "",
    categoryId: "",
    accountId: "",
    method: "PIX",
    cardId: "",
    installments: 1,
    mode: "IMMEDIATE",
    occurredOn: today,
    dueOn: today,
    scope: "FAMILY",
    personMemberId: "",
    merchant: "",
    notes: "",
    ...defaults,
  };
}

/**
 * A template fills everything but the date (today). Without its own account the current
 * one is kept, and without a default amount the amount is left for the user to type.
 */
export function fromTemplate(
  t: QuickTemplate,
  today: string,
  current: EntryValues,
): EntryValues {
  return {
    ...emptyEntry(today),
    kind: t.kind,
    amountCents: t.defaultAmountCents ?? 0,
    description: t.description,
    categoryId: t.categoryId,
    accountId: t.accountId ?? current.accountId,
    method: t.paymentMethod,
    cardId: t.creditCardId ?? "",
    scope: t.scope,
    personMemberId: t.personMemberId ?? "",
  };
}

/** A complete template (amount and account or card) needs only the confirmation. */
export function isCompleteTemplate(t: QuickTemplate): boolean {
  return (
    t.defaultAmountCents !== null &&
    (t.accountId !== null || t.creditCardId !== null)
  );
}

/**
 * An existing entry as form values: "repetir último" uses today's date, "corrigir" keeps
 * the original one. Always paid now (a correction of a paid entry is paid too); a card
 * purchase comes back on its card with its number of installments.
 */
export function fromTransaction(
  tx: Transaction,
  occurredOn: string,
): EntryValues {
  return {
    ...emptyEntry(occurredOn),
    kind: tx.kind,
    amountCents: tx.amountCents,
    description: tx.description,
    categoryId: tx.categoryId,
    accountId: tx.accountId ?? "",
    method: tx.paymentMethod,
    cardId: tx.creditCardId ?? "",
    installments: tx.installmentCount ?? 1,
    scope: tx.scope,
    personMemberId: tx.personMemberId ?? "",
    merchant: tx.merchant ?? "",
    notes: tx.notes ?? "",
  };
}

/**
 * The API request. A forecast carries its due date, an optional account and no merchant
 * or notes (I-44); a card purchase carries its card and installments and no account or
 * mode (I-50); the person is sent only when chosen.
 */
export function toCreateRequest(v: EntryValues): CreateTransactionRequest {
  const card = v.method === "CREDIT_CARD";
  const planned = !card && v.mode === "PLANNED";
  const description = v.description.trim();
  const merchant = v.merchant.trim();
  const notes = v.notes.trim();
  return {
    kind: v.kind,
    scope: v.scope,
    description,
    amountCents: v.amountCents,
    categoryId: v.categoryId,
    occurredOn: v.occurredOn,
    ...(v.personMemberId ? { personMemberId: v.personMemberId } : {}),
    ...(!planned && merchant ? { merchant } : {}),
    ...(!planned && notes ? { notes } : {}),
    payment: card
      ? { method: v.method, cardId: v.cardId, installments: v.installments }
      : {
          method: v.method,
          mode: v.mode,
          ...(v.accountId ? { accountId: v.accountId } : {}),
          ...(planned ? { dueOn: v.dueOn } : {}),
        },
  };
}

/**
 * The installment split shown before sending (RN-010, same rule as the API): the first
 * `amount % n` installments carry one extra cent. Returns the first and the last value.
 */
export function installmentPreview(
  amountCents: number,
  n: number,
): { first: number; last: number } | null {
  if (!Number.isInteger(n) || n < 1 || n > 60 || amountCents < n) return null;
  const base = Math.floor(amountCents / n);
  return { first: amountCents % n > 0 ? base + 1 : base, last: base };
}
