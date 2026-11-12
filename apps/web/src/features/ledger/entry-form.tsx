"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useRef, useState } from "react";
import { Controller, useForm, useWatch } from "react-hook-form";
import { z } from "zod";

import { FormError, FormField } from "@/components/form-field";
import { MoneyInput } from "@/components/money-input";
import { SelectField } from "@/components/select-field";
import { Button } from "@/components/ui/button";
import {
  ApiError,
  type Account,
  type Category,
  type Member,
  type QuickTemplate,
  type Transaction,
} from "@/lib/api/client";
import { formatDate } from "@/lib/dates";
import { applyApiError } from "@/lib/form-errors";
import { formatBRL, MAX_ABS_CENTS } from "@/lib/money";
import { useOnline } from "@/lib/use-online";

import {
  emptyEntry,
  fromTemplate,
  fromTransaction,
  isCompleteTemplate,
  toCreateRequest,
  type EntryValues,
} from "./entry";
import { useCreateEntry, useVoidTransaction } from "./hooks";
import {
  categoryOptions,
  kindLabel,
  methodLabel,
  paymentMethods,
} from "./labels";

const isoDate = /^\d{4}-\d{2}-\d{2}$/;

const schema = z
  .object({
    kind: z.enum(["EXPENSE", "INCOME"]),
    amountCents: z
      .number()
      .int()
      .min(1, "Informe um valor maior que zero.")
      .max(MAX_ABS_CENTS, "Valor fora do limite permitido."),
    description: z.string().trim().max(200, "Máximo de 200 caracteres."),
    categoryId: z.string().min(1, "Escolha a categoria."),
    accountId: z.string(),
    method: z.enum([
      "PIX",
      "DEBIT",
      "CASH",
      "BANK_TRANSFER",
      "BOLETO",
      "OTHER",
    ]),
    mode: z.enum(["IMMEDIATE", "PLANNED"]),
    occurredOn: z.iso.date("Informe a data."),
    dueOn: z.string(),
    scope: z.enum(["FAMILY", "PERSONAL"]),
    personMemberId: z.string(),
    merchant: z.string().trim().max(120, "Máximo de 120 caracteres."),
    notes: z.string().trim().max(1000, "Máximo de 1000 caracteres."),
  })
  .superRefine((v, ctx) => {
    if (v.mode === "IMMEDIATE" && !v.accountId) {
      ctx.addIssue({
        code: "custom",
        path: ["accountId"],
        message: "Escolha a conta.",
      });
    }
    if (v.mode === "PLANNED" && !isoDate.test(v.dueOn)) {
      ctx.addIssue({
        code: "custom",
        path: ["dueOn"],
        message: "Informe o vencimento.",
      });
    }
    if (v.scope === "PERSONAL" && !v.personMemberId) {
      ctx.addIssue({
        code: "custom",
        path: ["personMemberId"],
        message: "Escolha a pessoa.",
      });
    }
  });

const fields = [
  "amountCents",
  "description",
  "categoryId",
  "accountId",
  "method",
  "occurredOn",
  "dueOn",
  "scope",
  "personMemberId",
  "merchant",
  "notes",
] as const;

// The API names the nested payment fields "payment.x"; the form keeps them flat.
const apiFieldNames: Record<string, string> = {
  "payment.accountId": "accountId",
  "payment.method": "method",
  "payment.dueOn": "dueOn",
  "payment.mode": "mode",
};

function withFormFieldNames(err: unknown): unknown {
  if (!(err instanceof ApiError)) return err;
  return new ApiError(
    err.status,
    err.code,
    err.message,
    err.details.map((d) => ({
      ...d,
      field: apiFieldNames[d.field] ?? d.field,
    })),
  );
}

type Props = {
  householdId: string;
  /** Active accounts. */
  accounts: Account[];
  /** All categories (archived ones are offered only when already in use). */
  categories: Category[];
  /** Active members (person of a personal entry). */
  members: Member[];
  /** Today in the household timezone. */
  today: string;
  templates: QuickTemplate[];
  /** The caller's last entry ("Repetir último"), if any. */
  last: Transaction | null;
  /** "Corrigir": the entry being replaced. It is voided right before the new one is sent. */
  correcting?: Transaction;
  onDone?: (message: string) => void;
};

/**
 * Quick entry (README §9): amount first, today by default, paid now or planned. A complete
 * favorite fills everything, so an entry takes three interactions: open, favorite,
 * confirm. Never optimistic: success is shown only after the server confirms, and a retry
 * of the same data reuses the Idempotency-Key (useCreateEntry).
 */
export function EntryForm({
  householdId,
  accounts,
  categories,
  members,
  today,
  templates,
  last,
  correcting,
  onDone,
}: Props) {
  const create = useCreateEntry(householdId);
  const voidTx = useVoidTransaction(householdId);
  const online = useOnline();
  const [done, setDone] = useState<string | null>(null);
  const [voided, setVoided] = useState(false);
  const submitRef = useRef<HTMLButtonElement>(null);
  const defaultAccount = accounts.length === 1 ? accounts[0].id : "";
  const form = useForm<EntryValues>({
    resolver: zodResolver(schema),
    defaultValues: correcting
      ? fromTransaction(correcting, correcting.occurredOn)
      : emptyEntry(today, { accountId: defaultAccount }),
  });
  const { errors, isSubmitting } = form.formState;
  const kind = useWatch({ control: form.control, name: "kind" });
  const mode = useWatch({ control: form.control, name: "mode" });
  const scope = useWatch({ control: form.control, name: "scope" });
  const categoryId = useWatch({ control: form.control, name: "categoryId" });
  const options = categoryOptions(categories, kind, categoryId);
  const income = kind === "INCOME";

  const fill = (values: EntryValues, complete: boolean) => {
    setDone(null);
    form.reset(values);
    // A complete favorite only needs the confirmation; otherwise type the amount.
    setTimeout(() => {
      if (complete) submitRef.current?.focus();
      else form.setFocus("amountCents");
    });
  };

  const setKind = (next: EntryValues["kind"]) => {
    if (next === kind) return;
    form.setValue("kind", next);
    form.setValue("categoryId", "");
  };

  const onSubmit = form.handleSubmit(async (v) => {
    setDone(null);
    // Without a description, the entry is named after its category.
    const category = options.find((o) => o.id === v.categoryId);
    const description =
      v.description.trim() || category?.label.replace(/^— /, "") || "";
    let oldVoided = voided;
    try {
      if (correcting && !oldVoided) {
        await voidTx.mutateAsync(correcting.id);
        oldVoided = true;
        setVoided(true);
      }
      const e = await create.mutateAsync(
        toCreateRequest({ ...v, description }),
      );
      const what = kindLabel[v.kind];
      const message = correcting
        ? `Lançamento corrigido: ${what.toLowerCase()} de ${formatBRL(v.amountCents)} em ${formatDate(v.occurredOn)}.`
        : e.transaction
          ? `${what} de ${formatBRL(e.transaction.amountCents)} lançada em ${formatDate(e.transaction.occurredOn)}.`
          : `Previsão de ${formatBRL(e.obligation.amountCents)} lançada, vence em ${formatDate(e.obligation.dueOn)}.`;
      setDone(message);
      onDone?.(message);
      if (!correcting) {
        form.reset(
          emptyEntry(today, {
            kind: v.kind,
            accountId: v.accountId,
            method: v.method,
          }),
        );
      }
    } catch (err) {
      applyApiError(withFormFieldNames(err), form.setError, fields);
      if (correcting && oldVoided) {
        form.setError("root.server", {
          message: `${err instanceof ApiError ? err.message : "Erro inesperado."} O lançamento antigo já foi cancelado: envie de novo para registrar o corrigido.`,
        });
      }
    }
  });

  const accountOptions = accounts.map((a) => (
    <option key={a.id} value={a.id}>
      {a.name}
    </option>
  ));

  return (
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
      <div
        role="group"
        aria-label="Tipo de lançamento"
        className="grid grid-cols-2 gap-2"
      >
        {(["EXPENSE", "INCOME"] as const).map((k) => (
          <Button
            key={k}
            type="button"
            variant={kind === k ? "default" : "outline"}
            aria-pressed={kind === k}
            onClick={() => setKind(k)}
          >
            {kindLabel[k]}
          </Button>
        ))}
      </div>

      {!correcting && (templates.length > 0 || last) && (
        <div className="flex flex-col gap-1.5">
          <span className="text-sm font-medium">Favoritos</span>
          <div className="flex flex-wrap gap-2">
            {templates.map((t) => (
              <Button
                key={t.id}
                type="button"
                variant="secondary"
                size="sm"
                onClick={() =>
                  fill(
                    fromTemplate(t, today, form.getValues()),
                    isCompleteTemplate(t),
                  )
                }
              >
                {t.label}
                {t.defaultAmountCents !== null &&
                  ` · ${formatBRL(t.defaultAmountCents)}`}
              </Button>
            ))}
            {last && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => fill(fromTransaction(last, today), true)}
              >
                Repetir último ({last.description})
              </Button>
            )}
          </div>
        </div>
      )}

      <Controller
        control={form.control}
        name="amountCents"
        render={({ field }) => (
          <MoneyInput
            large
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

      <SelectField
        label="Categoria"
        error={errors.categoryId?.message}
        {...form.register("categoryId")}
      >
        <option value="">Escolha a categoria</option>
        {options.map((o) => (
          <option key={o.id} value={o.id}>
            {o.label}
          </option>
        ))}
      </SelectField>

      <FormField
        label="Descrição (opcional)"
        placeholder="Usa o nome da categoria se ficar em branco"
        maxLength={200}
        error={errors.description?.message}
        {...form.register("description")}
      />

      <fieldset className="flex flex-col gap-1.5">
        <legend className="mb-1.5 text-sm font-medium">Quando</legend>
        <div className="flex flex-wrap gap-4 text-sm">
          <label className="flex items-center gap-2">
            <input
              type="radio"
              value="IMMEDIATE"
              disabled={Boolean(correcting)}
              {...form.register("mode")}
            />
            {income ? "Recebido agora" : "Pago agora"}
          </label>
          <label className="flex items-center gap-2">
            <input
              type="radio"
              value="PLANNED"
              disabled={Boolean(correcting)}
              {...form.register("mode")}
            />
            Previsto (conta a {income ? "receber" : "pagar"})
          </label>
        </div>
      </fieldset>

      <div className="grid gap-4 sm:grid-cols-2">
        <FormField
          label={
            mode === "PLANNED"
              ? "Data da compra/competência"
              : income
                ? "Data do recebimento"
                : "Data do pagamento"
          }
          type="date"
          error={errors.occurredOn?.message}
          {...form.register("occurredOn")}
        />
        {mode === "PLANNED" && (
          <FormField
            label="Vencimento"
            type="date"
            error={errors.dueOn?.message}
            {...form.register("dueOn")}
          />
        )}
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <SelectField
          label={mode === "PLANNED" ? "Conta (opcional)" : "Conta"}
          error={errors.accountId?.message}
          {...form.register("accountId")}
        >
          <option value="">
            {mode === "PLANNED" ? "Escolher ao pagar" : "Escolha a conta"}
          </option>
          {accountOptions}
        </SelectField>
        <SelectField
          label="Meio"
          error={errors.method?.message}
          {...form.register("method")}
        >
          {paymentMethods.map((m) => (
            <option key={m} value={m}>
              {methodLabel[m]}
            </option>
          ))}
        </SelectField>
      </div>

      <details className="rounded-lg border px-3 py-2">
        <summary className="cursor-pointer text-sm font-medium">
          Mais detalhes
        </summary>
        <div className="mt-3 flex flex-col gap-4">
          <SelectField
            label="De quem é (só para filtro)"
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
          {mode === "IMMEDIATE" ? (
            <>
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
            </>
          ) : (
            <p className="text-sm text-muted-foreground">
              Estabelecimento e observações são informados depois do pagamento.
            </p>
          )}
          <p className="text-sm text-muted-foreground">
            O valor conta uma vez para a família, seja quem for que pagou ou
            usou. Não há divisão entre membros.
          </p>
        </div>
      </details>

      <FormError message={errors.root?.server?.message} />
      {!online && (
        <p role="status" className="text-sm text-muted-foreground">
          Sem conexão: o lançamento só pode ser enviado quando a conexão voltar.
        </p>
      )}
      {done && (
        <p role="status" className="text-sm font-medium">
          {done}
        </p>
      )}
      <Button
        ref={submitRef}
        type="submit"
        size="lg"
        // A correction happens once: after it, the old entry is gone and the new one saved.
        disabled={isSubmitting || !online || Boolean(correcting && done)}
      >
        {isSubmitting
          ? "Salvando…"
          : correcting
            ? "Salvar correção"
            : mode === "PLANNED"
              ? "Lançar previsão"
              : "Lançar"}
      </Button>
    </form>
  );
}
