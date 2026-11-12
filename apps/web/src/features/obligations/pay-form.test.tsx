import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Account, Obligation } from "@/lib/api/client";

import { PayForm } from "./pay-form";

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

const obligation: Obligation = {
  id: "22222222-2222-4222-8222-222222222222",
  transactionId: null,
  kind: "PAYABLE",
  source: "MANUAL_FORECAST",
  status: "PLANNED",
  paymentStatus: "UNPAID",
  isOverdue: false,
  amountCents: 65000,
  paidCents: 0,
  outstandingCents: 65000,
  dueOn: "2026-12-10",
  description: "Condomínio",
  categoryId: "33333333-3333-4333-8333-333333333333",
  scope: "FAMILY",
  personMemberId: null,
  paymentMethod: "BOLETO",
  accountId: account.id,
  competenceOn: "2026-12-01",
  createdByMemberId: "44444444-4444-4444-8444-444444444444",
  createdAt: "2026-11-11T12:00:00Z",
  updatedAt: "2026-11-11T12:00:00Z",
};

const detail = (paid: number) => ({
  obligation: {
    ...obligation,
    status: "CONFIRMED",
    transactionId: "55555555-5555-4555-8555-555555555555",
    paidCents: paid,
    outstandingCents: obligation.amountCents - paid,
    paymentStatus: paid === obligation.amountCents ? "PAID" : "PARTIALLY_PAID",
  },
  payments: [],
});

function renderForm(onDone = vi.fn()) {
  const qc = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={qc}>
      <PayForm
        householdId="hh"
        obligation={obligation}
        accounts={[account]}
        today="2026-11-11"
        onDone={onDone}
        onCancel={() => {}}
      />
    </QueryClientProvider>,
  );
  return onDone;
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("PayForm", () => {
  it("starts with the outstanding amount and the obligation account", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(detail(65000)), {
        status: 201,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const onDone = renderForm();

    expect(screen.getByLabelText<HTMLInputElement>("Valor pago").value).toBe(
      "650,00",
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Registrar pagamento" }),
    );

    await waitFor(() =>
      expect(onDone).toHaveBeenCalledWith("Condomínio: paga por completo."),
    );
    const request = fetchMock.mock.calls[0][0] as Request;
    expect(new URL(request.url).pathname).toBe(
      `/api/v1/households/hh/obligations/${obligation.id}/payments`,
    );
    expect(request.headers.get("Idempotency-Key")).toBeTruthy();
    expect(await request.json()).toEqual({
      amountCents: 65000,
      paidOn: "2026-11-11",
      accountId: account.id,
    });
  });

  it("never sends more than the outstanding amount", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    renderForm();

    fireEvent.change(screen.getByLabelText("Valor pago"), {
      target: { value: "650,01" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Registrar pagamento" }),
    );

    expect(await screen.findByText("O valor máximo é R$ 650,00.")).toBeTruthy();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("reports a partial payment with what is left", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify(detail(20000)), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    const onDone = renderForm();

    fireEvent.change(screen.getByLabelText("Valor pago"), {
      target: { value: "200,00" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Registrar pagamento" }),
    );

    await waitFor(() =>
      expect(onDone).toHaveBeenCalledWith(
        "Condomínio: R$ 200,00 pagos, faltam R$ 450,00.",
      ),
    );
  });
});
