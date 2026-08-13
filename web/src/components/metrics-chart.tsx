import * as React from "react"
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts"

import type { MetricSample } from "@/lib/api"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart"

const config = {
  cpuPercent: { label: "CPU", color: "var(--chart-2)" },
  memoryPercent: { label: "Memory", color: "var(--chart-4)" },
} satisfies ChartConfig

type ChartSample = MetricSample & { memoryPercent: number }

function transform(sample: MetricSample): ChartSample {
  return { ...sample, memoryPercent: sample.memoryTotal ? (sample.memoryUsed / sample.memoryTotal) * 100 : 0 }
}

export function MetricsChart({ initialSamples }: { initialSamples: MetricSample[] }) {
  const [liveSamples, setLiveSamples] = React.useState<ChartSample[]>([])
  const samples = React.useMemo(
    () => [...initialSamples.map(transform), ...liveSamples].slice(-450),
    [initialSamples, liveSamples]
  )

  React.useEffect(() => {
    const source = new EventSource("/api/v1/metrics/stream")
    source.addEventListener("metric", (event) => {
      const next = transform(JSON.parse((event as MessageEvent<string>).data) as MetricSample)
      setLiveSamples((current) => [...current.slice(-449), next])
    })
    return () => source.close()
  }, [])

  return (
    <Card>
      <CardHeader>
        <CardTitle>Live utilization</CardTitle>
        <CardDescription>CPU and memory · 2-second samples · 15-minute in-memory history</CardDescription>
      </CardHeader>
      <CardContent>
        <ChartContainer config={config} className="aspect-auto h-72 w-full">
          <AreaChart data={samples} accessibilityLayer>
            <CartesianGrid vertical={false} />
            <XAxis dataKey="timestamp" tickLine={false} axisLine={false} minTickGap={48} tickFormatter={(value) => new Date(value).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })} />
            <YAxis domain={[0, 100]} tickLine={false} axisLine={false} tickFormatter={(value) => `${value}%`} width={38} />
            <ChartTooltip content={<ChartTooltipContent labelFormatter={(value) => new Date(value).toLocaleTimeString()} />} />
            <Area dataKey="memoryPercent" type="monotone" fill="var(--color-memoryPercent)" fillOpacity={0.12} stroke="var(--color-memoryPercent)" stackId="memory" />
            <Area dataKey="cpuPercent" type="monotone" fill="var(--color-cpuPercent)" fillOpacity={0.18} stroke="var(--color-cpuPercent)" />
          </AreaChart>
        </ChartContainer>
      </CardContent>
    </Card>
  )
}
