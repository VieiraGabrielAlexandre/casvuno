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
import { useAccounts } from "@/features/accounts/hooks";
import { useHousehold, useMembers } from "@/features/households/hooks";
import { namesById } from "@/features/ledger/labels";
import { errorMessage, type Invoice } from "@/lib/api/client";
import { formatDate, todayIn } from "@/lib/dates";
import { formatBRL } from "@/lib/money";
import { useOnline } from "@/lib/use-online";

import { AdjustInvoiceForm } from "./adjust-invoice-form";
import { useCard, useCloseInvoice, useInvoice } from "./hooks";
import { cardName, invoiceSituation, referenceMonthLabel } from "./labels";
import { PayInvoiceForm } from "./pay-invoice-form";
import { CommitmentTag } from "./tags";

type Mode = "view" | "pay" | "adjust" | "close";

/** One invoice: balance, installments, payments and adjustments; close, pay and adjust. */
export function InvoiceDetail({
  householdId,
  invoiceId,
}: {
  householdId: string;
  invoiceId: string;
}) {
  const detail = useInvoice(householdId, invoiceId);
  const household = useHousehold(householdId);
  const accounts = useAccounts(householdId, true);
  const members = useMembers(householdId);
  const card = useCard(householdId, detail.data?.invoice.creditCardId ?? "");
  const close = useCloseInvoice(householdId, invoiceId);
  const online = useOnline();
  const [mode, setMode] = useState<Mode>("view");
  const [done, setDone] = useState<string | null>(null);

  if (detail.isPending || household.isPending) {
    return <p role="status">Carregando…</p>;
  }
  if (detail.isError || household.isError) {
    return (
      <>
        <Link
          href={`/cards?household=${householdId}`}
          className="text-sm text-muted-foreground underline"
        >
          Voltar para os cartões
        </Link>
        <FormError message={errorMessage(detail.error ?? household.error)} />
      </>
    );
  }
  const { invoice: inv, installments, payments, adjustments } = detail.data;
  const today = todayIn(household.data.timezone);
  const accountName = namesById(accounts.data?.items ?? []);
  const memberName = namesById(members.data ?? []);
  const activeAccounts = (accounts.data?.items ?? []).filter((a) => a.isActive);
  const closable = inv.status === "OPEN" && inv.closingOn <= today;
  const finish = (message: string) => {
    setDone(message);
    setMode("view");
  };

  return (
    <>
      <div className="flex flex-col gap-1">
        <Link
          href={`/card?household=${householdId}&id=${inv.creditCardId}`}
          className="text-sm text-muted-foreground underline"
        >
          Voltar para o cartão
        </Link>
        <h1 className="text-2xl font-semibold tracking-tight first-letter:uppercase">
          Fatura de {referenceMonthLabel(inv.referenceMonth)}
        </h1>
        <p className="text-sm text-muted-foreground">
          {card.data ? `${cardName(card.data)} · ` : ""}compras de{" "}
          {formatDate(inv.cycleStartOn)} até a véspera de{" "}
          {formatDate(inv.closingOn)} · vence {formatDate(inv.dueOn)}
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardDescription>
            Em aberto <CommitmentTag />
          </CardDescription>
          <CardTitle className="text-3xl tabular-nums">
            {formatBRL(inv.outstandingCents)}
          </CardTitle>
          <CardDescription>{invoiceSituation(inv)}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3 text-sm">
          <Totals invoice={inv} />
          {inv.closingPending && (
            <p className="font-medium">
              O fechamento chegou: confira com o extrato do banco e feche a
              fatura. Depois de fechada, nenhuma parcela entra nem sai;
              correções viram ajustes.
            </p>
          )}
          {inv.status === "CLOSED" && inv.closedAt && (
            <p className="text-muted-foreground">
              Fechada em {formatDate(inv.closedAt.slice(0, 10))}
              {inv.closedByMemberId &&
                ` por ${memberName.get(inv.closedByMemberId) ?? "um membro"}`}
              , com {formatBRL(inv.closedBaseAmountCents ?? 0)} em parcelas.
            </p>
          )}
          {done && (
            <p role="status" className="font-medium">
              {done}
            </p>
          )}

          {mode === "view" && (
            <div className="flex flex-wrap gap-2">
              {inv.outstandingCents > 0 && (
                <Button
                  size="sm"
                  onClick={() => {
                    setDone(null);
                    setMode("pay");
                  }}
                >
                  Pagar
                </Button>
              )}
              {inv.status === "OPEN" && (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={!closable}
                  onClick={() => {
                    setDone(null);
                    setMode("close");
                  }}
                >
                  Fechar fatura
                </Button>
              )}
              {card.data?.isActive !== false && (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    setDone(null);
                    setMode("adjust");
                  }}
                >
                  Ajustar
                </Button>
              )}
            </div>
          )}
          {mode === "view" && inv.status === "OPEN" && !closable && (
            <p className="text-muted-foreground">
              Pode ser fechada a partir de {formatDate(inv.closingOn)}. Pagar
              antes (antecipar) já é possível.
            </p>
          )}

          {mode === "pay" &&
            (activeAccounts.length === 0 ? (
              <p className="text-muted-foreground">
                Cadastre uma conta para pagar a fatura.
              </p>
            ) : (
              <PayInvoiceForm
                householdId={householdId}
                invoice={inv}
                accounts={activeAccounts}
                today={today}
                onDone={finish}
                onCancel={() => setMode("view")}
              />
            ))}
          {mode === "adjust" && (
            <AdjustInvoiceForm
              householdId={householdId}
              invoice={inv}
              onDone={finish}
              onCancel={() => setMode("view")}
            />
          )}
          {mode === "close" && (
            <div
              role="alertdialog"
              aria-label="Confirmar fechamento"
              className="flex flex-col gap-2 rounded-lg border p-3"
            >
              <p>
                Fechar a fatura com {formatBRL(inv.installmentsCents)} em
                parcelas? O valor fica congelado; compras atrasadas deste ciclo
                vão para a próxima fatura.
              </p>
              <FormError
                message={close.isError ? errorMessage(close.error) : undefined}
              />
              <div className="flex gap-2">
                <Button
                  size="sm"
                  disabled={close.isPending || !online}
                  onClick={() =>
                    close.mutate(
                      {},
                      { onSuccess: () => finish("Fatura fechada.") },
                    )
                  }
                >
                  {close.isPending ? "Fechando…" : "Confirmar fechamento"}
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => setMode("view")}
                >
                  Voltar
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Parcelas</CardTitle>
          <CardDescription>
            Cada compra entrou no consumo uma vez, na data dela. Aqui aparecem
            só as parcelas desta fatura.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {installments.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Nenhuma parcela nesta fatura.
            </p>
          ) : (
            <ul className="flex flex-col divide-y text-sm">
              {installments.map((i) => {
                const cancelled = i.status === "CANCELLED";
                return (
                  <li
                    key={i.id}
                    className="flex items-center justify-between gap-3 py-2"
                  >
                    <span className="flex min-w-0 flex-col">
                      <span
                        className={`truncate ${cancelled ? "line-through" : ""}`}
                      >
                        {i.description}
                      </span>
                      <span className="truncate text-muted-foreground">
                        {formatDate(i.occurredOn)}
                        {i.total > 1 &&
                          ` · parcela ${i.number}/${i.total} de ${formatBRL(i.purchaseAmountCents)}`}
                        {i.merchant ? ` · ${i.merchant}` : ""}
                        {cancelled ? " · cancelada" : ""}
                      </span>
                    </span>
                    <span
                      className={`shrink-0 tabular-nums ${cancelled ? "text-muted-foreground line-through" : ""}`}
                    >
                      {formatBRL(i.amountCents)}
                    </span>
                  </li>
                );
              })}
            </ul>
          )}
        </CardContent>
      </Card>

      {(payments.length > 0 || adjustments.length > 0) && (
        <Card>
          <CardHeader>
            <CardTitle>Pagamentos e ajustes</CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="flex flex-col divide-y text-sm">
              {payments.map((p) => (
                <li
                  key={p.id}
                  className="flex items-center justify-between gap-3 py-2"
                >
                  <span className="flex flex-col">
                    <span>Pagamento · {formatDate(p.paidOn)}</span>
                    <span className="text-muted-foreground">
                      {accountName.get(p.accountId) ?? "Conta"} (caixa)
                    </span>
                  </span>
                  <span className="shrink-0 tabular-nums">
                    −{formatBRL(p.amountCents)}
                  </span>
                </li>
              ))}
              {adjustments.map((a) => (
                <li
                  key={a.id}
                  className="flex items-center justify-between gap-3 py-2"
                >
                  <span className="flex flex-col">
                    <span>Ajuste · {a.reason}</span>
                    <span className="text-muted-foreground">
                      {formatDate(a.createdAt.slice(0, 10))}
                    </span>
                  </span>
                  <span className="shrink-0 tabular-nums">
                    {a.amountCents > 0 ? "+" : "−"}
                    {formatBRL(Math.abs(a.amountCents))}
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}
    </>
  );
}

function Totals({ invoice: inv }: { invoice: Invoice }) {
  return (
    <dl className="grid grid-cols-2 gap-x-4 gap-y-1">
      <dt className="text-muted-foreground">Parcelas</dt>
      <dd className="text-right tabular-nums">
        {formatBRL(inv.installmentsCents)}
      </dd>
      {inv.adjustmentsCents !== 0 && (
        <>
          <dt className="text-muted-foreground">Ajustes</dt>
          <dd className="text-right tabular-nums">
            {inv.adjustmentsCents > 0 ? "+" : "−"}
            {formatBRL(Math.abs(inv.adjustmentsCents))}
          </dd>
        </>
      )}
      <dt className="text-muted-foreground">Total</dt>
      <dd className="text-right tabular-nums">{formatBRL(inv.totalCents)}</dd>
      <dt className="text-muted-foreground">Pago</dt>
      <dd className="text-right tabular-nums">{formatBRL(inv.paidCents)}</dd>
    </dl>
  );
}
