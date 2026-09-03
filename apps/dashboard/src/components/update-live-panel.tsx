import { Ban } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { Progress } from "@/components/ui/progress"
import type { UpdateObservation } from "@/lib/api"

function formatPackageId(packageId: string) {
  const fields = packageId.split(";")
  if (fields.length < 3 || !fields[0]) return packageId
  return `${fields[0]} ${fields[1]}${fields[2] ? ` (${fields[2]})` : ""}`
}

export function UpdateLivePanel({
  observation,
  canceling,
  onCancel,
}: {
  observation: UpdateObservation
  canceling?: boolean
  onCancel?: () => void
}) {
  const live = observation.live
  const log = observation.log ?? []
  const percent =
    live.percentage >= 0 && live.percentage <= 100 ? live.percentage : null
  const remaining = live.remainingSeconds
    ? ` · about ${Math.max(1, Math.round(live.remainingSeconds / 60))} min remaining`
    : ""

  if (!live.active) {
    return log.length > 0 ? <LogCollapsible log={log} /> : null
  }

  return (
    <div className="space-y-2 rounded-md border p-3" aria-live="polite">
      <div className="flex items-center justify-between gap-2 text-sm">
        <span className="font-medium">
          {live.status || "Updating"}
          {live.currentPackage ? `: ${live.currentPackage}` : ""}
        </span>
        <span className="text-muted-foreground tabular-nums">
          {percent === null ? "Working…" : `${percent}%`}
        </span>
      </div>
      <Progress
        value={percent}
        aria-label={`Update progress${percent === null ? "" : `: ${percent}%`}`}
      />
      <p className="text-xs text-muted-foreground">
        A package update is in progress{remaining}.
      </p>
      {live.allowCancel && onCancel && (
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
      {log.length > 0 && <LogCollapsible log={log} />}
    </div>
  )
}

export function ForeignUpdateAlert({
  observation,
  canceling,
  onCancel,
}: {
  observation: UpdateObservation
  canceling: boolean
  onCancel: () => void
}) {
  return (
    <Alert className="py-2">
      <AlertTitle className="text-sm">Package update in progress</AlertTitle>
      <AlertDescription className="space-y-2 text-xs">
        Another session or tool started a package update. Tako is watching it
        live; updates here stay paused until it finishes.
        <UpdateLivePanel
          observation={observation}
          onCancel={onCancel}
          canceling={canceling}
        />
      </AlertDescription>
    </Alert>
  )
}

function LogCollapsible({ log }: { log: UpdateObservation["log"] }) {
  return (
    <Collapsible>
      <CollapsibleTrigger render={<Button variant="ghost" size="sm" />}>
        View update log ({log.length})
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="max-h-40 overflow-auto rounded-md border bg-muted/20 p-2 font-mono text-xs">
          {log.map((entry) => (
            <div
              key={`${entry.timestamp ?? ""}:${entry.packageId}:${entry.status}`}
              className="flex gap-2"
            >
              <span className="w-24 shrink-0 text-muted-foreground">
                {entry.statusLabel}
              </span>
              <span className="truncate">
                {formatPackageId(entry.packageId)}
              </span>
            </div>
          ))}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
