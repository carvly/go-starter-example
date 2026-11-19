import { keepPreviousData, queryOptions } from "@tanstack/react-query";

import { api } from "@/api/client";
import { call } from "@/api/errors";
import { listOrdersMeta } from "@/api/meta.gen";
import type { components } from "@/api/schema";
import { registerErrorMessages } from "@/lib/server-error";

registerErrorMessages({
  order_not_found: "订单不存在",
  invalid_status: "订单状态不正确",
});

export type Order = components["schemas"]["Order"];
export type OrderStatus = components["schemas"]["OrderStatus"];

export const ordersQuery = (query: { status?: OrderStatus[]; from?: string; to?: string; page: number }) =>
  queryOptions({
    queryKey: ["orders", "list", query],
    queryFn: () => call(api.GET("/orders", { params: { query: { ...query, size: listOrdersMeta.pageSize } } })),
    placeholderData: keepPreviousData,
  });

export const orderQuery = (id: string) =>
  queryOptions({
    queryKey: ["orders", "detail", id],
    queryFn: () => call(api.GET("/orders/{id}", { params: { path: { id } } })),
  });
