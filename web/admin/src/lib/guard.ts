import type { QueryClient } from "@tanstack/react-query";
import { redirect } from "@tanstack/react-router";

import { ApiError } from "@/api/errors";
import { type NavItem, type Permission, hasPermission } from "@/lib/permissions";

import { type Me, meQuery, permissionsQuery } from "@/lib/session";

// loadMe 在路由的 beforeLoad 里取当前员工；未登录时跳登录页，并带上回跳地址。
export async function loadMe(queryClient: QueryClient, href: string): Promise<Me> {
  try {
    return await queryClient.ensureQueryData(meQuery);
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      throw redirect({ to: "/login", search: { redirect: href } });
    }
    throw error;
  }
}

type GuardContext = { context: { queryClient: QueryClient } };

// page 生成页面路由的 staticData 和 beforeLoad：权限只写一处，侧边栏（staticData.nav）和
// 路由守卫（beforeLoad）都从这里取。页面挂在 _authed 下，登录和必须改密由它先检查，这里只
// 判断权限，规则只在 hasPermission 一处。前端校验只是体验，真正的拦截在后端。
export function page(meta: { permission?: Permission; nav?: NavItem }) {
  return {
    staticData: meta,
    beforeLoad: async ({ context }: GuardContext) => {
      const me = await context.queryClient.ensureQueryData(meQuery);
      const permissions = await context.queryClient.ensureQueryData(permissionsQuery);
      if (!hasPermission(me, permissions, meta.permission)) throw redirect({ to: "/forbidden" });
    },
  };
}
