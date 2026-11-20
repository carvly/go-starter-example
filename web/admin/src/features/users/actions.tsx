import { ConfirmButton } from "@/components/confirm-button";
import { toast } from "@/components/ui/toast";
import { useCan } from "@/lib/session";

import { type User, useDisableUser, useEnableUser } from "./queries";

// 详情页上的操作按钮：按权限显示；有前置条件的动作只在记录满足条件时显示。
export function UserActions({ user: v }: { user: User }) {
  const canDisable = useCan("user:disable");
  const disableAction = useDisableUser();
  const canEnable = useCan("user:enable");
  const enableAction = useEnableUser();
  return (
    <>
      {canDisable && v.status === "active" && (
        <ConfirmButton
          title={`停用用户：${v.display_name}？`}
          description="停用会吊销对方的全部会话，可以随时启用。"
          confirmText="停用"
          disabled={disableAction.isPending}
          onConfirm={() => disableAction.mutate(v.id, { onSuccess: () => toast.add({ title: "已停用" }) })}
        >
          停用
        </ConfirmButton>
      )}
      {canEnable && v.status === "disabled" && (
        <ConfirmButton
          title={`启用用户：${v.display_name}？`}
          confirmText="启用"
          disabled={enableAction.isPending}
          onConfirm={() => enableAction.mutate(v.id, { onSuccess: () => toast.add({ title: "已启用" }) })}
        >
          启用
        </ConfirmButton>
      )}
    </>
  );
}
