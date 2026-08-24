import * as React from "react"
import { getRouteApi, useNavigate } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import { createFileRoute } from "@tanstack/react-router"

import { api, type MetricSample, type ProcessInfo } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { DataTable } from "@/components/data-table"
import { MetricsCharts } from "@/components/metrics-chart"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { metricsSearch, type MetricRange, metricRanges } from "@/lib/search"
import { Page, State, usePageInterval, bytes } from "@/lib/page"

const processColumns: ColumnDef<ProcessInfo>[] = [
  { accessorKey: "pid", header: "PID", meta: { align: "end" }, size: 88 },
  { accessorKey: "program", header: "Program" },
  { accessorKey: "user", header: "User" },
  {
    accessorKey: "state",
    header: "State",
    cell: ({ row }) => <Badge variant="outline">{row.original.state}</Badge>,
  },
  { accessorKey: "threads", header: "Threads", meta: { align: "end" }, size: 96 },
  {
    accessorKey: "cpuPercent",
    header: "CPU",
    meta: { align: "end" },
    cell: ({ row }) => `${(row.original.cpuPercent ?? 0).toFixed(1)}%`,
  },
  {
    accessorKey: "cpuTime",
    header: "CPU time",
    meta: { align: "end" },
    cell: ({ row }) => `${row.original.cpuTime.toFixed(1)}s`,
  },
  {
    accessorKey: "memory",
    header: "Memory",
    meta: { align: "end" },
    cell: ({ row }) => bytes(row.original.memory),
  },
  {
    accessorKey: "virtualMemory",
    header: "Virtual",
    meta: { align: "end" },
    cell: ({ row }) => bytes(row.original.virtualMemory),
  },
  {
    accessorKey: "diskRead",
    header: "Disk read",
    meta: { align: "end" },
    cell: ({ row }) => bytes(row.original.diskRead),
  },
  {
    accessorKey: "diskWrite",
    header: "Disk write",
    meta: { align: "end" },
    cell: ({ row }) => bytes(row.original.diskWrite),
  },
  {
    accessorKey: "diskReadRate",
    header: "Read/s",
    meta: { align: "end" },
    cell: ({ row }) => bytes(row.original.diskReadRate ?? 0),
  },
  {
    accessorKey: "diskWriteRate",
    header: "Write/s",
    meta: { align: "end" },
    cell: ({ row }) => bytes(row.original.diskWriteRate ?? 0),
  },
  {
    accessorKey: "command",
    header: "Command",
    size: 360,
    cell: ({ row }) => (
      <span
        className="block max-w-xl truncate font-mono text-xs"
        title={row.original.command}
      >
        {row.original.command}
      </span>
    ),
  },
]

function ProcessesTable({
  interval,
  search,
  onSearchChange,
}: {
  interval: ReturnType<typeof usePageInterval>
  search?: string
  onSearchChange?: (value: string) => void
}) {
  const navigate = useNavigate({ from: "/metrics" })
  const previous = React.useRef<{
    at: number
    items: Map<string, ProcessInfo>
  } | null>(null)
  const query = useQuery({
    queryKey: ["processes"],
    queryFn: async () => {
      const response = await api<{ items: ProcessInfo[] }>("/processes")
      const now = performance.now()
      const before = previous.current
      const seconds = before ? Math.max(0.001, (now - before.at) / 1000) : 1
      const items = response.items.map((item) => {
        const old = before?.items.get(`${item.pid}:${item.started}`)
        return {
          ...item,
          cpuPercent: old
            ? (Math.max(0, item.cpuTime - old.cpuTime) / seconds) * 100
            : 0,
          diskReadRate: old
            ? Math.max(0, item.diskRead - old.diskRead) / seconds
            : 0,
          diskWriteRate: old
            ? Math.max(0, item.diskWrite - old.diskWrite) / seconds
            : 0,
        }
      })
      previous.current = {
        at: now,
        items: new Map(
          response.items.map((item) => [`${item.pid}:${item.started}`, item])
        ),
      }
      return { items }
    },
    refetchInterval: interval.milliseconds,
  })
  const items = query.data?.items ?? []
  return (
    <State query={query} empty={!items.length}>
      <Card>
        <CardHeader>
          <CardTitle>Processes</CardTitle>
          <CardDescription>
            {items.length} processes · per-process network requires optional eBPF
            collector
          </CardDescription>
        </CardHeader>
        <CardContent>
          <DataTable
            data={items}
            columns={processColumns}
            searchPlaceholder="Search PID, program, command, or user"
            search={search}
            onSearchChange={onSearchChange}
            onRowClick={(row) =>
              navigate({
                to: "/processes/$pid",
                params: { pid: String(row.pid) },
                search: { started: String(row.started) },
              })
            }
            initialVisibility={{
              virtualMemory: false,
              diskRead: false,
              diskWrite: false,
              diskReadRate: false,
              diskWriteRate: false,
            }}
          />
        </CardContent>
      </Card>
    </State>
  )
}

function MetricsPage() {
  const interval = usePageInterval("metrics")
  const search = getRouteApi("/metrics").useSearch()
  const navigate = useNavigate({ from: "/metrics" })
  const query = useQuery({
    queryKey: ["metrics", search.range ?? "1h"],
    queryFn: () =>
      api<{ samples: MetricSample[] }>(`/metrics?range=${search.range ?? "1h"}`),
  })
  return (
    <Page
      description="Live CPU, memory, storage, network, and process telemetry."
      interval={interval}
    >
      <Tabs
        value={search.range ?? "1h"}
        onValueChange={(range) =>
          navigate({
            search: (previous) => ({
              ...previous,
              range: range === "1h" ? undefined : (range as MetricRange),
            }),
            replace: true,
          })
        }
      >
        <TabsList>
          {metricRanges.map((value) => (
            <TabsTrigger key={value} value={value}>
              {value}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <MetricsCharts
        key={`${search.range ?? "1h"}:${interval.value}`}
        range={search.range ?? "1h"}
        initialSamples={query.data?.samples ?? []}
        interval={interval.value}
      />
      <ProcessesTable
        interval={interval}
        search={search.q}
        onSearchChange={(q) =>
          navigate({
            search: (previous) => ({ ...previous, q: q || undefined }),
            replace: true,
          })
        }
      />
    </Page>
  )
}

export const Route = createFileRoute("/metrics")({
  validateSearch: metricsSearch,
  component: MetricsPage,
})
