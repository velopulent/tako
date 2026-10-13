import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  createFileRoute,
  getRouteApi,
  useNavigate,
} from "@tanstack/react-router"
import { PlusIcon, SearchIcon } from "lucide-react"
import * as React from "react"

import { AdminRequired, useAdminAccess } from "@/components/admin-access"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { toast } from "@/components/ui/toast"
import { adminGroups, initials, UserSheet } from "@/components/user-sheet"
import { api, type GroupInfo, type UserInfo } from "@/lib/api"
import { QueryError } from "@/lib/page"
import { useSession } from "@/lib/session"

type UsersSearch = { q?: string; user?: string; system?: boolean }

function UsersPage() {
  const search = getRouteApi("/accounts/users").useSearch()
  const navigate = useNavigate({ from: "/accounts/users" })
  const session = useSession()
  const { administrative } = useAdminAccess()
  const [creating, setCreating] = React.useState(false)
  const users = useQuery({
    queryKey: ["accounts", "users"],
    queryFn: () => api<{ items: UserInfo[] }>("/accounts/users"),
  })
  const groups = useQuery({
    queryKey: ["accounts", "groups"],
    queryFn: () => api<{ items: GroupInfo[] }>("/accounts/groups"),
  })
  const update = (next: Partial<UsersSearch>) =>
    navigate({
      search: (previous) => ({ ...previous, ...next }),
      replace: true,
    })
  const needle = (search.q ?? "").trim().toLowerCase()
  const all = users.data?.items ?? []
  const items = all
    .filter(
      (user) =>
        search.system || !user.system || user.username === session.user.username
    )
    .filter(
      (user) =>
        !needle ||
        `${user.username} ${user.name}`.toLowerCase().includes(needle)
    )
    .sort(
      (left, right) =>
        Number(right.username === session.user.username) -
          Number(left.username === session.user.username) ||
        left.username.localeCompare(right.username)
    )
  const selected = all.find((user) => user.username === search.user)
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <InputGroup className="max-w-sm flex-1">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput
            placeholder="Search users"
            value={search.q ?? ""}
            onChange={(event) => update({ q: event.target.value || undefined })}
          />
        </InputGroup>
        <Field orientation="horizontal" className="w-auto">
          <Switch
            id="show-system-users"
            size="sm"
            checked={Boolean(search.system)}
            onCheckedChange={(on) => update({ system: on || undefined })}
          />
          <FieldLabel
            htmlFor="show-system-users"
            className="font-normal text-muted-foreground"
          >
            System accounts
          </FieldLabel>
        </Field>
        <Button
          className="ml-auto"
          disabled={!administrative}
          onClick={() => setCreating(true)}
        >
          <PlusIcon data-icon="inline-start" />
          New user
        </Button>
      </div>
      {!administrative && (
        <AdminRequired>
          Creating and changing accounts requires administrative access.
        </AdminRequired>
      )}
      {users.isPending ? (
        <Skeleton className="h-64" />
      ) : users.isError ? (
        <QueryError error={users.error} retry={() => void users.refetch()} />
      ) : (
        <Card className="gap-0 py-0">
          <ul className="divide-y">
            {items.map((user) => (
              <li key={user.username}>
                <button
                  type="button"
                  className="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none"
                  onClick={() => update({ user: user.username })}
                >
                  <Avatar className="size-9">
                    <AvatarFallback>{initials(user)}</AvatarFallback>
                  </Avatar>
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate text-sm font-medium">
                      {user.name || user.username}
                    </span>
                    <span className="truncate font-mono text-xs text-muted-foreground">
                      {user.username}
                    </span>
                  </span>
                  <span className="hidden flex-wrap justify-end gap-1.5 sm:flex">
                    {user.username === session.user.username && (
                      <Badge>You</Badge>
                    )}
                    {user.groups.some((group) =>
                      adminGroups.includes(group)
                    ) && <Badge variant="secondary">Administrator</Badge>}
                    {!user.local && <Badge variant="outline">Directory</Badge>}
                    {user.system && <Badge variant="outline">System</Badge>}
                  </span>
                </button>
              </li>
            ))}
            {!items.length && (
              <li className="px-4 py-10 text-center text-sm text-muted-foreground">
                No users match.
              </li>
            )}
          </ul>
        </Card>
      )}
      {selected && (
        <UserSheet
          key={selected.username}
          user={selected}
          groups={groups.data?.items ?? []}
          onClose={() => update({ user: undefined })}
        />
      )}
      {creating && (
        <CreateUser
          onClose={() => setCreating(false)}
          onCreated={(username) => update({ user: username })}
        />
      )}
    </div>
  )
}

function CreateUser({
  onClose,
  onCreated,
}: {
  onClose: () => void
  onCreated: (username: string) => void
}) {
  const client = useQueryClient()
  const [username, setUsername] = React.useState("")
  const [name, setName] = React.useState("")
  const valid = /^[a-z_][a-z0-9_-]{0,31}$/.test(username)
  const create = useMutation({
    mutationFn: () =>
      api("/accounts/users/account", {
        method: "POST",
        body: JSON.stringify({
          action: "create",
          username,
          name: name || undefined,
        }),
      }),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ["accounts"] })
      toast.add({
        title: `Created ${username}`,
        description: "Set a password so they can sign in.",
        type: "success",
      })
      onClose()
      onCreated(username)
    },
  })
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <form
          className="flex flex-col gap-5"
          onSubmit={(event) => {
            event.preventDefault()
            create.mutate()
          }}
        >
          <DialogHeader>
            <DialogTitle>New user</DialogTitle>
            <DialogDescription>
              Creates a local account with a home directory.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup className="gap-3">
            <Field>
              <FieldLabel htmlFor="new-name">Full name</FieldLabel>
              <Input
                id="new-name"
                autoFocus
                value={name}
                onChange={(event) => {
                  const value = event.target.value
                  if (!username || username === suggest(name))
                    setUsername(suggest(value))
                  setName(value)
                }}
              />
            </Field>
            <Field data-invalid={username !== "" && !valid}>
              <FieldLabel htmlFor="new-username">Username</FieldLabel>
              <Input
                id="new-username"
                required
                className="font-mono"
                value={username}
                aria-invalid={username !== "" && !valid}
                onChange={(event) => setUsername(event.target.value)}
              />
              <FieldDescription>
                Lowercase letters, digits, hyphens, and underscores.
              </FieldDescription>
            </Field>
          </FieldGroup>
          {create.isError && (
            <Alert variant="destructive">
              <AlertTitle>User not created</AlertTitle>
              <AlertDescription>{create.error.message}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={!valid || create.isPending}>
              {create.isPending && <Spinner data-icon="inline-start" />}
              Create
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function suggest(name: string) {
  return name
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[^a-z0-9]+/g, "")
    .slice(0, 32)
}

export const Route = createFileRoute("/accounts/users")({
  validateSearch: (search: Record<string, unknown>): UsersSearch => ({
    q: typeof search.q === "string" && search.q ? search.q : undefined,
    user:
      typeof search.user === "string" && search.user ? search.user : undefined,
    system:
      search.system === true || search.system === "true" ? true : undefined,
  }),
  component: UsersPage,
})
