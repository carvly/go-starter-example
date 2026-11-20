import { useRouter } from "@tanstack/react-router";
import { useMemo } from "react";

import { useMe, usePermissions } from "@/lib/session";
import { type NavGroup, visibleNav } from "@/lib/permissions";

// useVisibleNav 是当前员工能进入的页面（按权限过滤、分组），侧边栏和首页的入口共用。
export function useVisibleNav(): NavGroup[] {
  const router = useRouter();
  const me = useMe();
  const permissions = usePermissions();
  return useMemo(() => visibleNav(Object.values(router.routesById), me, permissions), [router.routesById, me, permissions]);
}
