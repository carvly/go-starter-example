import { useQuery } from "@tanstack/react-query";
import { Plus } from "lucide-react";

import { roleMeta } from "@/api/meta.gen";
import { ConfirmButton } from "@/components/confirm-button";
import { type DataTableColumn, DataTable } from "@/components/data-table";
import { TitleWithTip } from "@/components/info-tip";
import { Badge } from "@/components/ui/badge";
import { RoleFlowGuide } from "./role-guide";
import { formatDateTime } from "@/lib/format";

import { PermissionsDialog } from "./permissions-dialog";
import { type Role, rolesQuery, useDeleteRole } from "./queries";
import { RoleFormDialog } from "./role-form-dialog";

function RoleActions({ role }: { role: Role }) {
  const remove = useDeleteRole();
  return (
    <div className="flex justify-end gap-2">
      <PermissionsDialog role={role} />
      <RoleFormDialog role={role} variant="outline" size="sm">
        修改
      </RoleFormDialog>
      <ConfirmButton
        title={`删除角色 ${role.name}？`}
        description="已分配这个角色的员工会立即失去它带来的功能。"
        confirmText="删除"
        variant="destructive"
        size="sm"
        onConfirm={() => remove.mutate(role.id)}
      >
        删除
      </ConfirmButton>
    </div>
  );
}

const { fields } = roleMeta;

const columns: DataTableColumn<Role>[] = [
  { header: fields.name.title, accessorKey: "name" },
  { header: fields.description.title, cell: ({ row }) => row.original.description || "—" },
  {
    id: "permissions",
    header: () => (
      <TitleWithTip tip="这个角色能做的事，点这一行的“权限”勾选。标着“未设置权限”的角色，分到它的员工暂时用不了任何功能；勾选后对已分配的员工立即生效。">
        权限
      </TitleWithTip>
    ),
    cell: ({ row }) =>
      row.original.permissions.length === 0 ? <Badge variant="destructive">未设置权限</Badge> : `${row.original.permissions.length} 项`,
  },
  { header: fields.created_at.title, cell: ({ row }) => formatDateTime(row.original.created_at) },
  { id: "actions", header: "", cell: ({ row }) => <RoleActions role={row.original} /> },
];

export function RolesPage() {
  const { data, isLoading } = useQuery(rolesQuery);
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h1 className="text-lg font-medium">
            <TitleWithTip tip={<RoleFlowGuide />}>角色</TitleWithTip>
          </h1>
          <p className="text-sm text-muted-foreground">
            角色决定普通员工能用哪些功能。超级管理员拥有全部功能，不需要角色（在员工详情里设为超级管理员）。
          </p>
        </div>
        <RoleFormDialog>
          <Plus />
          新建角色
        </RoleFormDialog>
      </div>
      <DataTable
        columns={columns}
        data={data}
        loading={isLoading}
        getRowId={(r) => r.id}
        empty="还没有角色。给普通员工分配权限前，先新建一个角色。"
      />
    </div>
  );
}
