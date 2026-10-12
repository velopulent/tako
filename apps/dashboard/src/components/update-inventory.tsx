import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  CircleCheck,
  Lock,
  PackageCheck,
  RefreshCw,
  RotateCcw,
  ServerCog,
  ShieldAlert,
  TriangleAlert,
} from "lucide-react"
import * as React from "react"

import { KpatchSettingsCard } from "@/components/kpatch-settings-card"
import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
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
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import {
  UpdateAdvisoryList,
  VersionDiff,
} from "@/components/update-advisory-list"
import { UpdateHistoryCard } from "@/components/update-history-card"
import { activeUpdateJobStates } from "@/components/update-job-state"
import { UpdateLivePanel } from "@/components/update-live-panel"
import {
  APIError,
  api,
  type DiagnosticJob,
  type SessionResponse,
  type UpdateChange,
  type UpdateObservation,
  type UpdateOperation,
  type UpdateOutput,
  type UpdatePreview,
  type UpdateProgress,
  type UpdateStatus,
} from "@/lib/api"
import {
  formatAbsolute,
  formatRelative,
  plural,
  type SeveritySummary,
  summarizeSeverity,
} from "@/lib/update-advisories"
import { cn } from "@/lib/utils"

const updateJobStorageKey = "tako-update-job:v2"
const idleProgress: UpdateProgress = {
  sequence: 0,
  active: false,
  phase: "idle",
  current: 0,
  total: 0,
  percent: -1,
  message: "No update is running.",
  cancelable: false,
  timestamp: "",
}

function readSavedJob() {
  try {
    return sessionStorage.getItem(updateJobStorageKey) ?? ""
  } catch {
    return ""
  }
}

function removeSavedJob() {
  try {
    sessionStorage.removeItem(updateJobStorageKey)
  } catch {
    // Storage may be disabled.
  }
}

function parseEvent<T>(event: MessageEvent<string>) {
  try {
    return JSON.parse(event.data) as T
  } catch {
    return null
  }
}

function severitySentence(counts: SeveritySummary) {
  if (!counts.classified) {
    return "Review what changes below, then install everything in one transaction."
  }
  const parts: string[] = []
  if (counts.security > 0) {
    parts.push(
      plural(counts.security, "security update") +
        (counts.critical > 0 ? ` (${counts.critical} critical)` : "")
    )
  }
  if (counts.bugfix > 0)
    parts.push(plural(counts.bugfix, "bug fix", "bug fixes"))
  if (counts.enhancement > 0) {
    parts.push(plural(counts.enhancement, "enhancement"))
  }
  if (counts.other > 0) {
    parts.push(`${counts.other.toLocaleString()} other package updates`)
  }
  return `Includes ${parts.join(", ").replace(/, ([^,]*)$/, " and $1")}.`
}

// Mirrors UpdateChange.Risky in the backend.
function isRiskyChange(change: UpdateChange) {
  return (
    change.action === "remove" ||
    change.action === "downgrade" ||
    change.action === "replace" ||
    Boolean(
      change.currentRepository &&
        change.targetRepository &&
        change.currentRepository !== change.targetRepository
    ) ||
    Boolean(
      change.currentVendor &&
        change.targetVendor &&
        change.currentVendor !== change.targetVendor
    )
  )
}

const changeActions: Record<string, { label: string; order: number }> = {
  remove: { label: "Remove", order: 0 },
  downgrade: { label: "Downgrade", order: 1 },
  replace: { label: "Replace", order: 2 },
  install: { label: "Install", order: 3 },
  upgrade: { label: "Upgrade", order: 4 },
}

function groupChanges(changes: UpdateChange[]) {
  const groups = new Map<string, UpdateChange[]>()
  for (const change of changes) {
    const list = groups.get(change.action)
    if (list) list.push(change)
    else groups.set(change.action, [change])
  }
  return Array.from(groups, ([action, list]) => ({
    action,
    label:
      changeActions[action]?.label ??
      action.charAt(0).toUpperCase() + action.slice(1),
    risky: list.some(isRiskyChange) && action !== "upgrade",
    changes: list,
  })).sort(
    (a, b) =>
      (changeActions[a.action]?.order ?? 5) -
      (changeActions[b.action]?.order ?? 5)
  )
}

function useNow(intervalMs: number) {
  const [now, setNow] = React.useState(() => Date.now())
  React.useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), intervalMs)
    return () => window.clearInterval(timer)
  }, [intervalMs])
  return now
}

export function UpdateInventory() {
  const queryClient = useQueryClient()
  const now = useNow(60_000)
  const [jobID, setJobID] = React.useState(readSavedJob)
  const jobIDRef = React.useRef(jobID)
  const progressJobIDRef = React.useRef("")
  const seenSequencesRef = React.useRef(new Set<number>())
  const snapshotPendingRef = React.useRef(true)
  const snapshotSequenceRef = React.useRef(0)
  const snapshotTerminalRef = React.useRef(false)
  const [preview, setPreview] = React.useState<UpdatePreview | null>(null)
  const [dialogOpen, setDialogOpen] = React.useState(false)
  const [riskAccepted, setRiskAccepted] = React.useState(false)
  const [terminalError, setTerminalError] = React.useState<string | null>(null)
  const [observation, setObservation] = React.useState<UpdateObservation>({
    progress: idleProgress,
    output: [],
  })

  const query = useQuery({
    queryKey: ["updates"],
    queryFn: () => api<UpdateStatus>("/updates"),
  })
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const job = useQuery({
    queryKey: ["update-job", jobID],
    queryFn: () => api<{ job: DiagnosticJob }>(`/jobs/${jobID}`),
    enabled: jobID !== "",
    refetchInterval: (current) => {
      const state = current.state.data?.job?.state
      return state && activeUpdateJobStates.has(state) ? 2000 : false
    },
  })

  const clearLiveState = React.useCallback(() => {
    progressJobIDRef.current = ""
    setObservation({ progress: idleProgress, output: [] })
  }, [])

  const settleActivity = React.useCallback(
    (failure?: string) => {
      jobIDRef.current = ""
      setJobID("")
      removeSavedJob()
      clearLiveState()
      setTerminalError(failure ?? null)
      void queryClient.invalidateQueries({ queryKey: ["updates"] })
      void queryClient.invalidateQueries({ queryKey: ["update-history"] })
    },
    [clearLiveState, queryClient]
  )

  React.useEffect(() => {
    jobIDRef.current = jobID
  }, [jobID])

  React.useEffect(() => {
    const source = new EventSource("/api/v1/updates/live")
    const onOpen = () => {
      snapshotPendingRef.current = true
      snapshotSequenceRef.current = 0
      snapshotTerminalRef.current = false
    }
    const acceptSequence = (sequence: number) => {
      if (!Number.isFinite(sequence)) return false
      if (sequence <= 0) return true
      const seen = seenSequencesRef.current
      if (seen.has(sequence)) return false
      seen.add(sequence)
      if (seen.size > 2000) {
        const oldest = seen.values().next().value
        if (typeof oldest === "number") seen.delete(oldest)
      }
      return true
    }

    const onProgress = (event: MessageEvent<string>) => {
      const progress = parseEvent<UpdateProgress>(event)
      if (
        !progress ||
        typeof progress.sequence !== "number" ||
        typeof progress.active !== "boolean" ||
        typeof progress.phase !== "string" ||
        !acceptSequence(progress.sequence)
      ) {
        return
      }

      const isInitialSnapshot = snapshotPendingRef.current
      if (isInitialSnapshot) {
        snapshotPendingRef.current = false
        snapshotSequenceRef.current = progress.sequence
        snapshotTerminalRef.current = !progress.active
      } else if (
        snapshotSequenceRef.current > 0 &&
        progress.sequence > 0 &&
        progress.sequence <= snapshotSequenceRef.current
      ) {
        return
      }

      const eventJobID = progress.jobId ?? ""
      const trackedJobID = jobIDRef.current
      if (trackedJobID && eventJobID && trackedJobID !== eventJobID) return

      const previousJobID = progressJobIDRef.current
      progressJobIDRef.current = eventJobID
      setObservation((current) => ({
        progress,
        output: previousJobID !== eventJobID ? [] : current.output,
      }))
      if (progress.active) setTerminalError(null)

      if (!progress.active) {
        const belongsToTrackedJob =
          !trackedJobID ||
          eventJobID === trackedJobID ||
          (eventJobID === "" && previousJobID === trackedJobID)
        if (!belongsToTrackedJob) return

        const failed = progress.phase === "failed" || progress.phase === "error"
        if (progress.phase === "idle" && !trackedJobID && !previousJobID) {
          return
        }
        settleActivity(
          failed
            ? progress.message ||
                "The update failed. Review the error and retry."
            : undefined
        )
      }
    }

    const onOutput = (event: MessageEvent<string>) => {
      const output = parseEvent<UpdateOutput>(event)
      if (
        !output ||
        typeof output.sequence !== "number" ||
        typeof output.stream !== "string" ||
        typeof output.line !== "string" ||
        !acceptSequence(output.sequence)
      ) {
        return
      }

      if (
        snapshotTerminalRef.current &&
        snapshotSequenceRef.current > 0 &&
        output.sequence > 0 &&
        output.sequence <= snapshotSequenceRef.current
      ) {
        return
      }

      const outputJobID = output.jobId ?? ""
      const trackedJobID = jobIDRef.current
      const currentProgressJobID = progressJobIDRef.current
      if (!trackedJobID && !currentProgressJobID) {
        return
      }
      if (
        (trackedJobID && outputJobID && trackedJobID !== outputJobID) ||
        (currentProgressJobID &&
          outputJobID &&
          currentProgressJobID !== outputJobID)
      ) {
        return
      }

      setObservation((current) => ({
        ...current,
        output: [...current.output, output].slice(-500),
      }))
    }

    source.addEventListener("open", onOpen as EventListener)
    source.addEventListener("progress", onProgress as EventListener)
    source.addEventListener("output", onOutput as EventListener)
    return () => source.close()
  }, [settleActivity])

  React.useEffect(() => {
    const onVisibilityChange = () => {
      if (document.hidden) return
      void queryClient.invalidateQueries({ queryKey: ["updates"] })
      void queryClient.invalidateQueries({ queryKey: ["update-history"] })
    }
    document.addEventListener("visibilitychange", onVisibilityChange)
    return () =>
      document.removeEventListener("visibilitychange", onVisibilityChange)
  }, [queryClient])

  React.useEffect(() => {
    if (!jobID) return

    if (job.isError) {
      if (job.error instanceof APIError && job.error.status === 404) {
        settleActivity()
      }
      return
    }

    if (!job.isSuccess) return
    const value = job.data?.job
    if (!value) {
      settleActivity()
      return
    }
    if (activeUpdateJobStates.has(value.state)) return

    const failed = value.state === "failed" || value.state === "interrupted"
    settleActivity(
      failed
        ? value.error ||
            value.message ||
            "The update failed. Review the error and retry."
        : undefined
    )
  }, [job.data, job.error, job.isError, job.isSuccess, jobID, settleActivity])

  const refresh = useMutation({
    mutationFn: () =>
      api<UpdateStatus>("/updates/refresh", {
        method: "POST",
        body: JSON.stringify({ force: true }),
      }),
    onMutate: () => setTerminalError(null),
    onSuccess: (status) => {
      queryClient.setQueryData(["updates"], status)
      clearLiveState()
    },
  })
  const previewRequest = useMutation({
    mutationFn: (operation: UpdateOperation) =>
      api<UpdatePreview>("/updates/preview", {
        method: "POST",
        body: JSON.stringify(operation),
      }),
    onMutate: () => setTerminalError(null),
    onSuccess: (value) => {
      setPreview(value)
      setRiskAccepted(false)
    },
  })
  const apply = useMutation({
    mutationFn: (operation: UpdateOperation) =>
      api<{ job: DiagnosticJob }>("/updates", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(operation),
      }),
    onMutate: () => setTerminalError(null),
    onSuccess: ({ job: value }) => {
      jobIDRef.current = value.id
      setJobID(value.id)
      clearLiveState()
      setDialogOpen(false)
      try {
        sessionStorage.setItem(updateJobStorageKey, value.id)
      } catch {
        // Storage may be disabled.
      }
    },
  })
  const cancel = useMutation({
    mutationFn: () => {
      const trackedJobID = jobIDRef.current
      return api<{ job: DiagnosticJob }>(`/jobs/${trackedJobID}/cancel`, {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
      })
    },
    onMutate: () => setTerminalError(null),
    onSuccess: ({ job: value }) => {
      if (activeUpdateJobStates.has(value.state)) return
      const failed = value.state === "failed" || value.state === "interrupted"
      settleActivity(
        failed
          ? value.error ||
              value.message ||
              "The update failed. Review the error and retry."
          : undefined
      )
    },
  })

  const requestPreview = () => {
    if (!query.data) return
    previewRequest.reset()
    setPreview(null)
    setRiskAccepted(false)
    setDialogOpen(true)
    previewRequest.mutate({
      expectedFingerprint: query.data.fingerprint,
      confirmed: false,
    })
  }
  const retryUpdateState = () => {
    setTerminalError(null)
    refresh.reset()
    apply.reset()
    cancel.reset()
    refresh.mutate()
  }

  if (query.isPending) {
    return (
      <div
        className="mx-auto flex w-full max-w-7xl flex-col gap-6"
        aria-busy="true"
      >
        <span className="sr-only" role="status">
          Loading updates…
        </span>
        <Skeleton className="h-36 w-full rounded-xl" />
        <div className="flex flex-col gap-3">
          <Skeleton className="h-8 w-full sm:w-72" />
          <Skeleton className="h-80 w-full rounded-xl" />
        </div>
      </div>
    )
  }
  if (query.isError || !query.data) {
    return (
      <div className="mx-auto w-full max-w-7xl">
        <Alert variant="destructive">
          <TriangleAlert />
          <AlertTitle>Update inventory unavailable</AlertTitle>
          <AlertDescription>
            {query.error?.message ||
              "The package inventory could not be loaded."}
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              Retry
            </Button>
          </AlertAction>
        </Alert>
      </div>
    )
  }

  const status = query.data
  const activeJob = job.data?.job
  const packageCount = status.packages.length
  const counts = summarizeSeverity(status.packages)
  const trackedJob = Boolean(jobID)
  const updateRunning =
    trackedJob ||
    Boolean(activeJob && activeUpdateJobStates.has(activeJob.state)) ||
    observation.progress.active
  const activityActive =
    updateRunning || refresh.isPending || previewRequest.isPending
  const pageError =
    terminalError ||
    refresh.error?.message ||
    apply.error?.message ||
    cancel.error?.message
  const canUpdate =
    status.available &&
    !status.externalLock &&
    packageCount > 0 &&
    !activityActive &&
    !previewRequest.isPending
  const tone: "ok" | "pending" | "security" | "blocked" =
    !status.available || status.externalLock
      ? "blocked"
      : packageCount === 0
        ? "ok"
        : counts.security > 0
          ? "security"
          : "pending"
  const statusTitle = !status.available
    ? "Updates unavailable"
    : status.externalLock
      ? "Package manager busy"
      : packageCount === 0
        ? "System is up to date"
        : `${plural(packageCount, "update")} available`
  const statusMessage = !status.available
    ? status.reason ||
      status.message ||
      "The package manager could not be reached. Check that the Tako session service is running, then try again."
    : status.externalLock
      ? "Updates are paused until the other package operation finishes."
      : packageCount === 0
        ? "Every installed package is on its latest available version."
        : severitySentence(counts)
  const StatusIcon =
    tone === "blocked"
      ? TriangleAlert
      : tone === "ok"
        ? CircleCheck
        : tone === "security"
          ? ShieldAlert
          : PackageCheck
  const refreshedAt = status.metadataUpdated
  const meta = [
    status.version || status.backend,
    refreshedAt
      ? `Repositories refreshed ${formatRelative(refreshedAt, now)}`
      : "",
  ].filter(Boolean)
  const recovery = status.recovery
  const afterNotes = [
    ...(recovery && !recovery.authoritative && recovery.reason
      ? [recovery.reason]
      : []),
    ...(recovery && !recovery.authoritative ? recovery.hints : []),
  ]
  const sideCards = (
    <>
      <KpatchSettingsCard
        csrfToken={session.data?.csrfToken ?? ""}
        administrative={session.data?.administrative ?? false}
      />
      <UpdateHistoryCard enabled={status.available} />
    </>
  )
  const riskyChanges = preview?.changes.filter(isRiskyChange) ?? []
  const changeGroups = preview ? groupChanges(preview.changes) : []

  return (
    <div className="mx-auto flex w-full max-w-7xl flex-col gap-6">
      {pageError && (
        <Alert variant="destructive">
          <TriangleAlert />
          <AlertTitle>Update action needs attention</AlertTitle>
          <AlertDescription>{pageError}</AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              disabled={refresh.isPending}
              onClick={retryUpdateState}
            >
              Check again
            </Button>
          </AlertAction>
        </Alert>
      )}

      <Card>
        <CardHeader className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="flex min-w-0 items-start gap-4">
            <span
              className={cn(
                "flex size-11 shrink-0 items-center justify-center rounded-xl bg-muted text-foreground",
                tone === "security" && "bg-destructive/10 text-destructive",
                tone === "blocked" && "text-muted-foreground"
              )}
              aria-hidden="true"
            >
              <StatusIcon className="size-5" />
            </span>
            <div className="flex min-w-0 flex-col gap-1">
              <CardTitle className="text-lg font-semibold tracking-tight">
                <h2>{statusTitle}</h2>
              </CardTitle>
              <CardDescription className="text-pretty">
                {statusMessage}
              </CardDescription>
              {meta.length > 0 && (
                <p
                  className="text-xs text-muted-foreground"
                  title={
                    refreshedAt
                      ? `Repository metadata downloaded ${formatAbsolute(refreshedAt)}`
                      : undefined
                  }
                >
                  {meta.join(" · ")}
                </p>
              )}
            </div>
          </div>
          <div className="flex w-full shrink-0 items-center gap-2 sm:w-auto">
            <Button
              variant="outline"
              className="flex-1 sm:flex-none"
              disabled={refresh.isPending || activityActive}
              onClick={() => refresh.mutate()}
            >
              {refresh.isPending ? (
                <Spinner data-icon="inline-start" aria-hidden="true" />
              ) : (
                <RefreshCw data-icon="inline-start" aria-hidden="true" />
              )}
              {refresh.isPending ? "Checking…" : "Check for updates"}
            </Button>
            {status.available && packageCount > 0 && !updateRunning && (
              <Button
                className="flex-1 sm:flex-none"
                disabled={!canUpdate}
                onClick={requestPreview}
              >
                {previewRequest.isPending && (
                  <Spinner data-icon="inline-start" aria-hidden="true" />
                )}
                Review and install
              </Button>
            )}
          </div>
        </CardHeader>

        {(status.externalLock ||
          recovery?.rebootRequired ||
          (recovery?.restartServices.length ?? 0) > 0 ||
          (recovery?.manualPackages?.length ?? 0) > 0) && (
          <CardContent className="flex flex-col gap-3">
            {status.externalLock && (
              <Alert>
                <Lock />
                <AlertTitle>Another package operation is running</AlertTitle>
                <AlertDescription>
                  {status.lockReason ||
                    "Another package-manager operation is active."}{" "}
                  Check again once it finishes.
                </AlertDescription>
              </Alert>
            )}
            {recovery?.rebootRequired && (
              <Alert>
                <RotateCcw />
                <AlertTitle>Restart required</AlertTitle>
                <AlertDescription>
                  {recovery.reason ||
                    recovery.hints[0] ||
                    "Installed updates take effect after the host restarts."}
                  {recovery.rebootPackages &&
                    recovery.rebootPackages.length > 0 && (
                      <span className="mt-1 block">
                        Affected packages: {recovery.rebootPackages.join(", ")}
                      </span>
                    )}
                </AlertDescription>
              </Alert>
            )}
            {recovery && recovery.restartServices.length > 0 && (
              <Alert>
                <ServerCog />
                <AlertTitle>Services may need a restart</AlertTitle>
                <AlertDescription>
                  {recovery.restartServices.join(", ")}
                </AlertDescription>
              </Alert>
            )}
            {recovery?.manualPackages && recovery.manualPackages.length > 0 && (
              <Alert>
                <TriangleAlert />
                <AlertTitle>Manual recovery may be required</AlertTitle>
                <AlertDescription>
                  {recovery.manualPackages.join(", ")}
                </AlertDescription>
              </Alert>
            )}
          </CardContent>
        )}
      </Card>

      <UpdateLivePanel
        observation={observation}
        job={activeJob}
        canceling={cancel.isPending}
        onCancel={
          jobID &&
          activeJob &&
          activeUpdateJobStates.has(activeJob.state) &&
          (activeJob.state === "pending" ||
            observation.progress.cancelable ||
            (activeJob.state === "running" &&
              activeJob.kind === "software-update" &&
              activeJob.progress < 20))
            ? () => cancel.mutate()
            : undefined
        }
      />

      {packageCount > 0 ? (
        <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_20rem]">
          <section
            aria-labelledby="pending-updates-heading"
            className="flex min-w-0 flex-col gap-3"
          >
            <div className="flex items-baseline justify-between gap-3">
              <h3
                id="pending-updates-heading"
                className="text-base font-semibold"
              >
                Pending updates
              </h3>
              <span className="text-xs text-muted-foreground tabular-nums">
                {plural(packageCount, "package")}
              </span>
            </div>
            <UpdateAdvisoryList packages={status.packages} />
          </section>
          <aside
            className="flex min-w-0 flex-col gap-4 xl:sticky xl:top-4"
            aria-label="Update tools"
          >
            {sideCards}
          </aside>
        </div>
      ) : (
        <div className="grid items-start gap-6 md:grid-cols-2">{sideCards}</div>
      )}

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="flex max-h-[min(90dvh,48rem)] flex-col gap-4 sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>
              {preview?.allowed && !preview.stale
                ? "Confirm full-system update"
                : preview
                  ? "Update cannot start"
                  : "Preparing update plan"}
            </DialogTitle>
            <DialogDescription>
              {preview?.allowed && !preview.stale
                ? "The package manager resolved this exact transaction. Nothing changes until you install it."
                : preview?.reason ||
                  (preview
                    ? "Review the details below."
                    : "Resolving dependencies against the current inventory…")}
            </DialogDescription>
          </DialogHeader>

          <div className="-mx-4 flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4">
            {previewRequest.isPending && !preview && (
              <div className="flex flex-col gap-2" role="status">
                <Skeleton className="h-6 w-2/3" />
                <Skeleton className="h-40 w-full" />
                <span className="sr-only">Preparing update plan</span>
              </div>
            )}

            {previewRequest.isError && !preview && (
              <Alert variant="destructive">
                <TriangleAlert />
                <AlertTitle>Preview unavailable</AlertTitle>
                <AlertDescription>
                  {previewRequest.error.message}
                </AlertDescription>
              </Alert>
            )}

            {preview && (
              <>
                {(preview.stale || !preview.allowed) && (
                  <Alert variant="destructive">
                    <TriangleAlert />
                    <AlertTitle>
                      {preview.stale
                        ? "Inventory changed"
                        : "Update is blocked"}
                    </AlertTitle>
                    <AlertDescription>
                      {preview.stale
                        ? "Packages changed since this page loaded. Check for updates, then review again."
                        : preview.reason ||
                          "The update backend did not authorize this plan."}
                    </AlertDescription>
                  </Alert>
                )}

                {preview.changes.length > 0 && (
                  <dl className="grid grid-cols-[repeat(auto-fit,minmax(7rem,1fr))] gap-2">
                    {changeGroups.map((group) => (
                      <div
                        key={group.action}
                        className={cn(
                          "flex flex-col gap-0.5 rounded-lg border px-3 py-2",
                          group.risky &&
                            "border-destructive/40 bg-destructive/5"
                        )}
                      >
                        <dt className="text-xs text-muted-foreground">
                          {group.label}
                        </dt>
                        <dd
                          className={cn(
                            "text-lg font-semibold tabular-nums",
                            group.risky && "text-destructive"
                          )}
                        >
                          {group.changes.length}
                        </dd>
                      </div>
                    ))}
                  </dl>
                )}

                {preview.warnings.length > 0 && (
                  <Alert>
                    <TriangleAlert />
                    <AlertTitle>Review these warnings</AlertTitle>
                    <AlertDescription>
                      <ul className="flex list-disc flex-col gap-1 pl-4">
                        {preview.warnings.map((warning) => (
                          <li key={warning}>{warning}</li>
                        ))}
                      </ul>
                    </AlertDescription>
                  </Alert>
                )}

                {preview.changes.length > 0 ? (
                  <div className="flex flex-col overflow-hidden rounded-lg border">
                    {changeGroups.map((group) => (
                      <section
                        key={group.action}
                        aria-label={group.label}
                        className="not-last:border-b"
                      >
                        <h3
                          className={cn(
                            "sticky top-0 bg-muted px-3 py-1.5 text-xs font-medium",
                            group.risky && "text-destructive"
                          )}
                        >
                          {group.label} · {group.changes.length}
                        </h3>
                        <ul>
                          {group.changes.map((change) => (
                            <li
                              key={`${change.action}:${change.name}:${change.architecture ?? ""}`}
                              className="flex flex-col gap-0.5 px-3 py-2 text-sm not-last:border-b sm:flex-row sm:items-center sm:justify-between sm:gap-4"
                            >
                              <span className="min-w-0 truncate">
                                {change.name}
                                {change.architecture && (
                                  <span className="text-muted-foreground">
                                    .{change.architecture}
                                  </span>
                                )}
                                {isRiskyChange(change) &&
                                  change.action === "upgrade" && (
                                    <Badge
                                      variant="destructive"
                                      className="ml-2"
                                    >
                                      Source changes
                                    </Badge>
                                  )}
                              </span>
                              {change.candidateVersion ? (
                                <VersionDiff
                                  from={change.currentVersion}
                                  to={change.candidateVersion}
                                  className="min-w-0 truncate sm:text-right"
                                />
                              ) : (
                                <span className="font-mono text-xs text-muted-foreground">
                                  {change.currentVersion}
                                </span>
                              )}
                            </li>
                          ))}
                        </ul>
                      </section>
                    ))}
                  </div>
                ) : (
                  <p className="text-sm text-muted-foreground">
                    No package changes are included in this plan.
                  </p>
                )}

                {afterNotes.length > 0 && preview.allowed && !preview.stale && (
                  <div className="flex flex-col gap-1 rounded-lg bg-muted/50 px-3 py-2.5 text-xs text-muted-foreground">
                    <p className="font-medium text-foreground">
                      After installing
                    </p>
                    {afterNotes.map((note) => (
                      <p key={note}>{note}</p>
                    ))}
                  </div>
                )}

                {preview.requiresRiskConfirmation && (
                  <FieldGroup>
                    <Field
                      orientation="horizontal"
                      className="rounded-lg border border-destructive/40 p-3"
                    >
                      <Checkbox
                        id="update-risk-accepted"
                        checked={riskAccepted}
                        onCheckedChange={(checked) =>
                          setRiskAccepted(checked === true)
                        }
                      />
                      <FieldContent>
                        <FieldLabel htmlFor="update-risk-accepted">
                          I reviewed the{" "}
                          {riskyChanges.length > 0
                            ? plural(riskyChanges.length, "high-risk change")
                            : "high-risk changes"}
                        </FieldLabel>
                        <FieldDescription>
                          Removals, downgrades, replacements, and repository or
                          vendor switches can break installed software.
                        </FieldDescription>
                      </FieldContent>
                    </Field>
                  </FieldGroup>
                )}

                {session.data && !session.data.administrative && (
                  <Alert variant="destructive">
                    <ShieldAlert />
                    <AlertTitle>Administrative access required</AlertTitle>
                    <AlertDescription>
                      Gain administrative access before applying system changes.
                    </AlertDescription>
                  </Alert>
                )}
              </>
            )}
          </div>

          <DialogFooter className="flex-col-reverse sm:flex-row">
            <Button variant="outline" onClick={() => setDialogOpen(false)}>
              Cancel
            </Button>
            {previewRequest.isError && !preview ? (
              <Button
                onClick={requestPreview}
                disabled={previewRequest.isPending}
              >
                Retry preview
              </Button>
            ) : preview?.stale ? (
              <Button
                onClick={() => {
                  setDialogOpen(false)
                  refresh.mutate()
                }}
              >
                <RefreshCw data-icon="inline-start" />
                Check for updates
              </Button>
            ) : (
              <Button
                disabled={
                  !preview?.allowed ||
                  (preview?.requiresRiskConfirmation && !riskAccepted) ||
                  session.data?.administrative === false ||
                  !session.data?.csrfToken ||
                  apply.isPending
                }
                onClick={() =>
                  preview &&
                  apply.mutate({
                    expectedFingerprint: preview.fingerprint,
                    confirmed: true,
                    riskAccepted,
                  })
                }
              >
                {apply.isPending && (
                  <Spinner data-icon="inline-start" aria-hidden="true" />
                )}
                {apply.isPending
                  ? "Starting…"
                  : preview && preview.changes.length > 0
                    ? `Install ${plural(preview.changes.length, "change")}`
                    : "Install updates"}
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
