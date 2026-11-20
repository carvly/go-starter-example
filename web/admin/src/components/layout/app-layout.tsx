import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { ChevronsUpDown, LayoutDashboard, LogOut, Settings } from "lucide-react";
import { Suspense } from "react";

import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { useLogout, useMe } from "@/lib/session";
import { isNavActive } from "@/lib/permissions";

import { useVisibleNav } from "./use-visible-nav";

function NavMenu() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const groups = useVisibleNav();

  return groups.map((group) => (
    <SidebarGroup key={group.title}>
      <SidebarGroupLabel>{group.title}</SidebarGroupLabel>
      <SidebarMenu>
        {group.items.map((item) => (
          <SidebarMenuItem key={item.to}>
            <SidebarMenuButton isActive={isNavActive(pathname, item.to)} tooltip={item.title} render={<Link to={item.to} />}>
              <item.icon />
              <span>{item.title}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        ))}
      </SidebarMenu>
    </SidebarGroup>
  ));
}

function UserMenu() {
  const me = useMe();
  const logout = useLogout();
  const navigate = useNavigate();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<SidebarMenuButton size="lg" />}>
        <Avatar className="size-8 rounded-lg">
          <AvatarFallback className="rounded-lg">{me.display_name.slice(0, 1)}</AvatarFallback>
        </Avatar>
        <div className="grid flex-1 text-left text-sm leading-tight">
          <span className="truncate font-medium">{me.display_name}</span>
          <span className="truncate text-xs text-muted-foreground">{me.super_admin ? "超级管理员" : me.username}</span>
        </div>
        <ChevronsUpDown className="ml-auto" />
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="start" className="min-w-56">
        <DropdownMenuGroup>
          <DropdownMenuLabel>{me.username}</DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => navigate({ to: "/settings" })}>
          <Settings />
          个人设置
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => logout.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) })}>
          <LogOut />
          退出登录
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function AppLayout() {
  return (
    <SidebarProvider>
      <Sidebar collapsible="icon">
        <SidebarHeader>
          {/* 收起成图标栏时只留图标，否则标题会被挤成竖排。 */}
          <div className="flex h-8 items-center gap-2 px-2 text-sm font-semibold group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0">
            <LayoutDashboard className="size-4 shrink-0" />
            <span className="truncate group-data-[collapsible=icon]:hidden">管理后台</span>
          </div>
        </SidebarHeader>
        <SidebarContent>
          <NavMenu />
        </SidebarContent>
        <SidebarFooter>
          <SidebarMenu>
            <SidebarMenuItem>
              <UserMenu />
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarFooter>
      </Sidebar>
      {/* min-w-0：flex 子元素默认不会比内容窄，宽表格会把整页撑出横向滚动条（右侧的列被截掉）；
          允许收窄后，宽表格在自己的容器里横向滚动。 */}
      <SidebarInset className="min-w-0">
        <header className="flex h-12 items-center gap-2 border-b px-4">
          <SidebarTrigger />
        </header>
        <div className="flex-1 p-6">
          <Suspense fallback={<Skeleton className="h-32 w-full" />}>
            <Outlet />
          </Suspense>
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}
