import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";

import { ApiError } from "@/api/errors";
import { changePasswordRequestMeta } from "@/api/meta.gen";
import { FormError, onFormSubmit, useAppForm } from "@/components/form";
import { Button } from "@/components/ui/button";
import { errorMessage } from "@/lib/server-error";
import { useLogout, useMe } from "@/lib/session";

import { AuthCard } from "./auth-card";
import { useChangePassword } from "./queries";
import { changePasswordSchema, safeRedirect } from "./schemas";

const { fields } = changePasswordRequestMeta;

export function ChangePasswordPage({ redirect }: { redirect?: string }) {
  const me = useMe();
  const change = useChangePassword();
  const logout = useLogout();
  const navigate = useNavigate();
  const [error, setError] = useState<string>();
  const currentLabel = me.must_change_password ? "临时密码" : fields.current_password.title;
  const form = useAppForm({
    defaultValues: { current_password: "", new_password: "", confirm_password: "" },
    validators: { onSubmit: changePasswordSchema },
    onSubmit: async ({ value }) => {
      setError(undefined);
      try {
        await change.mutateAsync({ current_password: value.current_password, new_password: value.new_password });
        await navigate({ href: safeRedirect(redirect) });
      } catch (e) {
        if (e instanceof ApiError && e.code === "wrong_password") {
          form.setFieldMeta("current_password", (m) => ({ ...m, errorMap: { ...m.errorMap, onServer: `${currentLabel}不正确` } }));
        } else {
          setError(errorMessage(e));
        }
      }
    },
  });

  return (
    <AuthCard
      title="修改密码"
      description={me.must_change_password ? "首次登录或密码被重设后，需要先设置自己的密码。" : "修改后，其它设备上的登录会失效。"}
    >
      <form className="flex flex-col gap-4" onSubmit={onFormSubmit(form)}>
        <FormError message={error} />
        <form.AppField name="current_password">
          {(f) => <f.PasswordField label={currentLabel} autoComplete="current-password" autoFocus />}
        </form.AppField>
        <form.AppField name="new_password">
          {(f) => (
            <f.PasswordField
              label={fields.new_password.title}
              description={`至少 ${fields.new_password.min} 个字符`}
              autoComplete="new-password"
            />
          )}
        </form.AppField>
        <form.AppField name="confirm_password">
          {(f) => <f.PasswordField label="再次输入新密码" autoComplete="new-password" />}
        </form.AppField>
        <form.AppForm>
          <form.SubmitButton>保存</form.SubmitButton>
        </form.AppForm>
        <Button type="button" variant="ghost" onClick={() => logout.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) })}>
          退出登录
        </Button>
      </form>
    </AuthCard>
  );
}
