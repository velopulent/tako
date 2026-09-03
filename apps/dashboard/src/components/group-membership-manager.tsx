import { useMutation, useQueryClient } from "@tanstack/react-query"
import * as React from "react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import {
  APIError,
  api,
  type GroupInfo,
  type GroupMembershipOperation,
  type GroupMembershipPreview,
  type GroupMembershipState,
  type UserInfo,
} from "@/lib/api"

function groupError(error: unknown) {
  if (error instanceof APIError && error.code === "group-conflict")
    return "The group changed elsewhere. Preview again before applying."
  return error instanceof Error ? error.message : "The group operation failed."
}

export function GroupMembershipManager({
  group,
  users,
  csrfToken,
  administrative,
  onApplied,
}: {
  group: GroupInfo
  users: UserInfo[]
  csrfToken: string
  administrative: boolean
  onApplied: () => void
}) {
  const client = useQueryClient()
  const [username, setUsername] = React.useState("")
  const [action, setAction] = React.useState<"add" | "remove">("add")
  const [fingerprint, setFingerprint] = React.useState("")
  const localUsers = users.filter((user) => user.local)
  const operation: GroupMembershipOperation = {
    action,
    username,
    group: group.name,
    expectedFingerprint: fingerprint || undefined,
  }
  const preview = useMutation({
    mutationFn: (value: GroupMembershipOperation) =>
      api<GroupMembershipPreview>("/accounts/groups/membership/preview", {
        method: "POST",
        body: JSON.stringify(value),
      }),
    onSuccess: (value) => setFingerprint(value.current.fingerprint),
  })
  const apply = useMutation({
    mutationFn: (value: GroupMembershipOperation) =>
      api<GroupMembershipState>("/accounts/groups/membership", {
        method: "POST",
        headers: { "X-CSRF-Token": csrfToken },
        body: JSON.stringify(value),
      }),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: ["groups"] })
      client.invalidateQueries({ queryKey: ["users"] })
      setFingerprint("")
      preview.reset()
      onApplied()
    },
  })
  const pending = preview.isPending || apply.isPending
  const canWrite =
    administrative && group.local && group.mutable && username !== ""

  return (
    <div className="flex flex-col gap-5">
      {!group.local && (
        <Alert>
          <AlertTitle>Remote group is read-only</AlertTitle>
          <AlertDescription>
            Tako cannot mutate membership in a remote identity provider.
          </AlertDescription>
        </Alert>
      )}
      {(preview.isError || apply.isError) && (
        <Alert variant="destructive">
          <AlertTitle>Membership operation failed</AlertTitle>
          <AlertDescription>
            {groupError(preview.error ?? apply.error)}
          </AlertDescription>
        </Alert>
      )}
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor="membership-user">Local user</FieldLabel>
          <Select
            items={localUsers.map((user) => ({
              value: user.username,
              label: user.username,
            }))}
            value={username}
            onValueChange={(value) => {
              setUsername(value ?? "")
              setFingerprint("")
              preview.reset()
            }}
            disabled={pending || !administrative || !group.local}
          >
            <SelectTrigger id="membership-user" aria-label="Local user">
              <SelectValue placeholder="Choose a local user" />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {localUsers.map((user) => (
                  <SelectItem key={user.username} value={user.username}>
                    {user.username}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant={action === "add" ? "secondary" : "outline"}
            disabled={pending || !canWrite}
            onClick={() => {
              setAction("add")
              setFingerprint("")
              preview.reset()
            }}
          >
            Add member
          </Button>
          <Button
            type="button"
            variant={action === "remove" ? "secondary" : "outline"}
            disabled={pending || !canWrite}
            onClick={() => {
              setAction("remove")
              setFingerprint("")
              preview.reset()
            }}
          >
            Remove member
          </Button>
        </div>
      </FieldGroup>
      {preview.data && (
        <Alert
          variant={
            preview.data.stale || !preview.data.allowed
              ? "destructive"
              : "default"
          }
        >
          <AlertTitle>
            {preview.data.stale ? "Refresh required" : "Membership preview"}
          </AlertTitle>
          <AlertDescription>
            {preview.data.reason ??
              `Changes: ${preview.data.changes.join(", ")}.`}
            {preview.data.warnings.length
              ? ` ${preview.data.warnings.join(" ")}`
              : ""}
          </AlertDescription>
        </Alert>
      )}
      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="outline"
          disabled={pending || !canWrite}
          onClick={() =>
            preview.mutate({ ...operation, expectedFingerprint: undefined })
          }
        >
          {preview.isPending ? "Previewing…" : "Preview"}
        </Button>
        <Button
          type="button"
          disabled={pending || !canWrite || !fingerprint}
          onClick={() => apply.mutate(operation)}
        >
          {apply.isPending ? "Applying…" : "Apply membership"}
        </Button>
      </div>
      {pending && <Skeleton className="h-1 w-full" />}
    </div>
  )
}
