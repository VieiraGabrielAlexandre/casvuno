import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Account } from "@/lib/api/client";

import { TransferForm } from "./transfer-form";

const account = (id: string, name: string, balanceCents: number): Account => ({
  id,
  name,
  type: "CHECKING",
  ownership: "JOINT",
  ownerMemberId: null,
  openingBalanceCents: 0,
  openedOn: "2026-01-01",
  balanceCents,
  isActive: true,
  createdAt: "2026-01-01T00:00:00Z",
});
const checking = account(
  "11111111-1111-4111-8111-111111111111",
  "Corrente",
  200000,
);
const savings = account(
  "22222222-2222-4222-8222-222222222222",
  "Poupança",
  100000,
);

const created = {
  id: "33333333-3333-4333-8333-333333333333",
  fromAccountId: checking.id,
  fromAccountName: checking.name,
  toAccountId: savings.id,
  toAccountName: savings.name,
  amountCents: 50000,
  postedOn: "2026-11-10",
  description: "Transferência",
  createdByMemberId: "44444444-4444-4444-8444-444444444444",
  createdAt: "2026-11-10T12:00:00Z",
};

function renderForm() {
  const qc = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={qc}>
      <TransferForm
        householdId="hh"
        accounts={[checking, savings]}
        today="2026-11-10"
      />
    </QueryClientProvider>,
  );
}

function fillAndSubmit(from: Account, to: Account, digits: string) {
  fireEvent.change(screen.getByLabelText("De"), { target: { value: from.id } });
  fireEvent.change(screen.getByLabelText("Para"), { target: { value: to.id } });
  fireEvent.change(screen.getByLabelText("Valor"), {
    target: { value: digits },
  });
  fireEvent.click(screen.getByRole("button", { name: "Transferir" }));
}

const created201 = () =>
  new Response(JSON.stringify(created), {
    status: 201,
    headers: { "Content-Type": "application/json" },
  });

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("TransferForm", () => {
  it("validates before calling the API", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    renderForm();

    fillAndSubmit(checking, checking, "");

    expect(
      await screen.findByText("Escolha contas de origem e destino diferentes."),
    ).toBeTruthy();
    expect(screen.getByText("Informe um valor maior que zero.")).toBeTruthy();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("sends integer cents with an Idempotency-Key and confirms only after the server", async () => {
    const fetchMock = vi.fn().mockResolvedValue(created201());
    vi.stubGlobal("fetch", fetchMock);
    renderForm();

    fillAndSubmit(checking, savings, "500,00");

    expect((await screen.findByText(/registrada/)).textContent).toBe(
      "Transferência de R$ 500,00 de Corrente para Poupança registrada em 10/11/2026.",
    );
    const request = fetchMock.mock.calls[0][0] as Request;
    expect(request.method).toBe("POST");
    expect(new URL(request.url).pathname).toBe(
      "/api/v1/households/hh/transfers",
    );
    expect(request.headers.get("Idempotency-Key")).toBeTruthy();
    expect(await request.json()).toEqual({
      fromAccountId: checking.id,
      toAccountId: savings.id,
      amountCents: 50000,
      postedOn: "2026-11-10",
    });
    // Ready for the next transfer: amount cleared.
    expect(screen.getByLabelText<HTMLInputElement>("Valor").value).toBe("0,00");
  });

  it("reuses the key when retrying after a network failure, and renews it after success", async () => {
    const fetchMock = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
      .mockResolvedValueOnce(created201())
      .mockResolvedValueOnce(created201());
    vi.stubGlobal("fetch", fetchMock);
    renderForm();

    fillAndSubmit(checking, savings, "500,00");
    expect((await screen.findByRole("alert")).textContent).toMatch(
      /Sem conexão/,
    );
    expect(screen.queryByText(/registrada/)).toBeNull(); // no false success

    fireEvent.click(screen.getByRole("button", { name: "Transferir" }));
    await screen.findByText(/registrada/);

    fillAndSubmit(checking, savings, "500,00");
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3));

    const keys = fetchMock.mock.calls.map(([r]) =>
      (r as Request).headers.get("Idempotency-Key"),
    );
    expect(keys[1]).toBe(keys[0]); // retry of the same submission
    expect(keys[2]).not.toBe(keys[0]); // a new transfer
  });

  it("shows API field errors next to the field", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            error: {
              code: "ACCOUNT_ARCHIVED",
              message: "Não é possível movimentar uma conta arquivada.",
              details: [{ field: "toAccountId", reason: "archived" }],
              requestId: "r",
            },
          }),
          { status: 422, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );
    renderForm();

    fillAndSubmit(checking, savings, "1");

    expect(
      await screen.findByText("Não é possível movimentar uma conta arquivada."),
    ).toBeTruthy();
    expect(screen.getByLabelText("Para").getAttribute("aria-invalid")).toBe(
      "true",
    );
  });

  it("blocks submission while offline", () => {
    vi.spyOn(navigator, "onLine", "get").mockReturnValue(false);
    renderForm();

    const button = screen.getByRole("button", { name: "Transferir" });
    expect(button.hasAttribute("disabled")).toBe(true);
    expect(screen.getByText(/Sem conexão/)).toBeTruthy();
    vi.restoreAllMocks();
  });
});
