"use client";

import Link from "next/link";
import { useState } from "react";

import { FormError, FormField } from "@/components/form-field";
import { SelectField } from "@/components/select-field";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useAccounts } from "@/features/accounts/hooks";
import { useCategories } from "@/features/categories/hooks";
import { useMembers } from "@/features/households/hooks";
import { useTransactions, useVoidTransaction } from "@/features/ledger/hooks";
import {
  categoryOptions,
  kindLabel,
  methodLabel,
  namesById,
  paymentMethods,
  scopeLabel,
} from "@/features/ledger/labels";
import { QuickEntryButton } from "@/features/ledger/quick-entry-button";
import {
  errorMessage,
  type Account,
  type Category,
  type Member,
  type Transaction,
} from "@/lib/api/client";
import { formatDate } from "@/lib/dates";
import { formatBRL } from "@/lib/money";
import { useOnline } from "@/lib/use-online";

import { EditTransactionForm } from "./edit-transaction-form";
import {
  activeFilterCount,
  noFilters,
  toQuery,
  type FilterValues,
} from "./filters";

type Lookups = {
  categories: Category[];
  accounts: Account[];
  members: Member[];
  categoryName: Map<string, string>;
  accountName: Map<string, string>;
  memberName: Map<string, string>;
};

export function TransactionsView({ householdId }: { householdId: string }) {
  const categories = useCategories(householdId, true);
  const accounts = useAccounts(householdId, true);
  const members = useMembers(householdId);
  const [filters, setFilters] = useState<FilterValues>(noFilters);

  const failed = [categories, accounts, members].find((q) => q.isError);
  const ready = categories.data && accounts.data && members.data;
  const lookups: Lookups | null = ready
    ? {
        categories: categories.data,
        accounts: accounts.data.items,
        members: members.data,
        categoryName: namesById(categories.data),
        accountName: namesById(accounts.data.items),
        memberName: namesById(members.data),
      }
    : null;

  return (
    <>
      <div className="flex flex-col gap-1">
        <Link
          href={`/household?id=${householdId}`}
          className="text-sm text-muted-foreground underline"
        >
          Voltar
        </Link>
        <h1 className="text-2xl font-semibold tracking-tight">Lançamentos</h1>
        <p className="text-sm text-muted-foreground">
          Despesas (consumo) e receitas já realizadas. Previsões ainda não pagas
          ficam em{" "}
          <Link
            href={`/obligations?household=${householdId}`}
            className="underline"
          >
            contas a pagar e receber
          </Link>
          .
        </p>
      </div>
      {failed ? (
        <FormError message={errorMessage(failed.error)} />
      ) : !lookups ? (
        <p role="status">Carregando…</p>
      ) : (
        <>
          <Filters lookups={lookups} value={filters} onApply={setFilters} />
          <List householdId={householdId} filters={filters} lookups={lookups} />
        </>
      )}
      <QuickEntryButton householdId={householdId} />
    </>
  );
}

function Filters({
  lookups,
  value,
  onApply,
}: {
  lookups: Lookups;
  value: FilterValues;
  onApply: (v: FilterValues) => void;
}) {
  const [draft, setDraft] = useState(value);
  const set =
    (k: keyof FilterValues) =>
    (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
      setDraft((d) => ({
        ...d,
        [k]: e.target.value,
        // A category only makes sense for its kind.
        ...(k === "kind" ? { categoryId: "" } : {}),
      }));
  const count = activeFilterCount(value);
  const kinds = draft.kind
    ? [draft.kind as "EXPENSE" | "INCOME"]
    : (["EXPENSE", "INCOME"] as const);

  return (
    <details className="rounded-xl border px-4 py-3">
      <summary className="cursor-pointer font-medium">
        Filtros{count > 0 ? ` (${count})` : ""}
      </summary>
      <form
        className="mt-4 flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          onApply(draft);
        }}
      >
        <FormField
          label="Buscar"
          placeholder="Descrição, estabelecimento ou observação"
          maxLength={100}
          value={draft.q}
          onChange={set("q")}
        />
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField
            label="De"
            type="date"
            value={draft.from}
            onChange={set("from")}
          />
          <FormField
            label="Até"
            type="date"
            value={draft.to}
            onChange={set("to")}
          />
          <SelectField label="Tipo" value={draft.kind} onChange={set("kind")}>
            <option value="">Despesas e receitas</option>
            <option value="EXPENSE">Despesas</option>
            <option value="INCOME">Receitas</option>
          </SelectField>
          <SelectField
            label="Categoria"
            value={draft.categoryId}
            onChange={set("categoryId")}
          >
            <option value="">Todas</option>
            {kinds.flatMap((k) =>
              categoryOptions(lookups.categories, k).map((o) => (
                <option key={o.id} value={o.id}>
                  {o.label}
                </option>
              )),
            )}
          </SelectField>
          <SelectField
            label="Conta"
            value={draft.accountId}
            onChange={set("accountId")}
          >
            <option value="">Todas</option>
            {lookups.accounts.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </SelectField>
          <SelectField
            label="Meio"
            value={draft.paymentMethod}
            onChange={set("paymentMethod")}
          >
            <option value="">Todos</option>
            {paymentMethods.map((m) => (
              <option key={m} value={m}>
                {methodLabel[m]}
              </option>
            ))}
          </SelectField>
          <SelectField
            label="De quem é"
            value={draft.scope}
            onChange={set("scope")}
          >
            <option value="">Família e pessoal</option>
            <option value="FAMILY">Família</option>
            <option value="PERSONAL">Pessoal</option>
          </SelectField>
          <SelectField
            label="Pessoa"
            value={draft.personMemberId}
            onChange={set("personMemberId")}
          >
            <option value="">Todas</option>
            {lookups.members.map((m) => (
              <option key={m.id} value={m.id}>
                {m.name}
              </option>
            ))}
          </SelectField>
          <SelectField
            label="Situação"
            value={draft.status}
            onChange={set("status")}
          >
            <option value="ACTIVE">Ativos</option>
            <option value="VOIDED">Cancelados</option>
            <option value="ALL">Todos</option>
          </SelectField>
        </div>
        <div className="flex gap-2">
          <Button type="submit" size="sm">
            Aplicar
          </Button>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => {
              setDraft(noFilters);
              onApply(noFilters);
            }}
          >
            Limpar
          </Button>
        </div>
      </form>
    </details>
  );
}

function List({
  householdId,
  filters,
  lookups,
}: {
  householdId: string;
  filters: FilterValues;
  lookups: Lookups;
}) {
  const transactions = useTransactions(householdId, toQuery(filters));
  const items = transactions.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <Card>
      <CardHeader>
        <CardTitle>Consumo e receitas</CardTitle>
        <CardDescription>Mais recentes primeiro.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {transactions.isPending && <p role="status">Carregando…</p>}
        {transactions.isError && (
          <FormError message={errorMessage(transactions.error)} />
        )}
        {transactions.isSuccess && items.length === 0 && (
          <p className="text-sm text-muted-foreground">
            {activeFilterCount(filters) > 0
              ? "Nenhum lançamento com esses filtros."
              : "Nenhum lançamento ainda. Use o botão + para lançar."}
          </p>
        )}
        {items.length > 0 && (
          <ul className="flex flex-col divide-y">
            {items.map((t) => (
              <TransactionRow
                key={t.id}
                householdId={householdId}
                transaction={t}
                lookups={lookups}
              />
            ))}
          </ul>
        )}
        {transactions.hasNextPage && (
          <Button
            variant="outline"
            size="sm"
            className="self-start"
            disabled={transactions.isFetchingNextPage}
            onClick={() => transactions.fetchNextPage()}
          >
            {transactions.isFetchingNextPage ? "Carregando…" : "Carregar mais"}
          </Button>
        )}
      </CardContent>
    </Card>
  );
}

function TransactionRow({
  householdId,
  transaction: t,
  lookups,
}: {
  householdId: string;
  transaction: Transaction;
  lookups: Lookups;
}) {
  const [mode, setMode] = useState<"closed" | "open" | "edit" | "void">(
    "closed",
  );
  const voidTx = useVoidTransaction(householdId);
  const online = useOnline();
  const voided = t.status === "VOIDED";
  const income = t.kind === "INCOME";
  const person = t.personMemberId
    ? lookups.memberName.get(t.personMemberId)
    : undefined;
  const editMembers = lookups.members.filter(
    (m) => m.status === "ACTIVE" || m.id === t.personMemberId,
  );

  return (
    <li className="flex flex-col gap-2 py-2">
      <button
        type="button"
        className="flex items-center justify-between gap-3 text-left"
        aria-expanded={mode !== "closed"}
        onClick={() => setMode(mode === "closed" ? "open" : "closed")}
      >
        <span className="flex min-w-0 flex-col">
          <span className={`truncate ${voided ? "line-through" : ""}`}>
            {t.description}
          </span>
          <span className="truncate text-sm text-muted-foreground">
            {formatDate(t.occurredOn)} ·{" "}
            {lookups.categoryName.get(t.categoryId) ?? "Categoria"} ·{" "}
            {lookups.accountName.get(t.accountId) ?? "Conta"}
            {voided ? " · Cancelado" : ""}
          </span>
        </span>
        <span
          className={`shrink-0 tabular-nums ${voided ? "text-muted-foreground line-through" : ""}`}
        >
          {income ? "+" : "−"}
          {formatBRL(t.amountCents)}
        </span>
      </button>

      {mode === "open" && (
        <div className="flex flex-col gap-2 rounded-lg bg-muted/50 p-3 text-sm">
          <p>
            {kindLabel[t.kind]} ({income ? "receita" : "consumo"}) ·{" "}
            {methodLabel[t.paymentMethod]} · {scopeLabel[t.scope]}
            {person ? ` · ${person}` : ""}
          </p>
          {t.merchant && <p>Estabelecimento: {t.merchant}</p>}
          {t.notes && <p>Observações: {t.notes}</p>}
          {!voided && (
            <div className="flex flex-wrap gap-2">
              <Button
                size="sm"
                variant="outline"
                onClick={() => setMode("edit")}
              >
                Editar
              </Button>
              <Link
                href={`/entry?household=${householdId}&correct=${t.id}`}
                className="inline-flex h-7 items-center rounded-lg border px-2.5 text-[0.8rem] font-medium hover:bg-muted"
              >
                Corrigir valor ou data
              </Link>
              <Button
                size="sm"
                variant="destructive"
                onClick={() => setMode("void")}
              >
                Cancelar lançamento
              </Button>
            </div>
          )}
        </div>
      )}

      {mode === "edit" && (
        <EditTransactionForm
          householdId={householdId}
          transaction={t}
          categories={lookups.categories}
          members={editMembers}
          onClose={() => setMode("open")}
        />
      )}

      {mode === "void" && (
        <div
          role="alertdialog"
          aria-label="Confirmar cancelamento"
          className="flex flex-col gap-2 rounded-lg border border-destructive/40 p-3 text-sm"
        >
          <p>
            Cancelar &ldquo;{t.description}&rdquo; ({formatBRL(t.amountCents)})?
            Ele sai do {income ? "total de receitas" : "consumo"} e o dinheiro{" "}
            {income ? "sai de novo da" : "volta para a"} conta por um estorno. O
            histórico fica guardado.
          </p>
          <FormError
            message={voidTx.isError ? errorMessage(voidTx.error) : undefined}
          />
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="destructive"
              disabled={voidTx.isPending || !online}
              onClick={() =>
                voidTx.mutate(t.id, { onSuccess: () => setMode("closed") })
              }
            >
              {voidTx.isPending ? "Cancelando…" : "Confirmar cancelamento"}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setMode("open")}>
              Manter
            </Button>
          </div>
        </div>
      )}
    </li>
  );
}
