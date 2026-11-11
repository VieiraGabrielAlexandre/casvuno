import { describe, expect, it } from "vitest";

import {
  digitsToCents,
  formatBRL,
  formatDecimal,
  MAX_ABS_CENTS,
} from "./money";

describe("formatDecimal", () => {
  it.each([
    [0, "0,00"],
    [5, "0,05"],
    [99, "0,99"],
    [100, "1,00"],
    [123456, "1.234,56"],
    [-5, "-0,05"],
    [-123456789, "-1.234.567,89"],
    [MAX_ABS_CENTS, "10.000.000.000.000,00"],
  ])("%d → %s", (cents, text) => {
    expect(formatDecimal(cents)).toBe(text);
  });

  it("rejects non-integers instead of rounding", () => {
    expect(() => formatDecimal(0.1 + 0.2)).toThrow(RangeError);
  });
});

describe("formatBRL", () => {
  it("adds the currency and keeps the sign in front", () => {
    expect(formatBRL(150000)).toBe("R$ 1.500,00");
    expect(formatBRL(-150)).toBe("-R$ 1,50");
  });
});

describe("digitsToCents", () => {
  it.each([
    ["", 0],
    ["0,00", 0],
    ["1", 1],
    ["12,34", 1234],
    ["0,123", 123],
    ["R$ 1.000,00", 100000],
    ["-50", 50],
    ["10.000.000.000.000,00", MAX_ABS_CENTS],
  ])("%j → %d", (text, cents) => {
    expect(digitsToCents(text)).toBe(cents);
  });

  it("returns null above the API limit", () => {
    expect(digitsToCents("10.000.000.000.000,01")).toBeNull();
    expect(digitsToCents("99999999999999999")).toBeNull();
  });

  it("is exact where float math is not (0,1 + 0,2)", () => {
    expect(digitsToCents("0,10")! + digitsToCents("0,20")!).toBe(30);
  });
});
