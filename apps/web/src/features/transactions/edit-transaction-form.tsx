"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useForm, useWatch } from "react-hook-form";
import { z } from "zod";

import { FormError, FormField } from "@/components/form-field";
import { SelectField } from "@/components/select-field";
import { Button } from "@/components/ui/button";
import { useUpdateTransaction } from "@/features/ledger/hooks";
import { categoryOptions } from "@/features/ledger/labels";
import type {
  Category,
  Member,
  Transaction,
  UpdateTransactionRequest,
} from "@/lib/api/client";
import { applyApiError } from "@/lib/form-errors";

const schema = z
  .object({
    description: z
      .string()
      .trim()
      .min(1, "Informe a descrição.")
      .max(200, "Máximo de 200 caracteres."),
    merchant: z.string().trim().max(120, "Máximo de 120 caracteres."),
    notes: z.string().trim().max(1000, "Máximo de 1000 caracteres."),
    categoryId: z.string().min(1, "Escolha a categoria."),
    scope: z.enum(["FAMILY", "PERSONAL"]),
    personMemberId: z.string(),
  })
  .refine((v) => v.scope === "FAMILY" || v.personMemberId !== "", {
    path: ["personMemberId"],
    message: "Escolha a pessoa.",
  });
type Values = z.infer<typeof schema>;
const fields = [
  "description",
  "merchant",
  "notes",
  "categoryId",
  "scope",
  "personMemberId",
] as const;

/** Only the fields that changed ("" clears merchant, notes and person; I-45). */
export function changedFields(
  t: Transaction,
  v: Values,
): UpdateTransactionRequest {
  const body: UpdateTransactionRequest = {};
  if (v.description !== t.description) body.description = v.description;
  if (v.merchant !== (t.merchant ?? "")) body.merchant = v.merchant;
  if (v.notes !== (t.notes ?? "")) body.notes = v.notes;
  if (v.categoryId !== t.categoryId) body.categoryId = v.categoryId;
  if (v.scope !== t.scope) body.scope = v.scope;
  if (v.personMemberId !== (t.personMemberId ?? "")) {
    body.personMemberId = v.personMemberId;
  }
  return body;
}

/**
 * Edits what does not change money (I-18): description, merchant, notes, category, scope
 * and person. Amount, date, method and account are changed with "Corrigir".
 */
export function EditTransactionForm({
  householdId,
  transaction: t,
  categories,
  members,
  onClose,
}: {
  householdId: string;
  transaction: Transaction;
  categories: Category[];
  members: Member[];
  onClose: () => void;
}) {
  const update = useUpdateTransaction(householdId);
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      description: t.description,
      merchant: t.merchant ?? "",
      notes: t.notes ?? "",
      categoryId: t.categoryId,
      scope: t.scope,
      personMemberId: t.personMemberId ?? "",
    },
  });
  const { errors, isSubmitting } = form.formState;
  const scope = useWatch({ control: form.control, name: "scope" });

  const onSubmit = form.handleSubmit(async (v) => {
    const body = changedFields(t, v);
    if (Object.keys(body).length === 0) {
      onClose();
      return;
    }
    try {
      await update.mutateAsync({ id: t.id, body });
      onClose();
    } catch (err) {
      applyApiError(err, form.setError, fields);
    }
  });

  return (
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-3">
      <FormField
        label="Descrição"
        maxLength={200}
        error={errors.description?.message}
        {...form.register("description")}
      />
      <SelectField
        label="Categoria"
        error={errors.categoryId?.message}
        {...form.register("categoryId")}
      >
        {categoryOptions(categories, t.kind, t.categoryId).map((o) => (
          <option key={o.id} value={o.id}>
            {o.label}
          </option>
        ))}
      </SelectField>
      <div className="grid gap-3 sm:grid-cols-2">
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
      <FormField
        label="Estabelecimento (opcional)"
        maxLength={120}
        error={errors.merchant?.message}
        {...form.register("merchant")}
      />
      <FormField
        label="Observações (opcional)"
        maxLength={1000}
        error={errors.notes?.message}
        {...form.register("notes")}
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
