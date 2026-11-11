import createClient from "openapi-fetch";

import type { components, paths } from "./schema";

export type User = components["schemas"]["User"];
export type Household = components["schemas"]["Household"];
export type Member = components["schemas"]["Member"];
export type Invite = components["schemas"]["Invite"];
export type CreatedInvite = components["schemas"]["CreatedInvite"];
export type Category = components["schemas"]["Category"];
export type CategoryKind = components["schemas"]["CategoryKind"];
export type Account = components["schemas"]["Account"];
export type AccountType = components["schemas"]["AccountType"];
export type Ownership = components["schemas"]["Ownership"];
export type Movement = components["schemas"]["Movement"];
export type Transfer = components["schemas"]["Transfer"];
export type CreateAccountRequest =
  components["schemas"]["CreateAccountRequest"];
export type UpdateAccountRequest =
  components["schemas"]["UpdateAccountRequest"];
export type CreateTransferRequest =
  components["schemas"]["CreateTransferRequest"];
type ErrorEnvelope = components["schemas"]["ErrorEnvelope"];
export type FieldError = NonNullable<ErrorEnvelope["error"]["details"]>[number];

/** Typed client for docs/openapi.yaml. Same-origin: the session cookie travels automatically. */
export const api = createClient<paths>({
  baseUrl: typeof window === "undefined" ? "" : window.location.origin,
  credentials: "same-origin",
  // Resolve fetch at call time (not at import), so test doubles and polyfills apply.
  fetch: (request) => globalThis.fetch(request),
});

/** API failure in the standard envelope; status 0 means the server could not be reached. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly details: FieldError[] = [],
  ) {
    super(message);
    this.name = "ApiError";
  }
}

const offlineMessage =
  "Sem conexão com o servidor. Verifique sua internet e tente novamente.";
const unavailableMessage =
  "O servidor está indisponível no momento. Tente novamente em instantes.";

/**
 * Awaits an openapi-fetch call and returns its data, throwing ApiError on any failure.
 * Network failures never resolve as success (no false confirmation while offline).
 */
export async function call<T>(
  request: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  let result: { data?: T; error?: unknown; response: Response };
  try {
    result = await request;
  } catch {
    throw new ApiError(0, "NETWORK_ERROR", offlineMessage);
  }
  const { data, error, response } = result;
  if (!response.ok) {
    const env = error as Partial<ErrorEnvelope> | undefined;
    // A 5xx without our envelope comes from the proxy (Next dev server, Caddy), not from
    // the API: the API is down or unreachable.
    if (!env?.error && response.status >= 500) {
      throw new ApiError(
        response.status,
        "SERVER_UNAVAILABLE",
        unavailableMessage,
      );
    }
    throw new ApiError(
      response.status,
      env?.error?.code ?? "UNKNOWN_ERROR",
      env?.error?.message ?? "Erro inesperado. Tente novamente.",
      env?.error?.details ?? [],
    );
  }
  return data as T;
}

export function errorMessage(err: unknown): string {
  return err instanceof ApiError
    ? err.message
    : "Erro inesperado. Tente novamente.";
}
