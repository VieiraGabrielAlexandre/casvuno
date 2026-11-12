import type {
  CreateTransactionRequest,
  PaymentMethod,
  QuickTemplate,
  Scope,
  Transaction,
  TransactionKind,
} from "@/lib/api/client";

export type EntryMode = "IMMEDIATE" | "PLANNED";

/** Quick-entry form state. Amounts are integer cents; dates are "YYYY-MM-DD". */
export type EntryValues = {
  kind: TransactionKind;
  amountCents: number;
  description: string;
  categoryId: string;
  accountId: string;
  method: PaymentMethod;
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
    scope: t.scope,
    personMemberId: t.personMemberId ?? "",
  };
}

/** A complete template (amount and account) needs only the confirmation. */
export function isCompleteTemplate(t: QuickTemplate): boolean {
  return t.defaultAmountCents !== null && t.accountId !== null;
}

/**
 * An existing entry as form values: "repetir último" uses today's date, "corrigir" keeps
 * the original one. Always paid now (a correction of a paid entry is paid too).
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
    accountId: tx.accountId,
    method: tx.paymentMethod,
    scope: tx.scope,
    personMemberId: tx.personMemberId ?? "",
    merchant: tx.merchant ?? "",
    notes: tx.notes ?? "",
  };
}

/**
 * The API request. A forecast carries its due date, an optional account and no merchant
 * or notes (I-44); the person is sent only when chosen.
 */
export function toCreateRequest(v: EntryValues): CreateTransactionRequest {
  const planned = v.mode === "PLANNED";
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
    payment: {
      method: v.method,
      mode: v.mode,
      ...(v.accountId ? { accountId: v.accountId } : {}),
      ...(planned ? { dueOn: v.dueOn } : {}),
    },
  };
}
