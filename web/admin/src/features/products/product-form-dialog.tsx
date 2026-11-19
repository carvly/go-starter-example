import { useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";

import { createProductRequestMeta, updateProductRequestMeta } from "@/api/meta.gen";
import { useAppForm } from "@/components/form";
import { FormDialog, useDialog } from "@/components/overlay";
import { Button } from "@/components/ui/button";
import { toast } from "@/components/ui/toast";
import { centsToYuan } from "@/lib/money";

import { type Product, productQuery, useCreateProduct, useUpdateProduct } from "./queries";

const { fields: createFields, form: createSchema } = createProductRequestMeta;
const { fields: updateFields, form: updateSchema } = updateProductRequestMeta;

const blank = { name: "", price_cents: "", stock: "0" };

export function CreateProductDialog() {
  const dialog = useDialog();
  const create = useCreateProduct();
  const form = useAppForm({
    defaultValues: blank,
    validators: { onSubmit: createSchema },
    onSubmit: async ({ value }) => {
      await create.mutateAsync(createSchema.parse(value));
      toast.add({ title: "已创建" });
      dialog.close();
    },
  });
  return (
    <FormDialog
      {...dialog}
      form={form}
      onOpen={() => form.reset(blank)}
      trigger={
        <Button>
          <Plus />
          新建商品
        </Button>
      }
      title="新建商品"
      submitText="创建"
    >
      <form.AppField name="name">{(f) => <f.TextField label={createFields.name.title} autoComplete="off" autoFocus />}</form.AppField>
      <form.AppField name="price_cents">
        {(f) => <f.MoneyField label={createFields.price_cents.title} placeholder="199.00" />}
      </form.AppField>
      <form.AppField name="stock">{(f) => <f.NumberField label={createFields.stock.title} />}</form.AppField>
    </FormDialog>
  );
}

// version 是打开表单（或冲突后刷新）时读到的版本，原样提交。
const editValues = (p: Product) => ({ name: p.name, price_cents: centsToYuan(p.price_cents), version: p.version });

export function EditProductDialog({ product }: { product: Product }) {
  const dialog = useDialog();
  const update = useUpdateProduct();
  const queryClient = useQueryClient();
  const form = useAppForm({
    defaultValues: editValues(product),
    validators: { onSubmit: updateSchema },
    onSubmit: async ({ value }) => {
      await update.mutateAsync({ id: product.id, ...updateSchema.parse(value) });
      toast.add({ title: "已保存" });
      dialog.close();
    },
  });
  return (
    <FormDialog
      {...dialog}
      form={form}
      onOpen={() => form.reset(editValues(product))}
      onConflict={async () => {
        const latest = await queryClient.fetchQuery({ ...productQuery(product.id), staleTime: 0 });
        form.reset(editValues(latest));
        void queryClient.invalidateQueries({ queryKey: ["products", "list"] });
      }}
      trigger={
        <Button variant="outline" size="sm">
          修改
        </Button>
      }
      title="修改商品"
      description="改价只影响之后的订单，已下的订单保留下单时的名称和单价。"
      submitText="保存"
    >
      <form.AppField name="name">{(f) => <f.TextField label={updateFields.name.title} autoComplete="off" />}</form.AppField>
      <form.AppField name="price_cents">{(f) => <f.MoneyField label={updateFields.price_cents.title} />}</form.AppField>
    </FormDialog>
  );
}
