/**
 * Idempotency-Key generation for critical financial mutations (ADR 0007).
 *
 * One key per submission: a retry of the same payload (after a timeout or while offline)
 * reuses the key, so the server replays the original result instead of moving money twice.
 * A different payload gets a new key (reusing it would be rejected with 409), and after a
 * success the next submission starts fresh.
 */
export class IdempotencyKeys {
  private current: { key: string; payload: string } | null = null;

  constructor(private readonly generate: () => string = newIdempotencyKey) {}

  /** Returns the key for this payload: the same one while the payload does not change. */
  keyFor(payload: unknown): string {
    const serialized = JSON.stringify(payload);
    if (this.current?.payload !== serialized) {
      this.current = { key: this.generate(), payload: serialized };
    }
    return this.current.key;
  }

  /** Forgets the key; call it after the server confirmed the operation. */
  reset(): void {
    this.current = null;
  }
}

/**
 * Random UUID v4. `crypto.randomUUID` only exists in secure contexts (HTTPS/localhost);
 * `getRandomValues` also works on plain HTTP inside a home network.
 */
export function newIdempotencyKey(): string {
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID();
  const b = crypto.getRandomValues(new Uint8Array(16));
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const hex = Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
