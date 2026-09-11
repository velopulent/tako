import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ClipboardListIcon, DatabaseIcon, XCircleIcon } from "lucide-react"

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
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type DiagnosticJob, type SessionResponse } from "@/lib/api"

const activeStates = new Set<DiagnosticJob["state"]>(["pending", "running"])

function stateVariant(state: DiagnosticJob["state"]) {
  if (state === "succeeded") return "secondary" as const
  if (state === "failed" || state === "interrupted")
    return "destructive" as const
  return "outline" as const
}

function stateLabel(state: DiagnosticJob["state"]) {
  if (state === "interrupted") return "Interrupted — not retried"
  return state[0].toUpperCase() + state.slice(1)
}

function dateLabel(value?: string) {
  return value ? new Date(value).toLocaleString() : "Not started"
}

function JobCard({
  job,
  csrfToken,
  onCancel,
  canceling,
}: {
  job: DiagnosticJob
  csrfToken: string
  onCancel: (id: string) => void
  canceling: boolean
}) {
  const host = job.result?.host
  return (
    <Card className="min-w-0">
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <ClipboardListIcon aria-hidden="true" className="size-4" />
              Host inventory
            </CardTitle>
            <CardDescription className="truncate" title={job.id}>
              {job.id} · {job.actor} · {dateLabel(job.createdAt)}
            </CardDescription>
          </div>
          <Badge variant={stateVariant(job.state)}>
            {stateLabel(job.state)}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="space-y-2">
          <div className="flex items-center justify-between text-sm">
            <span>{job.message}</span>
            <span className="text-muted-foreground tabular-nums">
              {job.progress}%
            </span>
          </div>
          <Progress
            value={job.progress}
            aria-label={`Job progress: ${job.progress}%`}
          />
        </div>
        {job.error && <p className="text-sm text-destructive">{job.error}</p>}
        {host && (
          <div className="grid gap-3 rounded-lg border p-3 text-sm sm:grid-cols-2">
            <div>
              <p className="text-muted-foreground">Hostname</p>
              <p className="font-medium">{host.hostname || "Unknown"}</p>
            </div>
            <div>
              <p className="text-muted-foreground">Operating system</p>
              <p className="font-medium">{host.operatingSystem}</p>
            </div>
            <div>
              <p className="text-muted-foreground">Kernel</p>
              <p className="font-medium">{host.kernel}</p>
            </div>
            <div>
              <p className="text-muted-foreground">Capabilities</p>
              <p className="font-medium">
                {job.result?.capabilities.length ?? 0} detected
              </p>
            </div>
          </div>
        )}
        {activeStates.has(job.state) && (
          <Button
            className="w-full sm:w-auto"
            disabled={!csrfToken || canceling}
            onClick={() => onCancel(job.id)}
            variant="outline"
          >
            <XCircleIcon data-icon="inline-start" />
            {canceling ? "Canceling…" : "Cancel job"}
          </Button>
        )}
      </CardContent>
    </Card>
  )
}

export function JobsPage() {
  const queryClient = useQueryClient()
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const jobs = useQuery({
    queryKey: ["jobs"],
    queryFn: () => api<{ items: DiagnosticJob[] }>("/jobs?limit=50"),
    refetchInterval: (jobsQuery) =>
      jobsQuery.state.data?.items.some((job) => activeStates.has(job.state))
        ? 2000
        : false,
  })
  const start = useMutation({
    mutationFn: () =>
      api<{ job: DiagnosticJob }>("/jobs/host-inventory", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: "{}",
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["jobs"] }),
  })
  const cancel = useMutation({
    mutationFn: (id: string) =>
      api<{ job: DiagnosticJob }>(`/jobs/${id}/cancel`, {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["jobs"] }),
  })

  const items = jobs.data?.items ?? []
  return (
    <main className="@container/main flex flex-1 flex-col gap-6 p-4 lg:p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-sm text-muted-foreground">
            In-memory diagnostics that keep running across navigation.
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            Jobs clear on service restart and are never retried silently.
          </p>
        </div>
        <Button
          disabled={!session.data?.csrfToken || start.isPending}
          onClick={() => start.mutate()}
        >
          <DatabaseIcon data-icon="inline-start" />
          {start.isPending ? "Starting…" : "Run host inventory"}
        </Button>
      </div>
      {start.isError && (
        <Alert variant="destructive">
          <AlertTitle>Could not start diagnostic job</AlertTitle>
          <AlertDescription>{start.error.message}</AlertDescription>
        </Alert>
      )}
      {jobs.isPending && <Skeleton className="h-64" />}
      {jobs.isError && (
        <Alert variant="destructive">
          <AlertTitle>Could not load diagnostic jobs</AlertTitle>
          <AlertDescription>{jobs.error.message}</AlertDescription>
        </Alert>
      )}
      {!jobs.isPending && !jobs.isError && items.length === 0 && (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ClipboardListIcon />
            </EmptyMedia>
            <EmptyTitle>No diagnostic jobs yet</EmptyTitle>
            <EmptyDescription>
              Run a host inventory to capture an in-memory, reconnectable
              report.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
      {items.length > 0 && (
        <section
          className="grid gap-4 @4xl/main:grid-cols-2"
          aria-label="Diagnostic jobs"
        >
          {items.map((job) => (
            <JobCard
              key={job.id}
              job={job}
              csrfToken={session.data?.csrfToken ?? ""}
              onCancel={(id) => cancel.mutate(id)}
              canceling={cancel.isPending && cancel.variables === job.id}
            />
          ))}
        </section>
      )}
    </main>
  )
}
