"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Controller, useForm, useWatch } from "react-hook-form";
import { z } from "zod";

import { FormError, FormField } from "@/components/form-field";
import { MoneyInput } from "@/components/money-input";
import { SelectField } from "@/components/select-field";
import { Button } from "@/components/ui/button";
import type { Account, Member, UpdateAccountRequest } from "@/lib/api/client";
import { applyApiError } from "@/lib/form-errors";
import { MAX_ABS_CENTS } from "@/lib/money";

import { useCreateAccount, useUpdateAccount } from "./hooks";
import { accountTypeLabel, ownershipLabel } from "./labels";

const schema = z
  .object({
    name: z
      .string()
      .trim()
      .min(1, "Informe o nome.")
      .max(60, "Máximo de 60 caracteres."),
    type: z.enum(["CHECKING", "SAVINGS", "CASH", "OTHER"]),
    ownership: z.enum(["INDIVIDUAL", "JOINT"]),
    ownerMemberId: z.string(),
    openingBalanceCents: z
      .number()
      .int()
      .min(-MAX_ABS_CENTS, "Valor fora do limite permitido.")
      .max(MAX_ABS_CENTS, "Valor fora do limite permitido."),
    openedOn: z.iso.date("Informe a data."),
  })
  .refine((v) => v.ownership === "JOINT" || v.ownerMemberId !== "", {
    path: ["ownerMemberId"],
    message: "Informe o titular da conta individual.",
  });
type Values = z.infer<typeof schema>;
const fields = [
  "name",
  "type",
  "ownership",
  "ownerMemberId",
  "openingBalanceCents",
  "openedOn",
] as const;

type Props = {
  householdId: string;
  /** Active members (owner candidates); the current owner is kept even if inactive. */
  members: Member[];
  /** Pre-selected owner for a new individual account (usually the current member). */
  defaultOwnerId: string;
  today: string;
  /** Present when editing. */
  account?: Account;
  onDone: () => void;
};

/** Creates or edits an account. Editing sends only the fields that changed. */
export function AccountForm({
  householdId,
  members,
  defaultOwnerId,
  today,
  account,
  onDone,
}: Props) {
  const create = useCreateAccount(householdId);
  const update = useUpdateAccount(householdId, account?.id ?? "");
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: account
      ? {
          name: account.name,
          type: account.type,
          ownership: account.ownership,
          ownerMemberId: account.ownerMemberId ?? defaultOwnerId,
          openingBalanceCents: account.openingBalanceCents,
          openedOn: account.openedOn,
        }
      : {
          name: "",
          type: "CHECKING",
          ownership: "INDIVIDUAL",
          ownerMemberId: defaultOwnerId,
          openingBalanceCents: 0,
          openedOn: today,
        },
  });
  const { errors, isSubmitting, dirtyFields } = form.formState;
  const ownership = useWatch({ control: form.control, name: "ownership" });
  const owners = members.filter(
    (m) => m.status === "ACTIVE" || m.id === account?.ownerMemberId,
  );

  const onSubmit = form.handleSubmit(async (v) => {
    const ownerMemberId =
      v.ownership === "INDIVIDUAL" ? v.ownerMemberId : undefined;
    try {
      if (!account) {
        await create.mutateAsync({ ...v, ownerMemberId });
      } else {
        const body: UpdateAccountRequest = {};
        if (dirtyFields.name) body.name = v.name;
        if (dirtyFields.type) body.type = v.type;
        if (dirtyFields.ownership || dirtyFields.ownerMemberId) {
          body.ownership = v.ownership;
          body.ownerMemberId = ownerMemberId;
        }
        if (dirtyFields.openingBalanceCents) {
          body.openingBalanceCents = v.openingBalanceCents;
        }
        if (dirtyFields.openedOn) body.openedOn = v.openedOn;
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
        label="Tipo"
        error={errors.type?.message}
        {...form.register("type")}
      >
        {Object.entries(accountTypeLabel).map(([value, label]) => (
          <option key={value} value={value}>
            {label}
          </option>
        ))}
      </SelectField>
      <SelectField
        label="Titularidade"
        error={errors.ownership?.message}
        {...form.register("ownership")}
      >
        {Object.entries(ownershipLabel).map(([value, label]) => (
          <option key={value} value={value}>
            {label}
          </option>
        ))}
      </SelectField>
      {ownership === "INDIVIDUAL" && (
        <SelectField
          label="Titular"
          error={errors.ownerMemberId?.message}
          {...form.register("ownerMemberId")}
        >
          <option value="">Escolha o titular</option>
          {owners.map((m) => (
            <option key={m.id} value={m.id}>
              {m.name}
            </option>
          ))}
        </SelectField>
      )}
      <Controller
        control={form.control}
        name="openingBalanceCents"
        render={({ field }) => (
          <MoneyInput
            label="Saldo inicial"
            allowNegative
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
            name={field.name}
            ref={field.ref}
            error={errors.openingBalanceCents?.message}
          />
        )}
      />
      <FormField
        label="Saldo inicial em"
        type="date"
        error={errors.openedOn?.message}
        {...form.register("openedOn")}
      />
      <p className="text-sm text-muted-foreground">
        Use o saldo da conta nessa data. Digite &quot;-&quot; para um saldo
        negativo.
      </p>
      <FormError message={errors.root?.server?.message} />
      <div className="flex gap-2">
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting ? "Salvando…" : account ? "Salvar" : "Criar conta"}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          Cancelar
        </Button>
      </div>
    </form>
  );
}
