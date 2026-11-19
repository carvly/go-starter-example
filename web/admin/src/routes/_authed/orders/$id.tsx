import { createFileRoute } from "@tanstack/react-router";

import { page } from "@/features/auth/guard";
import { OrderDetailPage } from "@/features/orders/order-detail-page";
import { orderQuery } from "@/features/orders/queries";

export const Route = createFileRoute("/_authed/orders/$id")({
  ...page({ permission: "order:read" }),
  loader: ({ context, params }) => context.queryClient.ensureQueryData(orderQuery(params.id)),
  component: function OrderDetail() {
    const { id } = Route.useParams();
    return <OrderDetailPage id={id} />;
  },
});
