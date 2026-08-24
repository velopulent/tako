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
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"

const columns: ColumnDef<GroupInfo>[] = [
  { accessorKey: "name", header: "Group" },
  { accessorKey: "gid", header: "GID", meta: { align: "end" }, size: 88 },
  {
    accessorKey: "members",
    header: "Members",
    cell: ({ row }) => (row.original.members ?? []).join(", ") || "-",
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

export function GroupInventory({
  search,
  onSearchChange,
}: {
  search?: string
  onSearchChange?: (value: string) => void
}) {
  const [selected, setSelected] = React.useState<GroupInfo>()
  const query = useQuery({
    queryKey: ["groups"],
    queryFn: () => api<{ items: GroupInfo[] }>("/accounts/groups"),
  })
  const users = useQuery({
    queryKey: ["users"],
    queryFn: () => api<{ items: UserInfo[] }>("/accounts/users"),
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
  const membershipOpen = Boolean(selected)
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <p className="text-sm text-muted-foreground" aria-live="polite">
            {items.length} groups · {localCount} local ·{" "}
            {items.length - localCount} NSS read-only
          </p>
        </div>
        <DataTable
          data={items}
          columns={columns}
          searchPlaceholder="Search groups, members, or identity source"
          search={search}
          onSearchChange={onSearchChange}
          onRowClick={setSelected}
        />
      </div>
      <AdminRoleManager
        users={users.data?.items ?? []}
        csrfToken={session.data?.csrfToken ?? ""}
        administrative={admin.data?.administrative === true}
        onApplied={() => undefined}
      />

      <Sheet
        open={membershipOpen}
        onOpenChange={(open) => {
          if (!open) setSelected(undefined)
        }}
      >
        <SheetContent side="right" className="w-full overflow-y-auto sm:max-w-xl">
          <SheetHeader>
            <SheetTitle>Manage {selected?.name ?? ""} membership</SheetTitle>
            <SheetDescription>
              Changes use the local group database only. Remote NSS groups remain
              read-only.
            </SheetDescription>
          </SheetHeader>
          <div className="px-4 pb-4">
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
          </div>
        </SheetContent>
      </Sheet>

    </div>
  )
}
