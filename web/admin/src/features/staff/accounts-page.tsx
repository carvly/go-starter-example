import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { TriangleAlert, UserRoundX } from "lucide-react";

import { accountMeta } from "@/api/meta.gen";
import { type DataTableColumn, DataTable, Pager, useCursorPager } from "@/components/data-table";
import { TitleWithTip } from "@/components/info-tip";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { type Role, RoleFlowGuide, rolesQuery } from "@/features/roles";
import { formatDateTime } from "@/lib/format";

import { AccountBadges } from "./badges";
import { CreateAccountDialog } from "./create-account-dialog";
import { type Account, accountsQuery, needsRole } from "./queries";

const { fields } = accountMeta;

function AccountRoles({ account, roles }: { account: Account; roles: Map<string, Role> | undefined }) {
  if (account.super_admin) return <span className="text-muted-foreground">全部权限</span>;
  if (account.role_ids.length === 0) {
    if (account.status === "disabled") return <span className="text-muted-foreground">—</span>;
    return (
      <Link to="/accounts/$id" params={{ id: account.id }}>
        <Badge variant="destructive">未分配角色</Badge>
      </Link>
    );
  }
  return (
    <span className="flex flex-wrap gap-1">
      {account.role_ids.map((id) => {
        const role = roles?.get(id);
        // 角色还没勾选权限时标出来：员工分到了角色，却仍然用不了任何功能。
        return role?.permissions.length === 0 ? (
          <Badge key={id} variant="outline" title="这个角色还没有勾选权限">
            {role.name}（未设置权限）
          </Badge>
        ) : (
          <Badge key={id} variant="secondary">
            {role?.name ?? "…"}
          </Badge>
        );
      })}
    </span>
  );
}

export function AccountsPage() {
  const pager = useCursorPager();
  const { data, isLoading } = useQuery(accountsQuery(pager.cursor));
  // active_super_admins 只在第一页返回；翻页时沿用第一页的值。
  const firstPage = useQuery({ ...accountsQuery(), enabled: pager.page > 1 });
  const supers = (pager.page === 1 ? data : firstPage.data)?.active_super_admins;
  const { data: roleList } = useQuery(rolesQuery);
  const roles = roleList && new Map(roleList.map((r) => [r.id, r]));
  const withoutRole = data?.items.filter(needsRole) ?? [];

  const columns: DataTableColumn<Account>[] = [
    {
      header: fields.username.title,
      cell: ({ row }) => (
        <Link to="/accounts/$id" params={{ id: row.original.id }} className="font-medium underline-offset-4 hover:underline">
          {row.original.username}
        </Link>
      ),
    },
    { header: fields.display_name.title, accessorKey: "display_name" },
    { header: fields.status.title, cell: ({ row }) => <AccountBadges account={row.original} /> },
    {
      id: "roles",
      header: () => (
        <TitleWithTip tip="员工能用哪些功能，由这里的角色决定；有多个角色时，拥有这些角色的全部功能。点员工名进入详情修改。超级管理员拥有全部功能，不需要角色。">
          角色
        </TitleWithTip>
      ),
      cell: ({ row }) => <AccountRoles account={row.original} roles={roles} />,
    },
    { header: fields.created_at.title, cell: ({ row }) => formatDateTime(row.original.created_at) },
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-medium">
          <TitleWithTip tip={<RoleFlowGuide />}>员工</TitleWithTip>
        </h1>
        <CreateAccountDialog />
      </div>
      {supers === 1 && (
        <Alert>
          <TriangleAlert />
          <AlertTitle>只剩一个启用的超级管理员</AlertTitle>
          <AlertDescription>建议再指定一个超级管理员：唯一的超级管理员离职或忘记密码时，只能由运维用命令行恢复。</AlertDescription>
        </Alert>
      )}
      {withoutRole.length > 0 && (
        <Alert>
          <UserRoundX />
          <AlertTitle>本页有 {withoutRole.length} 个员工还没有分配角色</AlertTitle>
          <AlertDescription>
            他们登录后只能看到首页。点他那一行的“未分配角色”，在详情里勾选角色并保存
            {roleList?.length === 0 && (
              <>
                ；目前还没有任何角色，可以先到
                <Link to="/roles" className="mx-1 underline underline-offset-4">
                  角色
                </Link>
                页新建一个
              </>
            )}
            。
          </AlertDescription>
        </Alert>
      )}
      <DataTable columns={columns} data={data?.items} loading={isLoading} getRowId={(a) => a.id} />
      <Pager page={pager.page} hasPrev={pager.hasPrev} nextCursor={data?.next_cursor} onPrev={pager.prev} onNext={pager.next} />
    </div>
  );
}
