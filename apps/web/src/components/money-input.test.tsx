import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it } from "vitest";

import { MoneyInput } from "./money-input";

function Harness({ allowNegative }: { allowNegative?: boolean }) {
  const [cents, setCents] = useState(0);
  return (
    <>
      <MoneyInput
        label="Valor"
        value={cents}
        onChange={setCents}
        allowNegative={allowNegative}
      />
      <output data-testid="cents">{cents}</output>
    </>
  );
}

function type(text: string) {
  fireEvent.change(screen.getByLabelText("Valor"), {
    target: { value: text },
  });
}

const field = () => screen.getByLabelText<HTMLInputElement>("Valor");
const cents = () => screen.getByTestId("cents").textContent;

afterEach(cleanup);

describe("MoneyInput", () => {
  it("fills from the right like a cash register", () => {
    render(<Harness />);
    expect(field().value).toBe("0,00");
    type("0,001");
    expect(field().value).toBe("0,01");
    type("0,012");
    type("0,123");
    type("1,234");
    expect(field().value).toBe("12,34");
    expect(cents()).toBe("1234");
  });

  it("formats thousands and ignores non-digits", () => {
    render(<Harness />);
    type("R$ 1a2b3c4d5d6");
    expect(field().value).toBe("1.234,56");
    expect(cents()).toBe("123456");
  });

  it("removes digits from the right on backspace", () => {
    render(<Harness />);
    type("12,34");
    type("12,3");
    expect(field().value).toBe("1,23");
    expect(cents()).toBe("123");
  });

  it("keeps the previous value above the API limit", () => {
    render(<Harness />);
    type("10.000.000.000.000,00");
    expect(cents()).toBe("1000000000000000");
    type("10.000.000.000.000,001");
    expect(cents()).toBe("1000000000000000");
  });

  it("ignores the minus sign unless negatives are allowed", () => {
    render(<Harness />);
    type("-5");
    expect(cents()).toBe("5");
  });

  it("accepts a negative amount when allowed, even typed before the digits", () => {
    render(<Harness allowNegative />);
    type("-");
    expect(field().value).toBe("-0,00");
    type("-0,005");
    expect(cents()).toBe("-5");
    type("0,05");
    expect(cents()).toBe("5");
  });

  it("exposes an accessible error", () => {
    render(
      <MoneyInput
        label="Valor"
        value={0}
        onChange={() => {}}
        error="Informe um valor maior que zero."
      />,
    );
    expect(field().getAttribute("aria-invalid")).toBe("true");
    expect(field().getAttribute("aria-describedby")).toBeTruthy();
  });
});
