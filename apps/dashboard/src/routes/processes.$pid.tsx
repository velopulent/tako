import { useQuery } from "@tanstack/react-query"
import { createFileRoute, getRouteApi, Link } from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import {
  ActivityIcon,
  ArrowLeftIcon,
  BracesIcon,
  Clock3Icon,
  CpuIcon,
  HardDriveIcon,
  LayersIcon,
  MemoryStickIcon,
  NetworkIcon,
  ShieldAlertIcon,
  TerminalIcon,
  UserIcon,
} from "lucide-react"
import type * as React from "react"
import { DataTable, type DataTableFeatures } from "@/components/data-table"
import { ResourceHistoryCharts } from "@/components/process-resource-charts"
import { ProcessSignal } from "@/components/process-signal"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb"
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
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import {
  api,
  type ProcessDetails as ProcessDetailsData,
  type ProcessSocket,
} from "@/lib/api"
import { processDetailSearch } from "@/lib/search"

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

function StatCard({
  icon: Icon,
  label,
  value,
  hint,
}: {
  icon: React.ElementType
  label: string
  value: React.ReactNode
  hint?: string
}) {
  return (
    <Card size="sm">
      <CardContent className="flex items-center gap-3 py-3">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
          <Icon className="size-4" aria-hidden="true" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-xs text-muted-foreground">{label}</span>
          <span className="block truncate text-sm font-medium">{value}</span>
          {hint ? (
            <span className="block truncate text-xs text-muted-foreground">
              {hint}
            </span>
          ) : null}
        </span>
      </CardContent>
    </Card>
  )
}

function DetailSkeleton() {
  return (
    <main className="@container/main flex flex-1 flex-col gap-4 p-4 sm:gap-6 sm:p-6">
      <Skeleton className="h-5 w-48" />
      <Skeleton className="h-32" />
      <div className="grid gap-3 sm:grid-cols-2 @3xl/main:grid-cols-4">
        <Skeleton className="h-20" />
        <Skeleton className="h-20" />
        <Skeleton className="h-20" />
        <Skeleton className="h-20" />
      </div>
      <Skeleton className="h-64" />
    </main>
  )
}

const socketColumns: ColumnDef<DataTableFeatures, ProcessSocket>[] = [
  {
    accessorKey: "protocol",
    header: "Protocol",
    size: 88,
    cell: ({ row }) => (
      <Badge variant="outline" className="font-mono text-xs">
        {row.original.protocol}
      </Badge>
    ),
  },
  {
    accessorKey: "local",
    header: "Local",
    size: 200,
    cell: ({ row }) => (
      <span className="font-mono text-xs truncate" title={row.original.local}>
        {row.original.local}
      </span>
    ),
  },
  {
    accessorKey: "remote",
    header: "Remote",
    size: 200,
    cell: ({ row }) =>
      row.original.remote ? (
        <span
          className="font-mono text-xs truncate text-muted-foreground"
          title={row.original.remote}
        >
          {row.original.remote}
        </span>
      ) : (
        <span className="text-xs text-muted-foreground">—</span>
      ),
  },
  {
    accessorKey: "state",
    header: "State",
    size: 110,
    meta: { align: "end" },
    cell: ({ row }) =>
      row.original.state ? (
        <Badge variant="secondary" className="text-[10px] leading-none">
          {row.original.state}
        </Badge>
      ) : (
        <span className="text-xs text-muted-foreground">—</span>
      ),
  },
]

export function ProcessDetailPage() {
  const search = getRouteApi("/processes/$pid").useSearch()
  const params = getRouteApi("/processes/$pid").useParams() as {
    pid: string
  }
  const pid = Number(params.pid)
  const started = search.started ? Number(search.started) : undefined
  const hasStarted = Number.isFinite(started) && (started as number) > 0

  const query = useQuery({
    queryKey: ["process-details", pid, started],
    queryFn: () =>
      api<ProcessDetailsData>(
        `/processes/${pid}${hasStarted ? `?started=${started}` : ""}`
      ),
    retry: false,
    refetchInterval: 10_000,
  })

  const details = query.data
  const proc = details?.process

  if (Number.isNaN(pid) || pid < 1) {
    return (
      <main className="@container/main flex flex-1 flex-col gap-4 p-4 sm:gap-6 sm:p-6">
        <Alert variant="destructive">
          <AlertTitle>Invalid process</AlertTitle>
          <AlertDescription>Process ID in URL is invalid.</AlertDescription>
        </Alert>
        <Button variant="outline" size="sm" render={<Link to="/processes" />}>
          <ArrowLeftIcon data-icon="inline-start" />
          Back to processes
        </Button>
      </main>
    )
  }

  if (query.isPending) return <DetailSkeleton />

  if (query.isError) {
    const message = (query.error as Error).message
    const isNotFound = message.toLowerCase().includes("no longer exists")
    const isReused = message.toLowerCase().includes("different process")
    return (
      <main className="@container/main flex flex-1 flex-col gap-4 p-4 sm:gap-6 sm:p-6">
        <Breadcrumb>
          <BreadcrumbList>
            <BreadcrumbItem>
              <BreadcrumbLink render={<Link to="/processes" />}>
                Processes
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbPage>PID {pid}</BreadcrumbPage>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>
        <Alert variant="destructive">
          <AlertTitle>
            {isNotFound
              ? "Process no longer exists"
              : isReused
                ? "Process identity changed"
                : "Process details unavailable"}
          </AlertTitle>
          <AlertDescription>
            {message ||
              "Could not load process details. The process may have exited or PID was reused."}
            {isReused && hasStarted
              ? " The started identity in the URL no longer matches this PID."
              : null}
          </AlertDescription>
        </Alert>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" render={<Link to="/processes" />}>
            <ArrowLeftIcon data-icon="inline-start" />
            Back to processes
          </Button>
          <Button variant="outline" size="sm" onClick={() => query.refetch()}>
            Retry
          </Button>
        </div>
      </main>
    )
  }

  if (!details || !proc) {
    return (
      <main className="@container/main flex flex-1 flex-col gap-4 p-4 sm:gap-6 sm:p-6">
        <Alert variant="destructive">
          <AlertTitle>Process unavailable</AlertTitle>
          <AlertDescription>Could not load process details.</AlertDescription>
        </Alert>
      </main>
    )
  }

  return (
    <main className="@container/main flex flex-1 flex-col gap-4 p-4 sm:gap-6 sm:p-6">
      <Breadcrumb>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink render={<Link to="/processes" />}>
              Processes
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <BreadcrumbPage>
              {proc.program} · PID {proc.pid}
            </BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>

      {/* Header */}
      <Card>
        <CardHeader>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <CardTitle className="truncate text-lg sm:text-xl">
                  {proc.program}
                </CardTitle>
                <Badge variant="outline" className="font-mono">
                  PID {proc.pid}
                </Badge>
                <Badge
                  variant={proc.state === "R" ? "secondary" : "outline"}
                  className="capitalize"
                >
                  <ActivityIcon data-icon="inline-start" />
                  {proc.state || "unknown"}
                </Badge>
                {proc.permissionDenied ? (
                  <Badge variant="destructive">
                    <ShieldAlertIcon data-icon="inline-start" />
                    Restricted
                  </Badge>
                ) : null}
              </div>
              <CardDescription className="mt-1.5 flex items-center gap-1.5 font-mono text-xs">
                <TerminalIcon className="size-3 shrink-0" aria-hidden="true" />
                <span className="min-w-0 truncate">{proc.command}</span>
              </CardDescription>
              <div className="mt-2 flex flex-wrap gap-2 text-xs text-muted-foreground">
                <span className="inline-flex items-center gap-1">
                  <UserIcon className="size-3" aria-hidden="true" />
                  {proc.user}
                </span>
                <span className="inline-flex items-center gap-1">
                  <Clock3Icon className="size-3" aria-hidden="true" />
                  start {proc.started}
                </span>
                <span className="inline-flex items-center gap-1">
                  <LayersIcon className="size-3" aria-hidden="true" />
                  PPID {proc.ppid} · {proc.threads} thread
                  {proc.threads === 1 ? "" : "s"}
                </span>
              </div>
            </div>
            <div className="flex shrink-0 gap-2">
              <Button
                variant="outline"
                size="sm"
                render={<Link to="/processes" />}
              >
                <ArrowLeftIcon data-icon="inline-start" />
                Back
              </Button>
            </div>
          </div>
        </CardHeader>
        {(details.process.permissionDenied || details.accessIssues?.length) && (
          <CardContent className="pt-0">
            <Alert>
              <ShieldAlertIcon aria-hidden="true" />
              <AlertTitle>Some process data is restricted</AlertTitle>
              <AlertDescription>
                {details.process.reason ?? details.accessIssues?.join(" ")}
              </AlertDescription>
            </Alert>
          </CardContent>
        )}
      </Card>

      {/* Stat grid */}
      <div className="grid gap-3 grid-cols-2 @3xl/main:grid-cols-4">
        <StatCard
          icon={CpuIcon}
          label="CPU time"
          value={`${proc.cpuTime.toFixed(1)}s`}
          hint={`${proc.threads} thread${proc.threads === 1 ? "" : "s"} · ${proc.state}`}
        />
        <StatCard
          icon={MemoryStickIcon}
          label="Memory"
          value={bytes(proc.memory)}
        />
        <StatCard
          icon={HardDriveIcon}
          label="Disk I/O"
          value={
            proc.ioDenied
              ? "—"
              : `R ${bytes(proc.diskRead)} · W ${bytes(proc.diskWrite)}`
          }
          hint={
            proc.ioDenied
              ? "I/O counters require elevated read access"
              : "Cumulative read/write"
          }
        />
        <StatCard
          icon={NetworkIcon}
          label="Network"
          value={
            proc.networkRxRate !== undefined || proc.networkTxRate !== undefined
              ? `Rx ${bytes(proc.networkRxRate ?? 0)}/s`
              : "—"
          }
          hint={
            proc.networkTxRate !== undefined
              ? `Tx ${bytes(proc.networkTxRate ?? 0)}/s`
              : "eBPF collector optional"
          }
        />
      </div>

      {/* Main two-column layout */}
      <div className="grid gap-4 sm:gap-6 @5xl/main:grid-cols-3">
        {/* Left: primary details span 2 */}
        <div className="flex flex-col gap-4 sm:gap-6 @5xl/main:col-span-2">
          {/* Identity + command */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <BracesIcon
                  className="size-4 text-muted-foreground"
                  aria-hidden="true"
                />
                Identity &amp; command
              </CardTitle>
              <CardDescription>
                PID start identity guards against PID reuse.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <dl className="grid gap-x-4 gap-y-3 text-sm grid-cols-1 sm:grid-cols-[9rem_1fr]">
                <dt className="font-medium text-muted-foreground">Program</dt>
                <dd className="font-mono text-sm break-all">{proc.program}</dd>
                <dt className="font-medium text-muted-foreground">Command</dt>
                <dd className="break-all font-mono text-xs">{proc.command}</dd>
                <dt className="font-medium text-muted-foreground">User</dt>
                <dd className="truncate">{proc.user}</dd>
                <dt className="font-medium text-muted-foreground">
                  PID / PPID
                </dt>
                <dd className="font-mono">
                  {proc.pid} / {proc.ppid}
                </dd>
                <dt className="font-medium text-muted-foreground">Started</dt>
                <dd className="font-mono text-xs sm:text-sm break-all">
                  {proc.started}
                </dd>
                <dt className="font-medium text-muted-foreground">State</dt>
                <dd>
                  <Badge variant="outline">{proc.state || "—"}</Badge>
                </dd>
                {proc.reason ? (
                  <>
                    <dt className="font-medium text-muted-foreground">Note</dt>
                    <dd className="text-muted-foreground break-words">
                      {proc.reason}
                    </dd>
                  </>
                ) : null}
              </dl>
              <Separator />
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  render={
                    <Link
                      to="/processes/$pid"
                      params={{ pid: String(proc.ppid) }}
                      search={{ started: undefined }}
                    />
                  }
                  disabled={proc.ppid <= 1}
                >
                  View parent (PID {proc.ppid})
                </Button>
              </div>
            </CardContent>
          </Card>

          {/* Relationships */}
          <Card>
            <CardHeader>
              <CardTitle>Relationships</CardTitle>
              <CardDescription>Parent and direct children.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div>
                <p className="mb-1.5 text-sm font-medium">Parent</p>
                {details.parent ? (
                  <Link
                    to="/processes/$pid"
                    params={{ pid: String(details.parent.pid) }}
                    search={{ started: String(details.parent.started) }}
                    className="inline-flex max-w-full flex-wrap items-center gap-2 rounded-lg border px-3 py-2 text-sm hover:bg-muted"
                  >
                    <span className="font-medium truncate">
                      {details.parent.program}
                    </span>
                    <Badge
                      variant="outline"
                      className="font-mono text-xs shrink-0"
                    >
                      PID {details.parent.pid}
                    </Badge>
                    <span className="text-xs text-muted-foreground truncate">
                      {details.parent.user} · {details.parent.state}
                    </span>
                  </Link>
                ) : (
                  <p className="text-sm text-muted-foreground">
                    No parent (init or reaped).
                  </p>
                )}
              </div>
              <Separator />
              <div>
                <p className="mb-1.5 text-sm font-medium">
                  Children ({(details.children ?? []).length})
                </p>
                {(details.children ?? []).length ? (
                  <ul className="flex flex-col gap-2">
                    {(details.children ?? []).map((child) => (
                      <li key={`${child.pid}:${child.started}`}>
                        <Link
                          to="/processes/$pid"
                          params={{ pid: String(child.pid) }}
                          search={{ started: String(child.started) }}
                          className="flex flex-col gap-1 rounded-lg border px-3 py-2 text-sm hover:bg-muted sm:flex-row sm:flex-wrap sm:items-center sm:gap-2"
                        >
                          <span className="font-medium truncate">
                            {child.program}
                          </span>
                          <Badge
                            variant="outline"
                            className="font-mono text-xs w-fit"
                          >
                            PID {child.pid}
                          </Badge>
                          <span className="text-xs text-muted-foreground">
                            {child.user} · {child.state} · {child.threads} thr
                          </span>
                          <span className="hidden truncate font-mono text-xs text-muted-foreground @2xl/main:block ml-auto">
                            {child.command}
                          </span>
                        </Link>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-sm text-muted-foreground">No children.</p>
                )}
              </div>
            </CardContent>
          </Card>

          {/* Resource history */}
          <Card>
            <CardHeader>
              <CardTitle>Resource history</CardTitle>
              <CardDescription>
                Sampled from the process tracker after inventory refresh.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <ResourceHistoryCharts history={details.history ?? []} />
            </CardContent>
          </Card>
        </div>

        {/* Right rail */}
        <div className="flex flex-col gap-4 sm:gap-6">
          <ProcessSignal process={proc} />

          <Card>
            <CardHeader>
              <CardTitle>Cgroup</CardTitle>
              <CardDescription>Control group membership.</CardDescription>
            </CardHeader>
            <CardContent>
              <pre className="max-h-48 overflow-auto rounded-lg bg-muted p-3 text-xs whitespace-pre-wrap break-all">
                {details.cgroup || "Unavailable"}
              </pre>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>
                Open files ({(details.openFiles ?? []).length})
              </CardTitle>
              <CardDescription>Readable descriptors.</CardDescription>
            </CardHeader>
            <CardContent>
              {(details.openFiles ?? []).length ? (
                <ul className="max-h-64 space-y-1 overflow-auto font-mono text-xs">
                  {(details.openFiles ?? []).map((file, index) => (
                    <li
                      // biome-ignore lint/suspicious/noArrayIndexKey: the same path can appear multiple times (one row per file descriptor), so the index disambiguates duplicates
                      key={`${file}:${index}`}
                      className="truncate rounded px-1 py-0.5 hover:bg-muted"
                      title={file}
                    >
                      {file}
                    </li>
                  ))}
                </ul>
              ) : (
                <Empty className="border border-dashed py-6">
                  <EmptyHeader>
                    <EmptyTitle>No readable open files</EmptyTitle>
                    <EmptyDescription>
                      Permission restrictions or a short-lived process may hide
                      this data.
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Sockets ({(details.sockets ?? []).length})</CardTitle>
              <CardDescription>Active network sockets.</CardDescription>
            </CardHeader>
            <CardContent>
              {(details.sockets ?? []).length ? (
                <DataTable
                  data={(details.sockets ?? []) as ProcessSocket[]}
                  columns={socketColumns}
                  height="18rem"
                  searchPlaceholder="Search sockets"
                />
              ) : (
                <p className="text-sm text-muted-foreground">
                  No readable sockets.
                </p>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </main>
  )
}

export const Route = createFileRoute("/processes/$pid")({
  validateSearch: processDetailSearch,
  component: ProcessDetailPage,
})
