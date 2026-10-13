import { useQuery } from "@tanstack/react-query"
import { createFileRoute, Link } from "@tanstack/react-router"
import {
  ArrowRightIcon,
  CircleAlertIcon,
  CpuIcon,
  HardDriveIcon,
  MemoryStickIcon,
  NetworkIcon,
  RotateCcwIcon,
} from "lucide-react"
import type * as React from "react"

import {
  MetricsCharts,
  toChartSamples,
  useLiveSamples,
} from "@/components/metrics-chart"
import { RefreshSelect } from "@/components/refresh-select"
import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import {
  api,
  type DashboardResponse,
  type HostInfo,
  type LogEntry,
  type MetricSample,
  type ProcessInfo,
  type ServiceInfo,
} from "@/lib/api"
import {
  formatBytes,
  formatDateTime,
  formatPercent,
  formatRate,
  formatRelative,
  formatUptime,
  plural,
} from "@/lib/format"
import { usePageInterval } from "@/lib/page"
import { cn } from "@/lib/utils"

function DashboardPage() {
  const interval = usePageInterval("dashboard")
  const dashboard = useQuery({
    queryKey: ["dashboard"],
    queryFn: () => api<DashboardResponse>("/dashboard"),
    refetchInterval: interval.milliseconds
      ? Math.max(interval.milliseconds, 30_000)
      : false,
  })
  const history = useQuery({
    queryKey: ["metrics", "1h"],
    queryFn: () => api<{ samples: MetricSample[] }>("/metrics?range=1h"),
  })
  const processes = useQuery({
    queryKey: ["processes"],
    queryFn: () => api<{ items: ProcessInfo[] }>("/processes"),
    refetchInterval: interval.milliseconds
      ? Math.max(interval.milliseconds, 5_000)
      : false,
  })
  const services = useQuery({
    queryKey: ["services", "system", "service"],
    queryFn: () =>
      api<{ items: ServiceInfo[] }>("/services?scope=system&type=service"),
    refetchInterval: interval.milliseconds
      ? Math.max(interval.milliseconds, 30_000)
      : false,
  })
  const logs = useQuery({
    queryKey: ["logs", "recent-problems"],
    queryFn: () => api<{ items: LogEntry[] }>("/logs?limit=25&priority=0..3"),
    refetchInterval: 60_000,
  })
  const samples = useLiveSamples(history.data?.samples ?? [], interval.value)

  if (dashboard.isPending) return <DashboardSkeleton />
  if (dashboard.isError || !dashboard.data) {
    return (
      <main className="flex flex-1 flex-col p-4 lg:p-6">
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>The host did not respond</AlertTitle>
          <AlertDescription>
            Tako could not read the host summary. Check that the session service
            is running.
          </AlertDescription>
          <AlertAction>
            <Button
              size="sm"
              variant="outline"
              onClick={() => void dashboard.refetch()}
            >
              <RotateCcwIcon data-icon="inline-start" />
              Retry
            </Button>
          </AlertAction>
        </Alert>
      </main>
    )
  }

  const { host, storage } = dashboard.data
  const rates = toChartSamples(samples.slice(-2)).at(-1)
  const latest = samples.at(-1) ?? dashboard.data.metrics
  const memoryPercent = latest.memoryTotal
    ? (latest.memoryUsed / latest.memoryTotal) * 100
    : 0
  const failed =
    services.data?.items.filter((item) => item.activeState === "failed") ?? []

  return (
    <main className="@container/main flex flex-1 flex-col gap-6 p-4 lg:p-6">
      <section className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h2 className="truncate text-2xl font-semibold tracking-tight">
              {host.hostname}
            </h2>
            <Badge variant="outline" className="gap-1.5">
              <span
                className="size-1.5 rounded-full bg-success"
                aria-hidden="true"
              />
              Online
            </Badge>
          </div>
          <p className="text-sm text-muted-foreground">
            {host.operatingSystem} · up {formatUptime(host.uptimeSeconds)}
          </p>
        </div>
        <RefreshSelect value={interval.value} onChange={interval.setValue} />
      </section>

      <HostNotices host={host} failed={failed} />

      <section className="grid gap-4 sm:grid-cols-2 @5xl/main:grid-cols-4">
        <Stat
          title="CPU"
          icon={CpuIcon}
          value={formatPercent(latest.cpuPercent)}
          detail={`Load ${latest.load1.toFixed(2)} · ${latest.load5.toFixed(2)} · ${latest.load15.toFixed(2)}`}
          percent={latest.cpuPercent}
        />
        <Stat
          title="Memory"
          icon={MemoryStickIcon}
          value={formatPercent(memoryPercent)}
          detail={`${formatBytes(latest.memoryUsed)} of ${formatBytes(latest.memoryTotal)}`}
          percent={memoryPercent}
        />
        <Stat
          title="Storage"
          icon={HardDriveIcon}
          value={storage ? formatPercent(storage.percent) : "—"}
          detail={
            storage
              ? `${formatBytes(storage.used)} of ${formatBytes(storage.total)}`
              : "Capacity unavailable"
          }
          percent={storage?.percent}
          to="/storage"
        />
        <Stat
          title="Network"
          icon={NetworkIcon}
          value={rates ? formatRate(rates.networkRxRate) : "—"}
          detail={
            rates
              ? `${formatRate(rates.networkTxRate)} sent`
              : "Waiting for a second sample"
          }
          to="/network"
        />
      </section>

      <MetricsCharts samples={samples} />

      <section className="grid gap-4 @5xl/main:grid-cols-5">
        <SystemFacts host={host} className="@5xl/main:col-span-2" />
        <TopProcesses query={processes} className="@5xl/main:col-span-3" />
      </section>

      <RecentProblems query={logs} />
    </main>
  )
}

function HostNotices({
  host,
  failed,
}: {
  host: HostInfo
  failed: ServiceInfo[]
}) {
  return (
    <>
      {failed.length > 0 && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>{plural(failed.length, "service")} failed</AlertTitle>
          <AlertDescription className="truncate">
            {failed
              .slice(0, 4)
              .map((item) => item.name)
              .join(", ")}
            {failed.length > 4 ? ` and ${failed.length - 4} more` : ""}
          </AlertDescription>
          <AlertAction>
            <Button
              size="sm"
              variant="outline"
              render={<Link to="/services" search={{ active: "failed" }} />}
            >
              Review
            </Button>
          </AlertAction>
        </Alert>
      )}
      {host.restart.available && host.restart.required && (
        <Alert>
          <RotateCcwIcon />
          <AlertTitle>Restart required</AlertTitle>
          <AlertDescription>
            {host.restart.reason ||
              "Installed updates take effect after a restart."}
          </AlertDescription>
          <AlertAction>
            <Button size="sm" variant="outline" render={<Link to="/host" />}>
              Power options
            </Button>
          </AlertAction>
        </Alert>
      )}
    </>
  )
}

function Stat({
  title,
  icon: Icon,
  value,
  detail,
  percent,
  to,
}: {
  title: string
  icon: React.ComponentType
  value: string
  detail: string
  percent?: number
  to?: "/storage" | "/network"
}) {
  const body = (
    <Card
      className={cn(
        "h-full gap-3",
        to && "transition-colors hover:bg-accent/40"
      )}
    >
      <CardHeader>
        <CardDescription className="flex items-center gap-2 [&_svg]:size-4">
          <Icon />
          {title}
        </CardDescription>
        <CardTitle className="text-2xl tabular-nums">{value}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        {percent !== undefined && (
          <Progress
            value={Math.min(100, Math.max(0, percent))}
            aria-label={`${title} usage`}
            className={cn(
              percent >= 90 &&
                "[&_[data-slot=progress-indicator]]:bg-destructive"
            )}
          />
        )}
        <p className="truncate text-xs text-muted-foreground tabular-nums">
          {detail}
        </p>
      </CardContent>
    </Card>
  )
  return to ? (
    <Link
      to={to}
      className="rounded-xl outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
    >
      {body}
    </Link>
  ) : (
    body
  )
}

function SystemFacts({
  host,
  className,
}: {
  host: HostInfo
  className?: string
}) {
  const shutdown = !host.shutdown.available
    ? "Unknown"
    : host.shutdown.clean
      ? "Clean"
      : "Unclean"
  const facts: [string, React.ReactNode][] = [
    ["Processor", host.hardware.cpuModel || "Unknown"],
    ["Cores", host.hardware.cpuCores ?? "—"],
    ["Memory", formatBytes(host.hardware.memoryTotal)],
    ["Kernel", `${host.kernel} (${host.architecture})`],
    ["Booted", formatDateTime(host.bootedAt)],
    ["Last shutdown", shutdown],
    [
      "Restart",
      !host.restart.available
        ? "Unknown"
        : host.restart.required
          ? "Required"
          : "Not required",
    ],
  ]
  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle>System</CardTitle>
        <CardAction>
          <Button size="sm" variant="ghost" render={<Link to="/host" />}>
            Configure
            <ArrowRightIcon data-icon="inline-end" />
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2.5 text-sm">
          {facts.map(([label, value]) => (
            <div key={label} className="contents">
              <dt className="text-muted-foreground">{label}</dt>
              <dd className="min-w-0 truncate text-right font-medium">
                {value}
              </dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  )
}

function TopProcesses({
  query,
  className,
}: {
  query: {
    data?: { items: ProcessInfo[] }
    isPending: boolean
    isError: boolean
  }
  className?: string
}) {
  const items = [...(query.data?.items ?? [])]
    .sort(
      (a, b) => (b.cpuPercent ?? 0) - (a.cpuPercent ?? 0) || b.memory - a.memory
    )
    .slice(0, 8)
  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle>Top processes</CardTitle>
        <CardDescription>By CPU, then memory</CardDescription>
        <CardAction>
          <Button size="sm" variant="ghost" render={<Link to="/processes" />}>
            All processes
            <ArrowRightIcon data-icon="inline-end" />
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {query.isPending ? (
          <Skeleton className="h-64" />
        ) : query.isError ? (
          <p className="text-sm text-muted-foreground">
            Process inventory is unavailable.
          </p>
        ) : (
          <ul className="-mx-2 flex flex-col">
            {items.map((item) => (
              <li key={`${item.pid}:${item.started}`}>
                <Link
                  to="/processes/$pid"
                  params={{ pid: String(item.pid) }}
                  search={{ started: String(item.started) }}
                  className="grid grid-cols-[1fr_auto_auto] items-center gap-4 rounded-md px-2 py-1.5 text-sm outline-none hover:bg-accent focus-visible:ring-3 focus-visible:ring-ring/50"
                >
                  <span className="min-w-0 truncate">
                    <span className="font-medium">{item.program}</span>
                    <span className="ml-2 text-xs text-muted-foreground">
                      {item.user} · {item.pid}
                    </span>
                  </span>
                  <span className="w-16 text-right tabular-nums">
                    {formatPercent(item.cpuPercent ?? 0)}
                  </span>
                  <span className="w-20 text-right text-muted-foreground tabular-nums">
                    {formatBytes(item.memory)}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

const priorityNames = ["Emergency", "Alert", "Critical", "Error"]

function RecentProblems({
  query,
}: {
  query: { data?: { items: LogEntry[] }; isPending: boolean; isError: boolean }
}) {
  const items = (query.data?.items ?? []).slice(0, 6)
  return (
    <Card>
      <CardHeader>
        <CardTitle>Recent problems</CardTitle>
        <CardDescription>
          Journal entries at error priority or higher
        </CardDescription>
        <CardAction>
          <Button
            size="sm"
            variant="ghost"
            render={<Link to="/logs" search={{ priority: "0..3" }} />}
          >
            Open logs
            <ArrowRightIcon data-icon="inline-end" />
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {query.isPending ? (
          <Skeleton className="h-40" />
        ) : items.length === 0 ? (
          <Empty className="border-0 p-6">
            <EmptyHeader>
              <EmptyTitle>No recent problems</EmptyTitle>
              <EmptyDescription>
                Nothing at error priority or higher in the journal.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <ul className="flex flex-col divide-y">
            {items.map((entry, index) => (
              <li
                key={entry.id ?? `${entry.timestamp}-${index}`}
                className="flex gap-3 py-2.5 first:pt-0 last:pb-0"
              >
                <Badge
                  variant="destructive"
                  className="mt-0.5 w-20 shrink-0 justify-center"
                >
                  {priorityNames[Number(entry.priority)] ?? "Error"}
                </Badge>
                <div className="min-w-0 flex-1">
                  <p className="line-clamp-2 text-sm">{entry.message}</p>
                  <p className="text-xs text-muted-foreground">
                    {entry.unit || "kernel"} · {formatRelative(entry.timestamp)}
                  </p>
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

function DashboardSkeleton() {
  return (
    <main
      className="@container/main flex flex-1 flex-col gap-6 p-4 lg:p-6"
      aria-busy="true"
    >
      <Skeleton className="h-12 w-64" />
      <div className="grid gap-4 sm:grid-cols-2 @5xl/main:grid-cols-4">
        {["cpu", "memory", "storage", "network"].map((key) => (
          <Skeleton key={key} className="h-32" />
        ))}
      </div>
      <div className="grid gap-4 @4xl/main:grid-cols-2">
        <Skeleton className="h-72" />
        <Skeleton className="h-72" />
      </div>
    </main>
  )
}

export const Route = createFileRoute("/")({ component: DashboardPage })
