"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Controller, useForm, useWatch } from "react-hook-form";
import { z } from "zod";

import { FormError, FormField } from "@/components/form-field";
import { MoneyInput } from "@/components/money-input";
import { SelectField } from "@/components/select-field";
import { Button } from "@/components/ui/button";
import { useCreateTemplate, useUpdateTemplate } from "@/features/ledger/hooks";
import {
  categoryOptions,
  kindLabel,
  methodLabel,
  paymentMethods,
} from "@/features/ledger/labels";
import type {
  Account,
  Category,
  CreateQuickTemplateRequest,
  Member,
  QuickTemplate,
  UpdateQuickTemplateRequest,
} from "@/lib/api/client";
import { applyApiError } from "@/lib/form-errors";
import { MAX_ABS_CENTS } from "@/lib/money";

const schema = z
  .object({
    label: z
      .string()
      .trim()
      .min(1, "Dê um nome ao favorito.")
      .max(60, "Máximo de 60 caracteres."),
    kind: z.enum(["EXPENSE", "INCOME"]),
    description: z
      .string()
      .trim()
      .min(1, "Informe a descrição.")
      .max(200, "Máximo de 200 caracteres."),
    // 0 = no default amount (asked when the favorite is used)
    defaultAmountCents: z
      .number()
      .int()
      .min(0)
      .max(MAX_ABS_CENTS, "Valor fora do limite permitido."),
    categoryId: z.string().min(1, "Escolha a categoria."),
    paymentMethod: z.enum([
      "PIX",
      "DEBIT",
      "CASH",
      "BANK_TRANSFER",
      "BOLETO",
      "OTHER",
    ]),
    accountId: z.string(),
    scope: z.enum(["FAMILY", "PERSONAL"]),
    personMemberId: z.string(),
  })
  .refine((v) => v.scope === "FAMILY" || v.personMemberId !== "", {
    path: ["personMemberId"],
    message: "Escolha a pessoa.",
  });
export type TemplateValues = z.infer<typeof schema>;
const fields = [
  "label",
  "description",
  "defaultAmountCents",
  "categoryId",
  "paymentMethod",
  "accountId",
  "scope",
  "personMemberId",
] as const;

export function toTemplateValues(t?: QuickTemplate): TemplateValues {
  return {
    label: t?.label ?? "",
    kind: t?.kind ?? "EXPENSE",
    description: t?.description ?? "",
    defaultAmountCents: t?.defaultAmountCents ?? 0,
    categoryId: t?.categoryId ?? "",
    paymentMethod: t?.paymentMethod ?? "PIX",
    accountId: t?.accountId ?? "",
    scope: t?.scope ?? "FAMILY",
    personMemberId: t?.personMemberId ?? "",
  };
}

export function toCreateTemplate(
  v: TemplateValues,
): CreateQuickTemplateRequest {
  return {
    label: v.label.trim(),
    kind: v.kind,
    description: v.description.trim(),
    categoryId: v.categoryId,
    scope: v.scope,
    paymentMethod: v.paymentMethod,
    ...(v.defaultAmountCents > 0
      ? { defaultAmountCents: v.defaultAmountCents }
      : {}),
    ...(v.accountId ? { accountId: v.accountId } : {}),
    ...(v.personMemberId ? { personMemberId: v.personMemberId } : {}),
  };
}

/** Changed fields only; "" clears person/account and 0 clears the amount (I-46). */
export function toUpdateTemplate(
  t: QuickTemplate,
  v: TemplateValues,
): UpdateQuickTemplateRequest {
  const cur = toTemplateValues(t);
  const body: UpdateQuickTemplateRequest = {};
  if (v.label.trim() !== cur.label) body.label = v.label.trim();
  if (v.description.trim() !== cur.description) {
    body.description = v.description.trim();
  }
  if (v.defaultAmountCents !== cur.defaultAmountCents) {
    body.defaultAmountCents = v.defaultAmountCents;
  }
  if (v.categoryId !== cur.categoryId) body.categoryId = v.categoryId;
  if (v.paymentMethod !== cur.paymentMethod) {
    body.paymentMethod = v.paymentMethod;
  }
  if (v.accountId !== cur.accountId) body.accountId = v.accountId;
  if (v.scope !== cur.scope) body.scope = v.scope;
  if (v.personMemberId !== cur.personMemberId) {
    body.personMemberId = v.personMemberId;
  }
  return body;
}

export function TemplateForm({
  householdId,
  template,
  accounts,
  categories,
  members,
  onDone,
  onCancel,
}: {
  householdId: string;
  /** Present when editing. */
  template?: QuickTemplate;
  accounts: Account[];
  categories: Category[];
  members: Member[];
  onDone: (t: QuickTemplate) => void;
  onCancel?: () => void;
}) {
  const create = useCreateTemplate(householdId);
  const update = useUpdateTemplate(householdId);
  const form = useForm<TemplateValues>({
    resolver: zodResolver(schema),
    defaultValues: toTemplateValues(template),
  });
  const { errors, isSubmitting } = form.formState;
  const kind = useWatch({ control: form.control, name: "kind" });
  const scope = useWatch({ control: form.control, name: "scope" });
  const categoryId = useWatch({ control: form.control, name: "categoryId" });

  const onSubmit = form.handleSubmit(async (v) => {
    try {
      if (template) {
        const body = toUpdateTemplate(template, v);
        onDone(
          Object.keys(body).length === 0
            ? template
            : await update.mutateAsync({ id: template.id, body }),
        );
      } else {
        onDone(await create.mutateAsync(toCreateTemplate(v)));
        form.reset(toTemplateValues());
      }
    } catch (err) {
      applyApiError(err, form.setError, fields);
    }
  });

  return (
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-3">
      {!template && (
        <div
          role="group"
          aria-label="Tipo de lançamento"
          className="grid grid-cols-2 gap-2"
        >
          {(["EXPENSE", "INCOME"] as const).map((k) => (
            <Button
              key={k}
              type="button"
              size="sm"
              variant={kind === k ? "default" : "outline"}
              aria-pressed={kind === k}
              onClick={() => {
                form.setValue("kind", k);
                form.setValue("categoryId", "");
              }}
            >
              {kindLabel[k]}
            </Button>
          ))}
        </div>
      )}
      <FormField
        label="Nome do favorito"
        placeholder="Café, Almoço, Uber…"
        maxLength={60}
        error={errors.label?.message}
        {...form.register("label")}
      />
      <FormField
        label="Descrição do lançamento"
        maxLength={200}
        error={errors.description?.message}
        {...form.register("description")}
      />
      <Controller
        control={form.control}
        name="defaultAmountCents"
        render={({ field }) => (
          <MoneyInput
            label="Valor padrão (0 = perguntar na hora)"
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
            name={field.name}
            ref={field.ref}
            error={errors.defaultAmountCents?.message}
          />
        )}
      />
      <SelectField
        label="Categoria"
        error={errors.categoryId?.message}
        {...form.register("categoryId")}
      >
        <option value="">Escolha a categoria</option>
        {categoryOptions(categories, kind, categoryId).map((o) => (
          <option key={o.id} value={o.id}>
            {o.label}
          </option>
        ))}
      </SelectField>
      <div className="grid gap-3 sm:grid-cols-2">
        <SelectField
          label="Conta (opcional)"
          error={errors.accountId?.message}
          {...form.register("accountId")}
        >
          <option value="">Escolher na hora</option>
          {accounts.map((a) => (
            <option key={a.id} value={a.id}>
              {a.name}
            </option>
          ))}
        </SelectField>
        <SelectField
          label="Meio"
          error={errors.paymentMethod?.message}
          {...form.register("paymentMethod")}
        >
          {paymentMethods.map((m) => (
            <option key={m} value={m}>
              {methodLabel[m]}
            </option>
          ))}
        </SelectField>
        <SelectField
          label="De quem é"
          error={errors.scope?.message}
          {...form.register("scope")}
        >
          <option value="FAMILY">Família</option>
          <option value="PERSONAL">Pessoal</option>
        </SelectField>
        <SelectField
          label={scope === "PERSONAL" ? "Pessoa" : "Pessoa (opcional)"}
          error={errors.personMemberId?.message}
          {...form.register("personMemberId")}
        >
          <option value="">Ninguém em particular</option>
          {members.map((m) => (
            <option key={m.id} value={m.id}>
              {m.name}
            </option>
          ))}
        </SelectField>
      </div>
      <p className="text-sm text-muted-foreground">
        Com valor e conta preenchidos, o lançamento fica a um toque de
        confirmar.
      </p>
      <FormError message={errors.root?.server?.message} />
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={isSubmitting}>
          {isSubmitting ? "Salvando…" : template ? "Salvar" : "Criar favorito"}
        </Button>
        {onCancel && (
          <Button type="button" size="sm" variant="outline" onClick={onCancel}>
            Voltar
          </Button>
        )}
      </div>
    </form>
  );
}
