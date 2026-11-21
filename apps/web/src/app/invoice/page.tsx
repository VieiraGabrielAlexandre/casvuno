"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

import { AppShell } from "@/components/app-shell";
import { InvoiceDetail } from "@/features/cards/invoice-detail";

// Static export: household and invoice come from the query string (ADR 0009, I-24).
export default function InvoicePage() {
  return (
    <Suspense>
      <AppShell>
        <InvoiceFromQuery />
      </AppShell>
    </Suspense>
  );
}

function InvoiceFromQuery() {
  const params = useSearchParams();
  const householdId = params.get("household") ?? "";
  const invoiceId = params.get("id") ?? "";
  // key: switching invoices resets local state (open form, messages).
  return (
    <InvoiceDetail
      key={invoiceId}
      householdId={householdId}
      invoiceId={invoiceId}
    />
  );
}
