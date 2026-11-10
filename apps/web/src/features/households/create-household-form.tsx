"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { FormError, FormField } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import { applyApiError } from "@/lib/form-errors";

import { useCreateHousehold } from "./hooks";

const schema = z.object({
  name: z
    .string()
    .trim()
    .min(1, "Dê um nome à família.")
    .max(100, "Máximo de 100 caracteres."),
});
type Values = z.infer<typeof schema>;

export function CreateHouseholdForm() {
  const router = useRouter();
  const create = useCreateHousehold();
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { name: "" },
  });
  const { errors, isSubmitting } = form.formState;

  const onSubmit = form.handleSubmit(async (values) => {
    try {
      const h = await create.mutateAsync(values);
      router.replace(`/household?id=${h.id}`);
    } catch (err) {
      applyApiError(err, form.setError, ["name"]);
    }
  });

  return (
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
      <FormField
        label="Nome da família"
        placeholder="Ex.: Casa Silva"
        error={errors.name?.message}
        {...form.register("name")}
      />
      <FormError message={errors.root?.server?.message} />
      <Button type="submit" disabled={isSubmitting}>
        {isSubmitting ? "Criando…" : "Criar família"}
      </Button>
    </form>
  );
}
