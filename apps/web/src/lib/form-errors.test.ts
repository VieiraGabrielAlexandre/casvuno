import { describe, expect, it, vi } from "vitest";

import { ApiError } from "./api/client";
import { applyApiError, safeNext } from "./form-errors";

describe("safeNext", () => {
  it.each([
    ["/household?id=1", "/household?id=1"],
    [null, "/"],
    ["", "/"],
    ["https://evil.example", "/"],
    ["//evil.example", "/"],
    ["javascript:alert(1)", "/"],
  ])("%s -> %s", (input, expected) => {
    expect(safeNext(input)).toBe(expected);
  });
});

describe("applyApiError", () => {
  it("puts field errors on known fields", () => {
    const setError = vi.fn();
    applyApiError(
      new ApiError(409, "EMAIL_TAKEN", "Este e-mail já está cadastrado.", [
        { field: "email", reason: "taken" },
      ]),
      setError,
      ["email", "password"],
    );
    expect(setError).toHaveBeenCalledWith("email", {
      message: "Este e-mail já está cadastrado.",
    });
  });

  it("falls back to a form-level error", () => {
    const setError = vi.fn();
    applyApiError(
      new ApiError(401, "INVALID_CREDENTIALS", "E-mail ou senha inválidos."),
      setError,
      ["email"],
    );
    expect(setError).toHaveBeenCalledWith("root.server", {
      message: "E-mail ou senha inválidos.",
    });
  });
});
