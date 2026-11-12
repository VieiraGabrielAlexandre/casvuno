import type {
  Category,
  Obligation,
  PaymentMethod,
  Scope,
  TransactionKind,
} from "@/lib/api/client";
import { buildTree } from "@/features/categories/tree";

export const kindLabel: Record<TransactionKind, string> = {
  EXPENSE: "Despesa",
  INCOME: "Receita",
};

export const methodLabel: Record<PaymentMethod, string> = {
  PIX: "Pix",
  DEBIT: "Débito",
  CASH: "Dinheiro",
  BANK_TRANSFER: "Transferência bancária",
  BOLETO: "Boleto",
  OTHER: "Outro",
};

export const paymentMethods = Object.keys(methodLabel) as PaymentMethod[];

export const scopeLabel: Record<Scope, string> = {
  FAMILY: "Família",
  PERSONAL: "Pessoal",
};

export const obligationKindLabel: Record<Obligation["kind"], string> = {
  PAYABLE: "A pagar",
  RECEIVABLE: "A receber",
};

/** Situation of an obligation in one short label (status + derived payment status). */
export function obligationSituation(o: Obligation): string {
  if (o.status === "CANCELLED") return "Cancelada";
  if (o.paymentStatus === "PAID") {
    return o.kind === "PAYABLE" ? "Paga" : "Recebida";
  }
  if (o.isOverdue) return "Vencida";
  if (o.paymentStatus === "PARTIALLY_PAID") return "Parcial";
  return o.status === "PLANNED" ? "Prevista" : "Em aberto";
}

export type CategoryOption = { id: string; label: string };

/**
 * Active categories of one kind as select options, subcategories indented under their
 * parent. `keep` adds a category even if archived (an existing entry may still use it).
 */
export function categoryOptions(
  categories: Category[],
  kind: TransactionKind,
  keep?: string,
): CategoryOption[] {
  const usable = categories.filter(
    (c) => c.kind === kind && (c.isActive || c.id === keep),
  );
  const out: CategoryOption[] = [];
  for (const node of buildTree(usable)[kind]) {
    out.push({ id: node.id, label: node.name });
    for (const child of node.children) {
      out.push({ id: child.id, label: `— ${child.name}` });
    }
  }
  return out;
}

/** id → name for categories, accounts or members (archived ones included). */
export function namesById(list: { id: string; name: string }[]) {
  return new Map(list.map((x) => [x.id, x.name]));
}
