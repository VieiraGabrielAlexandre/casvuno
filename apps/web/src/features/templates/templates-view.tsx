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
import { useCategories } from "@/features/categories/hooks";
import { useMembers } from "@/features/households/hooks";
import { useDeleteTemplate, useTemplates } from "@/features/ledger/hooks";
import { kindLabel, methodLabel, namesById } from "@/features/ledger/labels";
import {
  errorMessage,
  type Account,
  type Category,
  type Member,
  type QuickTemplate,
} from "@/lib/api/client";
import { formatBRL } from "@/lib/money";

import { TemplateForm } from "./template-form";

/** The member's own favorites (I-46: the other members do not see them). */
export function TemplatesView({ householdId }: { householdId: string }) {
  const templates = useTemplates(householdId);
  const accounts = useAccounts(householdId);
  const categories = useCategories(householdId, true);
  const members = useMembers(householdId);
  const [message, setMessage] = useState<string | null>(null);
  const failed = [templates, accounts, categories, members].find(
    (q) => q.isError,
  );
  const ready =
    templates.data && accounts.data && categories.data && members.data;
  const activeAccounts = accounts.data?.items.filter((a) => a.isActive) ?? [];
  const activeMembers =
    members.data?.filter((m) => m.status === "ACTIVE") ?? [];

  return (
    <>
      <div className="flex flex-col gap-1">
        <Link
          href={`/entry?household=${householdId}`}
          className="text-sm text-muted-foreground underline"
        >
          Voltar para o lançamento
        </Link>
        <h1 className="text-2xl font-semibold tracking-tight">Favoritos</h1>
        <p className="text-sm text-muted-foreground">
          Atalhos para os lançamentos frequentes. Cada membro tem os seus.
        </p>
      </div>
      {message && (
        <p role="status" className="text-sm font-medium">
          {message}
        </p>
      )}
      {failed ? (
        <FormError message={errorMessage(failed.error)} />
      ) : !ready ? (
        <p role="status">Carregando…</p>
      ) : (
        <>
          <Card>
            <CardHeader>
              <CardTitle>Meus favoritos</CardTitle>
            </CardHeader>
            <CardContent>
              {templates.data.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  Nenhum favorito ainda.
                </p>
              ) : (
                <ul className="flex flex-col divide-y">
                  {templates.data.map((t) => (
                    <TemplateRow
                      key={t.id}
                      householdId={householdId}
                      template={t}
                      accounts={activeAccounts}
                      allAccounts={accounts.data.items}
                      categories={categories.data}
                      members={activeMembers}
                      onMessage={setMessage}
                    />
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>Novo favorito</CardTitle>
              <CardDescription>Até 50 por pessoa.</CardDescription>
            </CardHeader>
            <CardContent>
              <TemplateForm
                householdId={householdId}
                accounts={activeAccounts}
                categories={categories.data}
                members={activeMembers}
                onDone={(t) => setMessage(`Favorito "${t.label}" criado.`)}
              />
            </CardContent>
          </Card>
        </>
      )}
    </>
  );
}

function TemplateRow({
  householdId,
  template: t,
  accounts,
  allAccounts,
  categories,
  members,
  onMessage,
}: {
  householdId: string;
  template: QuickTemplate;
  accounts: Account[];
  allAccounts: Account[];
  categories: Category[];
  members: Member[];
  onMessage: (m: string) => void;
}) {
  const [mode, setMode] = useState<"view" | "edit" | "delete">("view");
  const remove = useDeleteTemplate(householdId);
  const category = namesById(categories).get(t.categoryId);
  const account = t.accountId
    ? namesById(allAccounts).get(t.accountId)
    : undefined;

  return (
    <li className="flex flex-col gap-2 py-3">
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <span className="font-medium">{t.label}</span>
          <span className="text-sm text-muted-foreground">
            {kindLabel[t.kind]} · {category ?? "Categoria"} ·{" "}
            {methodLabel[t.paymentMethod]}
            {account ? ` · ${account}` : ""}
          </span>
        </div>
        <span className="shrink-0 tabular-nums">
          {t.defaultAmountCents !== null
            ? formatBRL(t.defaultAmountCents)
            : "valor na hora"}
        </span>
      </div>
      {mode === "view" && (
        <div className="flex gap-2">
          <Button size="sm" variant="outline" onClick={() => setMode("edit")}>
            Editar
          </Button>
          <Button
            size="sm"
            variant="destructive"
            onClick={() => setMode("delete")}
          >
            Excluir
          </Button>
        </div>
      )}
      {mode === "edit" && (
        <TemplateForm
          householdId={householdId}
          template={t}
          accounts={accounts}
          categories={categories}
          members={members}
          onCancel={() => setMode("view")}
          onDone={() => setMode("view")}
        />
      )}
      {mode === "delete" && (
        <div
          role="alertdialog"
          aria-label="Confirmar exclusão"
          className="flex flex-col gap-2 rounded-lg border border-destructive/40 p-3 text-sm"
        >
          <p>
            Excluir o favorito &ldquo;{t.label}&rdquo;? Os lançamentos feitos
            com ele não mudam.
          </p>
          <FormError
            message={remove.isError ? errorMessage(remove.error) : undefined}
          />
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="destructive"
              disabled={remove.isPending}
              onClick={() =>
                remove.mutate(t.id, {
                  onSuccess: () => onMessage(`Favorito "${t.label}" excluído.`),
                })
              }
            >
              {remove.isPending ? "Excluindo…" : "Confirmar exclusão"}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setMode("view")}>
              Manter
            </Button>
          </div>
        </div>
      )}
    </li>
  );
}
