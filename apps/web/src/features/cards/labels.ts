import type { CreditCard, Invoice } from "@/lib/api/client";

const months = [
  "janeiro",
  "fevereiro",
  "março",
  "abril",
  "maio",
  "junho",
  "julho",
  "agosto",
  "setembro",
  "outubro",
  "novembro",
  "dezembro",
];

/** "2026-12" → "dezembro de 2026": an invoice's reference month is its due month (ADR 0003). */
export function referenceMonthLabel(referenceMonth: string): string {
  const m = /^(\d{4})-(\d{2})$/.exec(referenceMonth);
  if (!m) return referenceMonth;
  const name = months[Number(m[2]) - 1];
  return name ? `${name} de ${m[1]}` : referenceMonth;
}

/** "Nubank •••• 1234" or just the name. */
export function cardName(card: Pick<CreditCard, "name" | "lastFour">): string {
  return card.lastFour ? `${card.name} •••• ${card.lastFour}` : card.name;
}

/**
 * One short label for an invoice: lifecycle (open, closing pending, closed) and the payment
 * situation derived from its balance.
 */
export function invoiceSituation(inv: Invoice): string {
  if (inv.totalCents === 0 && inv.paidCents === 0) {
    return inv.status === "CLOSED"
      ? "Fechada · sem lançamentos"
      : "Sem lançamentos";
  }
  if (inv.paymentStatus === "PAID") return "Paga";
  const paid = inv.paymentStatus === "PARTIALLY_PAID" ? " · paga em parte" : "";
  if (inv.status === "CLOSED") return `Fechada${paid}`;
  if (inv.closingPending) return `Fechamento pendente${paid}`;
  return `Aberta${paid}`;
}

/** First day of the month `n` months before `today` ("2026-11-21", 3 → "2026-08-01"). */
export function monthsBefore(today: string, n: number): string {
  const m = /^(\d{4})-(\d{2})-\d{2}$/.exec(today);
  if (!m) return today;
  const index = Number(m[1]) * 12 + (Number(m[2]) - 1) - n;
  const year = Math.floor(index / 12);
  const month = (index % 12) + 1;
  return `${year}-${String(month).padStart(2, "0")}-01`;
}
