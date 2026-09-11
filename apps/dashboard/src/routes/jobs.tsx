import { createFileRoute } from "@tanstack/react-router"
import { JobsPage } from "@/components/pages/jobs-page"

export const Route = createFileRoute("/jobs")({
  component: JobsPage,
})
