/** Marks a figure as commitment (AGENTS.md §5: every metric names its view). */
export function CommitmentTag() {
  return (
    <span className="ml-1 rounded-full border px-2 py-0.5 text-xs font-medium">
      compromisso
    </span>
  );
}

/** The available limit is estimated from what the family recorded, never from the bank. */
export function EstimatedTag() {
  return (
    <span className="ml-1 rounded-full border border-dashed px-2 py-0.5 text-xs font-medium">
      estimado
    </span>
  );
}
