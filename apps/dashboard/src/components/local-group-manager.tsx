import { useMutation, useQueryClient } from "@tanstack/react-query"
import * as React from "react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import {
  APIError,
  api,
  type GroupInfo,
  type LocalGroupOperation,
  type LocalGroupPreview,
  type LocalGroupState,
} from "@/lib/api"

function groupError(error: unknown) {
  if (error instanceof APIError) {
    switch (error.code) {
      case "local-group-conflict":
        return "The group changed elsewhere. Preview again before applying."
      case "local-group-protected":
        return "This system group cannot be deleted."
      case "local-group-read-only":
        return "Remote NSS groups are read-only."
    }
  }
  return error instanceof Error
    ? error.message
    : "The local group operation failed."
}

export function LocalGroupManager({
  group,
  csrfToken,
  administrative,
  onApplied,
}: {
  group?: GroupInfo
  csrfToken: string
  administrative: boolean
  onApplied: () => void
}) {
  const client = useQueryClient()
  const [action, setAction] = React.useState<"create" | "delete">(
    group ? "delete" : "create"
  )
  const [groupName, setGroupName] = React.useState(group?.name ?? "")
  const [confirmation, setConfirmation] = React.useState("")
  const [fingerprint, setFingerprint] = React.useState("")

  // Synchronize form when selected group changes. Form should reset when group
  // prop changes.
  // biome-ignore lint/correctness/useExhaustiveDependencies: resync intentionally keys off the group name only; depending on the group object would reset the form on every parent render
  React.useEffect(() => {
    if (group) {
      setGroupName(group.name)
      setAction("delete")
    } else {
      setGroupName("")
      setAction("create")
    }
    setFingerprint("")
    setConfirmation("")
  }, [group?.name])

  const operation = React.useMemo<LocalGroupOperation>(
    () => ({
      action,
      group: group?.name ?? groupName,
      expectedFingerprint: fingerprint || undefined,
      confirmation: confirmation || undefined,
    }),
    [action, confirmation, fingerprint, group?.name, groupName]
  )

  const preview = useMutation({
    mutationFn: (value: LocalGroupOperation) =>
      api<LocalGroupPreview>("/accounts/groups/preview", {
        method: "POST",
        body: JSON.stringify(value),
      }),
    onSuccess: (value) => {
      setFingerprint(value.current.fingerprint)
    },
  })

  const apply = useMutation({
    mutationFn: (value: LocalGroupOperation) =>
      api<LocalGroupState>("/accounts/groups", {
        method: "POST",
        headers: { "X-CSRF-Token": csrfToken },
        body: JSON.stringify(value),
      }),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: ["groups"] })
      onApplied()
      preview.reset()
      setFingerprint("")
    },
  })

  const pending = preview.isPending || apply.isPending
  const destructive = action === "delete"
  const expectedConfirmation = `DELETE ${group?.name ?? groupName}`
  const canCreate = administrative && action === "create" && groupName !== ""
  const canDelete =
    administrative && action === "delete" && Boolean(group) && group?.local
  const canEdit = canCreate || canDelete

  return (
    <div className="flex flex-col gap-5">
      {!administrative && (
        <Alert>
          <AlertTitle>Administrative access required</AlertTitle>
          <AlertDescription>
            Gain temporary administrative access before changing local groups.
          </AlertDescription>
        </Alert>
      )}
      {group && !group.local && (
        <Alert>
          <AlertTitle>Remote group is read-only</AlertTitle>
          <AlertDescription>
            Tako cannot mutate membership or delete a remote identity provider
            group.
          </AlertDescription>
        </Alert>
      )}
      {(preview.isError || apply.isError) && (
        <Alert variant="destructive">
          <AlertTitle>Group operation failed</AlertTitle>
          <AlertDescription>
            {groupError(preview.error ?? apply.error)}
          </AlertDescription>
        </Alert>
      )}
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor="local-group-name">Group name</FieldLabel>
          <Input
            id="local-group-name"
            maxLength={256}
            value={group?.name ?? groupName}
            placeholder="developers"
            disabled={pending || Boolean(group) || !administrative}
            onChange={(event) => setGroupName(event.target.value)}
          />
          <FieldDescription>
            Group names are fixed at creation; use membership tools to manage
            members.
          </FieldDescription>
        </Field>
        {group && (
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant={action === "delete" ? "secondary" : "outline"}
              disabled={pending || !canDelete}
              onClick={() => {
                setAction("delete")
                setFingerprint("")
                setConfirmation("")
                preview.reset()
              }}
            >
              Delete
            </Button>
          </div>
        )}
        {destructive && (
          <Field>
            <FieldLabel htmlFor="local-group-confirmation">
              Type {expectedConfirmation} to confirm
            </FieldLabel>
            <Input
              id="local-group-confirmation"
              value={confirmation}
              autoComplete="off"
              disabled={pending || !canEdit}
              onChange={(event) => setConfirmation(event.target.value)}
            />
          </Field>
        )}
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
            {preview.data.stale ? "Refresh required" : "Group preview"}
          </AlertTitle>
          <AlertDescription>
            {preview.data.reason ??
              (preview.data.changes.length
                ? `Changes: ${preview.data.changes.join(", ")}.`
                : "No changes detected.")}
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
          disabled={pending || !canEdit}
          onClick={() =>
            preview.mutate({ ...operation, expectedFingerprint: undefined })
          }
        >
          {preview.isPending ? "Previewing…" : "Preview"}
        </Button>
        <Button
          type="button"
          disabled={
            pending ||
            !canEdit ||
            (action !== "create" && !fingerprint) ||
            (destructive && confirmation !== expectedConfirmation)
          }
          onClick={() => apply.mutate(operation)}
        >
          {apply.isPending ? "Applying…" : "Apply group change"}
        </Button>
      </div>
      {pending && <Skeleton className="h-1 w-full" />}
    </div>
  )
}
