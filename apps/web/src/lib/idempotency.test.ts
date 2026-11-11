import { describe, expect, it } from "vitest";

import { IdempotencyKeys, newIdempotencyKey } from "./idempotency";

function sequence() {
  let n = 0;
  return () => `key-${++n}`;
}

describe("IdempotencyKeys", () => {
  it("reuses the key while the payload is the same (retry)", () => {
    const keys = new IdempotencyKeys(sequence());
    expect(keys.keyFor({ amountCents: 100 })).toBe("key-1");
    expect(keys.keyFor({ amountCents: 100 })).toBe("key-1");
  });

  it("uses a new key when the payload changes (would be 409 otherwise)", () => {
    const keys = new IdempotencyKeys(sequence());
    keys.keyFor({ amountCents: 100 });
    expect(keys.keyFor({ amountCents: 200 })).toBe("key-2");
  });

  it("starts a new submission after reset (confirmed success)", () => {
    const keys = new IdempotencyKeys(sequence());
    keys.keyFor({ amountCents: 100 });
    keys.reset();
    expect(keys.keyFor({ amountCents: 100 })).toBe("key-2");
  });
});

describe("newIdempotencyKey", () => {
  const uuidV4 =
    /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

  it("returns distinct UUIDs", () => {
    const a = newIdempotencyKey();
    expect(a).toMatch(uuidV4);
    expect(newIdempotencyKey()).not.toBe(a);
  });

  it("works without crypto.randomUUID (plain HTTP)", () => {
    const original = crypto.randomUUID;
    Object.defineProperty(crypto, "randomUUID", {
      value: undefined,
      configurable: true,
    });
    try {
      expect(newIdempotencyKey()).toMatch(uuidV4);
    } finally {
      Object.defineProperty(crypto, "randomUUID", {
        value: original,
        configurable: true,
      });
    }
  });
});
