"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";

import { Button } from "@/components/ui/button";
import { useLogout, useMe } from "@/features/auth/hooks";
import { errorMessage } from "@/lib/api/client";

/** Layout for authenticated pages: redirects to /login when there is no session. */
export function AppShell({ children }: { children: React.ReactNode }) {
  const me = useMe();
  const logout = useLogout();
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    if (me.data === null) {
      const next = pathname + window.location.search;
      router.replace(`/login?next=${encodeURIComponent(next)}`);
    }
  }, [me.data, pathname, router]);

  if (me.isError) {
    return <CenteredMessage>{errorMessage(me.error)}</CenteredMessage>;
  }
  if (!me.data) {
    return <CenteredMessage>Carregando…</CenteredMessage>;
  }

  return (
    <div className="flex min-h-full flex-1 flex-col">
      <header className="flex items-center justify-between gap-4 border-b px-4 py-3">
        <Link href="/households" className="font-semibold">
          Casvuno
        </Link>
        <div className="flex items-center gap-3 text-sm">
          <span className="text-muted-foreground">{me.data.name}</span>
          <Button
            variant="outline"
            size="sm"
            disabled={logout.isPending}
            onClick={() =>
              logout.mutate(undefined, {
                onSuccess: () => router.replace("/login"),
              })
            }
          >
            Sair
          </Button>
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-2xl flex-1 flex-col gap-6 px-4 py-6">
        {children}
      </main>
    </div>
  );
}

export function CenteredMessage({ children }: { children: React.ReactNode }) {
  return (
    <main className="flex flex-1 items-center justify-center px-4 py-12">
      <p role="status" className="text-muted-foreground">
        {children}
      </p>
    </main>
  );
}

/** Narrow centered layout for public pages (login, register). */
export function PublicPage({ children }: { children: React.ReactNode }) {
  return (
    <main className="mx-auto flex w-full max-w-sm flex-1 flex-col justify-center gap-6 px-4 py-12">
      <h1 className="text-center text-2xl font-semibold tracking-tight">
        Casvuno
      </h1>
      {children}
    </main>
  );
}
