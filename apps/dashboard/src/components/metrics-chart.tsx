import * as React from "react"
import {
  Area,
  AreaChart,
  CartesianGrid,
  Line,
  LineChart,
  XAxis,
  YAxis,
} from "recharts"

import type { MetricSample } from "@/lib/api"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import type { RefreshInterval } from "@/lib/monitoring"

const configs = {
  cpu: { cpuPercent: { label: "CPU", color: "var(--chart-cpu)" } },
  memory: {
    memoryPercent: { label: "Memory", color: "var(--chart-memory)" },
    swapPercent: { label: "Swap", color: "var(--chart-swap)" },
  },
  disk: {
    diskReadRate: { label: "Read/s", color: "var(--chart-disk-read)" },
    diskWriteRate: { label: "Write/s", color: "var(--chart-disk-write)" },
  },
  network: {
    networkRxRate: { label: "Received/s", color: "var(--chart-network-rx)" },
    networkTxRate: { label: "Sent/s", color: "var(--chart-network-tx)" },
  },
} satisfies Record<string, ChartConfig>

type ChartSample = MetricSample & {
  memoryPercent: number
  swapPercent: number
  diskReadRate: number
  diskWriteRate: number
  networkRxRate: number
  networkTxRate: number
}

function elapsedSeconds(
  samples: MetricSample[],
  index: number
): number {
  const previous = samples[index - 1]
  return previous
    ? Math.max(
        1,
        (new Date(samples[index].timestamp).getTime() -
          new Date(previous.timestamp).getTime()) /
          1000
      )
    : 1
}

// All byte counters in MetricSample are cumulative. Rates come from
// consecutive deltas. Disk rates prefer the per-device map when present: a
// disk's first sighting contributes nothing, so hot-plugging a drive never
// fabricates a spike from its lifetime counter.
function diskRates(samples: MetricSample[], index: number): {
  read: number
  write: number
} {
  const sample = samples[index]
  const previous = samples[index - 1]
  const seconds = elapsedSeconds(samples, index)
  if (!previous || !sample.disks || !previous.disks) {
    return {
      read: previous
        ? Math.max(0, sample.diskRead - previous.diskRead) / seconds
        : 0,
      write: previous
        ? Math.max(0, sample.diskWrite - previous.diskWrite) / seconds
        : 0,
    }
  }
  let read = 0
  let write = 0
  for (const [device, current] of Object.entries(sample.disks)) {
    const before = previous.disks[device]
    if (!before) continue
    read += Math.max(0, current.read - before.read)
    write += Math.max(0, current.write - before.write)
  }
  return { read: read / seconds, write: write / seconds }
}

function transform(samples: MetricSample[]): ChartSample[] {
  return samples.map((sample, index) => {
    const previous = samples[index - 1]
    const seconds = elapsedSeconds(samples, index)
    const rates = diskRates(samples, index)
    return {
      ...sample,
      memoryPercent: sample.memoryTotal
        ? (sample.memoryUsed / sample.memoryTotal) * 100
        : 0,
      swapPercent: sample.swapTotal
        ? (sample.swapUsed / sample.swapTotal) * 100
        : 0,
      diskReadRate: rates.read,
      diskWriteRate: rates.write,
      networkRxRate: previous
        ? Math.max(0, sample.networkRx - previous.networkRx) / seconds
        : 0,
      networkTxRate: previous
        ? Math.max(0, sample.networkTx - previous.networkTx) / seconds
        : 0,
    }
  })
}

// Per-disk rates from consecutive deltas of one device. First sighting of the
// device contributes zero for the same hot-plug reason as above.
function transformDevice(
  samples: MetricSample[],
  device: string
): ChartSample[] {
  return samples.map((sample, index) => {
    const previous = samples[index - 1]?.disks?.[device]
    const current = sample.disks?.[device]
    const seconds = elapsedSeconds(samples, index)
    return {
      ...sample,
      memoryPercent: sample.memoryTotal
        ? (sample.memoryUsed / sample.memoryTotal) * 100
        : 0,
      swapPercent: sample.swapTotal
        ? (sample.swapUsed / sample.swapTotal) * 100
        : 0,
      diskReadRate:
        current && previous
          ? Math.max(0, current.read - previous.read) / seconds
          : 0,
      diskWriteRate:
        current && previous
          ? Math.max(0, current.write - previous.write) / seconds
          : 0,
      networkRxRate: 0,
      networkTxRate: 0,
    }
  })
}

function mergeSamples(
  history: MetricSample[],
  live: MetricSample[]
): MetricSample[] {
  const byTime = new Map<string, MetricSample>()
  for (const sample of history) {
    byTime.set(sample.timestamp, sample)
  }
  for (const sample of live) {
    byTime.set(sample.timestamp, sample)
  }
  return [...byTime.values()].sort((left, right) =>
    left.timestamp < right.timestamp
      ? -1
      : left.timestamp > right.timestamp
        ? 1
        : 0
  )
}

export function MetricsCharts({
  initialSamples,
  interval = "1m",
  compact = false,
  scope = "all",
  range,
}: {
  initialSamples: MetricSample[]
  interval?: RefreshInterval
  compact?: boolean
  scope?: "all" | "storage" | "network"
  range?: string
}) {
  const [live, setLive] = React.useState<MetricSample[]>([])
  const [diskDevice, setDiskDevice] = React.useState("all")
  React.useEffect(() => {
    if (interval === "off") return
    const source = new EventSource(
      `/api/v1/metrics/stream?interval=${interval}`
    )
    source.addEventListener("metric", (event) =>
      setLive((current) => [
        ...current.slice(-199),
        JSON.parse((event as MessageEvent<string>).data) as MetricSample,
      ])
    )
    return () => source.close()
  }, [interval, range])
  const samples = React.useMemo(
    () => transform(mergeSamples(initialSamples, live)),
    [initialSamples, live]
  )
  if (scope === "storage") {
    const devices = Object.keys(samples.at(-1)?.disks ?? {}).sort()
    const data =
      diskDevice !== "all" ? transformDevice(samples, diskDevice) : samples
    return (
      <section className="flex flex-col gap-4">
        <Select
          items={[
            { value: "all", label: "All devices" },
            ...devices.map((device) => ({ value: device, label: device })),
          ]}
          value={diskDevice}
          onValueChange={(next) => setDiskDevice(String(next))}
        >
          <SelectTrigger size="sm" className="w-44" aria-label="Disk device">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {["all", ...devices].map((device) => (
                <SelectItem key={device} value={device}>
                  {device === "all" ? "All devices" : device}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        <section className="grid gap-4 @4xl/main:grid-cols-2">
          <ResourceChart
            title="Storage reads"
            description={
              diskDevice === "all"
                ? "Read throughput across whole physical disks"
                : `Read throughput for ${diskDevice}`
            }
            data={data}
            config={configs.disk}
            keys={["diskReadRate"]}
          />
          <ResourceChart
            title="Storage writes"
            description={
              diskDevice === "all"
                ? "Write throughput across whole physical disks"
                : `Write throughput for ${diskDevice}`
            }
            data={data}
            config={configs.disk}
            keys={["diskWriteRate"]}
          />
        </section>
      </section>
    )
  }
  if (scope === "network") {
    return (
      <section className="grid gap-4 @4xl/main:grid-cols-2">
        <ResourceChart
          title="Network receiving"
          description="Aggregate receive throughput excluding loopback"
          data={samples}
          config={configs.network}
          keys={["networkRxRate"]}
        />
        <ResourceChart
          title="Network transmitting"
          description="Aggregate transmit throughput excluding loopback"
          data={samples}
          config={configs.network}
          keys={["networkTxRate"]}
        />
      </section>
    )
  }
  return (
    <section
      className={`grid gap-4 ${compact ? "@4xl/main:grid-cols-2" : "xl:grid-cols-2"}`}
    >
      <ResourceChart
        title="CPU utilization"
        description={`Load ${samples.at(-1)?.load1.toFixed(2) ?? "—"} · ${samples.at(-1)?.cpuCorePercent.length ?? 0} cores`}
        data={samples}
        config={configs.cpu}
        keys={["cpuPercent"]}
        percent
      />
      <ResourceChart
        title="Memory and swap"
        description="Working memory pressure"
        data={samples}
        config={configs.memory}
        keys={["memoryPercent", "swapPercent"]}
        percent
      />
      <ResourceChart
        title="Storage I/O"
        description="Whole-disk throughput"
        data={samples}
        config={configs.disk}
        keys={["diskReadRate", "diskWriteRate"]}
      />
      <ResourceChart
        title="Network traffic"
        description="Aggregate traffic excluding loopback"
        data={samples}
        config={configs.network}
        keys={["networkRxRate", "networkTxRate"]}
      />
    </section>
  )
}

function ResourceChart({
  title,
  description,
  data,
  config,
  keys,
  percent,
}: {
  title: string
  description: string
  data: ChartSample[]
  config: ChartConfig
  keys: (keyof ChartSample)[]
  percent?: boolean
}) {
  const Chart = percent ? AreaChart : LineChart
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent>
        <ChartContainer config={config} className="h-56 w-full">
          <Chart data={data} accessibilityLayer>
            <CartesianGrid vertical={false} />
            <XAxis
              dataKey="timestamp"
              tickLine={false}
              axisLine={false}
              minTickGap={40}
              tickFormatter={(value) =>
                new Date(value).toLocaleTimeString([], {
                  hour: "2-digit",
                  minute: "2-digit",
                })
              }
            />
            <YAxis
              domain={percent ? [0, 100] : undefined}
              tickLine={false}
              axisLine={false}
              width={48}
              tickFormatter={(value) =>
                percent ? `${value}%` : formatRate(value)
              }
            />
            <ChartTooltip
              content={
                <ChartTooltipContent
                  labelFormatter={(_, payload) =>
                    payload?.[0]
                      ? new Date(payload[0].payload.timestamp).toLocaleString()
                      : ""
                  }
                  formatter={(value, name) => (
                    <>
                      <span className="text-muted-foreground">
                        {config[String(name)]?.label}
                      </span>
                      <span className="ml-auto font-mono">
                        {percent
                          ? `${Number(value).toFixed(1)}%`
                          : formatRate(Number(value))}
                      </span>
                    </>
                  )}
                />
              }
            />
            {keys.map((key) =>
              percent ? (
                <Area
                  key={String(key)}
                  dataKey={String(key)}
                  type="monotone"
                  isAnimationActive={false}
                  fill={`var(--color-${String(key)})`}
                  fillOpacity={0.12}
                  stroke={`var(--color-${String(key)})`}
                />
              ) : (
                <Line
                  key={String(key)}
                  dataKey={String(key)}
                  type="monotone"
                  isAnimationActive={false}
                  dot={false}
                  stroke={`var(--color-${String(key)})`}
                />
              )
            )}
          </Chart>
        </ChartContainer>
      </CardContent>
    </Card>
  )
}

function formatRate(value: number) {
  const units = ["B/s", "KiB/s", "MiB/s", "GiB/s"]
  let amount = value,
    unit = 0
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024
    unit++
  }
  return `${amount.toFixed(unit ? 1 : 0)} ${units[unit]}`
}
