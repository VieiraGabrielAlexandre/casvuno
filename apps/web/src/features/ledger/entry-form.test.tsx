import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type {
  Account,
  Category,
  Member,
  QuickTemplate,
  Transaction,
} from "@/lib/api/client";

import { EntryForm } from "./entry-form";

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

const category = (
  id: string,
  name: string,
  kind: Category["kind"],
  parentId: string | null = null,
): Category => ({
  id,
  parentId,
  name,
  kind,
  icon: null,
  color: null,
  isActive: true,
  sortOrder: 0,
  createdAt: "2026-01-01T00:00:00Z",
});
const food = category(
  "22222222-2222-4222-8222-222222222222",
  "Alimentação",
  "EXPENSE",
);
const bakery = category(
  "33333333-3333-4333-8333-333333333333",
  "Padaria",
  "EXPENSE",
  food.id,
);
const salary = category(
  "44444444-4444-4444-8444-444444444444",
  "Salário",
  "INCOME",
);

const member: Member = {
  id: "55555555-5555-4555-8555-555555555555",
  userId: "66666666-6666-4666-8666-666666666666",
  name: "Ana",
  email: "ana@example.com",
  role: "OWNER",
  status: "ACTIVE",
  joinedAt: "2026-01-01T00:00:00Z",
};

const cafe: QuickTemplate = {
  id: "77777777-7777-4777-8777-777777777777",
  label: "Café",
  kind: "EXPENSE",
  description: "Café da padaria",
  defaultAmountCents: 1250,
  categoryId: bakery.id,
  scope: "FAMILY",
  personMemberId: null,
  paymentMethod: "PIX",
  accountId: account.id,
  sortOrder: 0,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const tx = (overrides: Partial<Transaction> = {}): Transaction => ({
  id: "88888888-8888-4888-8888-888888888888",
  kind: "EXPENSE",
  scope: "FAMILY",
  personMemberId: null,
  createdByMemberId: member.id,
  description: "Café da padaria",
  merchant: null,
  notes: null,
  amountCents: 1250,
  categoryId: bakery.id,
  occurredOn: "2026-11-11",
  paymentMethod: "PIX",
  accountId: account.id,
  status: "ACTIVE",
  createdAt: "2026-11-11T12:00:00Z",
  updatedAt: "2026-11-11T12:00:00Z",
  ...overrides,
});

const obligation = {
  id: "99999999-9999-4999-8999-999999999999",
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
  description: "Alimentação",
  categoryId: food.id,
  scope: "FAMILY",
  personMemberId: null,
  paymentMethod: "BOLETO",
  accountId: null,
  competenceOn: "2026-11-11",
  createdByMemberId: member.id,
  createdAt: "2026-11-11T12:00:00Z",
  updatedAt: "2026-11-11T12:00:00Z",
};

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

function renderForm(props: Partial<Parameters<typeof EntryForm>[0]> = {}) {
  const qc = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={qc}>
      <EntryForm
        householdId="hh"
        accounts={[account]}
        categories={[food, bakery, salary]}
        members={[member]}
        today="2026-11-11"
        templates={[cafe]}
        last={null}
        {...props}
      />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("EntryForm", () => {
  it("a complete favorite needs only the confirmation (≤ 3 interactions)", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(json(201, { transaction: tx(), obligation }));
    vi.stubGlobal("fetch", fetchMock);
    renderForm();

    fireEvent.click(screen.getByRole("button", { name: /Café/ }));
    fireEvent.click(screen.getByRole("button", { name: "Lançar" }));

    expect((await screen.findByText(/lançada/)).textContent).toBe(
      "Despesa de R$ 12,50 lançada em 11/11/2026.",
    );
    const request = fetchMock.mock.calls[0][0] as Request;
    expect(new URL(request.url).pathname).toBe(
      "/api/v1/households/hh/transactions",
    );
    expect(request.headers.get("Idempotency-Key")).toBeTruthy();
    expect(await request.json()).toEqual({
      kind: "EXPENSE",
      scope: "FAMILY",
      description: "Café da padaria",
      amountCents: 1250,
      categoryId: bakery.id,
      occurredOn: "2026-11-11",
      payment: { method: "PIX", mode: "IMMEDIATE", accountId: account.id },
    });
  });

  it("validates before calling the API", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    renderForm({ accounts: [account, { ...account, id: "other" }] });

    fireEvent.click(screen.getByRole("button", { name: "Lançar" }));

    expect(
      await screen.findByText("Informe um valor maior que zero."),
    ).toBeTruthy();
    expect(screen.getByText("Escolha a categoria.")).toBeTruthy();
    expect(screen.getByText("Escolha a conta.")).toBeTruthy();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("records a forecast with its due date; description defaults to the category", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(json(201, { transaction: null, obligation }));
    vi.stubGlobal("fetch", fetchMock);
    renderForm({ templates: [] });

    fireEvent.change(screen.getByLabelText("Valor"), {
      target: { value: "650,00" },
    });
    fireEvent.change(screen.getByLabelText("Categoria"), {
      target: { value: food.id },
    });
    fireEvent.click(screen.getByLabelText(/Previsto/));
    fireEvent.change(screen.getByLabelText("Vencimento"), {
      target: { value: "2026-12-10" },
    });
    fireEvent.change(screen.getByLabelText("Conta (opcional)"), {
      target: { value: "" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Lançar previsão" }));

    expect((await screen.findByRole("status")).textContent).toBe(
      "Previsão de R$ 650,00 lançada, vence em 10/12/2026.",
    );
    const body = await (fetchMock.mock.calls[0][0] as Request).json();
    expect(body).toEqual({
      kind: "EXPENSE",
      scope: "FAMILY",
      description: "Alimentação",
      amountCents: 65000,
      categoryId: food.id,
      occurredOn: "2026-11-11",
      payment: { method: "PIX", mode: "PLANNED", dueOn: "2026-12-10" },
    });
  });

  it("switching to income offers only income categories", () => {
    renderForm();
    fireEvent.click(screen.getByRole("button", { name: "Receita" }));
    const options = Array.from(
      screen.getByLabelText<HTMLSelectElement>("Categoria").options,
    ).map((o) => o.textContent);
    expect(options).toEqual(["Escolha a categoria", "Salário"]);
  });

  it("repeats the last entry with today's date", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(json(201, { transaction: tx(), obligation }));
    vi.stubGlobal("fetch", fetchMock);
    renderForm({ last: tx({ occurredOn: "2026-11-01", amountCents: 3000 }) });

    fireEvent.click(screen.getByRole("button", { name: /Repetir último/ }));
    fireEvent.click(screen.getByRole("button", { name: "Lançar" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const body = await (fetchMock.mock.calls[0][0] as Request).json();
    expect(body).toMatchObject({ amountCents: 3000, occurredOn: "2026-11-11" });
  });

  it("maps nested API field errors to the form fields", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        json(422, {
          error: {
            code: "ACCOUNT_ARCHIVED",
            message: "Não é possível movimentar uma conta arquivada.",
            details: [{ field: "payment.accountId", reason: "archived" }],
            requestId: "r",
          },
        }),
      ),
    );
    renderForm();

    fireEvent.click(screen.getByRole("button", { name: /Café/ }));
    fireEvent.click(screen.getByRole("button", { name: "Lançar" }));

    await screen.findByText("Não é possível movimentar uma conta arquivada.");
    expect(screen.getByLabelText("Conta").getAttribute("aria-invalid")).toBe(
      "true",
    );
  });

  it("corrects an entry: voids the old one first, then sends the new one once", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(json(200, tx({ status: "VOIDED" })))
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
      .mockResolvedValueOnce(json(201, { transaction: tx(), obligation }));
    vi.stubGlobal("fetch", fetchMock);
    renderForm({ correcting: tx({ amountCents: 1500 }) });

    fireEvent.change(screen.getByLabelText("Valor"), {
      target: { value: "12,50" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Salvar correção" }));

    // The void went through, the new entry did not: the user is told to send again.
    expect((await screen.findByRole("alert")).textContent).toMatch(
      /já foi cancelado/,
    );
    fireEvent.click(screen.getByRole("button", { name: "Salvar correção" }));
    await screen.findByText(/Lançamento corrigido/);

    const requests = fetchMock.mock.calls.map(([r]) => r as Request);
    expect(requests.map((r) => new URL(r.url).pathname)).toEqual([
      `/api/v1/households/hh/transactions/${tx().id}/void`,
      "/api/v1/households/hh/transactions",
      "/api/v1/households/hh/transactions",
    ]);
    // Same Idempotency-Key on the retry of the new entry; never a second void.
    expect(requests[2].headers.get("Idempotency-Key")).toBe(
      requests[1].headers.get("Idempotency-Key"),
    );
    expect(
      screen
        .getByRole("button", { name: "Salvar correção" })
        .hasAttribute("disabled"),
    ).toBe(true);
  });

  it("blocks submission while offline", () => {
    vi.spyOn(navigator, "onLine", "get").mockReturnValue(false);
    renderForm();
    expect(
      screen.getByRole("button", { name: "Lançar" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(screen.getByText(/Sem conexão/)).toBeTruthy();
  });
});
