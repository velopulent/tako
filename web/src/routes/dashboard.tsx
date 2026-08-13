import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { CpuIcon, HardDriveIcon, MemoryStickIcon, TimerIcon } from "lucide-react"

import { api, type Capability, type DashboardResponse, type MetricSample } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"

const MetricsChart = React.lazy(() =>
  import("@/components/metrics-chart").then((module) => ({ default: module.MetricsChart }))
)

function bytes(value: number) {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"]
  let amount = value
  let index = 0
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024
    index++
  }
  return `${amount.toFixed(index > 2 ? 1 : 0)} ${units[index]}`
}

function duration(seconds: number) {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  return days > 0 ? `${days}d ${hours}h` : `${hours}h ${Math.floor((seconds % 3600) / 60)}m`
}

export function DashboardPage() {
  const dashboard = useQuery({ queryKey: ["dashboard"], queryFn: () => api<DashboardResponse>("/dashboard"), refetchInterval: 10_000 })
  const history = useQuery({ queryKey: ["metrics"], queryFn: () => api<{ samples: MetricSample[] }>("/metrics") })
  const capabilities = useQuery({ queryKey: ["capabilities"], queryFn: () => api<{ capabilities: Capability[] }>("/capabilities") })

  if (!dashboard.data) {
    return <DashboardSkeleton />
  }

  const { host, metrics } = dashboard.data
  const memoryPercent = metrics.memoryTotal ? (metrics.memoryUsed / metrics.memoryTotal) * 100 : 0
  const available = capabilities.data?.capabilities.filter((item) => item.available).length ?? 0
  const total = capabilities.data?.capabilities.length ?? 0

  return (
    <main className="@container/main flex flex-1 flex-col gap-6 p-4 lg:p-6">
      <section className="flex flex-col gap-1">
        <div className="flex flex-wrap items-center gap-3">
          <h2 className="text-2xl font-semibold tracking-tight">{host.hostname}</h2>
          <Badge variant="secondary">Online</Badge>
        </div>
        <p className="text-sm text-muted-foreground">{host.operatingSystem} · Linux {host.kernel} · {host.architecture}</p>
      </section>

      <section className="grid gap-4 @xl/main:grid-cols-2 @5xl/main:grid-cols-4">
        <MetricCard title="CPU" value={`${metrics.cpuPercent.toFixed(1)}%`} description={`Load average ${metrics.load1.toFixed(2)}`} icon={CpuIcon} progress={metrics.cpuPercent} />
        <MetricCard title="Memory" value={bytes(metrics.memoryUsed)} description={`${bytes(metrics.memoryTotal)} total`} icon={MemoryStickIcon} progress={memoryPercent} />
        <MetricCard title="Network" value={bytes(metrics.networkRx)} description={`${bytes(metrics.networkTx)} transmitted`} icon={HardDriveIcon} />
        <MetricCard title="Uptime" value={duration(host.uptimeSeconds)} description={`${available} of ${total} capabilities ready`} icon={TimerIcon} />
      </section>

      <React.Suspense fallback={<Skeleton className="h-80" />}>
        <MetricsChart initialSamples={history.data?.samples ?? []} />
      </React.Suspense>

      <Card>
        <CardHeader>
          <CardTitle>Host capabilities</CardTitle>
          <CardDescription>Tako detects optional system services at runtime.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {capabilities.data?.capabilities.map((capability) => (
            <div key={capability.id} className="flex items-start justify-between gap-3 rounded-lg border p-3">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium capitalize">{capability.id}</p>
                <p className="truncate text-xs text-muted-foreground">{capability.reason ?? "Ready"}</p>
              </div>
              <Badge variant={capability.available ? "secondary" : "outline"}>{capability.available ? "Ready" : "Unavailable"}</Badge>
            </div>
          ))}
        </CardContent>
      </Card>
    </main>
  )
}

function MetricCard({ title, value, description, icon: Icon, progress }: { title: string; value: string; description: string; icon: React.ComponentType; progress?: number }) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between gap-4">
          <CardDescription>{title}</CardDescription>
          <Icon />
        </div>
        <CardTitle className="text-2xl tabular-nums">{value}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {progress !== undefined && <Progress value={Math.min(100, progress)} aria-label={`${title} usage`} />}
        <p className="text-xs text-muted-foreground">{description}</p>
      </CardContent>
    </Card>
  )
}

function DashboardSkeleton() {
  return (
    <main className="flex flex-col gap-6 p-4 lg:p-6">
      <Skeleton className="h-16 w-full max-w-xl" />
      <section className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className="h-40" />)}
      </section>
      <Skeleton className="h-80" />
    </main>
  )
}
