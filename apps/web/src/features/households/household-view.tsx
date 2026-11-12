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
import { QuickEntryButton } from "@/features/ledger/quick-entry-button";
import { errorMessage, type Household } from "@/lib/api/client";

import {
  useCreateInvite,
  useDeactivateMember,
  useHousehold,
  useInvites,
  useMembers,
  useRevokeInvite,
} from "./hooks";

const roleLabel = { OWNER: "Responsável", MEMBER: "Membro" } as const;

export function HouseholdView({ id }: { id: string }) {
  const household = useHousehold(id);

  if (household.isPending) return <p role="status">Carregando…</p>;
  if (household.isError) {
    return <FormError message={errorMessage(household.error)} />;
  }
  const h = household.data;
  const isOwner = h.role === "OWNER";

  return (
    <>
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">{h.name}</h1>
        <p className="text-sm text-muted-foreground">
          Você é {roleLabel[h.role].toLowerCase()} · fuso {h.timezone}
        </p>
        <nav className="flex flex-wrap gap-4 text-sm">
          <Link href={`/entry?household=${h.id}`} className="underline">
            Lançar
          </Link>
          <Link href={`/transactions?household=${h.id}`} className="underline">
            Lançamentos
          </Link>
          <Link href={`/obligations?household=${h.id}`} className="underline">
            Contas a pagar e receber
          </Link>
          <Link href={`/favorites?household=${h.id}`} className="underline">
            Favoritos
          </Link>
          <Link href={`/accounts?household=${h.id}`} className="underline">
            Contas
          </Link>
          <Link href={`/transfers?household=${h.id}`} className="underline">
            Transferências
          </Link>
          <Link href={`/categories?household=${h.id}`} className="underline">
            Categorias
          </Link>
        </nav>
      </div>
      <Members household={h} />
      {isOwner && <Invites householdId={h.id} />}
      <QuickEntryButton householdId={h.id} />
    </>
  );
}

function Members({ household }: { household: Household }) {
  const members = useMembers(household.id);
  const deactivate = useDeactivateMember(household.id);
  const isOwner = household.role === "OWNER";

  return (
    <Card>
      <CardHeader>
        <CardTitle>Membros</CardTitle>
        <CardDescription>
          Todos os membros ativos veem todos os lançamentos da família.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {members.isPending && <p role="status">Carregando…</p>}
        {members.isError && <FormError message={errorMessage(members.error)} />}
        <ul className="flex flex-col divide-y">
          {members.data?.map((m) => (
            <li
              key={m.id}
              className="flex items-center justify-between gap-3 py-2"
            >
              <div className="flex min-w-0 flex-col">
                <span className="truncate font-medium">{m.name}</span>
                <span className="truncate text-sm text-muted-foreground">
                  {m.email} · {roleLabel[m.role]}
                  {m.status === "INACTIVE" && " · inativo"}
                </span>
              </div>
              {isOwner &&
                m.status === "ACTIVE" &&
                m.id !== household.memberId && (
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={deactivate.isPending}
                    onClick={() => {
                      if (
                        window.confirm(
                          `Desativar ${m.name}? O histórico será preservado.`,
                        )
                      ) {
                        deactivate.mutate(m.id);
                      }
                    }}
                  >
                    Desativar
                  </Button>
                )}
            </li>
          ))}
        </ul>
        {deactivate.isError && (
          <FormError message={errorMessage(deactivate.error)} />
        )}
      </CardContent>
    </Card>
  );
}

function Invites({ householdId }: { householdId: string }) {
  const invites = useInvites(householdId, true);
  const create = useCreateInvite(householdId);
  const revoke = useRevokeInvite(householdId);
  const [copied, setCopied] = useState(false);
  const link = create.data?.url;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Convidar</CardTitle>
        <CardDescription>
          Gere um link de uso único (válido por 7 dias) e envie para quem vai
          entrar na família.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Button
          className="self-start"
          disabled={create.isPending}
          onClick={() => {
            setCopied(false);
            create.mutate();
          }}
        >
          {create.isPending ? "Gerando…" : "Gerar link de convite"}
        </Button>
        {create.isError && <FormError message={errorMessage(create.error)} />}
        {link && (
          <div className="flex flex-col gap-2 rounded-lg border p-3">
            <p className="text-sm">
              Copie agora: o link não poderá ser exibido novamente.
            </p>
            <code className="break-all text-xs">{link}</code>
            <Button
              variant="outline"
              size="sm"
              className="self-start"
              onClick={async () => {
                await navigator.clipboard.writeText(link);
                setCopied(true);
              }}
            >
              {copied ? "Copiado" : "Copiar link"}
            </Button>
          </div>
        )}
        {invites.data && invites.data.length > 0 && (
          <div className="flex flex-col gap-2">
            <h3 className="text-sm font-medium">Convites pendentes</h3>
            <ul className="flex flex-col divide-y">
              {invites.data.map((inv) => (
                <li
                  key={inv.id}
                  className="flex items-center justify-between gap-3 py-2 text-sm"
                >
                  <span>
                    Expira em{" "}
                    {new Date(inv.expiresAt).toLocaleDateString("pt-BR")}
                  </span>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={revoke.isPending}
                    onClick={() => revoke.mutate(inv.id)}
                  >
                    Revogar
                  </Button>
                </li>
              ))}
            </ul>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
