"use client";

import Link from "next/link";

import { FormError } from "@/components/form-field";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useAccounts } from "@/features/accounts/hooks";
import { useCategories } from "@/features/categories/hooks";
import { useHousehold, useMembers } from "@/features/households/hooks";
import { errorMessage } from "@/lib/api/client";
import { todayIn } from "@/lib/dates";

import { EntryForm } from "./entry-form";
import { useLastOwnTransaction, useTemplates, useTransaction } from "./hooks";

/** Quick entry page; with `correctId`, "Corrigir" (void + new entry, I-18). */
export function EntryView({
  householdId,
  correctId = "",
}: {
  householdId: string;
  correctId?: string;
}) {
  const household = useHousehold(householdId);
  const accounts = useAccounts(householdId);
  const categories = useCategories(householdId, true);
  const members = useMembers(householdId);
  const templates = useTemplates(householdId);
  const last = useLastOwnTransaction(householdId);
  const correcting = useTransaction(householdId, correctId);
  const queries: { isError: boolean; isPending: boolean; error: unknown }[] = [
    household,
    accounts,
    categories,
    members,
    templates,
    last,
  ];
  if (correctId) queries.push(correcting);
  const failed = queries.find((q) => q.isError);
  const back = `/transactions?household=${householdId}`;

  return (
    <>
      <div className="flex flex-col gap-1">
        <Link
          href={correctId ? back : `/household?id=${householdId}`}
          className="text-sm text-muted-foreground underline"
        >
          {correctId ? "Voltar para os lançamentos" : "Voltar"}
        </Link>
        <h1 className="text-2xl font-semibold tracking-tight">
          {correctId ? "Corrigir lançamento" : "Novo lançamento"}
        </h1>
        {correctId && (
          <p className="text-sm text-muted-foreground">
            Valor, data, meio e conta não se editam: o lançamento original é
            cancelado (o dinheiro volta para a conta) e este é lançado no lugar.
          </p>
        )}
      </div>
      <Card>
        <CardHeader>
          <CardTitle>Despesa ou receita</CardTitle>
          <CardDescription>
            Pago agora entra no consumo e no caixa; previsto entra só nos
            compromissos até ser pago.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {failed ? (
            <FormError message={errorMessage(failed.error)} />
          ) : queries.some((q) => q.isPending) ? (
            <p role="status">Carregando…</p>
          ) : correctId && correcting.data?.status === "VOIDED" ? (
            <p className="text-sm text-muted-foreground">
              Este lançamento já foi cancelado.{" "}
              <Link href={back} className="underline">
                Ver lançamentos
              </Link>
            </p>
          ) : accounts.data!.items.every((a) => !a.isActive) ? (
            <p className="text-sm text-muted-foreground">
              Cadastre uma conta antes de lançar.{" "}
              <Link
                href={`/accounts?household=${householdId}`}
                className="underline"
              >
                Cadastrar conta
              </Link>
            </p>
          ) : (
            <EntryForm
              householdId={householdId}
              accounts={accounts.data!.items.filter((a) => a.isActive)}
              categories={categories.data!}
              members={members.data!.filter((m) => m.status === "ACTIVE")}
              today={todayIn(household.data!.timezone)}
              templates={templates.data!}
              last={last.data ?? null}
              correcting={correctId ? correcting.data : undefined}
            />
          )}
        </CardContent>
      </Card>
      {!correctId && (
        <p className="text-sm text-muted-foreground">
          <Link
            href={`/favorites?household=${householdId}`}
            className="underline"
          >
            Gerenciar favoritos
          </Link>
        </p>
      )}
    </>
  );
}
