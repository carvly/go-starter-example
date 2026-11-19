import { adjustStockRequestMeta } from "@/api/meta.gen";

// 字段的文案和约束来自 api/admin.yaml（src/api/meta.gen.ts），这里只补 spec 表达不了的规则。
export const adjustStockSchema = adjustStockRequestMeta.form.refine((v) => v.delta !== 0, {
  message: "增减量不能为 0",
  path: ["delta"],
});
