import { createFileRoute, Outlet } from "@tanstack/react-router"

export const Route = createFileRoute("/processes")({
  component: () => <Outlet />,
})
