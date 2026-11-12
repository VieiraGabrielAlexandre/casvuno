"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

import { AppShell } from "@/components/app-shell";
import { ObligationsView } from "@/features/obligations/obligations-view";

// Static export: the household comes from the query string (ADR 0009).
export default function ObligationsPage() {
  return (
    <Suspense>
      <AppShell>
        <ObligationsFromQuery />
      </AppShell>
    </Suspense>
  );
}

function ObligationsFromQuery() {
  const params = useSearchParams();
  return <ObligationsView householdId={params.get("household") ?? ""} />;
}
