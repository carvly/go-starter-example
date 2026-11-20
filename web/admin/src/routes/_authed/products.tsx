import { createFileRoute } from "@tanstack/react-router";
import { Package } from "lucide-react";

import { listProductsMeta } from "@/api/meta.gen";
import { page } from "@/lib/guard";
import { ProductsPage } from "@/features/products/products-page";

export const Route = createFileRoute("/_authed/products")({
  ...page({ permission: "product:read", nav: { title: "商品", icon: Package, group: "业务", order: 1 } }),
  // 筛选条件不合法时丢掉，不报错。
  validateSearch: listProductsMeta.search,
  component: function Products() {
    const filters = Route.useSearch();
    const navigate = Route.useNavigate();
    return <ProductsPage filters={filters} onChange={(f) => void navigate({ search: f, replace: true })} />;
  },
});
