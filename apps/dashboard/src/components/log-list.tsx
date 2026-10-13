import type { StatusTone } from "@/components/status-badge"
import { Badge } from "@/components/ui/badge"
import type { LogEntry } from "@/lib/api"
import { formatDateTime, formatTimestamp } from "@/lib/format"
import { cn } from "@/lib/utils"

const priorities: { label: string; tone: StatusTone }[] = [
  { label: "Emergency", tone: "danger" },
  { label: "Alert", tone: "danger" },
  { label: "Critical", tone: "danger" },
  { label: "Error", tone: "danger" },
  { label: "Warning", tone: "warning" },
  { label: "Notice", tone: "info" },
  { label: "Info", tone: "muted" },
  { label: "Debug", tone: "muted" },
]

// logPriority names a syslog priority. Entries without a PRIORITY field
// (kernel audit records, for example) are reported as unknown.
export function logPriority(value: string | undefined) {
  if (value === undefined || value === "")
    return { label: "—", tone: "muted" as StatusTone }
  return (
    priorities[Number(value)] ?? { label: value, tone: "muted" as StatusTone }
  )
}

export function PriorityBadge({
  value,
  className,
}: {
  value: string | undefined
  className?: string
}) {
  const priority = logPriority(value)
  return (
    <Badge
      variant={priority.tone === "danger" ? "destructive" : "outline"}
      className={cn(
        "w-[5.5rem] justify-center font-normal",
        priority.tone === "warning" && "border-warning/50 text-warning",
        className
      )}
    >
      {priority.label}
    </Badge>
  )
}

// LogList renders journal entries compactly, newest first.
export function LogList({
  items,
  showUnit = true,
  className,
}: {
  items: LogEntry[]
  showUnit?: boolean
  className?: string
}) {
  return (
    <ol className={cn("flex flex-col divide-y font-mono text-xs", className)}>
      {items.map((entry, index) => (
        <li
          key={entry.id ?? `${entry.timestamp}-${index}`}
          className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 py-2 sm:grid-cols-[9.5rem_auto_1fr]"
        >
          <time
            dateTime={entry.timestamp}
            title={formatDateTime(entry.timestamp)}
            className="text-muted-foreground tabular-nums"
          >
            {formatTimestamp(entry.timestamp)}
          </time>
          <PriorityBadge
            value={entry.priority}
            className="row-start-1 sm:row-auto"
          />
          <p className="col-span-2 min-w-0 break-words whitespace-pre-wrap sm:col-span-1">
            {showUnit && entry.unit && (
              <span className="mr-2 font-sans font-medium">{entry.unit}</span>
            )}
            {entry.message}
          </p>
        </li>
      ))}
    </ol>
  )
}
