"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Controller, useForm } from "react-hook-form";
import { z } from "zod";

import { FormError, FormField } from "@/components/form-field";
import { MoneyInput } from "@/components/money-input";
import { SelectField } from "@/components/select-field";
import { Button } from "@/components/ui/button";
import type { CreditCard, Member, UpdateCardRequest } from "@/lib/api/client";
import { applyApiError } from "@/lib/form-errors";
import { MAX_ABS_CENTS } from "@/lib/money";

import { useCreateCard, useUpdateCard } from "./hooks";

const day = z
  .number({ error: "Informe um dia de 1 a 31." })
  .int("Informe um dia de 1 a 31.")
  .min(1, "Informe um dia de 1 a 31.")
  .max(31, "Informe um dia de 1 a 31.");

const schema = z.object({
  name: z
    .string()
    .trim()
    .min(1, "Informe o nome.")
    .max(60, "Máximo de 60 caracteres."),
  holderMemberId: z.string().min(1, "Escolha o titular."),
  lastFour: z
    .string()
    .trim()
    .regex(
      /^(\d{4})?$/,
      "Informe só os 4 últimos dígitos (ou deixe em branco).",
    ),
  limitCents: z
    .number()
    .int()
    .min(1, "Informe o limite.")
    .max(MAX_ABS_CENTS, "Valor fora do limite permitido."),
  closingDay: day,
  dueDay: day,
});
type Values = z.infer<typeof schema>;
const fields = [
  "name",
  "holderMemberId",
  "lastFour",
  "limitCents",
  "closingDay",
  "dueDay",
] as const;

type Props = {
  householdId: string;
  /** Members (holder candidates): active ones, plus the current holder. */
  members: Member[];
  defaultHolderId: string;
  /** Present when editing. */
  card?: CreditCard;
  onDone: () => void;
};

/**
 * Creates or edits a credit card. Only the last four digits are ever asked for: never the
 * full number or the CVV (I-49). Editing sends only the fields that changed.
 */
export function CardForm({
  householdId,
  members,
  defaultHolderId,
  card,
  onDone,
}: Props) {
  const create = useCreateCard(householdId);
  const update = useUpdateCard(householdId, card?.id ?? "");
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: card
      ? {
          name: card.name,
          holderMemberId: card.holderMemberId,
          lastFour: card.lastFour ?? "",
          limitCents: card.limitCents,
          closingDay: card.closingDay,
          dueDay: card.dueDay,
        }
      : {
          name: "",
          holderMemberId: defaultHolderId,
          lastFour: "",
          limitCents: 0,
          closingDay: 1,
          dueDay: 10,
        },
  });
  const { errors, isSubmitting, dirtyFields } = form.formState;
  const holders = members.filter(
    (m) => m.status === "ACTIVE" || m.id === card?.holderMemberId,
  );

  const onSubmit = form.handleSubmit(async (v) => {
    try {
      if (!card) {
        await create.mutateAsync({
          name: v.name,
          holderMemberId: v.holderMemberId,
          limitCents: v.limitCents,
          closingDay: v.closingDay,
          dueDay: v.dueDay,
          ...(v.lastFour ? { lastFour: v.lastFour } : {}),
        });
      } else {
        const body: UpdateCardRequest = {};
        if (dirtyFields.name) body.name = v.name;
        if (dirtyFields.holderMemberId) body.holderMemberId = v.holderMemberId;
        if (dirtyFields.lastFour) body.lastFour = v.lastFour; // "" clears
        if (dirtyFields.limitCents) body.limitCents = v.limitCents;
        if (dirtyFields.closingDay) body.closingDay = v.closingDay;
        if (dirtyFields.dueDay) body.dueDay = v.dueDay;
        if (Object.keys(body).length > 0) await update.mutateAsync(body);
      }
      onDone();
    } catch (err) {
      applyApiError(err, form.setError, fields);
    }
  });

  return (
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
      <FormField
        label="Nome"
        placeholder="Ex.: Nubank da Ana"
        error={errors.name?.message}
        {...form.register("name")}
      />
      <SelectField
        label="Titular"
        error={errors.holderMemberId?.message}
        {...form.register("holderMemberId")}
      >
        <option value="">Escolha o titular</option>
        {holders.map((m) => (
          <option key={m.id} value={m.id}>
            {m.name}
          </option>
        ))}
      </SelectField>
      <FormField
        label="4 últimos dígitos (opcional)"
        inputMode="numeric"
        maxLength={4}
        autoComplete="off"
        error={errors.lastFour?.message}
        {...form.register("lastFour")}
      />
      <Controller
        control={form.control}
        name="limitCents"
        render={({ field }) => (
          <MoneyInput
            label="Limite"
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
            name={field.name}
            ref={field.ref}
            error={errors.limitCents?.message}
          />
        )}
      />
      <div className="grid gap-4 sm:grid-cols-2">
        <FormField
          label="Dia do fechamento"
          type="number"
          inputMode="numeric"
          min={1}
          max={31}
          error={errors.closingDay?.message}
          {...form.register("closingDay", { valueAsNumber: true })}
        />
        <FormField
          label="Dia do vencimento"
          type="number"
          inputMode="numeric"
          min={1}
          max={31}
          error={errors.dueDay?.message}
          {...form.register("dueDay", { valueAsNumber: true })}
        />
      </div>
      <p className="text-sm text-muted-foreground">
        Nunca informe o número completo nem o código de segurança. Novos dias de
        fechamento e vencimento valem só para faturas ainda não criadas.
      </p>
      <FormError message={errors.root?.server?.message} />
      <div className="flex gap-2">
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting ? "Salvando…" : card ? "Salvar" : "Cadastrar cartão"}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          Cancelar
        </Button>
      </div>
    </form>
  );
}
