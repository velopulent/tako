import { createFileRoute } from "@tanstack/react-router"
import { TimersPage } from "@/components/pages/timers-page"

export const Route = createFileRoute("/timers")({
  component: TimersPage,
})
