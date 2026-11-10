"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, call } from "@/lib/api/client";

export const householdsKey = ["households"] as const;
const householdKey = (id: string) => ["households", id] as const;

export function useHouseholds(enabled = true) {
  return useQuery({
    queryKey: householdsKey,
    queryFn: async () => (await call(api.GET("/api/v1/households"))).items,
    enabled,
  });
}

export function useHousehold(id: string) {
  return useQuery({
    queryKey: householdKey(id),
    queryFn: () =>
      call(
        api.GET("/api/v1/households/{householdId}", {
          params: { path: { householdId: id } },
        }),
      ),
    enabled: id !== "",
  });
}

export function useMembers(id: string) {
  return useQuery({
    queryKey: [...householdKey(id), "members"],
    queryFn: async () =>
      (
        await call(
          api.GET("/api/v1/households/{householdId}/members", {
            params: { path: { householdId: id } },
          }),
        )
      ).items,
    enabled: id !== "",
  });
}

export function useInvites(id: string, enabled: boolean) {
  return useQuery({
    queryKey: [...householdKey(id), "invites"],
    queryFn: async () =>
      (
        await call(
          api.GET("/api/v1/households/{householdId}/invites", {
            params: { path: { householdId: id } },
          }),
        )
      ).items,
    enabled: enabled && id !== "",
  });
}

export function useCreateHousehold() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string }) =>
      call(api.POST("/api/v1/households", { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: householdsKey }),
  });
}

export function useCreateInvite(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () =>
      call(
        api.POST("/api/v1/households/{householdId}/invites", {
          params: { path: { householdId: id } },
          body: {},
        }),
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: [...householdKey(id), "invites"] }),
  });
}

export function useRevokeInvite(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (inviteId: string) =>
      call(
        api.DELETE("/api/v1/households/{householdId}/invites/{inviteId}", {
          params: { path: { householdId: id, inviteId } },
        }),
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: [...householdKey(id), "invites"] }),
  });
}

export function useDeactivateMember(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (memberId: string) =>
      call(
        api.DELETE("/api/v1/households/{householdId}/members/{memberId}", {
          params: { path: { householdId: id, memberId } },
        }),
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: [...householdKey(id), "members"] }),
  });
}

export function useAcceptInvite() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (token: string) =>
      call(api.POST("/api/v1/invites/accept", { body: { token } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: householdsKey }),
  });
}
