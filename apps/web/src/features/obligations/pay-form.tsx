"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Controller, useForm } from "react-hook-form";
import { z } from "zod";

import { FormError, FormField } from "@/components/form-field";
import { MoneyInput } from "@/components/money-input";
import { SelectField } from "@/components/select-field";
import { Button } from "@/components/ui/button";
import { usePayObligation } from "@/features/ledger/hooks";
import type { Account, Obligation } from "@/lib/api/client";
import { applyApiError } from "@/lib/form-errors";
import { formatBRL } from "@/lib/money";
import { useOnline } from "@/lib/use-online";

const fields = ["amountCents", "paidOn", "accountId"] as const;

/**
 * Pays (or receives) all or part of an obligation. The amount starts at the outstanding
 * balance and cannot exceed it; the first payment of a forecast turns it into a real entry
 * (consumption) on the server. Idempotency-Key per submission (usePayObligation).
 */
export function PayForm({
  householdId,
  obligation: o,
  accounts,
  today,
  onDone,
  onCancel,
}: {
  householdId: string;
  obligation: Obligation;
  /** Active accounts. */
  accounts: Account[];
  today: string;
  onDone: (message: string) => void;
  onCancel: () => void;
}) {
  const pay = usePayObligation(householdId, o.id);
  const online = useOnline();
  const receivable = o.kind === "RECEIVABLE";
  const schema = z.object({
    amountCents: z
      .number()
      .int()
      .min(1, "Informe um valor maior que zero.")
      .max(
        o.outstandingCents,
        `O valor máximo é ${formatBRL(o.outstandingCents)}.`,
      ),
    paidOn: z.iso.date("Informe a data."),
    accountId: z.string().min(1, "Escolha a conta."),
  });
  type Values = z.infer<typeof schema>;
  const defaultAccount =
    o.accountId && accounts.some((a) => a.id === o.accountId)
      ? o.accountId
      : accounts.length === 1
        ? accounts[0].id
        : "";
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      amountCents: o.outstandingCents,
      paidOn: today,
      accountId: defaultAccount,
    },
  });
  const { errors, isSubmitting } = form.formState;

  const onSubmit = form.handleSubmit(async (v) => {
    try {
      const d = await pay.mutateAsync(v);
      const left = d.obligation.outstandingCents;
      onDone(
        left === 0
          ? `${o.description}: ${receivable ? "recebida" : "paga"} por completo.`
          : `${o.description}: ${formatBRL(v.amountCents)} ${receivable ? "recebidos" : "pagos"}, faltam ${formatBRL(left)}.`,
      );
    } catch (err) {
      applyApiError(err, form.setError, fields);
    }
  });

  return (
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-3">
      <Controller
        control={form.control}
        name="amountCents"
        render={({ field }) => (
          <MoneyInput
            label={receivable ? "Valor recebido" : "Valor pago"}
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
            name={field.name}
            ref={field.ref}
            error={errors.amountCents?.message}
          />
        )}
      />
      <div className="grid gap-3 sm:grid-cols-2">
        <FormField
          label="Data"
          type="date"
          error={errors.paidOn?.message}
          {...form.register("paidOn")}
        />
        <SelectField
          label="Conta"
          error={errors.accountId?.message}
          {...form.register("accountId")}
        >
          <option value="">Escolha a conta</option>
          {accounts.map((a) => (
            <option key={a.id} value={a.id}>
              {a.name}
            </option>
          ))}
        </SelectField>
      </div>
      <FormError message={errors.root?.server?.message} />
      {!online && (
        <p role="status" className="text-sm text-muted-foreground">
          Sem conexão: o pagamento só pode ser enviado quando a conexão voltar.
        </p>
      )}
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={isSubmitting || !online}>
          {isSubmitting
            ? "Salvando…"
            : receivable
              ? "Registrar recebimento"
              : "Registrar pagamento"}
        </Button>
        <Button type="button" size="sm" variant="outline" onClick={onCancel}>
          Voltar
        </Button>
      </div>
    </form>
  );
}
