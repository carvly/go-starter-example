import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Copy } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { toast } from "@/components/ui/toast";
import { rolesQuery } from "@/features/roles";
import { formatDateTime } from "@/lib/format";

// assignRoleTo 只在新建普通员工时传：他还没有角色，关掉对话框后引导去分配（没有角色时先去建）。
export type TempPassword = { username: string; password: string; expiresAt: string; assignRoleTo?: string };

// 临时密码只在这一次响应里出现，关掉对话框后就再也看不到了：点遮罩不关闭，只能点按钮或按 Esc。
export function TempPasswordDialog({ value, onClose }: { value: TempPassword | undefined; onClose: () => void }) {
  // 关闭动画期间 value 已经清空，沿用最后一次的内容，避免对话框先变空再消失。
  const [shown, setShown] = useState(value);
  if (value && value !== shown) setShown(value);
  const navigate = useNavigate();
  const { data: roles } = useQuery({ ...rolesQuery, enabled: !!shown?.assignRoleTo });
  const assignTo = shown?.assignRoleTo;
  return (
    <Dialog open={!!value} onOpenChange={(open) => !open && onClose()} disablePointerDismissal>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{shown?.username} 的临时密码</DialogTitle>
          <DialogDescription>
            只显示这一次，请通过安全的渠道交给本人。{formatDateTime(shown?.expiresAt)} 前有效，首次登录后必须修改。
          </DialogDescription>
        </DialogHeader>
        <code className="rounded-md bg-muted px-3 py-2 font-mono text-base select-all">{shown?.password}</code>
        {assignTo && (
          <p className="rounded-md border border-dashed p-3 text-sm text-muted-foreground">
            下一步：{shown?.username} 还没有角色，登录后只能看到首页。
            {roles?.length === 0 ? "目前还没有任何角色，记下密码后去新建一个，再到他的详情里分配。" : "记下密码后去给他分配角色。"}
          </p>
        )}
        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => {
              if (!value) return;
              navigator.clipboard.writeText(value.password).then(
                () => toast.add({ title: "已复制" }),
                () => toast.add({ title: "复制失败，请手动选中密码复制", type: "error" }),
              );
            }}
          >
            <Copy />
            复制
          </Button>
          {assignTo ? (
            <Button
              onClick={() => {
                onClose();
                void (roles?.length === 0 ? navigate({ to: "/roles" }) : navigate({ to: "/accounts/$id", params: { id: assignTo } }));
              }}
            >
              {roles?.length === 0 ? "我已记下，去新建角色" : "我已记下，去分配角色"}
            </Button>
          ) : (
            <Button onClick={onClose}>我已记下</Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
