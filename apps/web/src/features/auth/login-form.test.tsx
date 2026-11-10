import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { LoginForm } from "./login-form";

const replace = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace }),
  useSearchParams: () => new URLSearchParams("next=/household?id=42"),
}));

function renderForm() {
  const qc = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={qc}>
      <LoginForm />
    </QueryClientProvider>,
  );
}

function fill(email: string, password: string) {
  fireEvent.change(screen.getByLabelText("E-mail"), {
    target: { value: email },
  });
  fireEvent.change(screen.getByLabelText("Senha"), {
    target: { value: password },
  });
  fireEvent.click(screen.getByRole("button", { name: "Entrar" }));
}

beforeEach(() => replace.mockReset());
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("LoginForm", () => {
  it("validates before calling the API", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    renderForm();

    fill("not-an-email", "");

    expect(await screen.findByText("Informe um e-mail válido.")).toBeTruthy();
    expect(screen.getByText("Informe a senha.")).toBeTruthy();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("shows the API error and stays on the page", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            error: {
              code: "INVALID_CREDENTIALS",
              message: "E-mail ou senha inválidos.",
              requestId: "r",
            },
          }),
          { status: 401, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );
    renderForm();

    fill("ana@example.com", "senha errada 123");

    expect((await screen.findByRole("alert")).textContent).toBe(
      "E-mail ou senha inválidos.",
    );
    expect(replace).not.toHaveBeenCalled();
  });

  it("redirects to the safe ?next= path after login", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(
            JSON.stringify({ id: "u1", name: "Ana", email: "ana@example.com" }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        ),
    );
    renderForm();

    fill("ana@example.com", "uma senha bem longa");

    await waitFor(() =>
      expect(replace).toHaveBeenCalledWith("/household?id=42"),
    );
  });
});
