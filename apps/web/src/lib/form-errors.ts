import type { FieldValues, Path, UseFormSetError } from "react-hook-form";

import { ApiError } from "@/lib/api/client";

/**
 * Shows API validation errors next to their fields. Errors that do not belong to a known
 * field are reported on the form root (`errors.root.server`).
 */
export function applyApiError<T extends FieldValues>(
  err: unknown,
  setError: UseFormSetError<T>,
  fields: readonly Path<T>[],
): void {
  if (err instanceof ApiError) {
    const known = err.details.filter((d) =>
      (fields as readonly string[]).includes(d.field),
    );
    for (const d of known) {
      setError(d.field as Path<T>, { message: err.message });
    }
    if (known.length > 0) return;
    setError("root.server", { message: err.message });
    return;
  }
  setError("root.server", { message: "Erro inesperado. Tente novamente." });
}

/** Accepts only same-site relative paths, preventing open redirects through ?next=. */
export function safeNext(next: string | null, fallback = "/"): string {
  return next && next.startsWith("/") && !next.startsWith("//")
    ? next
    : fallback;
}
