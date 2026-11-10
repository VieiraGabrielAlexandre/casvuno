"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, call, type CategoryKind } from "@/lib/api/client";

const categoriesKey = (householdId: string) =>
  ["households", householdId, "categories"] as const;

export function useCategories(householdId: string, includeArchived: boolean) {
  return useQuery({
    queryKey: [...categoriesKey(householdId), { includeArchived }],
    queryFn: async () =>
      (
        await call(
          api.GET("/api/v1/households/{householdId}/categories", {
            params: {
              path: { householdId },
              query: includeArchived ? { includeArchived: "true" } : {},
            },
          }),
        )
      ).items,
    enabled: householdId !== "",
  });
}

export type NewCategory = {
  name: string;
  kind: CategoryKind;
  parentId?: string;
};

export function useCreateCategory(householdId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: NewCategory) =>
      call(
        api.POST("/api/v1/households/{householdId}/categories", {
          params: { path: { householdId } },
          body,
        }),
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: categoriesKey(householdId) }),
  });
}

/** Archives (isActive=false) or restores (isActive=true) a category. */
export function useSetCategoryActive(householdId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, isActive }: { id: string; isActive: boolean }) =>
      call(
        api.PATCH("/api/v1/households/{householdId}/categories/{categoryId}", {
          params: { path: { householdId, categoryId: id } },
          body: { isActive },
        }),
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: categoriesKey(householdId) }),
  });
}
