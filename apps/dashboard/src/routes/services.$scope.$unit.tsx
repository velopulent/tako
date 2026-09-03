import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  createFileRoute,
  getRouteApi,
  Link,
  useNavigate,
  useParams,
} from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import * as React from "react"
import { DataTable, type DataTableFeatures } from "@/components/data-table"
import { ServiceActions } from "@/components/service-actions"
import { ServiceConfiguration } from "@/components/service-configuration"
import { ServiceOverride } from "@/components/service-override"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import {
  api,
  type LogEntry,
  type ServiceDetail,
  type SessionResponse,
} from "@/lib/api"
import { qSearch } from "@/lib/search"
import type { ServiceActionName } from "@/lib/service-actions"

const bytes = (value: number) =>
  value
    ? new Intl.NumberFormat(undefined, {
        notation: "compact",
        style: "unit",
        unit: "byte",
        unitDisplay: "narrow",
      }).format(value)
    : "—"

export function ServiceDetailPage() {
  const { scope, unit } = useParams({ strict: false }) as {
    scope: string
    unit: string
  }
  const search = getRouteApi("/services/$scope/$unit").useSearch()
  const navigate = useNavigate({ from: "/services/$scope/$unit" })
  const client = useQueryClient()
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const detail = useQuery({
    queryKey: ["service", scope, unit],
    queryFn: () =>
      api<ServiceDetail>(`/services/${scope}/${encodeURIComponent(unit)}`),
    refetchInterval: 10_000,
  })
  const logs = useQuery({
    queryKey: ["service-logs", unit],
    queryFn: () =>
      api<{ items: LogEntry[] }>(
        `/logs?limit=300&unit=${encodeURIComponent(unit)}`
      ),
  })
  const action = useMutation({
    mutationFn: (name: ServiceActionName) =>
      api<ServiceDetail>(
        `/services/${scope}/${encodeURIComponent(unit)}/actions`,
        {
          method: "POST",
          headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
          body: JSON.stringify({ action: name }),
        }
      ),
    onSuccess: (value) => client.setQueryData(["service", scope, unit], value),
  })
  if (detail.isPending)
    return (
      <main className="p-6">
        <Skeleton className="h-96" />
      </main>
    )
  if (!detail.data)
    return (
      <main className="p-6">
        <Alert variant="destructive">
          <AlertTitle>Service unavailable</AlertTitle>
          <AlertDescription>Could not load unit details.</AlertDescription>
        </Alert>
      </main>
    )
  const item = detail.data
  const relations: [string, string[]][] = [
    ["Requires", item.requires],
    ["Wants", item.wants],
    ["Wanted by", item.wantedBy],
    ["Conflicts", item.conflicts],
    ["Before", item.before],
    ["After", item.after],
  ]
  const logColumns: ColumnDef<DataTableFeatures, LogEntry>[] = [
    {
      accessorKey: "timestamp",
      header: "Time",
      cell: ({ row }) => new Date(row.original.timestamp).toLocaleString(),
    },
    { accessorKey: "priority", header: "Priority" },
    {
      accessorKey: "message",
      header: "Message",
      size: 360,
      meta: { wrap: true },
      cell: ({ row }) => (
        <span className="font-mono text-xs whitespace-normal">
          {row.original.message}
        </span>
      ),
    },
  ]
  return (
    <main className="flex flex-1 flex-col gap-6 p-4 lg:p-6">
      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <CardTitle>{item.description || item.name}</CardTitle>
              <CardDescription>
                {item.name} · {scope}
              </CardDescription>
            </div>
            <Badge
              variant={item.activeState === "active" ? "secondary" : "outline"}
            >
              {item.activeState} / {item.subState}
            </Badge>
          </div>
        </CardHeader>
        <CardContent className="flex flex-col gap-5">
          {action.isError && (
            <Alert variant="destructive">
              <AlertTitle>Action failed</AlertTitle>
              <AlertDescription>{action.error.message}</AlertDescription>
            </Alert>
          )}
          <ServiceActions
            scope={scope}
            unit={unit}
            csrfToken={session.data?.csrfToken ?? ""}
            administrative={session.data?.administrative === true}
            pending={action.isPending}
            onAction={(name) => action.mutate(name)}
          />
          <dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[10rem_1fr]">
            <dt className="font-medium">Path</dt>
            <dd className="font-mono text-xs">{item.path || "—"}</dd>
            <dt className="font-medium">Main PID</dt>
            <dd>{item.mainPid || "—"}</dd>
            <dt className="font-medium">Memory</dt>
            <dd>{bytes(item.memoryCurrent)}</dd>
            <dt className="font-medium">Tasks</dt>
            <dd>{item.tasksCurrent || "—"}</dd>
            {relations.map(([label, values]) => (
              <React.Fragment key={label}>
                <dt className="font-medium">{label}</dt>
                <dd className="flex flex-wrap gap-2">
                  {values?.length
                    ? values.map((value) => (
                        <Link
                          key={value}
                          to="/services/$scope/$unit"
                          params={{ scope, unit: value }}
                          className="text-primary underline-offset-4 hover:underline"
                        >
                          {value}
                        </Link>
                      ))
                    : "—"}
                </dd>
              </React.Fragment>
            ))}
          </dl>
        </CardContent>
      </Card>
      <ServiceConfiguration scope={scope} unit={unit} />
      <ServiceOverride
        scope={scope}
        unit={unit}
        csrfToken={session.data?.csrfToken ?? ""}
        administrative={session.data?.administrative === true}
      />
      <Card>
        <CardHeader>
          <CardTitle>Service logs</CardTitle>
          <CardDescription>
            Recent journal entries for this unit.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <DataTable
            data={logs.data?.items ?? []}
            columns={logColumns}
            height="45vh"
            search={search.q}
            onSearchChange={(q) =>
              navigate({
                search: (previous) => ({ ...previous, q: q || undefined }),
                replace: true,
              })
            }
          />
        </CardContent>
      </Card>
    </main>
  )
}

export const Route = createFileRoute("/services/$scope/$unit")({
  validateSearch: qSearch,
  component: ServiceDetailPage,
})
