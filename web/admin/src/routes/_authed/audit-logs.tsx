import { createFileRoute } from "@tanstack/react-router";
import { ScrollText } from "lucide-react";
import { listAuditLogsMeta } from "@/api/meta.gen";
import { AuditLogPage } from "@/features/audit-log/audit-log-page";
import { page } from "@/lib/guard";

export const Route = createFileRoute("/_authed/audit-logs")({
  ...page({ permission: "superadmin", nav: { title: "操作日志", icon: ScrollText, group: "系统", order: 3 } }),
  // 筛选条件不合法时丢掉，不报错。
  validateSearch: listAuditLogsMeta.search,
  component: function AuditLogs() {
    const filters = Route.useSearch();
    const navigate = Route.useNavigate();
    return <AuditLogPage filters={filters} onChange={(f) => void navigate({ search: f, replace: true })} />;
  },
});
