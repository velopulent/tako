import * as React from "react"
import { useNavigate } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import { RefreshCwIcon, TerminalIcon } from "lucide-react"

import {
  api,
  type InterfaceInfo,
  type LogEntry,
  type MetricSample,
  type MountInfo,
  type OperationReceipt,
  type ProcessInfo,
  type ServiceInfo,
  type TerminalStatus,
  type UpdateStatus,
  type UserInfo,
} from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { DataTable } from "@/components/data-table"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { MetricsCharts } from "@/components/metrics-chart"
import {
  NetworkMetrics,
  StorageMetrics,
} from "@/components/directional-metrics"
import { Progress } from "@/components/ui/progress"
import { RefreshSelect } from "@/components/refresh-select"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useMonitoringPreference } from "@/hooks/use-monitoring-preference"
import { usePreference } from "@/hooks/use-preference"
import { type RefreshInterval, refreshIntervals } from "@/lib/monitoring"
import { SettingsPage } from "@/routes/settings"
import { JobsPage } from "@/routes/jobs"
import { HostPage } from "@/routes/host"
import { JournalBrowser } from "@/components/journal-browser"

const bytes = (value: number) => {
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
  const result = refreshIntervals.find(
    (item) => item.value === value
  )?.milliseconds
  return typeof result === "number" ? result : false
}

function usePageInterval(page: string) {
  const defaultValue = useMonitoringPreference().data?.defaultInterval ?? "1m"
  const [value, setValue] = usePreference<RefreshInterval>(
    "current",
    `interval:${page}`,
    defaultValue
  )
  return { value, setValue, milliseconds: intervalMs(value) }
}
function Page({
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
function State({
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

export function ModulePage({ module }: { module: string }) {
  if (module === "metrics") return <MetricsPage />
  if (module === "processes") return <ProcessesPage />
  if (module === "services") return <ServicesPage />
  if (module === "storage") return <StoragePage />
  if (module === "network") return <NetworkPage />
  if (module === "logs") return <LogsPage />
  if (module === "settings") return <SettingsPage />
  if (module === "users") return <UsersPage />
  if (module === "updates") return <UpdatesPage />
  if (module === "operations") return <OperationsPage />
  if (module === "jobs") return <JobsPage />
  if (module === "host") return <HostPage />
  return <TerminalPage />
}

function MetricsPage() {
  const interval = usePageInterval("metrics")
  const [range, setRange] = React.useState("1h")
  const query = useQuery({
    queryKey: ["metrics", range],
    queryFn: () => api<{ samples: MetricSample[] }>(`/metrics?range=${range}`),
  })
  return (
    <Page
      description="Live CPU, memory, storage, network, and process telemetry."
      interval={interval}
    >
      <Tabs value={range} onValueChange={setRange}>
        <TabsList>
          {["15m", "1h", "6h", "24h"].map((value) => (
            <TabsTrigger key={value} value={value}>
              {value}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <MetricsCharts
        initialSamples={query.data?.samples ?? []}
        interval={interval.value}
      />
      <ProcessesTable interval={interval} />
    </Page>
  )
}

const processColumns: ColumnDef<ProcessInfo>[] = [
  { accessorKey: "pid", header: "PID" },
  { accessorKey: "program", header: "Program" },
  { accessorKey: "user", header: "User" },
  {
    accessorKey: "state",
    header: "State",
    cell: ({ row }) => <Badge variant="outline">{row.original.state}</Badge>,
  },
  { accessorKey: "threads", header: "Threads" },
  {
    accessorKey: "cpuPercent",
    header: "CPU",
    cell: ({ row }) => `${(row.original.cpuPercent ?? 0).toFixed(1)}%`,
  },
  {
    accessorKey: "cpuTime",
    header: "CPU time",
    cell: ({ row }) => `${row.original.cpuTime.toFixed(1)}s`,
  },
  {
    accessorKey: "memory",
    header: "Memory",
    cell: ({ row }) => bytes(row.original.memory),
  },
  {
    accessorKey: "virtualMemory",
    header: "Virtual",
    cell: ({ row }) => bytes(row.original.virtualMemory),
  },
  {
    accessorKey: "diskRead",
    header: "Disk read",
    cell: ({ row }) => bytes(row.original.diskRead),
  },
  {
    accessorKey: "diskWrite",
    header: "Disk write",
    cell: ({ row }) => bytes(row.original.diskWrite),
  },
  {
    accessorKey: "diskReadRate",
    header: "Read/s",
    cell: ({ row }) => bytes(row.original.diskReadRate ?? 0),
  },
  {
    accessorKey: "diskWriteRate",
    header: "Write/s",
    cell: ({ row }) => bytes(row.original.diskWriteRate ?? 0),
  },
  {
    accessorKey: "command",
    header: "Command",
    cell: ({ row }) => (
      <span
        className="block max-w-xl truncate font-mono text-xs"
        title={row.original.command}
      >
        {row.original.command}
      </span>
    ),
  },
]
function ProcessesTable({
  interval,
}: {
  interval: ReturnType<typeof usePageInterval>
}) {
  const previous = React.useRef<{
    at: number
    items: Map<string, ProcessInfo>
  } | null>(null)
  const query = useQuery({
    queryKey: ["processes"],
    queryFn: async () => {
      const response = await api<{ items: ProcessInfo[] }>("/processes")
      const now = performance.now()
      const before = previous.current
      const seconds = before ? Math.max(0.001, (now - before.at) / 1000) : 1
      const items = response.items.map((item) => {
        const old = before?.items.get(`${item.pid}:${item.started}`)
        return {
          ...item,
          cpuPercent: old
            ? (Math.max(0, item.cpuTime - old.cpuTime) / seconds) * 100
            : 0,
          diskReadRate: old
            ? Math.max(0, item.diskRead - old.diskRead) / seconds
            : 0,
          diskWriteRate: old
            ? Math.max(0, item.diskWrite - old.diskWrite) / seconds
            : 0,
        }
      })
      previous.current = {
        at: now,
        items: new Map(
          response.items.map((item) => [`${item.pid}:${item.started}`, item])
        ),
      }
      return { items }
    },
    refetchInterval: interval.milliseconds,
  })
  const items = query.data?.items ?? []
  return (
    <State query={query} empty={!items.length}>
      <Card>
        <CardHeader>
          <CardTitle>Processes</CardTitle>
          <CardDescription>
            {items.length} processes · per-process network requires optional
            eBPF collector
          </CardDescription>
        </CardHeader>
        <CardContent>
          <DataTable
            data={items}
            columns={processColumns}
            searchPlaceholder="Search PID, program, command, or user"
            initialVisibility={{
              virtualMemory: false,
              diskRead: false,
              diskWrite: false,
              diskReadRate: false,
              diskWriteRate: false,
            }}
          />
        </CardContent>
      </Card>
    </State>
  )
}
function ProcessesPage() {
  const interval = usePageInterval("processes")
  return (
    <Page
      description="Sortable, filterable live process inventory."
      interval={interval}
    >
      <ProcessesTable interval={interval} />
    </Page>
  )
}

function ServicesPage() {
  const interval = usePageInterval("services")
  const [scope, setScope] = React.useState("system")
  const [type, setType] = React.useState("service")
  const [activeState, setActiveState] = React.useState("all")
  const [fileState, setFileState] = React.useState("all")
  const navigate = useNavigate()
  const query = useQuery({
    queryKey: ["services", scope, type],
    queryFn: () =>
      api<{ items: ServiceInfo[] }>(`/services?scope=${scope}&type=${type}`),
    refetchInterval: interval.milliseconds,
  })
  const columns: ColumnDef<ServiceInfo>[] = [
    { accessorKey: "name", header: "Unit" },
    { accessorKey: "description", header: "Description" },
    {
      accessorKey: "activeState",
      header: "Active",
      cell: ({ row }) => (
        <Badge
          variant={
            row.original.activeState === "active" ? "secondary" : "outline"
          }
        >
          {row.original.activeState}
        </Badge>
      ),
    },
    { accessorKey: "subState", header: "Detail" },
    { accessorKey: "loadState", header: "Load" },
    { accessorKey: "fileState", header: "File state" },
  ]
  const items = (query.data?.items ?? []).filter(
    (item) =>
      (activeState === "all" || item.activeState === activeState) &&
      (fileState === "all" || item.fileState === fileState)
  )
  return (
    <Page
      description="systemd units, state, relationships, logs, and lifecycle controls."
      interval={interval}
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Tabs value={type} onValueChange={setType}>
          <TabsList>
            {["service", "target", "socket", "timer", "path"].map((value) => (
              <TabsTrigger key={value} value={value}>
                {value[0].toUpperCase() + value.slice(1)}s
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <Tabs value={scope} onValueChange={setScope}>
          <TabsList>
            <TabsTrigger value="system">System</TabsTrigger>
            <TabsTrigger value="user">User</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>
      <State query={query} empty={!items.length}>
        <DataTable
          data={items}
          columns={columns}
          searchPlaceholder="Search units and descriptions"
          toolbar={
            <>
              <FilterSelect
                label="Active state"
                value={activeState}
                values={["all", "active", "inactive", "failed"]}
                onChange={setActiveState}
              />
              <FilterSelect
                label="File state"
                value={fileState}
                values={["all", "enabled", "disabled", "static", "masked"]}
                onChange={setFileState}
              />
            </>
          }
          onRowClick={(unit) =>
            navigate({
              to: "/services/$scope/$unit",
              params: { scope: unit.scope, unit: unit.name },
            })
          }
        />
      </State>
    </Page>
  )
}

function FilterSelect({
  label,
  value,
  values,
  onChange,
}: {
  label: string
  value: string
  values: string[]
  onChange: (value: string) => void
}) {
  return (
    <Select value={value} onValueChange={(next) => onChange(String(next))}>
      <SelectTrigger size="sm" aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          {values.map((item) => (
            <SelectItem key={item} value={item}>
              {item === "all" ? `All ${label.toLowerCase()}s` : item}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}

function StoragePage() {
  const interval = usePageInterval("storage")
  const metrics = useQuery({
    queryKey: ["storage-metrics"],
    queryFn: () => api<{ samples: MetricSample[] }>("/metrics?range=1h"),
  })
  const query = useQuery({
    queryKey: ["storage"],
    queryFn: () => api<{ items: MountInfo[] }>("/storage"),
    refetchInterval: interval.milliseconds,
  })
  const items = query.data?.items ?? []
  const columns: ColumnDef<MountInfo>[] = [
    { accessorKey: "target", header: "Mount" },
    { accessorKey: "source", header: "Device" },
    { accessorKey: "filesystem", header: "Filesystem" },
    {
      accessorKey: "used",
      header: "Used",
      cell: ({ row }) => bytes(row.original.used),
    },
    {
      accessorKey: "available",
      header: "Available",
      cell: ({ row }) => bytes(row.original.available),
    },
    {
      accessorKey: "percent",
      header: "Usage",
      cell: ({ row }) => (
        <div className="min-w-32">
          <Progress value={row.original.percent} />
          <span className="text-xs text-muted-foreground">
            {row.original.percent.toFixed(1)}%
          </span>
        </div>
      ),
    },
  ]
  return (
    <Page
      description="Filesystem capacity and block-device activity."
      interval={interval}
    >
      <StorageMetrics
        samples={metrics.data?.samples ?? []}
        interval={interval.value}
        pending={metrics.isPending}
        error={metrics.isError}
      />
      <State query={query} empty={!items.length}>
        <DataTable
          data={items}
          columns={columns}
          searchPlaceholder="Search mounts, devices, and filesystems"
          height="45vh"
        />
      </State>
    </Page>
  )
}

function NetworkPage() {
  const interval = usePageInterval("network")
  const metrics = useQuery({
    queryKey: ["network-metrics"],
    queryFn: () => api<{ samples: MetricSample[] }>("/metrics?range=1h"),
  })
  const query = useQuery({
    queryKey: ["network"],
    queryFn: () => api<{ items: InterfaceInfo[] }>("/network"),
    refetchInterval: interval.milliseconds,
  })
  const logs = useQuery({
    queryKey: ["logs", "network"],
    queryFn: () => api<{ items: LogEntry[] }>("/logs?limit=300"),
    refetchInterval: interval.milliseconds,
  })
  const items = query.data?.items ?? []
  const columns: ColumnDef<InterfaceInfo>[] = [
    { accessorKey: "name", header: "Device" },
    {
      accessorKey: "up",
      header: "State",
      cell: ({ row }) => (
        <Badge variant={row.original.up ? "secondary" : "outline"}>
          {row.original.up ? "Up" : "Down"}
        </Badge>
      ),
    },
    {
      accessorKey: "addresses",
      header: "Addresses",
      cell: ({ row }) => row.original.addresses.join(", ") || "—",
    },
    { accessorKey: "hardware", header: "MAC" },
    { accessorKey: "mtu", header: "MTU" },
    { accessorKey: "manager", header: "Manager" },
    {
      accessorKey: "rx",
      header: "Received",
      cell: ({ row }) => bytes(row.original.rx),
    },
    {
      accessorKey: "tx",
      header: "Sent",
      cell: ({ row }) => bytes(row.original.tx),
    },
  ]
  const networkLogs = (logs.data?.items ?? []).filter((item) =>
    `${item.unit} ${item.message}`
      .toLowerCase()
      .match(/networkmanager|systemd-networkd|network|link is|carrier/)
  )
  const logColumns: ColumnDef<LogEntry>[] = [
    {
      accessorKey: "timestamp",
      header: "Time",
      cell: ({ row }) => new Date(row.original.timestamp).toLocaleString(),
    },
    { accessorKey: "unit", header: "Source" },
    {
      accessorKey: "message",
      header: "Message",
      cell: ({ row }) => (
        <span className="font-mono text-xs whitespace-normal">
          {row.original.message}
        </span>
      ),
    },
  ]
  return (
    <Page
      description="Interface health, traffic, addresses, and detected network manager."
      interval={interval}
    >
      <NetworkMetrics
        samples={metrics.data?.samples ?? []}
        interval={interval.value}
        pending={metrics.isPending}
        error={metrics.isError}
      />
      <State query={query} empty={!items.length}>
        <DataTable
          data={items}
          columns={columns}
          searchPlaceholder="Search devices, addresses, or managers"
          height="45vh"
        />
      </State>
      <Card>
        <CardHeader>
          <CardTitle>Network logs</CardTitle>
          <CardDescription>
            NetworkManager, systemd-networkd, and kernel link events.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <DataTable
            data={networkLogs}
            columns={logColumns}
            height="32vh"
            searchPlaceholder="Search network logs"
          />
        </CardContent>
      </Card>
    </Page>
  )
}

function LogsPage() {
  return (
    <Page description="Searchable, virtualized live journal with bounded browser memory.">
      <JournalBrowser />
    </Page>
  )
}

function UsersPage() {
  const query = useQuery({
    queryKey: ["users"],
    queryFn: () => api<{ items: UserInfo[] }>("/users"),
  })
  const items = query.data?.items ?? []
  const columns: ColumnDef<UserInfo>[] = [
    { accessorKey: "username", header: "User" },
    { accessorKey: "uid", header: "UID" },
    { accessorKey: "name", header: "Name" },
    { accessorKey: "home", header: "Home" },
    { accessorKey: "shell", header: "Shell" },
  ]
  return (
    <Page description="Local account inventory.">
      <State query={query} empty={!items.length}>
        <DataTable data={items} columns={columns} />
      </State>
    </Page>
  )
}
function UpdatesPage() {
  const query = useQuery({
    queryKey: ["updates"],
    queryFn: () => api<UpdateStatus>("/updates"),
  })
  return (
    <Page description="Package backend readiness.">
      <Alert>
        <RefreshCwIcon />
        <AlertTitle>{query.data?.backend || "Package updates"}</AlertTitle>
        <AlertDescription>{query.data?.message}</AlertDescription>
      </Alert>
    </Page>
  )
}

function OperationsPage() {
  const query = useQuery({
    queryKey: ["operations"],
    queryFn: () => api<{ items: OperationReceipt[] }>("/operations?limit=100"),
  })
  const columns: ColumnDef<OperationReceipt>[] = [
    { accessorKey: "actor", header: "Actor" },
    { accessorKey: "target", header: "Target" },
    {
      accessorKey: "completedAt",
      header: "Completed",
      cell: ({ row }) => new Date(row.original.completedAt).toLocaleString(),
    },
    {
      accessorKey: "result",
      header: "Result",
      cell: ({ row }) => (
        <Badge
          variant={
            row.original.result === "succeeded" ? "secondary" : "destructive"
          }
        >
          {row.original.result}
        </Badge>
      ),
    },
    {
      accessorKey: "administrative",
      header: "Authority",
      cell: ({ row }) =>
        row.original.administrative ? "Administrative" : "User",
    },
  ]
  return (
    <Page description="Sanitized service actions and their outcomes.">
      <State query={query} empty={!query.data?.items.length}>
        <DataTable
          data={query.data?.items ?? []}
          columns={columns}
          height="60vh"
          searchPlaceholder="Search actors, targets, or results"
        />
      </State>
    </Page>
  )
}

const SystemTerminal = React.lazy(() =>
  import("@/components/system-terminal").then((value) => ({
    default: value.SystemTerminal,
  }))
)
function TerminalPage() {
  const query = useQuery({
    queryKey: ["terminal"],
    queryFn: () => api<TerminalStatus>("/terminal"),
  })
  return (
    <Page description="Interactive shell under authenticated UNIX identity.">
      {query.data?.available ? (
        <React.Suspense fallback={<Skeleton className="h-[65vh]" />}>
          <SystemTerminal />
        </React.Suspense>
      ) : (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <TerminalIcon />
            </EmptyMedia>
            <EmptyTitle>User bridge unavailable</EmptyTitle>
            <EmptyDescription>{query.data?.message}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
    </Page>
  )
}
