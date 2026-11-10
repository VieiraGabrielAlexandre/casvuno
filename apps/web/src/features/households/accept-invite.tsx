"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";

import { CenteredMessage, PublicPage } from "@/components/app-shell";
import { FormError } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import { useMe } from "@/features/auth/hooks";
import { errorMessage } from "@/lib/api/client";

import { useAcceptInvite } from "./hooks";

export function AcceptInvite() {
  const params = useSearchParams();
  const token = params.get("token") ?? "";
  const me = useMe();
  const accept = useAcceptInvite();
  const router = useRouter();

  if (!token) {
    return <CenteredMessage>Link de convite inválido.</CenteredMessage>;
  }
  if (me.isPending) return <CenteredMessage>Carregando…</CenteredMessage>;

  const here = `/invite?token=${encodeURIComponent(token)}`;
  return (
    <PublicPage>
      <div className="flex flex-col gap-4 text-center">
        <h2 className="text-lg font-medium">Convite para uma família</h2>
        {me.data ? (
          <>
            <p className="text-sm text-muted-foreground">
              Você entrará como membro e verá todos os lançamentos da família.
            </p>
            <Button
              disabled={accept.isPending}
              onClick={() =>
                accept.mutate(token, {
                  onSuccess: (h) => router.replace(`/household?id=${h.id}`),
                })
              }
            >
              {accept.isPending ? "Entrando…" : `Aceitar como ${me.data.name}`}
            </Button>
            {accept.isError && (
              <FormError message={errorMessage(accept.error)} />
            )}
          </>
        ) : (
          <>
            <p className="text-sm text-muted-foreground">
              Entre ou crie uma conta para aceitar o convite.
            </p>
            <Link
              className="underline"
              href={`/register?invite=${encodeURIComponent(token)}&next=${encodeURIComponent(here)}`}
            >
              Criar conta
            </Link>
            <Link
              className="underline"
              href={`/login?next=${encodeURIComponent(here)}`}
            >
              Já tenho conta
            </Link>
          </>
        )}
      </div>
    </PublicPage>
  );
}
