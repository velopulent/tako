import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  KeyRoundIcon,
  LockIcon,
  PlusIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react"
import * as React from "react"

import { useAdminAccess } from "@/components/admin-access"
import { StatusBadge } from "@/components/status-badge"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "@/components/ui/combobox"
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
import { Separator } from "@/components/ui/separator"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { toast } from "@/components/ui/toast"
import {
  api,
  type GroupInfo,
  type LocalAccountPreview,
  type LoginHistoryPage,
  type SSHKeyState,
  type UserInfo,
} from "@/lib/api"
import { formatDateTime, formatRelative } from "@/lib/format"
import { guardedChange } from "@/lib/guarded"
import { useSession } from "@/lib/session"

export const adminGroups = ["wheel", "sudo", "admin"]

export function initials(user: Pick<UserInfo, "name" | "username">) {
  const words = (user.name || user.username).split(/[\s._-]+/).filter(Boolean)
  return ((words[0]?.[0] ?? "") + (words[1]?.[0] ?? "")).toUpperCase() || "?"
}

function Section({
  title,
  description,
  children,
  action,
}: {
  title: string
  description?: string
  children: React.ReactNode
  action?: React.ReactNode
}) {
  return (
    <section className="flex flex-col gap-3 px-4 py-5">
      <div className="flex items-start justify-between gap-3">
        <div className="flex flex-col gap-0.5">
          <h3 className="text-sm font-medium">{title}</h3>
          {description && (
            <p className="text-xs text-muted-foreground">{description}</p>
          )}
        </div>
        {action}
      </div>
      {children}
    </section>
  )
}

function useAccountChange<T extends Record<string, unknown>>(
  path: string,
  success: string,
  apply?: (operation: T) => Record<string, unknown>
) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (operation: T) =>
      guardedChange(
        path,
        operation,
        apply
          ? (preview) => ({
              expectedFingerprint: preview.current.fingerprint,
              ...apply(operation),
            })
          : undefined
      ),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["accounts"] })
      toast.add({ title: success, type: "success" })
    },
    onError: (error) =>
      toast.add({
        title: "Nothing changed",
        description: error.message,
        type: "error",
      }),
  })
}

export function UserSheet({
  user,
  groups,
  onClose,
}: {
  user: UserInfo
  groups: GroupInfo[]
  onClose: () => void
}) {
  const { administrative } = useAdminAccess()
  const session = useSession()
  const self = session.user.username === user.username
  const editable = administrative && user.local
  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent className="w-full gap-0 overflow-y-auto sm:max-w-lg">
        <SheetHeader className="flex-row items-center gap-3 border-b">
          <Avatar className="size-10">
            <AvatarFallback>{initials(user)}</AvatarFallback>
          </Avatar>
          <div className="flex min-w-0 flex-col">
            <SheetTitle className="truncate">
              {user.name || user.username}
            </SheetTitle>
            <SheetDescription className="truncate font-mono text-xs">
              {user.username} · uid {user.uid}
            </SheetDescription>
          </div>
        </SheetHeader>
        {!user.local && (
          <Alert className="m-4 mb-0 w-auto">
            <AlertTitle>Managed elsewhere</AlertTitle>
            <AlertDescription>
              This account comes from a directory service, so it cannot be
              changed here.
            </AlertDescription>
          </Alert>
        )}
        <Profile user={user} editable={editable} />
        <Separator />
        <Access user={user} groups={groups} editable={editable} self={self} />
        <Separator />
        <Password user={user} self={self} administrative={administrative} />
        <Separator />
        <SSHKeys user={user} self={self} administrative={administrative} />
        <Separator />
        <History user={user} self={self} administrative={administrative} />
        {editable && !self && (
          <>
            <Separator />
            <Danger user={user} onDeleted={onClose} />
          </>
        )}
      </SheetContent>
    </Sheet>
  )
}

function Profile({ user, editable }: { user: UserInfo; editable: boolean }) {
  const [name, setName] = React.useState(user.name)
  const [home, setHome] = React.useState(user.home)
  const [shell, setShell] = React.useState(user.shell)
  const update = useAccountChange("/accounts/users/account", "Account updated")
  const dirty = name !== user.name || home !== user.home || shell !== user.shell
  return (
    <Section title="Profile">
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault()
          update.mutate({
            action: "update",
            username: user.username,
            name: name !== user.name ? name : undefined,
            home: home !== user.home ? home : undefined,
            shell: shell !== user.shell ? shell : undefined,
          })
        }}
      >
        <FieldGroup className="gap-3">
          <Field>
            <FieldLabel htmlFor="user-name">Full name</FieldLabel>
            <Input
              id="user-name"
              value={name}
              disabled={!editable}
              maxLength={256}
              onChange={(event) => setName(event.target.value)}
            />
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="user-home">Home directory</FieldLabel>
              <Input
                id="user-home"
                className="font-mono"
                value={home}
                disabled={!editable}
                onChange={(event) => setHome(event.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="user-shell">Login shell</FieldLabel>
              <Input
                id="user-shell"
                className="font-mono"
                value={shell}
                disabled={!editable}
                onChange={(event) => setShell(event.target.value)}
              />
            </Field>
          </div>
        </FieldGroup>
        {editable && dirty && (
          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => {
                setName(user.name)
                setHome(user.home)
                setShell(user.shell)
              }}
            >
              Reset
            </Button>
            <Button type="submit" size="sm" disabled={update.isPending}>
              {update.isPending && <Spinner data-icon="inline-start" />}
              Save
            </Button>
          </div>
        )}
      </form>
    </Section>
  )
}

function Access({
  user,
  groups,
  editable,
  self,
}: {
  user: UserInfo
  groups: GroupInfo[]
  editable: boolean
  self: boolean
}) {
  const administrator = user.groups.some((group) => adminGroups.includes(group))
  const membership = useAccountChange<{
    action: "add" | "remove"
    username: string
    group: string
  }>("/accounts/groups/membership", "Group membership updated")
  const role = useAccountChange<{
    action: "grant" | "revoke"
    username: string
    role: "administrator"
  }>(
    "/accounts/groups/admin-role",
    "Administrator role updated",
    (operation) => ({
      confirmation: `${operation.action.toUpperCase()} ADMIN ${operation.username}`,
    })
  )
  const lock = useAccountChange<{
    action: "lock" | "unlock"
    username: string
  }>("/accounts/users/account", "Sign-in updated", (operation) => ({
    confirmation:
      operation.action === "lock" ? `LOCK ${operation.username}` : undefined,
  }))
  const state = useQuery({
    queryKey: ["accounts", "state", user.username],
    queryFn: () =>
      api<LocalAccountPreview>("/accounts/users/account/preview", {
        method: "POST",
        body: JSON.stringify({ action: "unlock", username: user.username }),
      }),
    enabled: editable,
  })
  const available = groups
    .filter(
      (group) =>
        group.local &&
        !user.groups.includes(group.name) &&
        !adminGroups.includes(group.name)
    )
    .map((group) => group.name)
  const [adding, setAdding] = React.useState<string | null>(null)
  return (
    <Section title="Access">
      <div className="flex flex-col divide-y rounded-lg border">
        <div className="flex items-center justify-between gap-4 px-3 py-2.5">
          <span className="flex flex-col">
            <span className="text-sm">Administrator</span>
            <span className="text-xs text-muted-foreground">
              Can unlock administrative access and use sudo.
            </span>
          </span>
          <Switch
            aria-label="Administrator"
            checked={administrator}
            disabled={!editable || self || role.isPending}
            onCheckedChange={(on) =>
              role.mutate({
                action: on ? "grant" : "revoke",
                username: user.username,
                role: "administrator",
              })
            }
          />
        </div>
        <div className="flex items-center justify-between gap-4 px-3 py-2.5">
          <span className="flex flex-col">
            <span className="text-sm">Allow sign-in</span>
            <span className="text-xs text-muted-foreground">
              {state.data?.current.locked
                ? "The password is locked."
                : "The user can sign in with a password."}
            </span>
          </span>
          {editable && state.isPending ? (
            <Skeleton className="h-5 w-9 rounded-full" />
          ) : (
            <Switch
              aria-label="Allow sign-in"
              checked={!state.data?.current.locked}
              disabled={!editable || self || lock.isPending}
              onCheckedChange={(on) =>
                lock.mutate(
                  { action: on ? "unlock" : "lock", username: user.username },
                  { onSuccess: () => void state.refetch() }
                )
              }
            />
          )}
        </div>
      </div>
      <div className="flex flex-col gap-2">
        <span className="text-xs font-medium text-muted-foreground">
          Groups
        </span>
        <div className="flex flex-wrap items-center gap-1.5">
          {user.groups.map((group) => (
            <Badge
              key={group}
              variant="outline"
              className="gap-1 pr-0.5 font-normal"
            >
              {group}
              {editable && !adminGroups.includes(group) && (
                <Button
                  variant="ghost"
                  size="icon-xs"
                  className="size-4 rounded-full"
                  aria-label={`Remove from ${group}`}
                  disabled={membership.isPending}
                  onClick={() =>
                    membership.mutate({
                      action: "remove",
                      username: user.username,
                      group,
                    })
                  }
                >
                  <XIcon />
                </Button>
              )}
            </Badge>
          ))}
          {!user.groups.length && (
            <span className="text-sm text-muted-foreground">
              No supplementary groups
            </span>
          )}
          {editable && available.length > 0 && (
            <Combobox
              items={available}
              value={adding}
              onValueChange={(group) => {
                if (!group) return
                setAdding(null)
                membership.mutate({
                  action: "add",
                  username: user.username,
                  group,
                })
              }}
            >
              <ComboboxInput
                placeholder="Add to group"
                className="h-6 w-36 text-xs"
                showTrigger={false}
              />
              <ComboboxContent>
                <ComboboxEmpty>No group found.</ComboboxEmpty>
                <ComboboxList>
                  {(item: string) => (
                    <ComboboxItem key={item} value={item}>
                      {item}
                    </ComboboxItem>
                  )}
                </ComboboxList>
              </ComboboxContent>
            </Combobox>
          )}
        </div>
      </div>
    </Section>
  )
}

function Password({
  user,
  self,
  administrative,
}: {
  user: UserInfo
  self: boolean
  administrative: boolean
}) {
  const [open, setOpen] = React.useState(false)
  const allowed = self || (administrative && user.local)
  return (
    <Section
      title="Password"
      description={
        self
          ? "Change the password you use to sign in."
          : "Set a new password for this user."
      }
      action={
        <Button
          variant="outline"
          size="sm"
          disabled={!allowed}
          onClick={() => setOpen(true)}
        >
          <KeyRoundIcon data-icon="inline-start" />
          {self ? "Change" : "Reset"}
        </Button>
      }
    >
      {open && (
        <PasswordDialog
          user={user}
          self={self}
          onClose={() => setOpen(false)}
        />
      )}
    </Section>
  )
}

function PasswordDialog({
  user,
  self,
  onClose,
}: {
  user: UserInfo
  self: boolean
  onClose: () => void
}) {
  const [current, setCurrent] = React.useState("")
  const [next, setNext] = React.useState("")
  const [repeat, setRepeat] = React.useState("")
  const change = useMutation({
    mutationFn: () =>
      api("/accounts/users/password", {
        method: "POST",
        body: JSON.stringify(
          self
            ? {
                action: "change",
                currentPassword: current,
                newPassword: next,
                confirmation: repeat,
              }
            : {
                action: "reset",
                username: user.username,
                newPassword: next,
                confirmation: repeat,
              }
        ),
      }),
    onSuccess: () => {
      toast.add({ title: "Password changed", type: "success" })
      onClose()
    },
  })
  const mismatch = repeat !== "" && repeat !== next
  return (
    <Dialog open onOpenChange={(value) => !value && onClose()}>
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
              {self
                ? "Change your password"
                : `Reset password for ${user.username}`}
            </DialogTitle>
            <DialogDescription>
              The host password policy decides what is accepted.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup className="gap-3">
            {self && (
              <Field>
                <FieldLabel htmlFor="password-current">
                  Current password
                </FieldLabel>
                <Input
                  id="password-current"
                  type="password"
                  autoComplete="current-password"
                  required
                  value={current}
                  onChange={(event) => setCurrent(event.target.value)}
                />
              </Field>
            )}
            <Field>
              <FieldLabel htmlFor="password-new">New password</FieldLabel>
              <Input
                id="password-new"
                type="password"
                autoComplete="new-password"
                required
                value={next}
                onChange={(event) => setNext(event.target.value)}
              />
            </Field>
            <Field data-invalid={mismatch}>
              <FieldLabel htmlFor="password-repeat">
                Repeat new password
              </FieldLabel>
              <Input
                id="password-repeat"
                type="password"
                autoComplete="new-password"
                required
                aria-invalid={mismatch}
                value={repeat}
                onChange={(event) => setRepeat(event.target.value)}
              />
              {mismatch && (
                <FieldDescription>The passwords do not match.</FieldDescription>
              )}
            </Field>
          </FieldGroup>
          {change.isError && (
            <Alert variant="destructive">
              <AlertTitle>Password unchanged</AlertTitle>
              <AlertDescription>{change.error.message}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={
                !next ||
                next !== repeat ||
                (self && !current) ||
                change.isPending
              }
            >
              {change.isPending && <Spinner data-icon="inline-start" />}
              {self ? "Change password" : "Set password"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function SSHKeys({
  user,
  self,
  administrative,
}: {
  user: UserInfo
  self: boolean
  administrative: boolean
}) {
  const client = useQueryClient()
  const readable = self || administrative
  const query = useQuery({
    queryKey: ["accounts", "ssh-keys", user.username],
    queryFn: () =>
      api<SSHKeyState>(
        `/accounts/users/ssh-keys?username=${encodeURIComponent(user.username)}`
      ),
    enabled: readable,
  })
  const [adding, setAdding] = React.useState(false)
  const [key, setKey] = React.useState("")
  const change = useMutation({
    mutationFn: (operation: {
      action: "add" | "remove"
      username: string
      key?: string
      fingerprint?: string
    }) =>
      guardedChange("/accounts/users/ssh-keys", operation, (preview) => ({
        expectedFingerprint: preview.current.fingerprint,
        confirmation:
          operation.action === "remove"
            ? `REMOVE KEY ${operation.fingerprint}`
            : undefined,
      })),
    onSuccess: (_, operation) => {
      void client.invalidateQueries({
        queryKey: ["accounts", "ssh-keys", user.username],
      })
      toast.add({
        title: operation.action === "add" ? "Key added" : "Key removed",
        type: "success",
      })
      setAdding(false)
      setKey("")
    },
    onError: (error) =>
      toast.add({
        title: "Keys unchanged",
        description: error.message,
        type: "error",
      }),
  })
  const writable = Boolean(query.data?.writable)
  return (
    <Section
      title="SSH keys"
      description="Public keys allowed to sign in over SSH."
      action={
        writable && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => setAdding((value) => !value)}
          >
            <PlusIcon data-icon="inline-start" />
            Add
          </Button>
        )
      }
    >
      {!readable ? (
        <p className="text-sm text-muted-foreground">
          Unlock administrative access to see this user’s keys.
        </p>
      ) : query.isPending ? (
        <Skeleton className="h-10" />
      ) : query.isError ? (
        <p className="text-sm text-muted-foreground">{query.error.message}</p>
      ) : (
        <>
          {adding && (
            <form
              className="flex flex-col gap-2"
              onSubmit={(event) => {
                event.preventDefault()
                change.mutate({
                  action: "add",
                  username: user.username,
                  key: key.trim(),
                })
              }}
            >
              <Textarea
                aria-label="Public key"
                required
                rows={3}
                className="font-mono text-xs"
                placeholder="ssh-ed25519 AAAA… user@laptop"
                value={key}
                onChange={(event) => setKey(event.target.value)}
              />
              <div className="flex justify-end gap-2">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setAdding(false)}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  size="sm"
                  disabled={!key.trim() || change.isPending}
                >
                  {change.isPending && <Spinner data-icon="inline-start" />}
                  Add key
                </Button>
              </div>
            </form>
          )}
          {query.data.keys.length ? (
            <ul className="flex flex-col divide-y rounded-lg border">
              {query.data.keys.map((item) => (
                <li
                  key={item.fingerprint}
                  className="flex items-center gap-3 px-3 py-2"
                >
                  <KeyRoundIcon className="size-4 shrink-0 text-muted-foreground" />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate text-sm">
                      {item.comment || item.type}
                    </span>
                    <span className="truncate font-mono text-xs text-muted-foreground">
                      {item.fingerprint}
                    </span>
                  </span>
                  {writable && (
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`Remove ${item.comment || item.fingerprint}`}
                      disabled={change.isPending}
                      onClick={() =>
                        change.mutate({
                          action: "remove",
                          username: user.username,
                          fingerprint: item.fingerprint,
                        })
                      }
                    >
                      <Trash2Icon />
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          ) : (
            !adding && <p className="text-sm text-muted-foreground">No keys.</p>
          )}
        </>
      )}
    </Section>
  )
}

function History({
  user,
  self,
  administrative,
}: {
  user: UserInfo
  self: boolean
  administrative: boolean
}) {
  const readable = self || administrative
  const query = useQuery({
    queryKey: ["accounts", "history", user.username],
    queryFn: () =>
      api<LoginHistoryPage>(
        `/accounts/users/${encodeURIComponent(user.username)}/login-history?limit=10`
      ),
    enabled: readable,
  })
  return (
    <Section title="Recent sign-ins">
      {!readable ? (
        <p className="text-sm text-muted-foreground">
          Unlock administrative access to see this user’s sign-ins.
        </p>
      ) : query.isPending ? (
        <Skeleton className="h-16" />
      ) : query.isError ? (
        <p className="text-sm text-muted-foreground">{query.error.message}</p>
      ) : query.data.items.length ? (
        <ul className="flex flex-col gap-2">
          {query.data.items
            .filter((item) => item.event !== "session-close")
            .map((item) => (
              <li
                key={`${item.timestamp}-${item.event}-${item.service}-${item.session ?? ""}`}
                className="flex items-center gap-3 text-sm"
              >
                <StatusBadge
                  tone={item.outcome === "success" ? "success" : "danger"}
                >
                  {item.outcome === "success" ? "Signed in" : "Failed"}
                </StatusBadge>
                <span className="min-w-0 flex-1 truncate text-muted-foreground">
                  {item.service}
                  {item.remote ? ` from ${item.remote}` : ""}
                </span>
                <time
                  className="shrink-0 text-xs text-muted-foreground"
                  title={formatDateTime(item.timestamp)}
                  dateTime={item.timestamp}
                >
                  {formatRelative(item.timestamp)}
                </time>
              </li>
            ))}
        </ul>
      ) : (
        <p className="text-sm text-muted-foreground">No sign-ins recorded.</p>
      )}
    </Section>
  )
}

function Danger({
  user,
  onDeleted,
}: {
  user: UserInfo
  onDeleted: () => void
}) {
  const [open, setOpen] = React.useState(false)
  const [typed, setTyped] = React.useState("")
  const remove = useAccountChange<{ action: "delete"; username: string }>(
    "/accounts/users/account",
    `Deleted ${user.username}`,
    () => ({ confirmation: `DELETE ${user.username}` })
  )
  return (
    <Section
      title="Delete account"
      description="Removes the user from this host. Files in the home directory are kept."
      action={
        <Button variant="destructive" size="sm" onClick={() => setOpen(true)}>
          <Trash2Icon data-icon="inline-start" />
          Delete
        </Button>
      }
    >
      {open && (
        <Dialog open onOpenChange={(value) => !value && setOpen(false)}>
          <DialogContent>
            <form
              className="flex flex-col gap-5"
              onSubmit={(event) => {
                event.preventDefault()
                remove.mutate(
                  { action: "delete", username: user.username },
                  { onSuccess: onDeleted }
                )
              }}
            >
              <DialogHeader>
                <DialogTitle>Delete {user.username}?</DialogTitle>
                <DialogDescription>
                  The user can no longer sign in, and running sessions may stop.
                </DialogDescription>
              </DialogHeader>
              <Field>
                <FieldLabel htmlFor="delete-confirm">
                  Type{" "}
                  <span className="font-mono font-semibold">
                    {user.username}
                  </span>{" "}
                  to confirm
                </FieldLabel>
                <Input
                  id="delete-confirm"
                  autoComplete="off"
                  value={typed}
                  onChange={(event) => setTyped(event.target.value)}
                />
              </Field>
              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setOpen(false)}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  variant="destructive"
                  disabled={typed !== user.username || remove.isPending}
                >
                  {remove.isPending ? (
                    <Spinner data-icon="inline-start" />
                  ) : (
                    <LockIcon data-icon="inline-start" />
                  )}
                  Delete account
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      )}
    </Section>
  )
}
