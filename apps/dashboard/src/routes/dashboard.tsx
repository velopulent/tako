import * as React from "react"
import { getRouteApi, useNavigate } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import {
  ActivityIcon,
  CircleAlertIcon,
  CpuIcon,
  HardDriveIcon,
  MemoryStickIcon,
  NetworkIcon,
} from "lucide-react"

import {
  api,
  type DashboardResponse,
  type LogEntry,
  type MetricSample,
  type ProcessInfo,
  type ServiceInfo,
} from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
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
import { HostInventory } from "@/components/host-inventory"
import { RefreshSelect } from "@/components/refresh-select"
import { Skeleton } from "@/components/ui/skeleton"
import { useMonitoringPreference } from "@/hooks/use-monitoring-preference"
import { usePreference } from "@/hooks/use-preference"
import { refreshIntervals, type RefreshInterval } from "@/lib/monitoring"

const bytes = (value: number) => {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"]
  let amount = value,
    index = 0
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024
    index++
  }
  return `${amount.toFixed(index ? 1 : 0)} ${units[index]}`
}
const duration = (seconds: number) => {
  const days = Math.floor(seconds / 86400),
    hours = Math.floor((seconds % 86400) / 3600),
    minutes = Math.floor((seconds % 3600) / 60)
  return `${days ? `${days}d ` : ""}${hours}h ${minutes}m`
}

export function DashboardPage() {
  const search = getRouteApi("/").useSearch()
  const navigate = useNavigate({ from: "/" })
  const defaultInterval =
    useMonitoringPreference().data?.defaultInterval ?? "1m"
  const [interval, setInterval] = usePreference<RefreshInterval>(
    "current",
    "interval:dashboard",
    defaultInterval
  )
  const milliseconds =
    refreshIntervals.find((item) => item.value === interval)?.milliseconds ||
    false
  const dashboard = useQuery({
    queryKey: ["dashboard"],
    queryFn: () => api<DashboardResponse>("/dashboard"),
    refetchInterval: milliseconds,
  })
  const history = useQuery({
    queryKey: ["metrics", "dashboard"],
    queryFn: () => api<{ samples: MetricSample[] }>("/metrics?range=1h"),
  })
  const processes = useQuery({
    queryKey: ["processes", "dashboard"],
    queryFn: () => api<{ items: ProcessInfo[] }>("/processes"),
    refetchInterval: milliseconds,
  })
  const services = useQuery({
    queryKey: ["services", "failed"],
    queryFn: () =>
      api<{ items: ServiceInfo[] }>("/services?scope=system&type=service"),
    refetchInterval: milliseconds,
  })
  const logs = useQuery({
    queryKey: ["logs", "critical"],
    queryFn: () => api<{ items: LogEntry[] }>("/logs?limit=100"),
  })
  if (dashboard.isPending)
    return (
      <main className="p-6">
        <Skeleton className="h-[70vh]" />
      </main>
    )
  if (dashboard.isError || !dashboard.data)
    return (
      <main className="p-6">
        <Alert variant="destructive">
          <AlertTitle>Could not load host inventory</AlertTitle>
          <AlertDescription>
            The host adapter did not return a dashboard snapshot.
          </AlertDescription>
        </Alert>
      </main>
    )
  const { host, metrics } = dashboard.data
  const memory = metrics.memoryTotal
    ? (metrics.memoryUsed / metrics.memoryTotal) * 100
    : 0
  const storage = dashboard.data.storage
  const failed =
    services.data?.items.filter((item) => item.activeState === "failed") ?? []
  const critical =
    logs.data?.items.filter((item) => Number(item.priority) <= 3).slice(0, 8) ??
    []
  const top = (processes.data?.items ?? []).slice(0, 10)
  const processColumns: ColumnDef<ProcessInfo>[] = [
    { accessorKey: "pid", header: "PID", meta: { align: "end" }, size: 88 },
    { accessorKey: "program", header: "Program" },
    { accessorKey: "user", header: "User" },
    {
      accessorKey: "memory",
      header: "Memory",
      meta: { align: "end" },
      cell: ({ row }) => bytes(row.original.memory),
    },
    {
      accessorKey: "cpuTime",
      header: "CPU time",
      meta: { align: "end" },
      cell: ({ row }) => `${row.original.cpuTime.toFixed(1)}s`,
    },
  ]
  return (
    <main className="@container/main flex flex-1 flex-col gap-6 p-4 lg:p-6">
      <section className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-3">
            <h2 className="text-2xl font-semibold tracking-tight">
              {host.hostname}
            </h2>
            <Badge variant="secondary">Online</Badge>
          </div>
          <p className="text-sm text-muted-foreground">
            {host.operatingSystem} · Linux {host.kernel} · {host.architecture} ·
            up {duration(host.uptimeSeconds)}
          </p>
        </div>
        <RefreshSelect value={interval} onChange={setInterval} />
      </section>
      {failed.length > 0 && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>
            {failed.length} failed service{failed.length === 1 ? "" : "s"}
          </AlertTitle>
          <AlertDescription>
            {failed
              .slice(0, 5)
              .map((item) => item.name)
              .join(", ")}
          </AlertDescription>
        </Alert>
      )}
      <section className="grid gap-4 sm:grid-cols-2 @5xl/main:grid-cols-4">
        <Summary
          title="CPU"
          value={`${metrics.cpuPercent.toFixed(1)}%`}
          detail={`Load ${metrics.load1.toFixed(2)}`}
          icon={CpuIcon}
        />
        <Summary
          title="Memory"
          value={`${memory.toFixed(1)}%`}
          detail={`${bytes(metrics.memoryUsed)} of ${bytes(metrics.memoryTotal)}`}
          icon={MemoryStickIcon}
        />
        <Summary
          title="Storage"
          value={storage ? `${storage.percent.toFixed(1)}%` : "—"}
          detail={
            storage
              ? `${bytes(storage.used)} of ${bytes(storage.total)} · ${storage.filesystems} filesystem${storage.filesystems === 1 ? "" : "s"}`
              : "Capacity summary unavailable"
          }
          icon={HardDriveIcon}
        />
        <Summary
          title="Network"
          value={bytes(metrics.networkRx)}
          detail={`${bytes(metrics.networkTx)} sent`}
          icon={NetworkIcon}
        />
      </section>
      <HostInventory host={host} />
      <MetricsCharts
        initialSamples={history.data?.samples ?? []}
        interval={interval}
        compact
      />
      <section className="grid gap-4 @5xl/main:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Top processes</CardTitle>
            <CardDescription>Highest resident memory usage.</CardDescription>
          </CardHeader>
          <CardContent>
            <DataTable
              data={top}
              columns={processColumns}
              height="22rem"
              search={search.q}
              onSearchChange={(q) =>
                navigate({
                  search: (previous) => ({ ...previous, q: q || undefined }),
                  replace: true,
                })
              }
            />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Recent critical logs</CardTitle>
            <CardDescription>Journal priority error or higher.</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            {critical.length ? (
              critical.map((entry, index) => (
                <div
                  key={`${entry.timestamp}-${index}`}
                  className="flex gap-3 border-b pb-3 last:border-0"
                >
                  <ActivityIcon />
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">
                      {entry.unit || "kernel"}
                    </p>
                    <p className="line-clamp-2 font-mono text-xs text-muted-foreground">
                      {entry.message}
                    </p>
                  </div>
                </div>
              ))
            ) : (
              <p className="text-sm text-muted-foreground">
                No recent critical entries.
              </p>
            )}
          </CardContent>
        </Card>
      </section>
    </main>
  )
}
function Summary({
  title,
  value,
  detail,
  icon: Icon,
}: {
  title: string
  value: string
  detail: string
  icon: React.ComponentType
}) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between gap-3">
          <CardDescription>{title}</CardDescription>
          <Icon />
        </div>
        <CardTitle className="text-2xl tabular-nums">{value}</CardTitle>
      </CardHeader>
      <CardContent>
        <p className="text-xs text-muted-foreground">{detail}</p>
      </CardContent>
    </Card>
  )
}
