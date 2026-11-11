"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { z } from "zod";

import { FormError, FormField } from "@/components/form-field";
import { MoneyInput } from "@/components/money-input";
import { SelectField } from "@/components/select-field";
import { Button } from "@/components/ui/button";
import type { Account } from "@/lib/api/client";
import { formatDate } from "@/lib/dates";
import { applyApiError } from "@/lib/form-errors";
import { formatBRL, MAX_ABS_CENTS } from "@/lib/money";
import { useOnline } from "@/lib/use-online";

import { useCreateTransfer } from "./hooks";

const schema = z
  .object({
    fromAccountId: z.string().min(1, "Escolha a conta de origem."),
    toAccountId: z.string().min(1, "Escolha a conta de destino."),
    amountCents: z
      .number()
      .int()
      .min(1, "Informe um valor maior que zero.")
      .max(MAX_ABS_CENTS, "Valor fora do limite permitido."),
    postedOn: z.iso.date("Informe a data."),
    description: z.string().trim().max(200, "Máximo de 200 caracteres."),
  })
  .refine((v) => v.fromAccountId !== v.toAccountId, {
    path: ["toAccountId"],
    message: "Escolha contas de origem e destino diferentes.",
  });
type Values = z.infer<typeof schema>;
const fields = [
  "fromAccountId",
  "toAccountId",
  "amountCents",
  "postedOn",
  "description",
] as const;

type Props = {
  householdId: string;
  /** Active accounts of the household. */
  accounts: Account[];
  today: string;
  defaultFromId?: string;
};

/**
 * Transfer between two accounts. Never optimistic: success is shown only after the server
 * confirms, and a retry of the same data reuses the Idempotency-Key (see useCreateTransfer).
 */
export function TransferForm({
  householdId,
  accounts,
  today,
  defaultFromId,
}: Props) {
  const create = useCreateTransfer(householdId);
  const online = useOnline();
  const [done, setDone] = useState<string | null>(null);
  const fromDefault = accounts.some((a) => a.id === defaultFromId)
    ? defaultFromId!
    : "";
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      fromAccountId: fromDefault,
      toAccountId: "",
      amountCents: 0,
      postedOn: today,
      description: "",
    },
  });
  const { errors, isSubmitting } = form.formState;

  const onSubmit = form.handleSubmit(async (v) => {
    setDone(null);
    try {
      const t = await create.mutateAsync({
        fromAccountId: v.fromAccountId,
        toAccountId: v.toAccountId,
        amountCents: v.amountCents,
        postedOn: v.postedOn,
        ...(v.description ? { description: v.description } : {}),
      });
      setDone(
        `Transferência de ${formatBRL(t.amountCents)} de ${t.fromAccountName} para ${t.toAccountName} registrada em ${formatDate(t.postedOn)}.`,
      );
      form.reset({ ...v, amountCents: 0, description: "" });
    } catch (err) {
      applyApiError(err, form.setError, fields);
    }
  });

  const options = accounts.map((a) => (
    <option key={a.id} value={a.id}>
      {a.name} ({formatBRL(a.balanceCents)})
    </option>
  ));

  return (
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
      <SelectField
        label="De"
        error={errors.fromAccountId?.message}
        {...form.register("fromAccountId")}
      >
        <option value="">Escolha a conta de origem</option>
        {options}
      </SelectField>
      <SelectField
        label="Para"
        error={errors.toAccountId?.message}
        {...form.register("toAccountId")}
      >
        <option value="">Escolha a conta de destino</option>
        {options}
      </SelectField>
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
        label="Data"
        type="date"
        error={errors.postedOn?.message}
        {...form.register("postedOn")}
      />
      <FormField
        label="Descrição (opcional)"
        placeholder="Transferência"
        maxLength={200}
        error={errors.description?.message}
        {...form.register("description")}
      />
      <FormError message={errors.root?.server?.message} />
      {!online && (
        <p role="status" className="text-sm text-muted-foreground">
          Sem conexão: a transferência só pode ser enviada quando a conexão
          voltar.
        </p>
      )}
      {done && (
        <p role="status" className="text-sm font-medium">
          {done}
        </p>
      )}
      <Button
        type="submit"
        className="self-start"
        disabled={isSubmitting || !online}
      >
        {isSubmitting ? "Transferindo…" : "Transferir"}
      </Button>
    </form>
  );
}
