import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import type { z } from "zod";

import { auditLogMeta, type listAuditLogsMeta } from "@/api/meta.gen";
import { type DataTableColumn, DataTable, Pager, useCursorPager } from "@/components/data-table";
import { DateTimeCell, TextCell } from "@/components/data-table/cells";
import { DateRangeFilter, FilterBar, SelectFilter } from "@/components/filters";
import { accountsQuery } from "@/features/staff";
import { dayRange } from "@/lib/format";

import { describeAction } from "./describe";
import { type AuditLog, auditLogsQuery } from "./queries";

// 筛选条件放在 URL 里（日期是 yyyy-mm-dd），刷新或分享链接时保留。
export type AuditFilters = z.output<typeof listAuditLogsMeta.search>;

const { fields } = auditLogMeta;

export function AuditLogPage({ filters, onChange }: { filters: AuditFilters; onChange: (f: AuditFilters) => void }) {
  // 筛选条件变了回到第一页。
  const pager = useCursorPager(JSON.stringify([filters.staff_id, filters.from, filters.to]));

  const query = { staff_id: filters.staff_id, ...dayRange(filters.from, filters.to), cursor: pager.cursor };
  const { data, isLoading } = useQuery(auditLogsQuery(query));
  // 员工下拉和"员工"列的用户名都来自员工列表的前 100 个。
  const { data: accounts } = useQuery(accountsQuery(undefined, 100));
  const names = useMemo(() => new Map(accounts?.items.map((a) => [a.id, a.username])), [accounts]);
  const staffOptions = accounts?.items.map((a) => ({ value: a.id, label: a.username })) ?? [];

  const columns: DataTableColumn<AuditLog>[] = [
    { header: fields.created_at.title, cell: ({ row }) => <DateTimeCell value={row.original.created_at} /> },
    {
      header: fields.staff_id.title,
      cell: ({ row }) => {
        const id = row.original.staff_id;
        if (!id) return <TextCell value={undefined} />;
        return names.get(id) ?? <span className="font-mono text-xs">{id.slice(0, 8)}</span>;
      },
    },
    { header: fields.action.title, cell: ({ row }) => <span className="font-mono text-xs">{describeAction(row.original.action)}</span> },
    { header: fields.target.title, cell: ({ row }) => <span className="font-mono text-xs">{row.original.target || "—"}</span> },
    { header: fields.result.title, cell: ({ row }) => <TextCell value={row.original.result} /> },
    { header: fields.ip.title, cell: ({ row }) => <TextCell value={row.original.ip} /> },
  ];

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-lg font-medium">操作日志</h1>
      <FilterBar>
        <SelectFilter
          label={fields.staff_id.title}
          options={staffOptions}
          value={filters.staff_id}
          onChange={(staff_id) => onChange({ ...filters, staff_id })}
          allLabel="全部员工"
        />
        <DateRangeFilter
          id="audit-date"
          label="日期"
          value={{ from: filters.from, to: filters.to }}
          onChange={(r) => onChange({ ...filters, from: r.from, to: r.to })}
        />
      </FilterBar>
      <DataTable columns={columns} data={data?.items} loading={isLoading} getRowId={(e) => e.id} empty="没有符合条件的日志" />
      <Pager page={pager.page} hasPrev={pager.hasPrev} nextCursor={data?.next_cursor} onPrev={pager.prev} onNext={pager.next} />
    </div>
  );
}
