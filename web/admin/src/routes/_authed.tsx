import { createFileRoute, redirect } from "@tanstack/react-router";

import { AppLayout } from "@/components/layout/app-layout";
import { loadMe } from "@/lib/guard";
import { permissionsQuery } from "@/lib/session";

// 需要登录的页面都挂在这个布局下：未登录跳登录页；必须改密时只能去改密页。
export const Route = createFileRoute("/_authed")({
  beforeLoad: async ({ context, location }) => {
    const me = await loadMe(context.queryClient, location.href);
    if (me.must_change_password) throw redirect({ to: "/change-password", search: { redirect: location.href } });
    await context.queryClient.ensureQueryData(permissionsQuery);
  },
  component: AppLayout,
});
