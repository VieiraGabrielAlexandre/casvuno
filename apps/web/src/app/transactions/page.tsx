"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

import { AppShell } from "@/components/app-shell";
import { TransactionsView } from "@/features/transactions/transactions-view";

// Static export: the household comes from the query string (ADR 0009).
export default function TransactionsPage() {
  return (
    <Suspense>
      <AppShell>
        <TransactionsFromQuery />
      </AppShell>
    </Suspense>
  );
}

function TransactionsFromQuery() {
  const params = useSearchParams();
  return <TransactionsView householdId={params.get("household") ?? ""} />;
}
