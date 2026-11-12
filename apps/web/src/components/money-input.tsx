"use client";

import { useId, useState } from "react";

import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { digitsToCents, formatDecimal } from "@/lib/money";

type Props = Omit<
  React.ComponentProps<"input">,
  "value" | "onChange" | "type" | "inputMode"
> & {
  label: string;
  /** Amount in integer cents. */
  value: number;
  onChange: (cents: number) => void;
  error?: string;
  /** Accepts a "-" anywhere in the text to make the amount negative (e.g. opening balance). */
  allowNegative?: boolean;
  /** Highlighted amount (quick entry). */
  large?: boolean;
};

/**
 * Money field filled from the right like a cash register: typing 1, 2, 3, 4 shows
 * "12,34" and yields 1234 cents. The value is never parsed as a float.
 */
export function MoneyInput({
  label,
  value,
  onChange,
  error,
  allowNegative = false,
  large = false,
  id,
  ...input
}: Props) {
  const generated = useId();
  const inputId = id ?? generated;
  const errorId = `${inputId}-error`;
  // Remembers a typed "-" while the amount is still zero (zero has no sign).
  const [negativeZero, setNegativeZero] = useState(false);
  const negative = value < 0 || (value === 0 && negativeZero);
  const text = (negative ? "-" : "") + formatDecimal(Math.abs(value));

  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={inputId}>{label}</Label>
      <div className="relative">
        <span
          aria-hidden="true"
          className={`pointer-events-none absolute inset-y-0 left-2.5 flex items-center text-muted-foreground ${large ? "text-lg" : "text-sm"}`}
        >
          R$
        </span>
        <Input
          id={inputId}
          type="text"
          inputMode={allowNegative ? "text" : "numeric"}
          autoComplete="off"
          className={
            large
              ? "h-14 pl-12 text-right text-3xl font-semibold tabular-nums md:text-3xl"
              : "pl-9 text-right tabular-nums"
          }
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? errorId : undefined}
          value={text}
          onChange={(e) => {
            const cents = digitsToCents(e.target.value);
            if (cents === null) return; // over the limit: keep the previous amount
            const neg = allowNegative && e.target.value.includes("-");
            setNegativeZero(neg && cents === 0);
            onChange(neg ? -cents : cents);
          }}
          {...input}
        />
      </div>
      {error && (
        <p id={errorId} className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}
