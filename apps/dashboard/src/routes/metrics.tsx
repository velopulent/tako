import { useQuery } from "@tanstack/react-query"
import {
  createFileRoute,
  getRouteApi,
  useNavigate,
} from "@tanstack/react-router"
import {
  Bar,
  BarChart,
  CartesianGrid,
  Line,
  LineChart,
  XAxis,
  YAxis,
} from "recharts"

import { MetricsCharts, useLiveSamples } from "@/components/metrics-chart"
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
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api, type MetricSample } from "@/lib/api"
import { clockFormatter, formatDateTime } from "@/lib/format"
import { Page, QueryError, usePageInterval } from "@/lib/page"
import { type MetricRange, metricRanges, metricsSearch } from "@/lib/search"

const rangeLabels: Record<MetricRange, string> = {
  "15m": "15 min",
  "1h": "1 hour",
  "6h": "6 hours",
  "24h": "24 hours",
}

const loadConfig = {
  load1: { label: "1 min", color: "var(--chart-1)" },
  load5: { label: "5 min", color: "var(--chart-3)" },
  load15: { label: "15 min", color: "var(--chart-4)" },
} satisfies ChartConfig

const coreConfig = {
  percent: { label: "Busy", color: "var(--chart-cpu)" },
} satisfies ChartConfig

function MetricsPage() {
  const interval = usePageInterval("metrics")
  const search = getRouteApi("/metrics").useSearch()
  const navigate = useNavigate({ from: "/metrics" })
  const range = search.range ?? "1h"
  const query = useQuery({
    queryKey: ["metrics", range],
    queryFn: () => api<{ samples: MetricSample[] }>(`/metrics?range=${range}`),
  })
  const samples = useLiveSamples(query.data?.samples ?? [], interval.value)
  const latest = samples.at(-1)
  const cores = (latest?.cpuCorePercent ?? []).map((percent, index) => ({
    core: String(index),
    percent,
  }))

  return (
    <Page
      description="Host telemetry sampled by Tako. History is kept in memory for up to 24 hours."
      interval={interval}
      actions={
        <Tabs
          value={range}
          onValueChange={(next) =>
            navigate({
              search: (previous) => ({
                ...previous,
                range: next === "1h" ? undefined : (next as MetricRange),
              }),
              replace: true,
            })
          }
        >
          <TabsList>
            {metricRanges.map((value) => (
              <TabsTrigger
                key={value}
                value={value}
                aria-label={rangeLabels[value]}
              >
                {value}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      }
    >
      {query.isError ? (
        <QueryError error={query.error} retry={() => void query.refetch()} />
      ) : query.isPending ? (
        <div className="grid gap-4 @4xl/main:grid-cols-2">
          {[0, 1, 2, 3].map((key) => (
            <Skeleton key={key} className="h-72" />
          ))}
        </div>
      ) : (
        <>
          <MetricsCharts samples={samples} />
          <div className="grid gap-4 @4xl/main:grid-cols-2">
            <Card className="min-w-0">
              <CardHeader>
                <CardTitle>Load average</CardTitle>
                <CardDescription>
                  Runnable and waiting tasks, averaged
                </CardDescription>
              </CardHeader>
              <CardContent>
                <ChartContainer
                  config={loadConfig}
                  className="aspect-auto h-48 w-full"
                >
                  <LineChart
                    data={samples}
                    margin={{ left: 0, right: 4, top: 4 }}
                    accessibilityLayer
                  >
                    <CartesianGrid vertical={false} />
                    <XAxis
                      dataKey="timestamp"
                      tickLine={false}
                      axisLine={false}
                      minTickGap={48}
                      tickMargin={8}
                      tickFormatter={clockFormatter(
                        samples[0]?.timestamp,
                        samples.at(-1)?.timestamp
                      )}
                    />
                    <YAxis tickLine={false} axisLine={false} width={40} />
                    <ChartTooltip
                      content={
                        <ChartTooltipContent
                          labelFormatter={(_, payload) =>
                            payload?.[0]
                              ? formatDateTime(payload[0].payload.timestamp)
                              : ""
                          }
                        />
                      }
                    />
                    <ChartLegend content={<ChartLegendContent />} />
                    {(
                      Object.keys(loadConfig) as (keyof typeof loadConfig)[]
                    ).map((key) => (
                      <Line
                        key={key}
                        dataKey={key}
                        type="linear"
                        dot={false}
                        strokeWidth={1.5}
                        isAnimationActive={false}
                        stroke={`var(--color-${key})`}
                      />
                    ))}
                  </LineChart>
                </ChartContainer>
              </CardContent>
            </Card>
            <Card className="min-w-0">
              <CardHeader>
                <CardTitle>CPU cores</CardTitle>
                <CardDescription>
                  Current busy time per logical core
                </CardDescription>
              </CardHeader>
              <CardContent>
                {cores.length ? (
                  <ChartContainer
                    config={coreConfig}
                    className="aspect-auto h-48 w-full"
                  >
                    <BarChart
                      data={cores}
                      margin={{ left: 0, right: 4, top: 4 }}
                      accessibilityLayer
                    >
                      <CartesianGrid vertical={false} />
                      <XAxis
                        dataKey="core"
                        tickLine={false}
                        axisLine={false}
                        interval={cores.length > 16 ? "preserveStartEnd" : 0}
                      />
                      <YAxis
                        domain={[0, 100]}
                        ticks={[0, 50, 100]}
                        tickLine={false}
                        axisLine={false}
                        width={40}
                        tickFormatter={(value) => `${value}%`}
                      />
                      <ChartTooltip
                        cursor={false}
                        content={
                          <ChartTooltipContent
                            labelFormatter={(label) => `Core ${label}`}
                            formatter={(value) =>
                              `${Number(value).toFixed(1)}%`
                            }
                          />
                        }
                      />
                      <Bar
                        dataKey="percent"
                        fill="var(--color-percent)"
                        radius={[3, 3, 0, 0]}
                        isAnimationActive={false}
                      />
                    </BarChart>
                  </ChartContainer>
                ) : (
                  <div className="flex h-48 items-center justify-center rounded-lg border border-dashed text-sm text-muted-foreground">
                    Collecting samples…
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        </>
      )}
    </Page>
  )
}

export const Route = createFileRoute("/metrics")({
  validateSearch: metricsSearch,
  component: MetricsPage,
})
