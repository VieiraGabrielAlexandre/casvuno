"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

import { AppShell } from "@/components/app-shell";
import { CardsView } from "@/features/cards/cards-view";

// Static export: the household comes from the query string (ADR 0009).
export default function CardsPage() {
  return (
    <Suspense>
      <AppShell>
        <CardsFromQuery />
      </AppShell>
    </Suspense>
  );
}

function CardsFromQuery() {
  const householdId = useSearchParams().get("household") ?? "";
  return <CardsView householdId={householdId} />;
}
