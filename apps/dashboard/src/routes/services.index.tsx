import { useQuery } from "@tanstack/react-query"
import {
  createFileRoute,
  getRouteApi,
  useNavigate,
} from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import * as React from "react"

import { DataTable, type DataTableFeatures } from "@/components/data-table"
import { capitalize, StatusBadge, unitStatus } from "@/components/status-badge"
import { Badge } from "@/components/ui/badge"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { api, type ServiceInfo } from "@/lib/api"
import { plural } from "@/lib/format"
import { Page, State, usePageInterval } from "@/lib/page"
import {
  type serviceActiveStates,
  type serviceFileStates,
  servicesSearch,
  serviceTypes,
} from "@/lib/search"

const typeLabels: Record<(typeof serviceTypes)[number], string> = {
  service: "Services",
  timer: "Timers",
  socket: "Sockets",
  target: "Targets",
  path: "Paths",
}

const activeLabels: Record<(typeof serviceActiveStates)[number], string> = {
  all: "Any state",
  active: "Active",
  inactive: "Inactive",
  failed: "Failed",
}

const fileLabels: Record<(typeof serviceFileStates)[number], string> = {
  all: "Any startup",
  enabled: "Enabled",
  disabled: "Disabled",
  static: "Static",
  masked: "Masked",
}

const columns: ColumnDef<DataTableFeatures, ServiceInfo>[] = [
  {
    accessorKey: "name",
    header: "Unit",
    size: 280,
    cell: ({ row }) => (
      <span className="flex min-w-0 flex-col">
        <span className="truncate font-medium">{row.original.name}</span>
        <span className="truncate text-xs text-muted-foreground">
          {row.original.description}
        </span>
      </span>
    ),
  },
  {
    accessorKey: "activeState",
    header: "Status",
    size: 140,
    cell: ({ row }) => {
      const status = unitStatus(row.original.activeState, row.original.subState)
      return <StatusBadge tone={status.tone}>{status.label}</StatusBadge>
    },
  },
  {
    accessorKey: "fileState",
    header: "Startup",
    size: 120,
    cell: ({ row }) =>
      row.original.fileState ? (
        <Badge
          variant={
            row.original.fileState === "masked" ? "destructive" : "outline"
          }
          className="font-normal"
        >
          {capitalize(row.original.fileState)}
        </Badge>
      ) : (
        <span className="text-muted-foreground">—</span>
      ),
  },
  {
    accessorKey: "loadState",
    header: "Loaded",
    size: 112,
    cell: ({ row }) => capitalize(row.original.loadState),
  },
  { accessorKey: "description", header: "Description", size: 280 },
]

function FilterSelect<T extends string>({
  label,
  value,
  labels,
  onChange,
}: {
  label: string
  value: T
  labels: Record<T, string>
  onChange: (value: T) => void
}) {
  const options = Object.entries(labels) as [T, string][]
  return (
    <Select
      items={options.map(([key, text]) => ({ value: key, label: text }))}
      value={value}
      onValueChange={(next) => onChange(next as T)}
    >
      <SelectTrigger size="sm" aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          {options.map(([key, text]) => (
            <SelectItem key={key} value={key}>
              {text}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}

function ServicesPage() {
  const interval = usePageInterval("services")
  const search = getRouteApi("/services/").useSearch()
  const navigate = useNavigate({ from: "/services/" })
  const type = search.type ?? "service"
  const scope = search.scope ?? "system"
  const active = search.active ?? "all"
  const file = search.file ?? "all"
  const patch = (next: Partial<typeof search>) =>
    navigate({
      search: (previous) => ({ ...previous, ...next }),
      replace: true,
    })
  const query = useQuery({
    queryKey: ["services", scope, type],
    queryFn: () =>
      api<{ items: ServiceInfo[] }>(`/services?scope=${scope}&type=${type}`),
    refetchInterval: interval.milliseconds,
  })
  const all = query.data?.items ?? []
  const items = React.useMemo(
    () =>
      all.filter(
        (item) =>
          (active === "all" || item.activeState === active) &&
          (file === "all" || item.fileState === file)
      ),
    [all, active, file]
  )
  const failed = all.filter((item) => item.activeState === "failed").length
  const running = all.filter((item) => item.activeState === "active").length

  return (
    <Page
      description={
        query.data ? (
          <>
            {plural(all.length, "unit")} · {running} active
            {failed > 0 && (
              <button
                type="button"
                className="ml-1 font-medium text-destructive underline-offset-4 hover:underline"
                onClick={() => patch({ active: "failed" })}
              >
                · {failed} failed
              </button>
            )}
          </>
        ) : (
          "systemd units on this host"
        )
      }
      interval={interval}
      actions={
        <ToggleGroup
          value={[scope]}
          onValueChange={(value) => {
            const next = value[0]
            if (next) patch({ scope: next === "user" ? "user" : undefined })
          }}
          variant="outline"
          size="sm"
          aria-label="Unit scope"
        >
          <ToggleGroupItem value="system">System</ToggleGroupItem>
          <ToggleGroupItem value="user">My session</ToggleGroupItem>
        </ToggleGroup>
      }
    >
      <div className="-mx-1 overflow-x-auto px-1">
        <Tabs
          value={type}
          onValueChange={(next) =>
            patch({
              type:
                next === "service"
                  ? undefined
                  : (next as (typeof serviceTypes)[number]),
            })
          }
        >
          <TabsList>
            {serviceTypes.map((value) => (
              <TabsTrigger key={value} value={value}>
                {typeLabels[value]}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>
      <State query={query} empty={false}>
        <DataTable
          data={items}
          columns={columns}
          height="calc(100svh - 17rem)"
          searchPlaceholder={`Search ${typeLabels[type].toLowerCase()}`}
          search={search.q}
          onSearchChange={(q) => patch({ q: q || undefined })}
          initialVisibility={{ description: false, loadState: false }}
          emptyMessage={
            active !== "all" || file !== "all"
              ? "No units match these filters."
              : "No units of this type."
          }
          toolbar={
            <>
              <FilterSelect
                label="Status"
                value={active}
                labels={activeLabels}
                onChange={(next) =>
                  patch({ active: next === "all" ? undefined : next })
                }
              />
              <FilterSelect
                label="Startup"
                value={file}
                labels={fileLabels}
                onChange={(next) =>
                  patch({ file: next === "all" ? undefined : next })
                }
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

export const Route = createFileRoute("/services/")({
  validateSearch: servicesSearch,
  component: ServicesPage,
})
