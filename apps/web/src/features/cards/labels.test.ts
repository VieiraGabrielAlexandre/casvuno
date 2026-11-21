import { describe, expect, it } from "vitest";

import type { Invoice } from "@/lib/api/client";

import {
  cardName,
  invoiceSituation,
  monthsBefore,
  referenceMonthLabel,
} from "./labels";

const invoice = (over: Partial<Invoice>): Invoice => ({
  id: "i1",
  creditCardId: "c1",
  cycleStartOn: "2026-11-03",
  closingOn: "2026-12-03",
  dueOn: "2026-12-10",
  referenceMonth: "2026-12",
  status: "OPEN",
  paymentStatus: "UNPAID",
  closingPending: false,
  installmentsCount: 1,
  installmentsCents: 20000,
  adjustmentsCents: 0,
  totalCents: 20000,
  paidCents: 0,
  outstandingCents: 20000,
  closedBaseAmountCents: null,
  closedAt: null,
  closedByMemberId: null,
  createdAt: "2026-11-10T12:00:00Z",
  updatedAt: "2026-11-10T12:00:00Z",
  ...over,
});

describe("referenceMonthLabel", () => {
  it("names the due month", () => {
    expect(referenceMonthLabel("2026-12")).toBe("dezembro de 2026");
    expect(referenceMonthLabel("2027-01")).toBe("janeiro de 2027");
    expect(referenceMonthLabel("x")).toBe("x");
  });
});

describe("cardName", () => {
  it("shows the last four digits when known", () => {
    expect(cardName({ name: "Nubank", lastFour: "1234" })).toBe(
      "Nubank •••• 1234",
    );
    expect(cardName({ name: "Nubank", lastFour: null })).toBe("Nubank");
  });
});

describe("invoiceSituation", () => {
  it("combines lifecycle and payment", () => {
    expect(invoiceSituation(invoice({}))).toBe("Aberta");
    expect(invoiceSituation(invoice({ closingPending: true }))).toBe(
      "Fechamento pendente",
    );
    expect(
      invoiceSituation(
        invoice({
          status: "CLOSED",
          paymentStatus: "PARTIALLY_PAID",
          paidCents: 5000,
        }),
      ),
    ).toBe("Fechada · paga em parte");
    expect(
      invoiceSituation(invoice({ paymentStatus: "PAID", paidCents: 20000 })),
    ).toBe("Paga");
    expect(
      invoiceSituation(
        invoice({ totalCents: 0, installmentsCents: 0, paymentStatus: "PAID" }),
      ),
    ).toBe("Sem lançamentos");
  });
});

describe("monthsBefore", () => {
  it("goes back whole months, across the year", () => {
    expect(monthsBefore("2026-11-21", 3)).toBe("2026-08-01");
    expect(monthsBefore("2027-02-28", 3)).toBe("2026-11-01");
    expect(monthsBefore("2027-01-01", 0)).toBe("2027-01-01");
  });
});
