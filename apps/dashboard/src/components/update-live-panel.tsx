import { Ban } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { Progress } from "@/components/ui/progress"
import type { UpdateObservation } from "@/lib/api"

export function UpdateLivePanel({
  observation,
  canceling,
  onCancel,
}: {
  observation: UpdateObservation
  canceling?: boolean
  onCancel?: () => void
}) {
  const progress = observation.progress
  const percent =
    progress.percent >= 0 && progress.percent <= 100 ? progress.percent : null

  if (!progress.active && observation.output.length === 0) return null

  return (
    <div className="space-y-2 rounded-md border p-3" aria-live="polite">
      <div className="flex items-center justify-between gap-2 text-sm">
        <span className="font-medium">
          {progress.message || progress.phase}
          {progress.package ? `: ${progress.package}` : ""}
        </span>
        <span className="text-muted-foreground tabular-nums">
          {percent === null ? "Working…" : `${percent}%`}
        </span>
      </div>
      <Progress
        value={percent}
        aria-label={`Update progress${percent === null ? "" : `: ${percent}%`}`}
      />
      {progress.cancelable && onCancel && (
        <Button
          variant="outline"
          size="sm"
          disabled={canceling}
          onClick={onCancel}
        >
          <Ban data-icon="inline-start" />
          {canceling ? "Canceling…" : "Cancel update"}
        </Button>
      )}
      {observation.output.length > 0 && (
        <Collapsible>
          <CollapsibleTrigger render={<Button variant="ghost" size="sm" />}>
            Live output ({observation.output.length})
          </CollapsibleTrigger>
          <CollapsibleContent>
            <pre className="max-h-48 overflow-auto rounded-md border bg-muted/20 p-2 text-xs whitespace-pre-wrap">
              {observation.output
                .map((entry) => `${entry.stream}: ${entry.line}`)
                .join("\n")}
            </pre>
          </CollapsibleContent>
        </Collapsible>
      )}
    </div>
  )
}
