import * as React from "react"
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts"

import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  type ChartConfig,
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import type { MetricSample } from "@/lib/api"
import {
  clockFormatter,
  formatBytes,
  formatDateTime,
  formatPercent,
  formatRate,
} from "@/lib/format"
import type { RefreshInterval } from "@/lib/monitoring"
import { cn } from "@/lib/utils"

const configs = {
  cpu: { cpuPercent: { label: "CPU", color: "var(--chart-cpu)" } },
  memory: {
    memoryPercent: { label: "Memory", color: "var(--chart-memory)" },
    swapPercent: { label: "Swap", color: "var(--chart-swap)" },
  },
  diskRead: {
    diskReadRate: { label: "Read", color: "var(--chart-disk-read)" },
  },
  diskWrite: {
    diskWriteRate: { label: "Write", color: "var(--chart-disk-write)" },
  },
  disk: {
    diskReadRate: { label: "Read", color: "var(--chart-disk-read)" },
    diskWriteRate: { label: "Write", color: "var(--chart-disk-write)" },
  },
  networkRx: {
    networkRxRate: { label: "Received", color: "var(--chart-network-rx)" },
  },
  networkTx: {
    networkTxRate: { label: "Sent", color: "var(--chart-network-tx)" },
  },
  network: {
    networkRxRate: { label: "Received", color: "var(--chart-network-rx)" },
    networkTxRate: { label: "Sent", color: "var(--chart-network-tx)" },
  },
} satisfies Record<string, ChartConfig>

export type ChartSample = MetricSample & {
  memoryPercent: number
  swapPercent: number
  diskReadRate: number
  diskWriteRate: number
  networkRxRate: number
  networkTxRate: number
}

function seconds(samples: MetricSample[], index: number) {
  const previous = samples[index - 1]
  if (!previous) return 1
  return Math.max(
    1,
    (Date.parse(samples[index].timestamp) - Date.parse(previous.timestamp)) /
      1000
  )
}

// Byte counters are cumulative; rates come from consecutive deltas. A disk's
// first sighting contributes nothing, so hot-plugging a drive never fabricates
// a spike from its lifetime counter.
function diskDelta(
  sample: MetricSample,
  previous: MetricSample | undefined,
  device?: string
) {
  if (!previous) return { read: 0, write: 0 }
  if (device) {
    const now = sample.disks?.[device]
    const before = previous.disks?.[device]
    return now && before
      ? {
          read: Math.max(0, now.read - before.read),
          write: Math.max(0, now.write - before.write),
        }
      : { read: 0, write: 0 }
  }
  if (!sample.disks || !previous.disks) {
    return {
      read: Math.max(0, sample.diskRead - previous.diskRead),
      write: Math.max(0, sample.diskWrite - previous.diskWrite),
    }
  }
  let read = 0
  let write = 0
  for (const [name, now] of Object.entries(sample.disks)) {
    const before = previous.disks[name]
    if (!before) continue
    read += Math.max(0, now.read - before.read)
    write += Math.max(0, now.write - before.write)
  }
  return { read, write }
}

export function toChartSamples(
  samples: MetricSample[],
  device?: string
): ChartSample[] {
  return samples.map((sample, index) => {
    const previous = samples[index - 1]
    const elapsed = seconds(samples, index)
    const disk = diskDelta(sample, previous, device)
    return {
      ...sample,
      memoryPercent: sample.memoryTotal
        ? (sample.memoryUsed / sample.memoryTotal) * 100
        : 0,
      swapPercent: sample.swapTotal
        ? (sample.swapUsed / sample.swapTotal) * 100
        : 0,
      diskReadRate: disk.read / elapsed,
      diskWriteRate: disk.write / elapsed,
      networkRxRate: previous
        ? Math.max(0, sample.networkRx - previous.networkRx) / elapsed
        : 0,
      networkTxRate: previous
        ? Math.max(0, sample.networkTx - previous.networkTx) / elapsed
        : 0,
    }
  })
}

function merge(history: MetricSample[], live: MetricSample[]) {
  const byTime = new Map<string, MetricSample>()
  for (const sample of history) byTime.set(sample.timestamp, sample)
  for (const sample of live) byTime.set(sample.timestamp, sample)
  return [...byTime.values()].sort(
    (a, b) => Date.parse(a.timestamp) - Date.parse(b.timestamp)
  )
}

// useLiveSamples merges stored history with the live stream. The stream
// closes while the tab is hidden and reopens when it returns.
export function useLiveSamples(
  history: MetricSample[],
  interval: RefreshInterval
) {
  const [live, setLive] = React.useState<MetricSample[]>([])
  const [visible, setVisible] = React.useState(
    () => document.visibilityState !== "hidden"
  )
  React.useEffect(() => {
    const update = () => setVisible(document.visibilityState !== "hidden")
    document.addEventListener("visibilitychange", update)
    return () => document.removeEventListener("visibilitychange", update)
  }, [])
  React.useEffect(() => {
    if (interval === "off" || !visible) return
    const source = new EventSource(
      `/api/v1/metrics/stream?interval=${interval}`
    )
    source.addEventListener("metric", (event) => {
      const sample = JSON.parse(
        (event as MessageEvent<string>).data
      ) as MetricSample
      setLive((current) => [...current.slice(-299), sample])
    })
    return () => source.close()
  }, [interval, visible])
  return React.useMemo(() => merge(history, live), [history, live])
}

type Scope = "all" | "storage" | "network"

// MetricsCharts draws merged samples; pages obtain them from useLiveSamples
// so one stream feeds every widget on the page.
export function MetricsCharts({
  samples: merged,
  scope = "all",
}: {
  samples: MetricSample[]
  scope?: Scope
}) {
  const [device, setDevice] = React.useState("all")
  const samples = React.useMemo(
    () =>
      toChartSamples(
        merged,
        scope === "storage" && device !== "all" ? device : undefined
      ),
    [merged, scope, device]
  )
  const latest = samples.at(-1)

  if (scope === "storage") {
    const devices = Object.keys(merged.at(-1)?.disks ?? {}).sort()
    return (
      <section className="flex flex-col gap-4">
        {devices.length > 1 && (
          <Select
            items={[
              { value: "all", label: "All disks" },
              ...devices.map((name) => ({ value: name, label: name })),
            ]}
            value={device}
            onValueChange={(next) => setDevice(String(next))}
          >
            <SelectTrigger size="sm" className="w-44" aria-label="Disk">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="all">All disks</SelectItem>
                {devices.map((name) => (
                  <SelectItem key={name} value={name}>
                    {name}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        )}
        <div className="grid gap-4 @4xl/main:grid-cols-2">
          <ResourceChart
            title="Disk reads"
            value={formatRate(latest?.diskReadRate)}
            data={samples}
            config={configs.diskRead}
          />
          <ResourceChart
            title="Disk writes"
            value={formatRate(latest?.diskWriteRate)}
            data={samples}
            config={configs.diskWrite}
          />
        </div>
      </section>
    )
  }
  if (scope === "network") {
    return (
      <div className="grid gap-4 @4xl/main:grid-cols-2">
        <ResourceChart
          title="Received"
          value={formatRate(latest?.networkRxRate)}
          data={samples}
          config={configs.networkRx}
        />
        <ResourceChart
          title="Sent"
          value={formatRate(latest?.networkTxRate)}
          data={samples}
          config={configs.networkTx}
        />
      </div>
    )
  }
  const cores = latest?.cpuCorePercent?.length ?? 0
  return (
    <div className="grid gap-4 @4xl/main:grid-cols-2">
      <ResourceChart
        title="CPU"
        value={formatPercent(latest?.cpuPercent)}
        description={
          latest
            ? `Load ${latest.load1.toFixed(2)} · ${cores} cores`
            : undefined
        }
        data={samples}
        config={configs.cpu}
        percent
      />
      <ResourceChart
        title="Memory"
        value={formatPercent(latest?.memoryPercent)}
        description={
          latest
            ? `${formatBytes(latest.memoryUsed)} of ${formatBytes(latest.memoryTotal)}`
            : undefined
        }
        data={samples}
        config={configs.memory}
        percent
      />
      <ResourceChart
        title="Disk I/O"
        value={
          latest
            ? `${formatRate(latest.diskReadRate)} · ${formatRate(latest.diskWriteRate)}`
            : "—"
        }
        description="Read · write"
        data={samples}
        config={configs.disk}
      />
      <ResourceChart
        title="Network"
        value={
          latest
            ? `${formatRate(latest.networkRxRate)} · ${formatRate(latest.networkTxRate)}`
            : "—"
        }
        description="Received · sent, excluding loopback"
        data={samples}
        config={configs.network}
      />
    </div>
  )
}

export function ResourceChart({
  title,
  value,
  description,
  data,
  config,
  percent = false,
  className,
}: {
  title: string
  value?: string
  description?: string
  data: ChartSample[]
  config: ChartConfig
  percent?: boolean
  className?: string
}) {
  const id = React.useId().replace(/:/g, "")
  const keys = Object.keys(config) as (keyof ChartSample)[]
  const format = (amount: number) =>
    percent ? formatPercent(amount) : formatRate(amount)
  return (
    <Card className={cn("min-w-0", className)}>
      <CardHeader>
        <CardDescription>{title}</CardDescription>
        <CardTitle className="text-xl tabular-nums">{value ?? "—"}</CardTitle>
        {description && (
          <CardAction className="text-xs text-muted-foreground">
            {description}
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        {data.length < 2 ? (
          <div className="flex h-48 items-center justify-center rounded-lg border border-dashed text-sm text-muted-foreground">
            Collecting samples…
          </div>
        ) : (
          <ChartContainer config={config} className="aspect-auto h-48 w-full">
            <AreaChart
              data={data}
              margin={{ left: 0, right: 4, top: 4 }}
              accessibilityLayer
            >
              <defs>
                {keys.map((key) => (
                  <linearGradient
                    key={String(key)}
                    id={`${id}-${String(key)}`}
                    x1="0"
                    y1="0"
                    x2="0"
                    y2="1"
                  >
                    <stop
                      offset="0%"
                      stopColor={`var(--color-${String(key)})`}
                      stopOpacity={0.28}
                    />
                    <stop
                      offset="100%"
                      stopColor={`var(--color-${String(key)})`}
                      stopOpacity={0.02}
                    />
                  </linearGradient>
                ))}
              </defs>
              <CartesianGrid vertical={false} />
              <XAxis
                dataKey="timestamp"
                tickLine={false}
                axisLine={false}
                minTickGap={48}
                tickMargin={8}
                tickFormatter={clockFormatter(
                  data[0]?.timestamp,
                  data.at(-1)?.timestamp
                )}
              />
              <YAxis
                domain={percent ? [0, 100] : [0, "auto"]}
                ticks={percent ? [0, 25, 50, 75, 100] : undefined}
                tickLine={false}
                axisLine={false}
                width={percent ? 40 : 64}
                tickFormatter={(amount: number) =>
                  percent ? `${amount}%` : formatRate(amount)
                }
              />
              <ChartTooltip
                cursor={{ strokeDasharray: "3 3" }}
                content={
                  <ChartTooltipContent
                    labelFormatter={(_, payload) =>
                      payload?.[0]
                        ? formatDateTime(payload[0].payload.timestamp)
                        : ""
                    }
                    formatter={(amount, name, item) => (
                      <div className="flex w-full items-center gap-2">
                        <span
                          className="size-2 shrink-0 rounded-[2px]"
                          style={{ background: item.color }}
                        />
                        <span className="text-muted-foreground">
                          {config[String(name)]?.label}
                        </span>
                        <span className="ml-auto font-mono tabular-nums">
                          {format(Number(amount))}
                        </span>
                      </div>
                    )}
                  />
                }
              />
              {keys.length > 1 && (
                <ChartLegend content={<ChartLegendContent />} />
              )}
              {keys.map((key) => (
                <Area
                  key={String(key)}
                  dataKey={String(key)}
                  type="linear"
                  isAnimationActive={false}
                  strokeWidth={1.5}
                  stroke={`var(--color-${String(key)})`}
                  fill={`url(#${id}-${String(key)})`}
                />
              ))}
            </AreaChart>
          </ChartContainer>
        )}
      </CardContent>
    </Card>
  )
}
