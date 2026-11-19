import { useQuery } from "@tanstack/react-query";
import type { z } from "zod";

import { listProductsMeta, productMeta } from "@/api/meta.gen";
import { type DataTableColumn, DataTable, PagePager } from "@/components/data-table";
import { DateTimeCell, MoneyCell, NumberCell, TextCell, actionsColumn } from "@/components/data-table/cells";
import { SearchInput } from "@/components/filters";
import { useCan } from "@/features/auth/queries";

import { AdjustStockDialog } from "./adjust-stock-dialog";
import { CreateProductDialog, EditProductDialog } from "./product-form-dialog";
import { type Product, productsQuery } from "./queries";

// 筛选条件和页码放在 URL 里，刷新或分享链接时保留。
export type ProductFilters = z.output<typeof listProductsMeta.search>;

export function ProductsPage({ filters, onChange }: { filters: ProductFilters; onChange: (f: ProductFilters) => void }) {
  const page = filters.page ?? 1;
  const { data, isLoading } = useQuery(productsQuery(filters.q, page));
  const canCreate = useCan("product:create");
  const canUpdate = useCan("product:update");
  const canAdjust = useCan("product:adjust_stock");

  const { fields } = productMeta;
  const columns: DataTableColumn<Product>[] = [
    { header: fields.name.title, cell: ({ row }) => <TextCell value={row.original.name} /> },
    { header: fields.price_cents.title, cell: ({ row }) => <MoneyCell cents={row.original.price_cents} /> },
    { header: fields.stock.title, cell: ({ row }) => <NumberCell value={row.original.stock} /> },
    { header: fields.updated_at.title, cell: ({ row }) => <DateTimeCell value={row.original.updated_at} /> },
  ];
  if (canUpdate || canAdjust) {
    columns.push(
      actionsColumn((p) => (
        <>
          {canUpdate && <EditProductDialog product={p} />}
          {canAdjust && <AdjustStockDialog product={p} />}
        </>
      )),
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-4">
        <h1 className="text-lg font-medium">商品</h1>
        {canCreate && <CreateProductDialog />}
      </div>
      {/* 换了关键字回到第一页：onChange 不带 page。 */}
      <SearchInput value={filters.q} placeholder="按名称搜索" onSearch={(q) => onChange({ q })} />
      <DataTable
        columns={columns}
        data={data?.items}
        loading={isLoading}
        getRowId={(p) => p.id}
        empty={filters.q ? "没有符合条件的商品" : "还没有商品"}
      />
      <PagePager
        page={page}
        size={listProductsMeta.pageSize}
        total={data?.total}
        totalCapped={data?.total_capped}
        onChange={(p) => onChange({ ...filters, page: p })}
      />
    </div>
  );
}
