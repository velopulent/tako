import { ShieldAlertIcon } from "lucide-react"
import * as React from "react"
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts"

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  type ChartConfig,
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import type { ProcessResourceSample } from "@/lib/api"

const bytes = (value: number) => {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"]
  let amount = value
  let index = 0
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024
    index += 1
  }
  return `${amount.toFixed(index ? 1 : 0)} ${units[index]}`
}

const bytesPerSecond = (value: number) => `${bytes(value)}/s`

type ChartPoint = {
  timestamp: string
  memory: number
  cpuRate: number
  diskReadRate: number
  diskWriteRate: number
}

function toChartPoints(history: ProcessResourceSample[]): ChartPoint[] {
  return history.map((sample, index) => {
    const previous = history[index - 1]
    if (!previous) {
      return {
        timestamp: sample.timestamp,
        memory: sample.memory,
        cpuRate: 0,
        diskReadRate: 0,
        diskWriteRate: 0,
      }
    }
    const deltaSeconds = Math.max(
      1,
      (new Date(sample.timestamp).getTime() -
        new Date(previous.timestamp).getTime()) /
        1000
    )
    return {
      timestamp: sample.timestamp,
      memory: sample.memory,
      cpuRate: Math.max(0, sample.cpuTime - previous.cpuTime) / deltaSeconds,
      diskReadRate:
        Math.max(0, sample.diskRead - previous.diskRead) / deltaSeconds,
      diskWriteRate:
        Math.max(0, sample.diskWrite - previous.diskWrite) / deltaSeconds,
    }
  })
}

const cpuMemoryConfig = {
  memory: { label: "Memory", color: "var(--chart-memory)" },
  cpuRate: { label: "CPU", color: "var(--chart-cpu)" },
} satisfies ChartConfig

const diskConfig = {
  diskReadRate: { label: "Read", color: "var(--chart-disk-read)" },
  diskWriteRate: { label: "Write", color: "var(--chart-disk-write)" },
} satisfies ChartConfig

export function ResourceHistoryCharts({
  history,
}: {
  history: ProcessResourceSample[]
}) {
  const data = React.useMemo(() => toChartPoints(history), [history])
  const ioRestricted = history.at(-1)?.ioDenied === true

  if (!history.length) {
    return (
      <Empty className="border border-dashed">
        <EmptyHeader>
          <EmptyTitle>No history yet</EmptyTitle>
          <EmptyDescription>
            History starts after the next inventory refresh.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className="grid gap-4">
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">CPU &amp; Memory</CardTitle>
          <CardDescription>
            Memory resident + CPU time delta per interval
          </CardDescription>
        </CardHeader>
        <CardContent>
          <ChartContainer
            config={cpuMemoryConfig}
            className="h-48 w-full sm:h-56"
          >
            <AreaChart
              data={data}
              accessibilityLayer
              margin={{ left: 8, right: 8 }}
            >
              <defs>
                <linearGradient id="fillMemory" x1="0" y1="0" x2="0" y2="1">
                  <stop
                    offset="5%"
                    stopColor="var(--color-memory)"
                    stopOpacity={0.8}
                  />
                  <stop
                    offset="95%"
                    stopColor="var(--color-memory)"
                    stopOpacity={0.08}
                  />
                </linearGradient>
                <linearGradient id="fillCpu" x1="0" y1="0" x2="0" y2="1">
                  <stop
                    offset="5%"
                    stopColor="var(--color-cpuRate)"
                    stopOpacity={0.8}
                  />
                  <stop
                    offset="95%"
                    stopColor="var(--color-cpuRate)"
                    stopOpacity={0.08}
                  />
                </linearGradient>
              </defs>
              <CartesianGrid vertical={false} />
              <XAxis
                dataKey="timestamp"
                tickLine={false}
                axisLine={false}
                minTickGap={32}
                tickMargin={8}
                tickFormatter={(value) =>
                  new Date(value).toLocaleTimeString([], {
                    hour: "2-digit",
                    minute: "2-digit",
                  })
                }
              />
              <YAxis
                yAxisId="memory"
                tickLine={false}
                axisLine={false}
                width={56}
                tickFormatter={(value) => bytes(value as number)}
              />
              <YAxis
                yAxisId="cpu"
                orientation="right"
                tickLine={false}
                axisLine={false}
                width={48}
                tickFormatter={(value) => `${Number(value).toFixed(1)}s`}
              />
              <ChartTooltip
                cursor={false}
                content={
                  <ChartTooltipContent
                    labelFormatter={(_, payload) =>
                      payload?.[0]
                        ? new Date(
                            payload[0].payload.timestamp
                          ).toLocaleString()
                        : ""
                    }
                    formatter={(value, name) => (
                      <>
                        <span className="text-muted-foreground">
                          {(
                            cpuMemoryConfig as Record<
                              string,
                              { label?: string }
                            >
                          )[String(name)]?.label ?? String(name)}
                        </span>
                        <span className="ml-auto font-mono">
                          {String(name) === "memory"
                            ? bytes(Number(value))
                            : `${Number(value).toFixed(2)}s/s`}
                        </span>
                      </>
                    )}
                  />
                }
              />
              <Area
                yAxisId="memory"
                dataKey="memory"
                type="monotone"
                stroke="var(--color-memory)"
                fill="url(#fillMemory)"
                strokeWidth={2}
                dot={false}
                isAnimationActive={false}
              />
              <Area
                yAxisId="cpu"
                dataKey="cpuRate"
                type="monotone"
                stroke="var(--color-cpuRate)"
                fill="url(#fillCpu)"
                strokeWidth={2}
                dot={false}
                isAnimationActive={false}
              />
            </AreaChart>
          </ChartContainer>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Disk I/O throughput</CardTitle>
          <CardDescription>
            {ioRestricted
              ? "Counters unavailable for this process"
              : "Stacked read vs write per interval"}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {ioRestricted ? (
            <Empty className="border border-dashed">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ShieldAlertIcon />
                </EmptyMedia>
                <EmptyTitle>Disk I/O unavailable</EmptyTitle>
                <EmptyDescription>
                  The server cannot read this process&apos;s I/O counters.
                  Elevated read access is required; CPU and memory history
                  remain accurate.
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <ChartContainer config={diskConfig} className="h-48 w-full sm:h-56">
              <AreaChart
                data={data}
                accessibilityLayer
                margin={{ left: 8, right: 8 }}
              >
                <defs>
                  <linearGradient id="fillDiskRead" x1="0" y1="0" x2="0" y2="1">
                    <stop
                      offset="5%"
                      stopColor="var(--color-diskReadRate)"
                      stopOpacity={0.8}
                    />
                    <stop
                      offset="95%"
                      stopColor="var(--color-diskReadRate)"
                      stopOpacity={0.08}
                    />
                  </linearGradient>
                  <linearGradient
                    id="fillDiskWrite"
                    x1="0"
                    y1="0"
                    x2="0"
                    y2="1"
                  >
                    <stop
                      offset="5%"
                      stopColor="var(--color-diskWriteRate)"
                      stopOpacity={0.8}
                    />
                    <stop
                      offset="95%"
                      stopColor="var(--color-diskWriteRate)"
                      stopOpacity={0.08}
                    />
                  </linearGradient>
                </defs>
                <CartesianGrid vertical={false} />
                <XAxis
                  dataKey="timestamp"
                  tickLine={false}
                  axisLine={false}
                  minTickGap={32}
                  tickMargin={8}
                  tickFormatter={(value) =>
                    new Date(value).toLocaleTimeString([], {
                      hour: "2-digit",
                      minute: "2-digit",
                    })
                  }
                />
                <YAxis
                  tickLine={false}
                  axisLine={false}
                  width={64}
                  tickFormatter={(value) => bytesPerSecond(value as number)}
                />
                <ChartTooltip
                  cursor={false}
                  content={
                    <ChartTooltipContent
                      labelFormatter={(_, payload) =>
                        payload?.[0]
                          ? new Date(
                              payload[0].payload.timestamp
                            ).toLocaleString()
                          : ""
                      }
                      formatter={(value, name) => (
                        <>
                          <span className="text-muted-foreground">
                            {(diskConfig as Record<string, { label?: string }>)[
                              String(name)
                            ]?.label ?? String(name)}
                          </span>
                          <span className="ml-auto font-mono">
                            {bytesPerSecond(Number(value))}
                          </span>
                        </>
                      )}
                    />
                  }
                />
                <Area
                  dataKey="diskReadRate"
                  type="monotone"
                  stackId="disk"
                  stroke="var(--color-diskReadRate)"
                  fill="url(#fillDiskRead)"
                  strokeWidth={2}
                  dot={false}
                  isAnimationActive={false}
                />
                <Area
                  dataKey="diskWriteRate"
                  type="monotone"
                  stackId="disk"
                  stroke="var(--color-diskWriteRate)"
                  fill="url(#fillDiskWrite)"
                  strokeWidth={2}
                  dot={false}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ChartContainer>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
