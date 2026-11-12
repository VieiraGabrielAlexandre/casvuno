"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

import { AppShell } from "@/components/app-shell";
import { TemplatesView } from "@/features/templates/templates-view";

// Static export: the household comes from the query string (ADR 0009).
export default function FavoritesPage() {
  return (
    <Suspense>
      <AppShell>
        <FavoritesFromQuery />
      </AppShell>
    </Suspense>
  );
}

function FavoritesFromQuery() {
  const params = useSearchParams();
  return <TemplatesView householdId={params.get("household") ?? ""} />;
}
