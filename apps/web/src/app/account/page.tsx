"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

import { AppShell } from "@/components/app-shell";
import { AccountDetail } from "@/features/accounts/account-detail";

// Static export: household and account come from the query string (ADR 0009).
export default function AccountPage() {
  return (
    <Suspense>
      <AppShell>
        <AccountFromQuery />
      </AppShell>
    </Suspense>
  );
}

function AccountFromQuery() {
  const params = useSearchParams();
  const householdId = params.get("household") ?? "";
  const accountId = params.get("id") ?? "";
  // key: switching accounts resets local state (edit mode).
  return (
    <AccountDetail
      key={accountId}
      householdId={householdId}
      accountId={accountId}
    />
  );
}
