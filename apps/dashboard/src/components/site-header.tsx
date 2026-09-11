import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useLocation } from "@tanstack/react-router"
import {
  KeyRoundIcon,
  LockKeyholeIcon,
  LogOutIcon,
  MonitorIcon,
  MoonIcon,
  SunIcon,
} from "lucide-react"
import * as React from "react"
import { type Theme, useTheme } from "@/components/theme-provider"
import { Alert, AlertDescription } from "@/components/ui/alert"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group"
import { Separator } from "@/components/ui/separator"
import { SidebarTrigger } from "@/components/ui/sidebar"
import type { User } from "@/lib/api"
import { api } from "@/lib/api"
import { pageTitle } from "@/lib/page-title"

export function SiteHeader({
  user,
  csrfToken,
}: {
  user: User
  csrfToken: string
}) {
  const location = useLocation(),
    client = useQueryClient(),
    { theme, setTheme } = useTheme(),
    [password, setPassword] = React.useState(""),
    [mfaResponse, setMfaResponse] = React.useState(""),
    [elevationError, setElevationError] = React.useState(""),
    [elevating, setElevating] = React.useState(false)
  const admin = useQuery({
    queryKey: ["admin"],
    queryFn: () =>
      api<{
        administrative: boolean
        until: string
        idleTimeoutSeconds: number
      }>("/admin"),
    refetchInterval: 10_000,
  })
  const currentPage = pageTitle(location.pathname)
  const remaining = admin.data?.until
    ? Math.max(
        0,
        Math.ceil(
          (new Date(admin.data.until).getTime() - admin.dataUpdatedAt) / 60_000
        )
      )
    : 0
  async function logout() {
    await api<void>("/auth/logout", {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken },
    })
    window.location.reload()
  }
  async function elevate() {
    setElevationError("")
    setElevating(true)
    try {
      await api("/admin/elevate", {
        method: "POST",
        headers: { "X-CSRF-Token": csrfToken },
        body: JSON.stringify({
          password,
          ...(mfaResponse ? { responses: [mfaResponse] } : {}),
        }),
      })
      setPassword("")
      setMfaResponse("")
      await admin.refetch()
      await client.invalidateQueries({ queryKey: ["session"] })
    } catch {
      setElevationError(
        "The host policy rejected this attempt. If your PAM stack requires MFA, enter the current challenge response and retry."
      )
      setPassword("")
      setMfaResponse("")
    } finally {
      setElevating(false)
    }
  }
  async function drop() {
    await api<void>("/admin/drop", {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken },
    })
    await admin.refetch()
  }
  return (
    <header className="flex h-(--header-height) shrink-0 items-center border-b">
      <div className="flex w-full items-center gap-2 px-4 lg:px-6">
        <SidebarTrigger className="-ml-1" />
        <Separator
          orientation="vertical"
          className="mx-1 h-4 data-vertical:self-auto"
        />
        <p className="min-w-0 flex-1 truncate text-sm font-medium">
          {currentPage}
        </p>
        {admin.data?.administrative ? (
          <Button variant="outline" size="sm" onClick={drop}>
            <KeyRoundIcon data-icon="inline-start" />
            Administrative access · {remaining}m
          </Button>
        ) : (
          <AlertDialog>
            <AlertDialogTrigger render={<Button variant="outline" size="sm" />}>
              <LockKeyholeIcon data-icon="inline-start" />
              <span className="hidden sm:inline">Limited access</span>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Gain Administrative access</AlertDialogTitle>
                <AlertDialogDescription>
                  Authenticate as {user.username}. Access expires after{" "}
                  {Math.round((admin.data?.idleTimeoutSeconds ?? 300) / 60)}{" "}
                  idle minutes.
                </AlertDialogDescription>
              </AlertDialogHeader>
              <InputGroup>
                <InputGroupAddon>
                  <LockKeyholeIcon aria-hidden="true" />
                </InputGroupAddon>
                <InputGroupInput
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder="Password"
                  aria-label="Password"
                  autoComplete="current-password"
                />
              </InputGroup>
              <InputGroup>
                <InputGroupAddon>
                  <KeyRoundIcon aria-hidden="true" />
                </InputGroupAddon>
                <InputGroupInput
                  type="password"
                  value={mfaResponse}
                  onChange={(event) => setMfaResponse(event.target.value)}
                  placeholder="MFA response (if required)"
                  aria-label="MFA response"
                  autoComplete="one-time-code"
                />
              </InputGroup>
              {elevationError && (
                <Alert variant="destructive" role="alert">
                  <AlertDescription>{elevationError}</AlertDescription>
                </Alert>
              )}
              <AlertDialogFooter>
                <AlertDialogCancel>Cancel</AlertDialogCancel>
                <AlertDialogAction
                  onClick={(event) => {
                    if (elevating) event.preventDefault()
                    void elevate()
                  }}
                  disabled={!password || elevating}
                >
                  {elevating ? "Checking policy…" : "Authenticate"}
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        )}
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger render={<Button variant="ghost" size="icon" />}>
            <SunIcon className="scale-100 rotate-0 transition-all dark:scale-0 dark:-rotate-90" />
            <MoonIcon className="absolute scale-0 rotate-90 transition-all dark:scale-100 dark:rotate-0" />
            <span className="sr-only">Toggle theme</span>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuGroup>
              <DropdownMenuRadioGroup
                value={theme}
                onValueChange={(value) => setTheme(value as Theme)}
              >
                <DropdownMenuRadioItem value="tako-light">
                  <SunIcon /> Tako Light
                </DropdownMenuRadioItem>
                <DropdownMenuRadioItem value="tako-dark">
                  <MoonIcon /> Tako Dark
                </DropdownMenuRadioItem>
                <DropdownMenuSeparator />
                <DropdownMenuRadioItem value="light">
                  <SunIcon /> Standard Light
                </DropdownMenuRadioItem>
                <DropdownMenuRadioItem value="dark">
                  <MoonIcon /> Standard Dark
                </DropdownMenuRadioItem>
                <DropdownMenuSeparator />
                <DropdownMenuRadioItem value="system">
                  <MonitorIcon /> System
                </DropdownMenuRadioItem>
              </DropdownMenuRadioGroup>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
        <Button
          variant="ghost"
          size="icon"
          onClick={logout}
          aria-label="Sign out"
        >
          <LogOutIcon />
        </Button>
      </div>
    </header>
  )
}
