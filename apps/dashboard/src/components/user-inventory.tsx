import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"

import { api, type SessionResponse, type UserInfo } from "@/lib/api"
import { UserAccountManager } from "@/components/user-account-manager"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
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
  const [selected, setSelected] = React.useState<UserInfo>()
  const [creating, setCreating] = React.useState(false)
  const query = useQuery({
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
      {(creating || selected) && (
        <UserAccountManager
          key={creating ? "create" : selected?.username}
          user={creating ? undefined : selected}
          csrfToken={session.data?.csrfToken ?? ""}
          administrative={admin.data?.administrative === true}
          onApplied={() => {
            setCreating(false)
            setSelected(undefined)
          }}
        />
      )}
      <DataTable
        data={items}
        columns={columns}
        searchPlaceholder="Search users, groups, or identity source"
        onRowClick={(row) => {
          setCreating(false)
          setSelected(row)
        }}
        toolbar={
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              setSelected(undefined)
              setCreating(true)
            }}
          >
            Create local account
          </Button>
        }
      />
    </div>
  )
}
