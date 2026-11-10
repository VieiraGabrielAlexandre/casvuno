"use client";

import { useCallback, useEffect, useState } from "react";

import { Button } from "@/components/ui/button";

type Status = "checking" | "ok" | "unavailable";

const labels: Record<Status, string> = {
  checking: "Verificando conexão com o servidor…",
  ok: "Servidor disponível.",
  unavailable:
    "Servidor indisponível. Verifique sua conexão e tente novamente.",
};

async function fetchStatus(): Promise<Status> {
  try {
    const res = await fetch("/health", { cache: "no-store" });
    return res.ok ? "ok" : "unavailable";
  } catch {
    return "unavailable";
  }
}

/** Shows whether the API (and its database) is reachable through the same-origin /health route. */
export function ApiStatus() {
  const [status, setStatus] = useState<Status>("checking");

  const refresh = useCallback(async () => {
    setStatus("checking");
    setStatus(await fetchStatus());
  }, []);

  useEffect(() => {
    let active = true;
    fetchStatus().then((s) => {
      if (active) setStatus(s);
    });
    return () => {
      active = false;
    };
  }, []);

  return (
    <div className="flex flex-col gap-3 rounded-lg border p-4">
      <p role="status" aria-live="polite">
        {labels[status]}
      </p>
      <Button
        variant="outline"
        className="self-start"
        onClick={refresh}
        disabled={status === "checking"}
      >
        Verificar novamente
      </Button>
    </div>
  );
}
