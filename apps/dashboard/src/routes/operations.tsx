import { createFileRoute, getRouteApi, useNavigate } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"

import { api, type OperationReceipt } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { DataTable } from "@/components/data-table"
import { qSearch } from "@/lib/search"
import { Page, State } from "@/lib/page"

function OperationsPage() {
  const search = getRouteApi("/operations").useSearch()
  const navigate = useNavigate({ from: "/operations" })
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

export const Route = createFileRoute("/operations")({
  validateSearch: qSearch,
  component: OperationsPage,
})
