/**
 * Money helpers. Amounts are always integer cents (AGENTS.md §2): no floating-point
 * arithmetic, no `/ 100`. Formatting and parsing work on the decimal digits as text.
 */

/** Largest absolute amount the API accepts (kernel.MaxAbsCents = 10^15 cents). */
export const MAX_ABS_CENTS = 1_000_000_000_000_000;

const MAX_DIGITS = String(MAX_ABS_CENTS).length;

/** Groups the integer part in thousands with dots (pt-BR): "1234567" → "1.234.567". */
function groupThousands(digits: string): string {
  return digits.replace(/\B(?=(\d{3})+(?!\d))/g, ".");
}

/** Formats cents as a pt-BR decimal without currency: 123456 → "1.234,56", -5 → "-0,05". */
export function formatDecimal(cents: number): string {
  if (!Number.isSafeInteger(cents)) {
    throw new RangeError(`cents must be a safe integer, got ${cents}`);
  }
  const digits = String(Math.abs(cents)).padStart(3, "0");
  const int = groupThousands(digits.slice(0, -2));
  const sign = cents < 0 ? "-" : "";
  return `${sign}${int},${digits.slice(-2)}`;
}

/** Formats cents as BRL: 123456 → "R$ 1.234,56", -150 → "-R$ 1,50". */
export function formatBRL(cents: number): string {
  const decimal = formatDecimal(Math.abs(cents));
  return `${cents < 0 ? "-" : ""}R$ ${decimal}`;
}

/**
 * Reads the digits typed in a money field as cents ("12,34" → 1234, "R$ 1.000,00" →
 * 100000), the way a cash register fills from the right. Non-digits are ignored. Returns
 * null when the amount would exceed MAX_ABS_CENTS, so the caller keeps the previous value.
 */
export function digitsToCents(text: string): number | null {
  const digits = text.replace(/\D/g, "").replace(/^0+/, "");
  if (digits === "") return 0;
  if (digits.length > MAX_DIGITS) return null;
  const cents = Number(digits); // exact: at most 16 digits, below 2^53
  return cents > MAX_ABS_CENTS ? null : cents;
}
