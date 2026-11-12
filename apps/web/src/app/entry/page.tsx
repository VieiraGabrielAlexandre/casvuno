"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

import { AppShell } from "@/components/app-shell";
import { EntryView } from "@/features/ledger/entry-view";

// Static export: the household comes from the query string (ADR 0009).
export default function EntryPage() {
  return (
    <Suspense>
      <AppShell>
        <EntryFromQuery />
      </AppShell>
    </Suspense>
  );
}

function EntryFromQuery() {
  const params = useSearchParams();
  return (
    <EntryView
      householdId={params.get("household") ?? ""}
      correctId={params.get("correct") ?? ""}
    />
  );
}
