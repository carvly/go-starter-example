import { useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";

import { identityProviderMeta, userMeta, userSessionClientMeta, userStatusMeta } from "@/api/meta.gen";
import { DateTimeCell, EnumCell, TextCell } from "@/components/data-table/cells";
import { ConfirmButton } from "@/components/confirm-button";
import { DescriptionItem, DescriptionList } from "@/components/overlay";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { toast } from "@/components/ui/toast";
import { useCan } from "@/lib/session";
import { formatDateTime } from "@/lib/format";

import { UserActions } from "./actions";
import { useKickUserSession, userIdentitiesQuery, userQuery, userSessionsQuery } from "./queries";

const { fields } = userMeta;

export function UserDetailPage({ id }: { id: string }) {
  const { data: v } = useSuspenseQuery(userQuery(id));
  const { data: identities } = useSuspenseQuery(userIdentitiesQuery(id));
  const { data: sessions } = useSuspenseQuery(userSessionsQuery(id));
  const canKick = useCan("user:kick");
  const kick = useKickUserSession(id);
  return (
    <div className="flex flex-col gap-4">
      <Link to="/users" className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft className="size-4" />
        用户
      </Link>
      <Card>
        <CardHeader>
          <CardTitle>{v.display_name}</CardTitle>
          <CardDescription>
            创建于 {formatDateTime(v.created_at)} · 更新于 {formatDateTime(v.updated_at)}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-wrap gap-2">
            <UserActions user={v} />
          </div>
          <DescriptionList>
            <DescriptionItem label={fields.display_name.title}>
              <TextCell value={v.display_name} />
            </DescriptionItem>
            <DescriptionItem label={fields.avatar_url.title}>
              <TextCell value={v.avatar_url} />
            </DescriptionItem>
            <DescriptionItem label={fields.status.title}>
              <EnumCell value={v.status} labels={userStatusMeta.labels} />
            </DescriptionItem>
            <DescriptionItem label={fields.created_at.title}>
              <DateTimeCell value={v.created_at} />
            </DescriptionItem>
            <DescriptionItem label={fields.updated_at.title}>
              <DateTimeCell value={v.updated_at} />
            </DescriptionItem>
          </DescriptionList>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>登录方式</CardTitle>
          <CardDescription>由登录创建，不能在后台增删。</CardDescription>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>登录方式</TableHead>
                <TableHead>第三方唯一 ID</TableHead>
                <TableHead>最近登录</TableHead>
                <TableHead>绑定时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {identities.items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell>
                    <EnumCell value={item.provider} labels={identityProviderMeta.labels} />
                  </TableCell>
                  <TableCell className="font-mono text-xs">{item.subject}</TableCell>
                  <TableCell>{item.last_login_at ? formatDateTime(item.last_login_at) : "—"}</TableCell>
                  <TableCell>{formatDateTime(item.created_at)}</TableCell>
                </TableRow>
              ))}
              {identities.items.length === 0 && (
                <TableRow>
                  <TableCell colSpan={4} className="text-muted-foreground">
                    没有登录方式
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>登录会话</CardTitle>
          <CardDescription>踢下线后对方的 access token 立即失效。</CardDescription>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>客户端</TableHead>
                <TableHead>设备名</TableHead>
                <TableHead>登录 IP</TableHead>
                <TableHead>登录时间</TableHead>
                <TableHead>最近使用</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {sessions.items.map((s) => (
                <TableRow key={s.id}>
                  <TableCell>
                    <EnumCell value={s.client} labels={userSessionClientMeta.labels} />
                  </TableCell>
                  <TableCell>{s.device_name || "—"}</TableCell>
                  <TableCell className="font-mono text-xs">{s.ip}</TableCell>
                  <TableCell>{formatDateTime(s.created_at)}</TableCell>
                  <TableCell>{formatDateTime(s.last_used_at)}</TableCell>
                  <TableCell className="text-right">
                    {canKick && (
                      <ConfirmButton
                        title="踢下线？"
                        description="该设备的登录立即失效，需要重新登录。"
                        confirmText="踢下线"
                        size="sm"
                        variant="outline"
                        disabled={kick.isPending}
                        onConfirm={() => kick.mutate(s.id, { onSuccess: () => toast.add({ title: "已踢下线" }) })}
                      >
                        踢下线
                      </ConfirmButton>
                    )}
                  </TableCell>
                </TableRow>
              ))}
              {sessions.items.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} className="text-muted-foreground">
                    没有活跃会话
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
