"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

import { AppShell } from "@/components/app-shell";
import { CardDetail } from "@/features/cards/card-detail";

// Static export: household and card come from the query string (ADR 0009).
export default function CardPage() {
  return (
    <Suspense>
      <AppShell>
        <CardFromQuery />
      </AppShell>
    </Suspense>
  );
}

function CardFromQuery() {
  const params = useSearchParams();
  const householdId = params.get("household") ?? "";
  const cardId = params.get("id") ?? "";
  // key: switching cards resets local state (edit mode, old invoices).
  return <CardDetail key={cardId} householdId={householdId} cardId={cardId} />;
}
