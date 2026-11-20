import { createFileRoute } from "@tanstack/react-router";
import { ShieldCheck } from "lucide-react";

import { page } from "@/lib/guard";
import { RolesPage } from "@/features/roles/roles-page";

export const Route = createFileRoute("/_authed/roles")({
  ...page({ permission: "superadmin", nav: { title: "角色", icon: ShieldCheck, group: "系统", order: 2 } }),
  component: RolesPage,
});
