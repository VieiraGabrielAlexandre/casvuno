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
import { errorMessage, type Account, type Member } from "@/lib/api/client";
import { todayIn } from "@/lib/dates";
import { formatBRL } from "@/lib/money";

import { AccountForm } from "./account-form";
import { useAccounts } from "./hooks";
import { accountTypeLabel, ownershipLabel } from "./labels";

export function AccountsView({ householdId }: { householdId: string }) {
  const [showArchived, setShowArchived] = useState(false);
  const [creating, setCreating] = useState(false);
  const accounts = useAccounts(householdId, showArchived);
  const household = useHousehold(householdId);
  const members = useMembers(householdId);

  if (accounts.isPending) return <p role="status">Carregando…</p>;
  if (accounts.isError) {
    return <FormError message={errorMessage(accounts.error)} />;
  }
  const { items, totalBalanceCents } = accounts.data;
  const active = items.filter((a) => a.isActive);

  return (
    <>
      <div className="flex flex-col gap-1">
        <Link
          href={`/household?id=${householdId}`}
          className="text-sm text-muted-foreground underline"
        >
          Voltar para a família
        </Link>
        <h1 className="text-2xl font-semibold tracking-tight">Contas</h1>
      </div>

      <Card>
        <CardHeader>
          <CardDescription>
            Caixa consolidado <CashTag />
          </CardDescription>
          <CardTitle
            className={`text-3xl tabular-nums ${totalBalanceCents < 0 ? "text-destructive" : ""}`}
          >
            {formatBRL(totalBalanceCents)}
          </CardTitle>
          <CardDescription>
            Soma dos saldos de todas as contas da família, individuais e
            conjuntas. Faturas de cartão não entram aqui.
          </CardDescription>
        </CardHeader>
        {active.length >= 2 && (
          <CardContent>
            <Link
              href={`/transfers?household=${householdId}`}
              className={buttonVariants({ variant: "outline", size: "sm" })}
            >
              Transferir entre contas
            </Link>
          </CardContent>
        )}
      </Card>

      {creating ? (
        <Card>
          <CardHeader>
            <CardTitle>Nova conta</CardTitle>
          </CardHeader>
          <CardContent>
            {household.isPending || members.isPending ? (
              <p role="status">Carregando…</p>
            ) : household.isError || members.isError ? (
              <FormError
                message={errorMessage(household.error ?? members.error)}
              />
            ) : (
              <AccountForm
                householdId={householdId}
                members={members.data}
                defaultOwnerId={household.data.memberId}
                today={todayIn(household.data.timezone)}
                onDone={() => setCreating(false)}
              />
            )}
          </CardContent>
        </Card>
      ) : (
        <Button className="self-start" onClick={() => setCreating(true)}>
          Nova conta
        </Button>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Saldos</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {items.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Nenhuma conta ainda. Cadastre a primeira para acompanhar o caixa
              da família.
            </p>
          ) : (
            <ul className="flex flex-col divide-y">
              {items.map((a) => (
                <AccountRow
                  key={a.id}
                  account={a}
                  householdId={householdId}
                  members={members.data ?? []}
                />
              ))}
            </ul>
          )}
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={showArchived}
              onChange={(e) => setShowArchived(e.target.checked)}
            />
            Mostrar arquivadas
          </label>
        </CardContent>
      </Card>
    </>
  );
}

/** Marks a figure as part of the cash view (AGENTS.md §5: every metric names its view). */
export function CashTag() {
  return (
    <span className="ml-1 rounded-full border px-2 py-0.5 text-xs font-medium">
      caixa
    </span>
  );
}

export function ownerText(account: Account, members: Member[]): string {
  if (account.ownership === "JOINT") return ownershipLabel.JOINT;
  const owner = members.find((m) => m.id === account.ownerMemberId);
  return owner
    ? `${ownershipLabel.INDIVIDUAL} · ${owner.name}`
    : ownershipLabel.INDIVIDUAL;
}

function AccountRow({
  account,
  householdId,
  members,
}: {
  account: Account;
  householdId: string;
  members: Member[];
}) {
  return (
    <li>
      <Link
        href={`/account?household=${householdId}&id=${account.id}`}
        className="flex items-center justify-between gap-3 py-3 hover:bg-muted/50"
      >
        <div className="flex min-w-0 flex-col">
          <span
            className={`truncate font-medium ${account.isActive ? "" : "text-muted-foreground line-through"}`}
          >
            {account.name}
          </span>
          <span className="truncate text-sm text-muted-foreground">
            {accountTypeLabel[account.type]} · {ownerText(account, members)}
            {!account.isActive && " · arquivada"}
          </span>
        </div>
        <span
          className={`shrink-0 tabular-nums ${account.balanceCents < 0 ? "text-destructive" : ""}`}
        >
          {formatBRL(account.balanceCents)}
        </span>
      </Link>
    </li>
  );
}
