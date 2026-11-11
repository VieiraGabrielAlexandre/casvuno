"use client";

import Link from "next/link";
import { useState } from "react";

import { FormError } from "@/components/form-field";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useHousehold, useMembers } from "@/features/households/hooks";
import { errorMessage, type Account, type Movement } from "@/lib/api/client";
import { formatDate, todayIn } from "@/lib/dates";
import { formatBRL } from "@/lib/money";

import { AccountForm } from "./account-form";
import { CashTag, ownerText } from "./accounts-view";
import { useAccount, useStatement, useUpdateAccount } from "./hooks";
import { accountTypeLabel, movementKindLabel } from "./labels";

export function AccountDetail({
  householdId,
  accountId,
}: {
  householdId: string;
  accountId: string;
}) {
  const account = useAccount(householdId, accountId);
  const members = useMembers(householdId);
  const [editing, setEditing] = useState(false);
  const back = (
    <Link
      href={`/accounts?household=${householdId}`}
      className="text-sm text-muted-foreground underline"
    >
      Voltar para as contas
    </Link>
  );

  if (account.isPending) return <p role="status">Carregando…</p>;
  if (account.isError) {
    return (
      <>
        {back}
        <FormError message={errorMessage(account.error)} />
      </>
    );
  }
  const a = account.data;

  return (
    <>
      <div className="flex flex-col gap-1">
        {back}
        <h1 className="text-2xl font-semibold tracking-tight">{a.name}</h1>
        <p className="text-sm text-muted-foreground">
          {accountTypeLabel[a.type]} · {ownerText(a, members.data ?? [])}
          {!a.isActive && " · arquivada"}
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardDescription>
            Saldo <CashTag />
          </CardDescription>
          <CardTitle
            className={`text-3xl tabular-nums ${a.balanceCents < 0 ? "text-destructive" : ""}`}
          >
            {formatBRL(a.balanceCents)}
          </CardTitle>
          <CardDescription>
            Saldo inicial de {formatBRL(a.openingBalanceCents)} em{" "}
            {formatDate(a.openedOn)}, mais entradas e menos saídas.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {a.isActive ? (
            <div className="flex flex-wrap gap-2">
              <Link
                href={`/transfers?household=${householdId}&from=${a.id}`}
                className={buttonVariants({ size: "sm" })}
              >
                Transferir
              </Link>
              {!editing && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setEditing(true)}
                >
                  Editar
                </Button>
              )}
              <ArchiveButton householdId={householdId} account={a} />
            </div>
          ) : (
            <RestoreButton householdId={householdId} account={a} />
          )}
        </CardContent>
      </Card>

      {editing && a.isActive && (
        <EditCard
          householdId={householdId}
          account={a}
          onDone={() => setEditing(false)}
        />
      )}

      <Statement householdId={householdId} accountId={a.id} />
    </>
  );
}

function EditCard({
  householdId,
  account,
  onDone,
}: {
  householdId: string;
  account: Account;
  onDone: () => void;
}) {
  const household = useHousehold(householdId);
  const members = useMembers(householdId);
  return (
    <Card>
      <CardHeader>
        <CardTitle>Editar conta</CardTitle>
        <CardDescription>
          Corrigir o saldo inicial altera o saldo atual na mesma medida.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {household.isPending || members.isPending ? (
          <p role="status">Carregando…</p>
        ) : household.isError || members.isError ? (
          <FormError message={errorMessage(household.error ?? members.error)} />
        ) : (
          <AccountForm
            householdId={householdId}
            members={members.data}
            defaultOwnerId={household.data.memberId}
            today={todayIn(household.data.timezone)}
            account={account}
            onDone={onDone}
          />
        )}
      </CardContent>
    </Card>
  );
}

function ArchiveButton({
  householdId,
  account,
}: {
  householdId: string;
  account: Account;
}) {
  const update = useUpdateAccount(householdId, account.id);
  const hasBalance = account.balanceCents !== 0;
  return (
    <div className="flex basis-full flex-col gap-1">
      <Button
        variant="ghost"
        size="sm"
        className="self-start"
        disabled={hasBalance || update.isPending}
        onClick={() => {
          if (
            window.confirm(
              `Arquivar ${account.name}? Ela some dos novos lançamentos, mas o histórico é preservado.`,
            )
          ) {
            update.mutate({ isActive: false });
          }
        }}
      >
        Arquivar
      </Button>
      {hasBalance && (
        <p className="text-sm text-muted-foreground">
          Para arquivar, deixe o saldo em zero (transfira o saldo para outra
          conta).
        </p>
      )}
      {update.isError && <FormError message={errorMessage(update.error)} />}
    </div>
  );
}

function RestoreButton({
  householdId,
  account,
}: {
  householdId: string;
  account: Account;
}) {
  const update = useUpdateAccount(householdId, account.id);
  return (
    <div className="flex flex-col gap-2">
      <p className="text-sm text-muted-foreground">
        Conta arquivada: não recebe novos lançamentos nem transferências.
      </p>
      <Button
        variant="outline"
        size="sm"
        className="self-start"
        disabled={update.isPending}
        onClick={() => update.mutate({ isActive: true })}
      >
        Restaurar
      </Button>
      {update.isError && <FormError message={errorMessage(update.error)} />}
    </div>
  );
}

function Statement({
  householdId,
  accountId,
}: {
  householdId: string;
  accountId: string;
}) {
  const statement = useStatement(householdId, accountId);
  const movements = statement.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <Card>
      <CardHeader>
        <CardTitle>Extrato</CardTitle>
        <CardDescription>
          Movimentos de caixa, mais recentes primeiro.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {statement.isPending && <p role="status">Carregando…</p>}
        {statement.isError && (
          <FormError message={errorMessage(statement.error)} />
        )}
        {statement.isSuccess && movements.length === 0 && (
          <p className="text-sm text-muted-foreground">
            Nenhum movimento ainda.
          </p>
        )}
        {movements.length > 0 && (
          <ul className="flex flex-col divide-y">
            {movements.map((m) => (
              <MovementRow key={m.id} movement={m} />
            ))}
          </ul>
        )}
        {statement.hasNextPage && (
          <Button
            variant="outline"
            size="sm"
            className="self-start"
            disabled={statement.isFetchingNextPage}
            onClick={() => statement.fetchNextPage()}
          >
            {statement.isFetchingNextPage ? "Carregando…" : "Carregar mais"}
          </Button>
        )}
      </CardContent>
    </Card>
  );
}

function MovementRow({ movement: m }: { movement: Movement }) {
  const incoming = m.direction === "IN";
  return (
    <li className="flex items-center justify-between gap-3 py-2">
      <div className="flex min-w-0 flex-col">
        <span className="truncate">{m.description}</span>
        <span className="text-sm text-muted-foreground">
          {formatDate(m.postedOn)} · {movementKindLabel[m.kind]}
        </span>
      </div>
      <span
        className={`shrink-0 tabular-nums ${incoming ? "text-emerald-700 dark:text-emerald-400" : ""}`}
      >
        <span className="sr-only">{incoming ? "Entrada" : "Saída"} </span>
        {incoming ? "+" : "−"}
        {formatBRL(m.amountCents)}
      </span>
    </li>
  );
}
