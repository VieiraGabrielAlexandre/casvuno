"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

import { AppShell } from "@/components/app-shell";
import { TransfersView } from "@/features/transfers/transfers-view";

// Static export: the household comes from the query string (ADR 0009).
export default function TransfersPage() {
  return (
    <Suspense>
      <AppShell>
        <TransfersFromQuery />
      </AppShell>
    </Suspense>
  );
}

function TransfersFromQuery() {
  const params = useSearchParams();
  return (
    <TransfersView
      householdId={params.get("household") ?? ""}
      fromAccountId={params.get("from") ?? undefined}
    />
  );
}
