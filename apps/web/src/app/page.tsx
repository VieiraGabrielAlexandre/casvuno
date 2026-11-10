"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";

import { CenteredMessage } from "@/components/app-shell";
import { useMe } from "@/features/auth/hooks";
import { useHouseholds } from "@/features/households/hooks";
import { errorMessage } from "@/lib/api/client";

/** Entry point: sends the user to login, onboarding or their household. */
export default function Home() {
  const router = useRouter();
  const me = useMe();
  const households = useHouseholds(Boolean(me.data));

  useEffect(() => {
    if (me.data === null) {
      router.replace("/login");
      return;
    }
    const list = households.data;
    if (!list) return;
    if (list.length === 0) router.replace("/onboarding");
    else if (list.length === 1) router.replace(`/household?id=${list[0].id}`);
    else router.replace("/households");
  }, [me.data, households.data, router]);

  const error = me.error ?? households.error;
  return (
    <CenteredMessage>
      {error ? errorMessage(error) : "Carregando…"}
    </CenteredMessage>
  );
}
