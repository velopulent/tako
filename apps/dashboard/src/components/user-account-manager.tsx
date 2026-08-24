import * as React from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"

import {
  api,
  APIError,
  type LocalAccountOperation,
  type LocalAccountPreview,
  type LocalAccountState,
  type UserInfo,
} from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"

type AccountAction = LocalAccountOperation["action"]

function accountError(error: unknown) {
  if (error instanceof APIError && error.code === "local-account-conflict") {
    return "The account changed elsewhere. Preview again before applying."
  }
  if (error instanceof APIError && error.code === "local-account-protected") {
    return "The current operator account cannot be locked or deleted."
  }
  return error instanceof Error
    ? error.message
    : "The local account operation failed."
}

export function UserAccountManager({
  user,
  csrfToken,
  administrative,
  onApplied,
}: {
  user?: UserInfo
  csrfToken: string
  administrative: boolean
  onApplied: () => void
}) {
  const client = useQueryClient()
  const [action, setAction] = React.useState<AccountAction>(
    user ? "update" : "create"
  )
  const [username, setUsername] = React.useState("")
  const [name, setName] = React.useState(user?.name ?? "")
  const [home, setHome] = React.useState(user?.home ?? "")
  const [shell, setShell] = React.useState(user?.shell ?? "")
  const [confirmation, setConfirmation] = React.useState("")
  const [fingerprint, setFingerprint] = React.useState("")

  const operation = React.useMemo<LocalAccountOperation>(
    () => ({
      action,
      username: user?.username ?? username,
      name: action === "update" || action === "create" ? name : undefined,
      home: action === "update" || action === "create" ? home : undefined,
      shell: action === "update" || action === "create" ? shell : undefined,
      expectedFingerprint: fingerprint || undefined,
      confirmation: confirmation || undefined,
    }),
    [
      action,
      confirmation,
      fingerprint,
      home,
      name,
      shell,
      user?.username,
      username,
    ]
  )
  const preview = useMutation({
    mutationFn: (value: LocalAccountOperation) =>
      api<LocalAccountPreview>("/accounts/users/account/preview", {
        method: "POST",
        body: JSON.stringify(value),
      }),
    onSuccess: (value) => {
      setFingerprint(value.current.fingerprint)
    },
  })
  const apply = useMutation({
    mutationFn: (value: LocalAccountOperation) =>
      api<LocalAccountState>("/accounts/users/account", {
        method: "POST",
        headers: { "X-CSRF-Token": csrfToken },
        body: JSON.stringify(value),
      }),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: ["users"] })
      onApplied()
      preview.reset()
      setFingerprint("")
    },
  })
  const pending = preview.isPending || apply.isPending
  const selectedRemote = user && !user.local
  const destructive = action === "lock" || action === "delete"
  const expectedConfirmation =
    action === "delete"
      ? `DELETE ${user?.username ?? ""}`
      : `LOCK ${user?.username ?? ""}`
  const canEdit =
    administrative && !selectedRemote && Boolean(user || action === "create")

  return (
    <div className="flex flex-col gap-5">
      {user && (
        <div className="flex">
          <Badge variant={user.local ? "secondary" : "outline"}>
            {user.local ? "Local" : "NSS read-only"}
          </Badge>
        </div>
      )}
        {!administrative && (
          <Alert>
            <AlertTitle>Administrative access required</AlertTitle>
            <AlertDescription>
              Gain temporary administrative access before changing local
              accounts.
            </AlertDescription>
          </Alert>
        )}
        {selectedRemote && (
          <Alert>
            <AlertTitle>Remote identity is read-only</AlertTitle>
            <AlertDescription>
              This NSS entry is visible for inventory but cannot be changed by
              Tako.
            </AlertDescription>
          </Alert>
        )}
        {(preview.isError || apply.isError) && (
          <Alert variant="destructive">
            <AlertTitle>Account operation failed</AlertTitle>
            <AlertDescription>
              {accountError(preview.error ?? apply.error)}
            </AlertDescription>
          </Alert>
        )}
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="account-username">Username</FieldLabel>
            <Input
              id="account-username"
              maxLength={32}
              value={user?.username ?? username}
              placeholder="new-user"
              disabled={pending || Boolean(user) || !canEdit}
              onChange={(event) => setUsername(event.target.value)}
            />
            <FieldDescription>
              Usernames are fixed before creation and cannot be renamed here.
            </FieldDescription>
          </Field>
          {user && (
            <div className="flex flex-wrap gap-2">
              {(["update", "lock", "unlock", "delete"] as AccountAction[]).map(
                (value) => (
                  <Button
                    key={value}
                    type="button"
                    variant={value === action ? "secondary" : "outline"}
                    disabled={pending || !canEdit}
                    onClick={() => {
                      setAction(value)
                      setFingerprint("")
                      setConfirmation("")
                      preview.reset()
                    }}
                  >
                    {value[0].toUpperCase() + value.slice(1)}
                  </Button>
                )
              )}
            </div>
          )}
          {(action === "update" || action === "create") && (
            <>
              <Field>
                <FieldLabel htmlFor="account-name">Display name</FieldLabel>
                <Input
                  id="account-name"
                  maxLength={256}
                  value={name}
                  disabled={pending || !canEdit}
                  onChange={(event) => setName(event.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="account-home">Home directory</FieldLabel>
                <Input
                  id="account-home"
                  maxLength={4096}
                  value={home}
                  placeholder={user ? "/home/user" : undefined}
                  disabled={pending || !canEdit}
                  onChange={(event) => setHome(event.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="account-shell">Login shell</FieldLabel>
                <Input
                  id="account-shell"
                  maxLength={4096}
                  value={shell}
                  placeholder="/bin/bash"
                  disabled={pending || !canEdit}
                  onChange={(event) => setShell(event.target.value)}
                />
              </Field>
            </>
          )}
          {destructive && (
            <Field>
              <FieldLabel htmlFor="account-confirmation">
                Type {expectedConfirmation} to confirm
              </FieldLabel>
              <Input
                id="account-confirmation"
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
              {preview.data.stale ? "Refresh required" : "Account preview"}
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
            {apply.isPending ? "Applying…" : "Apply account change"}
          </Button>
        </div>
        {pending && <Skeleton className="h-1 w-full" />}
    </div>
  )
}
