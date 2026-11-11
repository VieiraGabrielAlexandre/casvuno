import { useId } from "react";

import { Label } from "@/components/ui/label";

type Props = React.ComponentProps<"select"> & {
  label: string;
  error?: string;
};

/** Labelled native select (works well on mobile) with an accessible error message. */
export function SelectField({ label, error, id, children, ...select }: Props) {
  const generated = useId();
  const selectId = id ?? generated;
  const errorId = `${selectId}-error`;
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={selectId}>{label}</Label>
      <select
        id={selectId}
        className="h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-base aria-invalid:border-destructive md:text-sm"
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errorId : undefined}
        {...select}
      >
        {children}
      </select>
      {error && (
        <p id={errorId} className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}
