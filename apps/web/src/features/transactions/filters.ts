import type { TransactionFilters } from "@/features/ledger/hooks";

/** Filter form state: every field is a string, "" meaning "any". */
export type FilterValues = {
  from: string;
  to: string;
  kind: string;
  categoryId: string;
  accountId: string;
  personMemberId: string;
  scope: string;
  paymentMethod: string;
  q: string;
  status: string;
};

export const noFilters: FilterValues = {
  from: "",
  to: "",
  kind: "",
  categoryId: "",
  accountId: "",
  personMemberId: "",
  scope: "",
  paymentMethod: "",
  q: "",
  status: "ACTIVE",
};

/** Query parameters for GET /transactions: empty fields are dropped, text is trimmed. */
export function toQuery(v: FilterValues): TransactionFilters {
  const out: Record<string, string> = {};
  for (const [k, raw] of Object.entries(v)) {
    const value = raw.trim();
    if (value !== "") out[k] = value;
  }
  return out as TransactionFilters;
}

/** How many filters differ from the defaults (shown on the collapsed filter panel). */
export function activeFilterCount(v: FilterValues): number {
  return (Object.keys(v) as (keyof FilterValues)[]).filter(
    (k) => v[k].trim() !== noFilters[k],
  ).length;
}
