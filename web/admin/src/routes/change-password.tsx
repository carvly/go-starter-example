import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { ChangePasswordPage } from "@/features/auth/change-password-page";
import { loadMe } from "@/lib/guard";

export const Route = createFileRoute("/change-password")({
  validateSearch: z.object({ redirect: z.string().optional() }),
  beforeLoad: ({ context, location }) => loadMe(context.queryClient, location.href),
  component: function ChangePassword() {
    const { redirect } = Route.useSearch();
    return <ChangePasswordPage redirect={redirect} />;
  },
});
