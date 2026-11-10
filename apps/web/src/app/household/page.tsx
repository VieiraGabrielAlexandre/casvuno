"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

import { AppShell } from "@/components/app-shell";
import { HouseholdView } from "@/features/households/household-view";

// Static export: detail pages take their ID from the query string (ADR 0009).
export default function HouseholdPage() {
  return (
    <Suspense>
      <AppShell>
        <HouseholdFromQuery />
      </AppShell>
    </Suspense>
  );
}

function HouseholdFromQuery() {
  const id = useSearchParams().get("id") ?? "";
  return <HouseholdView id={id} />;
}
