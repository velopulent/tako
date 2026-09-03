import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import * as React from "react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
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
import { Textarea } from "@/components/ui/textarea"
import {
  APIError,
  api,
  type SessionResponse,
  type SSHKeyOperation,
  type SSHKeyPreview,
  type SSHKeyState,
  type UserInfo,
} from "@/lib/api"

function keyError(error: unknown) {
  if (error instanceof APIError) {
    switch (error.code) {
      case "ssh-key-conflict":
        return "Authorized keys changed elsewhere. Refresh and preview again."
      case "ssh-key-protected":
        return "The authorized_keys path is protected and was not changed."
      case "ssh-key-read-only":
        return "Remote NSS identities are read-only in Tako."
      case "ssh-key-unauthorized":
        return "You are not authorized to manage this user's SSH keys."
    }
  }
  return error instanceof Error
    ? error.message
    : "The SSH key operation failed."
}

export function SSHKeyManager() {
  const client = useQueryClient()
  const [target, setTarget] = React.useState("")
  const [key, setKey] = React.useState("")
  const [action, setAction] = React.useState<"add" | "remove">("add")
  const [fingerprint, setFingerprint] = React.useState("")
  const [confirmation, setConfirmation] = React.useState("")
  const [expectedFingerprint, setExpectedFingerprint] = React.useState("")
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
  const selectedTarget = target || session.data?.user.username || ""
  const state = useQuery({
    queryKey: ["ssh-keys", selectedTarget],
    enabled: selectedTarget !== "",
    queryFn: () =>
      api<SSHKeyState>(
        `/accounts/users/ssh-keys?username=${encodeURIComponent(selectedTarget)}`
      ),
  })
  const canManage =
    selectedTarget === session.data?.user.username || administrative
  const operation: SSHKeyOperation =
    action === "add"
      ? {
          action,
          username: selectedTarget,
          key: key || undefined,
          expectedFingerprint: expectedFingerprint || undefined,
        }
      : {
          action,
          username: selectedTarget,
          fingerprint: fingerprint || undefined,
          expectedFingerprint: expectedFingerprint || undefined,
          confirmation: confirmation || undefined,
        }
  const preview = useMutation({
    mutationFn: (value: SSHKeyOperation) =>
      api<SSHKeyPreview>("/accounts/users/ssh-keys/preview", {
        method: "POST",
        body: JSON.stringify(value),
      }),
    onSuccess: (value) => {
      setExpectedFingerprint(value.current.fingerprint)
      setConfirmation("")
    },
  })
  const apply = useMutation({
    mutationFn: (value: SSHKeyOperation) =>
      api<SSHKeyState>("/accounts/users/ssh-keys", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(value),
      }),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: ["ssh-keys", selectedTarget] })
      setKey("")
      setFingerprint("")
      setExpectedFingerprint("")
      setConfirmation("")
      preview.reset()
      apply.reset()
    },
  })
  const pending = preview.isPending || apply.isPending
  const confirmationText =
    action === "remove" ? `REMOVE KEY ${fingerprint}` : ""

  return (
    <Card>
      <CardHeader>
        <CardTitle>Authorized SSH keys</CardTitle>
        <CardDescription>
          Keys are validated and atomically saved as{" "}
          <code>authorized_keys</code> with restrictive permissions. Public key
          material is not copied to operation history.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        {(session.isPending || users.isPending) && (
          <Skeleton className="h-48" />
        )}
        {(session.isError || users.isError) && (
          <Alert variant="destructive">
            <AlertTitle>SSH key inventory unavailable</AlertTitle>
            <AlertDescription>
              {(session.error ?? users.error)?.message}
            </AlertDescription>
          </Alert>
        )}
        {!session.isPending &&
          !users.isPending &&
          !session.isError &&
          !users.isError && (
            <>
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="ssh-key-user">Account</FieldLabel>
                  {administrative ? (
                    <Select
                      items={localUsers.map((user) => ({
                        value: user.username,
                        label: user.username,
                      }))}
                      value={selectedTarget}
                      onValueChange={(value) => {
                        setTarget(value ?? "")
                        setExpectedFingerprint("")
                        preview.reset()
                      }}
                      disabled={pending}
                    >
                      <SelectTrigger id="ssh-key-user" aria-label="Account">
                        <SelectValue placeholder="Choose a local account" />
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
                  ) : (
                    <Input id="ssh-key-user" value={selectedTarget} readOnly />
                  )}
                </Field>
              </FieldGroup>
              {state.isPending && <Skeleton className="h-28" />}
              {state.isError && (
                <Alert variant="destructive">
                  <AlertTitle>Authorized keys unavailable</AlertTitle>
                  <AlertDescription>{state.error.message}</AlertDescription>
                </Alert>
              )}
              {state.data && (
                <div className="space-y-2" aria-live="polite">
                  <p className="text-sm text-muted-foreground">
                    {(state.data.keys ?? []).length} authorized key
                    {(state.data.keys ?? []).length === 1 ? "" : "s"} ·{" "}
                    {state.data.authority} authority
                  </p>
                  <p className="text-xs break-all text-muted-foreground">
                    Path: <code>{state.data.path}</code>
                  </p>
                  {!(state.data.keys ?? []).length ? (
                    <Empty>
                      <EmptyHeader>
                        <EmptyTitle>No authorized keys</EmptyTitle>
                        <EmptyDescription>
                          Add a public key to enable SSH access for this
                          account.
                        </EmptyDescription>
                      </EmptyHeader>
                    </Empty>
                  ) : (
                    <div className="divide-y rounded-md border">
                      {(state.data.keys ?? []).map((item) => (
                        <div
                          key={item.fingerprint}
                          className="flex flex-col gap-2 p-3 sm:flex-row sm:items-center sm:justify-between"
                        >
                          <div className="min-w-0">
                            <div className="flex flex-wrap items-center gap-2">
                              <Badge variant="secondary">{item.type}</Badge>
                              {item.comment && (
                                <span className="text-sm">{item.comment}</span>
                              )}
                            </div>
                            <code className="text-xs break-all text-muted-foreground">
                              SHA256:{item.fingerprint}
                            </code>
                          </div>
                          <Button
                            type="button"
                            variant="outline"
                            disabled={pending || !canManage}
                            onClick={() => {
                              setAction("remove")
                              setFingerprint(item.fingerprint)
                              setKey("")
                              setExpectedFingerprint("")
                              setConfirmation("")
                              preview.reset()
                            }}
                          >
                            Remove
                          </Button>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
              {canManage && (
                <FieldGroup>
                  <Field>
                    <FieldLabel>Key action</FieldLabel>
                    <div className="flex flex-wrap gap-2">
                      <Button
                        type="button"
                        variant={action === "add" ? "secondary" : "outline"}
                        disabled={pending}
                        onClick={() => {
                          setAction("add")
                          setFingerprint("")
                          setConfirmation("")
                          preview.reset()
                        }}
                      >
                        Add key
                      </Button>
                      <Button
                        type="button"
                        variant={action === "remove" ? "secondary" : "outline"}
                        disabled={pending || !(state.data?.keys ?? []).length}
                        onClick={() => {
                          setAction("remove")
                          setKey("")
                          preview.reset()
                        }}
                      >
                        Remove selected key
                      </Button>
                    </div>
                  </Field>
                  {action === "add" ? (
                    <Field>
                      <FieldLabel htmlFor="ssh-key-value">
                        Public key
                      </FieldLabel>
                      <Textarea
                        id="ssh-key-value"
                        value={key}
                        disabled={pending}
                        onChange={(event) => setKey(event.target.value)}
                        placeholder="ssh-ed25519 AAAA… comment"
                      />
                    </Field>
                  ) : (
                    <Field>
                      <FieldLabel htmlFor="ssh-key-fingerprint">
                        Key fingerprint
                      </FieldLabel>
                      <Input
                        id="ssh-key-fingerprint"
                        value={fingerprint}
                        readOnly
                      />
                    </Field>
                  )}
                  {action === "remove" && (
                    <Field>
                      <FieldLabel htmlFor="ssh-key-confirmation">
                        Type {confirmationText} to confirm
                      </FieldLabel>
                      <Input
                        id="ssh-key-confirmation"
                        value={confirmation}
                        disabled={pending}
                        onChange={(event) =>
                          setConfirmation(event.target.value)
                        }
                      />
                    </Field>
                  )}
                </FieldGroup>
              )}
              {(preview.isError || apply.isError) && (
                <Alert variant="destructive">
                  <AlertTitle>SSH key operation failed</AlertTitle>
                  <AlertDescription>
                    {keyError(preview.error ?? apply.error)}
                  </AlertDescription>
                </Alert>
              )}
              {preview.data && (
                <Alert
                  variant={
                    preview.data.stale || !preview.data.allowed
                      ? "destructive"
                      : "default"
                  }
                >
                  <AlertTitle>
                    {preview.data.stale ? "Refresh required" : "Key preview"}
                  </AlertTitle>
                  <AlertDescription>
                    {preview.data.reason ?? preview.data.changes.join(", ")}
                  </AlertDescription>
                </Alert>
              )}
              {canManage && (
                <div className="flex flex-wrap gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    disabled={
                      pending || (action === "add" ? !key : !fingerprint)
                    }
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
                      !expectedFingerprint ||
                      (action === "add"
                        ? !key
                        : !fingerprint || confirmation !== confirmationText)
                    }
                    onClick={() => apply.mutate(operation)}
                  >
                    {apply.isPending ? "Applying…" : "Apply key change"}
                  </Button>
                </div>
              )}
            </>
          )}
      </CardContent>
    </Card>
  )
}
