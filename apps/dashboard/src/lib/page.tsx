import { CircleAlertIcon, RotateCcwIcon } from "lucide-react"
import type * as React from "react"
import { AdminRequired } from "@/components/admin-access"
import { RefreshSelect } from "@/components/refresh-select"
import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { useMonitoringPreference } from "@/hooks/use-monitoring-preference"
import { usePreference } from "@/hooks/use-preference"
import { APIError } from "@/lib/api"
import { type RefreshInterval, refreshIntervals } from "@/lib/monitoring"

export { formatBytes as bytes } from "@/lib/format"

const intervalMs = (value: RefreshInterval): number | false => {
  const result = refreshIntervals.find(
    (item) => item.value === value
  )?.milliseconds
  return typeof result === "number" ? result : false
}

export function usePageInterval(page: string) {
  const defaultValue = useMonitoringPreference().data?.defaultInterval ?? "5s"
  const [value, setValue] = usePreference<RefreshInterval>(
    "current",
    `interval:${page}`,
    defaultValue
  )
  return { value, setValue, milliseconds: intervalMs(value) }
}

// Page lays out a route: a one-line description with page-level actions,
// then the content. The page title lives in the site header.
export function Page({
  description,
  interval,
  actions,
  children,
}: {
  description?: React.ReactNode
  interval?: ReturnType<typeof usePageInterval>
  actions?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <main className="@container/main flex min-w-0 flex-1 flex-col gap-6 p-4 lg:p-6">
      {(description || interval || actions) && (
        <div className="flex flex-wrap items-center justify-between gap-3">
          {description ? (
            <p className="min-w-0 max-w-prose text-sm text-muted-foreground">
              {description}
            </p>
          ) : (
            <span />
          )}
          <div className="flex flex-wrap items-center gap-2">
            {actions}
            {interval && (
              <RefreshSelect
                value={interval.value}
                onChange={interval.setValue}
              />
            )}
          </div>
        </div>
      )}
      {children}
    </main>
  )
}

// QueryError explains a failed request: an unlock prompt when administrative
// access is missing, otherwise the server's message with a retry.
export function QueryError({
  error,
  retry,
  title = "Could not load this data",
}: {
  error: unknown
  retry?: () => void
  title?: string
}) {
  if (
    error instanceof APIError &&
    error.code === "administrative-access-required"
  ) {
    return <AdminRequired />
  }
  const detail =
    error instanceof APIError &&
    error.message &&
    !error.message.startsWith("API request failed")
      ? error.message
      : "The host did not return a usable response."
  return (
    <Alert variant="destructive">
      <CircleAlertIcon />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{detail}</AlertDescription>
      {retry && (
        <AlertAction>
          <Button size="sm" variant="outline" onClick={retry}>
            <RotateCcwIcon data-icon="inline-start" />
            Retry
          </Button>
        </AlertAction>
      )}
    </Alert>
  )
}

export function State({
  query,
  empty,
  emptyTitle = "Nothing here yet",
  emptyDescription = "No matching items were found.",
  children,
}: {
  query: {
    isPending: boolean
    isError: boolean
    error?: unknown
    refetch?: () => unknown
  }
  empty: boolean
  emptyTitle?: string
  emptyDescription?: string
  children: React.ReactNode
}) {
  if (query.isPending) return <Skeleton className="h-72" />
  if (query.isError)
    return (
      <QueryError
        error={query.error}
        retry={query.refetch ? () => void query.refetch?.() : undefined}
      />
    )
  if (empty)
    return (
      <Empty className="border">
        <EmptyHeader>
          <EmptyTitle>{emptyTitle}</EmptyTitle>
          <EmptyDescription>{emptyDescription}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  return children
}
