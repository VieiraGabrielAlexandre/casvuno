"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { FormError, FormField } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import { applyApiError, safeNext } from "@/lib/form-errors";

import { useRegister } from "./hooks";

// Mirrors the API rules for immediate feedback; the API remains the authority.
export const registerSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, "Informe seu nome.")
    .max(100, "Máximo de 100 caracteres."),
  email: z.email("Informe um e-mail válido.").max(254),
  password: z
    .string()
    .min(10, "A senha deve ter pelo menos 10 caracteres.")
    .max(128, "A senha deve ter no máximo 128 caracteres."),
});
type Values = z.infer<typeof registerSchema>;

/** Sign-up. With ?invite=<token>, the token also authorises sign-up when registration is closed. */
export function RegisterForm() {
  const router = useRouter();
  const params = useSearchParams();
  const inviteToken = params.get("invite") ?? undefined;
  const next = safeNext(params.get("next"));
  const register = useRegister();
  const form = useForm<Values>({
    resolver: zodResolver(registerSchema),
    defaultValues: { name: "", email: "", password: "" },
  });
  const { errors, isSubmitting } = form.formState;

  const onSubmit = form.handleSubmit(async (values) => {
    try {
      await register.mutateAsync({ ...values, inviteToken });
      router.replace(next);
    } catch (err) {
      applyApiError(err, form.setError, ["name", "email", "password"]);
    }
  });

  return (
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
      <h2 className="text-lg font-medium">Criar conta</h2>
      <FormField
        label="Nome"
        autoComplete="name"
        error={errors.name?.message}
        {...form.register("name")}
      />
      <FormField
        label="E-mail"
        type="email"
        autoComplete="email"
        inputMode="email"
        error={errors.email?.message}
        {...form.register("email")}
      />
      <FormField
        label="Senha (mínimo de 10 caracteres)"
        type="password"
        autoComplete="new-password"
        error={errors.password?.message}
        {...form.register("password")}
      />
      <FormError message={errors.root?.server?.message} />
      <Button type="submit" disabled={isSubmitting}>
        {isSubmitting ? "Criando conta…" : "Criar conta"}
      </Button>
      <p className="text-center text-sm text-muted-foreground">
        Já tem conta?{" "}
        <Link
          className="underline"
          href={`/login?next=${encodeURIComponent(next)}`}
        >
          Entrar
        </Link>
      </p>
    </form>
  );
}
