"use client";

import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { useState } from "react";

import { accountsKey } from "@/features/accounts/hooks";
import { api, call, type CreateTransferRequest } from "@/lib/api/client";
import { IdempotencyKeys } from "@/lib/idempotency";

const PAGE_SIZE = 20;

const transfersKey = (householdId: string) =>
  ["households", householdId, "transfers"] as const;

export function useTransfers(householdId: string, accountId?: string) {
  return useInfiniteQuery({
    queryKey: [...transfersKey(householdId), { accountId }],
    queryFn: ({ pageParam }) =>
      call(
        api.GET("/api/v1/households/{householdId}/transfers", {
          params: {
            path: { householdId },
            query: {
              limit: PAGE_SIZE,
              ...(accountId ? { accountId } : {}),
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

/**
 * Creates a transfer with an Idempotency-Key per submission (ADR 0007): retrying the same
 * payload after a network failure reuses the key, so the money never moves twice. There is
 * no optimistic update: balances change only after the server confirms.
 */
export function useCreateTransfer(householdId: string) {
  const qc = useQueryClient();
  const [keys] = useState(() => new IdempotencyKeys());
  return useMutation({
    mutationFn: (body: CreateTransferRequest) =>
      call(
        api.POST("/api/v1/households/{householdId}/transfers", {
          params: {
            path: { householdId },
            header: { "Idempotency-Key": keys.keyFor(body) },
          },
          body,
        }),
      ),
    onSuccess: async () => {
      keys.reset();
      await Promise.all([
        qc.invalidateQueries({ queryKey: accountsKey(householdId) }),
        qc.invalidateQueries({ queryKey: transfersKey(householdId) }),
      ]);
    },
  });
}
