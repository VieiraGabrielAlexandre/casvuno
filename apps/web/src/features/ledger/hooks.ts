"use client";

import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useState } from "react";

import { accountsKey } from "@/features/accounts/hooks";
import {
  api,
  call,
  type CreateQuickTemplateRequest,
  type CreateTransactionRequest,
  type Obligation,
  type PayObligationRequest,
  type TransactionQuery,
  type UpdateObligationRequest,
  type UpdateQuickTemplateRequest,
  type UpdateTransactionRequest,
} from "@/lib/api/client";
import { IdempotencyKeys } from "@/lib/idempotency";

const PAGE_SIZE = 25;

/** Transactions and obligations: every ledger mutation invalidates both (and the balances). */
const ledgerKey = (householdId: string) =>
  ["households", householdId, "ledger"] as const;
const templatesKey = (householdId: string) =>
  ["households", householdId, "quick-templates"] as const;

function useInvalidateLedger(householdId: string) {
  const qc = useQueryClient();
  return () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: ledgerKey(householdId) }),
      qc.invalidateQueries({ queryKey: accountsKey(householdId) }),
    ]);
}

/** Filters accepted by the transaction list (all optional; empty values are dropped). */
export type TransactionFilters = Omit<TransactionQuery, "limit" | "cursor">;

export function useTransactions(
  householdId: string,
  filters: TransactionFilters,
) {
  return useInfiniteQuery({
    queryKey: [...ledgerKey(householdId), "transactions", filters],
    queryFn: ({ pageParam }) =>
      call(
        api.GET("/api/v1/households/{householdId}/transactions", {
          params: {
            path: { householdId },
            query: {
              ...filters,
              limit: PAGE_SIZE,
              ...(pageParam ? { cursor: pageParam } : {}),
            },
          },
        }),
      ),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    enabled: householdId !== "",
  });
}

export function useTransaction(householdId: string, transactionId: string) {
  return useQuery({
    queryKey: [...ledgerKey(householdId), "transaction", transactionId],
    queryFn: () =>
      call(
        api.GET(
          "/api/v1/households/{householdId}/transactions/{transactionId}",
          { params: { path: { householdId, transactionId } } },
        ),
      ),
    enabled: householdId !== "" && transactionId !== "",
  });
}

/** "Repetir último": the caller's own most recent active entry, or null. */
export function useLastOwnTransaction(householdId: string) {
  return useQuery({
    queryKey: [...ledgerKey(householdId), "last-own"],
    queryFn: async () =>
      (
        await call(
          api.GET("/api/v1/households/{householdId}/transactions", {
            params: {
              path: { householdId },
              query: { createdBy: "me", limit: 1 },
            },
          }),
        )
      ).items[0] ?? null,
    enabled: householdId !== "",
  });
}

/**
 * Creates an entry (immediate or planned) with an Idempotency-Key per submission
 * (ADR 0007): a retry of the same payload reuses the key, so it is never recorded twice.
 */
export function useCreateEntry(householdId: string) {
  const invalidate = useInvalidateLedger(householdId);
  const [keys] = useState(() => new IdempotencyKeys());
  return useMutation({
    mutationFn: (body: CreateTransactionRequest) =>
      call(
        api.POST("/api/v1/households/{householdId}/transactions", {
          params: {
            path: { householdId },
            header: { "Idempotency-Key": keys.keyFor(body) },
          },
          body,
        }),
      ),
    onSuccess: async () => {
      keys.reset();
      await invalidate();
    },
  });
}

/** Voids an entry (cash comes back through a reversal). Idempotent on the server too. */
export function useVoidTransaction(householdId: string) {
  const invalidate = useInvalidateLedger(householdId);
  const [keys] = useState(() => new IdempotencyKeys());
  return useMutation({
    mutationFn: (transactionId: string) =>
      call(
        api.POST(
          "/api/v1/households/{householdId}/transactions/{transactionId}/void",
          {
            params: {
              path: { householdId, transactionId },
              header: {
                "Idempotency-Key": keys.keyFor({ void: transactionId }),
              },
            },
          },
        ),
      ),
    onSuccess: async () => {
      keys.reset();
      await invalidate();
    },
  });
}

export function useUpdateTransaction(householdId: string) {
  const invalidate = useInvalidateLedger(householdId);
  return useMutation({
    mutationFn: ({
      id,
      body,
    }: {
      id: string;
      body: UpdateTransactionRequest;
    }) =>
      call(
        api.PATCH(
          "/api/v1/households/{householdId}/transactions/{transactionId}",
          { params: { path: { householdId, transactionId: id } }, body },
        ),
      ),
    onSuccess: invalidate,
  });
}

export type ObligationView = "open" | "overdue" | "all";

export function useObligations(
  householdId: string,
  view: ObligationView,
  kind?: Obligation["kind"],
) {
  return useInfiniteQuery({
    queryKey: [...ledgerKey(householdId), "obligations", { view, kind }],
    queryFn: ({ pageParam }) =>
      call(
        api.GET("/api/v1/households/{householdId}/obligations", {
          params: {
            path: { householdId },
            query: {
              view,
              limit: PAGE_SIZE,
              ...(kind ? { kind } : {}),
              ...(pageParam ? { cursor: pageParam } : {}),
            },
          },
        }),
      ),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    enabled: householdId !== "",
  });
}

/** Pays or receives (part of) an obligation; Idempotency-Key per submission. */
export function usePayObligation(householdId: string, obligationId: string) {
  const invalidate = useInvalidateLedger(householdId);
  const [keys] = useState(() => new IdempotencyKeys());
  return useMutation({
    mutationFn: (body: PayObligationRequest) =>
      call(
        api.POST(
          "/api/v1/households/{householdId}/obligations/{obligationId}/payments",
          {
            params: {
              path: { householdId, obligationId },
              header: { "Idempotency-Key": keys.keyFor(body) },
            },
            body,
          },
        ),
      ),
    onSuccess: async () => {
      keys.reset();
      await invalidate();
    },
  });
}

export function useUpdateObligation(householdId: string, obligationId: string) {
  const invalidate = useInvalidateLedger(householdId);
  return useMutation({
    mutationFn: (body: UpdateObligationRequest) =>
      call(
        api.PATCH(
          "/api/v1/households/{householdId}/obligations/{obligationId}",
          { params: { path: { householdId, obligationId } }, body },
        ),
      ),
    onSuccess: invalidate,
  });
}

export function useCancelObligation(householdId: string, obligationId: string) {
  const invalidate = useInvalidateLedger(householdId);
  return useMutation({
    mutationFn: () =>
      call(
        api.POST(
          "/api/v1/households/{householdId}/obligations/{obligationId}/cancel",
          { params: { path: { householdId, obligationId } } },
        ),
      ),
    onSuccess: invalidate,
  });
}

export function useTemplates(householdId: string) {
  return useQuery({
    queryKey: templatesKey(householdId),
    queryFn: async () =>
      (
        await call(
          api.GET("/api/v1/households/{householdId}/quick-templates", {
            params: { path: { householdId } },
          }),
        )
      ).items,
    enabled: householdId !== "",
  });
}

export function useCreateTemplate(householdId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateQuickTemplateRequest) =>
      call(
        api.POST("/api/v1/households/{householdId}/quick-templates", {
          params: { path: { householdId } },
          body,
        }),
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: templatesKey(householdId) }),
  });
}

export function useUpdateTemplate(householdId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      body,
    }: {
      id: string;
      body: UpdateQuickTemplateRequest;
    }) =>
      call(
        api.PATCH(
          "/api/v1/households/{householdId}/quick-templates/{templateId}",
          { params: { path: { householdId, templateId: id } }, body },
        ),
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: templatesKey(householdId) }),
  });
}

export function useDeleteTemplate(householdId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      call(
        api.DELETE(
          "/api/v1/households/{householdId}/quick-templates/{templateId}",
          { params: { path: { householdId, templateId: id } } },
        ),
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: templatesKey(householdId) }),
  });
}
