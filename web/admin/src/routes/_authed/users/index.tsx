import { createFileRoute } from "@tanstack/react-router";
import { UserRound as NavIcon } from "lucide-react";

import { listUsersMeta } from "@/api/meta.gen";
import { page } from "@/lib/guard";
import { UsersPage } from "@/features/users/list-page";

export const Route = createFileRoute("/_authed/users/")({
  ...page({ permission: "user:read", nav: { title: "用户", icon: NavIcon, group: "业务", order: 3 } }),
  // 筛选条件不合法时丢掉，不报错。
  validateSearch: listUsersMeta.search,
  component: function Users() {
    const filters = Route.useSearch();
    const navigate = Route.useNavigate();
    return <UsersPage filters={filters} onChange={(f) => void navigate({ search: f, replace: true })} />;
  },
});
