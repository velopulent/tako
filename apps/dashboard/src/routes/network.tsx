import { useQuery } from "@tanstack/react-query"
import {
  createFileRoute,
  getRouteApi,
  useNavigate,
} from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import { ArrowDownIcon, ArrowUpIcon, SettingsIcon } from "lucide-react"
import * as React from "react"

import { useAdminAccess } from "@/components/admin-access"
import { DataTable, type DataTableFeatures } from "@/components/data-table"
import { NetworkMetrics } from "@/components/directional-metrics"
import { FirewallPanel } from "@/components/firewall-panel"
import { LogList } from "@/components/log-list"
import {
  ConfigureInterface,
  PendingNetworkChange,
} from "@/components/network-configure"
import { StatusBadge } from "@/components/status-badge"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { isNarrowViewport } from "@/hooks/use-mobile"
import {
  api,
  type InterfaceInfo,
  type LogEntry,
  type MetricSample,
  type NetworkResponse,
  type NetworkState,
} from "@/lib/api"
import { formatBytes } from "@/lib/format"
import { Page, State, usePageInterval } from "@/lib/page"
import { type NetworkView, networkSearch } from "@/lib/search"

const virtual =
  /^(lo|docker\d*|br-|veth|virbr|vnet|tun|tap|podman|cni|flannel|cali|wg)/

function NetworkPage() {
  const interval = usePageInterval("network")
  const search = getRouteApi("/network").useSearch()
  const navigate = useNavigate({ from: "/network" })
  const view = search.view ?? "overview"
  const metrics = useQuery({
    queryKey: ["metrics", "1h"],
    queryFn: () => api<{ samples: MetricSample[] }>("/metrics?range=1h"),
  })
  const query = useQuery({
    queryKey: ["network"],
    queryFn: () => api<NetworkResponse>("/network"),
    refetchInterval: interval.milliseconds,
  })
  const [pending, setPending] = React.useState<NetworkState>()
  return (
    <Page
      description="Interfaces, traffic, routing, and the firewall."
      interval={view === "overview" ? interval : undefined}
    >
      <Tabs
        value={view}
        onValueChange={(next) =>
          navigate({
            search: {
              view: next === "overview" ? undefined : (next as NetworkView),
            },
            replace: true,
          })
        }
        className="flex flex-col gap-6"
      >
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="firewall">Firewall</TabsTrigger>
          <TabsTrigger value="logs">Logs</TabsTrigger>
        </TabsList>
        <TabsContent value="overview" className="flex flex-col gap-6">
          {pending && (
            <PendingNetworkChange
              state={pending}
              snapshot={query.data}
              onDone={() => setPending(undefined)}
            />
          )}
          {query.data?.ownership?.conflicted && (
            <Alert variant="destructive">
              <AlertTitle>More than one network manager is active</AlertTitle>
              <AlertDescription>{query.data.ownership.reason}</AlertDescription>
            </Alert>
          )}
          <NetworkMetrics
            samples={metrics.data?.samples ?? []}
            interval={interval.value}
            pending={metrics.isPending}
            error={metrics.isError}
          />
          <State query={query} empty={false}>
            {query.data && (
              <Interfaces snapshot={query.data} onPending={setPending} />
            )}
          </State>
          {query.data && <Routing snapshot={query.data} />}
        </TabsContent>
        <TabsContent value="firewall">
          <FirewallPanel />
        </TabsContent>
        <TabsContent value="logs">
          <NetworkLogs />
        </TabsContent>
      </Tabs>
    </Page>
  )
}

function Interfaces({
  snapshot,
  onPending,
}: {
  snapshot: NetworkResponse
  onPending: (state: NetworkState) => void
}) {
  const { administrative } = useAdminAccess()
  const [editing, setEditing] = React.useState<InterfaceInfo>()
  const [showVirtual, setShowVirtual] = React.useState(false)
  const configurable =
    administrative &&
    snapshot.ownership?.activeOwner === "NetworkManager" &&
    !snapshot.ownership.conflicted
  const items = showVirtual
    ? snapshot.items
    : snapshot.items.filter((item) => !virtual.test(item.name))
  const hidden = snapshot.items.length - items.length
  const columns: ColumnDef<DataTableFeatures, InterfaceInfo>[] = [
    {
      accessorKey: "name",
      header: "Interface",
      size: 160,
      cell: ({ row }) => (
        <span className="flex min-w-0 flex-col">
          <span className="truncate font-medium">{row.original.name}</span>
          {row.original.profile && (
            <span className="truncate text-xs text-muted-foreground">
              {row.original.profile}
            </span>
          )}
        </span>
      ),
    },
    {
      accessorKey: "up",
      header: "State",
      size: 90,
      cell: ({ row }) => (
        <StatusBadge tone={row.original.up ? "success" : "muted"}>
          {row.original.up ? "Up" : "Down"}
        </StatusBadge>
      ),
    },
    {
      id: "addresses",
      header: "Addresses",
      size: 280,
      accessorFn: (row) => row.addresses.join(" "),
      cell: ({ row }) => {
        const [first, ...rest] = row.original.addresses
        if (!first) return <span className="text-muted-foreground">—</span>
        return (
          <span className="flex min-w-0 items-center gap-1.5">
            <span className="truncate font-mono text-xs">{first}</span>
            {rest.length > 0 && (
              <Tooltip>
                <TooltipTrigger
                  render={<Badge variant="outline" className="font-normal" />}
                >
                  +{rest.length}
                </TooltipTrigger>
                <TooltipContent className="flex flex-col items-start font-mono">
                  {rest.map((address) => (
                    <span key={address}>{address}</span>
                  ))}
                </TooltipContent>
              </Tooltip>
            )}
          </span>
        )
      },
    },
    {
      id: "traffic",
      header: "Traffic",
      size: 170,
      accessorFn: (row) => row.rx + row.tx,
      cell: ({ row }) => (
        <span className="flex gap-3 text-xs text-muted-foreground tabular-nums">
          <span className="flex items-center gap-1">
            <ArrowDownIcon className="size-3" />
            {formatBytes(row.original.rx)}
          </span>
          <span className="flex items-center gap-1">
            <ArrowUpIcon className="size-3" />
            {formatBytes(row.original.tx)}
          </span>
        </span>
      ),
    },
    {
      accessorKey: "hardware",
      header: "MAC",
      size: 150,
      cell: ({ row }) => (
        <span className="font-mono text-xs text-muted-foreground">
          {row.original.hardware || "—"}
        </span>
      ),
    },
    { accessorKey: "mtu", header: "MTU", size: 70, meta: { align: "end" } },
    {
      id: "actions",
      header: "",
      size: 52,
      enableSorting: false,
      cell: ({ row }) =>
        configurable &&
        row.original.manager === "NetworkManager" &&
        !virtual.test(row.original.name) ? (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Configure ${row.original.name}`}
            onClick={() => setEditing(row.original)}
          >
            <SettingsIcon />
          </Button>
        ) : null,
    },
  ]
  return (
    <Card>
      <CardHeader>
        <CardTitle>Interfaces</CardTitle>
        <CardDescription>
          Managed by {snapshot.ownership?.activeOwner || "the kernel"}
          {hidden > 0 && !showVirtual && (
            <>
              {" · "}
              <button
                type="button"
                className="underline-offset-4 hover:underline"
                onClick={() => setShowVirtual(true)}
              >
                show {hidden} virtual
              </button>
            </>
          )}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <DataTable
          data={items}
          columns={columns}
          initialVisibility={
            isNarrowViewport()
              ? { hardware: false, mtu: false, traffic: false }
              : undefined
          }
          emptyMessage="No interfaces."
          searchPlaceholder="Search interfaces and addresses"
        />
      </CardContent>
      {editing && (
        <ConfigureInterface
          snapshot={snapshot}
          item={editing}
          onClose={() => setEditing(undefined)}
          onPending={onPending}
        />
      )}
    </Card>
  )
}

function Routing({ snapshot }: { snapshot: NetworkResponse }) {
  const routes = snapshot.routes ?? []
  const dns = snapshot.dns ?? []
  return (
    <div className="grid gap-4 @4xl/main:grid-cols-3">
      <Card size="sm" className="@4xl/main:col-span-2">
        <CardHeader>
          <CardTitle>Routes</CardTitle>
        </CardHeader>
        <CardContent>
          {routes.length ? (
            <ul className="flex flex-col divide-y text-sm">
              {routes.map((route) => (
                <li
                  key={`${route.destination}-${route.gateway}-${route.device}-${route.metric}`}
                  className="grid grid-cols-[1fr_auto] items-center gap-x-4 py-2 sm:grid-cols-[minmax(0,2fr)_minmax(0,2fr)_auto]"
                >
                  <span className="truncate font-mono text-xs">
                    {route.destination}
                  </span>
                  <span className="hidden truncate font-mono text-xs text-muted-foreground sm:block">
                    {route.gateway ? `via ${route.gateway}` : "direct"}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {route.device}
                    {route.metric !== undefined ? ` · ${route.metric}` : ""}
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-muted-foreground">No routes.</p>
          )}
        </CardContent>
      </Card>
      <Card size="sm">
        <CardHeader>
          <CardTitle>DNS servers</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-1.5">
          {dns.length ? (
            dns.map((server) => (
              <span key={server} className="font-mono text-xs">
                {server}
              </span>
            ))
          ) : (
            <p className="text-sm text-muted-foreground">None configured.</p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function NetworkLogs() {
  const query = useQuery({
    queryKey: ["logs", "network"],
    queryFn: () =>
      api<{ items: LogEntry[] }>(
        "/logs?limit=200&unit=NetworkManager.service&unit=systemd-networkd.service&unit=firewalld.service"
      ),
    refetchInterval: 15_000,
  })
  return (
    <State
      query={query}
      empty={!query.data?.items.length}
      emptyTitle="No network events"
      emptyDescription="NetworkManager, systemd-networkd, and firewalld have not logged anything recently."
    >
      <Card className="py-0">
        <LogList items={query.data?.items ?? []} />
      </Card>
    </State>
  )
}

export const Route = createFileRoute("/network")({
  validateSearch: networkSearch,
  component: NetworkPage,
})
