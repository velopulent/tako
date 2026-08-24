import * as React from "react"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
import { RefreshSelect } from "@/components/refresh-select"
import { Skeleton } from "@/components/ui/skeleton"
import { useMonitoringPreference } from "@/hooks/use-monitoring-preference"
import { usePreference } from "@/hooks/use-preference"
import { refreshIntervals, type RefreshInterval } from "@/lib/monitoring"

export const bytes = (value: number) => {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"]
  let amount = value,
    index = 0
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024
    index++
  }
  return `${amount.toFixed(index ? 1 : 0)} ${units[index]}`
}

const intervalMs = (value: RefreshInterval): number | false => {
  const result = refreshIntervals.find((item) => item.value === value)?.milliseconds
  return typeof result === "number" ? result : false
}

export function usePageInterval(page: string) {
  const defaultValue = useMonitoringPreference().data?.defaultInterval ?? "1m"
  const [value, setValue] = usePreference<RefreshInterval>(
    "current",
    `interval:${page}`,
    defaultValue
  )
  return { value, setValue, milliseconds: intervalMs(value) }
}

export function Page({
  description,
  interval,
  children,
}: {
  description: string
  interval?: ReturnType<typeof usePageInterval>
  children: React.ReactNode
}) {
  return (
    <main className="@container/main flex flex-1 flex-col gap-6 p-4 lg:p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <p className="text-sm text-muted-foreground">{description}</p>
        {interval && (
          <RefreshSelect value={interval.value} onChange={interval.setValue} />
        )}
      </div>
      {children}
    </main>
  )
}

export function State({
  query,
  empty,
  children,
}: {
  query: { isPending: boolean; isError: boolean }
  empty: boolean
  children: React.ReactNode
}) {
  if (query.isPending) return <Skeleton className="h-72" />
  if (query.isError)
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load data</AlertTitle>
        <AlertDescription>System adapter returned an error.</AlertDescription>
      </Alert>
    )
  if (empty)
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>No results</EmptyTitle>
          <EmptyDescription>No matching resources found.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  return children
}
