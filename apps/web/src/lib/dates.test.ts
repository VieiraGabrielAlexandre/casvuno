import { describe, expect, it } from "vitest";

import { formatDate, todayIn } from "./dates";

describe("formatDate", () => {
  it("formats ISO dates as dd/mm/yyyy without timezone shifts", () => {
    expect(formatDate("2026-01-01")).toBe("01/01/2026");
    expect(formatDate("not a date")).toBe("not a date");
  });
});

describe("todayIn", () => {
  // 02:30 UTC on Nov 11 is still Nov 10 in São Paulo (UTC−3).
  const now = new Date("2026-11-11T02:30:00Z");

  it("uses the household timezone", () => {
    expect(todayIn("America/Sao_Paulo", now)).toBe("2026-11-10");
    expect(todayIn("UTC", now)).toBe("2026-11-11");
  });

  it("falls back to the device timezone for an invalid name", () => {
    expect(todayIn("Not/AZone", now)).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  });
});
