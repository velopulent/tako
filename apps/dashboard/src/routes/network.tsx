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
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  api,
  type InterfaceInfo,
  type LogEntry,
  type MetricSample,
} from "@/lib/api"
import { bytes, Page, State, usePageInterval } from "@/lib/page"
import { qSearch } from "@/lib/search"

function NetworkPage() {
  const interval = usePageInterval("network")
  const search = getRouteApi("/network").useSearch()
  const navigate = useNavigate({ from: "/network" })
  const setQ = (q: string) =>
    navigate({
      search: (previous) => ({ ...previous, q: q || undefined }),
      replace: true,
    })
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
      <NetworkControls />
      <FirewallControls />
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
            search={search.q}
            onSearchChange={setQ}
          />
        </CardContent>
      </Card>
    </Page>
  )
}

export const Route = createFileRoute("/network")({
  validateSearch: qSearch,
  component: NetworkPage,
})
