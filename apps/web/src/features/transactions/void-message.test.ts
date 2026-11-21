import { describe, expect, it } from "vitest";

import { voidMessage } from "./void-message";

describe("voidMessage", () => {
  it("explains that a card purchase leaves the invoices without moving cash", () => {
    const m = voidMessage({
      description: "TV",
      amountCents: 120000,
      kind: "EXPENSE",
      creditCardId: "c1",
    });
    expect(m).toContain("parcelas saem das faturas");
    expect(m).not.toContain("estorno");
  });

  it("explains the reversal of an expense paid from an account", () => {
    const m = voidMessage({
      description: "Mercado",
      amountCents: 5000,
      kind: "EXPENSE",
      creditCardId: null,
    });
    expect(m).toContain("sai do consumo");
    expect(m).toContain("volta para a conta");
  });

  it("explains the reversal of an income", () => {
    const m = voidMessage({
      description: "Salário",
      amountCents: 500000,
      kind: "INCOME",
    });
    expect(m).toContain("total de receitas");
    expect(m).toContain("sai de novo da conta");
  });
});
