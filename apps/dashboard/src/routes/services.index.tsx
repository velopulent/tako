import { useQuery } from "@tanstack/react-query"
import {
  createFileRoute,
  getRouteApi,
  useNavigate,
} from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import { DataTable, type DataTableFeatures } from "@/components/data-table"
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
import { api, type ServiceInfo } from "@/lib/api"
import { Page, State, usePageInterval } from "@/lib/page"
import {
  serviceActiveStates,
  serviceFileStates,
  serviceScopes,
  servicesSearch,
  serviceTypes,
} from "@/lib/search"

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
  const items = values.map((item) => ({
    value: item,
    label: item === "all" ? `All ${label.toLowerCase()}s` : item,
  }))
  return (
    <Select
      items={items}
      value={value}
      onValueChange={(next) => onChange(String(next))}
    >
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
  const navigateDetail = useNavigate()
  const query = useQuery({
    queryKey: ["services", scope, type],
    queryFn: () =>
      api<{ items: ServiceInfo[] }>(`/services?scope=${scope}&type=${type}`),
    refetchInterval: interval.milliseconds,
  })
  const columns: ColumnDef<DataTableFeatures, ServiceInfo>[] = [
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
      (active === "all" || item.activeState === active) &&
      (file === "all" || item.fileState === file)
  )
  return (
    <Page
      description="systemd units, state, relationships, logs, and lifecycle controls."
      interval={interval}
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
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
                {value[0].toUpperCase() + value.slice(1)}s
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <Tabs
          value={scope}
          onValueChange={(next) =>
            patch({
              scope:
                next === "system"
                  ? undefined
                  : (next as (typeof serviceScopes)[number]),
            })
          }
        >
          <TabsList>
            {serviceScopes.map((value) => (
              <TabsTrigger key={value} value={value}>
                {value[0].toUpperCase() + value.slice(1)}s
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>
      <State query={query} empty={!items.length}>
        <DataTable
          data={items}
          columns={columns}
          searchPlaceholder="Search units and descriptions"
          search={search.q}
          onSearchChange={(q) => patch({ q: q || undefined })}
          toolbar={
            <>
              <FilterSelect
                label="Active state"
                value={active}
                values={[...serviceActiveStates]}
                onChange={(next) =>
                  patch({
                    active:
                      next === "all"
                        ? undefined
                        : (next as (typeof serviceActiveStates)[number]),
                  })
                }
              />
              <FilterSelect
                label="File state"
                value={file}
                values={[...serviceFileStates]}
                onChange={(next) =>
                  patch({
                    file:
                      next === "all"
                        ? undefined
                        : (next as (typeof serviceFileStates)[number]),
                  })
                }
              />
            </>
          }
          onRowClick={(unit) =>
            navigateDetail({
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
