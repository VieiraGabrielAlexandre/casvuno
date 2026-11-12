"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import Link from "next/link";
import { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { z } from "zod";

import { FormError, FormField } from "@/components/form-field";
import { MoneyInput } from "@/components/money-input";
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
import {
  useCancelObligation,
  useObligations,
  useUpdateObligation,
  type ObligationView,
} from "@/features/ledger/hooks";
import {
  obligationKindLabel,
  obligationSituation,
} from "@/features/ledger/labels";
import { QuickEntryButton } from "@/features/ledger/quick-entry-button";
import { errorMessage, type Account, type Obligation } from "@/lib/api/client";
import { formatDate, todayIn } from "@/lib/dates";
import { applyApiError } from "@/lib/form-errors";
import { formatBRL, MAX_ABS_CENTS } from "@/lib/money";
import { useOnline } from "@/lib/use-online";

import { PayForm } from "./pay-form";

const views: { value: ObligationView; label: string }[] = [
  { value: "open", label: "Em aberto" },
  { value: "overdue", label: "Vencidas" },
  { value: "all", label: "Todas" },
];

const kinds: { value: Obligation["kind"] | ""; label: string }[] = [
  { value: "", label: "Pagar e receber" },
  { value: "PAYABLE", label: "A pagar" },
  { value: "RECEIVABLE", label: "A receber" },
];

export function ObligationsView({ householdId }: { householdId: string }) {
  const household = useHousehold(householdId);
  const accounts = useAccounts(householdId);
  const [view, setView] = useState<ObligationView>("open");
  const [kind, setKind] = useState<Obligation["kind"] | "">("");
  const [message, setMessage] = useState<string | null>(null);
  const obligations = useObligations(householdId, view, kind || undefined);
  const items = obligations.data?.pages.flatMap((p) => p.items) ?? [];
  const failed = [household, accounts, obligations].find((q) => q.isError);
  const loading = household.isPending || accounts.isPending;

  return (
    <>
      <div className="flex flex-col gap-1">
        <Link
          href={`/household?id=${householdId}`}
          className="text-sm text-muted-foreground underline"
        >
          Voltar
        </Link>
        <h1 className="text-2xl font-semibold tracking-tight">
          Contas a pagar e receber
        </h1>
        <p className="text-sm text-muted-foreground">
          Compromissos: o que vence e ainda não foi pago ou recebido. Uma
          previsão só vira consumo quando é paga.
        </p>
      </div>

      <div className="flex flex-col gap-2">
        <div
          role="group"
          aria-label="Situação"
          className="flex flex-wrap gap-2"
        >
          {views.map((v) => (
            <Button
              key={v.value}
              size="sm"
              variant={view === v.value ? "default" : "outline"}
              aria-pressed={view === v.value}
              onClick={() => setView(v.value)}
            >
              {v.label}
            </Button>
          ))}
        </div>
        <div role="group" aria-label="Tipo" className="flex flex-wrap gap-2">
          {kinds.map((k) => (
            <Button
              key={k.value}
              size="sm"
              variant={kind === k.value ? "secondary" : "ghost"}
              aria-pressed={kind === k.value}
              onClick={() => setKind(k.value)}
            >
              {k.label}
            </Button>
          ))}
        </div>
      </div>

      {message && (
        <p role="status" className="text-sm font-medium">
          {message}
        </p>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Compromissos</CardTitle>
          <CardDescription>
            Por vencimento, mais antigos primeiro.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {failed ? (
            <FormError message={errorMessage(failed.error)} />
          ) : loading || obligations.isPending ? (
            <p role="status">Carregando…</p>
          ) : items.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              {view === "overdue"
                ? "Nada vencido."
                : view === "open"
                  ? "Nenhuma conta em aberto."
                  : "Nenhuma conta ainda."}
            </p>
          ) : (
            <ul className="flex flex-col divide-y">
              {items.map((o) => (
                <ObligationRow
                  key={o.id}
                  householdId={householdId}
                  obligation={o}
                  accounts={accounts.data!.items.filter((a) => a.isActive)}
                  today={todayIn(household.data!.timezone)}
                  onMessage={setMessage}
                />
              ))}
            </ul>
          )}
          {obligations.hasNextPage && (
            <Button
              variant="outline"
              size="sm"
              className="self-start"
              disabled={obligations.isFetchingNextPage}
              onClick={() => obligations.fetchNextPage()}
            >
              {obligations.isFetchingNextPage ? "Carregando…" : "Carregar mais"}
            </Button>
          )}
        </CardContent>
      </Card>
      <QuickEntryButton householdId={householdId} />
    </>
  );
}

function ObligationRow({
  householdId,
  obligation: o,
  accounts,
  today,
  onMessage,
}: {
  householdId: string;
  obligation: Obligation;
  accounts: Account[];
  today: string;
  onMessage: (m: string) => void;
}) {
  const [mode, setMode] = useState<"closed" | "pay" | "edit" | "cancel">(
    "closed",
  );
  const cancel = useCancelObligation(householdId, o.id);
  const online = useOnline();
  const open = o.outstandingCents > 0;
  const planned = o.status === "PLANNED";
  const receivable = o.kind === "RECEIVABLE";

  return (
    <li className="flex flex-col gap-2 py-3">
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <span className="truncate">{o.description}</span>
          <span className="text-sm text-muted-foreground">
            {obligationKindLabel[o.kind]} · vence {formatDate(o.dueOn)} ·{" "}
            <span className={o.isOverdue ? "font-medium text-destructive" : ""}>
              {obligationSituation(o)}
            </span>
          </span>
        </div>
        <div className="flex shrink-0 flex-col items-end tabular-nums">
          <span>{formatBRL(o.amountCents)}</span>
          {o.paidCents > 0 && open && (
            <span className="text-sm text-muted-foreground">
              falta {formatBRL(o.outstandingCents)}
            </span>
          )}
        </div>
      </div>

      {mode === "closed" && (open || planned) && (
        <div className="flex flex-wrap gap-2">
          {open && (
            <Button size="sm" onClick={() => setMode("pay")}>
              {receivable ? "Receber" : "Pagar"}
            </Button>
          )}
          {planned && (
            <>
              <Button
                size="sm"
                variant="outline"
                onClick={() => setMode("edit")}
              >
                Editar previsão
              </Button>
              <Button
                size="sm"
                variant="destructive"
                onClick={() => setMode("cancel")}
              >
                Cancelar previsão
              </Button>
            </>
          )}
        </div>
      )}

      {mode === "pay" && (
        <PayForm
          householdId={householdId}
          obligation={o}
          accounts={accounts}
          today={today}
          onCancel={() => setMode("closed")}
          onDone={(m) => {
            setMode("closed");
            onMessage(m);
          }}
        />
      )}

      {mode === "edit" && (
        <EditForecastForm
          householdId={householdId}
          obligation={o}
          onClose={() => setMode("closed")}
        />
      )}

      {mode === "cancel" && (
        <div
          role="alertdialog"
          aria-label="Confirmar cancelamento"
          className="flex flex-col gap-2 rounded-lg border border-destructive/40 p-3 text-sm"
        >
          <p>
            Cancelar a previsão &ldquo;{o.description}&rdquo;? Ela sai dos
            compromissos. Nenhum dinheiro foi movimentado.
          </p>
          <FormError
            message={cancel.isError ? errorMessage(cancel.error) : undefined}
          />
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="destructive"
              disabled={cancel.isPending || !online}
              onClick={() =>
                cancel.mutate(undefined, {
                  onSuccess: () => {
                    setMode("closed");
                    onMessage(`Previsão "${o.description}" cancelada.`);
                  },
                })
              }
            >
              {cancel.isPending ? "Cancelando…" : "Confirmar cancelamento"}
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => setMode("closed")}
            >
              Manter
            </Button>
          </div>
        </div>
      )}
    </li>
  );
}

const forecastSchema = z.object({
  amountCents: z
    .number()
    .int()
    .min(1, "Informe um valor maior que zero.")
    .max(MAX_ABS_CENTS, "Valor fora do limite permitido."),
  dueOn: z.iso.date("Informe o vencimento."),
  description: z
    .string()
    .trim()
    .min(1, "Informe a descrição.")
    .max(200, "Máximo de 200 caracteres."),
});
type ForecastValues = z.infer<typeof forecastSchema>;

/** A forecast is editable until its first payment (amount, due date, description). */
function EditForecastForm({
  householdId,
  obligation: o,
  onClose,
}: {
  householdId: string;
  obligation: Obligation;
  onClose: () => void;
}) {
  const update = useUpdateObligation(householdId, o.id);
  const form = useForm<ForecastValues>({
    resolver: zodResolver(forecastSchema),
    defaultValues: {
      amountCents: o.amountCents,
      dueOn: o.dueOn,
      description: o.description,
    },
  });
  const { errors, isSubmitting } = form.formState;

  const onSubmit = form.handleSubmit(async (v) => {
    try {
      await update.mutateAsync(v);
      onClose();
    } catch (err) {
      applyApiError(err, form.setError, [
        "amountCents",
        "dueOn",
        "description",
      ] as const);
    }
  });

  return (
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-3">
      <Controller
        control={form.control}
        name="amountCents"
        render={({ field }) => (
          <MoneyInput
            label="Valor"
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
            name={field.name}
            ref={field.ref}
            error={errors.amountCents?.message}
          />
        )}
      />
      <FormField
        label="Vencimento"
        type="date"
        error={errors.dueOn?.message}
        {...form.register("dueOn")}
      />
      <FormField
        label="Descrição"
        maxLength={200}
        error={errors.description?.message}
        {...form.register("description")}
      />
      <FormError message={errors.root?.server?.message} />
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={isSubmitting}>
          {isSubmitting ? "Salvando…" : "Salvar"}
        </Button>
        <Button type="button" size="sm" variant="outline" onClick={onClose}>
          Voltar
        </Button>
      </div>
    </form>
  );
}
