"use client";

import Link from "next/link";
import { useState } from "react";

import { FormError } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useHousehold, useMembers } from "@/features/households/hooks";
import { errorMessage, type CreditCard } from "@/lib/api/client";
import { formatBRL } from "@/lib/money";

import { CardForm } from "./card-form";
import { useCards } from "./hooks";
import { cardName } from "./labels";
import { CommitmentTag, EstimatedTag } from "./tags";

export function CardsView({ householdId }: { householdId: string }) {
  const [showArchived, setShowArchived] = useState(false);
  const [creating, setCreating] = useState(false);
  const cards = useCards(householdId, showArchived);
  const household = useHousehold(householdId);
  const members = useMembers(householdId);

  if (cards.isPending) return <p role="status">Carregando…</p>;
  if (cards.isError) return <FormError message={errorMessage(cards.error)} />;
  const items = cards.data.items;

  return (
    <>
      <div className="flex flex-col gap-1">
        <Link
          href={`/household?id=${householdId}`}
          className="text-sm text-muted-foreground underline"
        >
          Voltar para a família
        </Link>
        <h1 className="text-2xl font-semibold tracking-tight">Cartões</h1>
        <p className="text-sm text-muted-foreground">
          Compras no cartão entram no consumo na data da compra. As faturas são
          compromissos; o dinheiro só sai da conta quando a fatura é paga.
        </p>
      </div>

      {creating ? (
        <Card>
          <CardHeader>
            <CardTitle>Novo cartão</CardTitle>
          </CardHeader>
          <CardContent>
            {household.isPending || members.isPending ? (
              <p role="status">Carregando…</p>
            ) : household.isError || members.isError ? (
              <FormError
                message={errorMessage(household.error ?? members.error)}
              />
            ) : (
              <CardForm
                householdId={householdId}
                members={members.data}
                defaultHolderId={household.data.memberId}
                onDone={() => setCreating(false)}
              />
            )}
          </CardContent>
        </Card>
      ) : (
        <Button className="self-start" onClick={() => setCreating(true)}>
          Novo cartão
        </Button>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Seus cartões</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {items.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Nenhum cartão ainda. Cadastre um para lançar compras parceladas e
              acompanhar as faturas.
            </p>
          ) : (
            <ul className="flex flex-col divide-y">
              {items.map((c) => (
                <CardRow key={c.id} card={c} householdId={householdId} />
              ))}
            </ul>
          )}
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={showArchived}
              onChange={(e) => setShowArchived(e.target.checked)}
            />
            Mostrar arquivados
          </label>
        </CardContent>
      </Card>
    </>
  );
}

function CardRow({
  card,
  householdId,
}: {
  card: CreditCard;
  householdId: string;
}) {
  const negative = card.estimatedAvailableLimitCents < 0;
  return (
    <li>
      <Link
        href={`/card?household=${householdId}&id=${card.id}`}
        className="flex items-center justify-between gap-3 py-3 hover:bg-muted/50"
      >
        <div className="flex min-w-0 flex-col">
          <span
            className={`truncate font-medium ${card.isActive ? "" : "text-muted-foreground line-through"}`}
          >
            {cardName(card)}
          </span>
          <span className="truncate text-sm text-muted-foreground">
            Fecha dia {card.closingDay} · vence dia {card.dueDay}
            {!card.isActive && " · arquivado"}
          </span>
          <span className="text-sm text-muted-foreground">
            Em aberto: {formatBRL(card.outstandingCents)}
            <CommitmentTag />
          </span>
        </div>
        <div className="flex shrink-0 flex-col items-end text-sm">
          <span className="text-muted-foreground">Disponível</span>
          <span
            className={`tabular-nums ${negative ? "text-destructive" : ""}`}
          >
            {formatBRL(card.estimatedAvailableLimitCents)}
          </span>
          <EstimatedTag />
        </div>
      </Link>
    </li>
  );
}
