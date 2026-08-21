import * as React from "react"
import { Link, getRouteApi } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import {
  ActivityIcon,
  ArrowLeftIcon,
  BracesIcon,
  Clock3Icon,
  CpuIcon,
  FileTextIcon,
  HardDriveIcon,
  LayersIcon,
  NetworkIcon,
  ShieldAlertIcon,
  TerminalIcon,
  UserIcon,
} from "lucide-react"

import { api, type ProcessDetails as ProcessDetailsData } from "@/lib/api"
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
import { ProcessSignal } from "@/components/process-signal"

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
    <main className="flex flex-1 flex-col gap-6 p-4 lg:p-6">
      <Skeleton className="h-5 w-48" />
      <Skeleton className="h-32" />
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Skeleton className="h-24" />
        <Skeleton className="h-24" />
        <Skeleton className="h-24" />
        <Skeleton className="h-24" />
      </div>
      <Skeleton className="h-64" />
    </main>
  )
}

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
      <main className="flex flex-1 flex-col gap-6 p-4 lg:p-6">
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
      <main className="flex flex-1 flex-col gap-6 p-4 lg:p-6">
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
      <main className="flex flex-1 flex-col gap-6 p-4 lg:p-6">
        <Alert variant="destructive">
          <AlertTitle>Process unavailable</AlertTitle>
          <AlertDescription>Could not load process details.</AlertDescription>
        </Alert>
      </main>
    )
  }

  return (
    <main className="flex flex-1 flex-col gap-6 p-4 lg:p-6">
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
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <CardTitle className="truncate text-xl">
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
              <CardDescription className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-xs">
                <span className="inline-flex items-center gap-1">
                  <TerminalIcon className="size-3" aria-hidden="true" />
                  <span className="truncate">{proc.command}</span>
                </span>
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
              <Button variant="outline" size="sm" render={<Link to="/processes" />}>
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
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          icon={CpuIcon}
          label="CPU time"
          value={`${proc.cpuTime.toFixed(1)}s`}
          hint={`${proc.threads} thread${proc.threads === 1 ? "" : "s"} · ${proc.state}`}
        />
        <StatCard
          icon={HardDriveIcon}
          label="Memory"
          value={bytes(proc.memory)}
          hint={`Virtual ${bytes(proc.virtualMemory)}`}
        />
        <StatCard
          icon={FileTextIcon}
          label="Disk I/O"
          value={`R ${bytes(proc.diskRead)} · W ${bytes(proc.diskWrite)}`}
          hint="Cumulative read/write"
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
      <div className="grid gap-6 lg:grid-cols-3">
        {/* Left: primary details span 2 */}
        <div className="flex flex-col gap-6 lg:col-span-2">
          {/* Identity + command */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <BracesIcon className="size-4 text-muted-foreground" aria-hidden="true" />
                Identity & command
              </CardTitle>
              <CardDescription>
                PID start identity guards against PID reuse.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <dl className="grid gap-x-4 gap-y-3 text-sm sm:grid-cols-[9rem_1fr]">
                <dt className="font-medium text-muted-foreground">Program</dt>
                <dd className="font-mono text-sm">{proc.program}</dd>
                <dt className="font-medium text-muted-foreground">Command</dt>
                <dd className="break-all font-mono text-xs">{proc.command}</dd>
                <dt className="font-medium text-muted-foreground">User</dt>
                <dd>{proc.user}</dd>
                <dt className="font-medium text-muted-foreground">PID / PPID</dt>
                <dd className="font-mono">
                  {proc.pid} / {proc.ppid}
                </dd>
                <dt className="font-medium text-muted-foreground">Started</dt>
                <dd className="font-mono">{proc.started}</dd>
                <dt className="font-medium text-muted-foreground">State</dt>
                <dd>
                  <Badge variant="outline">{proc.state || "—"}</Badge>
                </dd>
                {proc.reason ? (
                  <>
                    <dt className="font-medium text-muted-foreground">Note</dt>
                    <dd className="text-muted-foreground">{proc.reason}</dd>
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
                    className="inline-flex items-center gap-2 rounded-lg border px-3 py-2 text-sm hover:bg-muted"
                  >
                    <span className="font-medium">{details.parent.program}</span>
                    <Badge variant="outline" className="font-mono text-xs">
                      PID {details.parent.pid}
                    </Badge>
                    <span className="text-xs text-muted-foreground">
                      {details.parent.user} · {details.parent.state}
                    </span>
                  </Link>
                ) : (
                  <p className="text-sm text-muted-foreground">No parent (init or reaped).</p>
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
                          className="flex flex-wrap items-center gap-2 rounded-lg border px-3 py-2 text-sm hover:bg-muted"
                        >
                          <span className="font-medium">{child.program}</span>
                          <Badge variant="outline" className="font-mono text-xs">
                            PID {child.pid}
                          </Badge>
                          <span className="text-xs text-muted-foreground">
                            {child.user} · {child.state} · {child.threads} thr
                          </span>
                          <span className="ml-auto hidden truncate font-mono text-xs text-muted-foreground sm:block">
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
              {(details.history ?? []).length ? (
                <div className="overflow-x-auto">
                  <table className="w-full text-xs">
                    <thead>
                      <tr className="border-b text-left text-muted-foreground">
                        <th className="px-2 py-1.5 font-medium">Time</th>
                        <th className="px-2 py-1.5 font-medium">CPU</th>
                        <th className="px-2 py-1.5 font-medium">Memory</th>
                        <th className="px-2 py-1.5 font-medium">Virtual</th>
                        <th className="px-2 py-1.5 font-medium">Read</th>
                        <th className="px-2 py-1.5 font-medium">Write</th>
                      </tr>
                    </thead>
                    <tbody>
                      {(details.history ?? []).map((sample) => (
                        <tr
                          key={sample.timestamp}
                          className="border-b last:border-0 hover:bg-muted/50"
                        >
                          <td className="px-2 py-1.5 whitespace-nowrap">
                            {new Date(sample.timestamp).toLocaleString()}
                          </td>
                          <td className="px-2 py-1.5 tabular-nums">
                            {sample.cpuTime.toFixed(1)}s
                          </td>
                          <td className="px-2 py-1.5 tabular-nums">{bytes(sample.memory)}</td>
                          <td className="px-2 py-1.5 tabular-nums">
                            {bytes(sample.virtualMemory)}
                          </td>
                          <td className="px-2 py-1.5 tabular-nums">{bytes(sample.diskRead)}</td>
                          <td className="px-2 py-1.5 tabular-nums">{bytes(sample.diskWrite)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : (
                <Empty className="border border-dashed">
                  <EmptyHeader>
                    <EmptyTitle>No history yet</EmptyTitle>
                    <EmptyDescription>
                      History starts after the next inventory refresh.
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              )}
            </CardContent>
          </Card>
        </div>

        {/* Right rail */}
        <div className="flex flex-col gap-6">
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
              <CardTitle>Open files ({(details.openFiles ?? []).length})</CardTitle>
              <CardDescription>Readable descriptors.</CardDescription>
            </CardHeader>
            <CardContent>
              {(details.openFiles ?? []).length ? (
                <ul className="max-h-64 space-y-1 overflow-auto font-mono text-xs">
                  {(details.openFiles ?? []).map((file, index) => (
                    <li
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
                      Permission restrictions or a short-lived process may hide this data.
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
                <div className="flex flex-col gap-2">
                  {(details.sockets ?? []).map((socket, index) => (
                    <div
                      key={`${socket.protocol}:${socket.local}:${index}`}
                      className="flex flex-wrap items-center gap-2 rounded-lg border px-2.5 py-2 text-xs"
                    >
                      <Badge variant="outline" className="shrink-0">
                        {socket.protocol}
                      </Badge>
                      <span className="min-w-0 flex-1 truncate font-mono">{socket.local}</span>
                      {socket.remote ? (
                        <span className="font-mono text-muted-foreground">
                          → {socket.remote}
                        </span>
                      ) : null}
                      {socket.state ? (
                        <Badge variant="secondary" className="ml-auto shrink-0 text-[10px]">
                          {socket.state}
                        </Badge>
                      ) : null}
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">No readable sockets.</p>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </main>
  )
}
