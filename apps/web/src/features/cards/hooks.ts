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
  type AdjustInvoiceRequest,
  type CreateCardRequest,
  type InvoiceDetail,
  type PayInvoiceRequest,
  type UpdateCardRequest,
} from "@/lib/api/client";
import { IdempotencyKeys } from "@/lib/idempotency";

const INVOICE_PAGE_SIZE = 24;

/**
 * Cards and their invoices: purchases, voids, payments, adjustments and closing all change
 * invoice totals and the estimated limit, so they invalidate this whole key.
 */
export const cardsKey = (householdId: string) =>
  ["households", householdId, "cards"] as const;

export function useCards(householdId: string, includeArchived = false) {
  return useQuery({
    queryKey: [...cardsKey(householdId), "list", { includeArchived }],
    queryFn: () =>
      call(
        api.GET("/api/v1/households/{householdId}/cards", {
          params: {
            path: { householdId },
            query: includeArchived ? { includeArchived: "true" } : {},
          },
        }),
      ),
    enabled: householdId !== "",
  });
}

export function useCard(householdId: string, cardId: string) {
  return useQuery({
    queryKey: [...cardsKey(householdId), "detail", cardId],
    queryFn: () =>
      call(
        api.GET("/api/v1/households/{householdId}/cards/{cardId}", {
          params: { path: { householdId, cardId } },
        }),
      ),
    enabled: householdId !== "" && cardId !== "",
  });
}

export function useCreateCard(householdId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateCardRequest) =>
      call(
        api.POST("/api/v1/households/{householdId}/cards", {
          params: { path: { householdId } },
          body,
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: cardsKey(householdId) }),
  });
}

/** Edits, archives (isActive=false) or restores (isActive=true) a card. */
export function useUpdateCard(householdId: string, cardId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: UpdateCardRequest) =>
      call(
        api.PATCH("/api/v1/households/{householdId}/cards/{cardId}", {
          params: { path: { householdId, cardId } },
          body,
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: cardsKey(householdId) }),
  });
}

/** A card's invoices by due date (oldest first), from `from` on (reference month = due). */
export function useCardInvoices(
  householdId: string,
  cardId: string,
  from?: string,
) {
  return useInfiniteQuery({
    queryKey: [...cardsKey(householdId), "invoices", cardId, { from }],
    queryFn: ({ pageParam }) =>
      call(
        api.GET("/api/v1/households/{householdId}/cards/{cardId}/invoices", {
          params: {
            path: { householdId, cardId },
            query: {
              limit: INVOICE_PAGE_SIZE,
              ...(from ? { from } : {}),
              ...(pageParam ? { cursor: pageParam } : {}),
            },
          },
        }),
      ),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    enabled: householdId !== "" && cardId !== "",
  });
}

export function useInvoice(householdId: string, invoiceId: string) {
  return useQuery({
    queryKey: [...cardsKey(householdId), "invoice", invoiceId],
    queryFn: () =>
      call(
        api.GET("/api/v1/households/{householdId}/invoices/{invoiceId}", {
          params: { path: { householdId, invoiceId } },
        }),
      ),
    enabled: householdId !== "" && invoiceId !== "",
  });
}

/**
 * Invoice mutations (ADR 0007): one Idempotency-Key per submission, reused on retries of the
 * same payload, so a payment never goes out twice. Never optimistic.
 */
function useInvoiceMutation<T>(
  householdId: string,
  send: (key: string, body: T) => Promise<InvoiceDetail>,
  alsoAccounts = false,
) {
  const qc = useQueryClient();
  const [keys] = useState(() => new IdempotencyKeys());
  return useMutation({
    mutationFn: (body: T) => send(keys.keyFor(body), body),
    onSuccess: async () => {
      keys.reset();
      await Promise.all([
        qc.invalidateQueries({ queryKey: cardsKey(householdId) }),
        ...(alsoAccounts
          ? [qc.invalidateQueries({ queryKey: accountsKey(householdId) })]
          : []),
      ]);
    },
  });
}

export function useCloseInvoice(householdId: string, invoiceId: string) {
  return useInvoiceMutation<Record<string, never>>(householdId, (key) =>
    call(
      api.POST("/api/v1/households/{householdId}/invoices/{invoiceId}/close", {
        params: {
          path: { householdId, invoiceId },
          header: { "Idempotency-Key": key },
        },
      }),
    ),
  );
}

/** Paying an invoice moves cash (the account balance changes), never consumption. */
export function usePayInvoice(householdId: string, invoiceId: string) {
  return useInvoiceMutation<PayInvoiceRequest>(
    householdId,
    (key, body) =>
      call(
        api.POST(
          "/api/v1/households/{householdId}/invoices/{invoiceId}/payments",
          {
            params: {
              path: { householdId, invoiceId },
              header: { "Idempotency-Key": key },
            },
            body,
          },
        ),
      ),
    true,
  );
}

export function useAdjustInvoice(householdId: string, invoiceId: string) {
  return useInvoiceMutation<AdjustInvoiceRequest>(householdId, (key, body) =>
    call(
      api.POST(
        "/api/v1/households/{householdId}/invoices/{invoiceId}/adjustments",
        {
          params: {
            path: { householdId, invoiceId },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    ),
  );
}
