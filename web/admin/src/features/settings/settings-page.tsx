import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";

import { sessionMeta } from "@/api/meta.gen";
import { ConfirmButton } from "@/components/confirm-button";
import { type DataTableColumn, DataTable } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useMe } from "@/lib/session";
import { formatDateTime } from "@/lib/format";
import { describeUserAgent } from "@/lib/user-agent";

import { type Session, mySessionsQuery, useRevokeMySession } from "./queries";

const { fields } = sessionMeta;

// 列表是登录历史，也包括已经失效的会话：不认识的登录即使已经失效，也值得看到。
// 状态由服务端判断（空闲超时的配置只有服务端知道）。
function SessionStatus({ session }: { session: Session }) {
  if (session.current) return <Badge>{fields.current.title}</Badge>;
  switch (session.status) {
    case "active":
      return <Badge variant="outline">{fields.status.labels.active}</Badge>;
    case "revoked":
      return (
        <span className="text-muted-foreground">
          {fields.status.labels.revoked} · {formatDateTime(session.revoked_at)}
        </span>
      );
    case "expired":
      return <span className="text-muted-foreground">{fields.status.labels.expired}</span>;
  }
}

function SessionAction({ session }: { session: Session }) {
  const revoke = useRevokeMySession();
  if (session.current || session.status !== "active") return null;
  return (
    <ConfirmButton title="退出这个设备上的登录？" confirmText="退出" size="sm" onConfirm={() => revoke.mutate(session.id)}>
      退出登录
    </ConfirmButton>
  );
}

const columns: DataTableColumn<Session>[] = [
  { header: fields.created_at.title, cell: ({ row }) => formatDateTime(row.original.created_at) },
  { header: fields.last_used_at.title, cell: ({ row }) => formatDateTime(row.original.last_used_at) },
  { header: fields.ip.title, accessorKey: "ip" },
  {
    header: fields.user_agent.title,
    cell: ({ row }) => (
      <span className="line-clamp-1 max-w-64" title={row.original.user_agent}>
        {describeUserAgent(row.original.user_agent)}
      </span>
    ),
  },
  { header: fields.status.title, cell: ({ row }) => <SessionStatus session={row.original} /> },
  { id: "action", header: "", cell: ({ row }) => <SessionAction session={row.original} /> },
];

export function SettingsPage() {
  const me = useMe();
  const { data, isLoading } = useQuery(mySessionsQuery);
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-lg font-medium">个人设置</h1>
      <Card>
        <CardHeader>
          <CardTitle>账号</CardTitle>
          <CardDescription>
            {me.display_name}（{me.username}）{me.super_admin && " · 超级管理员"}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Link to="/change-password" search={{ redirect: "/settings" }} className={`${buttonVariants({ variant: "outline" })} self-start`}>
            修改密码
          </Link>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>登录设备</CardTitle>
          <CardDescription>
            最近 50 次登录，包括已经退出和过期的。修改密码会退出其它设备上的登录。看到不认识的登录，请修改密码。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <DataTable columns={columns} data={data} loading={isLoading} getRowId={(s) => s.id} />
        </CardContent>
      </Card>
    </div>
  );
}
