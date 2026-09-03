import { useMutation, useQuery } from "@tanstack/react-query"
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
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
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
  APIError,
  api,
  type PasswordChangeOperation,
  type SessionResponse,
  type UserInfo,
} from "@/lib/api"

function passwordError(error: unknown) {
  if (error instanceof APIError) {
    switch (error.code) {
      case "password-authentication-failed":
        return "The current password was not accepted."
      case "password-expired":
        return "The password is expired; follow the host policy to change it."
      case "password-policy-failed":
        return "The host password policy rejected the new password."
      case "administrative-access-required":
        return "Administrative access is required for a reset."
    }
  }
  return error instanceof Error
    ? error.message
    : "The password operation failed."
}

export function PasswordManager() {
  const [mode, setMode] = React.useState<"change" | "reset">("change")
  const [target, setTarget] = React.useState("")
  const [currentPassword, setCurrentPassword] = React.useState("")
  const [newPassword, setNewPassword] = React.useState("")
  const [confirmation, setConfirmation] = React.useState("")
  const [clientError, setClientError] = React.useState("")
  const [success, setSuccess] = React.useState(false)
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const users = useQuery({
    queryKey: ["users"],
    queryFn: () => api<{ items: UserInfo[] }>("/accounts/users"),
  })
  const localUsers = (users.data?.items ?? []).filter((user) => user.local)
  const administrative = session.data?.administrative === true
  const selectedTarget = target || localUsers[0]?.username || ""
  const clearSecrets = () => {
    setCurrentPassword("")
    setNewPassword("")
    setConfirmation("")
  }

  const mutation = useMutation({
    mutationFn: (operation: PasswordChangeOperation) =>
      api<void>("/accounts/users/password", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(operation),
      }),
    onSuccess: () => {
      setClientError("")
      setSuccess(true)
    },
    onSettled: clearSecrets,
  })

  const pending = mutation.isPending
  const loading = session.isPending || users.isPending
  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setSuccess(false)
    setClientError("")
    if (newPassword === "" || newPassword !== confirmation) {
      setClientError("New password and confirmation must match.")
      return
    }
    if (mode === "change") {
      if (!currentPassword) {
        setClientError("Enter your current password.")
        return
      }
      mutation.mutate({
        action: "change",
        currentPassword,
        newPassword,
        confirmation,
      })
      return
    }
    if (!administrative || !selectedTarget) {
      setClientError("Choose a local user after gaining Administrative access.")
      return
    }
    mutation.mutate({
      action: "reset",
      username: selectedTarget,
      newPassword,
      confirmation,
    })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Password management</CardTitle>
        <CardDescription>
          Passwords are sent only to the privileged PAM boundary and are never
          stored in Tako.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {loading && <Skeleton className="h-48" />}
        {!loading && session.isError && (
          <Alert variant="destructive">
            <AlertTitle>Session unavailable</AlertTitle>
            <AlertDescription>{session.error.message}</AlertDescription>
          </Alert>
        )}
        {!loading && !session.isError && (
          <form onSubmit={submit} className="space-y-5">
            {(mutation.isError || clientError) && (
              <Alert variant="destructive">
                <AlertTitle>Password operation failed</AlertTitle>
                <AlertDescription>
                  {clientError || passwordError(mutation.error)}
                </AlertDescription>
              </Alert>
            )}
            {success && (
              <Alert>
                <AlertTitle>Password updated</AlertTitle>
                <AlertDescription>
                  The password operation completed without storing the secret.
                </AlertDescription>
              </Alert>
            )}
            <FieldGroup>
              <Field>
                <FieldLabel>Password action</FieldLabel>
                <div className="flex flex-wrap gap-2">
                  <Button
                    type="button"
                    variant={mode === "change" ? "secondary" : "outline"}
                    disabled={pending}
                    onClick={() => {
                      setMode("change")
                      clearSecrets()
                      setClientError("")
                      mutation.reset()
                    }}
                  >
                    Change my password
                  </Button>
                  <Button
                    type="button"
                    variant={mode === "reset" ? "secondary" : "outline"}
                    disabled={pending || !administrative}
                    onClick={() => {
                      setMode("reset")
                      clearSecrets()
                      setClientError("")
                      mutation.reset()
                    }}
                  >
                    Reset another password
                  </Button>
                </div>
              </Field>
              {mode === "reset" && (
                <Field>
                  <FieldLabel htmlFor="password-target">Local user</FieldLabel>
                  {users.isError ? (
                    <Alert variant="destructive">
                      <AlertTitle>Users unavailable</AlertTitle>
                      <AlertDescription>{users.error.message}</AlertDescription>
                    </Alert>
                  ) : localUsers.length === 0 ? (
                    <Empty>
                      <EmptyHeader>
                        <EmptyTitle>No local users</EmptyTitle>
                        <EmptyDescription>
                          NSS returned no local account eligible for reset.
                        </EmptyDescription>
                      </EmptyHeader>
                    </Empty>
                  ) : (
                    <Select
                      items={localUsers.map((user) => ({
                        value: user.username,
                        label: user.username,
                      }))}
                      value={selectedTarget}
                      onValueChange={(value) => setTarget(value ?? "")}
                      disabled={pending || !administrative}
                    >
                      <SelectTrigger
                        id="password-target"
                        aria-label="Local user"
                      >
                        <SelectValue placeholder="Choose a local user" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          {localUsers.map((user) => (
                            <SelectItem
                              key={user.username}
                              value={user.username}
                            >
                              {user.username}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  )}
                </Field>
              )}
              {mode === "change" && (
                <Field>
                  <FieldLabel htmlFor="current-password">
                    Current password
                  </FieldLabel>
                  <Input
                    id="current-password"
                    type="password"
                    autoComplete="current-password"
                    value={currentPassword}
                    disabled={pending}
                    onChange={(event) => setCurrentPassword(event.target.value)}
                  />
                </Field>
              )}
              <Field>
                <FieldLabel htmlFor="new-password">New password</FieldLabel>
                <Input
                  id="new-password"
                  type="password"
                  autoComplete="new-password"
                  value={newPassword}
                  disabled={pending}
                  onChange={(event) => setNewPassword(event.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="password-confirmation">
                  Confirm new password
                </FieldLabel>
                <Input
                  id="password-confirmation"
                  type="password"
                  autoComplete="new-password"
                  value={confirmation}
                  disabled={pending}
                  onChange={(event) => setConfirmation(event.target.value)}
                />
              </Field>
            </FieldGroup>
            <Button
              type="submit"
              disabled={pending || (mode === "reset" && !administrative)}
            >
              {pending
                ? "Updating…"
                : mode === "change"
                  ? "Change password"
                  : "Reset password"}
            </Button>
          </form>
        )}
      </CardContent>
    </Card>
  )
}
