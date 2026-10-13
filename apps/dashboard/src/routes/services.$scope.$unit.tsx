import { useQuery } from "@tanstack/react-query"
import { createFileRoute, Link } from "@tanstack/react-router"
import { ArrowLeftIcon } from "lucide-react"
import type * as React from "react"

import { AdminRequired } from "@/components/admin-access"
import { LogList } from "@/components/log-list"
import { ServiceActions } from "@/components/service-actions"
import { ServiceConfiguration } from "@/components/service-configuration"
import { ServiceOverride } from "@/components/service-override"
import { capitalize, StatusBadge, unitStatus } from "@/components/status-badge"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api, type LogEntry, type ServiceDetail } from "@/lib/api"
import { formatBytes, formatDateTime, formatRelative } from "@/lib/format"
import { QueryError } from "@/lib/page"
import { parseEnum } from "@/lib/search"

const tabs = ["overview", "logs", "file", "override"] as const

function ServiceDetailPage() {
  const { scope: rawScope, unit } = Route.useParams()
  const scope = rawScope === "user" ? "user" : "system"
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const tab = search.tab ?? "overview"
  const detail = useQuery({
    queryKey: ["service", scope, unit],
    queryFn: () =>
      api<ServiceDetail>(`/services/${scope}/${encodeURIComponent(unit)}`),
    refetchInterval: 5_000,
  })
  const logs = useQuery({
    queryKey: ["service-logs", scope, unit],
    queryFn: () =>
      api<{ items: LogEntry[] }>(
        `/logs?limit=200&unit=${encodeURIComponent(unit)}`
      ),
    enabled: tab === "logs",
    refetchInterval: tab === "logs" ? 10_000 : false,
  })

  const back = (
    <Button
      variant="ghost"
      size="sm"
      className="-ml-2 self-start"
      render={
        <Link
          to="/services"
          search={scope === "user" ? { scope: "user" } : {}}
        />
      }
    >
      <ArrowLeftIcon data-icon="inline-start" />
      Services
    </Button>
  )

  if (detail.isPending) {
    return (
      <main className="flex flex-1 flex-col gap-4 p-4 lg:p-6">
        {back}
        <Skeleton className="h-28" />
        <Skeleton className="h-80" />
      </main>
    )
  }
  if (detail.isError) {
    return (
      <main className="flex flex-1 flex-col gap-4 p-4 lg:p-6">
        {back}
        <QueryError
          error={detail.error}
          title={`Could not load ${unit}`}
          retry={() => void detail.refetch()}
        />
      </main>
    )
  }

  const item = detail.data
  const status = unitStatus(item.activeState, item.subState)
  const since = item.activeEnterTimestamp
    ? new Date(item.activeEnterTimestamp / 1000)
    : undefined
  const relations: [string, string[]][] = [
    ["Requires", item.requires],
    ["Wants", item.wants],
    ["Wanted by", item.wantedBy],
    ["After", item.after],
    ["Before", item.before],
    ["Conflicts", item.conflicts],
  ]

  return (
    <main className="@container/main flex min-w-0 flex-1 flex-col gap-4 p-4 lg:p-6">
      {back}
      <header className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-xl font-semibold tracking-tight">
              {item.name}
            </h2>
            <StatusBadge tone={status.tone}>{status.label}</StatusBadge>
            {item.fileState && (
              <Badge variant="outline" className="font-normal">
                {capitalize(item.fileState)}
              </Badge>
            )}
            {scope === "user" && <Badge variant="secondary">User unit</Badge>}
          </div>
          <p className="mt-1 text-sm text-muted-foreground">
            {item.description || "No description"}
          </p>
        </div>
        <ServiceActions unit={item} />
      </header>
      {scope === "system" && (
        <AdminRequired>
          Starting, stopping, and configuring system services requires
          administrative access.
        </AdminRequired>
      )}

      <Tabs
        value={tab}
        onValueChange={(next) =>
          navigate({
            search: {
              tab:
                next === "overview"
                  ? undefined
                  : (next as "logs" | "file" | "override"),
            },
            replace: true,
          })
        }
      >
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="logs">Logs</TabsTrigger>
          <TabsTrigger value="file">Unit file</TabsTrigger>
          <TabsTrigger value="override">Override</TabsTrigger>
        </TabsList>
        <TabsContent
          value="overview"
          className="grid gap-4 pt-2 @4xl/main:grid-cols-2"
        >
          <Card>
            <CardHeader>
              <CardTitle>Runtime</CardTitle>
            </CardHeader>
            <CardContent>
              <Facts
                facts={[
                  [
                    "State",
                    `${capitalize(item.activeState)} (${item.subState})`,
                  ],
                  [
                    "Since",
                    since ? (
                      <span key="since" title={formatDateTime(since)}>
                        {formatRelative(since)}
                      </span>
                    ) : (
                      "—"
                    ),
                  ],
                  [
                    "Main process",
                    item.mainPid ? (
                      <Link
                        key="main-process"
                        to="/processes/$pid"
                        params={{ pid: String(item.mainPid) }}
                        className="text-primary underline-offset-4 hover:underline"
                      >
                        {item.mainPid}
                      </Link>
                    ) : (
                      "—"
                    ),
                  ],
                  [
                    "Memory",
                    item.memoryCurrent ? formatBytes(item.memoryCurrent) : "—",
                  ],
                  ["Tasks", item.tasksCurrent || "—"],
                  [
                    "Unit file",
                    <span key="path" className="font-mono text-xs">
                      {item.path || "—"}
                    </span>,
                  ],
                ]}
              />
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>Dependencies</CardTitle>
              <CardDescription>
                Units this one is ordered or linked with
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              {relations.map(([label, values]) => (
                <div key={label} className="flex flex-col gap-1.5">
                  <p className="text-xs font-medium text-muted-foreground">
                    {label}
                  </p>
                  {values?.length ? (
                    <div className="flex flex-wrap gap-1.5">
                      {values.slice(0, 24).map((value) => (
                        <Link
                          key={value}
                          to="/services/$scope/$unit"
                          params={{ scope, unit: value }}
                          className="rounded-md bg-muted px-2 py-0.5 font-mono text-xs hover:bg-accent hover:text-accent-foreground"
                        >
                          {value}
                        </Link>
                      ))}
                      {values.length > 24 && (
                        <span className="text-xs text-muted-foreground">
                          +{values.length - 24} more
                        </span>
                      )}
                    </div>
                  ) : (
                    <p className="text-sm text-muted-foreground">None</p>
                  )}
                </div>
              ))}
            </CardContent>
          </Card>
        </TabsContent>
        <TabsContent value="logs" className="pt-2">
          <Card>
            <CardHeader>
              <CardTitle>Recent journal entries</CardTitle>
              <CardDescription>
                The latest 200 entries for this unit.{" "}
                <Link
                  to="/logs"
                  search={{ unit }}
                  className="text-primary underline-offset-4 hover:underline"
                >
                  Open in Logs
                </Link>
              </CardDescription>
            </CardHeader>
            <CardContent>
              {logs.isPending ? (
                <Skeleton className="h-64" />
              ) : logs.isError ? (
                <QueryError
                  error={logs.error}
                  retry={() => void logs.refetch()}
                />
              ) : logs.data.items.length ? (
                <LogList
                  items={logs.data.items}
                  showUnit={false}
                  className="max-h-[60vh] overflow-auto"
                />
              ) : (
                <Empty>
                  <EmptyHeader>
                    <EmptyTitle>No journal entries</EmptyTitle>
                    <EmptyDescription>
                      This unit has not logged anything you can read.
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              )}
            </CardContent>
          </Card>
        </TabsContent>
        <TabsContent value="file" className="pt-2">
          <ServiceConfiguration scope={scope} unit={unit} />
        </TabsContent>
        <TabsContent value="override" className="pt-2">
          {item.type === "service" || unit.endsWith(".service") ? (
            <ServiceOverride scope={scope} unit={unit} />
          ) : (
            <Empty className="border">
              <EmptyHeader>
                <EmptyTitle>Overrides apply to services</EmptyTitle>
                <EmptyDescription>
                  Tako manages drop-in overrides for .service units only.
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
        </TabsContent>
      </Tabs>
    </main>
  )
}

function Facts({ facts }: { facts: [string, React.ReactNode][] }) {
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2.5 text-sm">
      {facts.map(([label, value]) => (
        <div key={label} className="contents">
          <dt className="text-muted-foreground">{label}</dt>
          <dd className="min-w-0 truncate text-right">{value}</dd>
        </div>
      ))}
    </dl>
  )
}

export const Route = createFileRoute("/services/$scope/$unit")({
  validateSearch: (
    search: Record<string, unknown>
  ): { tab?: "logs" | "file" | "override" } => {
    const tab = parseEnum(search.tab, tabs, "overview")
    return { tab: tab === "overview" ? undefined : tab }
  },
  component: ServiceDetailPage,
})
