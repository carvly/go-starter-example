import { describe, expect, it } from "vitest";

import { createProductRequestMeta } from "@/api/meta.gen";

import { adjustStockSchema } from "./schemas";

describe("createProductRequestMeta.form", () => {
  it("parse 的结果就是请求体：单价按元输入、转成分，库存转成整数", () => {
    expect(createProductRequestMeta.form.parse({ name: " 键盘 ", price_cents: "199.9", stock: "10" })).toEqual({
      name: "键盘",
      price_cents: 19990,
      stock: 10,
    });
  });

  it("拒绝不符合 spec 约束的输入", () => {
    for (const v of [
      { name: "", price_cents: "1", stock: "1" },
      { name: "p", price_cents: "0", stock: "1" },
      { name: "p", price_cents: "1.234", stock: "1" },
      { name: "p", price_cents: "1", stock: "-1" },
      { name: "p", price_cents: "1", stock: "1.5" },
      { name: "p", price_cents: "1", stock: "1000000001" },
      { name: "p".repeat(201), price_cents: "1", stock: "1" },
    ]) {
      expect(createProductRequestMeta.form.safeParse(v).success).toBe(false);
    }
  });
});

describe("adjustStockSchema", () => {
  it("接受带符号的整数，拒绝 0", () => {
    expect(adjustStockSchema.parse({ delta: "+50" })).toEqual({ delta: 50 });
    expect(adjustStockSchema.parse({ delta: "-3" })).toEqual({ delta: -3 });
    expect(adjustStockSchema.safeParse({ delta: "0" }).error?.issues[0]?.message).toBe("增减量不能为 0");
    expect(adjustStockSchema.safeParse({ delta: "abc" }).success).toBe(false);
  });
});
