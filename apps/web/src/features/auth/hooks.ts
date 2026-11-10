"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { ApiError, api, call, type User } from "@/lib/api/client";

export const meKey = ["me"] as const;

/** The logged-in user, or null when there is no valid session. */
export function useMe() {
  return useQuery({
    queryKey: meKey,
    queryFn: async (): Promise<User | null> => {
      try {
        return await call(api.GET("/api/v1/auth/me"));
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) return null;
        throw err;
      }
    },
  });
}

export type LoginInput = { email: string; password: string };
export type RegisterInput = {
  name: string;
  email: string;
  password: string;
  inviteToken?: string;
};

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: LoginInput) =>
      call(api.POST("/api/v1/auth/login", { body })),
    onSuccess: (user) => {
      qc.clear();
      qc.setQueryData(meKey, user);
    },
  });
}

export function useRegister() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: RegisterInput) =>
      call(api.POST("/api/v1/auth/register", { body })),
    onSuccess: (user) => {
      qc.clear();
      qc.setQueryData(meKey, user);
    },
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => call(api.POST("/api/v1/auth/logout")),
    onSuccess: () => {
      qc.clear();
      qc.setQueryData(meKey, null);
    },
  });
}
