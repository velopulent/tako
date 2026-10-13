import { useQueryClient } from "@tanstack/react-query"
import { KeyRoundIcon, LockKeyholeIcon, ShieldAlertIcon } from "lucide-react"
import * as React from "react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
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
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { APIError, api } from "@/lib/api"
import { isAdministrative, sessionKey, useSession } from "@/lib/session"

type AdminAccess = {
  administrative: boolean
  until?: Date
  request: () => void
  drop: () => Promise<void>
}

export const AdminAccessContext = React.createContext<AdminAccess | null>(null)

export function useAdminAccess() {
  const value = React.useContext(AdminAccessContext)
  if (!value) throw new Error("useAdminAccess needs AdminAccessProvider")
  return value
}

function elevationMessage(error: unknown) {
  if (error instanceof APIError) {
    if (error.code === "administrative-access-unavailable")
      return "This host has no sudo or Polkit rule that can grant administrative access to your account."
    if (error.status === 401) return "Your session has expired. Sign in again."
  }
  return "The password was not accepted, or your account is not allowed to administer this host."
}

export function AdminAccessProvider({
  children,
}: {
  children: React.ReactNode
}) {
  const session = useSession()
  const client = useQueryClient()
  const [open, setOpen] = React.useState(false)
  const [password, setPassword] = React.useState("")
  const [challenge, setChallenge] = React.useState("")
  const [error, setError] = React.useState("")
  const [pending, setPending] = React.useState(false)
  const administrative = isAdministrative(session)

  const reset = () => {
    setPassword("")
    setChallenge("")
    setError("")
  }

  async function elevate(event: React.FormEvent) {
    event.preventDefault()
    setPending(true)
    setError("")
    try {
      await api("/admin/elevate", {
        method: "POST",
        body: JSON.stringify({
          password,
          ...(challenge ? { responses: [challenge] } : {}),
        }),
      })
      reset()
      setOpen(false)
      await client.invalidateQueries()
      toast.add({ title: "Administrative access granted", type: "success" })
    } catch (caught) {
      setPassword("")
      setChallenge("")
      setError(elevationMessage(caught))
    } finally {
      setPending(false)
    }
  }

  const value = React.useMemo<AdminAccess>(
    () => ({
      administrative,
      until: session?.adminUntil ? new Date(session.adminUntil) : undefined,
      request: () => setOpen(true),
      drop: async () => {
        await api<void>("/admin/drop", { method: "POST" })
        await client.invalidateQueries()
        toast.add({ title: "Administrative access dropped" })
      },
    }),
    [administrative, session?.adminUntil, client]
  )

  // Watch for the grant expiring while the page is open.
  React.useEffect(() => {
    if (!value.until) return
    const remaining = value.until.getTime() - Date.now()
    if (remaining <= 0) return
    const timer = window.setTimeout(
      () => void client.invalidateQueries({ queryKey: sessionKey }),
      remaining + 500
    )
    return () => window.clearTimeout(timer)
  }, [value.until, client])

  const idleMinutes = Math.round((session?.adminIdleTimeoutSeconds ?? 300) / 60)

  return (
    <AdminAccessContext.Provider value={value}>
      {children}
      <Dialog
        open={open}
        onOpenChange={(next) => {
          setOpen(next)
          if (!next) reset()
        }}
      >
        <DialogContent className="sm:max-w-md">
          <form onSubmit={elevate} className="flex flex-col gap-6">
            <DialogHeader>
              <DialogTitle>Unlock administrative access</DialogTitle>
              <DialogDescription>
                Confirm your password as {session?.user.username}. Access ends
                after {idleMinutes} minutes.
              </DialogDescription>
            </DialogHeader>
            <FieldGroup>
              <Field data-invalid={Boolean(error) || undefined}>
                <FieldLabel htmlFor="admin-password">Password</FieldLabel>
                <Input
                  id="admin-password"
                  type="password"
                  autoComplete="current-password"
                  autoFocus
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  aria-invalid={Boolean(error) || undefined}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="admin-challenge">
                  Verification code
                </FieldLabel>
                <Input
                  id="admin-challenge"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  value={challenge}
                  onChange={(event) => setChallenge(event.target.value)}
                />
                <FieldDescription>
                  Only if this host asks for a second factor.
                </FieldDescription>
              </Field>
            </FieldGroup>
            {error && (
              <Alert variant="destructive" role="alert">
                <ShieldAlertIcon />
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setOpen(false)}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={!password || pending}>
                {pending ? (
                  <Spinner data-icon="inline-start" />
                ) : (
                  <KeyRoundIcon data-icon="inline-start" />
                )}
                Unlock
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </AdminAccessContext.Provider>
  )
}

// AdminRequired explains that a page needs administrative access and offers
// to unlock it. It renders nothing once access is granted.
export function AdminRequired({
  children = "Viewing and changing this requires administrative access.",
}: {
  children?: React.ReactNode
}) {
  const { administrative, request } = useAdminAccess()
  if (administrative) return null
  return (
    <Alert>
      <LockKeyholeIcon />
      <AlertTitle>Administrative access required</AlertTitle>
      <AlertDescription>{children}</AlertDescription>
      <AlertAction>
        <Button size="sm" onClick={request}>
          <KeyRoundIcon data-icon="inline-start" />
          Unlock
        </Button>
      </AlertAction>
    </Alert>
  )
}
