/** Calendar dates travel as "YYYY-MM-DD" strings and are never parsed into a Date (no timezone shifts). */

const isoDate = /^(\d{4})-(\d{2})-(\d{2})$/;

/** "2026-11-05" → "05/11/2026". Unknown formats are returned unchanged. */
export function formatDate(date: string): string {
  const m = isoDate.exec(date);
  return m ? `${m[3]}/${m[2]}/${m[1]}` : date;
}

/** Today's calendar date in the household timezone (falls back to the device timezone). */
export function todayIn(timeZone?: string, now: Date = new Date()): string {
  const format = (tz?: string) =>
    new Intl.DateTimeFormat("en-CA", {
      timeZone: tz,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(now);
  try {
    return format(timeZone);
  } catch {
    return format(undefined); // invalid timezone name
  }
}
