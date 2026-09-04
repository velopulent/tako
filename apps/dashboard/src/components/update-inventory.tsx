import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Check, RefreshCw, TriangleAlert } from "lucide-react"
import * as React from "react"

import { KpatchSettingsCard } from "@/components/kpatch-settings-card"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { UpdateHistoryCard } from "@/components/update-history-card"
import { UpdateJobProgress } from "@/components/update-job-progress"
import { activeUpdateJobStates } from "@/components/update-job-state"
import { UpdateLivePanel } from "@/components/update-live-panel"
import { UpdatePackageTable } from "@/components/update-package-table"
import {
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

function formatLastChecked(value?: string) {
  if (!value) return ""
  return new Date(value).toLocaleString()
}

export function UpdateInventory() {
  const queryClient = useQueryClient()
  const [jobID, setJobID] = React.useState(readSavedJob)
  const [preview, setPreview] = React.useState<UpdatePreview | null>(null)
  const [dialogOpen, setDialogOpen] = React.useState(false)
  const [riskAccepted, setRiskAccepted] = React.useState(false)
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

  React.useEffect(() => {
    const source = new EventSource("/api/v1/updates/live")
    const onProgress = (event: MessageEvent<string>) => {
      const progress = JSON.parse(event.data) as UpdateProgress
      setObservation((current) => ({
        progress,
        output:
          progress.jobId && progress.jobId !== current.progress.jobId
            ? []
            : current.output,
      }))
    }
    const onOutput = (event: MessageEvent<string>) => {
      const output = JSON.parse(event.data) as UpdateOutput
      setObservation((current) => ({
        ...current,
        output: [...current.output, output].slice(-500),
      }))
    }
    source.addEventListener("progress", onProgress as EventListener)
    source.addEventListener("output", onOutput as EventListener)
    return () => source.close()
  }, [])

  const refresh = useMutation({
    mutationFn: () =>
      api<UpdateStatus>("/updates/refresh", {
        method: "POST",
        body: JSON.stringify({ force: true }),
      }),
    onSuccess: (status) => queryClient.setQueryData(["updates"], status),
  })
  const previewRequest = useMutation({
    mutationFn: (operation: UpdateOperation) =>
      api<UpdatePreview>("/updates/preview", {
        method: "POST",
        body: JSON.stringify(operation),
      }),
    onSuccess: (value) => {
      setPreview(value)
      setRiskAccepted(false)
      setDialogOpen(true)
    },
  })
  const apply = useMutation({
    mutationFn: (operation: UpdateOperation) =>
      api<{ job: DiagnosticJob }>("/updates", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(operation),
      }),
    onSuccess: ({ job: value }) => {
      setJobID(value.id)
      setDialogOpen(false)
      try {
        sessionStorage.setItem(updateJobStorageKey, value.id)
      } catch {
        // Storage may be disabled.
      }
    },
  })
  const cancel = useMutation({
    mutationFn: () =>
      api<{ job: DiagnosticJob }>(`/jobs/${jobID}/cancel`, {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
      }),
  })

  if (query.isPending) return <Skeleton className="h-72" />
  if (query.isError || !query.data) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Update inventory unavailable</AlertTitle>
        <AlertDescription>{query.error?.message}</AlertDescription>
      </Alert>
    )
  }

  const status = query.data
  const activeJob = job.data?.job
  const error =
    refresh.error || previewRequest.error || apply.error || cancel.error
  const canUpdate =
    status.available && !status.externalLock && status.packages.length > 0

  return (
    <div className="flex flex-col gap-6">
      {status.externalLock && (
        <Alert variant="destructive">
          <TriangleAlert className="size-4" />
          <AlertTitle>Package manager busy</AlertTitle>
          <AlertDescription>
            {status.lockReason ||
              "Another package-manager operation is active."}
          </AlertDescription>
        </Alert>
      )}
      {error && (
        <Alert variant="destructive">
          <AlertTitle>Update action failed</AlertTitle>
          <AlertDescription>{error.message}</AlertDescription>
        </Alert>
      )}
      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Status</CardTitle>
            <CardAction>
              <Button
                variant="outline"
                size="icon-sm"
                aria-label="Check for updates"
                disabled={refresh.isPending}
                onClick={() => refresh.mutate()}
              >
                <RefreshCw
                  className={refresh.isPending ? "animate-spin" : ""}
                />
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            {status.packages.length === 0 && status.available ? (
              <p className="flex items-center gap-2">
                <Check className="size-4 text-green-600" />
                System is up to date
              </p>
            ) : (
              <p>{status.message}</p>
            )}
            <p className="text-xs text-muted-foreground">
              {status.backend} · {status.contract}
              {status.lastChecked
                ? ` · ${formatLastChecked(status.lastChecked)}`
                : ""}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Settings</CardTitle>
          </CardHeader>
          <CardContent>
            <KpatchSettingsCard
              csrfToken={session.data?.csrfToken ?? ""}
              administrative={session.data?.administrative ?? false}
            />
          </CardContent>
        </Card>
      </div>

      <UpdateLivePanel
        observation={observation}
        canceling={cancel.isPending}
        onCancel={jobID ? () => cancel.mutate() : undefined}
      />
      {activeJob && (
        <UpdateJobProgress
          job={activeJob}
          observation={observation}
          canceling={cancel.isPending}
          onCancel={() => cancel.mutate()}
        />
      )}

      <Card>
        <CardHeader>
          <CardTitle>Available updates</CardTitle>
          <CardDescription>
            Full-system update using manager-default dependency resolution.
          </CardDescription>
          <CardAction>
            <Button
              disabled={!canUpdate || previewRequest.isPending}
              onClick={() =>
                previewRequest.mutate({
                  expectedFingerprint: status.fingerprint,
                  confirmed: false,
                })
              }
            >
              Preview full update
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          <UpdatePackageTable packages={status.packages} />
        </CardContent>
      </Card>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {preview?.allowed
                ? "Confirm full-system update"
                : "Update cannot start"}
            </DialogTitle>
            <DialogDescription>
              {preview?.reason ||
                `${preview?.changes.length ?? 0} planned changes`}
            </DialogDescription>
          </DialogHeader>
          <div className="max-h-64 overflow-auto text-sm">
            {preview?.changes.map((change) => (
              <p key={`${change.action}:${change.name}`}>
                {change.action}: {change.name} {change.currentVersion || ""}{" "}
                {change.candidateVersion ? `→ ${change.candidateVersion}` : ""}
              </p>
            ))}
          </div>
          {preview?.requiresRiskConfirmation && (
            <label className="flex gap-2 text-sm">
              <input
                type="checkbox"
                checked={riskAccepted}
                onChange={(event) => setRiskAccepted(event.target.checked)}
              />
              I accept removals, downgrades, replacements, repository changes,
              or vendor changes shown above.
            </label>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>
              Cancel
            </Button>
            <Button
              disabled={
                !preview?.allowed ||
                (preview.requiresRiskConfirmation && !riskAccepted) ||
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
              Apply full update
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <UpdateHistoryCard enabled />
    </div>
  )
}
