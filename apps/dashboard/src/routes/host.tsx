import { createFileRoute } from "@tanstack/react-router"
import { HostPage } from "@/components/pages/host-page"

export const Route = createFileRoute("/host")({
  component: HostPage,
})
