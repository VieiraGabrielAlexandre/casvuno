"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Controller, useForm } from "react-hook-form";
import { z } from "zod";

import { FormError, FormField } from "@/components/form-field";
import { MoneyInput } from "@/components/money-input";
import { Button } from "@/components/ui/button";
import type { Invoice } from "@/lib/api/client";
import { applyApiError } from "@/lib/form-errors";
import { formatBRL, MAX_ABS_CENTS } from "@/lib/money";
import { useOnline } from "@/lib/use-online";

import { useAdjustInvoice } from "./hooks";

const fields = ["amountCents", "reason"] as const;

/**
 * Corrects the invoice balance with a sign and a reason (RN-012): positive for a fee,
 * interest or a difference on the statement; negative for a refund or credit. Purchases and
 * installments never change. The balance cannot go below what was already paid.
 */
export function AdjustInvoiceForm({
  householdId,
  invoice,
  onDone,
  onCancel,
}: {
  householdId: string;
  invoice: Invoice;
  onDone: (message: string) => void;
  onCancel: () => void;
}) {
  const adjust = useAdjustInvoice(householdId, invoice.id);
  const online = useOnline();
  const minimum = invoice.paidCents - invoice.totalCents;
  const schema = z.object({
    amountCents: z
      .number()
      .int()
      .refine((v) => v !== 0, "Informe um valor diferente de zero.")
      .refine(
        (v) => Math.abs(v) <= MAX_ABS_CENTS,
        "Valor fora do limite permitido.",
      )
      .refine(
        (v) => v >= minimum,
        `O crédito máximo é ${formatBRL(-minimum)}: a fatura não pode valer menos que o já pago.`,
      ),
    reason: z
      .string()
      .trim()
      .min(1, "Informe o motivo.")
      .max(200, "Máximo de 200 caracteres."),
  });
  type Values = z.infer<typeof schema>;
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { amountCents: 0, reason: "" },
  });
  const { errors, isSubmitting } = form.formState;

  const onSubmit = form.handleSubmit(async (v) => {
    try {
      const d = await adjust.mutateAsync(v);
      onDone(
        `Ajuste de ${formatBRL(v.amountCents)} registrado. Em aberto: ${formatBRL(d.invoice.outstandingCents)}.`,
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
            label="Valor do ajuste"
            allowNegative
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
            name={field.name}
            ref={field.ref}
            error={errors.amountCents?.message}
          />
        )}
      />
      <p className="text-sm text-muted-foreground">
        Positivo aumenta a fatura (anuidade, juros, diferença do extrato).
        Digite &quot;-&quot; para um crédito (estorno, devolução).
      </p>
      <FormField
        label="Motivo"
        maxLength={200}
        placeholder="Ex.: Anuidade"
        error={errors.reason?.message}
        {...form.register("reason")}
      />
      <FormError message={errors.root?.server?.message} />
      {!online && (
        <p role="status" className="text-sm text-muted-foreground">
          Sem conexão: o ajuste só pode ser enviado quando a conexão voltar.
        </p>
      )}
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={isSubmitting || !online}>
          {isSubmitting ? "Salvando…" : "Registrar ajuste"}
        </Button>
        <Button type="button" size="sm" variant="outline" onClick={onCancel}>
          Voltar
        </Button>
      </div>
    </form>
  );
}
