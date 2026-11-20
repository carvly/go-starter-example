import { createFileRoute } from "@tanstack/react-router";
import { House } from "lucide-react";

import { page } from "@/lib/guard";
import { HomePage } from "@/features/home/home-page";

export const Route = createFileRoute("/_authed/")({
  ...page({ nav: { title: "首页", icon: House, order: -1 } }),
  component: HomePage,
});
