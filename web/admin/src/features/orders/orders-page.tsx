import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { z } from "zod";

import { listOrdersMeta, orderMeta } from "@/api/meta.gen";
import { type DataTableColumn, DataTable, PagePager } from "@/components/data-table";
import { DateTimeCell, MoneyCell, NumberCell, TextCell } from "@/components/data-table/cells";
import { DateRangeFilter, EnumFilter, FilterBar } from "@/components/filters";
import { dayRange } from "@/lib/format";

import { type Order, ordersQuery } from "./queries";
import { OrderStatusBadge, statusOptions } from "./status";

// 筛选条件和页码放在 URL 里（日期是 yyyy-mm-dd），刷新或分享链接时保留。
export type OrderFilters = z.output<typeof listOrdersMeta.search>;

const { fields } = orderMeta;

const columns: DataTableColumn<Order>[] = [
  { header: fields.created_at.title, cell: ({ row }) => <DateTimeCell value={row.original.created_at} /> },
  {
    header: fields.id.title,
    cell: ({ row }) => (
      <Link to="/orders/$id" params={{ id: row.original.id }} className="font-mono text-xs underline-offset-4 hover:underline">
        {row.original.id.slice(0, 8)}
      </Link>
    ),
  },
  { header: fields.customer_name.title, cell: ({ row }) => <TextCell value={row.original.customer_name} /> },
  { header: fields.product_name.title, cell: ({ row }) => <TextCell value={row.original.product_name} /> },
  { header: fields.quantity.title, cell: ({ row }) => <NumberCell value={row.original.quantity} /> },
  { header: fields.total_cents.title, cell: ({ row }) => <MoneyCell cents={row.original.total_cents} /> },
  { header: fields.status.title, cell: ({ row }) => <OrderStatusBadge status={row.original.status} /> },
];

export function OrdersPage({ filters, onChange }: { filters: OrderFilters; onChange: (f: OrderFilters) => void }) {
  const page = filters.page ?? 1;
  const { data, isLoading } = useQuery(ordersQuery({ status: filters.status, ...dayRange(filters.from, filters.to), page }));

  // 筛选条件变了回到第一页（page 不带上）。
  const filter = (next: Omit<OrderFilters, "page">) => onChange({ status: filters.status, from: filters.from, to: filters.to, ...next });

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-lg font-medium">订单</h1>
      <FilterBar>
        <EnumFilter
          label={fields.status.title}
          options={statusOptions}
          value={filters.status}
          onChange={(status) => filter({ status })}
          allLabel="全部状态"
        />
        <DateRangeFilter
          id="order-date"
          label="下单日期"
          value={{ from: filters.from, to: filters.to }}
          onChange={(r) => filter({ from: r.from, to: r.to })}
        />
      </FilterBar>
      <DataTable columns={columns} data={data?.items} loading={isLoading} getRowId={(o) => o.id} empty="没有符合条件的订单" />
      <PagePager
        page={page}
        size={listOrdersMeta.pageSize}
        total={data?.total}
        totalCapped={data?.total_capped}
        onChange={(p) => onChange({ ...filters, page: p })}
      />
    </div>
  );
}
