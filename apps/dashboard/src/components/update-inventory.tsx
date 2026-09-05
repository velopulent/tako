import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  Check,
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
  CardFooter,
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
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Progress } from "@/components/ui/progress"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { UpdateHistoryCard } from "@/components/update-history-card"
import { activeUpdateJobStates } from "@/components/update-job-state"
import { UpdateLivePanel } from "@/components/update-live-panel"
import { UpdatePackageTable } from "@/components/update-package-table"
import {
  APIError,
  api,
  type DiagnosticJob,
  type SessionResponse,
  type UpdateObservation,
  type UpdateOperation,
  type UpdateOutput,
  type UpdatePreview,
  type UpdateProgress,
  type UpdateStatus,
} from "@/lib/api"

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

function formatLastChecked(value?: string) {
  if (!value) return ""
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? "" : date.toLocaleString()
}

function parseEvent<T>(event: MessageEvent<string>) {
  try {
    return JSON.parse(event.data) as T
  } catch {
    return null
  }
}

function severityCounts(packages: UpdateStatus["packages"]) {
  return packages.reduce(
    (counts, pkg) => {
      const severity = (pkg.severity ?? "").toLowerCase()
      if (severity === "security" || pkg.secSeverity) counts.security += 1
      else if (severity === "bugfix") counts.bugfix += 1
      else counts.enhancement += 1
      return counts
    },
    { security: 0, bugfix: 0, enhancement: 0 }
  )
}

export function UpdateInventory() {
  const queryClient = useQueryClient()
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
      <div className="mx-auto w-full max-w-7xl">
        <Skeleton className="h-72 w-full" />
      </div>
    )
  }
  if (query.isError || !query.data) {
    return (
      <div className="mx-auto w-full max-w-7xl">
        <Alert variant="destructive">
          <TriangleAlert />
          <AlertTitle>Update inventory unavailable</AlertTitle>
          <AlertDescription>{query.error?.message}</AlertDescription>
        </Alert>
      </div>
    )
  }

  const status = query.data
  const activeJob = job.data?.job
  const packageCount = status.packages.length
  const counts = severityCounts(status.packages)
  const trackedJob = Boolean(jobID)
  const activityActive =
    trackedJob ||
    Boolean(activeJob && activeUpdateJobStates.has(activeJob.state)) ||
    observation.progress.active ||
    refresh.isPending ||
    previewRequest.isPending
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
  const statusTitle = !status.available
    ? "Updates unavailable"
    : status.externalLock
      ? "Package manager busy"
      : packageCount === 0
        ? "System is up to date"
        : `${packageCount} update${packageCount === 1 ? "" : "s"} available`
  const statusMessage = !status.available
    ? status.reason || status.message
    : status.externalLock
      ? "Updates are paused until the package manager is free."
      : packageCount === 0
        ? "No installed-software updates are waiting."
        : status.message
  const icon =
    !status.available || status.externalLock ? (
      <TriangleAlert aria-hidden="true" />
    ) : packageCount === 0 ? (
      <Check aria-hidden="true" />
    ) : (
      <PackageCheck aria-hidden="true" />
    )

  return (
    <div className="mx-auto flex w-full max-w-7xl flex-col gap-6">
      {pageError && (
        <Alert variant="destructive">
          <TriangleAlert />
          <AlertTitle>Update action needs attention</AlertTitle>
          <AlertDescription>{pageError}</AlertDescription>
          <AlertAction>
            <Button
              variant="ghost"
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
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <CardTitle>System updates</CardTitle>
              <Badge variant={packageCount > 0 ? "secondary" : "outline"}>
                {packageCount} package{packageCount === 1 ? "" : "s"}
              </Badge>
            </div>
            <CardDescription>
              Review the exact changes before applying a full-system update.
            </CardDescription>
          </div>
          <div className="flex items-center gap-2">
            {query.isFetching && (
              <Badge variant="outline">Refreshing inventory…</Badge>
            )}
            <Button
              variant="outline"
              size="icon"
              aria-label="Check for updates"
              disabled={refresh.isPending || activityActive}
              onClick={() => refresh.mutate()}
            >
              <RefreshCw
                className={refresh.isPending ? "animate-spin" : ""}
                aria-hidden="true"
              />
            </Button>
          </div>
        </CardHeader>

        <CardContent className="flex flex-col gap-5">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-start">
            <div className="flex size-11 shrink-0 items-center justify-center rounded-xl bg-muted text-foreground">
              {icon}
            </div>
            <div className="min-w-0 flex-1">
              <h2 className="text-lg font-semibold tracking-tight">
                {statusTitle}
              </h2>
              <p className="mt-1 text-sm text-muted-foreground">
                {statusMessage}
              </p>
              <div className="mt-3 flex flex-wrap gap-2">
                <Badge
                  variant={counts.security > 0 ? "destructive" : "outline"}
                >
                  <ShieldAlert data-icon="inline-start" aria-hidden="true" />
                  {counts.security} security
                </Badge>
                <Badge variant={counts.bugfix > 0 ? "secondary" : "outline"}>
                  {counts.bugfix} bug fix{counts.bugfix === 1 ? "" : "es"}
                </Badge>
                <Badge variant="outline">
                  {counts.enhancement} enhancement
                  {counts.enhancement === 1 ? "" : "s"}
                </Badge>
              </div>
            </div>
          </div>

          {status.externalLock && (
            <Alert variant="destructive">
              <TriangleAlert />
              <AlertTitle>Updates paused</AlertTitle>
              <AlertDescription>
                {status.lockReason ||
                  "Another package-manager operation is active."}
              </AlertDescription>
            </Alert>
          )}
          {!status.available && (
            <Alert variant="destructive">
              <TriangleAlert />
              <AlertTitle>Update provider unavailable</AlertTitle>
              <AlertDescription>
                {status.reason || status.message}
              </AlertDescription>
            </Alert>
          )}
          {status.recovery?.rebootRequired && (
            <Alert>
              <RotateCcw />
              <AlertTitle>Reboot required after applying updates</AlertTitle>
              <AlertDescription>
                {status.recovery.reason ||
                  status.recovery.hints[0] ||
                  "Restart the system when the update is complete."}
                {status.recovery.rebootPackages &&
                  status.recovery.rebootPackages.length > 0 && (
                    <span className="mt-1 block">
                      Affected packages:{" "}
                      {status.recovery.rebootPackages.join(", ")}
                    </span>
                  )}
              </AlertDescription>
            </Alert>
          )}
          {status.recovery?.restartServices &&
            status.recovery.restartServices.length > 0 && (
              <Alert>
                <ServerCog />
                <AlertTitle>Services may need a restart</AlertTitle>
                <AlertDescription>
                  {status.recovery.restartServices.join(", ")}
                </AlertDescription>
              </Alert>
            )}
          {status.recovery?.manualPackages &&
            status.recovery.manualPackages.length > 0 && (
              <Alert>
                <TriangleAlert />
                <AlertTitle>Manual recovery may be required</AlertTitle>
                <AlertDescription>
                  {status.recovery.manualPackages.join(", ")}
                </AlertDescription>
              </Alert>
            )}
          {status.recovery &&
            !status.recovery.rebootRequired &&
            (status.recovery.hints.length > 0 || status.recovery.reason) && (
              <Alert>
                <TriangleAlert />
                <AlertTitle>Recovery guidance</AlertTitle>
                <AlertDescription>
                  {status.recovery.reason && <p>{status.recovery.reason}</p>}
                  {status.recovery.hints.length > 0 && (
                    <ul className="flex list-disc flex-col gap-1 pl-4">
                      {status.recovery.hints.map((hint) => (
                        <li key={hint}>{hint}</li>
                      ))}
                    </ul>
                  )}
                </AlertDescription>
              </Alert>
            )}

          <Separator />
          <div className="flex flex-wrap gap-x-5 gap-y-2 text-xs text-muted-foreground">
            <span>Backend: {status.backend}</span>
            <span>Contract: {status.contract}</span>
            {status.version && <span>Version: {status.version}</span>}
            {status.lastChecked && (
              <span>Last checked: {formatLastChecked(status.lastChecked)}</span>
            )}
          </div>

          <KpatchSettingsCard
            csrfToken={session.data?.csrfToken ?? ""}
            administrative={session.data?.administrative ?? false}
          />
        </CardContent>

        <CardFooter className="flex flex-col items-stretch gap-3 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-xs text-muted-foreground">
            Preview uses the current inventory fingerprint and preserves the
            safety checks.
          </p>
          <Button
            className="w-full sm:w-auto"
            disabled={!canUpdate}
            onClick={requestPreview}
          >
            {previewRequest.isPending && (
              <RefreshCw data-icon="inline-start" className="animate-spin" />
            )}
            {previewRequest.isPending
              ? "Loading preview…"
              : !status.available
                ? "Preview unavailable"
                : packageCount > 0
                  ? "Preview full update"
                  : "No updates available"}
          </Button>
        </CardFooter>
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

      <Card>
        <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle>Available updates</CardTitle>
            <CardDescription>
              {!status.available
                ? "The update provider did not return a usable inventory."
                : packageCount > 0
                  ? "Grouped by advisory so related packages are easy to review."
                  : "Your installed software is current."}
            </CardDescription>
          </div>
          <Badge variant={packageCount > 0 ? "secondary" : "outline"}>
            {packageCount} package{packageCount === 1 ? "" : "s"}
          </Badge>
        </CardHeader>
        <CardContent>
          {packageCount === 0 ? (
            <Empty className="min-h-56 border">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  {status.available ? (
                    <Check aria-hidden="true" />
                  ) : (
                    <TriangleAlert aria-hidden="true" />
                  )}
                </EmptyMedia>
                <EmptyTitle>
                  {status.available
                    ? "No updates available"
                    : "Inventory unavailable"}
                </EmptyTitle>
                <EmptyDescription>
                  {status.available
                    ? "Check again whenever you want to verify the package inventory."
                    : status.reason || status.message}
                </EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button
                  variant="outline"
                  disabled={refresh.isPending || activityActive}
                  onClick={() => refresh.mutate()}
                >
                  <RefreshCw
                    data-icon="inline-start"
                    className={refresh.isPending ? "animate-spin" : ""}
                  />
                  Check again
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <UpdatePackageTable packages={status.packages} />
          )}
        </CardContent>
      </Card>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-h-[min(90vh,48rem)] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>
              {preview?.allowed
                ? "Confirm full-system update"
                : preview
                  ? "Update cannot start"
                  : "Preparing update preview"}
            </DialogTitle>
            <DialogDescription>
              {preview?.reason ||
                (preview
                  ? "Review the planned package changes before applying them."
                  : "Checking the current inventory and calculating an exact plan.")}
            </DialogDescription>
          </DialogHeader>

          {previewRequest.isPending && !preview && (
            <div className="flex flex-col gap-3" role="status">
              <Progress value={null} aria-label="Preparing update preview" />
              <p className="text-sm text-muted-foreground">
                Preparing a fingerprint-bound preview…
              </p>
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
            <div className="flex flex-col gap-4">
              <div className="flex flex-wrap gap-2">
                <Badge variant="outline">
                  {preview.changes.length} planned change
                  {preview.changes.length === 1 ? "" : "s"}
                </Badge>
                {preview.warnings.length > 0 && (
                  <Badge variant="secondary">
                    {preview.warnings.length} warning
                    {preview.warnings.length === 1 ? "" : "s"}
                  </Badge>
                )}
                <Badge
                  variant={
                    preview.allowed && !preview.stale
                      ? "secondary"
                      : "destructive"
                  }
                >
                  {preview.stale
                    ? "Stale preview"
                    : preview.allowed
                      ? "Ready to review"
                      : "Blocked"}
                </Badge>
              </div>

              {(preview.stale || !preview.allowed) && (
                <Alert variant="destructive">
                  <TriangleAlert />
                  <AlertTitle>
                    {preview.stale ? "Inventory changed" : "Update is blocked"}
                  </AlertTitle>
                  <AlertDescription>
                    {preview.stale
                      ? "Refresh the inventory and create a new preview before applying changes."
                      : preview.reason ||
                        "The update backend did not authorize this plan."}
                  </AlertDescription>
                </Alert>
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

              <ScrollArea className="h-64 rounded-lg border">
                <div className="flex flex-col gap-2 p-3">
                  {preview.changes.length > 0 ? (
                    preview.changes.map((change) => (
                      <div
                        key={
                          change.action +
                          ":" +
                          change.name +
                          ":" +
                          (change.architecture ?? "")
                        }
                        className="flex flex-col gap-1 rounded-lg border bg-muted/20 p-3 text-sm sm:flex-row sm:items-center sm:justify-between"
                      >
                        <div className="min-w-0">
                          <p className="break-words font-medium">
                            {change.name}
                            {change.architecture
                              ? ` (${change.architecture})`
                              : ""}
                          </p>
                          <p className="text-xs capitalize text-muted-foreground">
                            {change.action}
                          </p>
                        </div>
                        <p className="break-all font-mono text-xs text-muted-foreground sm:text-right">
                          {change.currentVersion || "Not installed"}
                          {change.candidateVersion
                            ? ` → ${change.candidateVersion}`
                            : ""}
                        </p>
                      </div>
                    ))
                  ) : (
                    <p className="p-3 text-sm text-muted-foreground">
                      No package changes are included in this preview.
                    </p>
                  )}
                </div>
              </ScrollArea>

              {preview.requiresRiskConfirmation && (
                <FieldGroup>
                  <Field
                    data-invalid={!riskAccepted}
                    className="rounded-lg border p-3"
                  >
                    <FieldLabel>
                      <Checkbox
                        checked={riskAccepted}
                        onCheckedChange={(checked) =>
                          setRiskAccepted(checked === true)
                        }
                        aria-invalid={!riskAccepted}
                      />
                      <span>
                        I accept removals, downgrades, replacements, repository
                        changes, or vendor changes shown above.
                      </span>
                    </FieldLabel>
                    <FieldDescription>
                      This confirmation is required before the exact
                      fingerprint-bound plan can start.
                    </FieldDescription>
                  </Field>
                </FieldGroup>
              )}

              {session.data && !session.data.administrative && (
                <Alert variant="destructive">
                  <ShieldAlert />
                  <AlertTitle>Administrative access required</AlertTitle>
                  <AlertDescription>
                    Gain Administrative access before applying system changes.
                  </AlertDescription>
                </Alert>
              )}
            </div>
          )}

          <DialogFooter className="flex-col-reverse sm:flex-row">
            {previewRequest.isError && !preview && (
              <Button
                variant="outline"
                onClick={requestPreview}
                disabled={previewRequest.isPending}
              >
                Retry preview
              </Button>
            )}
            <Button variant="outline" onClick={() => setDialogOpen(false)}>
              Cancel
            </Button>
            <Button
              disabled={
                !preview?.allowed ||
                preview?.stale ||
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
              {apply.isPending ? "Starting update…" : "Apply full update"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <UpdateHistoryCard enabled />
    </div>
  )
}
