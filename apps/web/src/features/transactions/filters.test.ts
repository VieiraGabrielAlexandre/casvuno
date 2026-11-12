import { describe, expect, it } from "vitest";

import type { Transaction } from "@/lib/api/client";

import { changedFields } from "./edit-transaction-form";
import { activeFilterCount, noFilters, toQuery } from "./filters";

describe("transaction filters", () => {
  it("drops empty filters and trims the text", () => {
    expect(toQuery(noFilters)).toEqual({ status: "ACTIVE" });
    expect(
      toQuery({ ...noFilters, q: "  padaria ", from: "2026-11-01", kind: "" }),
    ).toEqual({ status: "ACTIVE", q: "padaria", from: "2026-11-01" });
    expect(toQuery({ ...noFilters, status: "ALL" })).toEqual({ status: "ALL" });
  });

  it("counts only the filters that differ from the defaults", () => {
    expect(activeFilterCount(noFilters)).toBe(0);
    expect(
      activeFilterCount({ ...noFilters, q: "x", status: "VOIDED", to: " " }),
    ).toBe(2);
  });
});

describe("changedFields", () => {
  const t = {
    description: "Café",
    merchant: "Padaria",
    notes: null,
    categoryId: "c1",
    scope: "FAMILY",
    personMemberId: null,
  } as Transaction;
  const same = {
    description: "Café",
    merchant: "Padaria",
    notes: "",
    categoryId: "c1",
    scope: "FAMILY" as const,
    personMemberId: "",
  };

  it("sends nothing when nothing changed", () => {
    expect(changedFields(t, same)).toEqual({});
  });

  it("sends only the changes, with empty strings to clear (I-45)", () => {
    expect(
      changedFields(t, {
        ...same,
        merchant: "",
        scope: "PERSONAL",
        personMemberId: "m1",
      }),
    ).toEqual({ merchant: "", scope: "PERSONAL", personMemberId: "m1" });
  });
});
