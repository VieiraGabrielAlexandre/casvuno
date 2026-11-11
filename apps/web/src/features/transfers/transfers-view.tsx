"use client";

import Link from "next/link";

import { FormError } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useAccounts } from "@/features/accounts/hooks";
import { useHousehold } from "@/features/households/hooks";
import { errorMessage, type Account, type Transfer } from "@/lib/api/client";
import { formatDate, todayIn } from "@/lib/dates";
import { formatBRL } from "@/lib/money";

import { useTransfers } from "./hooks";
import { TransferForm } from "./transfer-form";

export function TransfersView({
  householdId,
  fromAccountId,
}: {
  householdId: string;
  fromAccountId?: string;
}) {
  const accounts = useAccounts(householdId);
  const household = useHousehold(householdId);

  return (
    <>
      <div className="flex flex-col gap-1">
        <Link
          href={`/accounts?household=${householdId}`}
          className="text-sm text-muted-foreground underline"
        >
          Voltar para as contas
        </Link>
        <h1 className="text-2xl font-semibold tracking-tight">
          Transferências
        </h1>
        <p className="text-sm text-muted-foreground">
          Transferir entre contas da família não é receita nem despesa: o caixa
          consolidado não muda.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Nova transferência</CardTitle>
          <CardDescription>
            Não dá para editar depois: para corrigir, faça a transferência
            inversa.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {accounts.isPending || household.isPending ? (
            <p role="status">Carregando…</p>
          ) : accounts.isError || household.isError ? (
            <FormError
              message={errorMessage(accounts.error ?? household.error)}
            />
          ) : (
            <FormOrEmpty
              householdId={householdId}
              accounts={accounts.data.items.filter((a) => a.isActive)}
              today={todayIn(household.data.timezone)}
              fromAccountId={fromAccountId}
            />
          )}
        </CardContent>
      </Card>

      <History householdId={householdId} />
    </>
  );
}

function FormOrEmpty({
  householdId,
  accounts,
  today,
  fromAccountId,
}: {
  householdId: string;
  accounts: Account[];
  today: string;
  fromAccountId?: string;
}) {
  if (accounts.length < 2) {
    return (
      <p className="text-sm text-muted-foreground">
        Você precisa de pelo menos duas contas ativas para transferir.{" "}
        <Link href={`/accounts?household=${householdId}`} className="underline">
          Cadastrar contas
        </Link>
      </p>
    );
  }
  return (
    <TransferForm
      householdId={householdId}
      accounts={accounts}
      today={today}
      defaultFromId={fromAccountId}
    />
  );
}

function History({ householdId }: { householdId: string }) {
  const transfers = useTransfers(householdId);
  const items = transfers.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <Card>
      <CardHeader>
        <CardTitle>Histórico</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {transfers.isPending && <p role="status">Carregando…</p>}
        {transfers.isError && (
          <FormError message={errorMessage(transfers.error)} />
        )}
        {transfers.isSuccess && items.length === 0 && (
          <p className="text-sm text-muted-foreground">
            Nenhuma transferência ainda.
          </p>
        )}
        {items.length > 0 && (
          <ul className="flex flex-col divide-y">
            {items.map((t) => (
              <TransferRow key={t.id} transfer={t} />
            ))}
          </ul>
        )}
        {transfers.hasNextPage && (
          <Button
            variant="outline"
            size="sm"
            className="self-start"
            disabled={transfers.isFetchingNextPage}
            onClick={() => transfers.fetchNextPage()}
          >
            {transfers.isFetchingNextPage ? "Carregando…" : "Carregar mais"}
          </Button>
        )}
      </CardContent>
    </Card>
  );
}

function TransferRow({ transfer: t }: { transfer: Transfer }) {
  return (
    <li className="flex items-center justify-between gap-3 py-2">
      <div className="flex min-w-0 flex-col">
        <span className="truncate">
          {t.fromAccountName} → {t.toAccountName}
        </span>
        <span className="truncate text-sm text-muted-foreground">
          {formatDate(t.postedOn)} · {t.description}
        </span>
      </div>
      <span className="shrink-0 tabular-nums">{formatBRL(t.amountCents)}</span>
    </li>
  );
}
