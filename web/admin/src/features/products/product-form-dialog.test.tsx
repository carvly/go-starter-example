import { QueryClient, QueryClientProvider, queryOptions } from "@tanstack/react-query";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/api/errors";

import { EditProductDialog } from "./product-form-dialog";
import type { Product } from "./queries";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const mutateAsync = vi.fn();
const latest: Product = { id: "p1", name: "别人改的名字", price_cents: 300, stock: 3, version: 2, created_at: "", updated_at: "" };
vi.mock("./queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./queries")>()),
  useUpdateProduct: () => ({ mutateAsync }),
  productQuery: (id: string) => queryOptions({ queryKey: ["products", "detail", id], queryFn: () => latest }),
}));

function type(input: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("EditProductDialog", () => {
  it("保存时数据已被别人修改：表单换成最新内容并说明，再保存时带上新的 version", async () => {
    mutateAsync.mockRejectedValueOnce(new ApiError(409, "product_conflict", "conflict")).mockResolvedValueOnce(latest);
    const unhandled = vi.fn();
    window.addEventListener("unhandledrejection", unhandled);

    const root = createRoot(document.body.appendChild(document.createElement("div")));
    const product: Product = { ...latest, name: "原来的名字", price_cents: 100, version: 1 };
    act(() =>
      root.render(
        <QueryClientProvider client={new QueryClient()}>
          <EditProductDialog product={product} />
        </QueryClientProvider>,
      ),
    );
    await act(async () => document.querySelector<HTMLButtonElement>("button")!.click());
    const name = document.querySelector<HTMLInputElement>('input[name="name"]')!;
    act(() => type(name, "我改的名字"));
    await act(async () => name.form!.requestSubmit());
    await act(() => new Promise((r) => setTimeout(r, 0)));

    expect(mutateAsync).toHaveBeenLastCalledWith({ id: "p1", name: "我改的名字", price_cents: 100, version: 1 });
    expect(name.value).toBe("别人改的名字");
    expect(document.querySelector<HTMLInputElement>('input[name="price_cents"]')!.value).toBe("3.00");
    expect(name.form!.textContent).toContain("被别人修改过");

    await act(async () => name.form!.requestSubmit());
    await act(() => new Promise((r) => setTimeout(r, 0)));
    expect(mutateAsync).toHaveBeenLastCalledWith({ id: "p1", name: "别人改的名字", price_cents: 300, version: 2 });
    expect(unhandled).not.toHaveBeenCalled();

    window.removeEventListener("unhandledrejection", unhandled);
    act(() => root.unmount());
  });
});
