import { createFileRoute } from "@tanstack/react-router";

import { page } from "@/lib/guard";
import { SettingsPage } from "@/features/settings/settings-page";

// 不在侧边栏：从右下角的用户菜单进入。
export const Route = createFileRoute("/_authed/settings")({
  ...page({}),
  component: SettingsPage,
});
