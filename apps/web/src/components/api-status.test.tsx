import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiStatus } from "./api-status";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("ApiStatus", () => {
  it("shows the server as available when /health returns 200", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(new Response("{}", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    render(<ApiStatus />);

    expect(await screen.findByText("Servidor disponível.")).toBeTruthy();
    expect(fetchMock).toHaveBeenCalledWith("/health", { cache: "no-store" });
  });

  it("shows the server as unavailable on 503 and on network errors", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(new Response("{}", { status: 503 }))
        .mockRejectedValueOnce(new TypeError("offline")),
    );

    render(<ApiStatus />);
    expect(await screen.findByText(/Servidor indisponível/)).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "Verificar novamente" }),
    );
    expect(await screen.findByText(/Servidor indisponível/)).toBeTruthy();
  });
});
