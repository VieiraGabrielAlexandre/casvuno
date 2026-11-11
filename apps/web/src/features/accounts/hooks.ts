"use client";

import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";

import {
  api,
  call,
  type CreateAccountRequest,
  type UpdateAccountRequest,
} from "@/lib/api/client";

const STATEMENT_PAGE_SIZE = 25;

/** Everything derived from cash movements: account list, balances, statements. */
export const accountsKey = (householdId: string) =>
  ["households", householdId, "accounts"] as const;

export function useAccounts(householdId: string, includeArchived = false) {
  return useQuery({
    queryKey: [...accountsKey(householdId), "list", { includeArchived }],
    queryFn: () =>
      call(
        api.GET("/api/v1/households/{householdId}/accounts", {
          params: {
            path: { householdId },
            query: includeArchived ? { includeArchived: "true" } : {},
          },
        }),
      ),
    enabled: householdId !== "",
  });
}

export function useAccount(householdId: string, accountId: string) {
  return useQuery({
    queryKey: [...accountsKey(householdId), "detail", accountId],
    queryFn: () =>
      call(
        api.GET("/api/v1/households/{householdId}/accounts/{accountId}", {
          params: { path: { householdId, accountId } },
        }),
      ),
    enabled: householdId !== "" && accountId !== "",
  });
}

export function useStatement(householdId: string, accountId: string) {
  return useInfiniteQuery({
    queryKey: [...accountsKey(householdId), "statement", accountId],
    queryFn: ({ pageParam }) =>
      call(
        api.GET(
          "/api/v1/households/{householdId}/accounts/{accountId}/movements",
          {
            params: {
              path: { householdId, accountId },
              query: {
                limit: STATEMENT_PAGE_SIZE,
                ...(pageParam ? { cursor: pageParam } : {}),
              },
            },
          },
        ),
      ),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    enabled: householdId !== "" && accountId !== "",
  });
}

export function useCreateAccount(householdId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateAccountRequest) =>
      call(
        api.POST("/api/v1/households/{householdId}/accounts", {
          params: { path: { householdId } },
          body,
        }),
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: accountsKey(householdId) }),
  });
}

/** Edits, archives (isActive=false) or restores (isActive=true) an account. */
export function useUpdateAccount(householdId: string, accountId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: UpdateAccountRequest) =>
      call(
        api.PATCH("/api/v1/households/{householdId}/accounts/{accountId}", {
          params: { path: { householdId, accountId } },
          body,
        }),
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: accountsKey(householdId) }),
  });
}
