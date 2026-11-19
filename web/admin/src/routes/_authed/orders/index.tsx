import { createFileRoute } from "@tanstack/react-router";
import { ReceiptText } from "lucide-react";

import { listOrdersMeta } from "@/api/meta.gen";
import { page } from "@/features/auth/guard";
import { OrdersPage } from "@/features/orders/orders-page";

export const Route = createFileRoute("/_authed/orders/")({
  ...page({ permission: "order:read", nav: { title: "订单", icon: ReceiptText, group: "业务", order: 2 } }),
  // 筛选条件不合法时丢掉，不报错。
  validateSearch: listOrdersMeta.search,
  component: function Orders() {
    const filters = Route.useSearch();
    const navigate = Route.useNavigate();
    return <OrdersPage filters={filters} onChange={(f) => void navigate({ search: f, replace: true })} />;
  },
});
