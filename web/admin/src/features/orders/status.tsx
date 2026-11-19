import { orderStatusMeta } from "@/api/meta.gen";
import { EnumCell } from "@/components/data-table/cells";
import type { FilterOption } from "@/components/filters";

import type { OrderStatus } from "./queries";

// 取值和文案来自 api/admin.yaml 的 OrderStatus（x-enum-labels）；颜色是页面自己的选择。
export const statusOptions: FilterOption<OrderStatus>[] = orderStatusMeta.values.map((value) => ({
  value,
  label: orderStatusMeta.labels[value],
}));

const variants = { pending: "outline", confirmed: "secondary", failed: "destructive" } as const;

export function OrderStatusBadge({ status }: { status: OrderStatus }) {
  return <EnumCell value={status} labels={orderStatusMeta.labels} variants={variants} />;
}
