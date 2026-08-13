import { useQuery } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"

import { api, type UserInfo } from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { DataTable } from "@/components/data-table"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"

const columns: ColumnDef<UserInfo>[] = [
  { accessorKey: "username", header: "User" },
  { accessorKey: "uid", header: "UID" },
  { accessorKey: "name", header: "Name" },
  { accessorKey: "home", header: "Home" },
  { accessorKey: "shell", header: "Shell" },
  {
    accessorKey: "groups",
    header: "Groups",
    cell: ({ row }) => row.original.groups.join(", ") || "-",
  },
  {
    accessorKey: "source",
    header: "Identity source",
    cell: ({ row }) => (
      <Badge variant={row.original.local ? "secondary" : "outline"}>
        {row.original.local ? "Local" : "NSS read-only"}
      </Badge>
    ),
  },
]

export function UserInventory() {
  const query = useQuery({
    queryKey: ["users"],
    queryFn: () => api<{ items: UserInfo[] }>("/users"),
  })
  const items = query.data?.items ?? []
  if (query.isPending) return <Skeleton className="h-72" />
  if (query.isError) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Identity inventory unavailable</AlertTitle>
        <AlertDescription>{query.error.message}</AlertDescription>
      </Alert>
    )
  }
  if (!items.length) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>No identities returned</EmptyTitle>
          <EmptyDescription>NSS did not return any users.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  const localCount = items.filter((item) => item.local).length
  const remoteCount = items.length - localCount
  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm text-muted-foreground" aria-live="polite">
        {items.length} identities · {localCount} local · {remoteCount} NSS
        read-only
      </p>
      <DataTable
        data={items}
        columns={columns}
        searchPlaceholder="Search users, groups, or identity source"
      />
    </div>
  )
}
