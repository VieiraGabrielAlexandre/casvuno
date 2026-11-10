"use client";

import Link from "next/link";

import { AppShell } from "@/components/app-shell";
import { FormError } from "@/components/form-field";
import { useHouseholds } from "@/features/households/hooks";
import { errorMessage } from "@/lib/api/client";

/** Household selection (a user may belong to more than one household). */
export default function HouseholdsPage() {
  return (
    <AppShell>
      <HouseholdList />
    </AppShell>
  );
}

function HouseholdList() {
  const households = useHouseholds();
  return (
    <>
      <h1 className="text-2xl font-semibold tracking-tight">Suas famílias</h1>
      {households.isPending && <p role="status">Carregando…</p>}
      {households.isError && (
        <FormError message={errorMessage(households.error)} />
      )}
      {households.data?.length === 0 && (
        <p className="text-muted-foreground">
          Você ainda não participa de nenhuma família.
        </p>
      )}
      <ul className="flex flex-col gap-2">
        {households.data?.map((h) => (
          <li key={h.id}>
            <Link
              href={`/household?id=${h.id}`}
              className="flex items-center justify-between rounded-lg border p-4 hover:bg-muted"
            >
              <span className="font-medium">{h.name}</span>
              <span className="text-sm text-muted-foreground">
                {h.role === "OWNER" ? "Responsável" : "Membro"}
              </span>
            </Link>
          </li>
        ))}
      </ul>
      <Link href="/onboarding" className="text-sm underline">
        Criar outra família
      </Link>
    </>
  );
}
