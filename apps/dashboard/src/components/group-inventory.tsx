import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"

import {
  api,
  type GroupInfo,
  type SessionResponse,
  type UserInfo,
} from "@/lib/api"
import { AdminRoleManager } from "@/components/admin-role-manager"
import { GroupMembershipManager } from "@/components/group-membership-manager"
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

const columns: ColumnDef<GroupInfo>[] = [
  { accessorKey: "name", header: "Group" },
  { accessorKey: "gid", header: "GID" },
  {
    accessorKey: "members",
    header: "Members",
    cell: ({ row }) => row.original.members.join(", ") || "-",
  },
  {
    accessorKey: "source",
    header: "Group source",
    cell: ({ row }) => (
      <Badge variant={row.original.local ? "secondary" : "outline"}>
        {row.original.local ? "Local" : "NSS read-only"}
      </Badge>
    ),
  },
]

export function GroupInventory() {
  const [selected, setSelected] = React.useState<GroupInfo>()
  const query = useQuery({
    queryKey: ["groups"],
    queryFn: () => api<{ items: GroupInfo[] }>("/groups"),
  })
  const users = useQuery({
    queryKey: ["users"],
    queryFn: () => api<{ items: UserInfo[] }>("/users"),
  })
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const admin = useQuery({
    queryKey: ["admin"],
    queryFn: () => api<{ administrative: boolean }>("/admin"),
    refetchInterval: 10_000,
  })
  const items = query.data?.items ?? []
  if (query.isPending) return <Skeleton className="h-72" />
  if (query.isError) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Group inventory unavailable</AlertTitle>
        <AlertDescription>{query.error.message}</AlertDescription>
      </Alert>
    )
  }
  if (!items.length) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>No groups returned</EmptyTitle>
          <EmptyDescription>NSS did not return any groups.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  const localCount = items.filter((item) => item.local).length
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <p className="text-sm text-muted-foreground" aria-live="polite">
          {items.length} groups · {localCount} local ·{" "}
          {items.length - localCount} NSS read-only
        </p>
      </div>
      {selected && (
        <GroupMembershipManager
          key={selected.name}
          group={selected}
          users={users.data?.items ?? []}
          csrfToken={session.data?.csrfToken ?? ""}
          administrative={admin.data?.administrative === true}
          onApplied={() => setSelected(undefined)}
        />
      )}
      <AdminRoleManager
        users={users.data?.items ?? []}
        csrfToken={session.data?.csrfToken ?? ""}
        administrative={admin.data?.administrative === true}
        onApplied={() => undefined}
      />
      <DataTable
        data={items}
        columns={columns}
        searchPlaceholder="Search groups, members, or identity source"
        onRowClick={setSelected}
      />
    </div>
  )
}
