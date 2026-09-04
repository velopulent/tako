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
  const progress = observation?.progress
  const percent =
    progress?.active && progress.percent >= 0 && progress.percent <= 100
      ? progress.percent
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
          {progress?.active ? (
            <UpdateLivePanel
              observation={observation ?? { progress, output: [] }}
              canceling={canceling}
              onCancel={onCancel}
            />
          ) : (
            <p className="text-sm text-muted-foreground">{job.message}</p>
          )}
          {!progress?.active && (
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
