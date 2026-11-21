import { formatBRL } from "@/lib/money";

type VoidableTransaction = {
  description: string;
  amountCents: number;
  kind: "EXPENSE" | "INCOME";
  creditCardId?: string | null;
};

/**
 * Confirmation text of "Cancelar lançamento". A card purchase moves no cash: its
 * installments leave the invoices (F4.6, RN-019); other entries are reversed in the account.
 */
export function voidMessage(t: VoidableTransaction): string {
  const head = `Cancelar “${t.description}” (${formatBRL(t.amountCents)})?`;
  if (t.creditCardId) {
    return `${head} A compra sai do consumo e as parcelas saem das faturas do cartão. Pagamentos de fatura já feitos continuam registrados. O histórico fica guardado.`;
  }
  if (t.kind === "INCOME") {
    return `${head} Ele sai do total de receitas e o dinheiro sai de novo da conta por um estorno. O histórico fica guardado.`;
  }
  return `${head} Ele sai do consumo e o dinheiro volta para a conta por um estorno. O histórico fica guardado.`;
}
