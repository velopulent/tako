import { useMutation, useQueryClient } from "@tanstack/react-query"
import * as React from "react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
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
  type AdministrativeRoleOperation,
  type AdministrativeRolePreview,
  type AdministrativeRoleState,
  APIError,
  api,
  type UserInfo,
} from "@/lib/api"

function roleError(error: unknown) {
  if (
    error instanceof APIError &&
    error.code === "administrative-role-protected"
  )
    return "The current or last administrator cannot be removed."
  return error instanceof Error
    ? error.message
    : "The administrator role operation failed."
}

export function AdminRoleManager({
  users,
  csrfToken,
  administrative,
  onApplied,
}: {
  users: UserInfo[]
  csrfToken: string
  administrative: boolean
  onApplied: () => void
}) {
  const client = useQueryClient()
  const [username, setUsername] = React.useState("")
  const [action, setAction] = React.useState<"grant" | "revoke">("grant")
  const [fingerprint, setFingerprint] = React.useState("")
  const [confirmation, setConfirmation] = React.useState("")
  const localUsers = users.filter((user) => user.local)
  const expectedConfirmation = `${action.toUpperCase()} ADMIN ${username}`
  const operation: AdministrativeRoleOperation = {
    action,
    username,
    role: "administrator",
    expectedFingerprint: fingerprint || undefined,
    confirmation: confirmation || undefined,
  }
  const preview = useMutation({
    mutationFn: (value: AdministrativeRoleOperation) =>
      api<AdministrativeRolePreview>("/accounts/groups/admin-role/preview", {
        method: "POST",
        body: JSON.stringify(value),
      }),
    onSuccess: (value) => {
      setFingerprint(value.current.fingerprint)
      setConfirmation("")
    },
  })
  const apply = useMutation({
    mutationFn: (value: AdministrativeRoleOperation) =>
      api<AdministrativeRoleState>("/accounts/groups/admin-role", {
        method: "POST",
        headers: { "X-CSRF-Token": csrfToken },
        body: JSON.stringify(value),
      }),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: ["groups"] })
      client.invalidateQueries({ queryKey: ["users"] })
      setFingerprint("")
      setConfirmation("")
      preview.reset()
      onApplied()
    },
  })
  const pending = preview.isPending || apply.isPending
  const canWrite = administrative && username !== ""

  return (
    <Card>
      <CardHeader>
        <CardTitle>Administrator role</CardTitle>
        <CardDescription>
          Tako manages membership in the detected local sudo or wheel group.
          Sudoers text is never exposed.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        {(preview.isError || apply.isError) && (
          <Alert variant="destructive">
            <AlertTitle>Administrator role failed</AlertTitle>
            <AlertDescription>
              {roleError(preview.error ?? apply.error)}
            </AlertDescription>
          </Alert>
        )}
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="admin-role-user">Local user</FieldLabel>
            <Select
              items={localUsers.map((user) => ({
                value: user.username,
                label: user.username,
              }))}
              value={username}
              onValueChange={(value) => {
                setUsername(value ?? "")
                setFingerprint("")
                setConfirmation("")
                preview.reset()
              }}
              disabled={pending || !administrative}
            >
              <SelectTrigger id="admin-role-user" aria-label="Local user">
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
              variant={action === "grant" ? "secondary" : "outline"}
              disabled={pending || !canWrite}
              onClick={() => {
                setAction("grant")
                setFingerprint("")
                setConfirmation("")
                preview.reset()
              }}
            >
              Grant role
            </Button>
            <Button
              type="button"
              variant={action === "revoke" ? "secondary" : "outline"}
              disabled={pending || !canWrite}
              onClick={() => {
                setAction("revoke")
                setFingerprint("")
                setConfirmation("")
                preview.reset()
              }}
            >
              Revoke role
            </Button>
          </div>
          <Field>
            <FieldLabel htmlFor="admin-role-confirmation">
              Type {expectedConfirmation} to confirm
            </FieldLabel>
            <Input
              id="admin-role-confirmation"
              value={confirmation}
              autoComplete="off"
              disabled={pending || !canWrite}
              onChange={(event) => setConfirmation(event.target.value)}
            />
          </Field>
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
              {preview.data.stale ? "Refresh required" : "Role preview"}
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
              preview.mutate({
                ...operation,
                expectedFingerprint: undefined,
                confirmation: undefined,
              })
            }
          >
            {preview.isPending ? "Previewing…" : "Preview"}
          </Button>
          <Button
            type="button"
            disabled={
              pending ||
              !canWrite ||
              !fingerprint ||
              confirmation !== expectedConfirmation
            }
            onClick={() => apply.mutate(operation)}
          >
            {apply.isPending ? "Applying…" : "Apply role change"}
          </Button>
        </div>
        {pending && <Skeleton className="h-1 w-full" />}
      </CardContent>
    </Card>
  )
}
