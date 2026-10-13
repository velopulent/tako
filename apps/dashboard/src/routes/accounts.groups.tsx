import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { createFileRoute } from "@tanstack/react-router"
import { PlusIcon, SearchIcon, Trash2Icon } from "lucide-react"
import * as React from "react"

import { useAdminAccess } from "@/components/admin-access"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
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
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
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
import { api, type GroupInfo } from "@/lib/api"
import { guardedChange } from "@/lib/guarded"
import { QueryError } from "@/lib/page"

function GroupsPage() {
  const { administrative } = useAdminAccess()
  const [filter, setFilter] = React.useState("")
  const [system, setSystem] = React.useState(false)
  const [creating, setCreating] = React.useState(false)
  const [deleting, setDeleting] = React.useState<GroupInfo>()
  const query = useQuery({
    queryKey: ["accounts", "groups"],
    queryFn: () => api<{ items: GroupInfo[] }>("/accounts/groups"),
  })
  const needle = filter.trim().toLowerCase()
  const items = (query.data?.items ?? [])
    .filter((group) => system || group.gid >= 1000 || group.members.length > 0)
    .filter(
      (group) =>
        !needle ||
        group.name.includes(needle) ||
        group.members.some((member) => member.includes(needle))
    )
    .sort((left, right) => left.name.localeCompare(right.name))
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <InputGroup className="max-w-sm flex-1">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput
            placeholder="Search groups or members"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
          />
        </InputGroup>
        <Field orientation="horizontal" className="w-auto">
          <Switch
            id="show-system-groups"
            size="sm"
            checked={system}
            onCheckedChange={setSystem}
          />
          <FieldLabel
            htmlFor="show-system-groups"
            className="font-normal text-muted-foreground"
          >
            Empty system groups
          </FieldLabel>
        </Field>
        <Button
          className="ml-auto"
          disabled={!administrative}
          onClick={() => setCreating(true)}
        >
          <PlusIcon data-icon="inline-start" />
          New group
        </Button>
      </div>
      {query.isPending ? (
        <Skeleton className="h-64" />
      ) : query.isError ? (
        <QueryError error={query.error} retry={() => void query.refetch()} />
      ) : (
        <Card className="gap-0 py-0">
          <ul className="divide-y">
            {items.map((group) => (
              <li
                key={group.name}
                className="flex items-center gap-3 px-4 py-3"
              >
                <span className="flex w-40 shrink-0 flex-col">
                  <span className="truncate text-sm font-medium">
                    {group.name}
                  </span>
                  <span className="font-mono text-xs text-muted-foreground">
                    gid {group.gid}
                  </span>
                </span>
                <span className="flex min-w-0 flex-1 flex-wrap gap-1">
                  {group.members.length ? (
                    group.members.map((member) => (
                      <Badge
                        key={member}
                        variant="outline"
                        className="font-normal"
                      >
                        {member}
                      </Badge>
                    ))
                  ) : (
                    <span className="text-sm text-muted-foreground">
                      No members
                    </span>
                  )}
                </span>
                {!group.local && <Badge variant="outline">Directory</Badge>}
                {administrative && group.local && group.gid >= 1000 && (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Delete ${group.name}`}
                    onClick={() => setDeleting(group)}
                  >
                    <Trash2Icon />
                  </Button>
                )}
              </li>
            ))}
            {!items.length && (
              <li className="px-4 py-10 text-center text-sm text-muted-foreground">
                No groups match.
              </li>
            )}
          </ul>
        </Card>
      )}
      {creating && <GroupDialog onClose={() => setCreating(false)} />}
      {deleting && (
        <GroupDialog group={deleting} onClose={() => setDeleting(undefined)} />
      )}
    </div>
  )
}

function GroupDialog({
  group,
  onClose,
}: {
  group?: GroupInfo
  onClose: () => void
}) {
  const client = useQueryClient()
  const [name, setName] = React.useState("")
  const valid = group ? true : /^[a-z_][a-z0-9_-]{0,31}$/.test(name)
  const change = useMutation({
    mutationFn: () =>
      group
        ? guardedChange(
            "/accounts/groups",
            { action: "delete", group: group.name },
            (preview) => ({
              expectedFingerprint: preview.current.fingerprint,
              confirmation: `DELETE ${group.name}`,
            })
          )
        : guardedChange(
            "/accounts/groups",
            { action: "create", group: name },
            () => ({})
          ),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["accounts"] })
      toast.add({
        title: group ? `Deleted ${group.name}` : `Created ${name}`,
        type: "success",
      })
      onClose()
    },
  })
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <form
          className="flex flex-col gap-5"
          onSubmit={(event) => {
            event.preventDefault()
            change.mutate()
          }}
        >
          <DialogHeader>
            <DialogTitle>
              {group ? `Delete ${group.name}?` : "New group"}
            </DialogTitle>
            <DialogDescription>
              {group
                ? "Members lose any access granted through this group."
                : "Add members from each user’s page."}
            </DialogDescription>
          </DialogHeader>
          {!group && (
            <Field data-invalid={name !== "" && !valid}>
              <FieldLabel htmlFor="group-name">Name</FieldLabel>
              <Input
                id="group-name"
                autoFocus
                required
                className="font-mono"
                value={name}
                aria-invalid={name !== "" && !valid}
                onChange={(event) => setName(event.target.value)}
              />
              <FieldDescription>
                Lowercase letters, digits, hyphens, and underscores.
              </FieldDescription>
            </Field>
          )}
          {change.isError && (
            <Alert variant="destructive">
              <AlertTitle>Nothing changed</AlertTitle>
              <AlertDescription>{change.error.message}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button
              type="submit"
              variant={group ? "destructive" : "default"}
              disabled={!valid || change.isPending}
            >
              {change.isPending && <Spinner data-icon="inline-start" />}
              {group ? "Delete group" : "Create"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export const Route = createFileRoute("/accounts/groups")({
  component: GroupsPage,
})
