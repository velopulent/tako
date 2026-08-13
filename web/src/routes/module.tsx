import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { CircleAlertIcon, RefreshCwIcon, SearchIcon, TerminalIcon } from "lucide-react"

import {
  api,
  type Capability,
  type InterfaceInfo,
  type LogEntry,
  type MetricSample,
  type MountInfo,
  type ProcessInfo,
  type ServiceInfo,
  type TerminalStatus,
  type UpdateStatus,
  type UserInfo,
} from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

const MetricsChart = React.lazy(() =>
  import("@/components/metrics-chart").then((value) => ({ default: value.MetricsChart }))
)
const SystemTerminal = React.lazy(() =>
  import("@/components/system-terminal").then((value) => ({ default: value.SystemTerminal }))
)

function bytes(value: number) {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"]
  let amount = value
  let unit = 0
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024
    unit++
  }
  return `${amount.toFixed(unit > 2 ? 1 : 0)} ${units[unit]}`
}

function ModuleShell({ title, description, children, capability }: { title: string; description: string; children: React.ReactNode; capability?: Capability }) {
  return (
    <main className="flex flex-1 flex-col gap-6 p-4 lg:p-6">
      {capability && !capability.available && (
        <Alert><CircleAlertIcon /><AlertTitle>Optional integration unavailable</AlertTitle><AlertDescription>{capability.reason}</AlertDescription></Alert>
      )}
      <Card>
        <CardHeader><CardTitle>{title}</CardTitle><CardDescription>{description}</CardDescription></CardHeader>
        <CardContent>{children}</CardContent>
      </Card>
    </main>
  )
}

function DataState({ pending, error, empty, children }: { pending: boolean; error: boolean; empty: boolean; children: React.ReactNode }) {
  if (pending) return <Skeleton className="h-72" />
  if (error) return <Alert variant="destructive"><AlertTitle>Could not load data</AlertTitle><AlertDescription>System adapter returned an error.</AlertDescription></Alert>
  if (empty) return <Empty><EmptyHeader><EmptyTitle>No results</EmptyTitle><EmptyDescription>No matching system resources were found.</EmptyDescription></EmptyHeader></Empty>
  return children
}

export function ModulePage({ module }: { module: string }) {
  const capabilities = useQuery({ queryKey: ["capabilities"], queryFn: () => api<{ capabilities: Capability[] }>("/capabilities") })
  const capability = capabilities.data?.capabilities.find((item) => item.id === module)
  if (module === "metrics") return <MetricsPage />
  if (module === "logs") return <LogsPage capability={capability} />
  if (module === "processes") return <ProcessesPage capability={capability} />
  if (module === "users") return <UsersPage capability={capability} />
  if (module === "services") return <ServicesPage capability={capability} />
  if (module === "storage") return <StoragePage capability={capability} />
  if (module === "network") return <NetworkPage capability={capability} />
  if (module === "updates") return <UpdatesPage capability={capability} />
  return <TerminalPage capability={capability} />
}

function MetricsPage() {
  const query = useQuery({ queryKey: ["metrics"], queryFn: () => api<{ samples: MetricSample[] }>("/metrics") })
  return <main className="flex flex-1 flex-col gap-6 p-4 lg:p-6"><React.Suspense fallback={<Skeleton className="h-80" />}><MetricsChart initialSamples={query.data?.samples ?? []} /></React.Suspense></main>
}

function LogsPage({ capability }: { capability?: Capability }) {
  const [search, setSearch] = React.useState("")
  const [live, setLive] = React.useState<LogEntry[]>([])
  const query = useQuery({ queryKey: ["logs"], queryFn: () => api<{ items: LogEntry[] }>("/logs?limit=300") })
  React.useEffect(() => {
    const source = new EventSource("/api/v1/logs/stream")
    source.addEventListener("log", (event) => {
      const entry = JSON.parse((event as MessageEvent).data) as LogEntry
      setLive((current) => [entry, ...current].slice(0, 300))
    })
    return () => source.close()
  }, [])
  const items = [...live, ...(query.data?.items ?? [])].filter((item) => `${item.unit} ${item.message}`.toLowerCase().includes(search.toLowerCase())).slice(0, 300)
  return <ModuleShell title="System logs" description="Newest 300 journal entries." capability={capability}>
    <div className="mb-4 flex items-center gap-2"><SearchIcon aria-hidden="true" /><Input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Filter unit or message" aria-label="Filter logs" /></div>
    <DataState pending={query.isPending} error={query.isError} empty={items.length === 0}><div className="max-h-[65vh] overflow-auto rounded-lg border"><Table><TableHeader><TableRow><TableHead>Time</TableHead><TableHead>Priority</TableHead><TableHead>Unit</TableHead><TableHead>Message</TableHead></TableRow></TableHeader><TableBody>{items.map((item, index) => <TableRow key={`${item.timestamp}-${index}`}><TableCell className="whitespace-nowrap text-xs">{new Date(item.timestamp).toLocaleString()}</TableCell><TableCell><Badge variant="outline">{item.priority || "-"}</Badge></TableCell><TableCell className="whitespace-nowrap">{item.unit || "kernel"}</TableCell><TableCell className="max-w-xl whitespace-normal font-mono text-xs">{item.message}</TableCell></TableRow>)}</TableBody></Table></div></DataState>
  </ModuleShell>
}

function ProcessesPage({ capability }: { capability?: Capability }) {
  const [search, setSearch] = React.useState("")
  const query = useQuery({ queryKey: ["processes"], queryFn: () => api<{ items: ProcessInfo[] }>("/processes"), refetchInterval: 5000 })
  const items = query.data?.items.filter((item) => `${item.pid} ${item.user} ${item.command}`.toLowerCase().includes(search.toLowerCase())).slice(0, 500) ?? []
  return <ModuleShell title="Processes" description="Live process inventory sorted by resident memory." capability={capability}><Input className="mb-4" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Filter PID, user, or command" aria-label="Filter processes" /><DataState pending={query.isPending} error={query.isError} empty={items.length === 0}><div className="max-h-[65vh] overflow-auto rounded-lg border"><Table><TableHeader><TableRow><TableHead>PID</TableHead><TableHead>User</TableHead><TableHead>State</TableHead><TableHead>Memory</TableHead><TableHead>CPU time</TableHead><TableHead>Command</TableHead></TableRow></TableHeader><TableBody>{items.map((item) => <TableRow key={item.pid}><TableCell>{item.pid}</TableCell><TableCell>{item.user}</TableCell><TableCell><Badge variant="secondary">{item.state}</Badge></TableCell><TableCell>{bytes(item.memory)}</TableCell><TableCell>{item.cpuTime.toFixed(1)}s</TableCell><TableCell className="max-w-xl truncate font-mono text-xs" title={item.command}>{item.command}</TableCell></TableRow>)}</TableBody></Table></div></DataState></ModuleShell>
}

function UsersPage({ capability }: { capability?: Capability }) {
  const query = useQuery({ queryKey: ["users"], queryFn: () => api<{ items: UserInfo[] }>("/users") })
  const items = query.data?.items ?? []
  return <ModuleShell title="Users" description="Local accounts from the host identity database." capability={capability}><DataState pending={query.isPending} error={query.isError} empty={items.length === 0}><div className="overflow-auto rounded-lg border"><Table><TableHeader><TableRow><TableHead>User</TableHead><TableHead>UID</TableHead><TableHead>Name</TableHead><TableHead>Home</TableHead><TableHead>Shell</TableHead></TableRow></TableHeader><TableBody>{items.map((item) => <TableRow key={item.username}><TableCell className="font-medium">{item.username}</TableCell><TableCell>{item.uid}</TableCell><TableCell>{item.name || "—"}</TableCell><TableCell className="font-mono text-xs">{item.home}</TableCell><TableCell className="font-mono text-xs">{item.shell}</TableCell></TableRow>)}</TableBody></Table></div></DataState></ModuleShell>
}

function ServicesPage({ capability }: { capability?: Capability }) {
  const [search, setSearch] = React.useState("")
  const query = useQuery({ queryKey: ["services"], queryFn: () => api<{ items: ServiceInfo[] }>("/services"), refetchInterval: 10000 })
  const items = query.data?.items.filter((item) => `${item.name} ${item.description}`.toLowerCase().includes(search.toLowerCase())) ?? []
  return <ModuleShell title="Services" description="systemd service unit state from D-Bus." capability={capability}><Input className="mb-4" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Filter services" aria-label="Filter services" /><DataState pending={query.isPending} error={query.isError} empty={items.length === 0}><div className="max-h-[65vh] overflow-auto rounded-lg border"><Table><TableHeader><TableRow><TableHead>Unit</TableHead><TableHead>Description</TableHead><TableHead>State</TableHead><TableHead>Detail</TableHead></TableRow></TableHeader><TableBody>{items.map((item) => <TableRow key={item.name}><TableCell className="font-medium">{item.name}</TableCell><TableCell>{item.description}</TableCell><TableCell><Badge variant={item.activeState === "active" ? "secondary" : "outline"}>{item.activeState}</Badge></TableCell><TableCell>{item.subState}</TableCell></TableRow>)}</TableBody></Table></div></DataState></ModuleShell>
}

function StoragePage({ capability }: { capability?: Capability }) {
  const query = useQuery({ queryKey: ["storage"], queryFn: () => api<{ items: MountInfo[] }>("/storage"), refetchInterval: 15000 })
  const items = query.data?.items ?? []
  return <ModuleShell title="Storage" description="Mounted persistent filesystems and current usage." capability={capability}><DataState pending={query.isPending} error={query.isError} empty={items.length === 0}><div className="grid gap-4 md:grid-cols-2">{items.map((item) => <Card key={item.target}><CardHeader><CardTitle className="text-base">{item.target}</CardTitle><CardDescription>{item.source} · {item.filesystem}</CardDescription></CardHeader><CardContent className="flex flex-col gap-3"><Progress value={item.percent} /><div className="flex justify-between text-sm"><span>{bytes(item.used)} used</span><span className="text-muted-foreground">{bytes(item.total)} total</span></div></CardContent></Card>)}</div></DataState></ModuleShell>
}

function NetworkPage({ capability }: { capability?: Capability }) {
  const query = useQuery({ queryKey: ["network"], queryFn: () => api<{ items: InterfaceInfo[] }>("/network"), refetchInterval: 5000 })
  const items = query.data?.items ?? []
  return <ModuleShell title="Network" description="Kernel interface state; NetworkManager capability shown separately." capability={capability}><DataState pending={query.isPending} error={query.isError} empty={items.length === 0}><div className="grid gap-4 md:grid-cols-2">{items.map((item) => <Card key={item.index}><CardHeader><div className="flex items-center justify-between"><CardTitle className="text-base">{item.name}</CardTitle><Badge variant={item.up ? "secondary" : "outline"}>{item.up ? "Up" : "Down"}</Badge></div><CardDescription>{item.hardware || "No hardware address"} · MTU {item.mtu}</CardDescription></CardHeader><CardContent className="flex flex-col gap-3"><div className="flex flex-wrap gap-2">{item.addresses.map((address) => <Badge key={address} variant="outline">{address}</Badge>)}</div><p className="text-sm text-muted-foreground">Received {bytes(item.rx)} · Sent {bytes(item.tx)}</p></CardContent></Card>)}</div></DataState></ModuleShell>
}

function UpdatesPage({ capability }: { capability?: Capability }) {
  const query = useQuery({ queryKey: ["updates"], queryFn: () => api<UpdateStatus>("/updates") })
  return <ModuleShell title="Updates" description="PackageKit capability and transaction readiness." capability={capability}><DataState pending={query.isPending} error={query.isError} empty={!query.data}><Alert><RefreshCwIcon /><AlertTitle>{query.data?.backend || "Package updates"}</AlertTitle><AlertDescription>{query.data?.message}</AlertDescription></Alert></DataState></ModuleShell>
}

function TerminalPage({ capability }: { capability?: Capability }) {
  const query = useQuery({ queryKey: ["terminal"], queryFn: () => api<TerminalStatus>("/terminal") })
  return <ModuleShell title="Terminal" description="Interactive PTY sessions run as your authenticated UNIX identity." capability={capability}>
    <DataState pending={query.isPending} error={query.isError} empty={!query.data}>
      {query.data?.available ? <React.Suspense fallback={<Skeleton className="h-[65vh]" />}><SystemTerminal /></React.Suspense> : <Empty><EmptyHeader><EmptyMedia variant="icon"><TerminalIcon /></EmptyMedia><EmptyTitle>User bridge unavailable</EmptyTitle><EmptyDescription>{query.data?.message}</EmptyDescription></EmptyHeader></Empty>}
    </DataState>
  </ModuleShell>
}
