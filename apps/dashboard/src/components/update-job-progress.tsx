import { XCircleIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { activeUpdateJobStates } from "@/components/update-job-state"
import type { DiagnosticJob } from "@/lib/api"

export function UpdateJobProgress({
  job,
  canceling,
  onCancel,
}: {
  job: DiagnosticJob
  canceling: boolean
  onCancel: () => void
}) {
  return (
    <div className="space-y-2 rounded-md border p-3" aria-live="polite">
      <div className="flex items-center justify-between gap-2 text-sm">
        <span className="font-medium">Update job: {job.state}</span>
        <span className="text-muted-foreground tabular-nums">
          {job.progress}%
        </span>
      </div>
      <Progress
        value={job.progress}
        aria-label={`Update job progress: ${job.progress}%`}
      />
      <p className="text-sm text-muted-foreground">{job.message}</p>
      {job.error && <p className="text-sm text-destructive">{job.error}</p>}
      {activeUpdateJobStates.has(job.state) && (
        <Button
          variant="outline"
          size="sm"
          disabled={canceling}
          onClick={onCancel}
        >
          <XCircleIcon data-icon="inline-start" />
          {canceling ? "Canceling…" : "Cancel update job"}
        </Button>
      )}
    </div>
  )
}
