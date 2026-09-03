import { useQuery } from "@tanstack/react-query"
import * as React from "react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
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
import { api, type LoginHistoryPage, type SessionResponse } from "@/lib/api"

const outcomeItems = [
  { value: "all", label: "All events" },
  { value: "success", label: "Successful events" },
  { value: "failure", label: "Failed logins" },
]

function identityLabel(source: LoginHistoryPage["identity"]["source"]) {
  switch (source) {
    case "local":
      return "Local identity"
    case "nss-read-only":
      return "NSS read-only identity"
    default:
      return "Deleted or unknown identity"
  }
}

function eventLabel(event: LoginHistoryPage["items"][number]["event"]) {
  switch (event) {
    case "session-open":
      return "Session opened"
    case "session-close":
      return "Session closed"
    default:
      return "Login"
  }
}

export function AccountLoginHistory({ username }: { username?: string }) {
  const [target, setTarget] = React.useState(username ?? "")
  const [outcome, setOutcome] = React.useState<"all" | "success" | "failure">(
    "all"
  )
  const [cursor, setCursor] = React.useState("")
  const [cursorTarget, setCursorTarget] = React.useState("")
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const administrative = session.data?.administrative === true
  const selectedTarget =
    (username ?? target) || session.data?.user?.username || ""
  const activeCursor = cursorTarget === selectedTarget ? cursor : ""
  const history = useQuery({
    queryKey: ["login-history", selectedTarget, outcome, activeCursor],
    enabled: selectedTarget !== "",
    queryFn: () => {
      const params = new URLSearchParams({ limit: "50" })
      if (outcome !== "all") params.set("outcome", outcome)
      if (activeCursor) params.set("cursor", activeCursor)
      return api<LoginHistoryPage>(
        `/accounts/users/${encodeURIComponent(selectedTarget)}/login-history?${params}`
      )
    },
  })

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">
        Bounded events are read directly from the journal; Tako does not copy
        journal records into its database.
      </p>
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor="login-history-user">Account</FieldLabel>
          <Input
            id="login-history-user"
            value={selectedTarget}
            readOnly={!administrative || username !== undefined}
            onChange={(event) => {
              setTarget(event.target.value)
              setCursor("")
              setCursorTarget("")
            }}
            placeholder="Current account"
            autoComplete="off"
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="login-history-outcome">Outcome</FieldLabel>
          <Select
            items={outcomeItems}
            value={outcome}
            onValueChange={(value) => {
              setOutcome((value as "all" | "success" | "failure") || "all")
              setCursor("")
              setCursorTarget("")
            }}
          >
            <SelectTrigger id="login-history-outcome">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {outcomeItems.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
      </FieldGroup>

      {session.isPending && <Skeleton className="h-40" />}
      {session.isError && (
        <Alert variant="destructive">
          <AlertTitle>Session unavailable</AlertTitle>
          <AlertDescription>{session.error.message}</AlertDescription>
        </Alert>
      )}
      {!session.isPending && !session.isError && history.isPending && (
        <Skeleton className="h-56" />
      )}
      {!session.isPending && !session.isError && history.isError && (
        <Alert variant="destructive">
          <AlertTitle>Login history unavailable</AlertTitle>
          <AlertDescription>{history.error.message}</AlertDescription>
        </Alert>
      )}
      {!session.isPending &&
        !session.isError &&
        !history.isPending &&
        !history.isError &&
        history.data && (
          <>
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <span className="font-medium">
                {history.data.identity.username}
              </span>
              <Badge variant="outline">
                {identityLabel(history.data.identity.source)}
              </Badge>
            </div>
            {(history.data.items ?? []).length === 0 ? (
              <Empty>
                <EmptyHeader>
                  <EmptyTitle>No login events</EmptyTitle>
                  <EmptyDescription>
                    The journal has no matching login or session events for this
                    account and filter.
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <div className="overflow-x-auto rounded-md border">
                <table className="w-full min-w-[42rem] text-sm">
                  <thead className="bg-muted/50 text-left">
                    <tr>
                      <th className="px-3 py-2 font-medium">Time</th>
                      <th className="px-3 py-2 font-medium">Event</th>
                      <th className="px-3 py-2 font-medium">Outcome</th>
                      <th className="px-3 py-2 font-medium">Service</th>
                      <th className="px-3 py-2 font-medium">Remote</th>
                      <th className="px-3 py-2 font-medium">Session</th>
                    </tr>
                  </thead>
                  <tbody>
                    {history.data.items.map((item) => (
                      <tr
                        key={`${item.timestamp}-${item.event}-${item.service}-${item.session ?? ""}-${item.remote ?? ""}`}
                        className="border-t"
                      >
                        <td className="px-3 py-2 whitespace-nowrap">
                          {new Date(item.timestamp).toLocaleString()}
                        </td>
                        <td className="px-3 py-2">{eventLabel(item.event)}</td>
                        <td className="px-3 py-2">
                          <Badge
                            variant={
                              item.outcome === "failure"
                                ? "destructive"
                                : "secondary"
                            }
                          >
                            {item.outcome}
                          </Badge>
                        </td>
                        <td className="px-3 py-2">{item.service || "-"}</td>
                        <td className="px-3 py-2">{item.remote || "-"}</td>
                        <td className="px-3 py-2">{item.session || "-"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {history.data.nextCursor && (
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  setCursorTarget(selectedTarget)
                  setCursor(history.data.nextCursor ?? "")
                }}
              >
                Next page
              </Button>
            )}
          </>
        )}
    </div>
  )
}
