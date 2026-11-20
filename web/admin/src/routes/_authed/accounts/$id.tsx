import { createFileRoute } from "@tanstack/react-router";

import { page } from "@/lib/guard";
import { AccountDetailPage } from "@/features/staff/account-detail-page";
import { accountQuery } from "@/features/staff/queries";

export const Route = createFileRoute("/_authed/accounts/$id")({
  ...page({ permission: "superadmin" }),
  loader: ({ context, params }) => context.queryClient.ensureQueryData(accountQuery(params.id)),
  component: function AccountDetail() {
    const { id } = Route.useParams();
    // key 让切换员工时角色勾选状态重新初始化。
    return <AccountDetailPage key={id} id={id} />;
  },
});
