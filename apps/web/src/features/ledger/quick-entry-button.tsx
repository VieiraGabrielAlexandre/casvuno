import Link from "next/link";

/** Floating "+" that opens the quick entry from anywhere in the household (README §9). */
export function QuickEntryButton({ householdId }: { householdId: string }) {
  if (!householdId) return null;
  return (
    <>
      {/* Room at the end of the page, so the button never covers the last item. */}
      <div aria-hidden="true" className="h-16" />
      <Link
        href={`/entry?household=${householdId}`}
        aria-label="Novo lançamento"
        title="Novo lançamento"
        className="fixed right-4 bottom-4 z-10 flex size-14 items-center justify-center rounded-full bg-primary text-3xl leading-none text-primary-foreground shadow-lg hover:bg-primary/80 focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
      >
        <span aria-hidden="true">+</span>
      </Link>
    </>
  );
}
