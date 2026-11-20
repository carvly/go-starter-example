import { Link } from "@tanstack/react-router";
import { KeyRound } from "lucide-react";

import { useVisibleNav } from "@/components/layout/use-visible-nav";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useMe } from "@/lib/session";
import { RoleFlowGuide } from "@/features/roles";

// 首页：能进入的页面做成入口（和侧边栏同一份数据）。一个都没有时说明原因和怎么办：
// 没有角色、或角色还没勾选权限的员工登录后只能看到这里。
export function HomePage() {
  const me = useMe();
  const groups = useVisibleNav()
    .map((g) => ({ ...g, items: g.items.filter((item) => item.to !== "/") }))
    .filter((g) => g.items.length > 0);

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-lg font-medium">你好，{me.display_name}</h1>
      {groups.length === 0 && (
        <Alert>
          <KeyRound />
          <AlertTitle>你还没有可以使用的功能</AlertTitle>
          <AlertDescription>
            你的账号还没有分配角色，或者分配的角色还没有勾选权限。请联系超级管理员处理，处理后刷新页面即可看到对应的菜单。
          </AlertDescription>
        </Alert>
      )}
      {groups.map((group) => (
        <section key={group.title} className="flex flex-col gap-2">
          <h2 className="text-sm text-muted-foreground">{group.title}</h2>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(10rem,1fr))] gap-3">
            {group.items.map((item) => (
              <Link
                key={item.to}
                to={item.to}
                className="flex items-center gap-2 rounded-lg border bg-card p-4 text-sm font-medium hover:bg-muted"
              >
                <item.icon className="size-4 text-muted-foreground" />
                {item.title}
              </Link>
            ))}
          </div>
        </section>
      ))}
      {me.super_admin && (
        <Card>
          <CardHeader>
            <CardTitle>让员工用上后台</CardTitle>
            <CardDescription>员工能用哪些功能由角色决定。</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-2 text-sm">
            <RoleFlowGuide />
          </CardContent>
        </Card>
      )}
    </div>
  );
}
