import { Ban, ChevronDown, LoaderCircle, Terminal } from "lucide-react"
import { useEffect, useState } from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { Progress } from "@/components/ui/progress"
import { ScrollArea } from "@/components/ui/scroll-area"
import { activeUpdateJobStates } from "@/components/update-job-state"
import type { DiagnosticJob, UpdateObservation } from "@/lib/api"
import { cn } from "@/lib/utils"

function activityLabel(
  progress: UpdateObservation["progress"],
  job?: DiagnosticJob
) {
  if (job?.state === "pending") return "Queued"
  if (progress.phase === "refreshing") return "Refreshing"
  if (job?.state === "running") return "Running"
  if (progress.phase === "idle") return "Working"
  return progress.phase
}

export function UpdateLivePanel({
  observation,
  job,
  canceling,
  onCancel,
}: {
  observation: UpdateObservation
  job?: DiagnosticJob
  canceling?: boolean
  onCancel?: () => void
}) {
  const [outputOpen, setOutputOpen] = useState(false)
  const progress = observation.progress
  const jobIsActive = Boolean(job && activeUpdateJobStates.has(job.state))
  const isActive = progress.active || jobIsActive
  const percentFromProgress =
    progress.percent >= 0 && progress.percent <= 100 ? progress.percent : null
  const percentFromJob =
    job && job.progress >= 0 && job.progress <= 100 ? job.progress : null
  const percent = percentFromProgress ?? percentFromJob
  const canCancel = Boolean(
    onCancel &&
      job &&
      activeUpdateJobStates.has(job.state) &&
      (job.state === "pending" ||
        progress.cancelable ||
        (job.state === "running" &&
          job.kind === "software-update" &&
          job.progress < 20))
  )
  const outputCount = observation.output.length
  const jobKey = progress.jobId || job?.id || ""

  // The activity key is intentionally the reset boundary for the disclosure state.
  // biome-ignore lint/correctness/useExhaustiveDependencies: reset output disclosure when the job changes
  useEffect(() => {
    setOutputOpen(false)
  }, [jobKey])

  if (!isActive) return null

  const message =
    progress.message ||
    job?.message ||
    (progress.package ? progress.package : "Update activity in progress")

  return (
    <Card
      className="border-primary/30 bg-primary/5"
      aria-live="polite"
      aria-atomic="false"
    >
      <CardHeader className="flex flex-col gap-3 pb-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex min-w-0 items-start gap-3">
          <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <LoaderCircle className="animate-spin" aria-hidden="true" />
          </div>
          <div className="min-w-0">
            <CardTitle className="text-base">Update activity</CardTitle>
            <p
              className="mt-1 truncate text-sm text-muted-foreground"
              title={message}
            >
              {message}
              {progress.package ? `: ${progress.package}` : ""}
            </p>
          </div>
        </div>
        <Badge variant="secondary">{activityLabel(progress, job)}</Badge>
      </CardHeader>

      <CardContent className="flex flex-col gap-4">
        <div className="flex items-center gap-3">
          <Progress
            value={percent}
            className="min-w-0 flex-1"
            aria-label={`Update progress${percent === null ? "" : `: ${percent}%`}`}
          />
          <span className="shrink-0 text-sm text-muted-foreground tabular-nums">
            {percent === null ? "Working…" : `${percent}%`}
          </span>
        </div>

        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-muted-foreground">
          {progress.total > 0 && (
            <span>
              {progress.current} of {progress.total} steps
            </span>
          )}
          {outputCount > 0 && (
            <span className="inline-flex items-center gap-1.5">
              <Terminal aria-hidden="true" />
              {outputCount} output lines
            </span>
          )}
          {job?.state === "running" &&
            !progress.cancelable &&
            !(job.kind === "software-update" && job.progress < 20) && (
              <span>
                Cancellation is unavailable while the package manager is
                applying changes.
              </span>
            )}
        </div>

        {outputCount > 0 && (
          <Collapsible open={outputOpen} onOpenChange={setOutputOpen}>
            <CollapsibleTrigger
              render={
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full justify-between sm:w-auto"
                />
              }
            >
              <span className="inline-flex items-center gap-2">
                <Terminal data-icon="inline-start" />
                View live output
                <Badge variant="secondary">{outputCount}</Badge>
              </span>
              <ChevronDown
                aria-hidden="true"
                className={cn(
                  "transition-transform",
                  outputOpen && "rotate-180"
                )}
              />
            </CollapsibleTrigger>
            <CollapsibleContent className="pt-3">
              <ScrollArea className="h-52 rounded-lg border bg-background">
                <pre className="p-3 font-mono text-xs leading-5 whitespace-pre-wrap">
                  {observation.output
                    .map((entry) => `${entry.stream}: ${entry.line}`)
                    .join("\n")}
                </pre>
              </ScrollArea>
            </CollapsibleContent>
          </Collapsible>
        )}
      </CardContent>

      {canCancel && (
        <CardFooter className="border-0 bg-transparent pt-0">
          <Button
            variant="outline"
            size="sm"
            className="w-full sm:w-auto"
            disabled={canceling}
            onClick={onCancel}
          >
            <Ban data-icon="inline-start" />
            {canceling ? "Canceling…" : "Cancel update"}
          </Button>
        </CardFooter>
      )}
    </Card>
  )
}
