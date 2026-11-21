import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Account, Invoice, InvoiceDetail } from "@/lib/api/client";

import { AdjustInvoiceForm } from "./adjust-invoice-form";
import { PayInvoiceForm } from "./pay-invoice-form";

const account: Account = {
  id: "11111111-1111-4111-8111-111111111111",
  name: "Corrente",
  type: "CHECKING",
  ownership: "JOINT",
  ownerMemberId: null,
  openingBalanceCents: 0,
  openedOn: "2026-01-01",
  balanceCents: 100000,
  isActive: true,
  createdAt: "2026-01-01T00:00:00Z",
};

// R$ 200,00 with R$ 50,00 already paid (T08).
const invoice: Invoice = {
  id: "22222222-2222-4222-8222-222222222222",
  creditCardId: "33333333-3333-4333-8333-333333333333",
  cycleStartOn: "2026-11-03",
  closingOn: "2026-12-03",
  dueOn: "2026-12-10",
  referenceMonth: "2026-12",
  status: "CLOSED",
  paymentStatus: "PARTIALLY_PAID",
  closingPending: false,
  installmentsCount: 1,
  installmentsCents: 20000,
  adjustmentsCents: 0,
  totalCents: 20000,
  paidCents: 5000,
  outstandingCents: 15000,
  closedBaseAmountCents: 20000,
  closedAt: "2026-12-03T12:00:00Z",
  closedByMemberId: "44444444-4444-4444-8444-444444444444",
  createdAt: "2026-11-10T12:00:00Z",
  updatedAt: "2026-12-03T12:00:00Z",
};

const detail = (over: Partial<Invoice>): InvoiceDetail => ({
  invoice: { ...invoice, ...over },
  installments: [],
  payments: [],
  adjustments: [],
});

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("PayInvoiceForm", () => {
  it("pays the outstanding balance from the only account (T07)", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        json(201, detail({ paidCents: 20000, outstandingCents: 0 })),
      );
    vi.stubGlobal("fetch", fetchMock);
    const onDone = vi.fn();
    wrap(
      <PayInvoiceForm
        householdId="hh"
        invoice={invoice}
        accounts={[account]}
        today="2026-12-05"
        onDone={onDone}
        onCancel={() => {}}
      />,
    );

    expect(screen.getByLabelText<HTMLInputElement>("Valor pago").value).toBe(
      "150,00",
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Registrar pagamento" }),
    );

    await waitFor(() =>
      expect(onDone).toHaveBeenCalledWith(
        expect.stringMatching(/^Fatura paga por completo/),
      ),
    );
    const request = fetchMock.mock.calls[0][0] as Request;
    expect(new URL(request.url).pathname).toBe(
      `/api/v1/households/hh/invoices/${invoice.id}/payments`,
    );
    expect(request.headers.get("Idempotency-Key")).toBeTruthy();
    expect(await request.json()).toEqual({
      amountCents: 15000,
      paidOn: "2026-12-05",
      accountId: account.id,
    });
  });

  it("never sends more than the outstanding balance", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    wrap(
      <PayInvoiceForm
        householdId="hh"
        invoice={invoice}
        accounts={[account]}
        today="2026-12-05"
        onDone={() => {}}
        onCancel={() => {}}
      />,
    );
    fireEvent.change(screen.getByLabelText("Valor pago"), {
      target: { value: "150,01" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Registrar pagamento" }),
    );

    expect(
      await screen.findByText(/O valor máximo é R\$\s?150,00\./),
    ).toBeTruthy();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("shows the API error and keeps the form", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        json(422, {
          error: {
            code: "ACCOUNT_ARCHIVED",
            message: "Não é possível movimentar uma conta arquivada.",
            details: [{ field: "accountId", reason: "archived" }],
            requestId: "r",
          },
        }),
      ),
    );
    wrap(
      <PayInvoiceForm
        householdId="hh"
        invoice={invoice}
        accounts={[account]}
        today="2026-12-05"
        onDone={() => {}}
        onCancel={() => {}}
      />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Registrar pagamento" }),
    );
    await screen.findByText("Não é possível movimentar uma conta arquivada.");
    expect(screen.getByLabelText("Conta").getAttribute("aria-invalid")).toBe(
      "true",
    );
  });
});

describe("AdjustInvoiceForm", () => {
  it("sends a signed adjustment with its reason", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      json(
        201,
        detail({
          adjustmentsCents: -1000,
          totalCents: 19000,
          outstandingCents: 14000,
        }),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const onDone = vi.fn();
    wrap(
      <AdjustInvoiceForm
        householdId="hh"
        invoice={invoice}
        onDone={onDone}
        onCancel={() => {}}
      />,
    );
    fireEvent.change(screen.getByLabelText("Valor do ajuste"), {
      target: { value: "-10,00" },
    });
    fireEvent.change(screen.getByLabelText("Motivo"), {
      target: { value: "Estorno da loja" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Registrar ajuste" }));

    await waitFor(() => expect(onDone).toHaveBeenCalled());
    const request = fetchMock.mock.calls[0][0] as Request;
    expect(request.headers.get("Idempotency-Key")).toBeTruthy();
    expect(await request.json()).toEqual({
      amountCents: -1000,
      reason: "Estorno da loja",
    });
  });

  it("refuses a credit below what was paid and a missing reason", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    wrap(
      <AdjustInvoiceForm
        householdId="hh"
        invoice={invoice}
        onDone={() => {}}
        onCancel={() => {}}
      />,
    );
    // Total R$ 200,00, paid R$ 50,00: the largest credit is R$ 150,00.
    fireEvent.change(screen.getByLabelText("Valor do ajuste"), {
      target: { value: "-150,01" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Registrar ajuste" }));

    expect(
      await screen.findByText(/O crédito máximo é R\$\s?150,00/),
    ).toBeTruthy();
    expect(screen.getByText("Informe o motivo.")).toBeTruthy();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
