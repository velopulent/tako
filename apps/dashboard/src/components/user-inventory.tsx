import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"

import { api, type SessionResponse, type UserInfo } from "@/lib/api"
import { UserAccountManager } from "@/components/user-account-manager"
import { AccountLoginHistory } from "@/components/account-login-history"
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
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"

const columns: ColumnDef<UserInfo>[] = [
  { accessorKey: "username", header: "User" },
  { accessorKey: "uid", header: "UID", meta: { align: "end" }, size: 88 },
  { accessorKey: "name", header: "Name" },
  { accessorKey: "home", header: "Home" },
  { accessorKey: "shell", header: "Shell" },
  {
    accessorKey: "groups",
    header: "Groups",
    cell: ({ row }) => (row.original.groups ?? []).join(", ") || "-",
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

export function UserInventory({
  search,
  onSearchChange,
}: {
  search?: string
  onSearchChange?: (value: string) => void
}) {
  const [selected, setSelected] = React.useState<UserInfo>()
  const [creating, setCreating] = React.useState(false)
  const [activeTab, setActiveTab] = React.useState("details")
  const query = useQuery({
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

  React.useEffect(() => {
    const handler = () => {
      setSelected(undefined)
      setCreating(true)
      setActiveTab("details")
    }
    window.addEventListener("accounts:create-user", handler)
    return () => window.removeEventListener("accounts:create-user", handler)
  }, [])

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
  const sheetOpen = creating || Boolean(selected)
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
        search={search}
        onSearchChange={onSearchChange}
        onRowClick={(row) => {
          setCreating(false)
          setSelected(row)
          setActiveTab("details")
        }}
        toolbar={
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              setSelected(undefined)
              setCreating(true)
              setActiveTab("details")
            }}
          >
            Create user
          </Button>
        }
      />
      <Sheet
        open={sheetOpen}
        onOpenChange={(open) => {
          if (!open) {
            setCreating(false)
            setSelected(undefined)
          }
        }}
      >
        <SheetContent side="right" className="w-full overflow-y-auto sm:max-w-xl">
          <SheetHeader>
            <SheetTitle>
              {creating ? "Create local account" : `Manage ${selected?.username ?? ""}`}
            </SheetTitle>
            <SheetDescription>
              Shadow-utils changes are previewed, fingerprint-checked, and
              verified after they run.
            </SheetDescription>
          </SheetHeader>
          <div className="px-4 pb-4">
            {selected ? (
              <Tabs
                value={activeTab}
                onValueChange={setActiveTab}
                orientation="horizontal"
                className="mt-2 flex flex-col"
              >
                <TabsList className="w-full justify-start">
                  <TabsTrigger value="details">Details</TabsTrigger>
                  <TabsTrigger value="history">Login history</TabsTrigger>
                </TabsList>
                <TabsContent value="details" className="mt-4">
                  <UserAccountManager
                    key={selected.username}
                    user={selected}
                    csrfToken={session.data?.csrfToken ?? ""}
                    administrative={admin.data?.administrative === true}
                    onApplied={() => {
                      setCreating(false)
                      setSelected(undefined)
                    }}
                  />
                </TabsContent>
                <TabsContent value="history" className="mt-4">
                  <AccountLoginHistory username={selected.username} />
                </TabsContent>
              </Tabs>
            ) : (
              <div className="mt-4">
                <UserAccountManager
                  key="create"
                  user={undefined}
                  csrfToken={session.data?.csrfToken ?? ""}
                  administrative={admin.data?.administrative === true}
                  onApplied={() => {
                    setCreating(false)
                    setSelected(undefined)
                  }}
                />
              </div>
            )}
          </div>
        </SheetContent>
      </Sheet>
    </div>
  )
}
