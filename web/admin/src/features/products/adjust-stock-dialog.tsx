import { adjustStockRequestMeta } from "@/api/meta.gen";
import { useAppForm } from "@/components/form";
import { FormDialog, useDialog } from "@/components/overlay";
import { Button } from "@/components/ui/button";
import { toast } from "@/components/ui/toast";
import { fieldError } from "@/lib/server-error";

import { type Product, STOCK_ERRORS, useAdjustStock } from "./queries";
import { adjustStockSchema } from "./schemas";

// 库存只能按增减量调整：直接"设为某个值"会覆盖同时发生的下单扣减。
export function AdjustStockDialog({ product }: { product: Product }) {
  const dialog = useDialog();
  const adjust = useAdjustStock();
  const form = useAppForm({
    defaultValues: { delta: "" },
    validators: { onSubmit: adjustStockSchema },
    onSubmit: async ({ value, formApi }) => {
      const { delta } = adjustStockSchema.parse(value);
      try {
        const res = await adjust.mutateAsync({ id: product.id, delta });
        toast.add({ title: `库存已调整为 ${res.stock}` });
        dialog.close();
      } catch (e) {
        const message = fieldError(e, STOCK_ERRORS);
        if (!message) throw e;
        formApi.setFieldMeta("delta", (m) => ({ ...m, errorMap: { ...m.errorMap, onServer: message } }));
      }
    },
  });
  return (
    <FormDialog
      {...dialog}
      form={form}
      onOpen={() => form.reset({ delta: "" })}
      trigger={
        <Button variant="outline" size="sm">
          调库存
        </Button>
      }
      title={`调整库存：${product.name}`}
      description={`当前库存 ${product.stock}。进货填正数（如 50），盘亏填负数（如 -3）。按增减量调整，调整时正好有人下单也不会算错。`}
      submitText="调整"
    >
      <form.AppField name="delta">
        {(f) => <f.NumberField label={adjustStockRequestMeta.fields.delta.title} autoFocus placeholder="50 或 -3" />}
      </form.AppField>
    </FormDialog>
  );
}
