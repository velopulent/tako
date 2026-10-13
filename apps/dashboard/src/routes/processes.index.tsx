import { useQuery } from "@tanstack/react-query"
import {
  createFileRoute,
  getRouteApi,
  useNavigate,
} from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import * as React from "react"

import { DataTable, type DataTableFeatures } from "@/components/data-table"
import { Badge } from "@/components/ui/badge"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { api, type ProcessInfo } from "@/lib/api"
import { formatBytes, formatPercent, formatRate } from "@/lib/format"
import { Page, State, usePageInterval } from "@/lib/page"
import { processStateName } from "@/lib/process"
import { parseEnum, parseString } from "@/lib/search"
import { useSession } from "@/lib/session"

const columns: ColumnDef<DataTableFeatures, ProcessInfo>[] = [
  {
    accessorKey: "program",
    header: "Process",
    size: 260,
    cell: ({ row }) => (
      <span className="flex min-w-0 flex-col">
        <span className="truncate font-medium">{row.original.program}</span>
        <span
          className="truncate font-mono text-xs text-muted-foreground"
          title={row.original.command}
        >
          {row.original.command || "—"}
        </span>
      </span>
    ),
  },
  { accessorKey: "pid", header: "PID", meta: { align: "end" }, size: 88 },
  { accessorKey: "user", header: "User", size: 120 },
  {
    accessorKey: "cpuPercent",
    header: "CPU",
    meta: { align: "end" },
    size: 96,
    cell: ({ row }) => formatPercent(row.original.cpuPercent ?? 0),
  },
  {
    accessorKey: "memory",
    header: "Memory",
    meta: { align: "end" },
    size: 104,
    cell: ({ row }) => formatBytes(row.original.memory),
  },
  {
    accessorKey: "state",
    header: "State",
    size: 140,
    cell: ({ row }) => (
      <Badge
        variant={
          row.original.state === "Z"
            ? "destructive"
            : row.original.state === "R"
              ? "secondary"
              : "outline"
        }
      >
        {processStateName(row.original.state)}
      </Badge>
    ),
  },
  {
    accessorKey: "threads",
    header: "Threads",
    meta: { align: "end" },
    size: 96,
  },
  {
    accessorKey: "diskReadRate",
    header: "Disk read",
    meta: { align: "end" },
    size: 112,
    cell: ({ row }) =>
      row.original.ioDenied ? "—" : formatRate(row.original.diskReadRate ?? 0),
  },
  {
    accessorKey: "diskWriteRate",
    header: "Disk write",
    meta: { align: "end" },
    size: 112,
    cell: ({ row }) =>
      row.original.ioDenied ? "—" : formatRate(row.original.diskWriteRate ?? 0),
  },
  {
    accessorKey: "cpuTime",
    header: "CPU time",
    meta: { align: "end" },
    size: 104,
    cell: ({ row }) => `${row.original.cpuTime.toFixed(1)} s`,
  },
]

const owners = ["all", "mine"] as const

function ProcessesPage() {
  const interval = usePageInterval("processes")
  const search = getRouteApi("/processes/").useSearch()
  const navigate = useNavigate({ from: "/processes/" })
  const session = useSession()
  const owner = search.owner ?? "all"
  const query = useQuery({
    queryKey: ["processes"],
    queryFn: () => api<{ items: ProcessInfo[] }>("/processes"),
    refetchInterval: interval.milliseconds
      ? Math.max(interval.milliseconds, 2_000)
      : false,
  })
  const all = query.data?.items ?? []
  const items = React.useMemo(
    () =>
      owner === "mine"
        ? all.filter((item) => item.user === session.user.username)
        : all,
    [all, owner, session.user.username]
  )
  const running = all.filter((item) => item.state === "R").length
  return (
    <Page
      description={
        query.data
          ? `${all.length.toLocaleString()} processes · ${running} running`
          : "Every process on the host, sampled live."
      }
      interval={interval}
    >
      <State query={query} empty={false}>
        <DataTable
          data={items}
          columns={columns}
          height="calc(100svh - 14rem)"
          initialSorting={[{ id: "cpuPercent", desc: true }]}
          searchPlaceholder="Search processes, commands, or users"
          search={search.q}
          onSearchChange={(q) =>
            navigate({
              search: (previous) => ({ ...previous, q: q || undefined }),
              replace: true,
            })
          }
          initialVisibility={{
            diskReadRate: false,
            diskWriteRate: false,
            cpuTime: false,
            threads: false,
          }}
          toolbar={
            <ToggleGroup
              value={[owner]}
              onValueChange={(value) => {
                const next = value[0] as (typeof owners)[number] | undefined
                if (next)
                  navigate({
                    search: (previous) => ({
                      ...previous,
                      owner: next === "all" ? undefined : next,
                    }),
                    replace: true,
                  })
              }}
              variant="outline"
              size="sm"
            >
              <ToggleGroupItem value="all">All</ToggleGroupItem>
              <ToggleGroupItem value="mine">Mine</ToggleGroupItem>
            </ToggleGroup>
          }
          onRowClick={(row) =>
            navigate({
              to: "/processes/$pid",
              params: { pid: String(row.pid) },
              search: { started: String(row.started) },
            })
          }
          emptyMessage="No processes match this filter."
        />
      </State>
    </Page>
  )
}

export const Route = createFileRoute("/processes/")({
  validateSearch: (
    search: Record<string, unknown>
  ): { q?: string; owner?: "mine" } => {
    const owner = parseEnum(search.owner, owners, "all")
    const q = parseString(search.q)
    return { q: q || undefined, owner: owner === "mine" ? owner : undefined }
  },
  component: ProcessesPage,
})
