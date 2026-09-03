import { useQuery } from "@tanstack/react-query"
import {
  createFileRoute,
  getRouteApi,
  useNavigate,
} from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import * as React from "react"
import { DataTable, type DataTableFeatures } from "@/components/data-table"
import { StorageMetrics } from "@/components/directional-metrics"
import { Badge } from "@/components/ui/badge"
import { Progress } from "@/components/ui/progress"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { api, type FileSystemInfo, type MetricSample } from "@/lib/api"
import { bytes, Page, State, usePageInterval } from "@/lib/page"
import { qSearch } from "@/lib/search"

const storageScopes = ["all", "local", "network"] as const
type StorageScope = (typeof storageScopes)[number]

function MountTargets({ filesystem }: { filesystem: FileSystemInfo }) {
  const primary = filesystem.targets[0]
  const rest = filesystem.targets.slice(1)
  if (!primary) return null
  return (
    <div className="flex min-w-0 items-center gap-2">
      <span className="truncate font-medium">{primary.target}</span>
      {rest.length > 0 && (
        <Tooltip>
          <TooltipTrigger>
            <Badge variant="outline" className="shrink-0">
              +{rest.length}
            </Badge>
          </TooltipTrigger>
          <TooltipContent className="flex-col items-start gap-0.5">
            {filesystem.targets.map((target) => (
              <span key={target.target} className="font-mono">
                {target.target}
                {target.root && target.root !== "/" ? ` · ${target.root}` : ""}
              </span>
            ))}
          </TooltipContent>
        </Tooltip>
      )}
    </div>
  )
}

function StoragePage() {
  const interval = usePageInterval("storage")
  const search = getRouteApi("/storage").useSearch()
  const navigate = useNavigate({ from: "/storage" })
  const [scope, setScope] = React.useState<StorageScope>("all")
  const metrics = useQuery({
    queryKey: ["storage-metrics"],
    queryFn: () => api<{ samples: MetricSample[] }>("/metrics?range=1h"),
  })
  const query = useQuery({
    queryKey: ["storage"],
    queryFn: () => api<{ items: FileSystemInfo[] }>("/storage"),
    refetchInterval: interval.milliseconds,
  })
  const filesystems = query.data?.items ?? []
  const items =
    scope === "all"
      ? filesystems
      : filesystems.filter((item) => item.network === (scope === "network"))
  const columns: ColumnDef<DataTableFeatures, FileSystemInfo>[] = [
    {
      id: "mount",
      header: "Mount",
      accessorFn: (row) => row.targets.map((target) => target.target).join(" "),
      cell: ({ row }) => <MountTargets filesystem={row.original} />,
    },
    { accessorKey: "device", header: "Device" },
    {
      accessorKey: "type",
      header: "Type",
      cell: ({ row }) => (
        <div className="flex flex-wrap gap-1">
          <Badge variant={row.original.network ? "outline" : "secondary"}>
            {row.original.type}
          </Badge>
          {row.original.readOnly && <Badge variant="outline">Read-only</Badge>}
        </div>
      ),
    },
    {
      accessorKey: "used",
      header: "Used",
      meta: { align: "end" },
      cell: ({ row }) => bytes(row.original.used),
    },
    {
      accessorKey: "available",
      header: "Available",
      meta: { align: "end" },
      cell: ({ row }) => bytes(row.original.available),
    },
    {
      accessorKey: "percent",
      header: "Usage",
      meta: { wrap: true },
      size: 220,
      cell: ({ row }) =>
        row.original.total > 0 ? (
          <div className="min-w-32">
            <Progress value={row.original.percent} />
            <span className="text-xs text-muted-foreground">
              {row.original.percent.toFixed(1)}%
            </span>
          </div>
        ) : (
          <span className="text-xs text-muted-foreground">Unavailable</span>
        ),
    },
  ]
  return (
    <Page
      description="Filesystem capacity grouped by device, and block-device activity."
      interval={interval}
    >
      <StorageMetrics
        samples={metrics.data?.samples ?? []}
        interval={interval.value}
        pending={metrics.isPending}
        error={metrics.isError}
      />
      <Tabs
        value={scope}
        onValueChange={(next) => setScope(next as StorageScope)}
      >
        <TabsList>
          {storageScopes.map((value) => (
            <TabsTrigger key={value} value={value}>
              {value[0].toUpperCase() + value.slice(1)}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <State query={query} empty={!items.length}>
        <DataTable
          data={items}
          columns={columns}
          searchPlaceholder="Search mounts, devices, and filesystems"
          height="45vh"
          search={search.q}
          onSearchChange={(q) =>
            navigate({
              search: (previous) => ({ ...previous, q: q || undefined }),
              replace: true,
            })
          }
        />
      </State>
    </Page>
  )
}

export const Route = createFileRoute("/storage")({
  validateSearch: qSearch,
  component: StoragePage,
})
