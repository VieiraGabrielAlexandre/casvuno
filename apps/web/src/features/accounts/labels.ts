import type { AccountType, Movement, Ownership } from "@/lib/api/client";

export const accountTypeLabel: Record<AccountType, string> = {
  CHECKING: "Conta corrente",
  SAVINGS: "Poupança",
  CASH: "Dinheiro",
  OTHER: "Outra",
};

export const ownershipLabel: Record<Ownership, string> = {
  INDIVIDUAL: "Individual",
  JOINT: "Conjunta",
};

export const movementKindLabel: Record<Movement["kind"], string> = {
  DIRECT_PAYMENT: "Pagamento",
  DIRECT_RECEIPT: "Recebimento",
  INVOICE_PAYMENT: "Pagamento de fatura",
  TRANSFER: "Transferência",
  REVERSAL: "Estorno",
};
