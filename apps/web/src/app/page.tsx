import { ApiStatus } from "@/components/api-status";

export default function Home() {
  return (
    <main className="mx-auto flex w-full max-w-md flex-1 flex-col justify-center gap-6 px-4 py-12">
      <div className="flex flex-col gap-2">
        <h1 className="text-3xl font-semibold tracking-tight">Casvuno</h1>
        <p className="text-muted-foreground">Gestão financeira familiar.</p>
      </div>
      <ApiStatus />
    </main>
  );
}
