"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

import { AppShell } from "@/components/app-shell";
import { CategoriesView } from "@/features/categories/categories-view";

// Static export: the household comes from the query string (ADR 0009).
export default function CategoriesPage() {
  return (
    <Suspense>
      <AppShell>
        <CategoriesFromQuery />
      </AppShell>
    </Suspense>
  );
}

function CategoriesFromQuery() {
  const householdId = useSearchParams().get("household") ?? "";
  return <CategoriesView householdId={householdId} />;
}
