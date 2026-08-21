import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  Shield,
  Bug,
  Sparkles,
  RefreshCw,
  Check,
  RotateCcw,
  Settings,
  TriangleAlert,
} from "lucide-react"

import { UpdateJobProgress } from "@/components/update-job-progress"
import { activeUpdateJobStates } from "@/components/update-job-state"
import { UpdatePackageTable } from "@/components/update-package-table"
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
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
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

function formatLastChecked(iso?: string) {
  if (!iso) return ""
  const diff = Date.now() - new Date(iso).getTime()
  const seconds = Math.round(diff / 1000)
  if (seconds < 45) return "Last checked: just now"
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `Last checked: ${minutes} minute${minutes === 1 ? "" : "s"} ago`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `Last checked: ${hours} hour${hours === 1 ? "" : "s"} ago`
  const days = Math.round(hours / 24)
  return `Last checked: ${days} day${days === 1 ? "" : "s"} ago`
}

function getHighestSeverity(packages: UpdateStatus["packages"]) {
  if (packages.some((p) => p.severity === "security")) return "security"
  if (packages.some((p) => p.severity === "bugfix")) return "bugfix"
  return "enhancement"
}

function CountBadge({ severity }: { severity: string }) {
  if (severity === "security") return <Shield className="size-5 text-destructive" />
  if (severity === "bugfix") return <Bug className="size-5 text-amber-600" />
  return <Sparkles className="size-5 text-muted-foreground" />
}

export function UpdateInventory() {
  const queryClient = useQueryClient()
  const [scope, setScope] = React.useState<UpdateOperation["scope"]>("all")
  const [selected, setSelected] = React.useState<string[]>([])
  const [jobID, setJobID] = React.useState(readSavedJob)
  const [dialogOpen, setDialogOpen] = React.useState(false)
  const [pendingOperation, setPendingOperation] = React.useState<UpdateOperation | null>(null)
  const [pendingPreview, setPendingPreview] = React.useState<UpdatePreview | null>(null)

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
  })

  const applyRequest = useMutation({
    mutationFn: (operation: UpdateOperation) =>
      api<{ job: DiagnosticJob }>("/updates", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(operation),
      }),
    onSuccess: (value) => {
      setJobID(value.job.id)
      setDialogOpen(false)
      setPendingPreview(null)
      setPendingOperation(null)
      queryClient.invalidateQueries({ queryKey: ["updates"] })
      try {
        sessionStorage.setItem(updateJobStorageKey, value.job.id)
      } catch {
        // ignore
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
      // ignore
    }
  }, [currentJobState])

  const handleRefresh = () => {
    queryClient.invalidateQueries({ queryKey: ["updates"] })
    query.refetch()
  }

  const handleToggle = React.useCallback(
    (name: string) => {
      if (!query.data) return
      const pkg = query.data.packages.find((p) => p.name === name)
      const groupKey = pkg?.groupKey
      let groupNames: string[] = []
      if (groupKey) {
        groupNames = query.data.packages.filter((p) => p.groupKey === groupKey).map((p) => p.name)
      } else if (pkg?.dependencies?.length) {
        groupNames = [name, ...pkg.dependencies]
      } else {
        groupNames = [name]
      }
      setPendingPreview(null)
      setSelected((current) => {
        const hasAll = groupNames.every((n) => current.includes(n))
        if (hasAll) {
          return current.filter((n) => !groupNames.includes(n))
        }
        const next = new Set(current)
        groupNames.forEach((n) => next.add(n))
        return Array.from(next)
      })
    },
    [query.data]
  )

  const initiateInstall = (mode: "all" | "security" | "selected") => {
    if (!query.data) return
    let operation: UpdateOperation
    if (mode === "all") {
      operation = { scope: "all", expectedFingerprint: query.data.fingerprint }
    } else if (mode === "security") {
      const securityNames = query.data.packages
        .filter((p) => p.severity === "security")
        .map((p) => p.name)
      operation = { scope: "selected", packages: securityNames, expectedFingerprint: query.data.fingerprint }
    } else {
      // expand selected with dependencies already handled via toggle, but ensure deps included
      const expanded = new Set(selected)
      for (const name of selected) {
        const pkg = query.data.packages.find((p) => p.name === name)
        pkg?.dependencies?.forEach((dep) => expanded.add(dep))
        if (pkg?.groupKey) {
          query.data.packages
            .filter((p) => p.groupKey === pkg.groupKey)
            .forEach((p) => expanded.add(p.name))
        }
      }
      const packages = Array.from(expanded)
      operation = { scope: "selected", packages, expectedFingerprint: query.data.fingerprint }
    }
    setPendingOperation(operation)
    previewRequest.mutate(operation, {
      onSuccess: (preview) => {
        setPendingPreview(preview)
        setDialogOpen(true)
      },
    })
  }

  const handleConfirm = () => {
    if (!pendingOperation) return
    const operation: UpdateOperation = {
      ...pendingOperation,
      confirmation: "CONFIRM",
    }
    applyRequest.mutate(operation)
  }

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
  const actionError = previewRequest.error || applyRequest.error || cancelRequest.error

  const total = status.packages.length
  const securityCount = status.packages.filter((p) => p.severity === "security").length
  const bugfixCount = status.packages.filter((p) => p.severity === "bugfix").length
  const highestSeverity = getHighestSeverity(status.packages)
  const hasMixed = securityCount > 0 && securityCount < total
  const isLocked = status.externalLock
  const canInstall = status.available && !isLocked

  const selectedWithDeps = (() => {
    if (!status.packages.length) return []
    const expanded = new Set(selected)
    for (const name of selected) {
      const pkg = status.packages.find((p) => p.name === name)
      pkg?.dependencies?.forEach((dep) => expanded.add(dep))
      if (pkg?.groupKey) {
        status.packages.filter((p) => p.groupKey === pkg.groupKey).forEach((p) => expanded.add(p.name))
      }
    }
    return Array.from(expanded)
  })()

  return (
    <div className="flex flex-col gap-6">
      {/* Status + Settings grid like Cockpit */}
      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Status</CardTitle>
            <CardAction>
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button variant="outline" size="icon-sm" aria-label="Check for updates" onClick={handleRefresh} disabled={query.isFetching}>
                      <RefreshCw className={query.isFetching ? "animate-spin" : ""} />
                    </Button>
                  }
                />
                <TooltipContent>Check for updates</TooltipContent>
              </Tooltip>
            </CardAction>
          </CardHeader>
          <CardContent className="space-y-3">
            {total === 0 && status.available ? (
              <div className="flex gap-3">
                <Check className="size-5 text-green-600 mt-0.5 shrink-0" />
                <div>
                  <p className="text-sm font-medium">System is up to date</p>
                  {status.lastChecked && <p className="text-xs text-muted-foreground">{formatLastChecked(status.lastChecked)}</p>}
                </div>
              </div>
            ) : total > 0 ? (
              <div className="flex gap-3">
                <span className="mt-0.5 shrink-0">
                  <CountBadge severity={highestSeverity} />
                </span>
                <div className="space-y-1">
                  <p className="text-sm font-medium">
                    {securityCount === total && total > 0
                      ? `${securityCount} security fix${securityCount === 1 ? "" : "es"} available`
                      : hasMixed
                        ? `${total} updates available, including ${securityCount} security fix${securityCount === 1 ? "" : "es"}`
                        : `${total} update${total === 1 ? "" : "s"} available`}
                  </p>
                  {status.lastChecked && <p className="text-xs text-muted-foreground">{formatLastChecked(status.lastChecked)}</p>}
                  {bugfixCount > 0 && <p className="text-xs text-muted-foreground">{bugfixCount} bug fix{bugfixCount === 1 ? "" : "es"} · {total - securityCount - bugfixCount} enhancement{total - securityCount - bugfixCount === 1 ? "" : "s"}</p>}
                </div>
              </div>
            ) : null}

            {status.recovery?.rebootRequired && (
              <Alert className="py-2">
                <RotateCcw className="size-4" />
                <AlertTitle className="text-sm">Reboot required</AlertTitle>
                <AlertDescription className="text-xs">
                  {status.recovery.hints.join(" ") || status.recovery.reason || "Reboot the host after the update job completes."}
                </AlertDescription>
              </Alert>
            )}
            {!status.recovery?.rebootRequired && (status.recovery?.restartServices?.length ?? 0) > 0 && (
              <Alert className="py-2">
                <Settings className="size-4" />
                <AlertTitle className="text-sm">Restart services</AlertTitle>
                <AlertDescription className="text-xs">{status.recovery?.restartServices.join(", ")}</AlertDescription>
              </Alert>
            )}
            {status.recovery && !status.recovery?.authoritative && (
              <p className="text-xs text-muted-foreground">Advisory only; verify service state after applying updates.</p>
            )}
            {isLocked && (
              <Alert variant="destructive" className="py-2">
                <TriangleAlert className="size-4" />
                <AlertTitle className="text-sm">Another package tool holds a lock</AlertTitle>
                <AlertDescription className="text-xs">{status.lockReason || "Updates are paused until the other operation finishes."}</AlertDescription>
              </Alert>
            )}
            {!status.available && status.reason && (
              <Alert className="py-2">
                <AlertTitle className="text-sm">Updates are unavailable</AlertTitle>
                <AlertDescription className="text-xs">{status.reason}</AlertDescription>
              </Alert>
            )}
            {status.version && (
              <p className="text-xs text-muted-foreground">
                Backend {status.version} · contract {status.contract}
              </p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Settings</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm font-medium">Automatic updates</p>
                <p className="text-xs text-muted-foreground">Disabled</p>
              </div>
              <Button variant="outline" size="sm" disabled>
                Edit
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">Automatic update control will be available when the backend supports it.</p>
          </CardContent>
        </Card>
      </div>

      {actionError && (
        <Alert variant="destructive">
          <AlertTitle>Update action failed</AlertTitle>
          <AlertDescription>{actionError.message}</AlertDescription>
        </Alert>
      )}

      {activeJob && (
        <UpdateJobProgress job={activeJob} canceling={cancelRequest.isPending} onCancel={() => cancelRequest.mutate()} />
      )}

      <Card id="available-updates">
        <CardHeader>
          <CardTitle>Available updates</CardTitle>
          {total > 0 && <CardDescription>{total} package{total === 1 ? "" : "s"} with advisories grouped</CardDescription>}
          {total > 0 && (
            <CardAction className="flex flex-wrap gap-2">
              {scope === "all" && hasMixed && (
                <Button variant="outline" disabled={!canInstall || previewRequest.isPending} onClick={() => initiateInstall("security")}>
                  {previewRequest.isPending ? "Loading…" : "Install security updates"}
                </Button>
              )}
              {scope === "all" ? (
                <Button disabled={!canInstall || previewRequest.isPending} onClick={() => initiateInstall("all")}>
                  {securityCount === total ? "Install security updates" : "Install all updates"}
                </Button>
              ) : (
                <Button
                  disabled={!canInstall || selectedWithDeps.length === 0 || previewRequest.isPending}
                  onClick={() => initiateInstall("selected")}
                >
                  {previewRequest.isPending ? "Loading…" : `Install selected (${selectedWithDeps.length})`}
                </Button>
              )}
            </CardAction>
          )}
        </CardHeader>
        <CardContent className="space-y-4">
          {total === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyTitle>No installed-software updates</EmptyTitle>
                <EmptyDescription>The selected backend reported no available updates.</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-sm font-medium">Update scope</span>
                <div className="flex gap-2" role="group" aria-label="Update scope">
                  <Button
                    type="button"
                    size="sm"
                    variant={scope === "all" ? "default" : "outline"}
                    onClick={() => setScope("all")}
                  >
                    All packages
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant={scope === "selected" ? "default" : "outline"}
                    onClick={() => setScope("selected")}
                  >
                    Selected packages
                  </Button>
                </div>
                <span className="text-xs text-muted-foreground">Preview the current inventory before starting a serialized update job.</span>
              </div>

              <UpdatePackageTable packages={status.packages} selected={selectedWithDeps} onToggle={handleToggle} selectable={scope === "selected"} />

              {scope === "selected" && selectedWithDeps.length === 0 && (
                <p className="text-sm text-muted-foreground">Select at least one advisory to install selected updates. Dependent packages in the same advisory are auto-selected and cannot be toggled individually.</p>
              )}
              <p className="text-xs text-muted-foreground">
                {status.backend} · {status.contract} {status.lastChecked ? `· ${formatLastChecked(status.lastChecked)}` : ""}
              </p>
            </>
          )}
        </CardContent>
      </Card>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{pendingPreview?.allowed ? "Confirm updates" : "Update cannot start"}</DialogTitle>
            <DialogDescription>
              {pendingPreview?.allowed ? "Review the preview and confirm to start the update job." : "The preview indicates the operation cannot proceed."}
            </DialogDescription>
            <div className="space-y-2 pt-2 text-sm">
              {previewRequest.isPending ? (
                <p>Loading preview…</p>
              ) : pendingPreview ? (
                <>
                  <p>{pendingPreview.reason || pendingPreview.changes.join(", ") || `Update ${pendingPreview.selected.length} package${pendingPreview.selected.length === 1 ? "" : "s"}`}</p>
                  {pendingPreview.warnings.length > 0 && <p className="text-xs text-muted-foreground">{pendingPreview.warnings.join(" ")}</p>}
                  {pendingPreview.stale && <p className="text-destructive text-xs">Inventory changed; refresh before applying.</p>}
                  {pendingPreview.selected.length > 0 && (
                    <div className="rounded-md border bg-muted/20 p-2 max-h-40 overflow-auto text-xs">
                      {pendingPreview.selected.map((p) => (
                        <div key={p.name} className="flex justify-between gap-2">
                          <span className="font-medium">{p.name}</span>
                          <span className="text-muted-foreground truncate">{p.candidateVersion}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </>
              ) : null}
            </div>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)} disabled={applyRequest.isPending}>
              Cancel
            </Button>
            <Button
              disabled={!pendingPreview?.allowed || applyRequest.isPending || !session.data?.csrfToken}
              onClick={handleConfirm}
            >
              {applyRequest.isPending ? "Starting…" : "Confirm and install"}
            </Button>
          </DialogFooter>
          {!session.data?.csrfToken && <p className="text-xs text-muted-foreground text-right">Sign-in required to apply updates.</p>}
        </DialogContent>
      </Dialog>
    </div>
  )
}
