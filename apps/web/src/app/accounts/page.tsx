"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

import { AppShell } from "@/components/app-shell";
import { AccountsView } from "@/features/accounts/accounts-view";

// Static export: the household comes from the query string (ADR 0009).
export default function AccountsPage() {
  return (
    <Suspense>
      <AppShell>
        <AccountsFromQuery />
      </AppShell>
    </Suspense>
  );
}

function AccountsFromQuery() {
  const householdId = useSearchParams().get("household") ?? "";
  return <AccountsView householdId={householdId} />;
}
