import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/api/errors";

import { AdjustStockDialog } from "./adjust-stock-dialog";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const mutateAsync = vi.fn();
vi.mock("./queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./queries")>()),
  useAdjustStock: () => ({ mutateAsync }),
}));

function type(input: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("AdjustStockDialog", () => {
  // vitest 遇到未捕获的 rejection 会让整个测试运行失败；这里再显式监听一次，失败原因更清楚。
  it("库存不足时错误显示在增减量字段下，不变成未捕获的异常", async () => {
    mutateAsync.mockRejectedValueOnce(new ApiError(409, "insufficient_stock", "insufficient stock"));
    const unhandled = vi.fn();
    window.addEventListener("unhandledrejection", unhandled);

    const root = createRoot(document.body.appendChild(document.createElement("div")));
    const product = { id: "p1", name: "键盘", price_cents: 100, stock: 3, version: 1, created_at: "", updated_at: "" };
    act(() => root.render(<AdjustStockDialog product={product} />));
    await act(async () => document.querySelector<HTMLButtonElement>("button")!.click());
    const input = document.querySelector<HTMLInputElement>('input[name="delta"]')!;
    act(() => type(input, "-5"));
    await act(async () => input.form!.requestSubmit());
    await act(() => new Promise((r) => setTimeout(r, 0)));

    expect(input.getAttribute("aria-invalid")).toBe("true");
    expect(input.closest("[data-slot=field]")?.textContent).toContain("库存不足");
    expect(unhandled).not.toHaveBeenCalled();

    window.removeEventListener("unhandledrejection", unhandled);
    act(() => root.unmount());
  });
});
