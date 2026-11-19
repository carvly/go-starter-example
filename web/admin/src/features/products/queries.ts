import { keepPreviousData, queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";

import { api } from "@/api/client";
import { call } from "@/api/errors";
import { listProductsMeta } from "@/api/meta.gen";
import type { components } from "@/api/schema";
import { registerErrorMessages } from "@/lib/server-error";

registerErrorMessages({
  product_not_found: "商品不存在",
  product_conflict: "商品已被他人修改，请刷新后重试",
  invalid_name: "名称不能为空",
  invalid_price: "单价必须大于 0",
  invalid_stock: "库存必须在 0 到 10 亿之间",
  invalid_stock_delta: "调整数量不能为 0，且不超过 10 亿",
  insufficient_stock: "库存不足：调整后库存不能小于 0",
  stock_too_large: "调整后库存不能超过 10 亿",
});

export type Product = components["schemas"]["Product"];

export const productsQuery = (q: string | undefined, page: number) =>
  queryOptions({
    queryKey: ["products", "list", { q, page }],
    queryFn: () => call(api.GET("/products", { params: { query: { q, page, size: listProductsMeta.pageSize } } })),
    placeholderData: keepPreviousData,
  });

export const productQuery = (id: string) =>
  queryOptions({
    queryKey: ["products", "detail", id],
    queryFn: () => call(api.GET("/products/{id}", { params: { path: { id } } })),
  });

// 调库存时显示在"增减量"字段下的错误码。
export const STOCK_ERRORS = ["insufficient_stock", "stock_too_large", "invalid_stock_delta"] as const;

// 商品的写操作都让列表过期。
function useProductMutation<V, R>(fn: (v: V) => Promise<R>, handlesErrors?: readonly string[]) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["products"] }),
    meta: { handlesErrors },
  });
}

const path = (id: string) => ({ params: { path: { id } } });

export const useCreateProduct = () =>
  useProductMutation((body: components["schemas"]["CreateProductRequest"]) => call(api.POST("/products", { body })));
// product_conflict 由 FormDialog 显示在表单顶部并刷新表单。
export const useUpdateProduct = () =>
  useProductMutation(
    ({ id, ...body }: { id: string } & components["schemas"]["UpdateProductRequest"]) =>
      call(api.PUT("/products/{id}", { ...path(id), body })),
    ["product_conflict"],
  );
export const useAdjustStock = () =>
  useProductMutation(
    ({ id, delta }: { id: string; delta: number }) => call(api.POST("/products/{id}/stock-adjustments", { ...path(id), body: { delta } })),
    STOCK_ERRORS,
  );
