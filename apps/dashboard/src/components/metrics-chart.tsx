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

function transform(samples: MetricSample[]): ChartSample[] {
  return samples.map((sample, index) => {
    const previous = samples[index - 1]
    const seconds = previous
      ? Math.max(
          1,
          (new Date(sample.timestamp).getTime() -
            new Date(previous.timestamp).getTime()) /
            1000
        )
      : 1
    return {
      ...sample,
      memoryPercent: sample.memoryTotal
        ? (sample.memoryUsed / sample.memoryTotal) * 100
        : 0,
      swapPercent: sample.swapTotal
        ? (sample.swapUsed / sample.swapTotal) * 100
        : 0,
      diskReadRate: previous
        ? Math.max(0, sample.diskRead - previous.diskRead) / seconds
        : 0,
      diskWriteRate: previous
        ? Math.max(0, sample.diskWrite - previous.diskWrite) / seconds
        : 0,
      networkRxRate: previous
        ? Math.max(0, sample.networkRx - previous.networkRx) / seconds
        : 0,
      networkTxRate: previous
        ? Math.max(0, sample.networkTx - previous.networkTx) / seconds
        : 0,
    }
  })
}

export function MetricsCharts({
  initialSamples,
  interval = "1m",
  compact = false,
}: {
  initialSamples: MetricSample[]
  interval?: RefreshInterval
  compact?: boolean
}) {
  const [live, setLive] = React.useState<MetricSample[]>([])
  React.useEffect(() => {
    if (interval === "off") return
    const source = new EventSource(
      `/api/v1/metrics/stream?interval=${interval}`
    )
    source.addEventListener("metric", (event) =>
      setLive((current) => [
        ...current.slice(-999),
        JSON.parse((event as MessageEvent<string>).data) as MetricSample,
      ])
    )
    return () => source.close()
  }, [interval])
  const samples = React.useMemo(
    () => transform([...initialSamples, ...live].slice(-1000)),
    [initialSamples, live]
  )
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
        description="Aggregate block-device throughput"
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
                  fill={`var(--color-${String(key)})`}
                  fillOpacity={0.12}
                  stroke={`var(--color-${String(key)})`}
                />
              ) : (
                <Line
                  key={String(key)}
                  dataKey={String(key)}
                  type="monotone"
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
