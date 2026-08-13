import type { MetricSample } from "@/lib/api"
import type { RefreshInterval } from "@/lib/monitoring"
import { MetricsCharts } from "@/components/metrics-chart"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"

type Direction = "storage" | "network"

const copy = {
  storage: {
    label: "storage",
    unavailable: "Storage telemetry unavailable",
    empty: "No storage telemetry yet",
  },
  network: {
    label: "network",
    unavailable: "Network telemetry unavailable",
    empty: "No network telemetry yet",
  },
} satisfies Record<Direction, Record<string, string>>

function DirectionalMetrics({
  direction,
  samples,
  interval,
  pending = false,
  error = false,
}: {
  direction: Direction
  samples: MetricSample[]
  interval: RefreshInterval
  pending?: boolean
  error?: boolean
}) {
  const text = copy[direction]
  if (pending) {
    return (
      <div role="status" className="space-y-3">
        <span className="sr-only">Loading {text.label} telemetry</span>
        <Skeleton className="h-72 w-full" />
      </div>
    )
  }
  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{text.unavailable}</AlertTitle>
        <AlertDescription>
          The metrics adapter did not return a usable history.
        </AlertDescription>
      </Alert>
    )
  }
  if (!samples.length) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>{text.empty}</EmptyTitle>
          <EmptyDescription>
            New samples will appear when the host reports {text.label} activity.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  return (
    <MetricsCharts
      initialSamples={samples}
      interval={interval}
      scope={direction}
    />
  )
}

export function StorageMetrics({
  samples,
  interval,
  pending,
  error,
}: {
  samples: MetricSample[]
  interval: RefreshInterval
  pending?: boolean
  error?: boolean
}) {
  return (
    <DirectionalMetrics
      direction="storage"
      samples={samples}
      interval={interval}
      pending={pending}
      error={error}
    />
  )
}

export function NetworkMetrics({
  samples,
  interval,
  pending,
  error,
}: {
  samples: MetricSample[]
  interval: RefreshInterval
  pending?: boolean
  error?: boolean
}) {
  return (
    <DirectionalMetrics
      direction="network"
      samples={samples}
      interval={interval}
      pending={pending}
      error={error}
    />
  )
}
