import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

export type StatusTone = "success" | "warning" | "danger" | "info" | "muted"

const dots: Record<StatusTone, string> = {
  success: "bg-success",
  warning: "bg-warning",
  danger: "bg-destructive",
  info: "bg-primary",
  muted: "bg-muted-foreground/50",
}

// StatusBadge pairs a colored dot with a label, so state never relies on
// color alone.
export function StatusBadge({
  tone,
  children,
  className,
}: {
  tone: StatusTone
  children: React.ReactNode
  className?: string
}) {
  return (
    <Badge
      variant="outline"
      className={cn(
        "gap-1.5 font-normal",
        tone === "danger" && "border-destructive/40 text-destructive",
        className
      )}
    >
      <span
        className={cn("size-1.5 shrink-0 rounded-full", dots[tone])}
        aria-hidden="true"
      />
      {children}
    </Badge>
  )
}

// unitStatus describes a systemd unit's active and sub state.
export function unitStatus(
  activeState: string,
  subState: string
): { tone: StatusTone; label: string } {
  switch (activeState) {
    case "active":
      if (subState === "running") return { tone: "success", label: "Running" }
      if (subState === "exited") return { tone: "muted", label: "Exited" }
      if (subState === "listening")
        return { tone: "success", label: "Listening" }
      if (subState === "waiting") return { tone: "success", label: "Waiting" }
      if (subState === "mounted") return { tone: "success", label: "Mounted" }
      return { tone: "success", label: "Active" }
    case "failed":
      return { tone: "danger", label: "Failed" }
    case "activating":
      return { tone: "warning", label: "Starting" }
    case "deactivating":
      return { tone: "warning", label: "Stopping" }
    case "reloading":
      return { tone: "warning", label: "Reloading" }
    default:
      return {
        tone: "muted",
        label:
          subState === "dead" || !subState ? "Inactive" : capitalize(subState),
      }
  }
}

export function capitalize(value: string) {
  return value ? value[0].toUpperCase() + value.slice(1) : value
}
