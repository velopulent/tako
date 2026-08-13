import type { DiagnosticJob } from "@/lib/api"

export const activeUpdateJobStates = new Set<DiagnosticJob["state"]>([
  "pending",
  "running",
])
