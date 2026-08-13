import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { UpdateJobProgress } from "@/components/update-job-progress"
import { activeUpdateJobStates } from "@/components/update-job-state"
import { UpdateOperationControls } from "@/components/update-operation-controls"
import { UpdatePackageTable } from "@/components/update-package-table"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
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
  EmptyTitle,
} from "@/components/ui/empty"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import {
  api,
  type DiagnosticJob,
  type SessionResponse,
  type UpdateOperation,
  type UpdatePreview,
  type UpdateStatus,
} from "@/lib/api"

const updateJobStorageKey = "tako-update-job:v1"

function readSavedJob() {
  try {
    return sessionStorage.getItem(updateJobStorageKey) ?? ""
  } catch {
    return ""
  }
}

export function UpdateInventory() {
  const queryClient = useQueryClient()
  const [scope, setScope] = React.useState<UpdateOperation["scope"]>("all")
  const [selected, setSelected] = React.useState<string[]>([])
  const [preview, setPreview] = React.useState<UpdatePreview>()
  const [confirmation, setConfirmation] = React.useState("")
  const [jobID, setJobID] = React.useState(readSavedJob)
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
      const state = current?.state?.data?.job?.state
      return state && activeUpdateJobStates.has(state) ? 2000 : false
    },
  })
  const previewRequest = useMutation({
    mutationFn: (operation: UpdateOperation) =>
      api<UpdatePreview>("/updates/preview", {
        method: "POST",
        body: JSON.stringify(operation),
      }),
    onSuccess: (value) => {
      setPreview(value)
      setConfirmation("")
    },
  })
  const applyRequest = useMutation({
    mutationFn: () =>
      api<{ job: DiagnosticJob }>("/updates", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify({
          scope,
          packages: scope === "selected" ? selected : undefined,
          expectedFingerprint: query.data?.fingerprint,
          confirmation,
        } satisfies UpdateOperation),
      }),
    onSuccess: (value) => {
      setJobID(value.job.id)
      setPreview(undefined)
      setConfirmation("")
      queryClient.invalidateQueries({ queryKey: ["updates"] })
      try {
        sessionStorage.setItem(updateJobStorageKey, value.job.id)
      } catch {
        // Reconnect still works while this page remains mounted.
      }
    },
  })
  const cancelRequest = useMutation({
    mutationFn: () =>
      api<{ job: DiagnosticJob }>(`/jobs/${jobID}/cancel`, {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
      }),
  })

  const currentJobState = job.data?.job?.state
  React.useEffect(() => {
    if (!currentJobState || activeUpdateJobStates.has(currentJobState)) return
    try {
      sessionStorage.removeItem(updateJobStorageKey)
    } catch {
      // Ignore storage restrictions.
    }
  }, [currentJobState])

  if (query.isPending) return <Skeleton className="h-72" />
  if (query.isError) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Update inventory unavailable</AlertTitle>
        <AlertDescription>{query.error.message}</AlertDescription>
      </Alert>
    )
  }
  if (!query.data) return null
  const status = query.data
  const activeJob = job.data?.job
  const selectedPackages = status.packages.filter((item) =>
    selected.includes(item.name)
  )
  const actionError =
    previewRequest.error || applyRequest.error || cancelRequest.error

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <CardTitle>{status.backend || "Software updates"}</CardTitle>
            <CardDescription>{status.message}</CardDescription>
          </div>
          <Badge variant={status.available ? "secondary" : "outline"}>
            {status.available ? "Inventory ready" : "Unavailable"}
          </Badge>
        </div>
        {status.version && (
          <p className="text-xs text-muted-foreground">
            Backend {status.version} · contract {status.contract}
          </p>
        )}
        {status.externalLock && (
          <Alert variant="destructive">
            <AlertTitle>Another package tool holds a lock</AlertTitle>
            <AlertDescription>
              {status.lockReason ||
                "Updates are paused until the other operation finishes."}
            </AlertDescription>
          </Alert>
        )}
        {!status.available && status.reason && (
          <Alert>
            <AlertTitle>Updates are unavailable</AlertTitle>
            <AlertDescription>{status.reason}</AlertDescription>
          </Alert>
        )}
        {actionError && (
          <Alert variant="destructive">
            <AlertTitle>Update action failed</AlertTitle>
            <AlertDescription>{actionError.message}</AlertDescription>
          </Alert>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        {status.packages.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>No installed-software updates</EmptyTitle>
              <EmptyDescription>
                The selected backend reported no available updates.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <>
            <UpdateOperationControls
              status={status}
              scope={scope}
              selected={selected}
              preview={preview}
              confirmation={confirmation}
              previewing={previewRequest.isPending}
              applying={applyRequest.isPending}
              sessionReady={Boolean(session.data?.csrfToken)}
              onScopeChange={(next) => {
                setScope(next)
                setPreview(undefined)
                setConfirmation("")
              }}
              onPreview={() =>
                previewRequest.mutate({
                  scope,
                  packages: scope === "selected" ? selected : undefined,
                  expectedFingerprint: status.fingerprint,
                })
              }
              onConfirmationChange={setConfirmation}
              onApply={() => applyRequest.mutate()}
            />
            {activeJob && (
              <UpdateJobProgress
                job={activeJob}
                canceling={cancelRequest.isPending}
                onCancel={() => cancelRequest.mutate()}
              />
            )}
            <UpdatePackageTable
              packages={status.packages}
              selected={selected}
              onToggle={(name) => {
                setPreview(undefined)
                setConfirmation("")
                setSelected((current) =>
                  current.includes(name)
                    ? current.filter((item) => item !== name)
                    : [...current, name]
                )
              }}
            />
            {scope === "selected" && selectedPackages.length === 0 && (
              <p className="text-sm text-muted-foreground">
                Select at least one package to preview selected updates.
              </p>
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}
