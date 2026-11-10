import { describe, expect, it } from "vitest";

import { ApiError, call } from "./client";

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

describe("call", () => {
  it("returns data on success", async () => {
    const res = json(200, { id: "1" });
    await expect(
      call(Promise.resolve({ data: { id: "1" }, response: res })),
    ).resolves.toEqual({ id: "1" });
  });

  it("maps the error envelope to ApiError", async () => {
    const envelope = {
      error: {
        code: "VALIDATION_ERROR",
        message: "Dados inválidos.",
        details: [{ field: "email", reason: "email" }],
        requestId: "r1",
      },
    };
    const err = await call(
      Promise.resolve({ error: envelope, response: json(400, envelope) }),
    ).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({
      status: 400,
      code: "VALIDATION_ERROR",
      message: "Dados inválidos.",
      details: [{ field: "email", reason: "email" }],
    });
  });

  it("never reports success when the network fails (offline)", async () => {
    const err = await call(
      Promise.reject(new TypeError("Failed to fetch")),
    ).catch((e: unknown) => e);
    expect(err).toMatchObject({ status: 0, code: "NETWORK_ERROR" });
  });
});
