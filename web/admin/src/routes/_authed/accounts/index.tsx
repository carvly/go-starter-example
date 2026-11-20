import { createFileRoute } from "@tanstack/react-router";
import { Users } from "lucide-react";

import { page } from "@/lib/guard";
import { AccountsPage } from "@/features/staff/accounts-page";

export const Route = createFileRoute("/_authed/accounts/")({
  ...page({ permission: "superadmin", nav: { title: "员工", icon: Users, group: "系统", order: 1 } }),
  component: AccountsPage,
});
