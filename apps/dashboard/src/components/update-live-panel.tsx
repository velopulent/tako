import { Ban, ChevronDown, Terminal } from "lucide-react"
import { useEffect, useRef, useState } from "react"

import { Badge } from "@/components/ui/badge"
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
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { Progress } from "@/components/ui/progress"
import { Spinner } from "@/components/ui/spinner"
import { activeUpdateJobStates } from "@/components/update-job-state"
import type { DiagnosticJob, UpdateObservation } from "@/lib/api"

function activityLabel(
  progress: UpdateObservation["progress"],
  job?: DiagnosticJob
) {
  if (job?.state === "pending") return "Queued"
  if (progress.phase === "refreshing") return "Refreshing"
  if (job?.state === "running") return "Installing"
  if (progress.phase === "idle") return "Working"
  return progress.phase.charAt(0).toUpperCase() + progress.phase.slice(1)
}

function LiveOutput({ lines }: { lines: UpdateObservation["output"] }) {
  const viewport = useRef<HTMLDivElement>(null)
  const pinned = useRef(true)

  // Follow new output unless the reader has scrolled up to inspect it.
  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll when the line count changes
  useEffect(() => {
    const node = viewport.current
    if (node && pinned.current) node.scrollTop = node.scrollHeight
  }, [lines.length])

  return (
    <div
      ref={viewport}
      role="log"
      aria-label="Package manager output"
      // biome-ignore lint/a11y/noNoninteractiveTabindex: scrollable log must be reachable by keyboard
      tabIndex={0}
      onScroll={(event) => {
        const node = event.currentTarget
        pinned.current =
          node.scrollHeight - node.scrollTop - node.clientHeight < 24
      }}
      className="max-h-72 overflow-y-auto overscroll-contain rounded-lg bg-terminal p-3 font-mono text-xs leading-5 text-terminal-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      {lines.map((entry) => (
        <div
          key={
            entry.sequence > 0
              ? entry.sequence
              : `${entry.timestamp}:${entry.stream}:${entry.line}`
          }
          className={
            entry.stream === "stderr"
              ? "break-all whitespace-pre-wrap text-terminal-muted"
              : "break-all whitespace-pre-wrap"
          }
        >
          {entry.line || " "}
        </div>
      ))}
    </div>
  )
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
    progress.message || job?.message || "Update activity in progress"
  const committing =
    job?.state === "running" &&
    !progress.cancelable &&
    !(job.kind === "software-update" && job.progress < 20)

  return (
    <Card aria-live="polite" aria-atomic="false" className="ring-primary/20">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Spinner aria-hidden="true" role="presentation" />
          Update activity
          <Badge variant="secondary">{activityLabel(progress, job)}</Badge>
        </CardTitle>
        <CardDescription className="truncate" title={message}>
          {message}
          {progress.package ? `: ${progress.package}` : ""}
        </CardDescription>
        {canCancel && (
          <CardAction>
            <Button
              variant="outline"
              size="sm"
              disabled={canceling}
              onClick={onCancel}
            >
              {canceling ? (
                <Spinner data-icon="inline-start" aria-hidden="true" />
              ) : (
                <Ban data-icon="inline-start" />
              )}
              {canceling ? "Canceling…" : "Cancel update"}
            </Button>
          </CardAction>
        )}
      </CardHeader>

      <CardContent>
        <Collapsible
          open={outputOpen}
          onOpenChange={setOutputOpen}
          className="flex flex-col gap-3"
        >
          <div className="flex items-center gap-3">
            <Progress
              value={percent}
              className="min-w-0 flex-1"
              aria-label={`Update progress${percent === null ? "" : `: ${percent}%`}`}
            />
            <span className="w-12 shrink-0 text-right text-sm text-muted-foreground tabular-nums">
              {percent === null ? "—" : `${percent}%`}
            </span>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
            <p className="text-xs text-muted-foreground">
              {progress.total > 0 && (
                <span className="tabular-nums">
                  Step {progress.current} of {progress.total}.{" "}
                </span>
              )}
              {committing &&
                "Changes are being written. Cancel is unavailable until the transaction finishes."}
            </p>
            {outputCount > 0 && (
              <CollapsibleTrigger render={<Button variant="ghost" size="sm" />}>
                <Terminal data-icon="inline-start" />
                {outputOpen ? "Hide live output" : "View live output"}
                <ChevronDown
                  data-icon="inline-end"
                  aria-hidden="true"
                  className="transition-transform duration-200 group-aria-expanded/button:rotate-180 motion-reduce:transition-none"
                />
              </CollapsibleTrigger>
            )}
          </div>

          {outputCount > 0 && (
            <CollapsibleContent>
              <LiveOutput lines={observation.output} />
            </CollapsibleContent>
          )}
        </Collapsible>
      </CardContent>
    </Card>
  )
}
