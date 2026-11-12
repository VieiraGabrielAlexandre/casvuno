import { describe, expect, it } from "vitest";

import type { QuickTemplate, Transaction } from "@/lib/api/client";

import {
  emptyEntry,
  fromTemplate,
  fromTransaction,
  isCompleteTemplate,
  toCreateRequest,
} from "./entry";

const template: QuickTemplate = {
  id: "t1",
  label: "Café",
  kind: "EXPENSE",
  description: "Café da padaria",
  defaultAmountCents: 1250,
  categoryId: "cat",
  scope: "FAMILY",
  personMemberId: null,
  paymentMethod: "PIX",
  accountId: "acc",
  sortOrder: 0,
  createdAt: "2026-11-01T00:00:00Z",
  updatedAt: "2026-11-01T00:00:00Z",
};

const tx: Transaction = {
  id: "tx",
  kind: "INCOME",
  scope: "PERSONAL",
  personMemberId: "m1",
  createdByMemberId: "m1",
  description: "Freela",
  merchant: "Cliente",
  notes: "nota",
  amountCents: 50000,
  categoryId: "inc",
  occurredOn: "2026-11-03",
  paymentMethod: "BANK_TRANSFER",
  accountId: "acc2",
  status: "ACTIVE",
  createdAt: "2026-11-03T00:00:00Z",
  updatedAt: "2026-11-03T00:00:00Z",
};

describe("entry", () => {
  it("builds an immediate request with integer cents and no empty optionals", () => {
    const v = {
      ...emptyEntry("2026-11-11"),
      amountCents: 1250,
      description: " Café ",
      categoryId: "cat",
      accountId: "acc",
    };
    expect(toCreateRequest(v)).toEqual({
      kind: "EXPENSE",
      scope: "FAMILY",
      description: "Café",
      amountCents: 1250,
      categoryId: "cat",
      occurredOn: "2026-11-11",
      payment: { method: "PIX", mode: "IMMEDIATE", accountId: "acc" },
    });
  });

  it("builds a forecast with its due date, without merchant or notes (I-44)", () => {
    const v = {
      ...emptyEntry("2026-11-11"),
      mode: "PLANNED" as const,
      amountCents: 65000,
      description: "Condomínio",
      categoryId: "cat",
      dueOn: "2026-12-10",
      merchant: "x",
      notes: "y",
      method: "BOLETO" as const,
    };
    expect(toCreateRequest(v)).toEqual({
      kind: "EXPENSE",
      scope: "FAMILY",
      description: "Condomínio",
      amountCents: 65000,
      categoryId: "cat",
      occurredOn: "2026-11-11",
      payment: { method: "BOLETO", mode: "PLANNED", dueOn: "2026-12-10" },
    });
  });

  it("fills from a template, keeping the current account when it has none", () => {
    const current = { ...emptyEntry("2026-11-11"), accountId: "mine" };
    expect(fromTemplate(template, "2026-11-11", current)).toMatchObject({
      amountCents: 1250,
      accountId: "acc",
      occurredOn: "2026-11-11",
      mode: "IMMEDIATE",
    });
    const partial = { ...template, accountId: null, defaultAmountCents: null };
    expect(fromTemplate(partial, "2026-11-11", current)).toMatchObject({
      amountCents: 0,
      accountId: "mine",
    });
    expect(isCompleteTemplate(template)).toBe(true);
    expect(isCompleteTemplate(partial)).toBe(false);
  });

  it("repeats or corrects an entry with the chosen date", () => {
    expect(fromTransaction(tx, "2026-11-11")).toMatchObject({
      kind: "INCOME",
      amountCents: 50000,
      occurredOn: "2026-11-11",
      scope: "PERSONAL",
      personMemberId: "m1",
      merchant: "Cliente",
      method: "BANK_TRANSFER",
      accountId: "acc2",
    });
  });
});
