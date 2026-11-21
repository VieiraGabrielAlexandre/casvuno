"use client";

import Link from "next/link";
import { useState } from "react";

import { FormError } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useHousehold, useMembers } from "@/features/households/hooks";
import { errorMessage, type Invoice } from "@/lib/api/client";
import { formatDate, todayIn } from "@/lib/dates";
import { formatBRL } from "@/lib/money";
import { useOnline } from "@/lib/use-online";

import { CardForm } from "./card-form";
import { useCard, useCardInvoices, useUpdateCard } from "./hooks";
import {
  cardName,
  invoiceSituation,
  monthsBefore,
  referenceMonthLabel,
} from "./labels";
import { CommitmentTag, EstimatedTag } from "./tags";

/** A card: estimated limit, edit/archive/restore and its invoices by reference month. */
export function CardDetail({
  householdId,
  cardId,
}: {
  householdId: string;
  cardId: string;
}) {
  const card = useCard(householdId, cardId);
  const household = useHousehold(householdId);
  const members = useMembers(householdId);
  const update = useUpdateCard(householdId, cardId);
  const online = useOnline();
  const [editing, setEditing] = useState(false);
  const [showOld, setShowOld] = useState(false);
  const today = household.data ? todayIn(household.data.timezone) : "";
  const invoices = useCardInvoices(
    householdId,
    household.data ? cardId : "",
    showOld ? undefined : monthsBefore(today, 3),
  );
  const back = (
    <Link
      href={`/cards?household=${householdId}`}
      className="text-sm text-muted-foreground underline"
    >
      Voltar para os cartões
    </Link>
  );

  if (card.isPending || household.isPending) {
    return <p role="status">Carregando…</p>;
  }
  if (card.isError || household.isError) {
    return (
      <>
        {back}
        <FormError message={errorMessage(card.error ?? household.error)} />
      </>
    );
  }
  const c = card.data;
  const list = invoices.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <>
      <div className="flex flex-col gap-1">
        {back}
        <h1 className="text-2xl font-semibold tracking-tight">{cardName(c)}</h1>
        <p className="text-sm text-muted-foreground">
          Fecha dia {c.closingDay} · vence dia {c.dueDay}
          {!c.isActive && " · arquivado"}
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardDescription>
            Limite disponível <EstimatedTag />
          </CardDescription>
          <CardTitle
            className={`text-3xl tabular-nums ${c.estimatedAvailableLimitCents < 0 ? "text-destructive" : ""}`}
          >
            {formatBRL(c.estimatedAvailableLimitCents)}
          </CardTitle>
          <CardDescription>
            Limite de {formatBRL(c.limitCents)} menos{" "}
            {formatBRL(c.outstandingCents)} em aberto nas faturas (inclusive
            parcelas futuras). É uma estimativa: não é sincronizado com o banco.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {editing ? (
            members.isPending ? (
              <p role="status">Carregando…</p>
            ) : members.isError ? (
              <FormError message={errorMessage(members.error)} />
            ) : (
              <CardForm
                householdId={householdId}
                members={members.data}
                defaultHolderId={c.holderMemberId}
                card={c}
                onDone={() => setEditing(false)}
              />
            )
          ) : (
            <div className="flex flex-wrap gap-2">
              {c.isActive && (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => setEditing(true)}
                >
                  Editar
                </Button>
              )}
              <Button
                size="sm"
                variant="outline"
                disabled={update.isPending || !online}
                onClick={() => update.mutate({ isActive: !c.isActive })}
              >
                {c.isActive ? "Arquivar" : "Restaurar"}
              </Button>
            </div>
          )}
          {c.isActive && !editing && (
            <p className="text-sm text-muted-foreground">
              Só é possível arquivar um cartão sem saldo em aberto em nenhuma
              fatura, inclusive as futuras.
            </p>
          )}
          <FormError
            message={update.isError ? errorMessage(update.error) : undefined}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Faturas</CardTitle>
          <CardDescription>
            Por mês de vencimento. Faturas futuras mostram as parcelas a vencer.
            Saldos de fatura são compromissos, não consumo.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {invoices.isPending ? (
            <p role="status">Carregando…</p>
          ) : invoices.isError ? (
            <FormError message={errorMessage(invoices.error)} />
          ) : list.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Nenhuma fatura {showOld ? "" : "recente ou futura "}ainda. As
              faturas são criadas pelas compras no cartão.
            </p>
          ) : (
            <ul className="flex flex-col divide-y">
              {list.map((inv) => (
                <InvoiceRow
                  key={inv.id}
                  invoice={inv}
                  householdId={householdId}
                  today={today}
                />
              ))}
            </ul>
          )}
          {invoices.hasNextPage && (
            <Button
              variant="outline"
              size="sm"
              className="self-start"
              disabled={invoices.isFetchingNextPage}
              onClick={() => invoices.fetchNextPage()}
            >
              {invoices.isFetchingNextPage ? "Carregando…" : "Carregar mais"}
            </Button>
          )}
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={showOld}
              onChange={(e) => setShowOld(e.target.checked)}
            />
            Mostrar faturas antigas
          </label>
        </CardContent>
      </Card>
    </>
  );
}

function InvoiceRow({
  invoice: inv,
  householdId,
  today,
}: {
  invoice: Invoice;
  householdId: string;
  today: string;
}) {
  const future = inv.closingOn > today;
  return (
    <li>
      <Link
        href={`/invoice?household=${householdId}&id=${inv.id}`}
        className="flex items-center justify-between gap-3 py-3 hover:bg-muted/50"
      >
        <div className="flex min-w-0 flex-col">
          <span className="font-medium first-letter:uppercase">
            {referenceMonthLabel(inv.referenceMonth)}
          </span>
          <span
            className={`text-sm ${inv.closingPending ? "font-medium text-destructive" : "text-muted-foreground"}`}
          >
            {invoiceSituation(inv)} · vence {formatDate(inv.dueOn)}
            {future &&
              inv.installmentsCount > 0 &&
              ` · ${inv.installmentsCount} parcela${inv.installmentsCount > 1 ? "s" : ""} a vencer`}
          </span>
        </div>
        <div className="flex shrink-0 flex-col items-end text-sm">
          <span className="tabular-nums">
            {formatBRL(inv.outstandingCents)}
          </span>
          {inv.paidCents > 0 && (
            <span className="text-muted-foreground tabular-nums">
              de {formatBRL(inv.totalCents)}
            </span>
          )}
          <CommitmentTag />
        </div>
      </Link>
    </li>
  );
}
