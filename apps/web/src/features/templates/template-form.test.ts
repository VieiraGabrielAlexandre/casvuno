import { describe, expect, it } from "vitest";

import type { QuickTemplate } from "@/lib/api/client";

import {
  toCreateTemplate,
  toTemplateValues,
  toUpdateTemplate,
} from "./template-form";

const t: QuickTemplate = {
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

describe("template requests", () => {
  it("creates without the optional fields left blank", () => {
    const v = {
      ...toTemplateValues(),
      label: " Uber ",
      description: "Corrida",
      categoryId: "cat",
    };
    expect(toCreateTemplate(v)).toEqual({
      label: "Uber",
      kind: "EXPENSE",
      description: "Corrida",
      categoryId: "cat",
      scope: "FAMILY",
      paymentMethod: "PIX",
    });
  });

  it("updates only what changed; 0 and empty strings clear (I-46)", () => {
    expect(toUpdateTemplate(t, toTemplateValues(t))).toEqual({});
    expect(
      toUpdateTemplate(t, {
        ...toTemplateValues(t),
        defaultAmountCents: 0,
        accountId: "",
        label: "Cafezinho",
      }),
    ).toEqual({ label: "Cafezinho", defaultAmountCents: 0, accountId: "" });
  });
});
