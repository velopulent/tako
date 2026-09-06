import { useQuery } from "@tanstack/react-query"
import {
  createFileRoute,
  getRouteApi,
  useNavigate,
} from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import { DataTable, type DataTableFeatures } from "@/components/data-table"
import { NetworkMetrics } from "@/components/directional-metrics"
import { FirewallControls } from "@/components/firewall-controls"
import { NetworkControls } from "@/components/network-controls"
import { NetworkDetails, NetworkSummary } from "@/components/network-overview"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  api,
  type InterfaceInfo,
  type LogEntry,
  type MetricSample,
  type NetworkResponse,
} from "@/lib/api"
import { bytes, Page, State, usePageInterval } from "@/lib/page"
import { type NetworkView, networkSearch, networkViews } from "@/lib/search"

const networkTabLabels: Record<NetworkView, string> = {
  overview: "Overview",
  configuration: "Configuration",
  firewall: "Firewall",
  logs: "Logs",
}

function normalizeNetworkView(value: unknown): NetworkView {
  return networkViews.includes(value as NetworkView)
    ? (value as NetworkView)
    : "overview"
}

function NetworkPage() {
  const interval = usePageInterval("network")
  const search = getRouteApi("/network").useSearch()
  const navigate = useNavigate({ from: "/network" })
  const view = normalizeNetworkView(search.view)
  const setQ = (q: string) =>
    navigate({
      search: (previous) => ({ ...previous, q: q || undefined }),
      replace: true,
    })
  const setView = (next: string) => {
    const value = normalizeNetworkView(next)
    navigate({
      search: (previous) => ({
        ...previous,
        view: value === "overview" ? undefined : value,
      }),
      replace: true,
    })
  }
  const metrics = useQuery({
    queryKey: ["network-metrics"],
    queryFn: () => api<{ samples: MetricSample[] }>("/metrics?range=1h"),
  })
  const query = useQuery({
    queryKey: ["network"],
    queryFn: () => api<NetworkResponse>("/network"),
    refetchInterval: interval.milliseconds,
  })
  const logs = useQuery({
    queryKey: ["logs", "network"],
    queryFn: () => api<{ items: LogEntry[] }>("/logs?limit=300"),
    refetchInterval: interval.milliseconds,
  })
  const items = query.data?.items ?? []
  const columns: ColumnDef<DataTableFeatures, InterfaceInfo>[] = [
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
    { accessorKey: "mtu", header: "MTU", meta: { align: "end" }, size: 88 },
    { accessorKey: "manager", header: "Manager" },
    {
      accessorKey: "owner",
      header: "Ownership",
      cell: ({ row }) => row.original.owner || row.original.profile || "—",
    },
    {
      accessorKey: "rx",
      header: "Received",
      meta: { align: "end" },
      cell: ({ row }) => bytes(row.original.rx),
    },
    {
      accessorKey: "tx",
      header: "Sent",
      meta: { align: "end" },
      cell: ({ row }) => bytes(row.original.tx),
    },
  ]
  const networkLogs = (logs.data?.items ?? []).filter((item) =>
    `${item.unit} ${item.message}`
      .toLowerCase()
      .match(/networkmanager|systemd-networkd|network|link is|carrier/)
  )
  const logColumns: ColumnDef<DataTableFeatures, LogEntry>[] = [
    {
      accessorKey: "timestamp",
      header: "Time",
      cell: ({ row }) => new Date(row.original.timestamp).toLocaleString(),
    },
    { accessorKey: "unit", header: "Source" },
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
    <Page
      description="Interface health, traffic, addresses, and detected network manager."
      interval={interval}
    >
      <Tabs
        value={view}
        onValueChange={setView}
        className="flex flex-col gap-6"
      >
        <TabsList className="w-full justify-start overflow-x-auto sm:w-fit">
          {networkViews.map((value) => (
            <TabsTrigger key={value} value={value}>
              {networkTabLabels[value]}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="overview" className="flex flex-col gap-6">
          <NetworkSummary snapshot={query.data} />
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
              search={search.q}
              onSearchChange={setQ}
            />
          </State>
          {query.data && <NetworkDetails snapshot={query.data} />}
        </TabsContent>
        <TabsContent value="configuration" className="flex flex-col gap-6">
          <NetworkControls />
        </TabsContent>
        <TabsContent value="firewall" className="flex flex-col gap-6">
          <FirewallControls />
        </TabsContent>
        <TabsContent value="logs" className="flex flex-col gap-6">
          <Card>
            <CardHeader>
              <CardTitle>Network logs</CardTitle>
              <CardDescription>
                NetworkManager, systemd-networkd, and kernel link events.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <State query={logs} empty={!networkLogs.length}>
                <DataTable
                  data={networkLogs}
                  columns={logColumns}
                  height="55vh"
                  searchPlaceholder="Search network logs"
                  search={search.q}
                  onSearchChange={setQ}
                />
              </State>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </Page>
  )
}

export const Route = createFileRoute("/network")({
  validateSearch: networkSearch,
  component: NetworkPage,
})
