import { queryOptions, useMutation, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";

import { api } from "@/api/client";
import { call } from "@/api/errors";
import type { components } from "@/api/schema";
import { type Permission, hasPermission } from "@/lib/permissions";

// 后台外壳的会话契约：布局、路由守卫和各 Feature 都从这里读当前员工和权限码，不深入某个 Feature。

export type Me = components["schemas"]["Me"];

// 当前员工和权限码是全局状态的唯一来源：不另设状态库，都从这两个 query 读。
export const meQuery = queryOptions({
  queryKey: ["me"],
  queryFn: () => call(api.GET("/me")),
  staleTime: 60_000,
});

export const permissionsQuery = queryOptions({
  queryKey: ["me", "permissions"],
  queryFn: async () => (await call(api.GET("/me/permissions"))).permissions,
  staleTime: 60_000,
});

export function useMe(): Me {
  return useSuspenseQuery(meQuery).data;
}

export function usePermissions(): string[] {
  return useSuspenseQuery(permissionsQuery).data;
}

// useCan 判断当前员工有没有某个权限，用于显隐按钮；规则同路由守卫（hasPermission），
// 真正的拦截在后端。
export function useCan(permission: Permission): boolean {
  return hasPermission(useMe(), usePermissions(), permission);
}

export function useLogout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => call(api.POST("/auth/logout")),
    // 只在服务端确认吊销后才清缓存：请求失败时会话仍然有效，不能让界面看起来已经退出
    // （共用电脑上下一个人能直接进来）。失败由统一错误处理提示。
    onSuccess: () => queryClient.clear(),
  });
}
