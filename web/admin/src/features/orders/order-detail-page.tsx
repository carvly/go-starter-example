import { useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronLeft } from "lucide-react";

import { orderMeta } from "@/api/meta.gen";
import { DateTimeCell, MoneyCell, NumberCell } from "@/components/data-table/cells";
import { DescriptionItem, DescriptionList } from "@/components/overlay";
import { buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

import { orderQuery } from "./queries";
import { OrderStatusBadge } from "./status";

export function OrderDetailPage({ id }: { id: string }) {
  const { data: order } = useSuspenseQuery(orderQuery(id));
  const { fields } = orderMeta;
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        <Link to="/orders" className={buttonVariants({ variant: "ghost", size: "icon-sm" })} aria-label="返回订单列表">
          <ChevronLeft />
        </Link>
        <h1 className="text-lg font-medium">订单详情</h1>
      </div>
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <span className="font-mono text-sm">{order.id}</span>
            <OrderStatusBadge status={order.status} />
          </CardTitle>
        </CardHeader>
        <CardContent>
          <DescriptionList>
            <DescriptionItem label={fields.customer_name.title}>{order.customer_name}</DescriptionItem>
            <DescriptionItem label={fields.product_name.title}>
              {order.product_name}
              <span className="ml-2 font-mono text-xs text-muted-foreground">{order.product_id}</span>
            </DescriptionItem>
            <DescriptionItem label={fields.unit_price_cents.title}>
              <MoneyCell cents={order.unit_price_cents} />
            </DescriptionItem>
            <DescriptionItem label={fields.quantity.title}>
              <NumberCell value={order.quantity} />
            </DescriptionItem>
            <DescriptionItem label={fields.total_cents.title}>
              <MoneyCell cents={order.total_cents} />
            </DescriptionItem>
            <DescriptionItem label={fields.created_at.title}>
              <DateTimeCell value={order.created_at} />
            </DescriptionItem>
            <DescriptionItem label={fields.updated_at.title}>
              <DateTimeCell value={order.updated_at} />
            </DescriptionItem>
          </DescriptionList>
          <p className="mt-3 text-xs text-muted-foreground">商品名称和单价是下单时的快照，不随商品后来的改名、改价变化。</p>
        </CardContent>
      </Card>
    </div>
  );
}
