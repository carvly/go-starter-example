import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { useState } from "react";

import { ConfirmButton } from "@/components/confirm-button";
import { TitleWithTip } from "@/components/info-tip";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { toast } from "@/components/ui/toast";
import { useMe } from "@/lib/session";
import { RoleFlowGuide, rolesQuery } from "@/features/roles";
import { formatDateTime } from "@/lib/format";

import { AccountBadges } from "./badges";
import {
  accountQuery,
  useDisableAccount,
  useEnableAccount,
  useResetPassword,
  useRevokeSessions,
  useSetAccountRoles,
  useSetSuperAdmin,
} from "./queries";
import { type TempPassword, TempPasswordDialog } from "./temp-password-dialog";

export function AccountDetailPage({ id }: { id: string }) {
  const me = useMe();
  const { data: account } = useSuspenseQuery(accountQuery(id));
  const self = account.id === me.id;
  const disable = useDisableAccount();
  const enable = useEnableAccount();
  const reset = useResetPassword();
  const revoke = useRevokeSessions();
  const setSuper = useSetSuperAdmin();
  const [temp, setTemp] = useState<TempPassword>();
  const done = (title: string) => () => toast.add({ title });

  return (
    <div className="flex flex-col gap-4">
      <Link to="/accounts" className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft className="size-4" />
        员工
      </Link>
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            {account.display_name}
            <AccountBadges account={account} />
          </CardTitle>
          <CardDescription>
            {account.username} · 创建于 {formatDateTime(account.created_at)}
            {account.created_by ? "" : " · 由命令行创建"}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex flex-wrap gap-2">
            {account.status === "active" ? (
              <ConfirmButton
                title={`停用 ${account.username}？`}
                description="停用后对方立即退出登录、不能再登录，可以随时启用。"
                confirmText="停用"
                variant="destructive"
                disabled={self}
                onConfirm={() => disable.mutate(id, { onSuccess: done("已停用") })}
              >
                停用
              </ConfirmButton>
            ) : (
              <Button variant="outline" disabled={enable.isPending} onClick={() => enable.mutate(id, { onSuccess: done("已启用") })}>
                启用
              </Button>
            )}
            <ConfirmButton
              title={`重设 ${account.username} 的密码？`}
              description="会生成新的临时密码，对方的全部登录立即失效，下次登录后必须修改密码。"
              confirmText="重设"
              disabled={self}
              onConfirm={() =>
                reset.mutate(id, {
                  onSuccess: (r) =>
                    setTemp({ username: account.username, password: r.temporary_password, expiresAt: r.temporary_password_expires_at }),
                })
              }
            >
              重设密码
            </ConfirmButton>
            <ConfirmButton
              title={`让 ${account.username} 退出登录？`}
              description="吊销对方的全部会话，需要重新登录。"
              disabled={self}
              onConfirm={() => revoke.mutate(id, { onSuccess: done("已吊销全部会话") })}
            >
              踢下线
            </ConfirmButton>
            <ConfirmButton
              title={account.super_admin ? `取消 ${account.username} 的超级管理员？` : `把 ${account.username} 设为超级管理员？`}
              description={account.super_admin ? "取消后只保留角色授予的权限。" : "超级管理员可以执行所有操作，包括管理员工和角色。"}
              disabled={self && account.super_admin}
              onConfirm={() => setSuper.mutate({ id, superAdmin: !account.super_admin }, { onSuccess: done("已更新") })}
            >
              {account.super_admin ? "取消超级管理员" : "设为超级管理员"}
            </ConfirmButton>
          </div>
          {self && (
            <p className="mt-3 text-sm text-muted-foreground">
              这是你自己的账号：不能停用自己或取消自己的超级管理员；改密码、退出其它设备在“个人设置”里操作。
            </p>
          )}
        </CardContent>
      </Card>
      <RolesCard id={id} roleIds={account.role_ids} superAdmin={account.super_admin} />
      <TempPasswordDialog value={temp} onClose={() => setTemp(undefined)} />
    </div>
  );
}

function RolesCard({ id, roleIds, superAdmin }: { id: string; roleIds: string[]; superAdmin: boolean }) {
  const { data: roles } = useQuery(rolesQuery);
  const [selected, setSelected] = useState<string[]>(roleIds);
  const save = useSetAccountRoles();
  const dirty = [...selected].sort().join() !== [...roleIds].sort().join();

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <TitleWithTip tip={<RoleFlowGuide />}>角色</TitleWithTip>
        </CardTitle>
        <CardDescription>
          {superAdmin
            ? "超级管理员拥有全部功能，不受角色限制；取消超级管理员后，按这里的角色决定。"
            : "员工拥有所选角色的全部功能，可以选多个。"}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {roles?.length === 0 && (
          <div className="flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
            还没有任何角色，新建一个角色后回到这里分配。
            <Link to="/roles" className={buttonVariants({ variant: "outline", size: "sm" })}>
              去新建角色
            </Link>
          </div>
        )}
        {!superAdmin && roles && roles.length > 0 && selected.length === 0 && (
          <p className="text-sm text-destructive">还没有分配角色，他登录后只能看到首页。勾选下面的角色，点“保存角色”。</p>
        )}
        {roles?.map((role) => (
          <Label key={role.id} className="flex items-center gap-2 font-normal">
            <Checkbox
              checked={selected.includes(role.id)}
              onCheckedChange={(checked) => setSelected((s) => (checked ? [...s, role.id] : s.filter((x) => x !== role.id)))}
            />
            {role.name}
            {role.description && <span className="text-muted-foreground">— {role.description}</span>}
            {role.permissions.length === 0 && (
              <span className="text-muted-foreground">
                （还没有勾选权限，分到它暂时用不了任何功能；
                <Link to="/roles" className="underline underline-offset-4">
                  去勾选
                </Link>
                后立即生效）
              </span>
            )}
          </Label>
        ))}
        <div>
          <Button
            disabled={!dirty || save.isPending}
            onClick={() => save.mutate({ id, roleIds: selected }, { onSuccess: () => toast.add({ title: "已保存" }) })}
          >
            保存角色
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
