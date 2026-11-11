import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Account, Member } from "@/lib/api/client";

import { AccountForm } from "./account-form";

const ana: Member = {
  id: "11111111-1111-4111-8111-111111111111",
  userId: "u1",
  name: "Ana",
  email: "ana@example.com",
  role: "OWNER",
  status: "ACTIVE",
  joinedAt: "2026-01-01T00:00:00Z",
};

const existing: Account = {
  id: "22222222-2222-4222-8222-222222222222",
  name: "Corrente",
  type: "CHECKING",
  ownership: "INDIVIDUAL",
  ownerMemberId: ana.id,
  openingBalanceCents: 1000,
  openedOn: "2026-01-01",
  balanceCents: 1000,
  isActive: true,
  createdAt: "2026-01-01T00:00:00Z",
};

function renderForm(account?: Account) {
  const onDone = vi.fn();
  const qc = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={qc}>
      <AccountForm
        householdId="hh"
        members={[ana]}
        defaultOwnerId={ana.id}
        today="2026-11-10"
        account={account}
        onDone={onDone}
      />
    </QueryClientProvider>,
  );
  return onDone;
}

const okJSON = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

const change = (label: string, value: string) =>
  fireEvent.change(screen.getByLabelText(label), { target: { value } });

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("AccountForm", () => {
  it("creates an individual account with the current member as owner", async () => {
    const fetchMock = vi.fn().mockResolvedValue(okJSON(existing, 201));
    vi.stubGlobal("fetch", fetchMock);
    const onDone = renderForm();

    change("Nome", "Corrente");
    change("Saldo inicial", "-1.234,56");
    fireEvent.click(screen.getByRole("button", { name: "Criar conta" }));

    await waitFor(() => expect(onDone).toHaveBeenCalled());
    expect(await (fetchMock.mock.calls[0][0] as Request).json()).toEqual({
      name: "Corrente",
      type: "CHECKING",
      ownership: "INDIVIDUAL",
      ownerMemberId: ana.id,
      openingBalanceCents: -123456,
      openedOn: "2026-11-10",
    });
  });

  it("omits the owner of a joint account", async () => {
    const fetchMock = vi.fn().mockResolvedValue(okJSON(existing, 201));
    vi.stubGlobal("fetch", fetchMock);
    renderForm();

    change("Nome", "Casa");
    change("Titularidade", "JOINT");
    fireEvent.click(screen.getByRole("button", { name: "Criar conta" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const body = await (fetchMock.mock.calls[0][0] as Request).json();
    expect(body).not.toHaveProperty("ownerMemberId");
    expect(body.ownership).toBe("JOINT");
  });

  it("requires an owner for an individual account", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    renderForm();

    change("Nome", "Corrente");
    change("Titular", "");
    fireEvent.click(screen.getByRole("button", { name: "Criar conta" }));

    expect(
      await screen.findByText("Informe o titular da conta individual."),
    ).toBeTruthy();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("sends only the changed fields when editing", async () => {
    const fetchMock = vi.fn().mockResolvedValue(okJSON(existing));
    vi.stubGlobal("fetch", fetchMock);
    const onDone = renderForm(existing);

    change("Nome", "Conta da Ana");
    fireEvent.click(screen.getByRole("button", { name: "Salvar" }));

    await waitFor(() => expect(onDone).toHaveBeenCalled());
    const request = fetchMock.mock.calls[0][0] as Request;
    expect(request.method).toBe("PATCH");
    expect(await request.json()).toEqual({ name: "Conta da Ana" });
  });

  it("shows a conflict on the form", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        okJSON(
          {
            error: {
              code: "ACCOUNT_LIMIT",
              message: "Limite de 50 contas por família atingido.",
              requestId: "r",
            },
          },
          422,
        ),
      ),
    );
    const onDone = renderForm();

    change("Nome", "Mais uma");
    fireEvent.click(screen.getByRole("button", { name: "Criar conta" }));

    expect((await screen.findByRole("alert")).textContent).toBe(
      "Limite de 50 contas por família atingido.",
    );
    expect(onDone).not.toHaveBeenCalled();
  });
});
