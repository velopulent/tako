import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { StatusBadge } from "@/components/status-badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { toast } from "@/components/ui/toast"
import { APIError, api, setCSRFToken, type UserSession } from "@/lib/api"
import { formatDateTime, formatRelative } from "@/lib/format"
import { QueryError } from "@/lib/page"
import { sessionKey } from "@/lib/session"

const sessionsKey = ["user-sessions"] as const

// describeAgent turns a user agent into "Firefox on Linux". Unknown agents
// stay readable instead of guessing.
export function describeAgent(agent?: string) {
  if (!agent) return "Unknown browser"
  const browser = /Edg\//.test(agent)
    ? "Edge"
    : /Firefox\//.test(agent)
      ? "Firefox"
      : /Chrome\//.test(agent)
        ? "Chrome"
        : /Safari\//.test(agent)
          ? "Safari"
          : undefined
  const system = /Android/.test(agent)
    ? "Android"
    : /iPhone|iPad/.test(agent)
      ? "iOS"
      : /Windows/.test(agent)
        ? "Windows"
        : /Mac OS X/.test(agent)
          ? "macOS"
          : /Linux/.test(agent)
            ? "Linux"
            : undefined
  if (browser && system) return `${browser} on ${system}`
  return browser ?? system ?? agent.slice(0, 48)
}

export function UserSessionsCard() {
  const queryClient = useQueryClient()
  const sessions = useQuery({
    queryKey: sessionsKey,
    queryFn: () => api<{ sessions: UserSession[] }>("/auth/sessions"),
  })
  const revoke = useMutation({
    mutationFn: (item: UserSession) =>
      api(`/auth/sessions/${item.id}/revoke`, { method: "POST" }),
    onSuccess: (_, item) => {
      if (item.current) {
        setCSRFToken("")
        queryClient.setQueryData(sessionKey, null)
        return
      }
      toast.add({ title: "Session signed out", type: "success" })
      void queryClient.invalidateQueries({ queryKey: sessionsKey })
    },
    onError: (error) => {
      if (error instanceof APIError && error.status === 404) {
        void queryClient.invalidateQueries({ queryKey: sessionsKey })
        return
      }
      toast.add({
        title: "Could not sign out that session",
        description: error.message,
        type: "error",
      })
    },
  })
  return (
    <Card className="h-fit">
      <CardHeader>
        <CardTitle>Signed-in sessions</CardTitle>
        <CardDescription>
          Everywhere your account is signed in to this host. Signing a session
          out also closes its terminal.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {sessions.isPending ? (
          <Skeleton className="h-24" />
        ) : sessions.isError ? (
          <QueryError
            error={sessions.error}
            retry={() => void sessions.refetch()}
          />
        ) : (
          <ul className="flex flex-col divide-y">
            {sessions.data.sessions.map((item) => (
              <li
                key={item.id}
                className="flex flex-wrap items-center gap-3 py-2.5 first:pt-0 last:pb-0"
              >
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="flex flex-wrap items-center gap-2 text-sm font-medium">
                    {describeAgent(item.userAgent)}
                    {item.current && (
                      <StatusBadge tone="success">This browser</StatusBadge>
                    )}
                    {item.administrative && (
                      <StatusBadge tone="warning">Administrative</StatusBadge>
                    )}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {[
                      item.remoteAddress,
                      `signed in ${formatRelative(item.createdAt)}`,
                      `active ${formatRelative(item.seenAt)}`,
                    ]
                      .filter(Boolean)
                      .join(" · ")}
                  </span>
                  <span className="sr-only">
                    Signed in {formatDateTime(item.createdAt)}
                  </span>
                </span>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={revoke.isPending}
                  onClick={() => revoke.mutate(item)}
                >
                  {item.current ? "Sign out" : "Sign out session"}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}
