import { useMutation, useQueryClient } from "@tanstack/react-query";

import { api } from "@/api/client";
import { call } from "@/api/errors";
import type { components } from "@/api/schema";
import { meQuery } from "@/lib/session";

export function useLogin() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: components["schemas"]["StaffLoginRequest"]) => call(api.POST("/auth/login", { body })),
    // 登录换了身份：丢掉上一个身份的全部缓存，再放入新的 me。
    onSuccess: (me) => {
      queryClient.clear();
      queryClient.setQueryData(meQuery.queryKey, me);
    },
    meta: { handlesErrors: true },
  });
}

export function useChangePassword() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: components["schemas"]["ChangePasswordRequest"]) => call(api.PUT("/me/password", { body })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: meQuery.queryKey }),
    meta: { handlesErrors: true },
  });
}
