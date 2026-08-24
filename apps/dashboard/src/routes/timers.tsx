import * as React from "react"
import { createFileRoute } from "@tanstack/react-router"
import { useMutation, useQuery } from "@tanstack/react-query"

import { TimerForm } from "@/components/timer-form"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import {
  api,
  APIError,
  type SessionResponse,
  type TimerOperation,
  type TimerState,
} from "@/lib/api"

const initialOperation: TimerOperation = {
  action: "create",
  scope: "user",
  name: "nightly",
  description: "Nightly task",
  onCalendar: "*-*-* 03:00:00",
  command: "/usr/local/bin/backup",
  persistent: true,
}

function errorMessage(error: unknown, fallback: string) {
  if (error instanceof APIError) {
    if (error.code === "timer-conflict") {
      return "This timer changed in another browser. Preview it again before applying."
    }
    if (error.code === "administrative-access-required") {
      return "Gain administrative access before changing a system timer."
    }
    if (error.code === "user-session-required") {
      return "A user session is required for user timers."
    }
    if (error.message) return error.message
  }
  return fallback
}

function TimerStateCard({ state }: { state: TimerState }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Current timer state</CardTitle>
        <CardDescription>
          {state.timerUnit} and {state.serviceUnit} are managed as one pair.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        <div className="flex flex-wrap gap-x-6 gap-y-2">
          <span>{state.exists ? "Pair exists" : "No timer files yet"}</span>
          <span>{state.enabled ? "Enabled" : "Disabled"}</span>
        </div>
        {state.fingerprint && (
          <p className="font-mono text-xs break-all text-muted-foreground">
            Fingerprint: {state.fingerprint}
          </p>
        )}
        {state.definition?.command && (
          <p className="break-all text-muted-foreground">
            Command: {state.definition.command}
          </p>
        )}
      </CardContent>
    </Card>
  )
}

export function TimersPage() {
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const [operation, setOperation] = React.useState(initialOperation)
  const preview = useMutation({
    mutationFn: (value: TimerOperation) =>
      api<TimerState>("/timers/preview", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(value),
      }),
  })
  const apply = useMutation({
    mutationFn: (value: TimerOperation) =>
      api<TimerState>("/timers", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(value),
      }),
    onSuccess: () => preview.reset(),
  })
  const pending = preview.isPending || apply.isPending || session.isPending
  const state = apply.data ?? preview.data

  if (session.isPending) {
    return (
      <main className="flex flex-1 flex-col gap-6 p-4 lg:p-6">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-96 w-full" />
      </main>
    )
  }
  if (session.isError || !session.data) {
    return (
      <main className="flex flex-1 flex-col gap-6 p-4 lg:p-6">
        <Alert variant="destructive">
          <AlertTitle>Timer controls unavailable</AlertTitle>
          <AlertDescription>
            {errorMessage(session.error, "The session could not be loaded.")}
          </AlertDescription>
        </Alert>
      </main>
    )
  }

  return (
    <main className="@container/main flex flex-1 flex-col gap-6 p-4 lg:p-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">
          Systemd timers
        </h1>
        <p className="text-sm text-muted-foreground">
          Create safe timer/service pairs with stale-write protection. No shell
          unit text is accepted.
        </p>
      </div>
      {(preview.isError || apply.isError) && (
        <Alert variant="destructive">
          <AlertTitle>Timer operation failed</AlertTitle>
          <AlertDescription>
            {errorMessage(
              preview.error ?? apply.error,
              "Refresh the timer state and try again."
            )}
          </AlertDescription>
        </Alert>
      )}
      {!session.data.administrative && operation.scope === "system" && (
        <Alert>
          <AlertTitle>Read-only system scope</AlertTitle>
          <AlertDescription>
            System timer changes require temporary administrative access. User
            scope remains available.
          </AlertDescription>
        </Alert>
      )}
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(18rem,0.45fr)]">
        <Card>
          <CardHeader>
            <CardTitle>Timer definition</CardTitle>
            <CardDescription>
              Preview reads the current pair. Apply requires its fingerprint for
              every existing pair.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <TimerForm
              value={operation}
              onChange={setOperation}
              onSubmit={() => apply.mutate(operation)}
              disabled={pending}
              submitLabel={apply.isPending ? "Applying…" : "Apply timer"}
            />
            <Button
              type="button"
              variant="link"
              className="mt-3 px-0 text-muted-foreground"
              disabled={pending}
              onClick={() => preview.mutate(operation)}
            >
              Preview current state
            </Button>
          </CardContent>
        </Card>
        {state ? (
          <TimerStateCard state={state} />
        ) : (
          <Card className="h-fit">
            <CardHeader>
              <CardTitle>Preview</CardTitle>
              <CardDescription>
                Preview before applying to catch missing pairs and stale edits.
              </CardDescription>
            </CardHeader>
          </Card>
        )}
      </div>
    </main>
  )
}

export const Route = createFileRoute("/timers")({
  component: TimersPage,
})
