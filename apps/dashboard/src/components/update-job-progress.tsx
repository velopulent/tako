import { Ban } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { activeUpdateJobStates } from "@/components/update-job-state"
import { UpdateLivePanel } from "@/components/update-live-panel"
import type { DiagnosticJob, UpdateObservation } from "@/lib/api"

export function UpdateJobProgress({
  job,
  observation,
  canceling,
  onCancel,
}: {
  job: DiagnosticJob
  observation?: UpdateObservation
  canceling: boolean
  onCancel: () => void
}) {
  const live = observation?.live
  const percent =
    live?.active && live.percentage >= 0 && live.percentage <= 100
      ? live.percentage
      : job.progress

  return (
    <div className="space-y-2 rounded-md border p-3" aria-live="polite">
      <div className="flex items-center justify-between gap-2 text-sm">
        <span className="font-medium">Update job: {job.state}</span>
        <span className="text-muted-foreground tabular-nums">{percent}%</span>
      </div>
      <Progress
        value={percent}
        aria-label={`Update job progress: ${percent}%`}
      />
      {activeUpdateJobStates.has(job.state) ? (
        <>
          {live?.active ? (
            <UpdateLivePanel
              observation={observation ?? { live, log: [] }}
              canceling={canceling}
              onCancel={onCancel}
            />
          ) : (
            <p className="text-sm text-muted-foreground">{job.message}</p>
          )}
          {(!live?.active || !live.allowCancel) && (
            <Button
              variant="outline"
              size="sm"
              disabled={canceling}
              onClick={onCancel}
            >
              <Ban data-icon="inline-start" />
              {canceling ? "Canceling…" : "Cancel update job"}
            </Button>
          )}
        </>
      ) : job.error ? (
        <p className="text-sm text-destructive">{job.error}</p>
      ) : (
        <p className="text-sm text-muted-foreground">{job.message}</p>
      )}
    </div>
  )
}
