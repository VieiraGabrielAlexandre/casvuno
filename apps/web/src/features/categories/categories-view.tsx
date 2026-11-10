"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import Link from "next/link";
import { useId, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { z } from "zod";

import { FormError, FormField } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import {
  errorMessage,
  type Category,
  type CategoryKind,
} from "@/lib/api/client";
import { applyApiError } from "@/lib/form-errors";

import {
  useCategories,
  useCreateCategory,
  useSetCategoryActive,
} from "./hooks";
import { buildTree, type CategoryNode } from "./tree";

const kindLabel: Record<CategoryKind, string> = {
  EXPENSE: "Despesas",
  INCOME: "Receitas",
};

const selectClass =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-base md:text-sm";

export function CategoriesView({ householdId }: { householdId: string }) {
  const [showArchived, setShowArchived] = useState(false);
  const categories = useCategories(householdId, showArchived);
  const setActive = useSetCategoryActive(householdId);

  if (categories.isPending) return <p role="status">Carregando…</p>;
  if (categories.isError) {
    return <FormError message={errorMessage(categories.error)} />;
  }
  const tree = buildTree(categories.data);
  const toggle = (c: Category) =>
    setActive.mutate({ id: c.id, isActive: !c.isActive });

  return (
    <>
      <div className="flex flex-col gap-1">
        <Link
          href={`/household?id=${householdId}`}
          className="text-sm text-muted-foreground underline"
        >
          Voltar para a família
        </Link>
        <h1 className="text-2xl font-semibold tracking-tight">Categorias</h1>
        <p className="text-sm text-muted-foreground">
          Categorias arquivadas somem dos novos lançamentos, mas o histórico é
          preservado.
        </p>
      </div>

      <NewCategoryForm householdId={householdId} tree={tree} />

      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={showArchived}
          onChange={(e) => setShowArchived(e.target.checked)}
        />
        Mostrar arquivadas
      </label>
      {setActive.isError && (
        <FormError message={errorMessage(setActive.error)} />
      )}

      {(["EXPENSE", "INCOME"] as const).map((kind) => (
        <Card key={kind}>
          <CardHeader>
            <CardTitle>{kindLabel[kind]}</CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="flex flex-col divide-y">
              {tree[kind].map((node) => (
                <CategoryItem
                  key={node.id}
                  node={node}
                  busy={setActive.isPending}
                  onToggle={toggle}
                />
              ))}
            </ul>
          </CardContent>
        </Card>
      ))}
    </>
  );
}

function CategoryItem({
  node,
  busy,
  onToggle,
}: {
  node: CategoryNode;
  busy: boolean;
  onToggle: (c: Category) => void;
}) {
  return (
    <li className="py-2">
      <Row category={node} busy={busy} onToggle={onToggle} />
      {node.children.length > 0 && (
        <ul className="mt-1 flex flex-col border-l pl-4">
          {node.children.map((c) => (
            <li key={c.id} className="py-1">
              <Row category={c} busy={busy} onToggle={onToggle} />
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}

function Row({
  category,
  busy,
  onToggle,
}: {
  category: Category;
  busy: boolean;
  onToggle: (c: Category) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span
        className={
          category.isActive ? "" : "text-muted-foreground line-through"
        }
      >
        {category.name}
      </span>
      <Button
        variant="ghost"
        size="sm"
        disabled={busy}
        onClick={() => onToggle(category)}
      >
        {category.isActive ? "Arquivar" : "Restaurar"}
      </Button>
    </div>
  );
}

const schema = z.object({
  name: z
    .string()
    .trim()
    .min(1, "Informe o nome.")
    .max(60, "Máximo de 60 caracteres."),
  kind: z.enum(["EXPENSE", "INCOME"]),
  parentId: z.string(),
});
type Values = z.infer<typeof schema>;

function NewCategoryForm({
  householdId,
  tree,
}: {
  householdId: string;
  tree: ReturnType<typeof buildTree>;
}) {
  const create = useCreateCategory(householdId);
  const kindId = useId();
  const parentId = useId();
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { name: "", kind: "EXPENSE", parentId: "" },
  });
  const { errors, isSubmitting } = form.formState;
  const kind = useWatch({ control: form.control, name: "kind" });
  const parents = tree[kind].filter((c) => c.isActive && c.parentId === null);

  const onSubmit = form.handleSubmit(async ({ name, kind, parentId }) => {
    try {
      await create.mutateAsync({ name, kind, parentId: parentId || undefined });
      form.reset({ name: "", kind, parentId });
    } catch (err) {
      applyApiError(err, form.setError, ["name", "kind", "parentId"]);
    }
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Nova categoria</CardTitle>
        <CardDescription>
          Deixe &quot;Categoria principal&quot; vazia para criar uma categoria
          principal.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
          <FormField
            label="Nome"
            error={errors.name?.message}
            {...form.register("name")}
          />
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={kindId}>Tipo</Label>
            <select
              id={kindId}
              className={selectClass}
              {...form.register("kind", {
                onChange: () => form.setValue("parentId", ""),
              })}
            >
              <option value="EXPENSE">Despesa</option>
              <option value="INCOME">Receita</option>
            </select>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={parentId}>Categoria principal</Label>
            <select
              id={parentId}
              className={selectClass}
              aria-invalid={errors.parentId ? true : undefined}
              {...form.register("parentId")}
            >
              <option value="">(nenhuma)</option>
              {parents.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
            {errors.parentId && (
              <p className="text-sm text-destructive">
                {errors.parentId.message}
              </p>
            )}
          </div>
          <FormError message={errors.root?.server?.message} />
          <Button type="submit" className="self-start" disabled={isSubmitting}>
            {isSubmitting ? "Salvando…" : "Adicionar"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
